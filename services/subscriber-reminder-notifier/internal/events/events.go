// Package events holds the NATS subjects and payloads this service speaks,
// wire-compatible with consumer-reply-line-user.
package events

import "time"

const (
	// ReplySubject is consumed by consumer-reply-line-user.
	ReplySubject = "line.chat.reply"
	// DeliverySubject carries delivery acks back from consumer-reply-line-user.
	DeliverySubject = "line.chat.delivery"
)

// ReminderPayload carries the raw facts a fired reminder needs; building the
// flex bubble is consumer-reply-line-user's job, not this service's - it only
// orchestrates claiming and firing.
type ReminderPayload struct {
	Message            string    `json:"message"`
	CreatorDisplayName string    `json:"creator_display_name,omitempty"`
	RemindAt           time.Time `json:"remind_at"`
}

type ReplyEvent struct {
	UserID     string           `json:"user_id"`
	ReplyToken string           `json:"reply_token"`
	Text       string           `json:"text"`
	ReminderID int64            `json:"reminder_id,omitempty"`
	Reminder   *ReminderPayload `json:"reminder,omitempty"`
}

// DeliveryEvent reports the outcome of delivering a reminder reply.
// ErrorCode is the HTTP status LINE returned (429 = push quota exhausted).
type DeliveryEvent struct {
	ReminderID int64  `json:"reminder_id"`
	OK         bool   `json:"ok"`
	ErrorCode  int    `json:"error_code,omitempty"`
	Error      string `json:"error,omitempty"`
}
