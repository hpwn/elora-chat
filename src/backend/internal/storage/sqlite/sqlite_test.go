package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
)

func TestSQLiteGetRecentEphemeral(t *testing.T) {
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
	const total = 5
	want := make([]*storage.Message, 0, total)
	for i := 0; i < total; i++ {
		msg := &storage.Message{
			ID:         fmt.Sprintf("msg-%d", i),
			Timestamp:  base.Add(time.Duration(i) * time.Second),
			Username:   "tester",
			Platform:   "twitch",
			Text:       fmt.Sprintf("hello world %d", i),
			EmotesJSON: "[]",
			RawJSON:    fmt.Sprintf(`{"message":"hello world %d"}`, i),
		}
		want = append(want, msg)
		if err := store.InsertMessage(ctx, msg); err != nil {
			t.Fatalf("InsertMessage %d returned error: %v", i, err)
		}
	}

	got, err := store.GetRecent(ctx, storage.QueryOpts{Limit: total})
	if err != nil {
		t.Fatalf("GetRecent returned error: %v", err)
	}

	if len(got) != total {
		t.Fatalf("expected %d messages, got %d", total, len(got))
	}

	for i := 0; i < total; i++ {
		expected := want[total-1-i]
		result := got[i]
		if result.ID != expected.ID {
			t.Fatalf("result[%d] expected ID %s, got %s", i, expected.ID, result.ID)
		}
		if result.Username != expected.Username {
			t.Fatalf("result[%d] expected Username %s, got %s", i, expected.Username, result.Username)
		}
		if result.Platform != expected.Platform {
			t.Fatalf("result[%d] expected Platform %s, got %s", i, expected.Platform, result.Platform)
		}
		if result.Text != expected.Text {
			t.Fatalf("result[%d] expected Text %s, got %s", i, expected.Text, result.Text)
		}
		if result.EmotesJSON != expected.EmotesJSON {
			t.Fatalf("result[%d] expected EmotesJSON %s, got %s", i, expected.EmotesJSON, result.EmotesJSON)
		}
		if result.RawJSON != expected.RawJSON {
			t.Fatalf("result[%d] expected RawJSON %s, got %s", i, expected.RawJSON, result.RawJSON)
		}
		if delta := result.Timestamp.Sub(expected.Timestamp); delta > time.Millisecond || delta < -time.Millisecond {
			t.Fatalf("result[%d] expected Timestamp near %v, got %v", i, expected.Timestamp, result.Timestamp)
		}
	}
}

func TestSQLiteGetRecentBeforeTS(t *testing.T) {
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
	msgs := make([]*storage.Message, 0, 5)
	for i := 0; i < 5; i++ {
		msg := &storage.Message{
			ID:        fmt.Sprintf("msg-%d", i),
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Username:  "tester",
			Platform:  "twitch",
			Text:      fmt.Sprintf("body-%d", i),
		}
		msgs = append(msgs, msg)
		if err := store.InsertMessage(ctx, msg); err != nil {
			t.Fatalf("InsertMessage %d returned error: %v", i, err)
		}
	}

	before := msgs[len(msgs)-1].Timestamp
	got, err := store.GetRecent(ctx, storage.QueryOpts{Limit: 2, BeforeTS: &before})
	if err != nil {
		t.Fatalf("GetRecent returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	if got[0].ID != "msg-3" || got[1].ID != "msg-2" {
		t.Fatalf("unexpected IDs: %#v", got)
	}
}

func TestSQLiteGetRecentStableOrdering(t *testing.T) {
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

	ts := time.Now().UTC().Truncate(time.Millisecond)
	ids := []string{"msg-a", "msg-b", "msg-c"}
	for _, id := range ids {
		msg := &storage.Message{ID: id, Timestamp: ts, Username: "tester", Platform: "twitch", Text: id}
		if err := store.InsertMessage(ctx, msg); err != nil {
			t.Fatalf("InsertMessage returned error: %v", err)
		}
	}

	got, err := store.GetRecent(ctx, storage.QueryOpts{Limit: len(ids)})
	if err != nil {
		t.Fatalf("GetRecent returned error: %v", err)
	}
	wantOrder := []string{"msg-c", "msg-b", "msg-a"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Fatalf("expected ID %q at index %d, got %q", want, i, got[i].ID)
		}
	}
}

