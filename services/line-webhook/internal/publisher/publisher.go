package publisher

import (
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"

	"github.com/Chokchai-Fa/homelab-monorepo/libs/natsutil"
)

// Subjects of the LINE chat pipeline. AIRequestSubject must match the
// subscriber in services/consumer-llm-processor; ReplySubject must match
// services/consumer-reply-line-user; PostbackSubject and ProfileSubject
// must match services/consumer-reminder.
const (
	AIRequestSubject = "line.chat.ai_request"
	ReplySubject     = "line.chat.reply"
	PostbackSubject  = "line.chat.postback"
	ProfileSubject   = "line.chat.profile"
)

// AIRequestEvent is consumed by consumer-llm-processor. Text has the AI
// prefix (e.g. "/ai") already stripped. ImageKey, when set, is the Redis key
// (see internal/imagecache) holding an image the user attached; Text may be
// empty in that case.
type AIRequestEvent struct {
	UserID     string `json:"user_id"`
	ReplyToken string `json:"reply_token"`
	Text       string `json:"text"`
	ImageKey   string `json:"image_key,omitempty"`
	ImageMime  string `json:"image_mime,omitempty"`
	Timestamp  int64  `json:"timestamp"`
}

// ReplyEvent is consumed by consumer-reply-line-user, which sends it to LINE.
type ReplyEvent struct {
	UserID     string `json:"user_id"`
	ReplyToken string `json:"reply_token"`
	Text       string `json:"text"`
}

// PostbackEvent carries a quick-reply button press to consumer-reminder.
// Data is the raw postback payload (query-string style, e.g.
// "flow=rem&a=target&v=self").
type PostbackEvent struct {
	UserID     string `json:"user_id"`
	ReplyToken string `json:"reply_token"`
	Data       string `json:"data"`
	Timestamp  int64  `json:"timestamp"`
}

// ProfileEvent hands a LINE profile fetched by the webhook to
// consumer-reminder, which owns the line_users table.
type ProfileEvent struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	Timestamp   int64  `json:"timestamp"`
}

// Publisher publishes LINE chat events to the durable JetStream pipeline.
type Publisher struct {
	nc      *nats.Conn
	js      nats.JetStreamContext
	closing atomic.Bool
}

// New connects to NATS. The webhook must keep accepting LINE events when the
// broker is down, so callers should treat a connection error as non-fatal.
// RetryOnFailedConnect makes nats.Connect return a usable connection even when
// the broker is unreachable at startup (e.g. NATS restarting during a rollout):
// it reconnects in the background instead of leaving the publisher permanently
// nil. Without it, a failed initial connect would silently drop every message
// for the lifetime of the pod.
//
// MaxReconnects(-1) reconnects forever, but a NATS connection can still reach
// the terminal CLOSED state (e.g. a permanent auth/handshake failure). Unlike
// the consumers, the webhook stays alive on NATS trouble, so a closed
// connection would leave it holding a dead *nats.Conn and silently dropping
// every publish until the pod is restarted by hand. onClosed lets main mirror
// the consumers' self-heal model: exit the process so Kubernetes restarts the
// pod with a fresh connection instead of it limping along disconnected.
func New(url, user, password string, onClosed func()) (*Publisher, error) {
	nc, err := nats.Connect(url,
		nats.UserInfo(user, password),
		nats.Name("line-webhook"),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Warn().Err(err).Msg("nats: disconnected - buffering events until reconnect")
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Info().Str("url", nc.ConnectedUrl()).Msg("nats: reconnected")
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			log.Error().Msg("nats: connection closed permanently - triggering restart")
			if onClosed != nil {
				onClosed()
			}
		}),
	)
	if err != nil {
		return nil, err
	}

	// The LINE pipeline publishes into the durable memory-backed JetStream
	// stream, so an event survives a consumer restart. Ensuring the stream here
	// too means the webhook can be the first pod up after a NATS restart.
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, err
	}
	// Ensuring the stream is a server round trip, so it fails whenever the
	// broker is unreachable - exactly the startup case RetryOnFailedConnect
	// exists to survive. Warn and carry on: every consumer ensures the same
	// stream, and publishes reconnect on their own once the broker is back.
	if err := natsutil.EnsureLineChatStream(js); err != nil {
		log.Warn().Err(err).Msg("nats: could not ensure the LINE chat stream - a consumer will create it")
	}

	p := &Publisher{nc: nc, js: js}
	natsutil.StartWatchdog(nc, &p.closing)
	return p, nil
}

// PublishAIRequest sends one event to the AI request subject.
func (p *Publisher) PublishAIRequest(event AIRequestEvent) error {
	return p.publish(AIRequestSubject, event)
}

// PublishReply sends one event to the reply subject.
func (p *Publisher) PublishReply(event ReplyEvent) error {
	return p.publish(ReplySubject, event)
}

// PublishPostback sends one event to the postback subject.
func (p *Publisher) PublishPostback(event PostbackEvent) error {
	return p.publish(PostbackSubject, event)
}

// PublishProfile sends one event to the profile subject.
func (p *Publisher) PublishProfile(event ProfileEvent) error {
	return p.publish(ProfileSubject, event)
}

func (p *Publisher) publish(subject string, event any) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = p.js.Publish(subject, data)
	return err
}

func (p *Publisher) Close() {
	p.closing.Store(true)
	p.nc.Drain()
}
