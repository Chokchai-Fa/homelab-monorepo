// Package imagecache stashes downloaded LINE image bytes in Redis just long
// enough for consumer-llm-processor to pick them up. NATS caps message size
// well below typical photo sizes, so images travel via a Redis handoff:
// the webhook writes bytes keyed by the LINE message ID, and the event
// published to NATS carries only that key.
package imagecache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix = "chat:image:"
	// genPrefix holds images consumer-llm-processor generated; this service
	// serves them publicly at /images/<id> so LINE can fetch them.
	genPrefix = "chat:genimage:"
)

// Store puts short-lived image blobs in Redis.
type Store struct {
	redis *redis.Client
}

func New(rdb *redis.Client) *Store {
	return &Store{redis: rdb}
}

// Key derives the Redis key for a LINE message ID.
func Key(messageID string) string {
	return keyPrefix + messageID
}

// Put stores the image bytes under the message ID's key with the given TTL.
func (s *Store) Put(ctx context.Context, messageID string, data []byte, ttl time.Duration) error {
	return s.redis.Set(ctx, Key(messageID), data, ttl).Err()
}

// GetGenerated reads a generated image without deleting it - LINE fetches
// the same URL more than once (original + preview), so the writer's TTL is
// the cleanup.
func (s *Store) GetGenerated(ctx context.Context, id string) ([]byte, error) {
	return s.redis.Get(ctx, genPrefix+id).Bytes()
}
