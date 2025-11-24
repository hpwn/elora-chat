package routes

import (
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/hpwn/EloraChat/src/backend/internal/authutil"
	"github.com/hpwn/EloraChat/src/backend/internal/storage"
	"github.com/hpwn/EloraChat/src/backend/internal/tokenfile"
	"github.com/hpwn/EloraChat/src/backend/internal/ws"
	"github.com/jdavasligil/emodl"
)

var chatStore storage.Store
var ctx = context.Background()
var subscribersMu sync.Mutex
var subscribers map[chan []byte]struct{}

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return originAllowed(r.Header.Get("Origin"))
		},
	}

	allowedOriginsMu sync.RWMutex
	allowAllOrigins  = true
	allowedOrigins   = map[string]struct{}{}
)

type websocketConfig struct {
	pingInterval  time.Duration
	pongWait      time.Duration
	writeDeadline time.Duration
	maxBytes      int64
}

// WebsocketRuntimeConfig exposes the runtime websocket tuning knobs.
type WebsocketRuntimeConfig struct {
	PingInterval  time.Duration
	PongWait      time.Duration
	WriteDeadline time.Duration
	MaxMessage    int64
}

type uiConfig struct {
	hideYouTubeAt bool
	showBadges    bool
}

var (
	activeWebsocketConfig = websocketConfig{
		pingInterval:  25 * time.Second,
		pongWait:      30 * time.Second,
		writeDeadline: 5 * time.Second,
		maxBytes:      131072,
	}
	activeUIConfig = uiConfig{
		hideYouTubeAt: true,
		showBadges:    true,
	}
)

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
	ID       string  `json:"id"`
	Platform string  `json:"platform,omitempty"`
	Version  string  `json:"version,omitempty"`
	Images   []Image `json:"images,omitempty"`
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

	parseImages := func(raw any) []Image {
		arr, ok := raw.([]any)
		if !ok {
			return nil
		}
		out := make([]Image, 0, len(arr))
		for _, entry := range arr {
			if entry == nil {
				continue
			}
			rec, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			url, _ := rec["url"].(string)
			url = strings.TrimSpace(url)
			if url == "" {
				continue
			}

			width := 0
			height := 0
			switch v := rec["width"].(type) {
			case json.Number:
				if i, err := v.Int64(); err == nil {
					width = int(i)
				}
			case float64:
				width = int(v)
			case int64:
				width = int(v)
			case int:
				width = v
			}
			switch v := rec["height"].(type) {
			case json.Number:
				if i, err := v.Int64(); err == nil {
					height = int(i)
				}
			case float64:
				height = int(v)
			case int64:
				height = int(v)
			case int:
				height = v
			}

			id := ""
			if rawID, ok := rec["id"].(string); ok {
				id = strings.TrimSpace(rawID)
			}

			out = append(out, Image{URL: url, Width: width, Height: height, ID: id})
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}

	id := get("id", "badge_id", "name", "slug", "_id")
	version := get("version", "badge_version")
	platform := get("platform")
	images := parseImages(obj["images"])
	if len(images) == 0 {
		*b = Badge{ID: id, Platform: platform, Version: version}
		return nil
	}
	*b = Badge{ID: id, Platform: platform, Version: version, Images: images}
	return nil
}

type Message struct {
	Author    string  `json:"author"` // Adjusted to directly receive the author's name as a string
	Message   string  `json:"message"`
	Tokens    []Token `json:"fragments"`
	Emotes    []Emote `json:"emotes"`
	Badges    []Badge `json:"badges"`
	BadgesRaw any     `json:"badges_raw,omitempty"`
	Source    string  `json:"source"`
	Colour    string  `json:"colour"`
}

var errDropMessage = errors.New("chat: drop empty message")

func normalizeSource(src string) string {
	src = strings.TrimSpace(src)
	switch strings.ToLower(src) {
	case "twitch":
		return "Twitch"
	case "youtube":
		return "YouTube"
	default:
		return src
	}
}

