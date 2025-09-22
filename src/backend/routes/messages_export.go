package routes

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
)

const (
	defaultExportLimit = 1000
	maxExportLimit     = 100000
)

type exportRecord struct {
	ID         string `json:"id"`
	Timestamp  string `json:"ts"`
	Username   string `json:"username"`
	Platform   string `json:"platform"`
	Text       string `json:"text"`
	EmotesJSON string `json:"emotes_json"`
	RawJSON    string `json:"raw_json"`
}

func parseExportLimit(raw string) (int, error) {
	if raw == "" {
		return defaultExportLimit, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, errors.New("invalid limit")
	}
	if limit > maxExportLimit {
		limit = maxExportLimit
	}
	return limit, nil
}

func handleExportMessages(w http.ResponseWriter, r *http.Request) {
	if chatStore == nil {
		http.Error(w, "storage not initialized", http.StatusInternalServerError)
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "ndjson"
	}
	if format != "ndjson" && format != "csv" {
		http.Error(w, "unsupported format", http.StatusBadRequest)
		return
	}

	limit, err := parseExportLimit(r.URL.Query().Get("limit"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	since, err := parseSince(r.URL.Query().Get("since_ts"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	before, err := parseCursor(r.URL.Query().Get("before_ts"), "before_ts")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if since != nil && before != nil {
		http.Error(w, "since_ts and before_ts are mutually exclusive", http.StatusBadRequest)
		return
	}

	messages, err := chatStore.GetRecent(r.Context(), storage.QueryOpts{
		Limit:    limit,
		SinceTS:  since,
		BeforeTS: before,
	})
	if err != nil {
		http.Error(w, "failed to fetch messages", http.StatusInternalServerError)
		return
	}

	switch format {
	case "ndjson":
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		for _, msg := range messages {
			record := exportRecord{
				ID:         msg.ID,
				Timestamp:  msg.Timestamp.UTC().Format(time.RFC3339Nano),
				Username:   msg.Username,
				Platform:   msg.Platform,
				Text:       msg.Text,
				EmotesJSON: msg.EmotesJSON,
				RawJSON:    msg.RawJSON,
			}
			if err := enc.Encode(record); err != nil {
				return
			}
		}
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		writer := csv.NewWriter(w)
		if err := writer.Write([]string{"id", "ts", "username", "platform", "text", "emotes_json", "raw_json"}); err != nil {
			return
		}
		for _, msg := range messages {
			row := []string{
				msg.ID,
				msg.Timestamp.UTC().Format(time.RFC3339Nano),
				msg.Username,
				msg.Platform,
				msg.Text,
				msg.EmotesJSON,
				msg.RawJSON,
			}
			if err := writer.Write(row); err != nil {
				return
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return
		}
	}
}

func handlePurgeMessages(w http.ResponseWriter, r *http.Request) {
	if chatStore == nil {
		http.Error(w, "storage not initialized", http.StatusInternalServerError)
		return
	}

	var payload struct {
		BeforeTS int64 `json:"before_ts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if payload.BeforeTS <= 0 {
		http.Error(w, "before_ts required", http.StatusBadRequest)
		return
	}

	cutoff := time.UnixMilli(payload.BeforeTS).UTC()
	deleted, err := chatStore.PurgeBefore(r.Context(), cutoff)
	if err != nil {
		http.Error(w, "failed to purge", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp := struct {
		Deleted int `json:"deleted"`
	}{Deleted: deleted}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(resp); err != nil {
		return
	}
}
