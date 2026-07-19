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
	StepManage       = "manage"        // browsing the upcoming-reminders list
)

// State is one user's reminder conversation, kept in Redis under
// chat:reminder_flow:<uid> (line-webhook checks that key's existence to
// route mid-flow text here instead of the AI).
type State struct {
	Step         string    `json:"step"`
	TargetUserID string    `json:"target_user_id,omitempty"`
	Message      string    `json:"message,omitempty"`
	RemindAt     time.Time `json:"remind_at,omitempty"`
	// EditingID, when non-zero, makes confirm UPDATE this reminder instead
	// of INSERTing a new one.
	EditingID int64 `json:"editing_id,omitempty"`
}

// StateStore persists flow state. line-webhook and consumer-llm-processor
// both check the flow key's existence to route the user's free text here
// while the flow is alive; the AI session key is never touched, so finishing
// a reminder cannot leave the user stuck in AI mode.
type StateStore struct {
	redis *redis.Client
	ttl   time.Duration
}

func NewStateStore(rdb *redis.Client, ttl time.Duration) *StateStore {
	return &StateStore{redis: rdb, ttl: ttl}
}

func stateKey(userID string) string { return fmt.Sprintf("chat:reminder_flow:%s", userID) }

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

// Put saves the state and slides the flow TTL forward.
func (s *StateStore) Put(ctx context.Context, userID string, state *State) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.redis.Set(ctx, stateKey(userID), data, s.ttl).Err()
}

// Delete ends the flow. Any AI session the user had before starting the
// reminder is untouched and continues.
func (s *StateStore) Delete(ctx context.Context, userID string) error {
	return s.redis.Del(ctx, stateKey(userID)).Err()
}
