// These tests lock in the JSON wire format documented in events.go: it must
// stay compatible with line-webhook's publisher and
// consumer-reply-line-user's consumer, which each keep their own copy of
// these structs.
package events

import (
	"encoding/json"
	"testing"
)

func TestReminderRequestEventRoundTrip(t *testing.T) {
	raw := `{"user_id":"u1","reply_token":"tok","text":"/reminder กินยา","message":"กินยา","remind_at":"2026-07-20T09:00:00+07:00","timestamp":1234}`

	var ev ReminderRequestEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := ReminderRequestEvent{
		UserID: "u1", ReplyToken: "tok", Text: "/reminder กินยา",
		Message: "กินยา", RemindAt: "2026-07-20T09:00:00+07:00", Timestamp: 1234,
	}
	if ev != want {
		t.Fatalf("unmarshal = %+v, want %+v", ev, want)
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundTripped ReminderRequestEvent
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if roundTripped != ev {
		t.Fatalf("round trip = %+v, want %+v", roundTripped, ev)
	}
}

func TestReminderRequestEventOmitsEmptyOptionalFields(t *testing.T) {
	ev := ReminderRequestEvent{UserID: "u1", ReplyToken: "tok", Text: "hi"}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	for _, field := range []string{"message", "remind_at"} {
		if _, ok := m[field]; ok {
			t.Errorf("expected %q omitted when empty, got %+v", field, m)
		}
	}
	for _, field := range []string{"user_id", "reply_token", "text", "timestamp"} {
		if _, ok := m[field]; !ok {
			t.Errorf("expected %q present, got %+v", field, m)
		}
	}
}

func TestPostbackEventFields(t *testing.T) {
	raw := `{"user_id":"u1","reply_token":"tok","data":"flow=rem&a=confirm","timestamp":42}`
	var ev PostbackEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := PostbackEvent{UserID: "u1", ReplyToken: "tok", Data: "flow=rem&a=confirm", Timestamp: 42}
	if ev != want {
		t.Fatalf("unmarshal = %+v, want %+v", ev, want)
	}
}

func TestProfileEventFields(t *testing.T) {
	raw := `{"user_id":"u1","display_name":"Meow","timestamp":7}`
	var ev ProfileEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := ProfileEvent{UserID: "u1", DisplayName: "Meow", Timestamp: 7}
	if ev != want {
		t.Fatalf("unmarshal = %+v, want %+v", ev, want)
	}
}

func TestReplyEventMarshalsQuickReplies(t *testing.T) {
	ev := ReplyEvent{
		UserID: "u1", ReplyToken: "tok", Text: "hello",
		QuickReplies: []QuickReply{
			{Label: "Yes", Data: "flow=rem&a=confirm", DisplayText: "Yes"},
		},
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundTripped ReplyEvent
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(roundTripped.QuickReplies) != 1 || roundTripped.QuickReplies[0] != ev.QuickReplies[0] {
		t.Fatalf("round trip = %+v, want %+v", roundTripped, ev)
	}
}

func TestReplyEventOmitsEmptyQuickReplies(t *testing.T) {
	ev := ReplyEvent{UserID: "u1", ReplyToken: "tok", Text: "hello"}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	if _, ok := m["quick_replies"]; ok {
		t.Errorf("expected quick_replies omitted when empty, got %+v", m)
	}
}

func TestSubjectsAreDistinct(t *testing.T) {
	subjects := []string{ReminderRequestSubject, PostbackSubject, ProfileSubject, ReplySubject}
	seen := map[string]bool{}
	for _, s := range subjects {
		if s == "" {
			t.Fatalf("subject constant is empty: %+v", subjects)
		}
		if seen[s] {
			t.Fatalf("duplicate subject %q among %+v", s, subjects)
		}
		seen[s] = true
	}
}
