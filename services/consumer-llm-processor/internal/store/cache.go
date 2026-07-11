package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// CachedStore serves history from a shared Redis cache first and falls back
// to the underlying store (Postgres) on a miss (cache-aside). Writes go
// through to the underlying store so history survives restarts, and the
// cached copy is updated so every consumer replica sees the same history.
// Redis failures degrade to the backend instead of failing the request.
type CachedStore struct {
	backend Store
	redis   *redis.Client
	ttl     time.Duration
	limit   int
}

// NewCached wraps backend with a Redis cache. limit caps the messages kept
// per user; ttl controls how long a cached conversation stays fresh.
func NewCached(backend Store, rdb *redis.Client, limit int, ttl time.Duration) *CachedStore {
	return &CachedStore{backend: backend, redis: rdb, ttl: ttl, limit: limit}
}

func cacheKey(userID string) string {
	return fmt.Sprintf("chat:history:%s", userID)
}

func (c *CachedStore) GetRecent(ctx context.Context, userID string) ([]Message, error) {
	data, err := c.redis.Get(ctx, cacheKey(userID)).Bytes()
	if err == nil {
		var messages []Message
		if err := json.Unmarshal(data, &messages); err == nil {
			return messages, nil
		}
		log.Printf("Corrupt cache entry for %s, reloading from backend", userID)
	} else if err != redis.Nil {
		log.Printf("Redis get failed for %s, falling back to backend: %v", userID, err)
	}

	messages, err := c.backend.GetRecent(ctx, userID)
	if err != nil {
		return nil, err
	}
	c.setCache(ctx, userID, messages)
	return messages, nil
}

func (c *CachedStore) Append(ctx context.Context, userID, role, content string) error {
	if err := c.backend.Append(ctx, userID, role, content); err != nil {
		return err
	}

	// Write-through: update the cached copy if present; on any Redis issue
	// just drop the key so the next read reloads from the backend.
	key := cacheKey(userID)
	data, err := c.redis.Get(ctx, key).Bytes()
	if err != nil {
		if err != redis.Nil {
			log.Printf("Redis get failed for %s during append: %v", userID, err)
		}
		return nil
	}
	var messages []Message
	if err := json.Unmarshal(data, &messages); err != nil {
		c.redis.Del(ctx, key)
		return nil
	}
	messages = append(messages, Message{Role: role, Content: content})
	if len(messages) > c.limit {
		messages = messages[len(messages)-c.limit:]
	}
	c.setCache(ctx, userID, messages)
	return nil
}

func (c *CachedStore) Clear(ctx context.Context, userID string) error {
	if err := c.backend.Clear(ctx, userID); err != nil {
		return err
	}
	if err := c.redis.Del(ctx, cacheKey(userID)).Err(); err != nil {
		log.Printf("Redis del failed for %s: %v", userID, err)
	}
	return nil
}

func (c *CachedStore) Close() {
	if err := c.redis.Close(); err != nil {
		log.Printf("Redis close failed: %v", err)
	}
	c.backend.Close()
}

func (c *CachedStore) setCache(ctx context.Context, userID string, messages []Message) {
	data, err := json.Marshal(messages)
	if err != nil {
		return
	}
	if err := c.redis.Set(ctx, cacheKey(userID), data, c.ttl).Err(); err != nil {
		log.Printf("Redis set failed for %s: %v", userID, err)
	}
}
