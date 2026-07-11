package store

import "context"

// Role values follow the Gemini API convention.
const (
	RoleUser  = "user"
	RoleModel = "model"
)

// Message is a single turn in a user's conversation history.
type Message struct {
	Role    string
	Content string
}

// Store keeps per-user conversation history so the AI has context.
type Store interface {
	// GetRecent returns the most recent messages for a user, oldest first.
	GetRecent(ctx context.Context, userID string) ([]Message, error)
	// Append records one turn of the conversation.
	Append(ctx context.Context, userID, role, content string) error
	// Clear deletes all history for a user.
	Clear(ctx context.Context, userID string) error
	Close()
}
