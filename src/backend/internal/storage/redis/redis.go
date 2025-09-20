package redisstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
	"github.com/redis/go-redis/v9"
)

// Config configures the Redis-backed store.
type Config struct {
	Client *redis.Client
	Stream string
	MaxLen int64
}

// Store implements storage.Store using Redis streams.
type Store struct {
	client *redis.Client
	stream string
	maxLen int64
}

func sessionKey(token string) string {
	return "session:" + token
}

// New creates a new Redis-backed store.
func New(cfg Config) *Store {
	maxLen := cfg.MaxLen
	if maxLen <= 0 {
		maxLen = 100
	}
	stream := cfg.Stream
	if stream == "" {
		stream = "chatMessages"
	}
	return &Store{
		client: cfg.Client,
		stream: stream,
		maxLen: maxLen,
	}
}

// Init ensures the Redis connection is available.
func (s *Store) Init(ctx context.Context) error {
	if s.client == nil {
		return errors.New("redis: client is nil")
	}
	if err := s.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: ping: %w", err)
	}
	return nil
}

// InsertMessage appends a chat message to the Redis stream.
func (s *Store) InsertMessage(ctx context.Context, m *storage.Message) error {
	if s.client == nil {
		return errors.New("redis: client is nil")
	}
	if m == nil {
		return errors.New("redis: message is nil")
	}

	timestamp := m.Timestamp.UTC().UnixMilli()
	values := map[string]any{
		"id":          m.ID,
		"ts":          strconv.FormatInt(timestamp, 10),
		"username":    m.Username,
		"platform":    m.Platform,
		"text":        m.Text,
		"emotes_json": m.EmotesJSON,
		"raw_json":    m.RawJSON,
		"message":     m.RawJSON,
	}

	if _, err := s.client.XAdd(ctx, &redis.XAddArgs{
		Stream: s.stream,
		MaxLen: s.maxLen,
		Approx: true,
		Values: values,
	}).Result(); err != nil {
		return fmt.Errorf("redis: xadd: %w", err)
	}
	return nil
}

// GetRecent fetches the most recent messages from the Redis stream.
func (s *Store) GetRecent(ctx context.Context, q storage.QueryOpts) ([]storage.Message, error) {
	if s.client == nil {
		return nil, errors.New("redis: client is nil")
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}

	entries, err := s.client.XRevRangeN(ctx, s.stream, "+", "-", int64(limit)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("redis: xrevrange: %w", err)
	}

	var results []storage.Message
	sinceTS := int64(0)
	if q.SinceTS != nil {
		sinceTS = q.SinceTS.UTC().UnixMilli()
	}

	for _, entry := range entries {
		msg, err := s.entryToMessage(entry)
		if err != nil {
			log.Printf("redis: skipping malformed entry: %v", err)
			continue
		}
		if sinceTS > 0 && msg.Timestamp.UTC().UnixMilli() < sinceTS {
			continue
		}
		results = append(results, msg)
	}

	return results, nil
}

// PurgeAll removes the Redis stream.
func (s *Store) PurgeAll(ctx context.Context) error {
	if s.client == nil {
		return errors.New("redis: client is nil")
	}
	if err := s.client.Del(ctx, s.stream).Err(); err != nil {
		return fmt.Errorf("redis: del stream: %w", err)
	}
	return nil
}

// GetSession fetches a session record using the legacy JSON payload layout.
func (s *Store) GetSession(ctx context.Context, token string) (*storage.Session, error) {
	if s.client == nil {
		return nil, errors.New("redis: client is nil")
	}

	raw, err := s.client.Get(ctx, sessionKey(token)).Result()
	if err != nil {
		return nil, err
	}

	sess := &storage.Session{Token: token, DataJSON: raw}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err == nil {
		if svc, ok := payload["service"].(string); ok {
			sess.Service = svc
		}
		if exp, ok := toUnix(payload["token_expiry"]); ok {
			sess.TokenExpiry = time.Unix(exp, 0).UTC()
		}
		if upd, ok := toUnix(payload["updated_at"]); ok {
			sess.UpdatedAt = time.Unix(upd, 0).UTC()
		}
	}

	if sess.TokenExpiry.IsZero() {
		sess.TokenExpiry = time.Now().UTC()
	}
	if sess.UpdatedAt.IsZero() {
		sess.UpdatedAt = time.Now().UTC()
	}

	return sess, nil
}

// UpsertSession writes the session JSON blob using the legacy key layout.
func (s *Store) UpsertSession(ctx context.Context, sess *storage.Session) error {
	if s.client == nil {
		return errors.New("redis: client is nil")
	}
	if sess == nil {
		return errors.New("redis: session is nil")
	}
	ttl := 24 * time.Hour
	if sess.UpdatedAt.IsZero() {
		sess.UpdatedAt = time.Now().UTC()
	}

	// Ensure the JSON payload carries updated_at for downstream consumers when possible.
	if sess.DataJSON != "" {
		var payload map[string]any
		if err := json.Unmarshal([]byte(sess.DataJSON), &payload); err == nil {
			payload["updated_at"] = sess.UpdatedAt.UTC().Unix()
			if payloadSvc, ok := payload["service"].(string); !ok || payloadSvc == "" {
				payload["service"] = sess.Service
			}
			if _, ok := payload["token_expiry"]; !ok && !sess.TokenExpiry.IsZero() {
				payload["token_expiry"] = sess.TokenExpiry.UTC().Unix()
			}
			if encoded, err := json.Marshal(payload); err == nil {
				sess.DataJSON = string(encoded)
			}
		}
	}

	return s.client.Set(ctx, sessionKey(sess.Token), sess.DataJSON, ttl).Err()
}

// DeleteSession removes a stored session key.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	if s.client == nil {
		return errors.New("redis: client is nil")
	}
	return s.client.Del(ctx, sessionKey(token)).Err()
}

func toUnix(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		return int64(v), true
	case float32:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

// Close is a no-op for the Redis store since the client is managed externally.
func (s *Store) Close(ctx context.Context) error {
	return nil
}

func (s *Store) entryToMessage(entry redis.XMessage) (storage.Message, error) {
	values := entry.Values
	msg := storage.Message{}
	if id, ok := values["id"].(string); ok {
		msg.ID = id
	}
	if tsStr, ok := values["ts"].(string); ok {
		if ts, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
			msg.Timestamp = time.UnixMilli(ts).UTC()
		} else {
			return storage.Message{}, fmt.Errorf("invalid timestamp %q: %w", tsStr, err)
		}
	} else {
		msg.Timestamp = time.Now().UTC()
	}
	if username, ok := values["username"].(string); ok {
		msg.Username = username
	}
	if platform, ok := values["platform"].(string); ok {
		msg.Platform = platform
	}
	if text, ok := values["text"].(string); ok {
		msg.Text = text
	}
	if emotes, ok := values["emotes_json"].(string); ok {
		msg.EmotesJSON = emotes
	}
	if raw, ok := values["raw_json"].(string); ok && raw != "" {
		msg.RawJSON = raw
	} else if rawMessage, ok := values["message"].(string); ok {
		msg.RawJSON = rawMessage
	}
	if msg.ID == "" {
		msg.ID = entry.ID
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now().UTC()
	}
	return msg, nil
}
