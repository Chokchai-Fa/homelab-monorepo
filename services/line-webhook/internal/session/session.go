package session

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// Store tracks which users have an active AI session in Redis, shared with
// any future webhook replicas. A session starts with the AI prefix ("/ai"),
// ends with the end command ("/ai-end") and expires automatically after TTL
// of inactivity - every message in the session slides the expiry forward.
type Store struct {
	redis *redis.Client
	ttl   time.Duration
}

func New(rdb *redis.Client, ttl time.Duration) *Store {
	return &Store{redis: rdb, ttl: ttl}
}

func key(userID string) string {
	return fmt.Sprintf("chat:ai_session:%s", userID)
}

// Start opens (or refreshes) an AI session for the user.
func (s *Store) Start(ctx context.Context, userID string) error {
	return s.redis.Set(ctx, key(userID), "1", s.ttl).Err()
}

// Active reports whether the user has a live session and, if so, slides its
// expiry forward. Redis errors degrade to "no session" so the webhook keeps
// working with the /ai prefix alone.
func (s *Store) Active(ctx context.Context, userID string) bool {
	ok, err := s.redis.Expire(ctx, key(userID), s.ttl).Result()
	if err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("session: redis check failed - treating as inactive")
		return false
	}
	return ok
}

// End closes the user's AI session.
func (s *Store) End(ctx context.Context, userID string) error {
	return s.redis.Del(ctx, key(userID)).Err()
}

func flowKey(userID string) string {
	return fmt.Sprintf("chat:reminder_flow:%s", userID)
}

// FlowActive reports whether consumer-reminder has a reminder conversation
// in progress for the user (state it writes under the shared Redis key).
// Mid-flow free text must keep reaching the AI pipeline without opening an
// AI session. Redis errors degrade to "no flow".
func (s *Store) FlowActive(ctx context.Context, userID string) bool {
	n, err := s.redis.Exists(ctx, flowKey(userID)).Result()
	if err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("session: reminder flow check failed - treating as inactive")
		return false
	}
	return n > 0
}