func wsDropEmptyEnabled() bool {
	raw := strings.TrimSpace(os.Getenv("ELORA_WS_DROP_EMPTY"))
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

func (m *Message) normalize() {
	if m == nil {
		return
	}

	m.Author = strings.TrimSpace(m.Author)
	m.Message = strings.TrimSpace(m.Message)
	m.Source = normalizeSource(m.Source)

	if m.Tokens == nil {
		m.Tokens = []Token{}
	}
	if m.Emotes == nil {
		m.Emotes = []Emote{}
	}
	if m.Badges == nil {
		m.Badges = []Badge{}
	}
	if !activeUIConfig.showBadges {
		m.Badges = []Badge{}
		m.BadgesRaw = nil
	}

	if activeUIConfig.hideYouTubeAt && strings.EqualFold(m.Source, "YouTube") {
		if strings.HasPrefix(m.Author, "@") {
			m.Author = strings.TrimPrefix(m.Author, "@")
			m.Author = strings.TrimSpace(m.Author)
		}
	}
}

func (m Message) toChatPayload() ws.ChatPayload {
	m.normalize()

	fragments := make([]any, len(m.Tokens))
	for i, token := range m.Tokens {
		fragments[i] = token
	}

	emotes := make([]any, len(m.Emotes))
	for i, emote := range m.Emotes {
		emotes[i] = emote
	}

	badges := make([]any, len(m.Badges))
	for i, badge := range m.Badges {
		badges[i] = badge
	}

	return ws.ChatPayload{
		Author:    m.Author,
		Message:   m.Message,
		Fragments: fragments,
		Emotes:    emotes,
		Badges:    badges,
		BadgesRaw: m.BadgesRaw,
		Source:    m.Source,
		Colour:    m.Colour,
	}
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

func parseStoredBadges(raw string) ([]Badge, any) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	decodeBadges := func(in []Badge) []Badge {
		out := make([]Badge, 0, len(in))
		for _, badge := range in {
			badge.ID = strings.TrimSpace(badge.ID)
			badge.Platform = strings.TrimSpace(badge.Platform)
			badge.Version = strings.TrimSpace(badge.Version)
			if len(badge.Images) > 0 {
				filtered := make([]Image, 0, len(badge.Images))
				for _, img := range badge.Images {
					img.URL = strings.TrimSpace(img.URL)
					if img.URL == "" {
						continue
					}
					filtered = append(filtered, img)
				}
				if len(filtered) == 0 {
					badge.Images = nil
				} else {
					badge.Images = filtered
				}
			}
			if badge.ID == "" {
				continue
			}
			out = append(out, badge)
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}

	if strings.HasPrefix(raw, "{") {
		var container struct {
			Badges []Badge         `json:"badges"`
			Raw    json.RawMessage `json:"raw"`
		}
		if err := json.Unmarshal([]byte(raw), &container); err == nil {
			badges := decodeBadges(container.Badges)
			var rawAny any
			if len(container.Raw) > 0 {
				if err := json.Unmarshal(container.Raw, &rawAny); err != nil {
					rawAny = json.RawMessage(container.Raw)
				}
			}
			if badges != nil {
				return badges, rawAny
			}
			if rawAny != nil {
				return nil, rawAny
			}
		}
	}

	var entries []string
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, nil
	}
	out := make([]Badge, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		badge := Badge{Platform: "twitch"}
		if idx := strings.Index(entry, "/"); idx >= 0 {
			badge.ID = strings.TrimSpace(entry[:idx])
			badge.Version = strings.TrimSpace(entry[idx+1:])
		} else {
			badge.ID = entry
		}
		if badge.ID == "" {
			continue
		}
		badge.Version = strings.TrimSpace(badge.Version)
		out = append(out, badge)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
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
		var msg Message
		if err := json.Unmarshal([]byte(m.RawJSON), &msg); err == nil {
			if badges, raw := parseStoredBadges(m.BadgesJSON); badges != nil || raw != nil {
				if msg.Badges == nil || len(msg.Badges) == 0 {
					msg.Badges = badges
					msg.BadgesRaw = raw
				} else if msg.BadgesRaw == nil {
					msg.BadgesRaw = raw
				}
			}
			return json.Marshal(msg.toChatPayload())
		}
	}

	badges, badgesRaw := parseStoredBadges(m.BadgesJSON)
	if badges == nil {
		badges = []Badge{}
	}

	fallback := Message{
		Author:    m.Username,
		Message:   m.Text,
		Tokens:    []Token{},
		Emotes:    []Emote{},
		Badges:    badges,
		BadgesRaw: badgesRaw,
		Source:    m.Platform,
		Colour:    userColorMap[m.Username],
	}
	fallback.normalize()

	data, err := json.Marshal(fallback.toChatPayload())
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
	msg.normalize()

	if wsDropEmptyEnabled() && (msg.Source == "" || msg.Message == "") {
		return nil, errDropMessage
	}

	return json.Marshal(msg.toChatPayload())
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

	activeWebsocketConfig = loadWebsocketConfigFromEnv()
	activeUIConfig = loadUIConfigFromEnv()

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
	log.Printf("emodl: cache size = %d", len(tokenizer.EmoteCache))
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
		case <-time.After(2 * time.Second):
			log.Printf("ws: subscriber stalled; dropping connection")
			removeSubscriber(ch)
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

	if parsed, raw := parseStoredBadges(m.BadgesJSON); parsed != nil || raw != nil {
		if msg.Badges == nil || len(msg.Badges) == 0 {
			msg.Badges = parsed
			msg.BadgesRaw = raw
		} else if msg.BadgesRaw == nil {
			msg.BadgesRaw = raw
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

	// Fallback: decode Twitch first-party emote spans -> Emote slice, then seed cache.
	// This runs only if we don't already have emotes on the message.
	if strings.EqualFold(m.Platform, "twitch") && len(msg.Emotes) == 0 {
		if dec := decodeTwitchSpans(m.Text, m.EmotesJSON); len(dec) > 0 {
			// Seed tokenizer cache by NAME so tokenization emits emote fragments.
			for _, e := range dec {
				if strings.TrimSpace(e.Name) != "" {
					tokenizer.EmoteCache[e.Name] = e
				}
			}
			msg.Emotes = append(msg.Emotes, dec...)
		}
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

	msg.Source = normalizeSource(msg.Source)
	msg.normalize()

	return msg
}

// BroadcastFromTailer enqueues a stored message onto the WebSocket broadcast loop.
func BroadcastFromTailer(m storage.Message) {
	msg := enrichTailerMessage(m)
	if wsDropEmptyEnabled() && (msg.Source == "" || msg.Message == "") {
		return
	}
	payload, err := json.Marshal(msg.toChatPayload())
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

	cfg := activeWebsocketConfig
	sourceFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source")))
	if cfg.maxBytes > 0 {
		conn.SetReadLimit(cfg.maxBytes)
	}
	if cfg.pongWait > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(cfg.pongWait))
	}
	conn.SetPongHandler(func(string) error {
		if cfg.pongWait > 0 {
			return conn.SetReadDeadline(time.Now().Add(cfg.pongWait))
		}
		return nil
	})
	conn.SetPingHandler(func(appData string) error {
		deadline := time.Now().Add(cfg.writeDeadline)
		if cfg.writeDeadline <= 0 {
			deadline = time.Now().Add(5 * time.Second)
		}
		if cfg.pongWait > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(cfg.pongWait))
		}
		return conn.WriteControl(websocket.PongMessage, []byte(appData), deadline)
	})

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
					if errors.Is(marshalErr, errDropMessage) {
						continue
					}
					log.Printf("chat: Failed to sanitize history message: %v\n", marshalErr)
					continue
				}
				if shouldSkipSource(sanitized, sourceFilter) {
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
		ticker := time.NewTicker(cfg.pingInterval)
		defer ticker.Stop()

		for {
			select {
			case m, ok := <-messageChan:
				if !ok {
					return
				}
				sanitized, err := sanitizeMessagePayload(m)
				if err != nil {
					if errors.Is(err, errDropMessage) {
						continue
					}
					log.Println("json: ", err)
					continue
				}
				if shouldSkipSource(sanitized, sourceFilter) {
					continue
				}
				if err := writeWSMessage(conn, websocket.TextMessage, maybeEnvelope(sanitized), cfg.writeDeadline); err != nil {
					log.Println("ws: WebSocket write error:", err)
					return
				}
			case <-ticker.C:
				if err := writeWSMessage(conn, websocket.TextMessage, []byte("__keepalive__"), cfg.writeDeadline); err != nil {
					log.Println("ws: Failed to send keep-alive message:", err)
					return
				}
				deadline := time.Now().Add(cfg.writeDeadline)
				if cfg.writeDeadline <= 0 {
					deadline = time.Now().Add(5 * time.Second)
				}
				if err := conn.WriteControl(websocket.PingMessage, []byte{}, deadline); err != nil {
					log.Println("ws: Failed to send ping:", err)
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
		if cfg.pongWait > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(cfg.pongWait))
		}
	}
}

