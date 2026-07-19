package flow

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"consumer-reminder/internal/events"
	"consumer-reminder/internal/store"
)

type fakeStore struct {
	users     []store.User
	names     map[string]string
	reminders []savedReminder
	nextID    int64
}

type savedReminder struct {
	creator, target, message string
	remindAt                 time.Time
}

func (f *fakeStore) ListUsers(_ context.Context, exclude string, limit int) ([]store.User, error) {
	var out []store.User
	for _, u := range f.users {
		if u.ID != exclude && len(out) < limit {
			out = append(out, u)
		}
	}
	return out, nil
}

func (f *fakeStore) GetDisplayName(_ context.Context, userID string) (string, error) {
	return f.names[userID], nil
}

func (f *fakeStore) CreateReminder(_ context.Context, creator, target, message string, remindAt time.Time) (int64, error) {
	f.reminders = append(f.reminders, savedReminder{creator, target, message, remindAt})
	f.nextID++
	return f.nextID, nil
}

type harness struct {
	flow    *Flow
	store   *fakeStore
	replies []events.ReplyEvent
	now     time.Time
	redis   *miniredis.Miniredis
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	h := &harness{
		store: &fakeStore{names: map[string]string{}},
		now:   time.Date(2026, 7, 19, 12, 0, 0, 0, bangkok),
		redis: mr,
	}
	h.flow = New(h.store, NewStateStore(rdb, 10*time.Minute), func(ev events.ReplyEvent) error {
		h.replies = append(h.replies, ev)
		return nil
	})
	h.flow.now = func() time.Time { return h.now }
	return h
}

func (h *harness) lastReply(t *testing.T) events.ReplyEvent {
	t.Helper()
	if len(h.replies) == 0 {
		t.Fatal("no replies published")
	}
	return h.replies[len(h.replies)-1]
}

func request(text string) events.ReminderRequestEvent {
	return events.ReminderRequestEvent{UserID: "u1", ReplyToken: "tok", Text: text}
}

// extracted mimics an event whose Message/RemindAt consumer-llm-processor's
// extractor already filled in.
func extracted(text, message string, remindAt time.Time) events.ReminderRequestEvent {
	ev := request(text)
	ev.Message = message
	if !remindAt.IsZero() {
		ev.RemindAt = remindAt.Format(time.RFC3339)
	}
	return ev
}

func postback(data string) events.PostbackEvent {
	return events.PostbackEvent{UserID: "u1", ReplyToken: "tok", Data: data}
}

func TestFullFlowForSelf(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	remindAt := h.now.Add(2 * time.Hour)

	// 1. Trigger opens the flow and asks for the target.
	h.flow.HandleRequest(ctx, request("/reminder"))
	reply := h.lastReply(t)
	if len(reply.QuickReplies) != 3 {
		t.Fatalf("target prompt buttons = %+v", reply.QuickReplies)
	}
	// The flow key must exist so line-webhook routes free text here, and the
	// AI session key must be set so the webhook forwards it at all.
	if !h.redis.Exists("chat:reminder_flow:u1") || !h.redis.Exists("chat:ai_session:u1") {
		t.Fatal("flow/session keys not set on start")
	}

	// 2. Pick "myself" - no details yet, so the flow asks for them.
	h.flow.HandlePostback(ctx, postback("flow=rem&a=target&v=self"))
	if !strings.Contains(h.lastReply(t).Text, "เตือนว่าอะไร") {
		t.Fatalf("expected details prompt, got %q", h.lastReply(t).Text)
	}

	// 3. Free text (pre-extracted upstream) with message+time reaches
	// confirmation.
	h.flow.HandleRequest(ctx, extracted("พรุ่งนี้ 9 โมง กินยา", "กินยา", remindAt))
	reply = h.lastReply(t)
	if !strings.Contains(reply.Text, "กินยา") || len(reply.QuickReplies) != 3 {
		t.Fatalf("expected confirm preview, got %+v", reply)
	}

	// 4. Confirm saves and ends the flow.
	h.flow.HandlePostback(ctx, postback("flow=rem&a=confirm"))
	if len(h.store.reminders) != 1 {
		t.Fatalf("reminders saved = %+v", h.store.reminders)
	}
	saved := h.store.reminders[0]
	if saved.creator != "u1" || saved.target != "u1" || saved.message != "กินยา" || !saved.remindAt.Equal(remindAt) {
		t.Fatalf("saved reminder = %+v", saved)
	}
	if h.redis.Exists("chat:reminder_flow:u1") {
		t.Fatal("flow key not deleted after confirm")
	}
}

