package publisher

import (
	"encoding/json"

	"github.com/nats-io/nats.go"
)

// Subjects of the LINE chat pipeline. AIRequestSubject must match the
// subscriber in services/consumer-llm-processor; ReplySubject must match
// services/consumer-reply-line-user; PostbackSubject and ProfileSubject
// must match services/consumer-reminder.
const (
	AIRequestSubject = "line.chat.ai_request"
	ReplySubject     = "line.chat.reply"
	PostbackSubject  = "line.chat.postback"
	ProfileSubject   = "line.chat.profile"
)

// AIRequestEvent is consumed by consumer-llm-processor. Text has the AI
// prefix (e.g. "/ai") already stripped. ImageKey, when set, is the Redis key
// (see internal/imagecache) holding an image the user attached; Text may be
// empty in that case.
type AIRequestEvent struct {
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

// PostbackEvent carries a quick-reply button press to consumer-reminder.
// Data is the raw postback payload (query-string style, e.g.
// "flow=rem&a=target&v=self").
type PostbackEvent struct {
	UserID     string `json:"user_id"`
	ReplyToken string `json:"reply_token"`
	Data       string `json:"data"`
	Timestamp  int64  `json:"timestamp"`
}

// ProfileEvent hands a LINE profile fetched by the webhook to
// consumer-reminder, which owns the line_users table.
type ProfileEvent struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	Timestamp   int64  `json:"timestamp"`
}

// Publisher publishes LINE chat events to NATS.
type Publisher struct {
	nc *nats.Conn
}

// New connects to NATS. The webhook must keep accepting LINE events when the
// broker is down, so callers should treat a connection error as non-fatal.
func New(url, user, password string) (*Publisher, error) {
	nc, err := nats.Connect(url,
		nats.UserInfo(user, password),
		nats.Name("line-webhook"),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		return nil, err
	}
	return &Publisher{nc: nc}, nil
}

// PublishAIRequest sends one event to the AI request subject.
func (p *Publisher) PublishAIRequest(event AIRequestEvent) error {
	return p.publish(AIRequestSubject, event)
}

// PublishReply sends one event to the reply subject.
func (p *Publisher) PublishReply(event ReplyEvent) error {
	return p.publish(ReplySubject, event)
}

// PublishPostback sends one event to the postback subject.
func (p *Publisher) PublishPostback(event PostbackEvent) error {
	return p.publish(PostbackSubject, event)
}

// PublishProfile sends one event to the profile subject.
func (p *Publisher) PublishProfile(event ProfileEvent) error {
	return p.publish(ProfileSubject, event)
}

func (p *Publisher) publish(subject string, event any) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return p.nc.Publish(subject, data)
}

func (p *Publisher) Close() {
	p.nc.Drain()
}