func TestSQLiteGetRecentConflictingCursors(t *testing.T) {
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

	now := time.Now().UTC()
	opts := storage.QueryOpts{Limit: 1, SinceTS: &now, BeforeTS: &now}
	if _, err := store.GetRecent(ctx, opts); err == nil {
		t.Fatalf("expected error when both since_ts and before_ts set")
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

func TestSQLitePurgeBefore(t *testing.T) {
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
	msgs := make([]*storage.Message, 0, 5)
	for i := 0; i < 5; i++ {
		msg := &storage.Message{
			ID:         fmt.Sprintf("purge-before-%d", i),
			Timestamp:  base.Add(time.Duration(i) * time.Second),
			Username:   "tester",
			Platform:   "twitch",
			Text:       fmt.Sprintf("message %d", i),
			EmotesJSON: "[]",
			RawJSON:    fmt.Sprintf(`{"message":"message %d"}`, i),
		}
		msgs = append(msgs, msg)
		if err := store.InsertMessage(ctx, msg); err != nil {
			t.Fatalf("InsertMessage returned error: %v", err)
		}
	}

	cutoff := msgs[3].Timestamp
	deleted, err := store.PurgeBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("PurgeBefore returned error: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("expected 3 deleted rows, got %d", deleted)
	}

	got, err := store.GetRecent(ctx, storage.QueryOpts{Limit: 10})
	if err != nil {
		t.Fatalf("GetRecent returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 remaining messages, got %d", len(got))
	}
	if got[0].ID != msgs[4].ID || got[1].ID != msgs[3].ID {
		t.Fatalf("unexpected remaining IDs: %v", []string{got[0].ID, got[1].ID})
	}
}

func TestSQLiteGetRecentPersistent(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "chat.db")

	cfg := Config{Mode: "persistent", Path: dbPath}
	store := New(cfg)
	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	base := time.Now().UTC().Truncate(time.Millisecond)
	const total = 4
	want := make([]*storage.Message, 0, total)
	for i := 0; i < total; i++ {
		msg := &storage.Message{
			ID:         fmt.Sprintf("persist-%d", i),
			Timestamp:  base.Add(time.Duration(i) * time.Minute),
			Username:   "persistent-user",
			Platform:   "youtube",
			Text:       fmt.Sprintf("persistent message %d", i),
			EmotesJSON: "[]",
			RawJSON:    fmt.Sprintf(`{"message":"persistent message %d"}`, i),
		}
		want = append(want, msg)
		if err := store.InsertMessage(ctx, msg); err != nil {
			t.Fatalf("InsertMessage %d returned error: %v", i, err)
		}
	}

	if err := store.Close(ctx); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	reopened := New(cfg)
	if err := reopened.Init(ctx); err != nil {
		t.Fatalf("reopen Init returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(ctx); err != nil {
			t.Fatalf("reopen Close returned error: %v", err)
		}
	})

	got, err := reopened.GetRecent(ctx, storage.QueryOpts{Limit: total})
	if err != nil {
		t.Fatalf("GetRecent returned error: %v", err)
	}

	if len(got) != total {
		t.Fatalf("expected %d messages, got %d", total, len(got))
	}

	for i := 0; i < total; i++ {
		expected := want[total-1-i]
		result := got[i]
		if result.ID != expected.ID {
			t.Fatalf("result[%d] expected ID %s, got %s", i, expected.ID, result.ID)
		}
		if result.Username != expected.Username {
			t.Fatalf("result[%d] expected Username %s, got %s", i, expected.Username, result.Username)
		}
		if result.Platform != expected.Platform {
			t.Fatalf("result[%d] expected Platform %s, got %s", i, expected.Platform, result.Platform)
		}
		if result.Text != expected.Text {
			t.Fatalf("result[%d] expected Text %s, got %s", i, expected.Text, result.Text)
		}
		if result.EmotesJSON != expected.EmotesJSON {
			t.Fatalf("result[%d] expected EmotesJSON %s, got %s", i, expected.EmotesJSON, result.EmotesJSON)
		}
		if result.RawJSON != expected.RawJSON {
			t.Fatalf("result[%d] expected RawJSON %s, got %s", i, expected.RawJSON, result.RawJSON)
		}
		if delta := result.Timestamp.Sub(expected.Timestamp); delta > time.Millisecond || delta < -time.Millisecond {
			t.Fatalf("result[%d] expected Timestamp near %v, got %v", i, expected.Timestamp, result.Timestamp)
		}
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

func TestSessionsEphemeral(t *testing.T) {
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

	now := time.Now().UTC().Truncate(time.Second)
	sess := &storage.Session{
		Token:       "tok-ephemeral",
		Service:     "twitch",
		DataJSON:    `{"hello":"world"}`,
		TokenExpiry: now.Add(30 * time.Minute),
		UpdatedAt:   now,
	}

	if err := store.UpsertSession(ctx, sess); err != nil {
		t.Fatalf("UpsertSession returned error: %v", err)
	}

	got, err := store.GetSession(ctx, "tok-ephemeral")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}

	if got.Token != sess.Token || got.Service != sess.Service || got.DataJSON != sess.DataJSON {
		t.Fatalf("unexpected session data: %#v", got)
	}
	if !got.TokenExpiry.Equal(sess.TokenExpiry) {
		t.Fatalf("expected TokenExpiry %v, got %v", sess.TokenExpiry, got.TokenExpiry)
	}
	if !got.UpdatedAt.Equal(sess.UpdatedAt) {
		t.Fatalf("expected UpdatedAt %v, got %v", sess.UpdatedAt, got.UpdatedAt)
	}

	if err := store.DeleteSession(ctx, "tok-ephemeral"); err != nil {
		t.Fatalf("DeleteSession returned error: %v", err)
	}

	if _, err := store.GetSession(ctx, "tok-ephemeral"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows after delete, got %v", err)
	}
}

func TestSessionsPersistent(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "sessions.db")

	cfg := Config{Mode: "persistent", Path: dbPath}
	store := New(cfg)
	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	input := &storage.Session{
		Token:       "tok-persistent",
		Service:     "youtube",
		DataJSON:    `{"foo":"bar"}`,
		TokenExpiry: now.Add(time.Hour),
		UpdatedAt:   now,
	}
	if err := store.UpsertSession(ctx, input); err != nil {
		t.Fatalf("UpsertSession returned error: %v", err)
	}
	if err := store.Close(ctx); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	reopened := New(cfg)
	if err := reopened.Init(ctx); err != nil {
		t.Fatalf("reopen Init returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(ctx); err != nil {
			t.Fatalf("reopen Close returned error: %v", err)
		}
	})

	got, err := reopened.GetSession(ctx, "tok-persistent")
	if err != nil {
		t.Fatalf("GetSession after reopen returned error: %v", err)
	}

	if got.Token != input.Token || got.Service != input.Service || got.DataJSON != input.DataJSON {
		t.Fatalf("unexpected persisted session: %#v", got)
	}
	if !got.TokenExpiry.Equal(input.TokenExpiry) {
		t.Fatalf("expected TokenExpiry %v, got %v", input.TokenExpiry, got.TokenExpiry)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatalf("expected UpdatedAt to be set")
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
