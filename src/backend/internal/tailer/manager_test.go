package tailer

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
)

type managerStoreStub struct {
	failHead atomic.Bool
	headHits atomic.Int32
}

func (s *managerStoreStub) TailHead(context.Context) (storage.TailPosition, error) {
	s.headHits.Add(1)
	if s.failHead.Load() {
		return storage.TailPosition{}, errors.New("boom")
	}
	return storage.TailPosition{}, nil
}

func (s *managerStoreStub) TailNext(context.Context, storage.TailPosition, int) ([]storage.Message, storage.TailPosition, error) {
	return nil, storage.TailPosition{}, nil
}

func TestManagerApplyAndSnapshot(t *testing.T) {
	store := &managerStoreStub{}
	mgr := NewManager(context.Background(), store)

	cfg := Config{Enabled: true, Batch: 200}
	if err := mgr.StartInitial(cfg); err != nil {
		t.Fatalf("start initial failed: %v", err)
	}

	got := mgr.SnapshotConfig()
	if got.Batch != 200 || !got.Enabled {
		t.Fatalf("unexpected snapshot: %+v", got)
	}

	if gotHits := store.headHits.Load(); gotHits == 0 {
		t.Fatalf("expected tail head to be called for enabled runner")
	}

	mgr.Stop()
}

func TestManagerApplyRollbackOnFailure(t *testing.T) {
	store := &managerStoreStub{}
	mgr := NewManager(context.Background(), store)

	prev := Config{Enabled: true, Batch: 111}
	if err := mgr.StartInitial(prev); err != nil {
		t.Fatalf("start initial failed: %v", err)
	}

	store.failHead.Store(true)
	err := mgr.Apply(Config{Enabled: true, Batch: 999})
	if err == nil {
		t.Fatalf("expected apply failure")
	}

	got := mgr.SnapshotConfig()
	if got.Batch != prev.Batch {
		t.Fatalf("expected rollback config batch=%d, got %+v", prev.Batch, got)
	}

	mgr.Stop()
}
