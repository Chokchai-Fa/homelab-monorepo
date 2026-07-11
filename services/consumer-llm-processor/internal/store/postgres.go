package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const schema = `
CREATE TABLE IF NOT EXISTS line_ai_messages (
	id         bigserial PRIMARY KEY,
	user_id    text        NOT NULL,
	role       text        NOT NULL,
	content    text        NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_line_ai_messages_user_created
	ON line_ai_messages (user_id, created_at);
`

// PostgresStore persists conversation history in Postgres.
type PostgresStore struct {
	pool  *pgxpool.Pool
	limit int
}

// NewPostgres connects, ensures the schema exists and returns the store.
// limit caps how many recent messages GetRecent returns per user.
func NewPostgres(ctx context.Context, databaseURL string, limit int) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ensure schema: %w", err)
	}
	return &PostgresStore{pool: pool, limit: limit}, nil
}

func (s *PostgresStore) GetRecent(ctx context.Context, userID string) ([]Message, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT role, content FROM (
			SELECT role, content, created_at, id
			FROM line_ai_messages
			WHERE user_id = $1
			ORDER BY id DESC
			LIMIT $2
		) recent
		ORDER BY id ASC`, userID, s.limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.Role, &m.Content); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

func (s *PostgresStore) Append(ctx context.Context, userID, role, content string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO line_ai_messages (user_id, role, content) VALUES ($1, $2, $3)`,
		userID, role, content)
	return err
}

func (s *PostgresStore) Clear(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM line_ai_messages WHERE user_id = $1`, userID)
	return err
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}
