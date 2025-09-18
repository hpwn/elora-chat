package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

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
		path, ephemeral, dirCreated, err := s.resolvePath()
		if err != nil {
			s.initErr = err
			return
		}
		s.path = path
		s.ephemeral = ephemeral

		if !ephemeral {
			dir := filepath.Dir(path)
			if dirCreated {
				log.Printf("sqlite: persistent directory created: %s", dir)
			} else {
				log.Printf("sqlite: persistent directory already exists: %s", dir)
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

		journalMode, err := s.applyPragmas(ctx, db)
		if err != nil {
			_ = db.Close()
			s.initErr = err
			return
		}

		if err := s.applyMigrations(ctx, db); err != nil {
			_ = db.Close()
			s.initErr = err
			return
		}

		log.Printf("sqlite: opened database (mode=%s, path=%s, journal_mode=%s)", s.storageMode(), path, journalMode)

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

func (s *Store) resolvePath() (path string, ephemeral bool, dirCreated bool, err error) {
	mode := strings.ToLower(strings.TrimSpace(s.cfg.Mode))
	if mode != "persistent" {
		ephemeral = true
	}

	rawPath := strings.TrimSpace(s.cfg.Path)
	if ephemeral {
		if rawPath == "" {
			rawPath = filepath.Join(os.TempDir(), fmt.Sprintf("elora-chat-%d.db", os.Getpid()))
		}
	} else {
		if rawPath == "" {
			return "", false, false, errors.New("sqlite: persistent mode requires ELORA_DB_PATH to be set")
		}
	}

	if rawPath == "" {
		return "", ephemeral, false, errors.New("sqlite: database path is empty")
	}

	path = rawPath
	if abs, absErr := filepath.Abs(path); absErr == nil {
		path = abs
	}

	parent := filepath.Dir(path)
	if parent == "" {
		parent = "."
	}

	if ephemeral {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return "", true, false, fmt.Errorf("sqlite: create temp directory: %w", err)
		}
		return path, true, false, nil
	}

	info, statErr := os.Stat(parent)
	if statErr != nil {
		if !errors.Is(statErr, os.ErrNotExist) {
			return "", false, false, fmt.Errorf("sqlite: stat directory: %w", statErr)
		}
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return "", false, false, fmt.Errorf("sqlite: create directory: %w", err)
		}
		return path, false, true, nil
	}
	if !info.IsDir() {
		return "", false, false, fmt.Errorf("sqlite: path %s is not a directory", parent)
	}
	return path, false, false, nil
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

func (s *Store) applyPragmas(ctx context.Context, db *sql.DB) (string, error) {
	busy := s.cfg.BusyTimeoutMS
	if busy <= 0 {
		busy = 5000
	}

	if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout=%d;", busy)); err != nil {
		return "", fmt.Errorf("sqlite: set busy_timeout: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON;"); err != nil {
		return "", fmt.Errorf("sqlite: enable foreign keys: %w", err)
	}

	var journalMode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL;").Scan(&journalMode); err != nil {
		return "", fmt.Errorf("sqlite: set journal mode: %w", err)
	}

	return journalMode, nil
}

func (s *Store) applyMigrations(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("sqlite: ensure schema_migrations: %w", err)
	}

	applied, err := s.fetchAppliedMigrations(ctx, db)
	if err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("sqlite: list migrations: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}

		version, parseErr := parseMigrationVersion(name)
		if parseErr != nil {
			return parseErr
		}
		if applied[version] {
			continue
		}

		data, readErr := migrationsFS.ReadFile(path.Join("migrations", name))
		if readErr != nil {
			return fmt.Errorf("sqlite: read migration %s: %w", name, readErr)
		}

		if err := s.runMigration(ctx, db, version, string(data), name); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) fetchAppliedMigrations(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("sqlite: scan migration version: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate migrations: %w", err)
	}
	return applied, nil
}

func parseMigrationVersion(name string) (int, error) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	parts := strings.SplitN(base, "_", 2)
	if len(parts) == 0 || parts[0] == "" {
		return 0, fmt.Errorf("sqlite: invalid migration filename: %s", name)
	}
	version, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("sqlite: invalid migration version in %s: %w", name, err)
	}
	return version, nil
}

func (s *Store) runMigration(ctx context.Context, db *sql.DB, version int, sqlText, name string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, sqlText); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("sqlite: apply migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(?)`, version); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("sqlite: record migration %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit migration %s: %w", name, err)
	}
	log.Printf("sqlite: applied migration %s (version=%d)", name, version)
	return nil
}
