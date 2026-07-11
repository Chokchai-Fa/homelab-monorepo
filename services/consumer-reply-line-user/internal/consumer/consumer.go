package consumer

import (
	"encoding/json"

	"github.com/line/line-bot-sdk-go/v7/linebot"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// Subject and QueueGroup must match the publisher in
// services/consumer-llm-processor.
const (
	Subject    = "line.chat.reply"
	QueueGroup = "consumer-reply-line-user"
)

// ReplyEvent is published by line-webhook and consumer-llm-processor.
type ReplyEvent struct {
	UserID     string `json:"user_id"`
	ReplyToken string `json:"reply_token"`
	Text       string `json:"text"`
}

// Consumer subscribes to reply events and delivers them to LINE users.
type Consumer struct {
	bot *linebot.Client
}

func New(bot *linebot.Client) *Consumer {
	return &Consumer{bot: bot}
}

// Subscribe attaches the consumer to NATS as a queue subscriber so future
// replicas share the work.
func (c *Consumer) Subscribe(nc *nats.Conn) (*nats.Subscription, error) {
	return nc.QueueSubscribe(Subject, QueueGroup, func(msg *nats.Msg) {
		var event ReplyEvent
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			log.Error().Str("subject", Subject).Err(err).Msg("consume: failed to unmarshal event")
			return
		}
		c.Handle(event)
	})
}

// Handle delivers one reply: the (free) reply token first; if it has expired
// or was already consumed, falls back to a push message.
func (c *Consumer) Handle(event ReplyEvent) {
	if event.UserID == "" && event.ReplyToken == "" {
		log.Error().Str("subject", Subject).Msg("consume: dropping event without user_id or reply_token")
		return
	}
	if event.Text == "" {
		log.Error().Str("user_id", event.UserID).Msg("consume: dropping empty reply")
		return
	}
	log.Info().Str("user_id", event.UserID).Int("text_chars", len(event.Text)).Msg("consume: reply event received")

	message := linebot.NewTextMessage(event.Text)

	if event.ReplyToken != "" {
		if _, err := c.bot.ReplyMessage(event.ReplyToken, message).Do(); err == nil {
			log.Info().Str("user_id", event.UserID).Msg("deliver: sent via reply token")
			return
		} else {
			log.Error().Str("user_id", event.UserID).Err(err).Msg("deliver: reply token failed - falling back to push")
		}
	}

	if event.UserID == "" {
		log.Error().Msg("deliver: cannot push - no user_id")
		return
	}
	if _, err := c.bot.PushMessage(event.UserID, message).Do(); err != nil {
		log.Error().Str("user_id", event.UserID).Err(err).Msg("deliver: push message failed")
		return
	}
	log.Info().Str("user_id", event.UserID).Msg("deliver: sent via push message")
}