func writeWSMessage(conn *websocket.Conn, messageType int, payload []byte, deadline time.Duration) error {
	if deadline > 0 {
		_ = conn.SetWriteDeadline(time.Now().Add(deadline))
	} else {
		_ = conn.SetWriteDeadline(time.Time{})
	}
	return conn.WriteMessage(messageType, payload)
}

func shouldSkipSource(payload []byte, filter string) bool {
	if filter == "" {
		return false
	}

	var msg ws.ChatPayload
	if err := json.Unmarshal(payload, &msg); err != nil {
		return false
	}

	source := strings.ToLower(strings.TrimSpace(msg.Source))
	return source != filter
}

func originAllowed(origin string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return true
	}
	origin = strings.TrimRight(origin, "/")

	allowedOriginsMu.RLock()
	defer allowedOriginsMu.RUnlock()

	if allowAllOrigins {
		return true
	}
	_, ok := allowedOrigins[origin]
	return ok
}

// SetAllowedOrigins updates the accepted Origin headers for WebSocket connections.
func SetAllowedOrigins(origins []string) {
	allowedOriginsMu.Lock()
	defer allowedOriginsMu.Unlock()

	if len(origins) == 0 {
		allowAllOrigins = true
		allowedOrigins = map[string]struct{}{}
		return
	}

	allowAllOrigins = false
	allowedOrigins = make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		if trimmed == "*" {
			allowAllOrigins = true
			allowedOrigins = map[string]struct{}{}
			return
		}
		trimmed = strings.TrimRight(trimmed, "/")
		allowedOrigins[trimmed] = struct{}{}
	}
	if len(allowedOrigins) == 0 {
		allowAllOrigins = true
	}
}

