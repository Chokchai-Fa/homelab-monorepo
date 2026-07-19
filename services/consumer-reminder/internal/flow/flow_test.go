package flow

import (
	"context"
	"fmt"
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

func (f *fakeStore) ListUpcoming(_ context.Context, creatorID string, limit int) ([]store.Reminder, error) {
	var out []store.Reminder
	for i, r := range f.reminders {
		if r.creator == creatorID && len(out) < limit {
			out = append(out, store.Reminder{
				ID: int64(i + 1), Message: r.message, RemindAt: r.remindAt,
				TargetUserID: r.target, TargetName: f.names[r.target],
			})
		}
	}
	return out, nil
}

func (f *fakeStore) GetReminder(_ context.Context, id int64, creatorID string) (*store.Reminder, error) {
	i := int(id) - 1
	if i < 0 || i >= len(f.reminders) || f.reminders[i].creator != creatorID {
		return nil, nil
	}
	r := f.reminders[i]
	return &store.Reminder{ID: id, Message: r.message, RemindAt: r.remindAt,
		TargetUserID: r.target, TargetName: f.names[r.target]}, nil
}

func (f *fakeStore) CancelReminder(_ context.Context, id int64, creatorID string) (bool, error) {
	i := int(id) - 1
	if i < 0 || i >= len(f.reminders) || f.reminders[i].creator != creatorID {
		return false, nil
	}
	f.reminders = append(f.reminders[:i], f.reminders[i+1:]...)
	return true, nil
}

func (f *fakeStore) UpdateReminder(_ context.Context, id int64, creatorID, message string, remindAt time.Time) (bool, error) {
	i := int(id) - 1
	if i < 0 || i >= len(f.reminders) || f.reminders[i].creator != creatorID {
		return false, nil
	}
	f.reminders[i].message = message
	f.reminders[i].remindAt = remindAt
	return true, nil
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
	// The flow key must exist so line-webhook routes free text here - and
	// only the flow key: opening an AI session would leave the user stuck in
	// AI mode after the flow ends.
	if !h.redis.Exists("chat:reminder_flow:u1") {
		t.Fatal("flow key not set on start")
	}
	if h.redis.Exists("chat:ai_session:u1") {
		t.Fatal("reminder flow must not open an AI session")
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

func TestTypedCancelMidFlow(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.flow.HandleRequest(ctx, request("/reminder"))
	h.flow.HandlePostback(ctx, postback("flow=rem&a=target&v=self"))

	// Typing "ยกเลิก" must cancel, not become the reminder message.
	h.flow.HandleRequest(ctx, request("ยกเลิก"))
	if h.redis.Exists("chat:reminder_flow:u1") {
		t.Fatal("flow key survived typed cancel")
	}
	if !strings.Contains(h.lastReply(t).Text, "ยกเลิก") {
		t.Fatalf("expected cancel ack, got %q", h.lastReply(t).Text)
	}
	if len(h.store.reminders) != 0 {
		t.Fatalf("no reminder should be saved after cancel: %+v", h.store.reminders)
	}
}

func TestMultilineMessageSurvives(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	remindAt := h.now.Add(2 * time.Hour)
	message := "ซื้อของเข้าบ้าน:\n- นม\n- ไข่\n- ข้าวสาร"

	h.flow.HandleRequest(ctx, request("/reminder"))
	h.flow.HandlePostback(ctx, postback("flow=rem&a=target&v=self"))
	h.flow.HandleRequest(ctx, extracted(message+" พรุ่งนี้ 9 โมง", message, remindAt))
	h.flow.HandlePostback(ctx, postback("flow=rem&a=confirm"))

	if len(h.store.reminders) != 1 || h.store.reminders[0].message != message {
		t.Fatalf("multi-line message mangled: %+v", h.store.reminders)
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

// seedReminder puts one reminder in the fake store the way the create flow
// would, returning its id.
func (h *harness) seedReminder(message string, at time.Time) int64 {
	h.store.reminders = append(h.store.reminders, savedReminder{"u1", "u1", message, at})
	return int64(len(h.store.reminders))
}

func TestListUpcoming(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.seedReminder("กินยา", h.now.Add(2*time.Hour))
	h.seedReminder("ประชุม", h.now.Add(24*time.Hour))

	h.flow.HandleRequest(ctx, request("/reminders"))
	reply := h.lastReply(t)
	if !strings.Contains(reply.Text, "กินยา") || !strings.Contains(reply.Text, "ประชุม") {
		t.Fatalf("list text = %q", reply.Text)
	}
	// One pick button per reminder + cancel.
	if len(reply.QuickReplies) != 3 {
		t.Fatalf("list buttons = %+v", reply.QuickReplies)
	}
	if !h.redis.Exists("chat:reminder_flow:u1") {
		t.Fatal("manage flow state not set")
	}
}

func TestListEmpty(t *testing.T) {
	h := newHarness(t)
	h.flow.HandleRequest(context.Background(), request("ดูเตือน"))
	if !strings.Contains(h.lastReply(t).Text, "ยังไม่มีรายการ") {
		t.Fatalf("expected empty-list message, got %q", h.lastReply(t).Text)
	}
	if h.redis.Exists("chat:reminder_flow:u1") {
		t.Fatal("empty list must not leave flow state")
	}
}

func TestDeleteReminder(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	id := h.seedReminder("กินยา", h.now.Add(2*time.Hour))

	h.flow.HandleRequest(ctx, request("/reminders"))
	h.flow.HandlePostback(ctx, postback("flow=rem&a=pick&v=1"))
	reply := h.lastReply(t)
	if len(reply.QuickReplies) != 3 || !strings.Contains(reply.Text, "กินยา") {
		t.Fatalf("pick reply = %+v", reply)
	}

	h.flow.HandlePostback(ctx, postback(fmt.Sprintf("flow=rem&a=rdel&v=%d", id)))
	if !strings.Contains(h.lastReply(t).Text, "ลบแล้ว") {
		t.Fatalf("expected delete ack, got %q", h.lastReply(t).Text)
	}
	if len(h.store.reminders) != 0 {
		t.Fatalf("reminder not deleted: %+v", h.store.reminders)
	}
	if h.redis.Exists("chat:reminder_flow:u1") {
		t.Fatal("flow state survived delete")
	}
}

func TestEditReminderTimeOnly(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	id := h.seedReminder("กินยา", h.now.Add(2*time.Hour))
	newAt := h.now.Add(48 * time.Hour)

	h.flow.HandleRequest(ctx, request("/reminders"))
	h.flow.HandlePostback(ctx, postback(fmt.Sprintf("flow=rem&a=redit&v=%d", id)))
	if !strings.Contains(h.lastReply(t).Text, "แก้ไข") {
		t.Fatalf("expected edit prompt, got %q", h.lastReply(t).Text)
	}

	// User sends only a new time; the old message must survive.
	h.flow.HandleRequest(ctx, extracted("มะรืนนี้ 14:00", "", newAt))
	reply := h.lastReply(t)
	if !strings.Contains(reply.Text, "กินยา") || len(reply.QuickReplies) != 3 {
		t.Fatalf("expected confirm preview with old message, got %+v", reply)
	}

	h.flow.HandlePostback(ctx, postback("flow=rem&a=confirm"))
	if !strings.Contains(h.lastReply(t).Text, "แก้ไขแล้ว") {
		t.Fatalf("expected edit ack, got %q", h.lastReply(t).Text)
	}
	if len(h.store.reminders) != 1 {
		t.Fatalf("edit must not create a new reminder: %+v", h.store.reminders)
	}
	saved := h.store.reminders[0]
	if saved.message != "กินยา" || !saved.remindAt.Equal(newAt) {
		t.Fatalf("edited reminder = %+v", saved)
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
