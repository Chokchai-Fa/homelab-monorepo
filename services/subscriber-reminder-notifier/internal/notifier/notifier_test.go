package notifier

import (
	"context"
	"errors"
	"testing"
	"time"

	"subscriber-reminder-notifier/internal/events"
	"subscriber-reminder-notifier/internal/store"
)

type fakeStore struct {
	reminder    store.Reminder
	claimed     bool
	claimErr    error
	reverted    []int64
	sent        []int64
	failed      map[int64]string
	displayName string
}

func (f *fakeStore) Claim(_ context.Context, id int64) (store.Reminder, bool, error) {
	return f.reminder, f.claimed, f.claimErr
}
func (f *fakeStore) Revert(_ context.Context, id int64) error {
	f.reverted = append(f.reverted, id)
	return nil
}
func (f *fakeStore) MarkSent(_ context.Context, id int64) error {
	f.sent = append(f.sent, id)
	return nil
}
func (f *fakeStore) MarkFailed(_ context.Context, id int64, reason string) error {
	if f.failed == nil {
		f.failed = map[int64]string{}
	}
	f.failed[id] = reason
	return nil
}
func (f *fakeStore) GetDisplayName(_ context.Context, userID string) (string, error) {
	return f.displayName, nil
}

func TestParseFireKey(t *testing.T) {
	cases := []struct {
		key    string
		wantID int64
		wantOK bool
	}{
		{"reminder:fire:42", 42, true},
		{"chat:ai_session:u1", 0, false},
		{"reminder:fire:not-a-number", 0, false},
	}
	for _, c := range cases {
		id, ok := parseFireKey(c.key)
		if id != c.wantID || ok != c.wantOK {
			t.Errorf("parseFireKey(%q) = (%d, %v), want (%d, %v)", c.key, id, ok, c.wantID, c.wantOK)
		}
	}
}

func TestHandleExpiredPublishesAndLeavesRowSending(t *testing.T) {
	fs := &fakeStore{
		claimed: true,
		reminder: store.Reminder{
			ID: 42, CreatorID: "creator", TargetUserID: "target",
			Message: "กินยา", RemindAt: time.Now().Add(time.Hour),
		},
		displayName: "Meow",
	}
	var published []events.ReplyEvent
	n := &Notifier{store: fs, publish: func(ev events.ReplyEvent) error {
		published = append(published, ev)
		return nil
	}}

	n.handleExpired("reminder:fire:42")

	if len(published) != 1 {
		t.Fatalf("published = %+v, want 1 event", published)
	}
	ev := published[0]
	if ev.UserID != "target" || ev.ReminderID != 42 || ev.Reminder == nil {
		t.Fatalf("published event = %+v", ev)
	}
	if ev.Reminder.Message != "กินยา" || ev.Reminder.CreatorDisplayName != "Meow" {
		t.Errorf("reminder payload = %+v", ev.Reminder)
	}
	if len(fs.reverted) != 0 {
		t.Errorf("row should stay 'sending' awaiting the ack, but was reverted: %+v", fs.reverted)
	}
}

func TestHandleExpiredIgnoresForeignKey(t *testing.T) {
	fs := &fakeStore{claimed: true}
	called := false
	n := &Notifier{store: fs, publish: func(ev events.ReplyEvent) error {
		called = true
		return nil
	}}
	n.handleExpired("chat:ai_session:u1")
	if called {
		t.Fatal("foreign key must not trigger a publish")
	}
}

func TestHandleExpiredSkipsAlreadyClaimed(t *testing.T) {
	fs := &fakeStore{claimed: false}
	called := false
	n := &Notifier{store: fs, publish: func(ev events.ReplyEvent) error {
		called = true
		return nil
	}}
	n.handleExpired("reminder:fire:1")
	if called {
		t.Fatal("an already-claimed reminder must not be republished")
	}
}

func TestHandleExpiredRevertsOnPublishFailure(t *testing.T) {
	fs := &fakeStore{claimed: true, reminder: store.Reminder{ID: 9, RemindAt: time.Now()}}
	n := &Notifier{store: fs, publish: func(ev events.ReplyEvent) error {
		return errors.New("nats down")
	}}
	n.handleExpired("reminder:fire:9")
	if len(fs.reverted) != 1 || fs.reverted[0] != 9 {
		t.Fatalf("expected revert(9), got %+v", fs.reverted)
	}
}

func TestHandleDeliveryOK(t *testing.T) {
	fs := &fakeStore{}
	n := &Notifier{store: fs}
	n.handleDelivery(context.Background(), events.DeliveryEvent{ReminderID: 5, OK: true})
	if len(fs.sent) != 1 || fs.sent[0] != 5 {
		t.Fatalf("expected sent(5), got %+v", fs.sent)
	}
}

func TestHandleDeliveryQuotaFailure(t *testing.T) {
	fs := &fakeStore{}
	n := &Notifier{store: fs}
	n.handleDelivery(context.Background(), events.DeliveryEvent{ReminderID: 5, OK: false, ErrorCode: 429})
	if fs.failed[5] != "quota_429" {
		t.Fatalf("failed reasons = %+v, want quota_429 for id 5", fs.failed)
	}
}

func TestHandleDeliveryOtherFailure(t *testing.T) {
	fs := &fakeStore{}
	n := &Notifier{store: fs}
	n.handleDelivery(context.Background(), events.DeliveryEvent{ReminderID: 6, OK: false, ErrorCode: 500})
	if fs.failed[6] != "line_500" {
		t.Fatalf("failed reasons = %+v, want line_500 for id 6", fs.failed)
	}
}

func TestHandleDeliveryIgnoresNonReminderAck(t *testing.T) {
	fs := &fakeStore{}
	n := &Notifier{store: fs}
	n.handleDelivery(context.Background(), events.DeliveryEvent{ReminderID: 0, OK: true})
	if len(fs.sent) != 0 {
		t.Fatalf("zero reminder id must be ignored, got %+v", fs.sent)
	}
}
