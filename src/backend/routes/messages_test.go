package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
	"github.com/hpwn/EloraChat/src/backend/internal/storage/sqlite"
)

type recentMessageResponse struct {
	ID         string `json:"id"`
	Timestamp  string `json:"ts"`
	Username   string `json:"username"`
	Platform   string `json:"platform"`
	Text       string `json:"text"`
	EmotesJSON string `json:"emotes_json"`
	RawJSON    string `json:"raw_json"`
}

type messagesAPIPayload struct {
	Items        []recentMessageResponse `json:"items"`
	NextBeforeTS *int64                  `json:"next_before_ts"`
}

func withSQLiteStore(t *testing.T) func() {
	t.Helper()

	prevStore := chatStore

	store := sqlite.New(sqlite.Config{Mode: "ephemeral"})
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("sqlite init: %v", err)
	}

	chatStore = store

	return func() {
		if err := store.Close(context.Background()); err != nil {
			t.Errorf("sqlite close: %v", err)
		}
		chatStore = prevStore
	}
}

func seedRecentMessages(t *testing.T) []storage.Message {
	t.Helper()

	base := time.Now().UTC().Add(-2 * time.Minute)
	msgs := []storage.Message{
		{
			ID:        "msg-1",
			Timestamp: base.Add(10 * time.Second),
			Username:  "user-a",
			Platform:  "twitch",
			Text:      "hello",
		},
		{
			ID:        "msg-2",
			Timestamp: base.Add(20 * time.Second),
			Username:  "user-b",
			Platform:  "youtube",
			Text:      "world",
		},
		{
			ID:        "msg-3",
			Timestamp: base.Add(30 * time.Second),
			Username:  "user-c",
			Platform:  "twitch",
			Text:      "!!!",
		},
	}

	for i := range msgs {
		// ensure deterministic ordering in store
		if err := chatStore.InsertMessage(context.Background(), &msgs[i]); err != nil {
			t.Fatalf("insert message %d: %v", i, err)
		}
	}

	return msgs
}

func newMessagesRouter() *mux.Router {
	r := mux.NewRouter()
	SetupMessageRoutes(r)
	return r
}

func TestHandleGetRecentMessages_DefaultLimit(t *testing.T) {
	cleanup := withSQLiteStore(t)
	defer cleanup()

	msgs := seedRecentMessages(t)

	req := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	rr := httptest.NewRecorder()

	router := newMessagesRouter()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}

	var payload messagesAPIPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(payload.Items) != len(msgs) {
		t.Fatalf("expected %d messages, got %d", len(msgs), len(payload.Items))
	}

	if payload.Items[0].ID != msgs[2].ID {
		t.Fatalf("expected most recent message id %q, got %q", msgs[2].ID, payload.Items[0].ID)
	}

	if _, err := time.Parse(time.RFC3339Nano, payload.Items[0].Timestamp); err != nil {
		t.Fatalf("timestamp is not RFC3339: %v", err)
	}

	if payload.NextBeforeTS != nil {
		t.Fatalf("expected next_before_ts to be nil when results < limit")
	}
}

func TestHandleGetRecentMessages_SinceAndLimit(t *testing.T) {
	cleanup := withSQLiteStore(t)
	defer cleanup()

	msgs := seedRecentMessages(t)

	since := fmt.Sprintf("%d", msgs[1].Timestamp.UTC().UnixMilli())
	rawURL := fmt.Sprintf("/api/messages?since_ts=%s&limit=1", since)

	req := httptest.NewRequest(http.MethodGet, rawURL, nil)
	rr := httptest.NewRecorder()

	router := newMessagesRouter()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}

	var payload messagesAPIPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(payload.Items) != 1 {
		t.Fatalf("expected 1 message, got %d", len(payload.Items))
	}

	if payload.Items[0].ID != msgs[2].ID {
		t.Fatalf("expected message id %q, got %q", msgs[2].ID, payload.Items[0].ID)
	}

	if _, err := time.Parse(time.RFC3339Nano, payload.Items[0].Timestamp); err != nil {
		t.Fatalf("timestamp is not RFC3339: %v", err)
	}

	if payload.NextBeforeTS == nil {
		t.Fatalf("expected next_before_ts when more messages available")
	}
}

func TestHandleGetRecentMessages_SinceUnixMillis(t *testing.T) {
	cleanup := withSQLiteStore(t)
	defer cleanup()

	msgs := seedRecentMessages(t)

	since := msgs[0].Timestamp.UTC().UnixMilli()
	rawURL := fmt.Sprintf("/api/messages?since_ts=%d", since)

	req := httptest.NewRequest(http.MethodGet, rawURL, nil)
	rr := httptest.NewRecorder()

	router := newMessagesRouter()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}

	var payload messagesAPIPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(payload.Items) != len(msgs) {
		t.Fatalf("expected %d messages, got %d", len(msgs), len(payload.Items))
	}

	if payload.NextBeforeTS != nil {
		t.Fatalf("expected next_before_ts to be nil when full history returned")
	}
}

func TestHandleGetRecentMessages_BeforeTS(t *testing.T) {
	cleanup := withSQLiteStore(t)
	defer cleanup()

	msgs := seedRecentMessages(t)

	req := httptest.NewRequest(http.MethodGet, "/api/messages?limit=2", nil)
	rr := httptest.NewRecorder()

	router := newMessagesRouter()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}

	var first messagesAPIPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(first.Items) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(first.Items))
	}
	if first.NextBeforeTS == nil {
		t.Fatalf("expected next_before_ts on first page")
	}
	if first.Items[0].ID != msgs[2].ID {
		t.Fatalf("expected most recent message, got %q", first.Items[0].ID)
	}

	nextURL := fmt.Sprintf("/api/messages?limit=2&before_ts=%s", strconv.FormatInt(*first.NextBeforeTS, 10))
	req2 := httptest.NewRequest(http.MethodGet, nextURL, nil)
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr2.Code)
	}

	var second messagesAPIPayload
	if err := json.Unmarshal(rr2.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode page 2: %v", err)
	}

	if len(second.Items) != 1 {
		t.Fatalf("expected 1 message on second page, got %d", len(second.Items))
	}
	if second.Items[0].ID != msgs[0].ID {
		t.Fatalf("expected oldest message, got %q", second.Items[0].ID)
	}
	if second.NextBeforeTS != nil {
		t.Fatalf("expected no next_before_ts when no more pages")
	}
}

func TestHandleGetRecentMessages_ConflictingCursors(t *testing.T) {
	cleanup := withSQLiteStore(t)
	defer cleanup()

	_ = seedRecentMessages(t)

	req := httptest.NewRequest(http.MethodGet, "/api/messages?since_ts=100&before_ts=50", nil)
	rr := httptest.NewRecorder()

	router := newMessagesRouter()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rr.Code)
	}
}
