// Package imagecache moves image bytes through Redis, matching
// services/line-webhook/internal/imagecache's key scheme. Image bytes never
// ride on NATS - the payload would blow past NATS's default message size -
// so Redis is the handoff in both directions: line-webhook stashes incoming
// LINE images for this service to consume, and this service stashes
// generated images for line-webhook to serve publicly.
package imagecache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix = "chat:image:"
	genPrefix = "chat:genimage:"
)

// Store reads and writes short-lived image blobs in Redis.
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

// PutGenerated stashes a generated image for line-webhook's public
// /images/<id> endpoint. Not deleted on read - LINE fetches the URL more
// than once (original + preview) - so the TTL is the cleanup.
func (s *Store) PutGenerated(ctx context.Context, id string, data []byte, ttl time.Duration) error {
	return s.redis.Set(ctx, genPrefix+id, data, ttl).Err()
}
