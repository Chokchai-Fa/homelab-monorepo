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

func (s *Store) Close() {
	s.pool.Close()
}
