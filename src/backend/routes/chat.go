package routes

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/hpwn/EloraChat/src/backend/internal/authutil"
	"github.com/hpwn/EloraChat/src/backend/internal/storage"
	"github.com/hpwn/EloraChat/src/backend/internal/tokenfile"
	"github.com/jdavasligil/emodl"
)

var chatStore storage.Store
var ctx = context.Background()
var subscribersMu sync.Mutex
var subscribers map[chan []byte]struct{}

type CmdMap struct {
	data sync.Map
}

func (m *CmdMap) Store(key string, cmd *exec.Cmd) {
	m.data.Store(key, cmd)
}

func (m *CmdMap) Range(f func(key string, cmd *exec.Cmd) bool) {
	m.data.Range(func(key, value any) bool {
		return f(key.(string), value.(*exec.Cmd))
	})
}

var chatFetchCmds CmdMap

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for simplicity; adjust as needed for security
	},
}

var tokenizer Tokenizer

var commandParser CommandParser

// TODO: replace with table in SQLite
var userColorMap map[string]string = make(map[string]string)

type Image struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	ID     string `json:"id"`
}

type Emote struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Locations []string `json:"locations"`
	Images    []Image  `json:"images"`
}

type Badge struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
}

func (b *Badge) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*b = Badge{}
		return nil
	}

	if trimmed[0] == '"' {
		var entry string
		if err := json.Unmarshal(trimmed, &entry); err != nil {
			return err
		}
		entry = strings.TrimSpace(entry)
		if entry == "" {
			*b = Badge{}
			return nil
		}
		badge := Badge{ID: entry}
		if idx := strings.Index(entry, "/"); idx >= 0 {
			badge.ID = strings.TrimSpace(entry[:idx])
			badge.Version = strings.TrimSpace(entry[idx+1:])
		}
		*b = badge
		return nil
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		*b = Badge{}
		return nil
	}

	get := func(keys ...string) string {
		for _, key := range keys {
			if v, ok := obj[key]; ok {
				switch val := v.(type) {
				case string:
					if s := strings.TrimSpace(val); s != "" {
						return s
					}
				case json.Number:
					if s := strings.TrimSpace(val.String()); s != "" {
						return s
					}
				}
			}
		}
		return ""
	}

	id := get("id", "badge_id", "name", "slug", "_id")
	version := get("version", "badge_version")
	*b = Badge{ID: id, Version: version}
	return nil
}

type Message struct {
	Author  string  `json:"author"` // Adjusted to directly receive the author's name as a string
	Message string  `json:"message"`
	Tokens  []Token `json:"fragments"`
	Emotes  []Emote `json:"emotes"`
	Badges  []Badge `json:"badges"`
	Source  string  `json:"source"`
	Colour  string  `json:"colour"`
}

var fallbackColourPalette = []string{
	"#f97316",
	"#22d3ee",
	"#c084fc",
	"#34d399",
	"#facc15",
	"#38bdf8",
	"#f472b6",
	"#a3e635",
}

func colorFromName(name string) string {
	if name == "" {
		return "#94a3b8"
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(name)))
	sum := h.Sum32()
	idx := int(sum % uint32(len(fallbackColourPalette)))
	return fallbackColourPalette[idx]
}

