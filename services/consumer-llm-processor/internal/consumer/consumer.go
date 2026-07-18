package consumer

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"

	"consumer-llm-processor/internal/ai"
	"consumer-llm-processor/internal/store"
)

// Subjects and queue group shared across the LINE chat pipeline:
// line-webhook publishes RequestSubject, this service consumes it and
// publishes ReplySubject, consumer-reply-line-user delivers it to LINE.
const (
	RequestSubject = "line.chat.ai_request"
	ReplySubject   = "line.chat.reply"
	QueueGroup     = "consumer-llm-processor"

	generateTimeout = 60 * time.Second
)

// RequestEvent is published by line-webhook. Text already has the AI prefix
// (e.g. "/ai") stripped. ImageKey, when set, names an image in Redis (see
// internal/imagecache) that the user attached; Text may be empty in that
// case.
type RequestEvent struct {
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

// ImageStore fetches an image line-webhook stashed in Redis for this
// request, deleting it in the same call since it is single-use.
type ImageStore interface {
	Take(ctx context.Context, messageID string) ([]byte, error)
}

// Consumer subscribes to AI requests, asks the LLM provider (usually the
// difficulty router) with conversation context and publishes the answer as a
// reply event.
type Consumer struct {
	store    store.Store
	ai       ai.Provider
	images   ImageStore
	nc       *nats.Conn
	debounce *Debouncer
}

// New creates the consumer. debounceWindow > 0 buffers each user's burst of
// chat messages until they've been quiet that long (capped at maxWait) and
// answers them as one merged request; 0 answers every message individually.
func New(s store.Store, p ai.Provider, images ImageStore, nc *nats.Conn, debounceWindow, debounceMaxWait time.Duration) *Consumer {
	c := &Consumer{store: s, ai: p, images: images, nc: nc}
	if debounceWindow > 0 {
		c.debounce = NewDebouncer(debounceWindow, debounceMaxWait, c.Handle)
	}
	return c
}

// Subscribe attaches the consumer to NATS as a queue subscriber so future
// replicas share the work.
func (c *Consumer) Subscribe() (*nats.Subscription, error) {
	return c.nc.QueueSubscribe(RequestSubject, QueueGroup, func(msg *nats.Msg) {
		var event RequestEvent
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			log.Error().Str("subject", RequestSubject).Err(err).Msg("consume: failed to unmarshal event")
			return
		}
		if c.debounce != nil {
			c.debounce.Add(event)
			return
		}
		c.Handle(event)
	})
}

// Flush answers any buffered bursts immediately; call before shutdown so
// buffered messages aren't lost.
func (c *Consumer) Flush() {
	if c.debounce != nil {
		c.debounce.FlushAll()
	}
}

// Handle processes one AI request and publishes the reply event.
func (c *Consumer) Handle(event RequestEvent) {
	if event.UserID == "" {
		log.Error().Str("subject", RequestSubject).Msg("consume: dropping event without user_id")
		return
	}
	log.Info().Str("user_id", event.UserID).Str("text", event.Text).Msg("consume: AI request received")

	ctx, cancel := context.WithTimeout(context.Background(), generateTimeout)
	defer cancel()

	reply := ReplyEvent{
		UserID:     event.UserID,
		ReplyToken: event.ReplyToken,
		Text:       c.respond(ctx, event),
	}
	data, err := json.Marshal(reply)
	if err != nil {
		log.Error().Str("user_id", event.UserID).Err(err).Msg("publish: failed to marshal reply")
		return
	}
	if err := c.nc.Publish(ReplySubject, data); err != nil {
		log.Error().Str("subject", ReplySubject).Str("user_id", event.UserID).Err(err).Msg("publish: failed to publish reply")
		return
	}
	log.Info().Str("subject", ReplySubject).Str("user_id", event.UserID).Msg("publish: reply published")
}

// defaultImagePrompt is used when the user sends an image with no text (the
// normal case: LINE image messages carry no caption).
const defaultImagePrompt = "Describe what's in this image."

// respond computes the reply text for the event.
func (c *Consumer) respond(ctx context.Context, event RequestEvent) string {
	query := strings.TrimSpace(event.Text)

	if event.ImageKey == "" {
		switch strings.ToLower(query) {
		case "":
			log.Info().Str("user_id", event.UserID).Msg("respond: empty query - sending usage hint")
			return "Ask me anything after /ai, e.g. \"/ai แนะนำที่เที่ยวในเชียงใหม่\" or \"/ai explain kubernetes\".\nSend \"/ai /reset\" to start a new conversation."
		case "/reset", "ล้าง", "เริ่มใหม่":
			if err := c.store.Clear(ctx, event.UserID); err != nil {
				log.Error().Str("user_id", event.UserID).Err(err).Msg("respond: failed to clear history")
				return "Sorry, I couldn't reset the conversation. Please try again."
			}
			log.Info().Str("user_id", event.UserID).Msg("respond: history cleared")
			return "Conversation history cleared. เริ่มบทสนทนาใหม่ได้เลย!"
		}
	}

	var image *ai.Image
	historyText := query
	if event.ImageKey != "" {
		if c.images == nil {
			log.Error().Str("user_id", event.UserID).Msg("respond: image dropped - no image store configured")
			return "Sorry, I can't view images right now. Please try again later."
		}
		data, err := c.images.Take(ctx, event.ImageKey)
		if err != nil {
			log.Error().Str("user_id", event.UserID).Str("image_key", event.ImageKey).Err(err).Msg("respond: failed to fetch image - it may have expired")
			return "Sorry, I couldn't retrieve that image in time. Please send it again."
		}
		image = &ai.Image{Data: data, MimeType: event.ImageMime}
		if query == "" {
			query = defaultImagePrompt
		}
		historyText = "[user sent an image] " + query
	}

	history, err := c.store.GetRecent(ctx, event.UserID)
	if err != nil {
		// Degrade to a context-less answer rather than failing the request.
		log.Error().Str("user_id", event.UserID).Err(err).Msg("respond: failed to load history - continuing without context")
		history = nil
	} else {
		log.Info().Str("user_id", event.UserID).Int("messages", len(history)).Msg("respond: history loaded")
	}

	start := time.Now()
	answer, err := c.ai.Reply(ctx, history, query, image)
	if err != nil {
		log.Error().Str("user_id", event.UserID).Err(err).Msg("respond: llm request failed")
		if image != nil {
			return "Sorry, I can't view images right now. Please try again later.\nขออภัย ตอนนี้ฉันดูรูปไม่ได้ กรุณาลองใหม่ภายหลัง"
		}
		return "Sorry, the AI is unavailable right now. Please try again later.\nขออภัย ตอนนี้ AI ไม่พร้อมใช้งาน กรุณาลองใหม่ภายหลัง"
	}
	log.Info().Str("user_id", event.UserID).Dur("duration", time.Since(start)).Int("answer_chars", len(answer)).Msg("respond: llm answered")

	if err := c.store.Append(ctx, event.UserID, store.RoleUser, historyText); err != nil {
		log.Error().Str("user_id", event.UserID).Err(err).Msg("respond: failed to store user message")
	}
	if err := c.store.Append(ctx, event.UserID, store.RoleModel, answer); err != nil {
		log.Error().Str("user_id", event.UserID).Err(err).Msg("respond: failed to store model reply")
	}
	log.Info().Str("user_id", event.UserID).Msg("respond: conversation stored")
	return answer
}
