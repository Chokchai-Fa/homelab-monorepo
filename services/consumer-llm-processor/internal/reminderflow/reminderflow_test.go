package reminderflow

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return New(rdb), mr
}

func TestActiveReturnsFalseWhenNoFlow(t *testing.T) {
	s, _ := newTestStore(t)
	if s.Active(context.Background(), "u1") {
		t.Error("expected Active to be false with no flow key set")
	}
}

func TestActiveReturnsTrueWhenFlowKeyExists(t *testing.T) {
	s, mr := newTestStore(t)
	if err := mr.Set(key("u1"), "1"); err != nil {
		t.Fatalf("seed redis: %v", err)
	}
	if !s.Active(context.Background(), "u1") {
		t.Error("expected Active to be true when flow key exists")
	}
}

func TestActiveIsPerUser(t *testing.T) {
	s, mr := newTestStore(t)
	if err := mr.Set(key("u1"), "1"); err != nil {
		t.Fatalf("seed redis: %v", err)
	}
	if s.Active(context.Background(), "u2") {
		t.Error("expected Active(u2) to be false; only u1 has a flow key")
	}
}

func TestActiveDegradesToFalseOnRedisError(t *testing.T) {
	s, mr := newTestStore(t)
	if err := mr.Set(key("u1"), "1"); err != nil {
		t.Fatalf("seed redis: %v", err)
	}
	mr.Close() // simulate Redis outage

	if s.Active(context.Background(), "u1") {
		t.Error("expected Active to degrade to false when redis is unreachable")
	}
}
