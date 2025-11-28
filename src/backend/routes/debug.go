package routes

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/hpwn/EloraChat/src/backend/internal/storage/sqlite"
	"github.com/hpwn/EloraChat/src/backend/internal/ytdebug"
)

const (
	debugRawDefaultLimit = 100
	debugRawMaxLimit     = 500
)

var (
	debugRawOnce    sync.Once
	debugRawEnabled bool
)

// SetupDebugRoutes registers debug-only helpers gated by an explicit opt-in flag.
func SetupDebugRoutes(r *mux.Router) {
	if r == nil {
		return
	}

	rawEnabled := debugRawRouteEnabled()
	ytEnabled := ytdebug.Enabled()
	if !rawEnabled && !ytEnabled {
		return
	}

	sub := r.PathPrefix("/api/debug").Subrouter()
	if rawEnabled {
		sub.HandleFunc("/raw-messages", handleDebugRawMessages).Methods(http.MethodGet)
	}
	if ytEnabled {
		sub.HandleFunc("/yt-latest", handleDebugYouTubeLatest).Methods(http.MethodGet)
	}
}

func debugRawRouteEnabled() bool {
	debugRawOnce.Do(func() {
		raw := strings.TrimSpace(os.Getenv("ELORA_DEBUG_RAW_MESSAGES"))
		if raw == "" {
			return
		}
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			log.Printf("debug: invalid ELORA_DEBUG_RAW_MESSAGES=%q", raw)
			return
		}
		if !enabled {
			return
		}

		env := strings.ToLower(strings.TrimSpace(os.Getenv("ELORA_ENV")))
		if env == "" {
			env = strings.ToLower(strings.TrimSpace(os.Getenv("GO_ENV")))
		}
		if env == "" {
			env = strings.ToLower(strings.TrimSpace(os.Getenv("ENVIRONMENT")))
		}
		if env == "production" || env == "prod" {
			log.Printf("debug: refusing to enable raw messages route in environment %q", env)
			return
		}

		debugRawEnabled = true
	})
	return debugRawEnabled
}

func handleDebugRawMessages(w http.ResponseWriter, r *http.Request) {
	provider, ok := chatStore.(*sqlite.Store)
	if !ok {
		http.Error(w, "debug raw messages only supported with sqlite backend", http.StatusNotImplemented)
		return
	}

	limit, err := parseDebugLimit(r.URL.Query().Get("limit"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	beforeTS, err := parseDebugTS(r.URL.Query().Get("before_ts"), "before_ts")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	afterTS, err := parseDebugTS(r.URL.Query().Get("after_ts"), "after_ts")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	rows, err := provider.DebugRawMessages(r.Context(), sqlite.DebugRawQueryOpts{
		Limit:    limit,
		Platform: platform,
		BeforeTS: beforeTS,
		AfterTS:  afterTS,
	})
	if err != nil {
		http.Error(w, "failed to fetch raw messages", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(rows)
}

func parseDebugLimit(raw string) (int, error) {
	if raw == "" {
		return debugRawDefaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("invalid limit")
	}
	if limit <= 0 {
		return 0, errors.New("limit must be positive")
	}
	if limit > debugRawMaxLimit {
		limit = debugRawMaxLimit
	}
	return limit, nil
}

func parseDebugTS(raw, name string) (*int64, error) {
	if raw == "" {
		return nil, nil
	}
	ts, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, errors.New("invalid " + name)
	}
	if ts < 0 {
		return nil, errors.New("invalid " + name)
	}
	return &ts, nil
}

type ytDebugMessage struct {
	RowID       int64  `json:"rowid"`
	ID          string `json:"id"`
	Timestamp   string `json:"ts"`
	Username    string `json:"username"`
	Platform    string `json:"platform"`
	Text        string `json:"text"`
	ChannelID   string `json:"channel_id,omitempty"`
	Fingerprint string `json:"fingerprint"`
}

type ytDebugEnvelope struct {
	Items []ytDebugMessage `json:"items"`
}

func handleDebugYouTubeLatest(w http.ResponseWriter, r *http.Request) {
	provider, ok := chatStore.(*sqlite.Store)
	if !ok {
		http.Error(w, "yt debug only supported with sqlite backend", http.StatusNotImplemented)
		return
	}

	limit, err := parseDebugLimit(r.URL.Query().Get("limit"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	usernameSubstring := strings.TrimSpace(r.URL.Query().Get("username_substring"))

	rows, err := provider.DebugYouTubeMessages(r.Context(), sqlite.DebugYouTubeQueryOpts{
		Limit:             limit,
		UsernameSubstring: usernameSubstring,
	})
	if err != nil {
		http.Error(w, "failed to fetch youtube debug messages", http.StatusInternalServerError)
		return
	}

	resp := ytDebugEnvelope{Items: make([]ytDebugMessage, 0, len(rows))}
	for _, row := range rows {
		ts := time.UnixMilli(row.TS).UTC()
		channelID := ytdebug.ChannelIDFromRaw(row.RawJSON)
		fp := ytdebug.Fingerprint(ytdebug.FingerprintInput{
			Platform:  row.Platform,
			ChannelID: channelID,
			Username:  row.Username,
			Text:      row.Text,
			Timestamp: ts,
		})

		resp.Items = append(resp.Items, ytDebugMessage{
			RowID:       row.RowID,
			ID:          row.ID,
			Timestamp:   ts.Format(time.RFC3339Nano),
			Username:    row.Username,
			Platform:    row.Platform,
			Text:        row.Text,
			ChannelID:   channelID,
			Fingerprint: fp,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(resp)
}
