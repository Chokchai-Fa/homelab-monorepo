// Package store claims fired reminders and records delivery outcomes.
// Same idempotent reminders DDL as consumer-reminder (the table owner) and
// worker-reminder-scheduler, so deploy order never matters.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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

// Reminder is a claimed row, ready to become a flex message.
type Reminder struct {
	ID           int64
	CreatorID    string
	TargetUserID string
	Message      string
	RemindAt     time.Time
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

// Claim atomically moves a scheduled reminder to sending and returns it.
// found=false means someone else already claimed it (re-arm race, a second
// replica, or a duplicate expiry event) - not an error, just a no-op.
func (s *Store) Claim(ctx context.Context, id int64) (Reminder, bool, error) {
	var r Reminder
	err := s.pool.QueryRow(ctx, `
		UPDATE reminders
		SET status = 'sending', updated_at = now()
		WHERE id = $1 AND status = 'scheduled'
		RETURNING id, creator_id, target_user_id, message, remind_at`, id).
		Scan(&r.ID, &r.CreatorID, &r.TargetUserID, &r.Message, &r.RemindAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reminder{}, false, nil
	}
	if err != nil {
		return Reminder{}, false, err
	}
	return r, true, nil
}

// Revert puts a claimed reminder back so the scheduler's recovery pass
// re-arms it, used when this service fails to publish the NATS event.
func (s *Store) Revert(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE reminders SET status = 'scheduled', updated_at = now()
		WHERE id = $1 AND status = 'sending'`, id)
	return err
}

// MarkSent records a successful delivery.
func (s *Store) MarkSent(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE reminders SET status = 'sent', updated_at = now()
		WHERE id = $1`, id)
	return err
}

// MarkFailed records a failed delivery with a reason worker-reminder-scheduler
// uses to decide whether (and when) to retry.
func (s *Store) MarkFailed(ctx context.Context, id int64, reason string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE reminders SET status = 'failed', fail_reason = $2, updated_at = now()
		WHERE id = $1`, id, reason)
	return err
}

// GetDisplayName returns a user's display name, or "" if unknown. line_users
// is owned by consumer-reminder (schema created there); this service only
// reads it, which is safe as long as consumer-reminder is deployed first
// (see the monorepo rollout wave order).
func (s *Store) GetDisplayName(ctx context.Context, userID string) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx,
		`SELECT display_name FROM line_users WHERE user_id = $1`, userID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return name, err
}

func (s *Store) Close() {
	s.pool.Close()
}
