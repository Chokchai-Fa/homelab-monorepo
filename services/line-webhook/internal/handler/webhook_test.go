package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/line/line-bot-sdk-go/v7/linebot"

	"line-webhook/internal/publisher"
)

func TestExtractMarkAsReadToken(t *testing.T) {
	payload := []byte(`{"events":[{"type":"message","message":{"markAsReadToken":"tok-1"}},{"type":"follow"}]}`)
	got, err := extractMarkAsReadToken(payload, 0)
	if err != nil {
		t.Fatalf("extractMarkAsReadToken() error = %v", err)
	}
	if got != "tok-1" {
		t.Fatalf("extractMarkAsReadToken() = %q, want %q", got, "tok-1")
	}

	if _, err := extractMarkAsReadToken(payload, 99); err != nil {
		t.Fatalf("extractMarkAsReadToken(out of range) error = %v", err)
	}
}

func TestIsAIRequest(t *testing.T) {
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}}

	cases := []struct {
		text string
		want bool
	}{
		{"/ai what is kubernetes?", true},
		{"/ai ถามอะไรก็ได้", true},
		{"/ai", true},
		{"  /ai reset  ", true},
		{"hello", false},
		{"/aid something", false},
		{"ai what is this", false},
		{"", false},
	}
	for _, c := range cases {
		if got := h.isAIRequest(c.text); got != c.want {
			t.Errorf("isAIRequest(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

// fakePublisher is not safe for concurrent Publish* calls in general, but
// ensureProfile does publish from a background goroutine (see
// TestEnsureProfile* in webhook_extra_test.go), so every method guards the
// slices with a mutex; tests that read the slices while such a goroutine
// may still be running should go through the *Snapshot helpers below.
type fakePublisher struct {
	mu         sync.Mutex
	aiRequests []publisher.AIRequestEvent
	replies    []publisher.ReplyEvent
	postbacks  []publisher.PostbackEvent
	profiles   []publisher.ProfileEvent
}

func (f *fakePublisher) PublishAIRequest(e publisher.AIRequestEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.aiRequests = append(f.aiRequests, e)
	return nil
}

func (f *fakePublisher) PublishReply(e publisher.ReplyEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = append(f.replies, e)
	return nil
}

func (f *fakePublisher) PublishPostback(e publisher.PostbackEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.postbacks = append(f.postbacks, e)
	return nil
}

func (f *fakePublisher) PublishProfile(e publisher.ProfileEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.profiles = append(f.profiles, e)
	return nil
}

// profilesSnapshot returns a copy of the published profile events, safe to
// call while a background goroutine (e.g. ensureProfile) may still be
// publishing.
func (f *fakePublisher) profilesSnapshot() []publisher.ProfileEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]publisher.ProfileEvent, len(f.profiles))
	copy(out, f.profiles)
	return out
}

type fakeSessions struct {
	active map[string]bool
	flows  map[string]bool
}

func (f *fakeSessions) Start(_ context.Context, userID string) error {
	f.active[userID] = true
	return nil
}
func (f *fakeSessions) Active(_ context.Context, userID string) bool { return f.active[userID] }
func (f *fakeSessions) End(_ context.Context, userID string) error {
	delete(f.active, userID)
	return nil
}
func (f *fakeSessions) FlowActive(_ context.Context, userID string) bool { return f.flows[userID] }

func textEvent(text string) (*linebot.Event, *linebot.TextMessage) {
	return &linebot.Event{
		ReplyToken: "token",
		Timestamp:  time.Now(),
		Source:     &linebot.EventSource{UserID: "u1"},
	}, &linebot.TextMessage{Text: text}
}