func TestFlowForOtherUserWithPrefill(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	remindAt := h.now.Add(24 * time.Hour)
	h.store.users = []store.User{{ID: "u2", DisplayName: "Meow"}}
	h.store.names["u2"] = "Meow"

	// Trigger with details (pre-extracted upstream) pre-fills message+time.
	h.flow.HandleRequest(ctx, extracted("/reminder เตือนพรุ่งนี้เที่ยง ประชุมทีม", "ประชุมทีม", remindAt))

	// Choose "someone else": picker lists Meow (not the creator) + cancel.
	h.flow.HandlePostback(ctx, postback("flow=rem&a=target&v=other"))
	reply := h.lastReply(t)
	if len(reply.QuickReplies) != 2 || reply.QuickReplies[0].Label != "Meow" {
		t.Fatalf("picker = %+v", reply.QuickReplies)
	}

	// Picking the user goes straight to confirm (details were pre-filled),
	// showing the target's display name.
	h.flow.HandlePostback(ctx, postback("flow=rem&a=user&v=u2"))
	reply = h.lastReply(t)
	if !strings.Contains(reply.Text, "Meow") || !strings.Contains(reply.Text, "ประชุมทีม") {
		t.Fatalf("confirm preview = %q", reply.Text)
	}

	h.flow.HandlePostback(ctx, postback("flow=rem&a=confirm"))
	if len(h.store.reminders) != 1 || h.store.reminders[0].target != "u2" {
		t.Fatalf("saved = %+v", h.store.reminders)
	}
}

func TestPastTimeReAsks(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	h.flow.HandleRequest(ctx, request("/reminder"))
	h.flow.HandlePostback(ctx, postback("flow=rem&a=target&v=self"))

	h.flow.HandleRequest(ctx, extracted("เมื่อวาน 9 โมง กินยา", "กินยา", h.now.Add(-2*time.Hour)))
	if len(h.store.reminders) != 0 {
		t.Fatal("past-time reminder must not be saved")
	}
	if !strings.Contains(h.lastReply(t).Text, "ตอนไหน") {
		t.Fatalf("expected time re-ask, got %q", h.lastReply(t).Text)
	}
}

func TestExpiredFlowPostback(t *testing.T) {
	h := newHarness(t)
	h.flow.HandlePostback(context.Background(), postback("flow=rem&a=confirm"))
	if !strings.Contains(h.lastReply(t).Text, "หมดเวลา") {
		t.Fatalf("expected expiry message, got %q", h.lastReply(t).Text)
	}
}

func TestCancelMidFlow(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.flow.HandleRequest(ctx, request("/reminder"))
	h.flow.HandlePostback(ctx, postback("flow=rem&a=cancel"))
	if h.redis.Exists("chat:reminder_flow:u1") {
		t.Fatal("flow key survived cancel")
	}
	if !strings.Contains(h.lastReply(t).Text, "ยกเลิก") {
		t.Fatalf("expected cancel ack, got %q", h.lastReply(t).Text)
	}
}

func TestForeignPostbackIgnored(t *testing.T) {
	h := newHarness(t)
	h.flow.HandlePostback(context.Background(), postback("flow=other&a=whatever"))
	if len(h.replies) != 0 {
		t.Fatalf("foreign postback must not reply: %+v", h.replies)
	}
}

func TestEmptyUserListEndsFlow(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.flow.HandleRequest(ctx, request("/reminder"))
	h.flow.HandlePostback(ctx, postback("flow=rem&a=target&v=other"))
	if h.redis.Exists("chat:reminder_flow:u1") {
		t.Fatal("flow should end when no users are listable")
	}
}

func TestTriggerRestartsFlow(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.flow.HandleRequest(ctx, request("/reminder"))
	h.flow.HandlePostback(ctx, postback("flow=rem&a=target&v=self"))

	// Typing the trigger again starts over at the target question.
	h.flow.HandleRequest(ctx, request("/reminder"))
	reply := h.lastReply(t)
	if len(reply.QuickReplies) != 3 || !strings.Contains(reply.Text, "ใคร") {
		t.Fatalf("expected fresh target prompt, got %+v", reply)
	}
}

func TestTruncateLabel(t *testing.T) {
	if got := truncateLabel("a very long display name over twenty runes"); len([]rune(got)) != maxLabelRunes {
		t.Errorf("truncateLabel long = %q (%d runes)", got, len([]rune(got)))
	}
	if got := truncateLabel("  "); got != "(no name)" {
		t.Errorf("truncateLabel blank = %q", got)
	}
	if got := truncateLabel("Meow"); got != "Meow" {
		t.Errorf("truncateLabel short = %q", got)
	}
}
