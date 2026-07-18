package consumer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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

// ReplyEvent is consumed by consumer-reply-line-user, which sends it to
// LINE. ImageKey, when set, names a generated image that line-webhook
// serves publicly at /images/<key>; the reply service turns it into a LINE
// image message.
type ReplyEvent struct {
	UserID     string `json:"user_id"`
	ReplyToken string `json:"reply_token"`
	Text       string `json:"text"`
	ImageKey   string `json:"image_key,omitempty"`
}

// ImageStore moves image bytes through the Redis handoff: Take fetches an
// incoming image line-webhook stashed for this request (deleting it in the
// same call since it is single-use), PutGenerated stashes a generated image
// for line-webhook's public /images endpoint.
type ImageStore interface {
	Take(ctx context.Context, messageID string) ([]byte, error)
	PutGenerated(ctx context.Context, id string, data []byte, ttl time.Duration) error
}

// Responder is the difficulty router: it answers with text or, when the
// user asked to draw something, a generated image.
type Responder interface {
	Route(ctx context.Context, history []store.Message, userMessage string, image *ai.Image) (ai.Result, error)
}

// generatedImageTTL bounds how long a generated image stays servable. LINE
// fetches the URL right after the reply is sent, so an hour is generous.
const generatedImageTTL = time.Hour

// Consumer subscribes to AI requests, asks the LLM provider (usually the
// difficulty router) with conversation context and publishes the answer as a
// reply event.
type Consumer struct {
	store    store.Store
	ai       Responder
	images   ImageStore
	nc       *nats.Conn
	debounce *Debouncer
}

// New creates the consumer. debounceWindow > 0 buffers each user's burst of
// chat messages until they've been quiet that long (capped at maxWait) and
// answers them as one merged request; 0 answers every message individually.
func New(s store.Store, p Responder, images ImageStore, nc *nats.Conn, debounceWindow, debounceMaxWait time.Duration) *Consumer {
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

	text, imageKey := c.respond(ctx, event)
	reply := ReplyEvent{
		UserID:     event.UserID,
		ReplyToken: event.ReplyToken,
		Text:       text,
		ImageKey:   imageKey,
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

// isResetCommand reports whether text is the reset command, in any of its
// accepted spellings. Shared with the debouncer, which must recognize and
// bypass it too - a control command shouldn't sit in the chat buffer
// waiting out the debounce window like an ordinary message.
func isResetCommand(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "/reset", "ล้าง", "เริ่มใหม่":
		return true
	default:
		return false
	}
}

// respond computes the reply for the event: the text to send and, when the
// user asked to draw something, the key of the generated image line-webhook
// serves at /images/<key>.
func (c *Consumer) respond(ctx context.Context, event RequestEvent) (string, string) {
	query := strings.TrimSpace(event.Text)

	if event.ImageKey == "" {
		switch {
		case query == "":
			log.Info().Str("user_id", event.UserID).Msg("respond: empty query - sending usage hint")
			return "Ask me anything after /ai, e.g. \"/ai แนะนำที่เที่ยวในเชียงใหม่\" or \"/ai explain kubernetes\".\nSend \"/ai /reset\" to start a new conversation.", ""
		case isResetCommand(query):
			if err := c.store.Clear(ctx, event.UserID); err != nil {
				log.Error().Str("user_id", event.UserID).Err(err).Msg("respond: failed to clear history")
				return "Sorry, I couldn't reset the conversation. Please try again.", ""
			}
			log.Info().Str("user_id", event.UserID).Msg("respond: history cleared")
			return "Conversation history cleared. เริ่มบทสนทนาใหม่ได้เลย!", ""
		}
	}

	var image *ai.Image
	historyText := query
	if event.ImageKey != "" {
		if c.images == nil {
			log.Error().Str("user_id", event.UserID).Msg("respond: image dropped - no image store configured")
			return "Sorry, I can't view images right now. Please try again later.", ""
		}
		data, err := c.images.Take(ctx, event.ImageKey)
		if err != nil {
			log.Error().Str("user_id", event.UserID).Str("image_key", event.ImageKey).Err(err).Msg("respond: failed to fetch image - it may have expired")
			return "Sorry, I couldn't retrieve that image in time. Please send it again.", ""
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
	result, err := c.ai.Route(ctx, history, query, image)
	if err != nil {
		log.Error().Str("user_id", event.UserID).Err(err).Msg("respond: llm request failed")
		if image != nil {
			return "Sorry, I can't view images right now. Please try again later.\nขออภัย ตอนนี้ฉันดูรูปไม่ได้ กรุณาลองใหม่ภายหลัง", ""
		}
		return "Sorry, the AI is unavailable right now. Please try again later.\nขออภัย ตอนนี้ AI ไม่พร้อมใช้งาน กรุณาลองใหม่ภายหลัง", ""
	}
	log.Info().Str("user_id", event.UserID).Dur("duration", time.Since(start)).Int("answer_chars", len(result.Text)).Bool("image", result.ImageData != nil).Msg("respond: llm answered")

	answer, generatedKey := result.Text, ""
	if result.ImageData != nil {
		generatedKey, err = c.stashGenerated(ctx, event.UserID, result.ImageData)
		if err != nil {
			return "Sorry, I drew your picture but couldn't deliver it. Please try again.\nขออภัย วาดรูปเสร็จแล้วแต่ส่งไม่ได้ ลองใหม่อีกครั้งน้า~", ""
		}
	}

	modelHistory := answer
	if generatedKey != "" {
		modelHistory = strings.TrimSpace("[generated an image] " + answer)
	}
	if err := c.store.Append(ctx, event.UserID, store.RoleUser, historyText); err != nil {
		log.Error().Str("user_id", event.UserID).Err(err).Msg("respond: failed to store user message")
	}
	if err := c.store.Append(ctx, event.UserID, store.RoleModel, modelHistory); err != nil {
		log.Error().Str("user_id", event.UserID).Err(err).Msg("respond: failed to store model reply")
	}
	log.Info().Str("user_id", event.UserID).Msg("respond: conversation stored")
	return answer, generatedKey
}

// stashGenerated puts a generated image into the Redis handoff under a
// random ID and returns the ID.
func (c *Consumer) stashGenerated(ctx context.Context, userID string, data []byte) (string, error) {
	if c.images == nil {
		log.Error().Str("user_id", userID).Msg("respond: generated image dropped - no image store configured")
		return "", errNoImageStore
	}
	id, err := randomID()
	if err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("respond: failed to create image id")
		return "", err
	}
	if err := c.images.PutGenerated(ctx, id, data, generatedImageTTL); err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("respond: failed to stash generated image")
		return "", err
	}
	return id, nil
}

var errNoImageStore = errors.New("image store not configured")

// randomID returns an unguessable URL-safe ID; the ID is the only access
// control on the public /images/<id> endpoint.
func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
