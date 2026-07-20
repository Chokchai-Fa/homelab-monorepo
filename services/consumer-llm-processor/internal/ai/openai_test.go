package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"consumer-llm-processor/internal/store"
)

func TestOpenAIName(t *testing.T) {
	o := NewOpenAI("groq", "https://api.groq.com/openai/v1", "key", "llama-3", "system", false)
	if got, want := o.Name(), "groq/llama-3"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

type capturedRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
}

func TestOpenAIReplySendsSystemAndHistory(t *testing.T) {
	var got capturedRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("Authorization header = %q", r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"hello back"}}]}`))
	}))
	defer ts.Close()

	o := NewOpenAI("groq", ts.URL, "secret", "llama-3", "be nice", false)
	history := []store.Message{
		{Role: store.RoleUser, Content: "hi"},
		{Role: store.RoleModel, Content: "hey there"},
	}
	answer, err := o.Reply(context.Background(), history, "how are you", nil)
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if answer != "hello back" {
		t.Errorf("answer = %q, want %q", answer, "hello back")
	}

	if got.Model != "llama-3" {
		t.Errorf("model = %q, want llama-3", got.Model)
	}
	if len(got.Messages) != 4 {
		t.Fatalf("messages = %d, want 4 (system, user, assistant, user)", len(got.Messages))
	}
	if got.Messages[0].Role != "system" {
		t.Errorf("messages[0].Role = %q, want system", got.Messages[0].Role)
	}
	var sysContent string
	json.Unmarshal(got.Messages[0].Content, &sysContent)
	if sysContent != "be nice" {
		t.Errorf("system content = %q, want %q", sysContent, "be nice")
	}
	if got.Messages[2].Role != "assistant" {
		t.Errorf("history model role mapped to %q, want assistant", got.Messages[2].Role)
	}
	var lastContent string
	json.Unmarshal(got.Messages[3].Content, &lastContent)
	if lastContent != "how are you" {
		t.Errorf("last message content = %q, want %q", lastContent, "how are you")
	}
}

func TestOpenAIReplyWithImageBuildsDataURL(t *testing.T) {
	var got capturedRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"choices":[{"message":{"content":"a cat"}}]}`))
	}))
	defer ts.Close()

	o := NewOpenAI("vision-model", ts.URL, "key", "vlm", "sys", true)
	img := &Image{Data: []byte("fake-bytes"), MimeType: "image/png"}
	answer, err := o.Reply(context.Background(), nil, "what is this?", img)
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if answer != "a cat" {
		t.Errorf("answer = %q, want %q", answer, "a cat")
	}

	// Last message should be the user message with image_url + text parts.
	last := got.Messages[len(got.Messages)-1]
	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(last.Content, &parts); err != nil {
		t.Fatalf("decode content parts: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("parts = %d, want 2 (image, text)", len(parts))
	}
	wantURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("fake-bytes"))
	if parts[0].ImageURL.URL != wantURL {
		t.Errorf("image url = %q, want %q", parts[0].ImageURL.URL, wantURL)
	}
	if parts[1].Text != "what is this?" {
		t.Errorf("text part = %q, want %q", parts[1].Text, "what is this?")
	}
}

func TestOpenAIReplyImageUnsupportedSkipsHTTPCall(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	}))
	defer ts.Close()

	o := NewOpenAI("text-only", ts.URL, "key", "m", "sys", false)
	img := &Image{Data: []byte("x"), MimeType: "image/png"}
	_, err := o.Reply(context.Background(), nil, "look", img)
	if err == nil {
		t.Fatal("expected error when provider lacks vision support")
	}
	if calls != 0 {
		t.Errorf("HTTP called %d times, want 0", calls)
	}
}

func TestOpenAIReplyNonOKStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer ts.Close()

	o := NewOpenAI("groq", ts.URL, "key", "m", "sys", false)
	_, err := o.Reply(context.Background(), nil, "hi", nil)
	if err == nil {
		t.Fatal("expected error on non-200 status")
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error = %v, want it to mention status and body", err)
	}
}

func TestOpenAIReplyEmptyChoices(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer ts.Close()

	o := NewOpenAI("groq", ts.URL, "key", "m", "sys", false)
	_, err := o.Reply(context.Background(), nil, "hi", nil)
	if err == nil {
		t.Fatal("expected error on empty choices")
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"shorter than limit", "hello", 10, "hello"},
		{"exactly at limit", "hello", 5, "hello"},
		{"longer than limit", "hello world", 5, "hello..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncate(tc.s, tc.n); got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.s, tc.n, got, tc.want)
			}
		})
	}
}
