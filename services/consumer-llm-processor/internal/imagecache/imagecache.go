// Package imagecache reads the image bytes line-webhook stashed in Redis for
// an AI request, matching services/line-webhook/internal/imagecache's key
// scheme. The webhook never puts image bytes on NATS - the payload would
// blow past NATS's default message size - so this is the other half of that
// handoff.
package imagecache

import (
	"context"

	"github.com/redis/go-redis/v9"
)

const keyPrefix = "chat:image:"

// Store reads short-lived image blobs from Redis.
type Store struct {
	redis *redis.Client
}

func New(rdb *redis.Client) *Store {
	return &Store{redis: rdb}
}

// Take fetches and deletes the image stored under messageID in one round
// trip, so a NATS-level redelivery can't reuse stale bytes.
func (s *Store) Take(ctx context.Context, messageID string) ([]byte, error) {
	return s.redis.GetDel(ctx, keyPrefix+messageID).Bytes()
}
