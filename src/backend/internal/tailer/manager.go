package tailer

import (
	"context"
	"log"
	"sync"
)

// Manager owns the live tailer runner and supports hot-applying tailer config.
type Manager struct {
	mu    sync.Mutex
	ctx   context.Context
	store Store

	cfg    Config
	runner *Runner
}

// NewManager creates a manager bound to the provided base context and store.
func NewManager(ctx context.Context, store Store) *Manager {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Manager{
		ctx:   ctx,
		store: store,
	}
}

// StartInitial starts the tailer with the initial config.
func (m *Manager) StartInitial(cfg Config) error {
	return m.Apply(cfg)
}

// Apply hot-applies the given config by restarting the managed runner.
func (m *Manager) Apply(cfg Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	prevCfg := m.cfg
	prevRunner := m.runner
	if prevRunner != nil {
		prevRunner.Stop()
		m.runner = nil
	}

	next := New(cfg, m.store)
	if err := next.Start(m.ctx); err != nil {
		// Best-effort rollback to the previous runner/config.
		if prevRunner != nil {
			rollback := New(prevCfg, m.store)
			if rollbackErr := rollback.Start(m.ctx); rollbackErr != nil {
				log.Printf("dbtailer: rollback failed after apply error: %v", rollbackErr)
			} else {
				m.runner = rollback
			}
		}
		return err
	}

	m.cfg = cfg
	m.runner = next
	return nil
}

// SnapshotConfig returns the currently active tailer config.
func (m *Manager) SnapshotConfig() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// Stop stops the managed runner.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runner != nil {
		m.runner.Stop()
		m.runner = nil
	}
}
