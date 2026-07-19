// Package reminderflow answers whether a user is in the middle of a reminder
// conversation. The flow state itself is owned by consumer-reminder (written
// under the same key in the shared Redis); this service only checks
// existence to route mid-flow free text to extraction + handoff instead of
// the chat LLM (and past the debouncer).
package reminderflow

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

type Store struct {
	redis *redis.Client
}

func New(rdb *redis.Client) *Store {
	return &Store{redis: rdb}
}

func key(userID string) string {
	return fmt.Sprintf("chat:reminder_flow:%s", userID)
}

// Active reports whether the user has a reminder flow in progress. Redis
// errors degrade to "no flow" so normal chat keeps working.
func (s *Store) Active(ctx context.Context, userID string) bool {
	n, err := s.redis.Exists(ctx, key(userID)).Result()
	if err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("reminderflow: redis check failed - treating as inactive")
		return false
	}
	return n > 0
}
