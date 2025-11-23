package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
	"github.com/hpwn/EloraChat/src/backend/internal/storage/sqlite"
)

func TestHandleDebugRawMessages(t *testing.T) {
	os.Setenv("ELORA_DEBUG_RAW_MESSAGES", "true")
	t.Cleanup(func() {
		os.Unsetenv("ELORA_DEBUG_RAW_MESSAGES")
		debugRawOnce = sync.Once{}
		debugRawEnabled = false
	})

	store := sqlite.New(sqlite.Config{})
	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close(ctx)
	})

	msg := &storage.Message{ID: "r1", Platform: "twitch", Username: "user", Text: "hello", Timestamp: time.Now().UTC()}
	if err := store.InsertMessage(ctx, msg); err != nil {
		t.Fatalf("InsertMessage returned error: %v", err)
	}

	prevStore := chatStore
	chatStore = store
	t.Cleanup(func() {
		chatStore = prevStore
	})

	r := mux.NewRouter()
	SetupDebugRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/debug/raw-messages?limit=1", nil)
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	var rows []sqlite.DebugRawMessage
	if err := json.Unmarshal(resp.Body.Bytes(), &rows); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Platform != "twitch" || rows[0].Username != "user" {
		t.Fatalf("unexpected row data: %#v", rows[0])
	}
}
