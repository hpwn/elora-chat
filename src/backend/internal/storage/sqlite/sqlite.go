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
		path, ephemeral, err := s.resolvePath()
		if err != nil {
			s.initErr = err
			return
		}

		s.path = path
		s.ephemeral = ephemeral

		dir := filepath.Dir(path)
		var dirCreated bool
		if ephemeral {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				s.initErr = fmt.Errorf("sqlite: create directory: %w", err)
				return
			}
		} else {
			if _, err := os.Stat(dir); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						s.initErr = fmt.Errorf("sqlite: create directory: %w", err)
						return
					}
					dirCreated = true
				} else {
					s.initErr = fmt.Errorf("sqlite: inspect directory: %w", err)
					return
				}
			} else {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					s.initErr = fmt.Errorf("sqlite: ensure directory: %w", err)
					return
				}
			}

			if dirCreated {
				log.Printf("sqlite: persistent parent directory created: %s", dir)
			} else {
				log.Printf("sqlite: persistent parent directory already existed: %s", dir)
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

		journalModeRow := db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL;")
		var journalMode string
		if err := journalModeRow.Scan(&journalMode); err != nil {
			_ = db.Close()
			s.initErr = fmt.Errorf("sqlite: set journal_mode: %w", err)
			return
		}

		busy := s.cfg.BusyTimeoutMS
		if busy <= 0 {
			busy = 5000
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout=%d;", busy)); err != nil {
			_ = db.Close()
			s.initErr = fmt.Errorf("sqlite: set busy_timeout: %w", err)
			return
		}
		if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON;"); err != nil {
			_ = db.Close()
			s.initErr = fmt.Errorf("sqlite: enable foreign_keys: %w", err)
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

	if q.SinceTS != nil && q.BeforeTS != nil {
		return nil, errors.New("sqlite: since_ts and before_ts are mutually exclusive")
	}

	query := `SELECT id, ts, username, platform, text, emotes_json, COALESCE(raw_json, '') FROM messages`
	var (
		clauses []string
		args    []any
	)
	if q.SinceTS != nil {
		clauses = append(clauses, "ts >= ?")
		args = append(args, q.SinceTS.UTC().UnixMilli())
	}
	if q.BeforeTS != nil {
		clauses = append(clauses, "ts < ?")
		args = append(args, q.BeforeTS.UTC().UnixMilli())
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY ts DESC, id DESC"
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

// TailHead returns the most recent (ts,rowid) pair from the same row.
// This avoids the inconsistent MAX(ts)/MAX(rowid) pair that can skip rows.
func (s *Store) TailHead(ctx context.Context) (storage.TailPosition, error) {
	if s.db == nil {
		return storage.TailPosition{}, errors.New("sqlite: store not initialized")
	}

	var pos storage.TailPosition

	err := s.db.QueryRowContext(ctx, `
		SELECT ts, rowid
		FROM messages
		ORDER BY ts DESC, rowid DESC
		LIMIT 1
	`).Scan(&pos.TS, &pos.RowID)

	// Empty table -> start at zero position
	if err == sql.ErrNoRows {
		return storage.TailPosition{}, nil
	}
	if err != nil {
		return storage.TailPosition{}, fmt.Errorf("sqlite: tail head: %w", err)
	}

	return pos, nil
}

// TailNext returns up to limit messages strictly after the provided position ordered by timestamp then rowid.
func (s *Store) TailNext(ctx context.Context, after storage.TailPosition, limit int) ([]storage.Message, storage.TailPosition, error) {
	if s.db == nil {
		return nil, after, errors.New("sqlite: store not initialized")
	}

	if limit <= 0 {
		limit = 500
	}

	query := `SELECT id, ts, username, platform, text, emotes_json, COALESCE(raw_json, ''), rowid
FROM messages
WHERE ts > ? OR (ts = ? AND rowid > ?)
ORDER BY ts ASC, rowid ASC
LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, after.TS, after.TS, after.RowID, limit)
	if err != nil {
		return nil, after, fmt.Errorf("sqlite: tail next query: %w", err)
	}
	defer rows.Close()

	results := make([]storage.Message, 0, limit)
	last := after

	for rows.Next() {
		var (
			msg   storage.Message
			ts    int64
			rowID int64
		)
		if err := rows.Scan(&msg.ID, &ts, &msg.Username, &msg.Platform, &msg.Text, &msg.EmotesJSON, &msg.RawJSON, &rowID); err != nil {
			return nil, after, fmt.Errorf("sqlite: tail next scan: %w", err)
		}
		msg.Timestamp = time.UnixMilli(ts).UTC()
		results = append(results, msg)
		last = storage.TailPosition{TS: ts, RowID: rowID}
	}

	if err := rows.Err(); err != nil {
		return nil, after, fmt.Errorf("sqlite: tail next rows: %w", err)
	}

	return results, last, nil
}

// PurgeBefore deletes chat messages with timestamps strictly less than the provided cutoff.
func (s *Store) PurgeBefore(ctx context.Context, cutoff time.Time) (int, error) {
	if s.db == nil {
		return 0, errors.New("sqlite: store not initialized")
	}

	res, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE ts < ?`, cutoff.UTC().UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("sqlite: purge before: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("sqlite: rows affected: %w", err)
	}

	return int(affected), nil
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

// GetSession retrieves a stored session by token.
func (s *Store) GetSession(ctx context.Context, token string) (*storage.Session, error) {
	if s.db == nil {
		return nil, errors.New("sqlite: store not initialized")
	}

	row := s.db.QueryRowContext(ctx, `SELECT token, service, data_json, token_expiry, updated_at FROM sessions WHERE token = ?`, token)
	var (
		tok, service, data string
		expiry, updated    int64
	)
	if err := row.Scan(&tok, &service, &data, &expiry, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("sqlite: get session: %w", err)
	}

	return &storage.Session{
		Token:       tok,
		Service:     service,
		DataJSON:    data,
		TokenExpiry: time.Unix(expiry, 0).UTC(),
		UpdatedAt:   time.Unix(updated, 0).UTC(),
	}, nil
}

// UpsertSession creates or updates a stored session record.
func (s *Store) UpsertSession(ctx context.Context, sess *storage.Session) error {
	if s.db == nil {
		return errors.New("sqlite: store not initialized")
	}
	if sess == nil {
		return errors.New("sqlite: session is nil")
	}
	if strings.TrimSpace(sess.Token) == "" {
		return errors.New("sqlite: session token is empty")
	}

	expiry := sess.TokenExpiry.UTC().Unix()
	updated := sess.UpdatedAt.UTC().Unix()
	if updated == 0 {
		updated = time.Now().UTC().Unix()
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions(token, service, data_json, token_expiry, updated_at)
         VALUES(?, ?, ?, ?, ?)
         ON CONFLICT(token) DO UPDATE SET
           service=excluded.service,
           data_json=excluded.data_json,
           token_expiry=excluded.token_expiry,
           updated_at=excluded.updated_at`,
		sess.Token,
		sess.Service,
		sess.DataJSON,
		expiry,
		updated,
	)
	if err != nil {
		return fmt.Errorf("sqlite: upsert session: %w", err)
	}
	return nil
}

// DeleteSession removes a stored session by token.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	if s.db == nil {
		return errors.New("sqlite: store not initialized")
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token); err != nil {
		return fmt.Errorf("sqlite: delete session: %w", err)
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

func (s *Store) resolvePath() (string, bool, error) {
	mode := strings.ToLower(strings.TrimSpace(s.cfg.Mode))
	if mode != "persistent" {
		mode = "ephemeral"
	}

	path := strings.TrimSpace(s.cfg.Path)
	if mode == "ephemeral" {
		if path == "" {
			path = filepath.Join(os.TempDir(), fmt.Sprintf("elora-chat-%d.db", os.Getpid()))
		}
	} else {
		if path == "" {
			return "", false, errors.New("sqlite: persistent mode requires ELORA_DB_PATH")
		}
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", mode == "ephemeral", fmt.Errorf("sqlite: resolve path: %w", err)
	}
	return abs, mode == "ephemeral", nil
}

func (s *Store) storageMode() string {
	if s.ephemeral {
		return "ephemeral"
	}
	return "persistent"
}

func (s *Store) buildDSN(path string) string {
	params := []string{"cache=shared", "mode=rwc"}
	params = append(params, s.parseExtraPragmas()...)

	query := strings.Join(params, "&")
	escapedPath := url.PathEscape(path)
	return fmt.Sprintf("file:%s?%s", escapedPath, query)
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
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("sqlite: ensure schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("sqlite: read migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin migrations: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version, err := parseMigrationVersion(entry.Name())
		if err != nil {
			_ = tx.Rollback()
			return err
		}

		var exists int
		switch err := tx.QueryRowContext(ctx, `SELECT 1 FROM schema_migrations WHERE version = ?`, version).Scan(&exists); {
		case err == nil:
			continue
		case errors.Is(err, sql.ErrNoRows):
			// not applied
		default:
			_ = tx.Rollback()
			return fmt.Errorf("sqlite: check migration %s: %w", entry.Name(), err)
		}

		data, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("sqlite: read migration %s: %w", entry.Name(), err)
		}
		if _, err := tx.ExecContext(ctx, string(data)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("sqlite: apply migration %s: %w", entry.Name(), err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES (?)`, version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("sqlite: record migration %s: %w", entry.Name(), err)
		}
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("sqlite: commit migrations: %w", err)
	}
	return nil
}

func parseMigrationVersion(name string) (int, error) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	parts := strings.SplitN(base, "_", 2)
	if len(parts) == 0 || parts[0] == "" {
		return 0, fmt.Errorf("sqlite: invalid migration name %q", name)
	}
	version, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("sqlite: invalid migration version in %q: %w", name, err)
	}
	return version, nil
}
