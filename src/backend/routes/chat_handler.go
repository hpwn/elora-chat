package routes

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/websocket"
	"github.com/hpwn/EloraChat/src/backend/pkg/storage"
	"github.com/redis/go-redis/v9"
)

// ChatHandler handles WebSocket connections and message streaming
type ChatHandler struct {
	db          *storage.DB
	upgrader    websocket.Upgrader
	redisClient RedisClient
}

// NewChatHandler creates a new ChatHandler instance
func NewChatHandler(db *storage.DB, redisClient RedisClient) *ChatHandler {
	return &ChatHandler{
		db: db,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for simplicity; adjust as needed for security
			},
		},
		redisClient: redisClient,
	}
}

// HandleWebSocket handles WebSocket connections for chat streaming
func (h *ChatHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("ws: WebSocket upgrade error:", err)
		return
	}
	defer conn.Close()

	// Channel to signal closure of WebSocket connection
	done := make(chan struct{})
	defer close(done)

	// Get the last 100 messages from SQLite
	lastMessages, err := h.db.GetLatestMessages(100)
	if err != nil {
		log.Printf("Failed to get latest messages from SQLite: %v", err)
		return
	}

	// Send historical messages to the client
	for i := len(lastMessages) - 1; i >= 0; i-- {
		if err := conn.WriteMessage(websocket.TextMessage, lastMessages[i].Data); err != nil {
			log.Println("ws: WebSocket write error:", err)
			return
		}
	}

	// Get the last message ID for real-time updates
	lastID := "0"
	if len(lastMessages) > 0 {
		lastID = strconv.FormatInt(lastMessages[len(lastMessages)-1].ID, 10)
	}

	// Start a goroutine to read new messages from Redis stream
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				streams, err := h.redisClient.XRead(context.Background(), &redis.XReadArgs{
					Streams: []string{"chatMessages", lastID},
					Block:   0,
				}).Result()

				if err != nil {
					log.Println("redis: Error reading from stream:", err)
					return
				}

				for _, stream := range streams {
					for _, message := range stream.Messages {
						// Send message to WebSocket client
						if err := conn.WriteMessage(websocket.TextMessage, []byte(message.Values["message"].(string))); err != nil {
							log.Println("ws: WebSocket write error:", err)
							return
						}
						lastID = message.ID
					}
				}
			}
		}
	}()

	// Keep the connection alive until the client disconnects
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

// ProcessMessage processes a new chat message and stores it in both Redis and SQLite
func (h *ChatHandler) ProcessMessage(msg []byte) error {
	// Store in SQLite
	if err := h.db.InsertMessage(json.RawMessage(msg)); err != nil {
		log.Printf("Failed to store message in SQLite: %v", err)
		return err
	}

	// Add to Redis stream for real-time delivery
	_, err := h.redisClient.XAdd(context.Background(), &redis.XAddArgs{
		Stream: "chatMessages",
		Values: map[string]any{"message": string(msg)},
		MaxLen: 100,
		Approx: true,
	}).Result()

	if err != nil {
		log.Printf("Failed to add message to Redis stream: %v", err)
		return err
	}

	return nil
}
