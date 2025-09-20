package routes

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
)

const (
	defaultMessagesLimit = 100
	maxMessagesLimit     = 500
)

func SetupMessageRoutes(r *mux.Router) {
	r.HandleFunc("/api/messages", handleGetRecentMessages).Methods(http.MethodGet)
}

func handleGetRecentMessages(w http.ResponseWriter, r *http.Request) {
	if chatStore == nil {
		http.Error(w, "storage not initialized", http.StatusInternalServerError)
		return
	}

	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	since, err := parseSince(r.URL.Query().Get("since_ts"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	messages, err := chatStore.GetRecent(r.Context(), storage.QueryOpts{
		Limit: limit,
		Since: since,
	})
	if err != nil {
		http.Error(w, "failed to fetch messages", http.StatusInternalServerError)
		return
	}

	type messageResponse struct {
		ID         string `json:"id"`
		Timestamp  string `json:"ts"`
		Username   string `json:"username"`
		Platform   string `json:"platform"`
		Text       string `json:"text"`
		EmotesJSON string `json:"emotes_json"`
		RawJSON    string `json:"raw_json"`
	}

	resp := make([]messageResponse, 0, len(messages))
	for _, msg := range messages {
		resp = append(resp, messageResponse{
			ID:         msg.ID,
			Timestamp:  msg.Timestamp.UTC().Format(time.RFC3339Nano),
			Username:   msg.Username,
			Platform:   msg.Platform,
			Text:       msg.Text,
			EmotesJSON: msg.EmotesJSON,
			RawJSON:    msg.RawJSON,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(resp); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}

func parseLimit(raw string) (int, error) {
	if raw == "" {
		return defaultMessagesLimit, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("invalid limit")
	}
	if limit <= 0 {
		return 0, errors.New("limit must be positive")
	}
	if limit > maxMessagesLimit {
		limit = maxMessagesLimit
	}
	return limit, nil
}

func parseSince(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}

	if ms, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if ms <= 0 {
			return nil, errors.New("invalid since_ts")
		}
		t := time.UnixMilli(ms).UTC()
		return &t, nil
	}

	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		t := ts.UTC()
		return &t, nil
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		t := ts.UTC()
		return &t, nil
	}

	return nil, errors.New("invalid since_ts")
}
