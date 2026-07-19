// Package store claims and repairs reminder rows. All time comparisons use
// the database's now() so a drifting Pi clock can't skip or double-fire
// reminders; only the Redis TTL computation uses the local clock.
//
// The DDL is the same idempotent statement consumer-reminder (the table
// owner) runs, so deploy order between the services never matters.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const schema = `
CREATE TABLE IF NOT EXISTS reminders (
	id             bigserial   PRIMARY KEY,
	creator_id     text        NOT NULL,
	target_user_id text        NOT NULL,
	message        text        NOT NULL,
	remind_at      timestamptz NOT NULL,
	status         text        NOT NULL DEFAULT 'pending',
	fail_reason    text,
	attempts       int         NOT NULL DEFAULT 0,
	created_at     timestamptz NOT NULL DEFAULT now(),
	updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_reminders_status_remind_at
	ON reminders (status, remind_at);
`

// Armed is a reminder claimed for arming in Redis.
type Armed struct {
	ID       int64
	RemindAt time.Time
}

type Store struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ensure schema: %w", err)
	}
	return &Store{pool: pool}, nil
}

// ArmDue atomically claims every pending reminder due within horizon
// (status -> scheduled) and returns them for arming in Redis. The atomic
// UPDATE...RETURNING makes an accidental second replica harmless.
func (s *Store) ArmDue(ctx context.Context, horizon time.Duration) ([]Armed, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE reminders
		SET status = 'scheduled', updated_at = now()
		WHERE status = 'pending' AND remind_at <= now() + make_interval(secs => $1)
		RETURNING id, remind_at`, horizon.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var armed []Armed
	for rows.Next() {
		var a Armed
		if err := rows.Scan(&a.ID, &a.RemindAt); err != nil {
			return nil, err
		}
		armed = append(armed, a)
	}
	return armed, rows.Err()
}

// Revert puts a claimed reminder back to pending (Redis arming failed) so
// the next tick retries it.
func (s *Store) Revert(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE reminders SET status = 'pending', updated_at = now()
		WHERE id = $1 AND status = 'scheduled'`, id)
	return err
}

// ListLostScheduled returns scheduled reminders whose fire time passed more
// than grace ago - their Redis expiry event was lost (restart, eviction,
// at-most-once delivery) and they need re-arming.
func (s *Store) ListLostScheduled(ctx context.Context, grace time.Duration) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM reminders
		WHERE status = 'scheduled' AND remind_at < now() - make_interval(secs => $1)`,
		grace.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// FailStuckSending marks reminders that were published but never
// delivery-acked (NATS drop, consumer crash) as failed so the retry pass
// picks them up.
func (s *Store) FailStuckSending(ctx context.Context, age time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE reminders
		SET status = 'failed', fail_reason = 'no_delivery_ack', updated_at = now()
		WHERE status = 'sending' AND updated_at < now() - make_interval(secs => $1)`,
		age.Seconds())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// RetryFailed re-queues retryable failures (push quota exhausted, lost acks)
// after cooldown, but gives up on reminders more than maxOverdue past their
// fire time - delivering a week-old reminder would burn next month's push
// quota on stale content.
func (s *Store) RetryFailed(ctx context.Context, cooldown, maxOverdue time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE reminders
		SET status = 'pending', attempts = attempts + 1, updated_at = now()
		WHERE status = 'failed'
		  AND fail_reason IN ('quota_429', 'no_delivery_ack')
		  AND updated_at < now() - make_interval(secs => $1)
		  AND remind_at > now() - make_interval(secs => $2)`,
		cooldown.Seconds(), maxOverdue.Seconds())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) Close() {
	s.pool.Close()
}
