package configreporter

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hpwn/EloraChat/src/backend/internal/ingest"
	"github.com/hpwn/EloraChat/src/backend/internal/storage/sqlite"
	"github.com/hpwn/EloraChat/src/backend/internal/tailer"
)

func TestSnapshotRedactsIngestSecrets(t *testing.T) {
	reporter := NewReporter(
		sqlite.Config{Mode: "persistent", Path: "/data/elora.db", JournalMode: "WAL", MaxConns: 8, BusyTimeoutMS: 1000, PragmasExtraCSV: "test=1"},
		tailer.Config{Enabled: true, Interval: 25 * time.Millisecond, Batch: 200, MaxLag: 50 * time.Millisecond, PersistOffsets: true, OffsetPath: "/tmp/off"},
		ingest.Env{Driver: ingest.DriverGnasty, GnastyArgs: []string{"--token=secret"}},
		Origins{AllowAny: false, Values: []string{"http://localhost:8080"}},
		WebsocketLimits{PingInterval: 25 * time.Second, PongWait: 30 * time.Second, WriteDeadline: 5 * time.Second, MaxMessage: 131072},
	)

	snapshot := reporter.Snapshot()
	if snapshot.Ingest.Driver != ingest.DriverGnasty {
		t.Fatalf("expected driver %q, got %q", ingest.DriverGnasty, snapshot.Ingest.Driver)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if strings.Contains(string(data), "secret") {
		t.Fatalf("snapshot leaked secret data: %s", data)
	}
}

func TestSummaryJSON(t *testing.T) {
	reporter := NewReporter(
		sqlite.Config{Mode: "ephemeral", Path: "", JournalMode: "WAL"},
		tailer.Config{Enabled: false},
		ingest.Env{Driver: ingest.DriverChatDownloader},
		Origins{AllowAny: true},
		WebsocketLimits{},
	)

	data, err := reporter.SummaryJSON()
	if err != nil {
		t.Fatalf("summary json: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected non-empty json summary")
	}
}
