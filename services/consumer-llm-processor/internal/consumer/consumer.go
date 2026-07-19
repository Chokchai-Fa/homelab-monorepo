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
	"consumer-llm-processor/internal/extract"
	"consumer-llm-processor/internal/store"
)

// Subjects and queue group shared across the LINE chat pipeline:
// line-webhook publishes RequestSubject, this service consumes it and
// publishes ReplySubject, consumer-reply-line-user delivers it to LINE.
const (
	RequestSubject = "line.chat.ai_request"
	ReplySubject   = "line.chat.reply"
	// ReminderRequestSubject hands reminder-intent messages to
	// consumer-reminder; this service only detects the intent.
	ReminderRequestSubject = "line.chat.reminder_request"
	QueueGroup             = "consumer-llm-processor"

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

// ReminderRequestEvent is consumed by consumer-reminder, which owns the
// whole reminder conversation. This service does the language work first:
// Message and RemindAt (RFC3339, +07:00) carry what the extractor LLM found
// in Text; either may be empty, in which case consumer-reminder asks the
// user for the missing part.
type ReminderRequestEvent struct {
	UserID     string `json:"user_id"`
	ReplyToken string `json:"reply_token"`
	Text       string `json:"text"`
	Message    string `json:"message,omitempty"`
	RemindAt   string `json:"remind_at,omitempty"`
	Timestamp  int64  `json:"timestamp"`
}

// FlowChecker reports whether the user is mid reminder-conversation (state
// owned by consumer-reminder); nil disables mid-flow routing.
type FlowChecker interface {
	Active(ctx context.Context, userID string) bool
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
	store     store.Store
	ai        Responder
	images    ImageStore
	nc        *nats.Conn
	debounce  *Debouncer
	extractor extract.Extractor
	flows     FlowChecker
}

// New creates the consumer. debounceWindow > 0 buffers each user's burst of
// chat messages until they've been quiet that long (capped at maxWait) and
// answers them as one merged request; 0 answers every message individually.
// extractor and flows serve the reminder handoff; both may be nil.
func New(s store.Store, p Responder, images ImageStore, nc *nats.Conn, debounceWindow, debounceMaxWait time.Duration, extractor extract.Extractor, flows FlowChecker) *Consumer {
	c := &Consumer{store: s, ai: p, images: images, nc: nc, extractor: extractor, flows: flows}
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
		// Reminder traffic (trigger keyword or mid-flow answer) must not sit
		// in the debounce buffer: flow steps feel instant, and merging a
		// burst would corrupt extraction.
		if c.debounce != nil && !c.isReminderPath(event) {
			c.debounce.Add(event)
			return
		}
		c.Handle(event)
	})
}

// isReminderPath reports whether the event belongs to the reminder pipeline:
// a trigger keyword, or any text while the user's reminder flow is active.
func (c *Consumer) isReminderPath(event RequestEvent) bool {
	if event.ImageKey != "" {
		return false
	}
	if isReminderCommand(event.Text) {
		return true
	}
	if c.flows == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return c.flows.Active(ctx, event.UserID)
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

	// Trigger keywords and mid-flow answers skip the chat LLM entirely.
	if c.isReminderPath(event) {
		c.handOffReminder(ctx, event)
		return
	}

	text, imageKey, reminder := c.respond(ctx, event)
	if reminder {
		// The classifier spotted a natural-language reminder ask.
		c.handOffReminder(ctx, event)
		return
	}
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

// isReminderCommand reports whether text is a reminder trigger keyword.
// Must match line-webhook's isReminderRequest and consumer-reminder's
// isTrigger.
func isReminderCommand(text string) bool {
	trimmed := strings.TrimSpace(text)
	return trimmed == "/reminder" ||
		strings.HasPrefix(trimmed, "/reminder ") ||
		strings.HasPrefix(trimmed, "ตั้งเตือน")
}

// isCancelText mirrors consumer-reminder's isCancelText: a typed cancel
// ends the flow over there, so don't burn an extraction call on it here.
func isCancelText(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "ยกเลิก", "cancel", "/cancel":
		return true
	default:
		return false
	}
}

// stripReminderTrigger removes the trigger keyword, leaving any trailing
// details for the extractor.
func stripReminderTrigger(trimmed string) string {
	rest := strings.TrimPrefix(trimmed, "/reminder")
	rest = strings.TrimPrefix(rest, "ตั้งเตือน")
	return strings.TrimSpace(rest)
}

// bangkok anchors extraction and the published RemindAt to UTC+7. A fixed
// zone avoids depending on tzdata in the container image.
var bangkok = time.FixedZone("ICT", 7*60*60)