func parseStoredBadges(raw string) []Badge {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var entries []string
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil
	}
	out := make([]Badge, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		badge := Badge{}
		if idx := strings.Index(entry, "/"); idx >= 0 {
			badge.ID = strings.TrimSpace(entry[:idx])
			badge.Version = strings.TrimSpace(entry[idx+1:])
		} else {
			badge.ID = entry
		}
		if badge.ID == "" {
			continue
		}
		if badge.Version == "" {
			badge.Version = ""
		}
		out = append(out, badge)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func encodeStoredBadges(badges []Badge) string {
	if len(badges) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(badges))
	for _, badge := range badges {
		id := strings.TrimSpace(badge.ID)
		if id == "" {
			continue
		}
		version := strings.TrimSpace(badge.Version)
		if version != "" {
			parts = append(parts, fmt.Sprintf("%s/%s", id, version))
		} else {
			parts = append(parts, id)
		}
	}
	if len(parts) == 0 {
		return "[]"
	}
	data, err := json.Marshal(parts)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func wsEnvelopeEnabled() bool {
	raw := strings.TrimSpace(os.Getenv("ELORA_WS_ENVELOPE"))
	if raw == "" {
		return true
	}

	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func maybeEnvelope(b []byte) []byte {
	if !wsEnvelopeEnabled() {
		return b
	}

	env := struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}{
		Type: "chat",
		Data: string(b),
	}

	out, err := json.Marshal(env)
	if err != nil {
		log.Printf("ws: failed to marshal envelope: %v", err)
		return b
	}

	return out
}

func messagePayloadFromStorage(m storage.Message) ([]byte, error) {
	if strings.TrimSpace(m.RawJSON) != "" {
		return []byte(m.RawJSON), nil
	}

	badges := parseStoredBadges(m.BadgesJSON)
	if badges == nil {
		badges = []Badge{}
	}

	fallback := Message{
		Author:  m.Username,
		Message: m.Text,
		Tokens:  []Token{},
		Emotes:  []Emote{},
		Badges:  badges,
		Source:  m.Platform,
		Colour:  userColorMap[m.Username],
	}

	data, err := json.Marshal(fallback)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func sanitizeMessagePayload(payload []byte) ([]byte, error) {
	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		return nil, err
	}
	if msg.Tokens == nil {
		msg.Tokens = []Token{}
	}
	if msg.Emotes == nil {
		msg.Emotes = []Emote{}
	}
	if msg.Badges == nil {
		msg.Badges = []Badge{}
	}

	return json.Marshal(msg)
}

func InitRoutes(store storage.Store) {
	if store == nil {
		log.Fatalf("storage: store is nil")
	}

	chatStore = store
	maybeExportStoredTwitchToken(store)
	subscribersMu.Lock()
	if subscribers == nil {
		subscribers = make(map[chan []byte]struct{})
	}
	subscribersMu.Unlock()

	// Initialize tokenizer
	tokenizer.TextEffectSep = ':'
	tokenizer.TextCommandPrefix = '!'

	// Initialize command parser
	// TODO: Replace hardcoded timer duration with config setting
	commandParser = CommandParser{
		HelpTimer:         time.NewTimer(10 * time.Second),
		HelpResetDuration: 10 * time.Second,
	}

	// Load third party emotes
	downloader := emodl.NewDownloader(emodl.DownloaderOptions{
		// TEMP: (Dayoman ID hard coded)
		SevenTV: &emodl.SevenTVOptions{
			Platform:   "twitch",
			PlatformID: "39226538",
		},
		BTTV: &emodl.BTTVOptions{
			Platform:   "twitch",
			PlatformID: "39226538",
		},
		FFZ: &emodl.FFZOptions{
			Platform:   "twitch",
			PlatformID: "39226538",
		},
	})
	emoteCacheTmp, err := downloader.Load()
	if err != nil {
		log.Printf("emodl: Failed to load third party emotes: %v", err)
	}
	tokenizer.EmoteCache = make(map[string]Emote, len(emoteCacheTmp))
	for name, emote := range emoteCacheTmp {
		tokenizer.EmoteCache[name] = Emote{
			ID:        emote.ID,
			Name:      emote.Name,
			Locations: emote.Locations,
			Images:    []Image{Image(emote.Images[0])},
		}
	}
	// DEBUG
	// log.Println("3P EMOTES SUPPORTED")
	// for _, e := range tokenizer.EmoteCache {
	// 	log.Println(e.Name)
	// }
}

func maybeExportStoredTwitchToken(store storage.Store) {
	if store == nil || tokenfile.PathFromEnv() == "" {
		return
	}

	export := func(sess *storage.Session) bool {
		if sess == nil || strings.TrimSpace(sess.DataJSON) == "" {
			return false
		}
		token := strings.TrimSpace(authutil.ExtractTwitchToken([]byte(sess.DataJSON)))
		if token == "" {
			return false
		}
		if err := tokenfile.Save(token); err != nil {
			if !errors.Is(err, tokenfile.ErrEmptyToken) {
				log.Printf("auth: twitch token export skipped (%v)", err)
			}
			return true
		}
		log.Printf("auth: twitch token exported to file")
		return true
	}

	if sess, err := store.LatestSessionByService(ctx, "twitch"); err != nil {
		log.Printf("auth: twitch token preload failed: %v", err)
	} else if export(sess) {
		return
	}

	if sess, err := store.LatestSession(ctx); err != nil {
		log.Printf("auth: twitch token preload fallback failed: %v", err)
		return
	} else {
		_ = export(sess)
	}
}

func StartChatFetch(urls []string) {
	pythonExecPath := "/usr/local/bin/python3"
	fetchChatScript := "/app/python/fetch_chat.py"

	for _, url := range urls {
		go monitorAndRestartChatFetch(url, pythonExecPath, fetchChatScript)
	}
}

func monitorAndRestartChatFetch(url, pythonExecPath, fetchChatScript string) {
	for {
		cmd := startChatFetch(url, pythonExecPath, fetchChatScript)
		chatFetchCmds.Store(url, cmd)

		err := cmd.Wait() // Waits for the command to exit
		if err != nil {
			log.Printf("chat: Chat fetch for %s stopped: %v", url, err)
		}

		// Wait for a short duration before restarting to prevent rapid restart loops
		log.Println("chat: Restarting chat fetch...")
		time.Sleep(1 * time.Second)
	}
}

func startChatFetch(url, pythonExecPath, fetchChatScript string) *exec.Cmd {
	cmd := exec.Command(pythonExecPath, "-u", fetchChatScript, url)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal("chat: Failed to create stdout pipe:", err)
	}

	if err := cmd.Start(); err != nil {
		log.Fatal("chat: Failed to start command:", err)
	}

	log.Println("chat: Fetching chat from URL: ", url)

	go processChatOutput(stdout, url)
	return cmd
}