func loadWebsocketConfigFromEnv() websocketConfig {
	cfg := websocketConfig{
		pingInterval:  durationFromEnv("ELORA_WS_PING_INTERVAL_MS", 25000),
		pongWait:      durationFromEnv("ELORA_WS_PONG_WAIT_MS", 30000),
		writeDeadline: durationFromEnv("ELORA_WS_WRITE_DEADLINE_MS", 5000),
		maxBytes:      int64FromEnv("ELORA_WS_MAX_MESSAGE_BYTES", 131072),
	}
	if cfg.pingInterval <= 0 {
		cfg.pingInterval = 25 * time.Second
	}
	if cfg.pongWait <= 0 {
		cfg.pongWait = 30 * time.Second
	}
	if cfg.writeDeadline < 0 {
		cfg.writeDeadline = 0
	}
	if cfg.maxBytes <= 0 {
		cfg.maxBytes = 131072
	}
	return cfg
}

// WebsocketConfig returns the currently active websocket runtime configuration.
func WebsocketConfig() WebsocketRuntimeConfig {
	cfg := activeWebsocketConfig
	return WebsocketRuntimeConfig{
		PingInterval:  cfg.pingInterval,
		PongWait:      cfg.pongWait,
		WriteDeadline: cfg.writeDeadline,
		MaxMessage:    cfg.maxBytes,
	}
}

