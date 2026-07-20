package handler

import (
	"testing"
)

func TestNewWiresDependencies(t *testing.T) {
	cfg := &Config{ChannelSecret: "secret", ChannelToken: "token", AIPrefix: "/ai"}
	pub := &fakePublisher{}
	sessions := &fakeSessions{active: map[string]bool{}, flows: map[string]bool{}}

	h := New(cfg, pub, sessions, nil, nil, nil)
	if h == nil {
		t.Fatal("New() returned nil")
	}

	lh, ok := h.(*LineHandler)
	if !ok {
		t.Fatalf("New() returned %T, want *LineHandler", h)
	}
	if lh.cfg != cfg {
		t.Error("New() did not wire cfg")
	}
	if lh.pub != pub {
		t.Error("New() did not wire pub")
	}
	if lh.sessions != sessions {
		t.Error("New() did not wire sessions")
	}
	if lh.images != nil {
		t.Error("New() should leave images nil when passed nil")
	}
	if lh.profiles != nil {
		t.Error("New() should leave profiles nil when passed nil")
	}
	if lh.bot != nil {
		t.Error("New() should leave bot nil when passed nil")
	}
}

func TestExtractMarkAsReadTokenErrors(t *testing.T) {
	if _, err := extractMarkAsReadToken([]byte("not json"), 0); err == nil {
		t.Fatal("extractMarkAsReadToken() with invalid JSON error = nil, want non-nil")
	}

	// Missing token in the event still succeeds with an empty string.
	payload := []byte(`{"events":[{"type":"follow"}]}`)
	got, err := extractMarkAsReadToken(payload, 0)
	if err != nil {
		t.Fatalf("extractMarkAsReadToken() error = %v", err)
	}
	if got != "" {
		t.Fatalf("extractMarkAsReadToken() = %q, want empty", got)
	}
}
