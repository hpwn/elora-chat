package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
)

func TestSQLiteInsertAndQuery(t *testing.T) {
	store := New(Config{Mode: "ephemeral"})
	ctx := context.Background()

	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(ctx); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	now := time.Now().UTC().Truncate(time.Millisecond)
	msg := &storage.Message{
		ID:         "msg-1",
		Timestamp:  now,
		Username:   "tester",
		Platform:   "twitch",
		Text:       "hello world",
		EmotesJSON: "[]",
		RawJSON:    `{"message":"hello world"}`,
	}

	if err := store.InsertMessage(ctx, msg); err != nil {
		t.Fatalf("InsertMessage returned error: %v", err)
	}

	got, err := store.GetRecent(ctx, storage.QueryOpts{Limit: 10})
	if err != nil {
		t.Fatalf("GetRecent returned error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}

	result := got[0]
	if result.ID != msg.ID {
		t.Fatalf("expected ID %s, got %s", msg.ID, result.ID)
	}
	if result.Username != msg.Username {
		t.Fatalf("expected Username %s, got %s", msg.Username, result.Username)
	}
	if result.Platform != msg.Platform {
		t.Fatalf("expected Platform %s, got %s", msg.Platform, result.Platform)
	}
	if result.Text != msg.Text {
		t.Fatalf("expected Text %s, got %s", msg.Text, result.Text)
	}
	if result.EmotesJSON != msg.EmotesJSON {
		t.Fatalf("expected EmotesJSON %s, got %s", msg.EmotesJSON, result.EmotesJSON)
	}
	if result.RawJSON != msg.RawJSON {
		t.Fatalf("expected RawJSON %s, got %s", msg.RawJSON, result.RawJSON)
	}

	if delta := result.Timestamp.Sub(msg.Timestamp); delta > time.Millisecond || delta < -time.Millisecond {
		t.Fatalf("expected Timestamp near %v, got %v", msg.Timestamp, result.Timestamp)
	}
}

func TestSQLitePurge(t *testing.T) {
	store := New(Config{Mode: "ephemeral"})
	ctx := context.Background()

	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(ctx); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	base := time.Now().UTC().Truncate(time.Millisecond)
	for i := 0; i < 3; i++ {
		msg := &storage.Message{
			ID:         fmt.Sprintf("msg-%d", i),
			Timestamp:  base.Add(time.Duration(i) * time.Second),
			Username:   "tester",
			Platform:   "twitch",
			Text:       fmt.Sprintf("message %d", i),
			EmotesJSON: "[]",
			RawJSON:    fmt.Sprintf(`{"message":"message %d"}`, i),
		}
		if err := store.InsertMessage(ctx, msg); err != nil {
			t.Fatalf("InsertMessage returned error: %v", err)
		}
	}

	if err := store.PurgeAll(ctx); err != nil {
		t.Fatalf("PurgeAll returned error: %v", err)
	}

	got, err := store.GetRecent(ctx, storage.QueryOpts{Limit: 10})
	if err != nil {
		t.Fatalf("GetRecent returned error: %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("expected 0 messages after purge, got %d", len(got))
	}
}
