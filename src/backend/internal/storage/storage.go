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
	BadgesJSON string
	RawJSON    string
}

// TailPosition represents a cursor for iterating over messages in timestamp/rowid order.
type TailPosition struct {
	TS    int64
	RowID int64
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
	Limit    int
	SinceTS  *time.Time // return messages with Timestamp >= SinceTS (newer-than)
	BeforeTS *time.Time // return messages with Timestamp < BeforeTS (older-than)
}

// Store describes a backend capable of persisting chat messages and sessions.
type Store interface {
	Init(ctx context.Context) error
	InsertMessage(ctx context.Context, m *Message) error
	GetRecent(ctx context.Context, q QueryOpts) ([]Message, error)
	// PurgeBefore deletes messages with timestamps strictly before the cutoff.
	PurgeBefore(ctx context.Context, cutoff time.Time) (int, error)
	PurgeAll(ctx context.Context) error
	GetSession(ctx context.Context, token string) (*Session, error)
	UpsertSession(ctx context.Context, s *Session) error
	DeleteSession(ctx context.Context, token string) error
	// LatestSessionByService returns the most recently updated session for the
	// given service. If none exist, (nil, nil) is returned.
	LatestSessionByService(ctx context.Context, service string) (*Session, error)
	// LatestSession returns the most recently updated session regardless of
	// service. If no sessions exist, (nil, nil) is returned.
	LatestSession(ctx context.Context) (*Session, error)
	Close(ctx context.Context) error
}