// AllowedOriginsConfig returns whether all origins are permitted along with the normalized allow-list.
func AllowedOriginsConfig() (allowAll bool, origins []string) {
	allowedOriginsMu.RLock()
	defer allowedOriginsMu.RUnlock()

	if allowAllOrigins {
		return true, nil
	}
	if len(allowedOrigins) == 0 {
		return false, nil
	}

	origins = make([]string, 0, len(allowedOrigins))
	for origin := range allowedOrigins {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	return false, origins
}

func loadUIConfigFromEnv() uiConfig {
	hide := true
	raw := strings.TrimSpace(os.Getenv("ELORA_UI_YT_PREFIX_AT"))
	if raw != "" {
		hide = !isTruthy(raw)
	}

	showBadges := true
	if val := strings.TrimSpace(os.Getenv("ELORA_UI_SHOW_BADGES")); val != "" {
		showBadges = isTruthy(val)
	}

	return uiConfig{
		hideYouTubeAt: hide,
		showBadges:    showBadges,
	}
}

// UIConfig returns the active presentation toggles for the frontend.
func UIConfig() (hideYouTubeAt bool, showBadges bool) {
	return activeUIConfig.hideYouTubeAt, activeUIConfig.showBadges
}

func durationFromEnv(key string, def int) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return time.Duration(def) * time.Millisecond
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("config: invalid %s=%q, using default %d", key, raw, def)
		return time.Duration(def) * time.Millisecond
	}
	return time.Duration(n) * time.Millisecond
}

func int64FromEnv(key string, def int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		log.Printf("config: invalid %s=%q, using default %d", key, raw, def)
		return def
	}
	return n
}

func isTruthy(raw string) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	switch raw {
	case "1", "true", "yes", "on", "show", "enable":
		return true
	default:
		return false
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

// StopChatFetches is retained for backward compatibility with the legacy UI control.
// The legacy Python harvester pipeline has been removed, so this endpoint now returns
// a no-op response while gnasty-chat handles harvesting via SQLite.
func StopChatFetches(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]string{
		"status":  "ok",
		"message": "gnasty-chat is the active harvester; no restart required",
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("http: failed to encode restart response: %v", err)
		http.Error(w, "restart response unavailable", http.StatusInternalServerError)
	}
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

// decodeTwitchSpans converts Twitch first-party span strings (e.g. "425618:12-14")
// into Emote entries with a 1x image URL. It is ASCII-safe and bounds-checked.
func decodeTwitchSpans(text string, spansJSON string) []Emote {
	if strings.TrimSpace(spansJSON) == "" {
		return nil
	}
	var raw []string
	if err := json.Unmarshal([]byte(spansJSON), &raw); err != nil || len(raw) == 0 {
		return nil
	}

	out := make([]Emote, 0, len(raw))
	// NOTE: we index by bytes; Twitch spans for common ASCII emotes (e.g., "LUL") align with byte boundaries.
	for _, s := range raw {
		parts := strings.SplitN(s, ":", 2)
		if len(parts) != 2 {
			continue
		}
		id := strings.TrimSpace(parts[0])
		rng := parts[1]
		bounds := strings.SplitN(rng, "-", 2)
		if len(bounds) != 2 {
			continue
		}
		start, err1 := strconv.Atoi(bounds[0])
		end, err2 := strconv.Atoi(bounds[1])
		if err1 != nil || err2 != nil || start < 0 || end < start {
			continue
		}
		if start >= len(text) {
			continue
		}
		if end >= len(text) {
			end = len(text) - 1
		}
		name := text[start : end+1]
		if strings.TrimSpace(name) == "" {
			continue
		}

		// Build a single 1x image (28px nominal) for first-party Twitch emotes.
		img := Image{
			ID:     id + "-1x",
			URL:    "https://static-cdn.jtvnw.net/emoticons/v2/" + id + "/default/dark/1.0",
			Width:  28,
			Height: 28,
		}
		out = append(out, Emote{
			ID:        id,
			Name:      name,
			Locations: []string{bounds[0] + "-" + bounds[1]},
			Images:    []Image{img},
		})
	}
	return out
}
