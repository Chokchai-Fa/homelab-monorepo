// Package profilegate rate-limits LINE profile fetches: the webhook calls
// GetProfile at most once per user per TTL, publishing the result for
// consumer-reminder to upsert into the line_users table.
package profilegate

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

type Gate struct {
	redis *redis.Client
	ttl   time.Duration
}

func New(rdb *redis.Client, ttl time.Duration) *Gate {
	return &Gate{redis: rdb, ttl: ttl}
}

func key(userID string) string {
	return fmt.Sprintf("chat:profile_seen:%s", userID)
}

// TryClaim reports whether this caller won the right to fetch the user's
// profile (at most one winner per TTL). Redis errors degrade to "no" so a
// broken Redis never triggers a GetProfile stampede.
func (g *Gate) TryClaim(ctx context.Context, userID string) bool {
	ok, err := g.redis.SetNX(ctx, key(userID), "1", g.ttl).Result()
	if err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("profilegate: redis claim failed - skipping profile fetch")
		return false
	}
	return ok
}

// Release gives the claim back after a failed fetch so the user's next
// message retries instead of waiting out the TTL.
func (g *Gate) Release(ctx context.Context, userID string) {
	if err := g.redis.Del(ctx, key(userID)).Err(); err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("profilegate: release failed")
	}
}
