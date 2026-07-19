package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Steps of the reminder conversation.
const (
	StepAwaitTarget  = "await_target"  // waiting for "myself / someone else"
	StepAwaitUser    = "await_user"    // waiting for a target-user pick
	StepAwaitDetails = "await_details" // waiting for free text with message+time
	StepAwaitConfirm = "await_confirm" // waiting for confirm/edit/cancel
)

// State is one user's reminder conversation, kept in Redis under
// chat:reminder_flow:<uid> (line-webhook checks that key's existence to
// route mid-flow text here instead of the AI).
type State struct {
	Step         string    `json:"step"`
	TargetUserID string    `json:"target_user_id,omitempty"`
	Message      string    `json:"message,omitempty"`
	RemindAt     time.Time `json:"remind_at,omitempty"`
}

// StateStore persists flow state. Every write also refreshes the shared AI
// session key so line-webhook keeps forwarding the user's free text while
// the flow is alive (both keys share the same TTL and can't desync).
type StateStore struct {
	redis *redis.Client
	ttl   time.Duration
}

func NewStateStore(rdb *redis.Client, ttl time.Duration) *StateStore {
	return &StateStore{redis: rdb, ttl: ttl}
}

func stateKey(userID string) string   { return fmt.Sprintf("chat:reminder_flow:%s", userID) }
func sessionKey(userID string) string { return fmt.Sprintf("chat:ai_session:%s", userID) }

// Get returns the user's flow state, or nil if none.
func (s *StateStore) Get(ctx context.Context, userID string) (*State, error) {
	data, err := s.redis.Get(ctx, stateKey(userID)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// Put saves the state and slides both the flow and AI-session TTLs forward.
func (s *StateStore) Put(ctx context.Context, userID string, state *State) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := s.redis.Set(ctx, stateKey(userID), data, s.ttl).Err(); err != nil {
		return err
	}
	return s.redis.Set(ctx, sessionKey(userID), "1", s.ttl).Err()
}

// Delete ends the flow. The AI session key is left alone: if the user was
// mid /ai conversation before starting the reminder, that continues.
func (s *StateStore) Delete(ctx context.Context, userID string) error {
	return s.redis.Del(ctx, stateKey(userID)).Err()
}
