package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
)

func TestSQLiteInsertAndQuery(t *testing.T) {
	store := New(Config{})
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
	store := New(Config{})
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

func TestSQLiteEphemeralDefaults(t *testing.T) {
	store := New(Config{})
	ctx := context.Background()

	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(ctx); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	if !store.ephemeral {
		t.Fatalf("expected store to be ephemeral")
	}

	expectedPath := filepath.Join(os.TempDir(), fmt.Sprintf("elora-chat-%d.db", os.Getpid()))
	if store.path != expectedPath {
		t.Fatalf("expected derived path %s, got %s", expectedPath, store.path)
	}

	var journalMode string
	if err := store.db.QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&journalMode); err != nil {
		t.Fatalf("failed to read journal_mode: %v", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Fatalf("expected journal mode WAL, got %s", journalMode)
	}

	var busy int
	if err := store.db.QueryRowContext(ctx, "PRAGMA busy_timeout;").Scan(&busy); err != nil {
		t.Fatalf("failed to read busy_timeout: %v", err)
	}
	if busy != 5000 {
		t.Fatalf("expected busy_timeout 5000, got %d", busy)
	}

	var foreignKeys int
	if err := store.db.QueryRowContext(ctx, "PRAGMA foreign_keys;").Scan(&foreignKeys); err != nil {
		t.Fatalf("failed to read foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("expected foreign_keys ON, got %d", foreignKeys)
	}
}

func TestSQLitePersistentMode(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	dbPath := filepath.Join(baseDir, "data", "elora.db")

	store := New(Config{Mode: "persistent", Path: dbPath})
	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(ctx); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	if store.ephemeral {
		t.Fatalf("expected persistent store")
	}

	expectedPath, err := filepath.Abs(dbPath)
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}
	if store.path != expectedPath {
		t.Fatalf("expected path %s, got %s", expectedPath, store.path)
	}

	if _, err := os.Stat(filepath.Dir(store.path)); err != nil {
		t.Fatalf("expected parent directory to exist: %v", err)
	}

	var journalMode string
	if err := store.db.QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&journalMode); err != nil {
		t.Fatalf("failed to read journal_mode: %v", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Fatalf("expected journal mode WAL, got %s", journalMode)
	}
}

func TestSQLiteMigrationsIdempotent(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "migrations.db")

	first := New(Config{Mode: "persistent", Path: dbPath})
	if err := first.Init(ctx); err != nil {
		t.Fatalf("first Init returned error: %v", err)
	}
	if err := first.Close(ctx); err != nil {
		t.Fatalf("first Close returned error: %v", err)
	}

	second := New(Config{Mode: "persistent", Path: dbPath})
	if err := second.Init(ctx); err != nil {
		t.Fatalf("second Init returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := second.Close(ctx); err != nil {
			t.Fatalf("second Close returned error: %v", err)
		}
	})

	var tableCount int
	if err := second.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='messages'`).Scan(&tableCount); err != nil {
		t.Fatalf("failed to query sqlite_master: %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("expected messages table to exist, got count %d", tableCount)
	}

	var applied int
	if err := second.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("failed to count schema_migrations: %v", err)
	}
	if applied == 0 {
		t.Fatalf("expected schema_migrations to record applied migrations")
	}
}
