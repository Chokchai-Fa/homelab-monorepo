package consumer

import (
	"encoding/json"
	"strings"

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
// ImageKey names a generated image that line-webhook serves publicly at
// /images/<key>; it becomes a LINE image message ahead of any text.
type ReplyEvent struct {
	UserID     string `json:"user_id"`
	ReplyToken string `json:"reply_token"`
	Text       string `json:"text"`
	ImageKey   string `json:"image_key,omitempty"`
}

// Consumer subscribes to reply events and delivers them to LINE users.
// imageBaseURL is the public base (e.g. https://line-webhook.example.com)
// LINE fetches generated images from; empty disables image replies.
type Consumer struct {
	bot          *linebot.Client
	imageBaseURL string
}

func New(bot *linebot.Client, imageBaseURL string) *Consumer {
	return &Consumer{bot: bot, imageBaseURL: strings.TrimRight(imageBaseURL, "/")}
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

// maxMessagesPerCall is LINE's limit on messages per reply/push API call.
const maxMessagesPerCall = 5

// Handle delivers one reply: the (free) reply token first; if it has expired
// or was already consumed, falls back to push messages.
//
// A reply token is single-use, so all parts must go out in one ReplyMessage
// call rather than one call per part - otherwise only the first part would
// use the free token and the rest would fall back to push anyway.
func (c *Consumer) Handle(event ReplyEvent) {
	if event.UserID == "" && event.ReplyToken == "" {
		log.Error().Str("subject", Subject).Msg("consume: dropping event without user_id or reply_token")
		return
	}
	if event.Text == "" && event.ImageKey == "" {
		log.Error().Str("user_id", event.UserID).Msg("consume: dropping empty reply")
		return
	}
	log.Info().Str("user_id", event.UserID).Int("text_chars", len(event.Text)).Bool("image", event.ImageKey != "").Msg("consume: reply event received")

	messages := c.buildMessages(event)
	if len(messages) == 0 {
		log.Error().Str("user_id", event.UserID).Msg("consume: nothing deliverable in reply")
		return
	}

	if event.ReplyToken != "" {
		replyBatch := messages
		if len(replyBatch) > maxMessagesPerCall {
			replyBatch = replyBatch[:maxMessagesPerCall]
		}
		if _, err := c.bot.ReplyMessage(event.ReplyToken, replyBatch...).Do(); err == nil {
			log.Info().Str("user_id", event.UserID).Int("parts", len(replyBatch)).Msg("deliver: sent via reply token")
			messages = messages[len(replyBatch):]
			if len(messages) == 0 {
				return
			}
		} else {
			log.Error().Str("user_id", event.UserID).Err(err).Msg("deliver: reply token failed - falling back to push")
		}
	}

	if event.UserID == "" {
		log.Error().Msg("deliver: cannot push - no user_id")
		return
	}
	for i := 0; i < len(messages); i += maxMessagesPerCall {
		end := i + maxMessagesPerCall
		if end > len(messages) {
			end = len(messages)
		}
		batch := messages[i:end]
		if _, err := c.bot.PushMessage(event.UserID, batch...).Do(); err != nil {
			log.Error().Str("user_id", event.UserID).Err(err).Msg("deliver: push message failed")
			return
		}
		log.Info().Str("user_id", event.UserID).Int("parts", len(batch)).Msg("deliver: sent via push message")
	}
}

// buildMessages turns a reply event into LINE messages: the generated image
// first (as an image message pointing at line-webhook's public /images
// endpoint), then the text split into chat-sized parts.
func (c *Consumer) buildMessages(event ReplyEvent) []linebot.SendingMessage {
	var messages []linebot.SendingMessage
	if event.ImageKey != "" {
		if c.imageBaseURL == "" {
			log.Error().Str("user_id", event.UserID).Msg("build: image reply but IMAGE_BASE_URL not set - sending text only")
		} else {
			url := c.imageBaseURL + "/images/" + event.ImageKey
			messages = append(messages, linebot.NewImageMessage(url, url))
		}
	}
	for _, part := range splitReplyMessages(event.Text) {
		messages = append(messages, linebot.NewTextMessage(part))
	}
	return messages
}

func splitReplyMessages(text string) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}

	parts := strings.Split(trimmed, "\n\n")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		result = append(result, part)
	}
	if len(result) == 0 {
		return []string{trimmed}
	}
	return result
}
