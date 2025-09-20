package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
)

const (
	defaultMessagesLimit = 50
	maxMessagesLimit     = 100
)

type messageResponse struct {
	ID         string `json:"id"`
	Timestamp  string `json:"ts"`
	Username   string `json:"username"`
	Platform   string `json:"platform"`
	Text       string `json:"text"`
	EmotesJSON string `json:"emotes_json"`
	RawJSON    string `json:"raw_json"`
}

type messagesEnvelope struct {
	Items        []messageResponse `json:"items"`
	NextBeforeTS *int64            `json:"next_before_ts,omitempty"`
}

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

	before, err := parseCursor(r.URL.Query().Get("before_ts"), "before_ts")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if since != nil && before != nil {
		http.Error(w, "since_ts and before_ts are mutually exclusive", http.StatusBadRequest)
		return
	}

	fetchLimit := limit
	if fetchLimit > 0 {
		fetchLimit++
	}

	messages, err := chatStore.GetRecent(r.Context(), storage.QueryOpts{
		Limit:    fetchLimit,
		SinceTS:  since,
		BeforeTS: before,
	})
	if err != nil {
		http.Error(w, "failed to fetch messages", http.StatusInternalServerError)
		return
	}

	var nextBefore *int64
	if limit > 0 && len(messages) > limit {
		messages = messages[:limit]
		oldest := messages[len(messages)-1].Timestamp.UTC().UnixMilli() - 1
		nextBefore = &oldest
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

	payload := messagesEnvelope{Items: resp, NextBeforeTS: nextBefore}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
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
	return parseCursor(raw, "since_ts")
}

func parseCursor(raw, param string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}

	ms, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid %s", param)
	}
	if ms < 0 {
		return nil, fmt.Errorf("invalid %s", param)
	}
	t := time.UnixMilli(ms).UTC()
	return &t, nil
}
