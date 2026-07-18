package handler

import (
	"context"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/line/line-bot-sdk-go/v7/linebot"

	"line-webhook/internal/publisher"
)

// LineHandler is the concrete implementation of Handler.
// It never calls the LINE API itself for replies: every outgoing message is
// published to NATS and delivered by consumer-reply-line-user. It does call
// the LINE content API directly to download image attachments, since only
// the webhook holds the channel token.
type LineHandler struct {
	cfg      *Config
	pub      EventPublisher
	sessions SessionStore
	images   ImageStore
	bot      *linebot.Client
}

// Config holds configuration for the handler.
type Config struct {
	ChannelSecret string
	ChannelToken  string
	// AIPrefix starts an AI session (e.g. "/ai"); AIPrefix+"-end" ends it.
	AIPrefix string
	// ImageTTL bounds how long a downloaded image waits in Redis for
	// consumer-llm-processor to pick it up.
	ImageTTL time.Duration
	// MaxImageBytes caps how large an image the webhook will download and
	// forward, to protect Redis and the downstream vision providers.
	MaxImageBytes int64
}

// SessionStore tracks which users are in an active AI session; nil disables
// session mode (only the explicit prefix routes to the AI).
type SessionStore interface {
	Start(ctx context.Context, userID string) error
	Active(ctx context.Context, userID string) bool
	End(ctx context.Context, userID string) error
}

// ImageStore stashes downloaded image bytes for consumer-llm-processor to
// pick up; nil disables image support (images are declined with a hint).
type ImageStore interface {
	Put(ctx context.Context, messageID string, data []byte, ttl time.Duration) error
}

// Handler defines the public behavior for a webhook handler.
// It is kept small so it can be easily mocked in tests.
type Handler interface {
	Webhook(c echo.Context) error
}

// EventPublisher publishes chat events; nil means NATS is unavailable and
// incoming messages are dropped (logged).
type EventPublisher interface {
	PublishAIRequest(event publisher.AIRequestEvent) error
	PublishReply(event publisher.ReplyEvent) error
}

// New creates a new LineHandler instance that implements Handler.
func New(cfg *Config, pub EventPublisher, sessions SessionStore, images ImageStore, bot *linebot.Client) Handler {
	return &LineHandler{cfg: cfg, pub: pub, sessions: sessions, images: images, bot: bot}
}