func processChatOutput(stdout io.ReadCloser, url string) {
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		var msg Message
		var err error
		rawMessage := scanner.Bytes()
		if err := json.Unmarshal(rawMessage, &msg); err != nil {
			log.Printf("chat: Failed to unmarshal message: %v, Raw message: %s\n", err, string(rawMessage))
			continue
		}
		if strings.Contains(url, "twitch.tv") {
			msg.Source = "Twitch"
		} else if strings.Contains(url, "youtube.com") {
			msg.Source = "YouTube"
		}

		// Add unknown emotes to the emote cache for tokenization
		for _, e := range msg.Emotes {
			tokenizer.EmoteCache[e.Name] = e
		}

		// Tokenize message
		msg.Tokens = make([]Token, 0)
		for token := range tokenizer.Iter(msg.Message) {
			msg.Tokens = append(msg.Tokens, token)
		}

		// Process command
		if len(msg.Tokens) > 0 && msg.Tokens[0].Type == TokenTypeCommand {
			msg, err = commandParser.Parse(msg, userColorMap)
			if err != nil {
				log.Printf("chat: Failed to process command: %v, Message: %#v\n", err, msg)
			}
		}

		// Apply user preferences
		// TODO: Replace map lookup with db query
		if _, ok := userColorMap[msg.Author]; ok {
			msg.Colour = userColorMap[msg.Author]
		}

		// Prevent nil slices
		if msg.Emotes == nil {
			msg.Emotes = []Emote{}
		}
		if msg.Badges == nil {
			msg.Badges = []Badge{}
		}

		// Re-marshal the message with the Source set.
		modifiedMessage, err := json.Marshal(msg)
		if err != nil {
			log.Printf("chat: Failed to marshal message: %v, Message: %#v\n", err, msg)
			continue
		}

		if chatStore != nil {
			emotesJSON, err := json.Marshal(msg.Emotes)
			if err != nil {
				log.Printf("storage: Failed to marshal emotes: %v", err)
				emotesJSON = []byte("[]")
			}
			storedMessage := &storage.Message{
				ID:         uuid.NewString(),
				Timestamp:  time.Now().UTC(),
				Username:   msg.Author,
				Platform:   msg.Source,
				Text:       msg.Message,
				EmotesJSON: string(emotesJSON),
				BadgesJSON: encodeStoredBadges(msg.Badges),
				RawJSON:    string(modifiedMessage),
			}
			if err := chatStore.InsertMessage(ctx, storedMessage); err != nil {
				log.Printf("storage: Failed to insert message: %v", err)
			}
		}

		broadcastChatMessage(modifiedMessage)
	}
	if err := scanner.Err(); err != nil {
		log.Println("chat: Error reading standard output:", err)
	}
}

