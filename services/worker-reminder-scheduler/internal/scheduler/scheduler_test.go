package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"worker-reminder-scheduler/internal/store"
)

type fakeStore struct {
	toArm      []store.Armed
	armErr     error
	reverted   []int64
	revertErr  error
	lost       []int64
	lostErr    error
	stuckCount int64
	stuckErr   error
	retryCount int64
	retryErr   error
}

func (f *fakeStore) ArmDue(_ context.Context, _ time.Duration) ([]store.Armed, error) {
	return f.toArm, f.armErr
}
func (f *fakeStore) Revert(_ context.Context, id int64) error {
	f.reverted = append(f.reverted, id)
	return f.revertErr
}
func (f *fakeStore) ListLostScheduled(_ context.Context, _ time.Duration) ([]int64, error) {
	return f.lost, f.lostErr
}
func (f *fakeStore) FailStuckSending(_ context.Context, _ time.Duration) (int64, error) {
	return f.stuckCount, f.stuckErr
}
func (f *fakeStore) RetryFailed(_ context.Context, _, _ time.Duration) (int64, error) {
	return f.retryCount, f.retryErr
}

func newRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

// newDeadRedis returns a client pointed at a miniredis instance that has
// already been shut down, so every command fails with a dial error. Used to
// exercise the redis-error branches without touching a real server.
// MaxRetries is disabled so the failing calls return quickly.
func newDeadRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	addr := mr.Addr()
	mr.Close()
	return redis.NewClient(&redis.Options{Addr: addr, MaxRetries: -1})
}

func TestArmDueSetsExpiringKey(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	fs := &fakeStore{toArm: []store.Armed{
		{ID: 42, RemindAt: now.Add(90 * time.Second)},
	}}
	rdb := newRedis(t)
	s := New(fs, rdb, 5*time.Minute)
	s.now = func() time.Time { return now }

	s.armDue(context.Background())

	ttl := rdb.TTL(context.Background(), fireKey(42)).Val()
	if ttl <= 0 || ttl > 90*time.Second {
		t.Fatalf("ttl = %v, want ~90s", ttl)
	}
	if len(fs.reverted) != 0 {
		t.Fatalf("unexpected revert: %+v", fs.reverted)
	}
}

func TestArmDueClampsPastDueToOneSecond(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	fs := &fakeStore{toArm: []store.Armed{
		{ID: 7, RemindAt: now.Add(-time.Minute)}, // already overdue when claimed
	}}
	rdb := newRedis(t)
	s := New(fs, rdb, 5*time.Minute)
	s.now = func() time.Time { return now }

	s.armDue(context.Background())

	ttl := rdb.TTL(context.Background(), fireKey(7)).Val()
	if ttl <= 0 || ttl > time.Second {
		t.Fatalf("ttl = %v, want ~1s", ttl)
	}
}

func TestRecoverLostReArmsMissingKey(t *testing.T) {
	fs := &fakeStore{lost: []int64{99}}
	rdb := newRedis(t)
	s := New(fs, rdb, 5*time.Minute)

	s.recoverLost(context.Background())

	if rdb.Exists(context.Background(), fireKey(99)).Val() == 0 {
		t.Fatal("lost reminder was not re-armed")
	}
}

func TestRecoverLostLeavesExistingKeyAlone(t *testing.T) {
	fs := &fakeStore{lost: []int64{5}}
	rdb := newRedis(t)
	rdb.Set(context.Background(), fireKey(5), "1", time.Hour)
	s := New(fs, rdb, 5*time.Minute)

	s.recoverLost(context.Background())

	ttl := rdb.TTL(context.Background(), fireKey(5)).Val()
	if ttl < 30*time.Minute {
		t.Fatalf("existing armed key was overwritten: ttl=%v", ttl)
	}
}

func TestArmDueClaimQueryFailureDoesNothing(t *testing.T) {
	fs := &fakeStore{armErr: errors.New("boom")}
	rdb := newRedis(t)
	s := New(fs, rdb, 5*time.Minute)

	s.armDue(context.Background()) // must not panic

	if len(fs.reverted) != 0 {
		t.Fatalf("unexpected revert on query failure: %+v", fs.reverted)
	}
}

func TestArmDueRedisFailureRevertsClaim(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	fs := &fakeStore{toArm: []store.Armed{
		{ID: 42, RemindAt: now.Add(90 * time.Second)},
	}}
	rdb := newDeadRedis(t)
	s := New(fs, rdb, 5*time.Minute)
	s.now = func() time.Time { return now }

	s.armDue(context.Background())

	if len(fs.reverted) != 1 || fs.reverted[0] != 42 {
		t.Fatalf("reverted = %+v, want [42]", fs.reverted)
	}
}

func TestArmDueRedisFailureAndRevertFailureDoesNotPanic(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	fs := &fakeStore{
		toArm:     []store.Armed{{ID: 7, RemindAt: now.Add(time.Minute)}},
		revertErr: errors.New("revert also failed"),
	}
	rdb := newDeadRedis(t)
	s := New(fs, rdb, 5*time.Minute)
	s.now = func() time.Time { return now }

	s.armDue(context.Background()) // must not panic even though revert fails too

	if len(fs.reverted) != 1 || fs.reverted[0] != 7 {
		t.Fatalf("reverted = %+v, want [7] (revert attempted regardless of its own error)", fs.reverted)
	}
}

func TestRecoverLostQueryFailureDoesNothing(t *testing.T) {
	fs := &fakeStore{lostErr: errors.New("boom")}
	rdb := newRedis(t)
	s := New(fs, rdb, 5*time.Minute)

	s.recoverLost(context.Background()) // must not panic
}

func TestRecoverLostRedisFailureDoesNotPanic(t *testing.T) {
	fs := &fakeStore{lost: []int64{99}}
	rdb := newDeadRedis(t)
	s := New(fs, rdb, 5*time.Minute)

	s.recoverLost(context.Background()) // Exists and Set both fail; must not panic
}

func TestRepairStatuses(t *testing.T) {
	tests := []struct {
		name string
		fs   *fakeStore
	}{
		{"both succeed with zero counts", &fakeStore{}},
		{"both succeed with nonzero counts", &fakeStore{stuckCount: 3, retryCount: 2}},
		{"stuck query fails", &fakeStore{stuckErr: errors.New("stuck query failed")}},
		{"retry query fails", &fakeStore{retryErr: errors.New("retry query failed")}},
		{"both fail", &fakeStore{stuckErr: errors.New("a"), retryErr: errors.New("b")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rdb := newRedis(t)
			s := New(tt.fs, rdb, 5*time.Minute)
			s.repairStatuses(context.Background()) // errors are only logged; must not panic
		})
	}
}

func TestRunPassCallsAllSteps(t *testing.T) {
	fs := &fakeStore{
		toArm:      []store.Armed{{ID: 1, RemindAt: time.Now().Add(time.Minute)}},
		lost:       []int64{2},
		stuckCount: 1,
		retryCount: 1,
	}
	rdb := newRedis(t)
	s := New(fs, rdb, 5*time.Minute)

	// Must not panic and must touch both reminder ids.
	s.RunPass(context.Background())

	if rdb.Exists(context.Background(), fireKey(1)).Val() == 0 {
		t.Error("armed reminder not set")
	}
	if rdb.Exists(context.Background(), fireKey(2)).Val() == 0 {
		t.Error("recovered reminder not set")
	}
}
