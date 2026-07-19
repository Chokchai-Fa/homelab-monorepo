// Package store owns the line_users and reminders tables. The two worker
// services (worker-reminder-scheduler, subscriber-reminder-notifier) run the
// same idempotent reminders DDL at startup so deploy order never matters.
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
CREATE TABLE IF NOT EXISTS line_users (
	user_id      text PRIMARY KEY,
	display_name text        NOT NULL,
	updated_at   timestamptz NOT NULL DEFAULT now()
);
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

// User is a LINE user known to the system, listable as a reminder target.
type User struct {
	ID          string
	DisplayName string
}

type Store struct {
	pool *pgxpool.Pool
}

// New connects, ensures the schema exists and returns the store.
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

// UpsertUser records (or refreshes) a user's display name from a profile
// event.
func (s *Store) UpsertUser(ctx context.Context, userID, displayName string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO line_users (user_id, display_name)
		VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE
		SET display_name = EXCLUDED.display_name, updated_at = now()`,
		userID, displayName)
	return err
}

// ListUsers returns known users other than exclude, most recently active
// first, for the "remind someone else" picker.
func (s *Store) ListUsers(ctx context.Context, exclude string, limit int) ([]User, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, display_name FROM line_users
		WHERE user_id <> $1
		ORDER BY updated_at DESC
		LIMIT $2`, exclude, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.DisplayName); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// GetDisplayName returns the user's display name, or "" if unknown.
func (s *Store) GetDisplayName(ctx context.Context, userID string) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx,
		`SELECT display_name FROM line_users WHERE user_id = $1`, userID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return name, err
}

// CreateReminder inserts a pending reminder and returns its id.
func (s *Store) CreateReminder(ctx context.Context, creatorID, targetID, message string, remindAt time.Time) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO reminders (creator_id, target_user_id, message, remind_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id`,
		creatorID, targetID, message, remindAt.UTC()).Scan(&id)
	return id, err
}

// Reminder is one upcoming reminder as shown in the manage flow.
type Reminder struct {
	ID           int64
	Message      string
	RemindAt     time.Time
	TargetUserID string
	// TargetName is the target's display name, "" when unknown or self.
	TargetName string
}

// upcomingStatuses are the states a user can still list/edit/delete:
// anything already firing (sending) or finished (sent/failed/cancelled) is
// out of reach, which is also what keeps edit/delete race-free against the
// notifier's status-guarded claims.
const upcomingStatuses = `('pending', 'scheduled')`

// ListUpcoming returns the creator's not-yet-fired reminders, soonest first.
func (s *Store) ListUpcoming(ctx context.Context, creatorID string, limit int) ([]Reminder, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.message, r.remind_at, r.target_user_id,
		       COALESCE(u.display_name, '')
		FROM reminders r
		LEFT JOIN line_users u ON u.user_id = r.target_user_id
		WHERE r.creator_id = $1 AND r.status IN `+upcomingStatuses+`
		ORDER BY r.remind_at ASC
		LIMIT $2`, creatorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Reminder
	for rows.Next() {
		var r Reminder
		if err := rows.Scan(&r.ID, &r.Message, &r.RemindAt, &r.TargetUserID, &r.TargetName); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetReminder returns one upcoming reminder, but only to its creator.
// (nil, nil) when it doesn't exist, belongs to someone else, or already left
// the upcoming states.
func (s *Store) GetReminder(ctx context.Context, id int64, creatorID string) (*Reminder, error) {
	var r Reminder
	err := s.pool.QueryRow(ctx, `
		SELECT r.id, r.message, r.remind_at, r.target_user_id,
		       COALESCE(u.display_name, '')
		FROM reminders r
		LEFT JOIN line_users u ON u.user_id = r.target_user_id
		WHERE r.id = $1 AND r.creator_id = $2 AND r.status IN `+upcomingStatuses,
		id, creatorID).Scan(&r.ID, &r.Message, &r.RemindAt, &r.TargetUserID, &r.TargetName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// CancelReminder marks an upcoming reminder cancelled. The status guard means
// an already-armed Redis timer fires into nothing: the notifier's claim
// (WHERE status='scheduled') finds no row. Returns false when there was
// nothing to cancel.
func (s *Store) CancelReminder(ctx context.Context, id int64, creatorID string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE reminders SET status = 'cancelled', updated_at = now()
		WHERE id = $1 AND creator_id = $2 AND status IN `+upcomingStatuses,
		id, creatorID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// UpdateReminder rewrites an upcoming reminder's message and time, resetting
// it to 'pending' so the scheduler re-arms it at the new time. If the old
// time was already armed in Redis, that expiry claims against
// status='scheduled', misses, and dies - no fire at the old time.
func (s *Store) UpdateReminder(ctx context.Context, id int64, creatorID, message string, remindAt time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE reminders
		SET message = $3, remind_at = $4, status = 'pending', updated_at = now()
		WHERE id = $1 AND creator_id = $2 AND status IN `+upcomingStatuses,
		id, creatorID, message, remindAt.UTC())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) Close() {
	s.pool.Close()
}
