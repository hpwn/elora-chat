package storage

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

const testDBPath = "test_chat.db"

func cleanup() {
	os.Remove(testDBPath)
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
	db := setupTest(t)
	defer db.Close()

	// Verify the database is accessible
	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}

	// Verify the messages table exists
	var tableName string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='messages'").Scan(&tableName)
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
	db := setupTest(t)
	defer db.Close()

	// Get current time before insertion
	beforeInsert := time.Now().Unix()
	time.Sleep(time.Millisecond * 100) // Small delay to ensure timestamp difference

	// Test message insertion
	testData := json.RawMessage(`{"platform": "test", "message": "hello"}`)
	if err := db.InsertMessage(testData); err != nil {
		t.Fatalf("Failed to insert message: %v", err)
	}

	time.Sleep(time.Millisecond * 100) // Small delay to ensure timestamp difference
	afterInsert := time.Now().Unix()

	// Test retrieving latest messages
	messages, err := db.GetLatestMessages(1)
	if err != nil {
		t.Fatalf("Failed to get latest messages: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}
	if string(messages[0].Data) != string(testData) {
		t.Errorf("Expected message data %s, got %s", testData, messages[0].Data)
	}

	// Test retrieving messages by time range
	messages, err = db.GetMessages(beforeInsert, afterInsert)
	if err != nil {
		t.Fatalf("Failed to get messages by time range: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("Expected 1 message in time range, got %d", len(messages))
	}
	if string(messages[0].Data) != string(testData) {
		t.Errorf("Expected message data %s, got %s", testData, messages[0].Data)
	}

	// Test retrieving messages outside time range
	messages, err = db.GetMessages(0, beforeInsert-1)
	if err != nil {
		t.Fatalf("Failed to get messages outside time range: %v", err)
	}
	if len(messages) != 0 {
		t.Errorf("Expected 0 messages outside time range, got %d", len(messages))
	}
}
