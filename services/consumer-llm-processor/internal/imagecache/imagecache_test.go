package imagecache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return New(rdb), mr
}

func TestStoreTakeReturnsAndDeletesOnce(t *testing.T) {
	ctx := context.Background()
	s, mr := newTestStore(t)
	if err := mr.Set(keyPrefix+"msg1", "image-bytes"); err != nil {
		t.Fatalf("seed redis: %v", err)
	}

	data, err := s.Take(ctx, "msg1")
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if string(data) != "image-bytes" {
		t.Errorf("data = %q, want image-bytes", data)
	}

	// single-use: a second Take must fail since GetDel already removed it.
	if _, err := s.Take(ctx, "msg1"); err == nil {
		t.Error("expected error on second Take of the same message id")
	}
}

func TestStoreTakeMissingKeyErrors(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.Take(context.Background(), "never-seeded"); err == nil {
		t.Error("expected error for missing key")
	}
}

func TestStorePutGeneratedSetsWithTTLAndPersists(t *testing.T) {
	ctx := context.Background()
	s, mr := newTestStore(t)

	if err := s.PutGenerated(ctx, "id1", []byte("generated-bytes"), time.Minute); err != nil {
		t.Fatalf("PutGenerated: %v", err)
	}

	got, err := mr.Get(genPrefix + "id1")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != "generated-bytes" {
		t.Errorf("stored value = %q, want generated-bytes", got)
	}
	if ttl := mr.TTL(genPrefix + "id1"); ttl <= 0 || ttl > time.Minute {
		t.Errorf("TTL = %v, want between 0 and 1m", ttl)
	}

	// Not single-use: reading twice must not delete it (unlike Take).
	if _, err := mr.Get(genPrefix + "id1"); err != nil {
		t.Errorf("second read should succeed, got: %v", err)
	}
}
