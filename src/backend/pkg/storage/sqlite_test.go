package storage

import (
	"encoding/json"
	"os"
	"testing"
)

const testDBPath = "test_chat.db"

func cleanup() {
	os.Remove("test_chat.db")
}

func setupTest(t *testing.T) *DB {
	cleanup()
	db, err := NewDB()
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	// Clear any existing data
	_, err = db.Exec("DELETE FROM messages")
	if err != nil {
		t.Fatalf("Failed to clear messages: %v", err)
	}
	return db
}

func TestMain(m *testing.M) {
	// Set testing environment
	os.Setenv("GO_TESTING", "1")

	// Run tests
	code := m.Run()

	// Cleanup after all tests
	cleanup()
	os.Exit(code)
}

func TestNewDB(t *testing.T) {
	cleanup()
	defer cleanup()

	db, err := NewDB()
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	// Test that we can ping the database
	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}

	// Verify the messages table exists
	var tableName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='messages'").Scan(&tableName)
	if err != nil {
		t.Fatalf("Failed to verify messages table: %v", err)
	}
	if tableName != "messages" {
		t.Errorf("Expected table name 'messages', got '%s'", tableName)
	}

	// Verify the timestamp index exists
	var indexName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name='idx_messages_timestamp'").Scan(&indexName)
	if err != nil {
		t.Fatalf("Failed to verify timestamp index: %v", err)
	}
	if indexName != "idx_messages_timestamp" {
		t.Errorf("Expected index name 'idx_messages_timestamp', got '%s'", indexName)
	}
}

func TestMessageOperations(t *testing.T) {
	cleanup()
	defer cleanup()

	db, err := NewDB()
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	// Test inserting a message
	msg := json.RawMessage(`{"author": "test", "message": "hello"}`)
	if err := db.InsertMessage(msg); err != nil {
		t.Fatalf("Failed to insert message: %v", err)
	}

	// Test retrieving messages
	messages, err := db.GetMessages(0, 9999999999)
	if err != nil {
		t.Fatalf("Failed to get messages: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(messages))
	}

	if string(messages[0].Data) != string(msg) {
		t.Errorf("Expected message data %s, got %s", string(msg), string(messages[0].Data))
	}
}

func TestGetMessagesAfterID(t *testing.T) {
	cleanup()
	defer cleanup()

	db, err := NewDB()
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	// Insert test messages
	messages := []json.RawMessage{
		json.RawMessage(`{"author": "user1", "message": "test1"}`),
		json.RawMessage(`{"author": "user2", "message": "test2"}`),
		json.RawMessage(`{"author": "user3", "message": "test3"}`),
	}

	for _, msg := range messages {
		if err := db.InsertMessage(msg); err != nil {
			t.Fatalf("Failed to insert test message: %v", err)
		}
	}

	// Test getting messages after ID 1
	msgs, err := db.GetMessagesAfterID(1, 2)
	if err != nil {
		t.Fatalf("Failed to get messages after ID: %v", err)
	}

	if len(msgs) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(msgs))
	}

	if msgs[0].ID != 2 {
		t.Errorf("Expected first message ID to be 2, got %d", msgs[0].ID)
	}

	if msgs[1].ID != 3 {
		t.Errorf("Expected second message ID to be 3, got %d", msgs[1].ID)
	}
}

func TestGetLastMessageID(t *testing.T) {
	cleanup()
	defer cleanup()

	db, err := NewDB()
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	// Test empty database
	lastID, err := db.GetLastMessageID()
	if err != nil {
		t.Fatalf("Failed to get last message ID: %v", err)
	}
	if lastID != 0 {
		t.Errorf("Expected last ID to be 0 for empty database, got %d", lastID)
	}

	// Insert test message
	msg := json.RawMessage(`{"author": "user1", "message": "test"}`)
	if err := db.InsertMessage(msg); err != nil {
		t.Fatalf("Failed to insert test message: %v", err)
	}

	// Test with one message
	lastID, err = db.GetLastMessageID()
	if err != nil {
		t.Fatalf("Failed to get last message ID: %v", err)
	}
	if lastID != 1 {
		t.Errorf("Expected last ID to be 1, got %d", lastID)
	}
}