func broadcastChatMessage(msg []byte) {
	subscribersMu.Lock()
	if len(subscribers) == 0 {
		subscribersMu.Unlock()
		return
	}
	targets := make([]chan []byte, 0, len(subscribers))
	for ch := range subscribers {
		targets = append(targets, ch)
	}
	subscribersMu.Unlock()

	for _, ch := range targets {
		payload := make([]byte, len(msg))
		copy(payload, msg)
		select {
		case ch <- payload:
		default:
		}
	}
}

func enrichTailerMessage(m storage.Message) Message {
	var msg Message
	raw := strings.TrimSpace(m.RawJSON)
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			log.Printf("dbtailer: failed to unmarshal raw JSON: %v", err)
			msg = Message{}
		}
	}

	if msg.Author == "" {
		msg.Author = m.Username
	}
	if msg.Message == "" {
		msg.Message = m.Text
	}
	if msg.Source == "" {
		msg.Source = m.Platform
	}

	if msg.Emotes == nil {
		if strings.TrimSpace(m.EmotesJSON) != "" {
			var emotes []Emote
			if err := json.Unmarshal([]byte(m.EmotesJSON), &emotes); err == nil {
				msg.Emotes = emotes
			}
		}
	}
	if msg.Emotes == nil {
		msg.Emotes = []Emote{}
	}

	if tokenizer.EmoteCache == nil {
		tokenizer.EmoteCache = make(map[string]Emote)
	}
	for _, e := range msg.Emotes {
		if e.Name != "" {
			tokenizer.EmoteCache[e.Name] = e
		}
	}

	if msg.Badges == nil {
		if parsed := parseStoredBadges(m.BadgesJSON); parsed != nil {
			msg.Badges = parsed
		}
	}
	if msg.Badges == nil {
		msg.Badges = []Badge{}
	}

	if msg.Tokens == nil {
		msg.Tokens = make([]Token, 0, 8)
	} else {
		msg.Tokens = msg.Tokens[:0]
	}
	for token := range tokenizer.Iter(msg.Message) {
		msg.Tokens = append(msg.Tokens, token)
	}

	if msg.Colour == "" {
		if colour, ok := userColorMap[msg.Author]; ok && colour != "" {
			msg.Colour = colour
		} else {
			msg.Colour = colorFromName(msg.Author)
		}
	}

	if msg.Source == "" {
		msg.Source = m.Platform
	}

	return msg
}

// BroadcastFromTailer enqueues a stored message onto the WebSocket broadcast loop.
func BroadcastFromTailer(m storage.Message) {
	msg := enrichTailerMessage(m)
	payload, err := json.Marshal(msg)
	if err != nil {
		log.Printf("dbtailer: failed to marshal enriched message: %v", err)
		return
	}

	broadcastChatMessage(payload)
}

func addSubscriber() chan []byte {
	ch := make(chan []byte, 64)
	subscribersMu.Lock()
	if subscribers == nil {
		subscribers = make(map[chan []byte]struct{})
	}
	subscribers[ch] = struct{}{}
	subscribersMu.Unlock()
	return ch
}

func removeSubscriber(ch chan []byte) {
	subscribersMu.Lock()
	if subscribers != nil {
		if _, ok := subscribers[ch]; ok {
			delete(subscribers, ch)
			close(ch)
		}
	}
	subscribersMu.Unlock()
}

