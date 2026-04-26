// Package session manages request or client-session state, isolating mutable
// state such as DeepSeek V4 thinking caches across unrelated requests.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	deepseekv4 "moonbridge/internal/extensions/deepseek_v4"
)

// Session holds mutable state that should be isolated across unrelated
// conversations (e.g., thinking blocks from one conversation leaking into
// another).
type Session struct {
	ID        string
	DeepSeek  *deepseekv4.State
	CreatedAt time.Time
}

// New creates a new Session with a unique ID and initialised state.
func New() *Session {
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	s := &Session{
		ID:        hex.EncodeToString(id),
		CreatedAt: time.Now(),
	}
	s.DeepSeek = deepseekv4.NewState()
	return s
}

// NewWithID creates a Session with the given ID (useful for testing).
func NewWithID(id string) *Session {
	s := New()
	s.ID = id
	return s
}

// ContextKey is the key used to store/retrieve a Session from a context.Context.
type ContextKey struct{}

// String returns the context key name for debugging.
func (k ContextKey) String() string {
	return "moonbridge-session"
}
