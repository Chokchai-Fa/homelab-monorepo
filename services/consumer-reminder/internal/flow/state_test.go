package flow

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestStateStore(t *testing.T, ttl time.Duration) (*StateStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return NewStateStore(rdb, ttl), mr
}

func TestStateStoreGetMissingReturnsNil(t *testing.T) {
	s, _ := newTestStateStore(t, time.Minute)
	got, err := s.Get(context.Background(), "u1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != nil {
		t.Fatalf("Get() = %+v, want nil", got)
	}
}

func TestStateStorePutGetRoundTrip(t *testing.T) {
	s, _ := newTestStateStore(t, time.Minute)
	ctx := context.Background()
	remindAt := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	want := &State{
		Step:         StepAwaitConfirm,
		TargetUserID: "u2",
		Message:      "กินยา",
		RemindAt:     remindAt,
		EditingID:    5,
	}

	if err := s.Put(ctx, "u1", want); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	got, err := s.Get(ctx, "u1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got == nil {
		t.Fatal("Get() = nil, want state")
	}
	if got.Step != want.Step || got.TargetUserID != want.TargetUserID ||
		got.Message != want.Message || got.EditingID != want.EditingID ||
		!got.RemindAt.Equal(want.RemindAt) {
		t.Fatalf("Get() = %+v, want %+v", got, want)
	}
}

func TestStateStoreDifferentUsersIsolated(t *testing.T) {
	s, _ := newTestStateStore(t, time.Minute)
	ctx := context.Background()

	if err := s.Put(ctx, "u1", &State{Step: StepAwaitTarget}); err != nil {
		t.Fatalf("Put(u1) error = %v", err)
	}
	got, err := s.Get(ctx, "u2")
	if err != nil {
		t.Fatalf("Get(u2) error = %v", err)
	}
	if got != nil {
		t.Fatalf("Get(u2) = %+v, want nil (u1's state must not leak)", got)
	}
}

func TestStateStoreDelete(t *testing.T) {
	s, mr := newTestStateStore(t, time.Minute)
	ctx := context.Background()

	if err := s.Put(ctx, "u1", &State{Step: StepAwaitTarget}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := s.Delete(ctx, "u1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if mr.Exists("chat:reminder_flow:u1") {
		t.Fatal("key still exists after Delete")
	}
	got, err := s.Get(ctx, "u1")
	if err != nil {
		t.Fatalf("Get() after delete error = %v", err)
	}
	if got != nil {
		t.Fatalf("Get() after delete = %+v, want nil", got)
	}
}

func TestStateStoreDeleteMissingIsNoop(t *testing.T) {
	s, _ := newTestStateStore(t, time.Minute)
	if err := s.Delete(context.Background(), "never-existed"); err != nil {
		t.Fatalf("Delete() on missing key error = %v", err)
	}
}

func TestStateStorePutSlidesTTL(t *testing.T) {
	s, mr := newTestStateStore(t, 10*time.Minute)
	ctx := context.Background()

	if err := s.Put(ctx, "u1", &State{Step: StepAwaitTarget}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	ttl := mr.TTL("chat:reminder_flow:u1")
	if ttl != 10*time.Minute {
		t.Fatalf("TTL = %v, want %v", ttl, 10*time.Minute)
	}

	mr.FastForward(9 * time.Minute)
	if err := s.Put(ctx, "u1", &State{Step: StepAwaitConfirm}); err != nil {
		t.Fatalf("second Put() error = %v", err)
	}
	ttl = mr.TTL("chat:reminder_flow:u1")
	if ttl != 10*time.Minute {
		t.Fatalf("TTL after re-Put = %v, want slid back to %v", ttl, 10*time.Minute)
	}
}

func TestStateStoreExpiredStateIsGone(t *testing.T) {
	s, mr := newTestStateStore(t, time.Minute)
	ctx := context.Background()

	if err := s.Put(ctx, "u1", &State{Step: StepAwaitTarget}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	mr.FastForward(2 * time.Minute)

	got, err := s.Get(ctx, "u1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != nil {
		t.Fatalf("Get() after TTL expiry = %+v, want nil", got)
	}
}

func TestStateStoreGetBadJSONErrors(t *testing.T) {
	s, mr := newTestStateStore(t, time.Minute)
	if err := mr.Set("chat:reminder_flow:u1", "not json"); err != nil {
		t.Fatalf("seeding bad value: %v", err)
	}
	if _, err := s.Get(context.Background(), "u1"); err == nil {
		t.Fatal("Get() with corrupt JSON: expected error, got nil")
	}
}