// StreamChat initializes a WebSocket connection and streams chat messages
func StreamChat(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("ws: WebSocket upgrade error:", err)
		return
	}
	defer conn.Close()

	// Channel to signal closure of WebSocket connection
	done := make(chan struct{})
	messageChan := addSubscriber()
	defer removeSubscriber(messageChan)

	// Read the last 100 messages from the backing store to send to the client immediately.
	if chatStore != nil {
		history, err := chatStore.GetRecent(ctx, storage.QueryOpts{Limit: 100})
		if err != nil {
			log.Printf("storage: Failed to read messages from store: %v\n", err)
		} else {
			for i := len(history) - 1; i >= 0; i-- {
				payload, marshalErr := messagePayloadFromStorage(history[i])
				if marshalErr != nil {
					log.Printf("chat: Failed to marshal history message: %v\n", marshalErr)
					continue
				}
				sanitized, marshalErr := sanitizeMessagePayload(payload)
				if marshalErr != nil {
					log.Printf("chat: Failed to sanitize history message: %v\n", marshalErr)
					continue
				}
				if err := conn.WriteMessage(websocket.TextMessage, maybeEnvelope(sanitized)); err != nil {
					log.Println("ws: WebSocket write error:", err)
					return
				}
			}
		}
	}

	// Websocket writer
	go func() {
		// Keep alive ticker
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case m, ok := <-messageChan:
				if !ok {
					return
				}
				sanitized, err := sanitizeMessagePayload(m)
				if err != nil {
					log.Println("json: ", err)
					continue
				}
				if err := conn.WriteMessage(websocket.TextMessage, maybeEnvelope(sanitized)); err != nil {
					log.Println("ws: WebSocket write error:", err)
					return
				}
			case <-ticker.C:
				if err := conn.WriteMessage(websocket.TextMessage, []byte("__keepalive__")); err != nil {
					log.Println("ws: Failed to send keep-alive message:", err)
					return
				}
			case <-done:
				return
			}
		}
	}()

	// Read loop to keep connection alive and detect close
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNoStatusReceived) {
				log.Println("ws: WebSocket read error, closing connection:", err)
			}
			close(done)
			break
		}
	}
}

func ImageProxy(w http.ResponseWriter, r *http.Request) {
	imageURL := r.URL.Query().Get("url")
	if imageURL == "" {
		http.Error(w, "Missing URL parameter", http.StatusBadRequest)
		return
	}

	resp, err := http.Get(imageURL)
	if err != nil || resp.StatusCode == 404 {
		// Log error and serve a default placeholder image
		log.Printf("http: Error fetching image or not found: %s", imageURL)
		http.ServeFile(w, r, "../public/refresh.png")
		return
	}
	defer resp.Body.Close()

	// Copy headers
	for key, value := range resp.Header {
		w.Header().Set(key, value[0])
	}

	// Stream the image content
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// StopChatFetches stops all ongoing chat fetch commands
func StopChatFetches(w http.ResponseWriter, r *http.Request) {
	chatFetchCmds.Range(func(key string, cmd *exec.Cmd) bool {
		if cmd != nil && cmd.Process != nil {
			err := cmd.Process.Kill()
			if err != nil {
				log.Printf("http: Failed to stop chat fetch command: %v", err)
			}
		}
		return true
	})
	fmt.Fprintln(w, "Chat fetch commands stopped. Restarting...")
}

// SetupChatRoutes sets up WebSocket routes
func SetupChatRoutes(router *mux.Router) {
	// Public routes
	router.HandleFunc("/ws/chat", StreamChat).Methods("GET")
	router.HandleFunc("/imageproxy", ImageProxy).Methods("GET")

	// Subrouter for chat routes that require authentication
	protectedRoutes := router.PathPrefix("").Subrouter()
	protectedRoutes.Use(SessionMiddleware)

	// Add protected chat routes to protectedRoutes
	protectedRoutes.HandleFunc("/restart-server", StopChatFetches).Methods("POST")
}
