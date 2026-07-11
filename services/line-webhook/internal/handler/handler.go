package handler

import (
	"context"

	"github.com/labstack/echo/v4"
	"github.com/line/line-bot-sdk-go/v7/linebot"

	"line-webhook/internal/publisher"
)

// LineHandler is the concrete implementation of Handler.
// It never calls the LINE API itself: every outgoing message is published to
// NATS and delivered by consumer-reply-line-user.
type LineHandler struct {
	cfg      *Config
	pub      EventPublisher
	sessions SessionStore
	bot      *linebot.Client
}

// Config holds configuration for the handler.
type Config struct {
	ChannelSecret string
	ChannelToken  string
	// AIPrefix starts an AI session (e.g. "/ai"); AIPrefix+"-end" ends it.
	AIPrefix string
}

// SessionStore tracks which users are in an active AI session; nil disables
// session mode (only the explicit prefix routes to the AI).
type SessionStore interface {
	Start(ctx context.Context, userID string) error
	Active(ctx context.Context, userID string) bool
	End(ctx context.Context, userID string) error
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
func New(cfg *Config, pub EventPublisher, sessions SessionStore, bot *linebot.Client) Handler {
	return &LineHandler{cfg: cfg, pub: pub, sessions: sessions, bot: bot}
}
