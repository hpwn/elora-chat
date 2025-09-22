package routes

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
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
	Name        string  `json:"name"`
	Title       string  `json:"title"`
	ClickAction string  `json:"clickAction"`
	ClickURL    string  `json:"clickURL"`
	Icons       []Image `json:"icons"`
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

func InitRoutes(store storage.Store) {
	if store == nil {
		log.Fatalf("storage: store is nil")
	}

	chatStore = store
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
				payload := history[i].RawJSON
				if payload == "" {
					fallback := Message{
						Author:  history[i].Username,
						Message: history[i].Text,
						Tokens:  []Token{},
						Emotes:  []Emote{},
						Badges:  []Badge{},
						Source:  history[i].Platform,
						Colour:  userColorMap[history[i].Username],
					}
					raw, marshalErr := json.Marshal(fallback)
					if marshalErr != nil {
						log.Printf("chat: Failed to marshal fallback history message: %v\n", marshalErr)
						continue
					}
					payload = string(raw)
				}
				if err := conn.WriteMessage(websocket.TextMessage, []byte(payload)); err != nil {
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
				var msg Message
				err := json.Unmarshal(m, &msg)
				if err != nil {
					log.Println("json: ", err)
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
				m, err = json.Marshal(msg)
				if err != nil {
					log.Println("json: ", err)
				}
				if err := conn.WriteMessage(websocket.TextMessage, m); err != nil {
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
