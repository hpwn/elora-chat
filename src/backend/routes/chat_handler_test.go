package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hpwn/EloraChat/src/backend/pkg/storage"
	"github.com/redis/go-redis/v9"
)

// MockRedisClient implements RedisClient interface for testing
type MockRedisClient struct {
	messages []redis.XMessage
	lastID   string
}

func (m *MockRedisClient) XAdd(ctx context.Context, a *redis.XAddArgs) *redis.StringCmd {
	m.lastID = "1234567890"
	values, ok := a.Values.(map[string]interface{})
	if !ok {
		values = make(map[string]interface{})
	}
	m.messages = append(m.messages, redis.XMessage{
		ID:     m.lastID,
		Values: values,
	})
	cmd := redis.NewStringCmd(ctx)
	cmd.SetVal(m.lastID)
	return cmd
}

func (m *MockRedisClient) XRead(ctx context.Context, a *redis.XReadArgs) *redis.XStreamSliceCmd {
	if len(m.messages) == 0 {
		return redis.NewXStreamSliceCmd(ctx)
	}

	streams := []redis.XStream{
		{
			Stream:   "chatMessages",
			Messages: m.messages,
		},
	}
	cmd := redis.NewXStreamSliceCmd(ctx)
	cmd.SetVal(streams)
	return cmd
}

func (m *MockRedisClient) XRevRangeN(ctx context.Context, stream, end, start string, count int64) *redis.XMessageSliceCmd {
	cmd := redis.NewXMessageSliceCmd(ctx)
	cmd.SetVal(m.messages)
	return cmd
}

func (m *MockRedisClient) Ping(ctx context.Context) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx)
	cmd.SetVal("PONG")
	return cmd
}

func setupTest(t *testing.T) (*storage.DB, *MockRedisClient) {
	os.Setenv("GO_TESTING", "1")
	defer os.Unsetenv("GO_TESTING")

	db, err := storage.NewDB()
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	mockRedis := &MockRedisClient{
		messages: make([]redis.XMessage, 0),
	}

	return db, mockRedis
}

func TestChatHandler_ProcessMessage(t *testing.T) {
	db, mockRedis := setupTest(t)
	defer db.Close()

	handler := NewChatHandler(db, mockRedis)

	testMsg := []byte(`{"author": "test", "message": "hello"}`)
	if err := handler.ProcessMessage(testMsg); err != nil {
		t.Fatalf("Failed to process message: %v", err)
	}

	// Verify message was stored in Redis
	if len(mockRedis.messages) != 1 {
		t.Errorf("Expected 1 message in Redis, got %d", len(mockRedis.messages))
	}

	// Verify message was stored in SQLite
	messages, err := db.GetLatestMessages(1)
	if err != nil {
		t.Fatalf("Failed to get messages from SQLite: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("Expected 1 message in SQLite, got %d", len(messages))
	}
	if string(messages[0].Data) != string(testMsg) {
		t.Errorf("Expected message %s, got %s", string(testMsg), string(messages[0].Data))
	}
}

func TestChatHandler_HandleWebSocket(t *testing.T) {
	db, mockRedis := setupTest(t)
	defer db.Close()

	handler := NewChatHandler(db, mockRedis)

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	// Convert http://... to ws://...
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect to WebSocket
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer ws.Close()

	// Insert a test message
	testMsg := []byte(`{"author": "test", "message": "hello"}`)
	if err := handler.ProcessMessage(testMsg); err != nil {
		t.Fatalf("Failed to process message: %v", err)
	}

	// Wait for message to be received
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, message, err := ws.ReadMessage()
		if err != nil {
			t.Errorf("Failed to read message: %v", err)
			return
		}
		if string(message) != string(testMsg) {
			t.Errorf("Expected message %s, got %s", string(testMsg), string(message))
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for message")
	}
}
