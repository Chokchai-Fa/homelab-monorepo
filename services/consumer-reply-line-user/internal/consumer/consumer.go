package consumer

import (
	"encoding/json"
	"log"

	"github.com/line/line-bot-sdk-go/v7/linebot"
	"github.com/nats-io/nats.go"
)

// Subject and QueueGroup must match the publisher in
// services/consumer-llm-processor.
const (
	Subject    = "line.chat.reply"
	QueueGroup = "consumer-reply-line-user"
)

// ReplyEvent is published by consumer-llm-processor.
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
			log.Printf("Failed to unmarshal event: %v", err)
			return
		}
		c.Handle(event)
	})
}

// Handle delivers one reply: the (free) reply token first; if it has expired
// or was already consumed, falls back to a push message.
func (c *Consumer) Handle(event ReplyEvent) {
	if event.UserID == "" && event.ReplyToken == "" {
		log.Printf("Dropping event without user_id or reply_token")
		return
	}
	if event.Text == "" {
		log.Printf("Dropping empty reply for user %s", event.UserID)
		return
	}

	message := linebot.NewTextMessage(event.Text)

	if event.ReplyToken != "" {
		if _, err := c.bot.ReplyMessage(event.ReplyToken, message).Do(); err == nil {
			log.Printf("Replied to user %s via reply token", event.UserID)
			return
		} else {
			log.Printf("ReplyMessage failed for %s, falling back to push: %v", event.UserID, err)
		}
	}

	if event.UserID == "" {
		return
	}
	if _, err := c.bot.PushMessage(event.UserID, message).Do(); err != nil {
		log.Printf("PushMessage failed for %s: %v", event.UserID, err)
		return
	}
	log.Printf("Replied to user %s via push message", event.UserID)
}
