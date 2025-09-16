package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const initMigration = "migrations/0001_init.sql"

// Config controls how the SQLite store is configured.
type Config struct {
	Mode            string
	Path            string
	MaxConns        int
	BusyTimeoutMS   int
	PragmasExtraCSV string
}

// Store implements storage.Store backed by SQLite.
type Store struct {
	cfg       Config
	db        *sql.DB
	path      string
	ephemeral bool
	initOnce  sync.Once
	initErr   error
}

// New creates a new SQLite store with the provided configuration.
func New(cfg Config) *Store {
	return &Store{cfg: cfg}
}

// Init opens the database connection, applies pragmas, and runs migrations.
func (s *Store) Init(ctx context.Context) error {
	s.initOnce.Do(func() {
		path, ephemeral := s.resolvePath()
		s.path = path
		s.ephemeral = ephemeral

		if !ephemeral {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				s.initErr = fmt.Errorf("sqlite: create directory: %w", err)
				return
			}
		}

		dsn := s.buildDSN(path)
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			s.initErr = fmt.Errorf("sqlite: open: %w", err)
			return
		}

		if s.cfg.MaxConns > 0 {
			db.SetMaxOpenConns(s.cfg.MaxConns)
			db.SetMaxIdleConns(s.cfg.MaxConns)
		}

		if err := db.PingContext(ctx); err != nil {
			_ = db.Close()
			s.initErr = fmt.Errorf("sqlite: ping: %w", err)
			return
		}

		if err := s.applyMigrations(ctx, db); err != nil {
			_ = db.Close()
			s.initErr = err
			return
		}

		journalMode := ""
		if err := db.QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&journalMode); err != nil {
			log.Printf("sqlite: failed to read journal_mode pragma: %v", err)
		} else {
			log.Printf("sqlite: opened database (mode=%s, path=%s, journal_mode=%s)", s.storageMode(), path, journalMode)
		}

		s.db = db
	})

	return s.initErr
}

// InsertMessage writes a chat message to the SQLite database.
func (s *Store) InsertMessage(ctx context.Context, m *storage.Message) error {
	if s.db == nil {
		return errors.New("sqlite: store not initialized")
	}
	if m == nil {
		return errors.New("sqlite: message is nil")
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (id, ts, username, platform, text, emotes_json, raw_json) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.ID,
		m.Timestamp.UTC().UnixMilli(),
		m.Username,
		m.Platform,
		m.Text,
		m.EmotesJSON,
		m.RawJSON,
	)
	if err != nil {
		return fmt.Errorf("sqlite: insert message: %w", err)
	}
	return nil
}

// GetRecent returns the most recent chat messages subject to the provided filters.
func (s *Store) GetRecent(ctx context.Context, q storage.QueryOpts) ([]storage.Message, error) {
	if s.db == nil {
		return nil, errors.New("sqlite: store not initialized")
	}

	query := `SELECT id, ts, username, platform, text, emotes_json, COALESCE(raw_json, '') FROM messages`
	var conditions []string
	var args []any

	if q.Since != nil {
		conditions = append(conditions, "ts >= ?")
		args = append(args, q.Since.UTC().UnixMilli())
	}
	if q.Platform != nil {
		conditions = append(conditions, "platform = ?")
		args = append(args, *q.Platform)
	}
	if q.Username != nil {
		conditions = append(conditions, "username = ?")
		args = append(args, *q.Username)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY ts DESC"
	if q.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, q.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: query recent messages: %w", err)
	}
	defer rows.Close()

	var results []storage.Message
	for rows.Next() {
		var (
			msg storage.Message
			ts  int64
		)
		if err := rows.Scan(&msg.ID, &ts, &msg.Username, &msg.Platform, &msg.Text, &msg.EmotesJSON, &msg.RawJSON); err != nil {
			return nil, fmt.Errorf("sqlite: scan message: %w", err)
		}
		msg.Timestamp = time.UnixMilli(ts).UTC()
		results = append(results, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate messages: %w", err)
	}

	return results, nil
}

// PurgeAll removes all stored chat messages and compacts the database.
func (s *Store) PurgeAll(ctx context.Context) error {
	if s.db == nil {
		return errors.New("sqlite: store not initialized")
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM messages`); err != nil {
		return fmt.Errorf("sqlite: purge messages: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("sqlite: vacuum: %w", err)
	}
	return nil
}

// Close terminates the database connection and cleans up any ephemeral files.
func (s *Store) Close(ctx context.Context) error {
	var errs []error
	if s.db != nil {
		if err := s.db.Close(); err != nil {
			errs = append(errs, err)
		}
		s.db = nil
	}
	if s.ephemeral && s.path != "" {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove temp file: %w", err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (s *Store) resolvePath() (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(s.cfg.Mode))
	if mode == "" || mode == "ephemeral" {
		return filepath.Join(os.TempDir(), fmt.Sprintf("elora-chat-%d.db", os.Getpid())), true
	}

	path := s.cfg.Path
	if path == "" {
		path = "./data/elora-chat.db"
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	return path, false
}

func (s *Store) storageMode() string {
	if s.ephemeral {
		return "ephemeral"
	}
	return "persistent"
}

func (s *Store) buildDSN(path string) string {
	busy := s.cfg.BusyTimeoutMS
	if busy <= 0 {
		busy = 5000
	}
	pragmas := []string{
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
		"_pragma=foreign_keys(ON)",
		fmt.Sprintf("_pragma=busy_timeout(%d)", busy),
	}
	pragmas = append(pragmas, s.parseExtraPragmas()...)

	query := strings.Join(pragmas, "&")
	escapedPath := url.PathEscape(path)
	return fmt.Sprintf("file:%s?cache=shared&mode=rwc&%s", escapedPath, query)
}

func (s *Store) parseExtraPragmas() []string {
	extra := strings.TrimSpace(s.cfg.PragmasExtraCSV)
	if extra == "" {
		return nil
	}
	parts := strings.Split(extra, ",")
	pragmas := make([]string, 0, len(parts))
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if strings.Contains(p, "(") {
			pragmas = append(pragmas, "_pragma="+p)
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		if len(kv) == 2 {
			key := strings.TrimSpace(kv[0])
			value := strings.TrimSpace(kv[1])
			if key != "" && value != "" {
				pragmas = append(pragmas, fmt.Sprintf("_pragma=%s(%s)", key, value))
				continue
			}
		}
		pragmas = append(pragmas, "_pragma="+p)
	}
	return pragmas
}

func (s *Store) applyMigrations(ctx context.Context, db *sql.DB) error {
	data, err := migrationsFS.ReadFile(initMigration)
	if err != nil {
		return fmt.Errorf("sqlite: read migration: %w", err)
	}
	if _, err := db.ExecContext(ctx, string(data)); err != nil {
		return fmt.Errorf("sqlite: apply migration: %w", err)
	}
	return nil
}
