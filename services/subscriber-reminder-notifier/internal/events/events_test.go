package events

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestSubjectConstants pins the wire-compatible subject names this service
// shares with consumer-reply-line-user; a silent rename here would break the
// pairing without either service failing to compile.
func TestSubjectConstants(t *testing.T) {
	if ReplySubject != "line.chat.reply" {
		t.Errorf("ReplySubject = %q, want %q", ReplySubject, "line.chat.reply")
	}
	if DeliverySubject != "line.chat.delivery" {
		t.Errorf("DeliverySubject = %q, want %q", DeliverySubject, "line.chat.delivery")
	}
}

func TestReplyEventMarshalOmitsEmptyFields(t *testing.T) {
	cases := []struct {
		name       string
		ev         ReplyEvent
		wantHas    []string
		wantAbsent []string
	}{
		{
			name: "zero reminder id and nil payload are omitted",
			ev:   ReplyEvent{UserID: "u1", ReplyToken: "tok", Text: "hi"},
			wantHas: []string{
				`"user_id":"u1"`, `"reply_token":"tok"`, `"text":"hi"`,
			},
			wantAbsent: []string{`"reminder_id"`, `"reminder"`},
		},
		{
			name: "reminder fields present when populated",
			ev: ReplyEvent{
				UserID:     "u2",
				ReminderID: 42,
				Reminder: &ReminderPayload{
					Message:  "กินยา",
					RemindAt: time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC),
				},
			},
			wantHas: []string{
				`"user_id":"u2"`, `"reminder_id":42`, `"reminder":{`,
				`"message":"กินยา"`,
			},
			wantAbsent: []string{`"creator_display_name"`},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data, err := json.Marshal(c.ev)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			got := string(data)
			for _, want := range c.wantHas {
				if !strings.Contains(got, want) {
					t.Errorf("Marshal() = %s, want substring %q", got, want)
				}
			}
			for _, absent := range c.wantAbsent {
				if strings.Contains(got, absent) {
					t.Errorf("Marshal() = %s, want %q absent", got, absent)
				}
			}
		})
	}
}

func TestReplyEventRoundTrip(t *testing.T) {
	remindAt := time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC)
	original := ReplyEvent{
		UserID:     "u3",
		ReplyToken: "tok3",
		Text:       "reminder fired",
		ReminderID: 7,
		Reminder: &ReminderPayload{
			Message:            "ดื่มน้ำ",
			CreatorDisplayName: "Meow",
			RemindAt:           remindAt,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got ReplyEvent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if got.UserID != original.UserID || got.ReplyToken != original.ReplyToken ||
		got.Text != original.Text || got.ReminderID != original.ReminderID {
		t.Errorf("round-tripped scalar fields = %+v, want %+v", got, original)
	}
	if got.Reminder == nil {
		t.Fatal("round-tripped Reminder is nil")
	}
	if got.Reminder.Message != original.Reminder.Message ||
		got.Reminder.CreatorDisplayName != original.Reminder.CreatorDisplayName ||
		!got.Reminder.RemindAt.Equal(original.Reminder.RemindAt) {
		t.Errorf("round-tripped Reminder = %+v, want %+v", got.Reminder, original.Reminder)
	}
}

func TestDeliveryEventMarshalOmitsEmptyFields(t *testing.T) {
	ev := DeliveryEvent{ReminderID: 5, OK: true}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	got := string(data)
	if strings.Contains(got, `"error_code"`) || strings.Contains(got, `"error"`) {
		t.Errorf("Marshal() = %s, want error_code/error absent when zero", got)
	}
	if !strings.Contains(got, `"reminder_id":5`) || !strings.Contains(got, `"ok":true`) {
		t.Errorf("Marshal() = %s, missing required fields", got)
	}
}

func TestDeliveryEventUnmarshalFailure(t *testing.T) {
	data := []byte(`{"reminder_id":9,"ok":false,"error_code":429,"error":"quota exceeded"}`)
	var ev DeliveryEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	want := DeliveryEvent{ReminderID: 9, OK: false, ErrorCode: 429, Error: "quota exceeded"}
	if ev != want {
		t.Errorf("Unmarshal() = %+v, want %+v", ev, want)
	}
}

func TestDeliveryEventUnmarshalInvalidJSON(t *testing.T) {
	var ev DeliveryEvent
	err := json.Unmarshal([]byte(`{not json`), &ev)
	if err == nil {
		t.Fatal("Unmarshal() error = nil, want error for malformed JSON")
	}
}
