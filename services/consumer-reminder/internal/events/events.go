// Package events holds the NATS subjects and payloads this service speaks.
// Each service keeps its own copy of the shared contracts (monorepo
// convention); these must stay wire-compatible with line-webhook's publisher
// and consumer-reply-line-user's consumer.
package events

const (
	// ReminderRequestSubject carries trigger keywords and mid-flow free text
	// from line-webhook, and LLM-detected reminder intents from
	// consumer-llm-processor.
	ReminderRequestSubject = "line.chat.reminder_request"
	// PostbackSubject carries quick-reply button presses from line-webhook.
	PostbackSubject = "line.chat.postback"
	// ProfileSubject carries LINE profiles fetched by line-webhook.
	ProfileSubject = "line.chat.profile"
	// ReplySubject is consumed by consumer-reply-line-user.
	ReplySubject = "line.chat.reply"
)

// ReminderRequestEvent arrives with the language work already done by
// consumer-llm-processor: Message and RemindAt (RFC3339, +07:00) are what
// its extractor LLM found in Text. Either may be empty - the flow then asks
// the user for the missing part.
type ReminderRequestEvent struct {
	UserID     string `json:"user_id"`
	ReplyToken string `json:"reply_token"`
	Text       string `json:"text"`
	Message    string `json:"message,omitempty"`
	RemindAt   string `json:"remind_at,omitempty"`
	Timestamp  int64  `json:"timestamp"`
}

// PostbackEvent's Data is query-string style, e.g. "flow=rem&a=target&v=self".
type PostbackEvent struct {
	UserID     string `json:"user_id"`
	ReplyToken string `json:"reply_token"`
	Data       string `json:"data"`
	Timestamp  int64  `json:"timestamp"`
}

type ProfileEvent struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	Timestamp   int64  `json:"timestamp"`
}

// QuickReply becomes a LINE quick-reply button with a postback action.
type QuickReply struct {
	Label       string `json:"label"`
	Data        string `json:"data"`
	DisplayText string `json:"display_text,omitempty"`
}

type ReplyEvent struct {
	UserID       string       `json:"user_id"`
	ReplyToken   string       `json:"reply_token"`
	Text         string       `json:"text"`
	QuickReplies []QuickReply `json:"quick_replies,omitempty"`
}