func TestAISessionFlow(t *testing.T) {
	pub := &fakePublisher{}
	sessions := &fakeSessions{active: map[string]bool{}, flows: map[string]bool{}}
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}, pub: pub, sessions: sessions}

	// 1. "/ai <question>" starts the session and forwards the question.
	if err := h.handleTextMessage(textEvent("/ai hello ai")); err != nil {
		t.Fatal(err)
	}
	if !sessions.active["u1"] {
		t.Fatal("session not started by /ai")
	}
	if len(pub.aiRequests) != 1 || pub.aiRequests[0].Text != "hello ai" {
		t.Fatalf("unexpected AI requests: %+v", pub.aiRequests)
	}

	// 2. During the session, plain messages route to the AI without prefix.
	if err := h.handleTextMessage(textEvent("สวัสดี")); err != nil {
		t.Fatal(err)
	}
	if len(pub.aiRequests) != 2 || pub.aiRequests[1].Text != "สวัสดี" {
		t.Fatalf("session message not routed to AI: %+v", pub.aiRequests)
	}

	// 3. "/ai-end" ends the session and confirms via reply.
	if err := h.handleTextMessage(textEvent("/ai-end")); err != nil {
		t.Fatal(err)
	}
	if sessions.active["u1"] {
		t.Fatal("session still active after /ai-end")
	}
	if len(pub.replies) != 1 {
		t.Fatalf("expected end confirmation reply, got %+v", pub.replies)
	}

	// 4. After the session, plain messages fall back to echo behavior.
	if err := h.handleTextMessage(textEvent("plain message")); err != nil {
		t.Fatal(err)
	}
	if len(pub.aiRequests) != 2 {
		t.Fatalf("message after session end should not go to AI: %+v", pub.aiRequests)
	}
	if len(pub.replies) != 2 || pub.replies[1].Text != "You said: plain message" {
		t.Fatalf("expected echo reply, got %+v", pub.replies)
	}

	// 5. "/ai" alone starts a session and replies with instructions.
	if err := h.handleTextMessage(textEvent("/ai")); err != nil {
		t.Fatal(err)
	}
	if !sessions.active["u1"] {
		t.Fatal("session not restarted by /ai")
	}
	if len(pub.replies) != 3 {
		t.Fatalf("expected session-start reply, got %+v", pub.replies)
	}
}

func TestIsReminderRequest(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"/reminder", true},
		{"/reminder เตือนพรุ่งนี้ 9 โมง กินยา", true},
		{"ตั้งเตือน", true},
		{"ตั้งเตือนกินยาพรุ่งนี้", true},
		{"/reminders", true}, // manage flow: list / edit / delete
		{"ดูเตือน", true},
		{"รายการเตือน", true},
		{"เตือนฉันหน่อย", false}, // natural phrasing goes via the LLM classifier
		{"hello", false},
	}
	for _, c := range cases {
		if got := isReminderRequest(c.text); got != c.want {
			t.Errorf("isReminderRequest(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestReminderRouting(t *testing.T) {
	pub := &fakePublisher{}
	sessions := &fakeSessions{active: map[string]bool{}, flows: map[string]bool{}}
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}, pub: pub, sessions: sessions}

	// Keyword triggers reach the AI pipeline even without a session (the
	// intent handoff to consumer-reminder happens in consumer-llm-processor).
	if err := h.handleTextMessage(textEvent("/reminder เตือนพรุ่งนี้")); err != nil {
		t.Fatal(err)
	}
	if len(pub.aiRequests) != 1 || pub.aiRequests[0].Text != "/reminder เตือนพรุ่งนี้" {
		t.Fatalf("keyword not forwarded to AI pipeline: %+v", pub.aiRequests)
	}
	if sessions.active["u1"] {
		t.Fatal("reminder keyword must not start the AI session")
	}

	// Outside a session, non-keyword text still falls back to echo.
	if err := h.handleTextMessage(textEvent("plain text")); err != nil {
		t.Fatal(err)
	}
	if len(pub.aiRequests) != 1 {
		t.Fatalf("non-keyword text leaked to the AI: %+v", pub.aiRequests)
	}

	// While consumer-reminder's flow is open, free text routes to the AI
	// pipeline without starting an AI session...
	sessions.flows["u1"] = true
	if err := h.handleTextMessage(textEvent("พรุ่งนี้ 9 โมง กินยา")); err != nil {
		t.Fatal(err)
	}
	if len(pub.aiRequests) != 2 || pub.aiRequests[1].Text != "พรุ่งนี้ 9 โมง กินยา" {
		t.Fatalf("mid-flow text not forwarded: %+v", pub.aiRequests)
	}
	if sessions.active["u1"] {
		t.Fatal("mid-flow routing must not start the AI session")
	}

	// ...and once the flow ends, routing stops with it.
	delete(sessions.flows, "u1")
	if err := h.handleTextMessage(textEvent("plain again")); err != nil {
		t.Fatal(err)
	}
	if len(pub.aiRequests) != 2 {
		t.Fatalf("text after flow end leaked to the AI: %+v", pub.aiRequests)
	}
}

func TestPostbackForwarding(t *testing.T) {
	pub := &fakePublisher{}
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}, pub: pub}

	event := &linebot.Event{
		ReplyToken: "token",
		Timestamp:  time.Now(),
		Source:     &linebot.EventSource{UserID: "u1"},
		Postback:   &linebot.Postback{Data: "flow=rem&a=target&v=self"},
	}
	if err := h.handlePostbackEvent(event); err != nil {
		t.Fatal(err)
	}
	if len(pub.postbacks) != 1 || pub.postbacks[0].Data != "flow=rem&a=target&v=self" {
		t.Fatalf("postback not forwarded raw: %+v", pub.postbacks)
	}
	if len(pub.replies) != 0 {
		t.Fatalf("postback must not reply directly: %+v", pub.replies)
	}
}
