// Package scheduler runs the periodic pass that turns due reminder rows
// into expiring Redis keys (`reminder:fire:<id>`), whose expiry events
// subscriber-reminder-notifier turns into LINE flex messages. It also
// repairs everything that can go wrong in that relay: lost expiry events,
// lost delivery acks, and push-quota failures.
package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"worker-reminder-scheduler/internal/store"
)

// FireKeyPrefix must match subscriber-reminder-notifier's filter.
const FireKeyPrefix = "reminder:fire:"

const (
	// lostGrace is how long past the fire time a scheduled reminder may sit
	// before it counts as a lost expiry event.
	lostGrace = 2 * time.Minute
	// sendingTimeout is how long a published reminder may await its
	// delivery ack.
	sendingTimeout = 5 * time.Minute
	// retryCooldown spaces retries of failed deliveries; with the push
	// quota exhausted this is what paces "try again until quota resets".
	retryCooldown = time.Hour
	// maxOverdue is the point where a failed reminder is abandoned.
	maxOverdue = 7 * 24 * time.Hour
)

// Store is the subset of the Postgres store the pass needs.
type Store interface {
	ArmDue(ctx context.Context, horizon time.Duration) ([]store.Armed, error)
	Revert(ctx context.Context, id int64) error
	ListLostScheduled(ctx context.Context, grace time.Duration) ([]int64, error)
	FailStuckSending(ctx context.Context, age time.Duration) (int64, error)
	RetryFailed(ctx context.Context, cooldown, maxOverdue time.Duration) (int64, error)
}

type Scheduler struct {
	store   Store
	redis   *redis.Client
	horizon time.Duration
	now     func() time.Time
}

func New(st Store, rdb *redis.Client, horizon time.Duration) *Scheduler {
	return &Scheduler{store: st, redis: rdb, horizon: horizon, now: time.Now}
}

func fireKey(id int64) string {
	return fmt.Sprintf("%s%d", FireKeyPrefix, id)
}

// RunPass executes one scheduling tick: arm, then repair.
func (s *Scheduler) RunPass(ctx context.Context) {
	s.armDue(ctx)
	s.recoverLost(ctx)
	s.repairStatuses(ctx)
}

// armDue claims pending reminders due within the horizon and arms each as a
// Redis key expiring at its fire time.
func (s *Scheduler) armDue(ctx context.Context) {
	armed, err := s.store.ArmDue(ctx, s.horizon)
	if err != nil {
		log.Error().Err(err).Msg("arm: claim query failed")
		return
	}
	for _, a := range armed {
		ttl := a.RemindAt.Sub(s.now())
		if ttl < time.Second {
			ttl = time.Second
		}
		if err := s.redis.Set(ctx, fireKey(a.ID), "1", ttl).Err(); err != nil {
			log.Error().Int64("reminder_id", a.ID).Err(err).Msg("arm: redis SET failed - reverting to pending")
			if revErr := s.store.Revert(ctx, a.ID); revErr != nil {
				log.Error().Int64("reminder_id", a.ID).Err(revErr).Msg("arm: revert failed - lost-recovery will pick it up")
			}
			continue
		}
		log.Info().Int64("reminder_id", a.ID).Time("remind_at", a.RemindAt).Dur("ttl", ttl).Msg("arm: reminder armed")
	}
}

// recoverLost re-arms scheduled reminders whose expiry event never fired
// (Redis restart, eviction under allkeys-lru, at-most-once pub/sub). A 1s
// TTL re-fires them through the normal notifier path - no duplicated
// delivery logic here.
func (s *Scheduler) recoverLost(ctx context.Context) {
	ids, err := s.store.ListLostScheduled(ctx, lostGrace)
	if err != nil {
		log.Error().Err(err).Msg("recover: lost query failed")
		return
	}
	for _, id := range ids {
		exists, err := s.redis.Exists(ctx, fireKey(id)).Result()
		if err != nil {
			log.Error().Int64("reminder_id", id).Err(err).Msg("recover: redis check failed")
			continue
		}
		if exists > 0 {
			// Key still armed (long horizon or clock skew) - leave it be.
			continue
		}
		if err := s.redis.Set(ctx, fireKey(id), "1", time.Second).Err(); err != nil {
			log.Error().Int64("reminder_id", id).Err(err).Msg("recover: re-arm failed")
			continue
		}
		log.Warn().Int64("reminder_id", id).Msg("recover: lost expiry event - re-armed with 1s TTL")
	}
}

// repairStatuses handles the delivery-side failures: stuck 'sending' rows
// become failed, and retryable failures go back to pending once their
// cooldown passes (the SQL cooldown paces this; running it every tick is
// harmless).
func (s *Scheduler) repairStatuses(ctx context.Context) {
	if n, err := s.store.FailStuckSending(ctx, sendingTimeout); err != nil {
		log.Error().Err(err).Msg("repair: stuck-sending query failed")
	} else if n > 0 {
		log.Warn().Int64("count", n).Msg("repair: sending reminders without delivery ack marked failed")
	}
	if n, err := s.store.RetryFailed(ctx, retryCooldown, maxOverdue); err != nil {
		log.Error().Err(err).Msg("repair: retry query failed")
	} else if n > 0 {
		log.Info().Int64("count", n).Msg("repair: failed reminders re-queued for retry")
	}
}
