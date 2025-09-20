package storage

import (
	"context"
	"time"
)

// Message represents a chat message stored in the backing store.
type Message struct {
	ID         string
	Timestamp  time.Time
	Username   string
	Platform   string
	Text       string
	EmotesJSON string
	RawJSON    string
}

// Session represents auth/session state persisted by the backend.
type Session struct {
	Token       string
	Service     string
	DataJSON    string
	TokenExpiry time.Time
	UpdatedAt   time.Time
}

// QueryOpts defines filters for retrieving stored messages.
type QueryOpts struct {
	Limit   int
	SinceTS *time.Time
}

// Store describes a backend capable of persisting chat messages and sessions.
type Store interface {
	Init(ctx context.Context) error
	InsertMessage(ctx context.Context, m *Message) error
	GetRecent(ctx context.Context, q QueryOpts) ([]Message, error)
	PurgeAll(ctx context.Context) error
	GetSession(ctx context.Context, token string) (*Session, error)
	UpsertSession(ctx context.Context, s *Session) error
	DeleteSession(ctx context.Context, token string) error
	Close(ctx context.Context) error
}
