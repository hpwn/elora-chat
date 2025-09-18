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

func TestSQLiteEphemeralDefaults(t *testing.T) {
	ctx := context.Background()
	store := New(Config{Mode: "ephemeral"})

	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(ctx); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	if store.path == "" {
		t.Fatal("expected path to be set")
	}
	tempPrefix := filepath.Join(filepath.Clean(os.TempDir()), "elora-chat-")
	if !strings.HasPrefix(store.path, tempPrefix) {
		t.Fatalf("expected ephemeral path to live under %s, got %s", tempPrefix, store.path)
	}

	if _, err := os.Stat(store.path); err != nil {
		t.Fatalf("expected database file to exist, stat returned error: %v", err)
	}

	mode := queryJournalMode(t, ctx, store)
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("expected journal_mode WAL, got %s", mode)
	}
}

func TestSQLitePersistentCreatesDirAndSetsWAL(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "nested", "elora.db")

	store := New(Config{Mode: "persistent", Path: dbPath})
	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(ctx); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	wantDir := filepath.Dir(dbPath)
	info, err := os.Stat(wantDir)
	if err != nil {
		t.Fatalf("expected parent directory to exist, stat returned error: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %s to be a directory", wantDir)
	}

	wantAbs, _ := filepath.Abs(dbPath)
	if store.path != wantAbs {
		t.Fatalf("expected absolute path %s, got %s", wantAbs, store.path)
	}

	mode := queryJournalMode(t, ctx, store)
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("expected journal_mode WAL, got %s", mode)
	}
}

func TestSQLiteMigrationsRunOnce(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "migrations.db")

	first := New(Config{Mode: "persistent", Path: dbPath})
	if err := first.Init(ctx); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	assertMigrationState(t, ctx, first, 1)
	assertTableExists(t, ctx, first, "messages")

	if err := first.Close(ctx); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	second := New(Config{Mode: "persistent", Path: dbPath})
	if err := second.Init(ctx); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := second.Close(ctx); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	assertMigrationState(t, ctx, second, 1)
	assertTableExists(t, ctx, second, "messages")
}

func queryJournalMode(t *testing.T, ctx context.Context, store *Store) string {
	t.Helper()
	if store.db == nil {
		t.Fatal("store database is nil")
	}
	var mode string
	if err := store.db.QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&mode); err != nil {
		t.Fatalf("failed to query journal_mode: %v", err)
	}
	return mode
}

func assertMigrationState(t *testing.T, ctx context.Context, store *Store, expectedCount int) {
	t.Helper()
	if store.db == nil {
		t.Fatal("store database is nil")
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = 1`).Scan(&count); err != nil {
		t.Fatalf("failed to read schema_migrations: %v", err)
	}
	if count != expectedCount {
		t.Fatalf("expected %d applied migrations, got %d", expectedCount, count)
	}
}

func assertTableExists(t *testing.T, ctx context.Context, store *Store, table string) {
	t.Helper()
	if store.db == nil {
		t.Fatal("store database is nil")
	}
	var name string
	query := `SELECT name FROM sqlite_master WHERE type='table' AND name = ?`
	if err := store.db.QueryRowContext(ctx, query, table).Scan(&name); err != nil {
		t.Fatalf("expected table %s to exist: %v", table, err)
	}
}
