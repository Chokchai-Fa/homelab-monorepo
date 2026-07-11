package store

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// fakeStore counts backend hits so tests can assert cache behavior.
type fakeStore struct {
	messages map[string][]Message
	getCalls int
}

func newFakeStore() *fakeStore {
	return &fakeStore{messages: make(map[string][]Message)}
}

func (f *fakeStore) GetRecent(_ context.Context, userID string) ([]Message, error) {
	f.getCalls++
	return append([]Message(nil), f.messages[userID]...), nil
}

func (f *fakeStore) Append(_ context.Context, userID, role, content string) error {
	f.messages[userID] = append(f.messages[userID], Message{Role: role, Content: content})
	return nil
}

func (f *fakeStore) Clear(_ context.Context, userID string) error {
	delete(f.messages, userID)
	return nil
}

func (f *fakeStore) Close() {}

func newTestCache(t *testing.T, backend Store, limit int) *CachedStore {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return NewCached(backend, rdb, limit, time.Minute)
}

func TestCachedStoreServesFromCacheAfterFirstLoad(t *testing.T) {
	ctx := context.Background()
	backend := newFakeStore()
	backend.messages["u1"] = []Message{{Role: RoleUser, Content: "hello"}}
	cached := newTestCache(t, backend, 20)

	for i := 0; i < 3; i++ {
		msgs, err := cached.GetRecent(ctx, "u1")
		if err != nil {
			t.Fatalf("GetRecent: %v", err)
		}
		if len(msgs) != 1 || msgs[0].Content != "hello" {
			t.Fatalf("unexpected messages: %+v", msgs)
		}
	}
	if backend.getCalls != 1 {
		t.Fatalf("backend hit %d times, want 1 (cache miss only on first read)", backend.getCalls)
	}
}

func TestCachedStoreWriteThroughAndCap(t *testing.T) {
	ctx := context.Background()
	backend := newFakeStore()
	cached := newTestCache(t, backend, 2)

	if _, err := cached.GetRecent(ctx, "u1"); err != nil { // prime cache
		t.Fatalf("GetRecent: %v", err)
	}
	for _, content := range []string{"a", "b", "c"} {
		if err := cached.Append(ctx, "u1", RoleUser, content); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// Backend keeps everything (write-through)...
	if len(backend.messages["u1"]) != 3 {
		t.Fatalf("backend has %d messages, want 3", len(backend.messages["u1"]))
	}
	// ...while the cached copy is capped without another backend read.
	msgs, err := cached.GetRecent(ctx, "u1")
	if err != nil {
		t.Fatalf("GetRecent: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Content != "b" || msgs[1].Content != "c" {
		t.Fatalf("unexpected cached messages: %+v", msgs)
	}
	if backend.getCalls != 1 {
		t.Fatalf("backend hit %d times, want 1", backend.getCalls)
	}
}

func TestCachedStoreClearEvictsBoth(t *testing.T) {
	ctx := context.Background()
	backend := newFakeStore()
	cached := newTestCache(t, backend, 20)

	if err := cached.Append(ctx, "u1", RoleUser, "hi"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := cached.Clear(ctx, "u1"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	msgs, err := cached.GetRecent(ctx, "u1")
	if err != nil {
		t.Fatalf("GetRecent: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected empty history after clear, got %+v", msgs)
	}
	if len(backend.messages["u1"]) != 0 {
		t.Fatalf("backend not cleared: %+v", backend.messages["u1"])
	}
}

func TestCachedStoreFallsBackWhenRedisDown(t *testing.T) {
	ctx := context.Background()
	backend := newFakeStore()
	backend.messages["u1"] = []Message{{Role: RoleUser, Content: "hello"}}

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cached := NewCached(backend, rdb, 20, time.Minute)
	mr.Close() // simulate Redis outage

	msgs, err := cached.GetRecent(ctx, "u1")
	if err != nil {
		t.Fatalf("GetRecent should degrade to backend, got error: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	if err := cached.Append(ctx, "u1", RoleModel, "hi there"); err != nil {
		t.Fatalf("Append should degrade to backend, got error: %v", err)
	}
}
