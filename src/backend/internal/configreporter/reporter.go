package configreporter

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/hpwn/EloraChat/src/backend/internal/ingest"
	"github.com/hpwn/EloraChat/src/backend/internal/storage/sqlite"
	"github.com/hpwn/EloraChat/src/backend/internal/tailer"
)

// Origins captures the configured WebSocket/CORS origin policy.
type Origins struct {
	AllowAny bool
	Values   []string
}

// WebsocketLimits summarizes the runtime WebSocket tuning knobs.
type WebsocketLimits struct {
	PingInterval  time.Duration
	PongWait      time.Duration
	WriteDeadline time.Duration
	MaxMessage    int64
}

// Reporter produces redacted runtime configuration snapshots for diagnostics.
type Reporter struct {
	db        sqlite.Config
	tailer    tailer.Config
	ingest    ingest.Env
	origins   Origins
	websocket WebsocketLimits
}

// NewReporter constructs a Reporter from the effective runtime configuration.
func NewReporter(db sqlite.Config, tailerCfg tailer.Config, ingestEnv ingest.Env, origins Origins, ws WebsocketLimits) Reporter {
	return Reporter{
		db:        db,
		tailer:    tailerCfg,
		ingest:    ingestEnv,
		origins:   normalizeOrigins(origins),
		websocket: ws,
	}
}

// Snapshot represents the redacted configuration payload returned by /configz.
type Snapshot struct {
	DB        DBSnapshot        `json:"db"`
	Tailer    TailerSnapshot    `json:"tailer"`
	Ingest    IngestSnapshot    `json:"ingest"`
	Websocket WebsocketSnapshot `json:"websocket"`
}

// DBSnapshot contains the SQLite-related configuration shared with operators.
type DBSnapshot struct {
	Mode         string `json:"mode"`
	Path         string `json:"path"`
	JournalMode  string `json:"journal_mode"`
	MaxConns     int    `json:"max_conns"`
	BusyTimeout  int    `json:"busy_timeout_ms"`
	PragmasExtra string `json:"pragmas_extra_csv"`
}

// TailerSnapshot captures the DB tailer's publish settings.
type TailerSnapshot struct {
	Enabled        bool   `json:"enabled"`
	IntervalMS     int    `json:"interval_ms"`
	Batch          int    `json:"batch"`
	MaxLagMS       int    `json:"max_lag_ms"`
	PersistOffsets bool   `json:"persist_offsets"`
	OffsetPath     string `json:"offset_path,omitempty"`
}

// IngestSnapshot surfaces the selected ingest driver without revealing secrets.
type IngestSnapshot struct {
	Driver string `json:"driver"`
}

// WebsocketSnapshot reports websocket/CORS tuning knobs.
type WebsocketSnapshot struct {
	AllowAny        bool     `json:"allow_any_origin"`
	AllowedOrigins  []string `json:"allowed_origins,omitempty"`
	PingIntervalMS  int      `json:"ping_interval_ms"`
	PongWaitMS      int      `json:"pong_wait_ms"`
	WriteDeadlineMS int      `json:"write_deadline_ms"`
	MaxMessageBytes int64    `json:"max_message_bytes"`
}

// Summary is the compact subset logged on startup.
type Summary struct {
	DB        DBSummary        `json:"db"`
	Tailer    TailerSummary    `json:"tailer"`
	Ingest    IngestSnapshot   `json:"ingest"`
	Websocket WebsocketSummary `json:"websocket"`
}

// DBSummary is the subset of DB settings logged at startup.
type DBSummary struct {
	Mode        string `json:"mode"`
	Path        string `json:"path"`
	JournalMode string `json:"journal_mode"`
}

// TailerSummary contains the subset of tailer settings logged at startup.
type TailerSummary struct {
	Enabled    bool `json:"enabled"`
	IntervalMS int  `json:"interval_ms"`
	Batch      int  `json:"batch"`
	MaxLagMS   int  `json:"max_lag_ms"`
}

// WebsocketSummary contains the subset of websocket settings logged at startup.
type WebsocketSummary struct {
	PingIntervalMS  int   `json:"ping_interval_ms"`
	PongWaitMS      int   `json:"pong_wait_ms"`
	WriteDeadlineMS int   `json:"write_deadline_ms"`
	MaxMessageBytes int64 `json:"max_message_bytes"`
}

// Snapshot returns the current redacted configuration snapshot.
func (r Reporter) Snapshot() Snapshot {
	offsetPath := r.tailer.OffsetPath
	if !r.tailer.PersistOffsets {
		offsetPath = ""
	}

	return Snapshot{
		DB: DBSnapshot{
			Mode:         r.db.Mode,
			Path:         r.db.Path,
			JournalMode:  r.db.JournalMode,
			MaxConns:     r.db.MaxConns,
			BusyTimeout:  r.db.BusyTimeoutMS,
			PragmasExtra: r.db.PragmasExtraCSV,
		},
		Tailer: TailerSnapshot{
			Enabled:        r.tailer.Enabled,
			IntervalMS:     int(r.tailer.Interval / time.Millisecond),
			Batch:          r.tailer.Batch,
			MaxLagMS:       int(r.tailer.MaxLag / time.Millisecond),
			PersistOffsets: r.tailer.PersistOffsets,
			OffsetPath:     offsetPath,
		},
		Ingest: IngestSnapshot{Driver: r.ingest.Driver},
		Websocket: WebsocketSnapshot{
			AllowAny:        r.origins.AllowAny,
			AllowedOrigins:  append([]string(nil), r.origins.Values...),
			PingIntervalMS:  durationToMS(r.websocket.PingInterval),
			PongWaitMS:      durationToMS(r.websocket.PongWait),
			WriteDeadlineMS: durationToMS(r.websocket.WriteDeadline),
			MaxMessageBytes: r.websocket.MaxMessage,
		},
	}
}

// Summary returns the compact subset logged at startup.
func (r Reporter) Summary() Summary {
	return Summary{
		DB: DBSummary{
			Mode:        r.db.Mode,
			Path:        r.db.Path,
			JournalMode: r.db.JournalMode,
		},
		Tailer: TailerSummary{
			Enabled:    r.tailer.Enabled,
			IntervalMS: int(r.tailer.Interval / time.Millisecond),
			Batch:      r.tailer.Batch,
			MaxLagMS:   int(r.tailer.MaxLag / time.Millisecond),
		},
		Ingest: IngestSnapshot{Driver: r.ingest.Driver},
		Websocket: WebsocketSummary{
			PingIntervalMS:  durationToMS(r.websocket.PingInterval),
			PongWaitMS:      durationToMS(r.websocket.PongWait),
			WriteDeadlineMS: durationToMS(r.websocket.WriteDeadline),
			MaxMessageBytes: r.websocket.MaxMessage,
		},
	}
}

// SummaryJSON returns the summary encoded as JSON.
func (r Reporter) SummaryJSON() ([]byte, error) {
	return json.Marshal(r.Summary())
}

func durationToMS(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(d / time.Millisecond)
}

func normalizeOrigins(origins Origins) Origins {
	out := Origins{AllowAny: origins.AllowAny}
	if len(origins.Values) == 0 {
		return out
	}
	out.Values = append([]string(nil), origins.Values...)
	sort.Strings(out.Values)
	return out
}
