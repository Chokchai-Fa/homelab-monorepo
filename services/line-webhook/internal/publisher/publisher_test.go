package publisher

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSubjects(t *testing.T) {
	// These must stay in sync with the consumer services documented in the
	// package comment; a change here breaks the pipeline silently.
	cases := map[string]string{
		AIRequestSubject: "line.chat.ai_request",
		ReplySubject:     "line.chat.reply",
		PostbackSubject:  "line.chat.postback",
		ProfileSubject:   "line.chat.profile",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("subject = %q, want %q", got, want)
		}
	}
}

func TestAIRequestEventJSON(t *testing.T) {
	e := AIRequestEvent{
		UserID:     "u1",
		ReplyToken: "tok",
		Text:       "hello",
		Timestamp:  1234,
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	s := string(data)
	// ImageKey/ImageMime are omitempty and must be absent when unset, so
	// consumer-llm-processor's "has image?" check (presence of image_key)
	// stays reliable.
	if strings.Contains(s, "image_key") || strings.Contains(s, "image_mime") {
		t.Errorf("expected omitempty image fields to be absent, got %s", s)
	}

	var got AIRequestEvent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got != e {
		t.Errorf("round-trip = %+v, want %+v", got, e)
	}
}

func TestAIRequestEventJSONWithImage(t *testing.T) {
	e := AIRequestEvent{
		UserID:    "u1",
		ImageKey:  "chat:image:msg-1",
		ImageMime: "image/jpeg",
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `"image_key":"chat:image:msg-1"`) {
		t.Errorf("expected image_key in output, got %s", s)
	}
	if !strings.Contains(s, `"image_mime":"image/jpeg"`) {
		t.Errorf("expected image_mime in output, got %s", s)
	}
}

func TestReplyEventJSON(t *testing.T) {
	e := ReplyEvent{UserID: "u1", ReplyToken: "tok", Text: "hi"}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var got ReplyEvent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got != e {
		t.Errorf("round-trip = %+v, want %+v", got, e)
	}
}

func TestPostbackEventJSON(t *testing.T) {
	e := PostbackEvent{UserID: "u1", ReplyToken: "tok", Data: "flow=rem&a=target", Timestamp: 99}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var got PostbackEvent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got != e {
		t.Errorf("round-trip = %+v, want %+v", got, e)
	}
}

func TestProfileEventJSON(t *testing.T) {
	e := ProfileEvent{UserID: "u1", DisplayName: "Alice", Timestamp: 42}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var got ProfileEvent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got != e {
		t.Errorf("round-trip = %+v, want %+v", got, e)
	}
}

func TestNewUnreachableServerRetries(t *testing.T) {
	// No server is listening on this loopback port. RetryOnFailedConnect(true)
	// means Connect must NOT error or hang: it returns a usable publisher that
	// reconnects in the background, so the webhook keeps accepting LINE events
	// while NATS is down.
	p, err := New("nats://127.0.0.1:1", "", "", nil)
	if err != nil {
		t.Fatalf("New() with unreachable server error = %v, want nil", err)
	}
	if p == nil {
		t.Fatal("New() publisher = nil, want usable publisher")
	}
	p.Close()
}

func TestNewClosedTriggersOnClosed(t *testing.T) {
	// A permanently closed connection must invoke onClosed so main can exit and
	// let Kubernetes restart the pod, rather than holding a dead connection.
	closed := make(chan struct{})
	p, err := New("nats://127.0.0.1:1", "", "", func() { close(closed) })
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	p.nc.Close()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("onClosed not called after connection close")
	}
}
