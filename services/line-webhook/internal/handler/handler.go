package handler

import (
	"github.com/labstack/echo/v4"
	"github.com/line/line-bot-sdk-go/v7/linebot"
)

// LineHandler is the concrete implementation of Handler.
type LineHandler struct {
	cfg *Config
	bot *linebot.Client
}

// Config holds configuration for the handler.
type Config struct {
	ChannelSecret string
}

// Handler defines the public behavior for a webhook handler.
// It is kept small so it can be easily mocked in tests.
type Handler interface {
	Webhook(c echo.Context) error
}

// New creates a new LineHandler instance that implements Handler.
func New(cfg *Config, bot *linebot.Client) Handler {
	return &LineHandler{cfg: cfg, bot: bot}
}
