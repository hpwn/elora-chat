package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var (
	// dbPath is the path to the SQLite database file
	dbPath = getDBPath()
)

// getDBPath returns the appropriate database path based on the environment
func getDBPath() string {
	// Check if we're running tests
	if os.Getenv("GO_TESTING") == "1" {
		return "test_chat.db"
	}
	// Check if we're running in a container
	if _, err := os.Stat("/app"); err == nil {
		return "/app/data/chat.db"
	}
	// Use local path for development
	return "chat.db"
}

// DB represents the SQLite database connection
type DB struct {
	*sql.DB
}

// Message represents a chat message
type Message struct {
	ID        int64           `json:"id"`
	Data      json.RawMessage `json:"data"`
	Timestamp int64           `json:"timestamp"`
}

// NewDB creates a new SQLite database connection
func NewDB() (*DB, error) {
	// Ensure the database directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %v", err)
	}

	// Open database connection with WAL mode
	db, err := sql.Open("sqlite3", fmt.Sprintf("%s?_journal=WAL", dbPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %v", err)
	}

	// Create messages table if it doesn't exist
	if err := createMessagesTable(db); err != nil {
		return nil, fmt.Errorf("failed to create messages table: %v", err)
	}

	return &DB{db}, nil
}

// createMessagesTable creates the messages table if it doesn't exist
func createMessagesTable(db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY,
		json BLOB,
		timestamp INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp);
	`

	_, err := db.Exec(query)
	return err
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.DB.Close()
}

// InsertMessage inserts a new message into the database
func (db *DB) InsertMessage(data json.RawMessage) error {
	query := `INSERT INTO messages (json, timestamp) VALUES (?, ?)`
	_, err := db.Exec(query, data, time.Now().Unix())
	return err
}

// GetMessages retrieves messages within a time range
func (db *DB) GetMessages(startTime, endTime int64) ([]Message, error) {
	query := `
		SELECT id, json, timestamp 
		FROM messages 
		WHERE timestamp BETWEEN ? AND ?
		ORDER BY timestamp ASC
	`
	rows, err := db.Query(query, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %v", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.ID, &msg.Data, &msg.Timestamp); err != nil {
			return nil, fmt.Errorf("failed to scan message: %v", err)
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating messages: %v", err)
	}

	return messages, nil
}

// GetLatestMessages retrieves the most recent messages
func (db *DB) GetLatestMessages(limit int) ([]Message, error) {
	query := `
		SELECT id, json, timestamp 
		FROM messages 
		ORDER BY timestamp DESC 
		LIMIT ?
	`
	rows, err := db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query latest messages: %v", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.ID, &msg.Data, &msg.Timestamp); err != nil {
			return nil, fmt.Errorf("failed to scan message: %v", err)
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating messages: %v", err)
	}

	return messages, nil
}
