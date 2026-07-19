package consumer

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/line/line-bot-sdk-go/v7/linebot"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"

	"consumer-reply-line-user/internal/flex"
)

// Subject and QueueGroup must match the publisher in
// services/consumer-llm-processor.
const (
	Subject    = "line.chat.reply"
	QueueGroup = "consumer-reply-line-user"

	// DeliverySubject carries delivery acks for reminder events so
	// subscriber-reminder-notifier can mark reminders sent/failed.
	DeliverySubject = "line.chat.delivery"
)

// QuickReply becomes a LINE quick-reply button with a postback action,
// attached to the last message of the reply.
type QuickReply struct {
	Label       string `json:"label"`
	Data        string `json:"data"`
	DisplayText string `json:"display_text,omitempty"`
}

// ReminderPayload carries the raw facts a fired reminder notification needs.
// consumer-reply-line-user builds the flex bubble itself (internal/flex) so
// all LINE-message construction - flex, quick-replies, text splitting - lives
// in the one service whose job is talking to the LINE API; publishers never
// ship pre-rendered flex JSON.
type ReminderPayload struct {
	Message            string    `json:"message"`
	CreatorDisplayName string    `json:"creator_display_name,omitempty"`
	RemindAt           time.Time `json:"remind_at"`
}

// ReplyEvent is published by line-webhook, consumer-llm-processor,
// consumer-reminder and subscriber-reminder-notifier.
// ImageKey names a generated image that line-webhook serves publicly at
// /images/<key>; it becomes a LINE image message ahead of any text.
// Reminder, when set, renders as a flex bubble ahead of everything else.
// ReminderID, when non-zero, makes this consumer publish a DeliveryEvent ack
// after the send attempt.
type ReplyEvent struct {
	UserID       string           `json:"user_id"`
	ReplyToken   string           `json:"reply_token"`
	Text         string           `json:"text"`
	ImageKey     string           `json:"image_key,omitempty"`
	QuickReplies []QuickReply     `json:"quick_replies,omitempty"`
	ReminderID   int64            `json:"reminder_id,omitempty"`
	Reminder     *ReminderPayload `json:"reminder,omitempty"`
}