// handOffReminder runs the extractor over the message and publishes a
// structured command for consumer-reminder: what to remind, and when
// (RFC3339, +07:00). Extraction is best-effort - missing fields just make
// consumer-reminder ask the user.
func (c *Consumer) handOffReminder(ctx context.Context, event RequestEvent) {
	trimmed := strings.TrimSpace(event.Text)
	details := trimmed
	if isReminderCommand(trimmed) {
		details = stripReminderTrigger(trimmed)
	}

	out := ReminderRequestEvent{
		UserID:     event.UserID,
		ReplyToken: event.ReplyToken,
		Text:       event.Text,
		Timestamp:  event.Timestamp,
	}
	if details != "" && c.extractor != nil && !isCancelText(details) {
		now := time.Now().In(bangkok)
		result, err := c.extractor.Extract(ctx, details, now)
		if err != nil {
			log.Error().Str("user_id", event.UserID).Err(err).Msg("reminder: extraction failed - deterministic time parsing only")
			result = extract.Result{}
		}
		out.Message = result.Message
		// Explicit numeric dates/clock times in the text override the LLM
		// (and cover for it entirely when the call failed).
		if at := extract.Refine(details, result.RemindAt, now); !at.IsZero() {
			out.RemindAt = at.In(bangkok).Format(time.RFC3339)
		}
		log.Info().Str("user_id", event.UserID).Str("message", out.Message).Str("remind_at", out.RemindAt).Msg("reminder: extracted")
	}

	data, err := json.Marshal(out)
	if err != nil {
		log.Error().Str("user_id", event.UserID).Err(err).Msg("publish: failed to marshal reminder request")
		return
	}
	if err := c.nc.Publish(ReminderRequestSubject, data); err != nil {
		log.Error().Str("subject", ReminderRequestSubject).Str("user_id", event.UserID).Err(err).Msg("publish: failed to publish reminder request")
		return
	}
	log.Info().Str("subject", ReminderRequestSubject).Str("user_id", event.UserID).Msg("publish: reminder request handed off")
}

// respond computes the reply for the event: the text to send and, when the
// user asked to draw something, the key of the generated image line-webhook
// serves at /images/<key>. The third result is true when the message is a
// reminder ask - the caller republishes it to consumer-reminder and nothing
// is answered or stored here.
func (c *Consumer) respond(ctx context.Context, event RequestEvent) (string, string, bool) {
	query := strings.TrimSpace(event.Text)

	if event.ImageKey == "" {
		switch {
		case query == "":
			log.Info().Str("user_id", event.UserID).Msg("respond: empty query - sending usage hint")
			return "Ask me anything after /ai, e.g. \"/ai แนะนำที่เที่ยวในเชียงใหม่\" or \"/ai explain kubernetes\".\nSend \"/ai /reset\" to start a new conversation.", "", false
		case isResetCommand(query):
			if err := c.store.Clear(ctx, event.UserID); err != nil {
				log.Error().Str("user_id", event.UserID).Err(err).Msg("respond: failed to clear history")
				return "Sorry, I couldn't reset the conversation. Please try again.", "", false
			}
			log.Info().Str("user_id", event.UserID).Msg("respond: history cleared")
			return "Conversation history cleared. เริ่มบทสนทนาใหม่ได้เลย!", "", false
		}
	}

	var image *ai.Image
	historyText := query
	if event.ImageKey != "" {
		if c.images == nil {
			log.Error().Str("user_id", event.UserID).Msg("respond: image dropped - no image store configured")
			return "Sorry, I can't view images right now. Please try again later.", "", false
		}
		data, err := c.images.Take(ctx, event.ImageKey)
		if err != nil {
			log.Error().Str("user_id", event.UserID).Str("image_key", event.ImageKey).Err(err).Msg("respond: failed to fetch image - it may have expired")
			return "Sorry, I couldn't retrieve that image in time. Please send it again.", "", false
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
			return "Sorry, I can't view images right now. Please try again later.\nขออภัย ตอนนี้ฉันดูรูปไม่ได้ กรุณาลองใหม่ภายหลัง", "", false
		}
		return "Sorry, the AI is unavailable right now. Please try again later.\nขออภัย ตอนนี้ AI ไม่พร้อมใช้งาน กรุณาลองใหม่ภายหลัง", "", false
	}
	if result.ReminderIntent {
		return "", "", true
	}
	log.Info().Str("user_id", event.UserID).Dur("duration", time.Since(start)).Int("answer_chars", len(result.Text)).Bool("image", result.ImageData != nil).Msg("respond: llm answered")

	answer, generatedKey := result.Text, ""
	if result.ImageData != nil {
		generatedKey, err = c.stashGenerated(ctx, event.UserID, result.ImageData)
		if err != nil {
			return "Sorry, I drew your picture but couldn't deliver it. Please try again.\nขออภัย วาดรูปเสร็จแล้วแต่ส่งไม่ได้ ลองใหม่อีกครั้งน้า~", "", false
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
	return answer, generatedKey, false
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
