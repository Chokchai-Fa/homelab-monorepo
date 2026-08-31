package natsutil

import (
	"errors"
	"time"

	"github.com/nats-io/nats.go"
)

// LineChatStream is the JetStream stream that makes the LINE chat pipeline
// durable, so a consumer restart no longer drops whatever it had not processed
// yet. It is memory-backed: the NATS server keeps the store in tmpfs, so
// nothing here ever writes to the SD card. The trade-off is deliberate -
// messages survive a consumer restart, not a NATS restart.
const LineChatStream = "LINE_CHAT"

// LineChatSubjects is the wildcard the stream captures. Every hop of the LINE
// pipeline (ai_request, reply, reminder_request, postback, profile, delivery)
// lives under it. The portfolio web chat deliberately does NOT: it is
// request-reply, where a late redelivery would answer nobody, so it stays on
// core NATS.
const LineChatSubjects = "line.chat.>"

const (
	// LineChatAckWait must sit above the slowest handler in the pipeline (an
	// LLM call plus its store round trips) so a slow-but-working message is
	// never redelivered underneath the service still working on it.
	LineChatAckWait = 120 * time.Second
	// LineChatMaxDeliver caps redelivery so a message that crashes its
	// consumer every time is eventually dropped instead of wedging the queue
	// forever. Handlers ack malformed payloads outright, so this only bites a
	// genuine crash loop.
	LineChatMaxDeliver = 5
	// lineChatMaxAge bounds how long an unconsumed message survives a downed
	// consumer. Past this a chat reply is stale enough to be worse than
	// silence.
	lineChatMaxAge = 10 * time.Minute
	// lineChatMaxBytes caps the memory store, well under the server's
	// max_memory_store so the stream can never exhaust it.
	lineChatMaxBytes = 32 << 20
)

// lineChatStreamConfig is the single source of truth for the stream's shape.
// WorkQueue retention drops each message the moment its consumer acks it,
// which keeps a small memory store lean. WorkQueue requires that no two
// consumers overlap on a subject; every line.chat.* subject has exactly one
// logical consumer, so that holds.
func lineChatStreamConfig() *nats.StreamConfig {
	return &nats.StreamConfig{
		Name:      LineChatStream,
		Subjects:  []string{LineChatSubjects},
		Retention: nats.WorkQueuePolicy,
		Storage:   nats.MemoryStorage,
		Discard:   nats.DiscardOld,
		MaxAge:    lineChatMaxAge,
		MaxBytes:  lineChatMaxBytes,
		Replicas:  1,
	}
}

// EnsureLineChatStream creates the LINE chat stream if it is missing and
// otherwise converges its config to match. Every pipeline service calls this
// on startup, so whichever comes up first creates it and the rest are no-ops -
// which is exactly why the config above must live in one place.
func EnsureLineChatStream(js nats.JetStreamContext) error {
	cfg := lineChatStreamConfig()
	if _, err := js.AddStream(cfg); err != nil {
		if errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
			_, uerr := js.UpdateStream(cfg)
			return uerr
		}
		return err
	}
	return nil
}

// LineChatSubOpts returns the subscribe options every durable LINE pipeline
// consumer uses. durable names the JetStream consumer: it is what makes the
// server remember the position across restarts, so it must be stable across
// deploys and unique per subject.
func LineChatSubOpts(durable string) []nats.SubOpt {
	return []nats.SubOpt{
		nats.Durable(durable),
		nats.ManualAck(),
		nats.AckWait(LineChatAckWait),
		nats.MaxDeliver(LineChatMaxDeliver),
		nats.BindStream(LineChatStream),
	}
}
