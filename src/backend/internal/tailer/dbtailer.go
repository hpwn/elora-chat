package tailer

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
	"github.com/hpwn/EloraChat/src/backend/routes"
)

// Position represents the tail cursor for iterating over messages.
// It aliases storage.TailPosition to avoid introducing an import cycle.
type Position = storage.TailPosition

// Store describes the subset of storage capabilities required by the tailer.
type Store interface {
	TailHead(ctx context.Context) (storage.TailPosition, error)
	TailNext(ctx context.Context, after storage.TailPosition, limit int) ([]storage.Message, storage.TailPosition, error)
}

// Config controls how the database tailer operates.
type Config struct {
	Enabled        bool
	Interval       time.Duration
	Batch          int
	MaxLag         time.Duration
	PersistOffsets bool
	OffsetPath     string
}

// Runner periodically polls the backing store for new messages and broadcasts them.
type Runner struct {
	cfg   Config
	store Store

	mu        sync.Mutex
	seen      map[string]struct{}
	last      storage.TailPosition
	persisted storage.TailPosition
	persistMu sync.Mutex

	cancel context.CancelFunc
}

// New creates a new Runner with the provided configuration and store.
func New(cfg Config, store Store) *Runner {
	return &Runner{
		cfg:   cfg,
		store: store,
		seen:  make(map[string]struct{}, 1024),
	}
}

// Start begins the background polling loop if enabled.
func (r *Runner) Start(ctx context.Context) error {
	if !r.cfg.Enabled {
		return nil
	}
	if r.store == nil {
		return nil
	}

	head, err := r.store.TailHead(ctx)
	if err != nil {
		return err
	}

	if r.cfg.PersistOffsets && r.cfg.OffsetPath != "" {
		persisted, err := loadOffset(r.cfg.OffsetPath)
		if err != nil {
			log.Printf("dbtailer: load offset: %v", err)
		} else if persisted.TS != 0 || persisted.RowID != 0 {
			head = persisted
			r.persisted = persisted
			log.Printf("dbtailer: resume from persisted offset ts=%d rowid=%d", head.TS, head.RowID)
		}
	}

	log.Printf("dbtailer: enabled interval=%s batch=%d start_pos ts=%d rowid=%d",
		r.cfg.Interval, r.cfg.Batch, head.TS, head.RowID)

	runCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.last = head
	r.persistPosition()

	go r.loop(runCtx)

	return nil
}

// Stop terminates the background polling loop if it is running.
func (r *Runner) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *Runner) loop(ctx context.Context) {
	interval := r.cfg.Interval
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *Runner) tick(ctx context.Context) {
	batchSize := r.cfg.Batch
	if batchSize <= 0 {
		batchSize = 500
	}

	msgs, last, err := r.store.TailNext(ctx, r.last, batchSize)
	if err != nil {
		log.Printf("dbtailer: error: %v", err)
		return
	}

	r.last = last
	r.persistPosition()
	if len(msgs) == 0 {
		return
	}

	toBroadcast := make([]storage.Message, 0, len(msgs))

	r.mu.Lock()
	for _, msg := range msgs {
		if msg.ID == "" {
			continue
		}
		if _, ok := r.seen[msg.ID]; ok {
			continue
		}
		r.seen[msg.ID] = struct{}{}
		toBroadcast = append(toBroadcast, msg)
	}
	if len(r.seen) > 200_000 {
		r.seen = make(map[string]struct{}, 1024)
	}
	r.mu.Unlock()

	if len(toBroadcast) == 0 {
		return
	}

	for _, msg := range toBroadcast {
		routes.BroadcastFromTailer(msg)
	}

	if r.cfg.MaxLag > 0 && r.last.TS > 0 {
		lag := time.Since(time.UnixMilli(r.last.TS))
		if lag > r.cfg.MaxLag {
			log.Printf("dbtailer: warning: publish lag %s (ts=%d rowid=%d)", lag, r.last.TS, r.last.RowID)
		}
	}

	log.Printf("dbtailer: published n=%d new messages; last_pos ts=%d rowid=%d",
		len(toBroadcast), r.last.TS, r.last.RowID)
}

func (r *Runner) persistPosition() {
	if !r.cfg.PersistOffsets || r.cfg.OffsetPath == "" {
		return
	}
	r.persistMu.Lock()
	defer r.persistMu.Unlock()

	if r.last == r.persisted {
		return
	}

	state := offsetState{TS: r.last.TS, RowID: r.last.RowID}
	if err := writeOffset(r.cfg.OffsetPath, state); err != nil {
		log.Printf("dbtailer: persist offset: %v", err)
		return
	}
	r.persisted = r.last
}

type offsetState struct {
	TS    int64 `json:"ts"`
	RowID int64 `json:"row_id"`
}

func loadOffset(path string) (storage.TailPosition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return storage.TailPosition{}, nil
		}
		return storage.TailPosition{}, err
	}
	var state offsetState
	if err := json.Unmarshal(data, &state); err != nil {
		return storage.TailPosition{}, err
	}
	return storage.TailPosition{TS: state.TS, RowID: state.RowID}, nil
}

func writeOffset(path string, state offsetState) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
