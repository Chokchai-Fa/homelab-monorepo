package handler

import (
	"github.com/labstack/echo/v4"

	"line-webhook/internal/publisher"
)

// LineHandler is the concrete implementation of Handler.
// It never calls the LINE API itself: every outgoing message is published to
// NATS and delivered by consumer-reply-line-user.
type LineHandler struct {
	cfg *Config
	pub EventPublisher
}

// Config holds configuration for the handler.
type Config struct {
	ChannelSecret string
	// AIPrefix marks a message as an AI request (e.g. "/ai").
	AIPrefix string
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
func New(cfg *Config, pub EventPublisher) Handler {
	return &LineHandler{cfg: cfg, pub: pub}
}
