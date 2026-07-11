package handler

import (
	"context"
	"testing"
	"time"

	"github.com/line/line-bot-sdk-go/v7/linebot"

	"line-webhook/internal/publisher"
)

func TestExtractMarkAsReadToken(t *testing.T) {
	payload := []byte(`{"events":[{"type":"message","markAsReadToken":"tok-1"},{"type":"follow"}]}`)
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

type fakePublisher struct {
	aiRequests []publisher.AIRequestEvent
	replies    []publisher.ReplyEvent
}

func (f *fakePublisher) PublishAIRequest(e publisher.AIRequestEvent) error {
	f.aiRequests = append(f.aiRequests, e)
	return nil
}

func (f *fakePublisher) PublishReply(e publisher.ReplyEvent) error {
	f.replies = append(f.replies, e)
	return nil
}

type fakeSessions struct{ active map[string]bool }

func (f *fakeSessions) Start(_ context.Context, userID string) error {
	f.active[userID] = true
	return nil
}
func (f *fakeSessions) Active(_ context.Context, userID string) bool { return f.active[userID] }
func (f *fakeSessions) End(_ context.Context, userID string) error {
	delete(f.active, userID)
	return nil
}

func textEvent(text string) (*linebot.Event, *linebot.TextMessage) {
	return &linebot.Event{
		ReplyToken: "token",
		Timestamp:  time.Now(),
		Source:     &linebot.EventSource{UserID: "u1"},
	}, &linebot.TextMessage{Text: text}
}

func TestAISessionFlow(t *testing.T) {
	pub := &fakePublisher{}
	sessions := &fakeSessions{active: map[string]bool{}}
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
