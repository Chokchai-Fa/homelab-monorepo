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
