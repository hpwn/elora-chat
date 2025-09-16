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

// QueryOpts defines filters for retrieving stored messages.
type QueryOpts struct {
	Limit    int
	Since    *time.Time
	Platform *string
	Username *string
}

// Store describes a backend capable of persisting chat messages.
type Store interface {
	Init(ctx context.Context) error
	InsertMessage(ctx context.Context, m *Message) error
	GetRecent(ctx context.Context, q QueryOpts) ([]Message, error)
	PurgeAll(ctx context.Context) error
	Close(ctx context.Context) error
}
