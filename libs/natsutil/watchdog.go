// Package natsutil holds the NATS wiring shared by every service in this
// repo: the reconnect watchdog and the durable LINE chat pipeline (a
// memory-backed JetStream stream). Both are things that MUST agree across
// services - a stream config that drifts between two services would have them
// fight over the stream on startup - so they live here rather than being
// copied per module.
package natsutil

import (
	"os"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

const (
	// watchdogGrace is how long the connection may stay down before the
	// process gives up and exits for a restart. Comfortably above
	// ReconnectWait so an ordinary NATS rollout reconnects well within it.
	watchdogGrace = 90 * time.Second
	// watchdogTick is how often the connection state is sampled.
	watchdogTick = 15 * time.Second
	// consumerWatchdogTick is how often a durable consumer's existence is
	// re-verified. Coarser than watchdogTick: this defends against a rare
	// race, not routine flapping, and each check is a JetStream API round
	// trip against the shared server.
	consumerWatchdogTick = 2 * time.Minute
)

// StartWatchdog exits the process if the NATS connection stays down past the
// grace period.
//
// This covers the "stuck reconnecting forever" state that ClosedHandler never
// catches. MaxReconnects(-1) means the connection retries indefinitely, so a
// name that will not resolve - e.g. an internal .svc.cluster.local name
// NXDOMAIN-cached by a public resolver after a kube-dns blip - leaves
// RetryOnFailedConnect looping without ever reaching the terminal CLOSED
// state. The pod then sits alive with dead subscriptions, silently dropping
// every message, until someone restarts it by hand. Exiting hands the problem
// to Kubernetes, which restarts the pod with fresh resolver state.
//
// closing guards intentional shutdown: a Drain on SIGTERM also stops the
// connection, and that must not be mistaken for a lost one.
func StartWatchdog(nc *nats.Conn, closing *atomic.Bool) {
	go func() {
		downSince := time.Time{}
		for {
			time.Sleep(watchdogTick)
			switch {
			case closing.Load():
				return
			case nc.IsConnected():
				downSince = time.Time{}
			default:
				if downSince.IsZero() {
					downSince = time.Now()
				}
				if time.Since(downSince) > watchdogGrace {
					log.Error().Dur("down", time.Since(downSince)).Msg("nats: not connected past watchdog grace - exiting for restart")
					os.Exit(1)
				}
			}
		}
	}()
}

// VerifyConsumer confirms the durable consumer for a line.chat.* subject
// actually exists on the server.
//
// QueueSubscribe can report success locally - it opens the inbox
// subscription before the server-side consumer create call - while that
// create silently fails to persist, e.g. when several services redeploy at
// once and race on the same memory-backed stream (observed in production:
// three LINE pipeline services rolled within 90s of each other, and one came
// up "subscribed - consumer running" bound to a durable the server never
// actually created). The pod then looks healthy and drops every message
// forever, since nothing ever gets pushed to an inbox no consumer feeds.
// Call this right after Subscribe and treat any error as fatal - a
// crash-loop that keeps retrying the create is far better than silent,
// unbounded message loss.
func VerifyConsumer(js nats.JetStreamContext, durable string) error {
	_, err := js.ConsumerInfo(LineChatStream, durable)
	return err
}

// StartConsumerWatchdog re-verifies durable's existence periodically and
// exits the process if it ever disappears. This is defense in depth on top
// of the startup check in VerifyConsumer: the trigger for the consumer
// silently vanishing after a clean start isn't fully understood, so this
// catches a recurrence rather than assuming the startup check is the whole
// fix.
func StartConsumerWatchdog(js nats.JetStreamContext, durable string, closing *atomic.Bool) {
	go func() {
		for {
			time.Sleep(consumerWatchdogTick)
			if closing.Load() {
				return
			}
			if err := VerifyConsumer(js, durable); err != nil {
				log.Error().Str("durable", durable).Err(err).Msg("nats: durable consumer missing - exiting for restart")
				os.Exit(1)
			}
		}
	}()
}