// DeliveryEvent reports the outcome of delivering a reminder reply.
// ErrorCode is the HTTP status LINE returned (429 = push quota exhausted).
type DeliveryEvent struct {
	ReminderID int64  `json:"reminder_id"`
	OK         bool   `json:"ok"`
	ErrorCode  int    `json:"error_code,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Consumer subscribes to reply events and delivers them to LINE users.
// imageBaseURL is the public base (e.g. https://line-webhook.example.com)
// LINE fetches generated images from; empty disables image replies.
type Consumer struct {
	bot          *linebot.Client
	imageBaseURL string
	nc           *nats.Conn // set by Subscribe; used for delivery acks
}

func New(bot *linebot.Client, imageBaseURL string) *Consumer {
	return &Consumer{bot: bot, imageBaseURL: strings.TrimRight(imageBaseURL, "/")}
}

// Subscribe attaches the consumer to NATS as a queue subscriber so future
// replicas share the work. The connection is kept for delivery acks.
func (c *Consumer) Subscribe(nc *nats.Conn) (*nats.Subscription, error) {
	c.nc = nc
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
	if event.Text == "" && event.ImageKey == "" && event.Reminder == nil {
		log.Error().Str("user_id", event.UserID).Msg("consume: dropping empty reply")
		c.ackDelivery(event, false, errors.New("empty reply"))
		return
	}
	log.Info().Str("user_id", event.UserID).Int("text_chars", len(event.Text)).Bool("image", event.ImageKey != "").Bool("reminder", event.Reminder != nil).Msg("consume: reply event received")

	messages := c.buildMessages(event)
	if len(messages) == 0 {
		log.Error().Str("user_id", event.UserID).Msg("consume: nothing deliverable in reply")
		c.ackDelivery(event, false, errors.New("nothing deliverable"))
		return
	}

	delivered, err := c.deliver(event, messages)
	c.ackDelivery(event, delivered, err)
}

// deliver sends the messages, reply token first (free), push as fallback and
// overflow. Returns whether at least one batch went out, and the last error.
func (c *Consumer) deliver(event ReplyEvent, messages []linebot.SendingMessage) (bool, error) {
	delivered := false

	if event.ReplyToken != "" {
		replyBatch := messages
		if len(replyBatch) > maxMessagesPerCall {
			replyBatch = replyBatch[:maxMessagesPerCall]
		}
		if _, err := c.bot.ReplyMessage(event.ReplyToken, replyBatch...).Do(); err == nil {
			log.Info().Str("user_id", event.UserID).Int("parts", len(replyBatch)).Msg("deliver: sent via reply token")
			delivered = true
			messages = messages[len(replyBatch):]
			if len(messages) == 0 {
				return true, nil
			}
		} else {
			log.Error().Str("user_id", event.UserID).Err(err).Msg("deliver: reply token failed - falling back to push")
		}
	}

	if event.UserID == "" {
		log.Error().Msg("deliver: cannot push - no user_id")
		return delivered, errors.New("cannot push: no user_id")
	}
	for i := 0; i < len(messages); i += maxMessagesPerCall {
		end := i + maxMessagesPerCall
		if end > len(messages) {
			end = len(messages)
		}
		batch := messages[i:end]
		if _, err := c.bot.PushMessage(event.UserID, batch...).Do(); err != nil {
			log.Error().Str("user_id", event.UserID).Err(err).Msg("deliver: push message failed")
			return delivered, err
		}
		delivered = true
		log.Info().Str("user_id", event.UserID).Int("parts", len(batch)).Msg("deliver: sent via push message")
	}
	return delivered, nil
}

// ackDelivery publishes a DeliveryEvent for reminder replies so
// subscriber-reminder-notifier can mark the reminder sent or failed. A 429
// error code tells it the push quota is exhausted (retry later).
func (c *Consumer) ackDelivery(event ReplyEvent, ok bool, err error) {
	if event.ReminderID == 0 {
		return
	}
	ack := DeliveryEvent{ReminderID: event.ReminderID, OK: ok}
	if err != nil {
		ack.Error = err.Error()
		var apiErr *linebot.APIError
		if errors.As(err, &apiErr) {
			ack.ErrorCode = apiErr.Code
		}
	}
	if c.nc == nil {
		log.Error().Int64("reminder_id", event.ReminderID).Msg("ack: no NATS connection - delivery ack dropped")
		return
	}
	data, marshalErr := json.Marshal(ack)
	if marshalErr != nil {
		log.Error().Int64("reminder_id", event.ReminderID).Err(marshalErr).Msg("ack: marshal failed")
		return
	}
	if pubErr := c.nc.Publish(DeliverySubject, data); pubErr != nil {
		log.Error().Int64("reminder_id", event.ReminderID).Err(pubErr).Msg("ack: publish failed")
		return
	}
	log.Info().Int64("reminder_id", event.ReminderID).Bool("ok", ok).Int("error_code", ack.ErrorCode).Msg("ack: delivery reported")
}

// buildMessages turns a reply event into LINE messages: a reminder's flex
// bubble first, then the generated image (pointing at line-webhook's public
// /images endpoint), then the text split into chat-sized parts. Quick-reply
// buttons attach to the last message (LINE only shows them there).
func (c *Consumer) buildMessages(event ReplyEvent) []linebot.SendingMessage {
	var messages []linebot.SendingMessage
	if event.Reminder != nil {
		if msg, err := reminderFlexMessage(event.Reminder); err == nil {
			messages = append(messages, msg)
		} else {
			// json.Marshal of the fixed flex struct is not expected to fail;
			// this is a last-resort so a reminder is never silently dropped.
			log.Error().Str("user_id", event.UserID).Err(err).Msg("build: flex build failed - falling back to plain text")
			messages = append(messages, linebot.NewTextMessage("⏰ "+event.Reminder.Message))
		}
	}
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

	if len(messages) > 0 && len(event.QuickReplies) > 0 {
		buttons := make([]*linebot.QuickReplyButton, 0, len(event.QuickReplies))
		for _, q := range event.QuickReplies {
			buttons = append(buttons, linebot.NewQuickReplyButton("",
				linebot.NewPostbackAction(q.Label, q.Data, "", q.DisplayText, "", "")))
		}
		last := len(messages) - 1
		messages[last] = messages[last].WithQuickReplies(linebot.NewQuickReplyItems(buttons...))
	}
	return messages
}

// maxAltTextRunes is LINE's limit on a flex message's alt text (the preview
// shown in chat lists and notifications).
const maxAltTextRunes = 400

// reminderFlexMessage renders a ReminderPayload as the flex bubble
// notification, with an alt text preview built from the reminder message.
func reminderFlexMessage(r *ReminderPayload) (*linebot.FlexMessage, error) {
	raw, err := flex.Build(r.Message, r.CreatorDisplayName, r.RemindAt)
	if err != nil {
		return nil, err
	}
	container, err := linebot.UnmarshalFlexMessageJSON(raw)
	if err != nil {
		return nil, err
	}
	altText := "⏰ " + r.Message
	if runes := []rune(altText); len(runes) > maxAltTextRunes {
		altText = string(runes[:maxAltTextRunes-1]) + "…"
	}
	return linebot.NewFlexMessage(altText, container), nil
}

// maxMessageChars is LINE's per-text-message length limit. Paragraphs are
// packed together up to this size so a long answer still fits within
// maxMessagesPerCall messages and never needs the (quota-limited) push
// fallback in Handle.
const maxMessageChars = 5000

func splitReplyMessages(text string) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}

	var result []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			result = append(result, current.String())
			current.Reset()
		}
	}

	for _, part := range strings.Split(trimmed, "\n\n") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		for len(part) > maxMessageChars {
			flush()
			result = append(result, part[:maxMessageChars])
			part = part[maxMessageChars:]
		}
		if current.Len() > 0 && current.Len()+len("\n\n")+len(part) > maxMessageChars {
			flush()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(part)
	}
	flush()

	if len(result) == 0 {
		return []string{trimmed}
	}
	return result
}
