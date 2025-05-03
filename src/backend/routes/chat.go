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

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/hpwn/EloraChat/src/backend/pkg/storage"
	"github.com/redis/go-redis/v9"

	"github.com/jdavasligil/emodl"
)

// Initialize clients as global variables.
var (
	redisClient *redis.Client
	sqliteDB    *storage.DB
	ctx         = context.Background()
	chatHandler *ChatHandler
)

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

var emoteCache map[string]Emote

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
	Emotes  []Emote `json:"emotes"`
	Badges  []Badge `json:"badges"`
	Source  string  `json:"source"`
	Colour  string  `json:"colour"`
}

func init() {
	// Initialize the Redis client without TLS.
	redisClient = redis.NewClient(&redis.Options{
		Addr:            os.Getenv("REDIS_ADDR"),
		Password:        os.Getenv("REDIS_PASSWORD"), // The password for the Redis server (if required)
		DB:              0,                           // Default DB
		PoolSize:        200,                         // Adjusted pool size
		MinIdleConns:    10,                          // Maintain a minimum of 10 idle connections
		ConnMaxIdleTime: 5 * time.Minute,             // Maximum amount of time a connection may be idle.
		ConnMaxLifetime: 30 * time.Minute,            // Maximum amount of time a connection may be reused.
	})

	// Check the Redis connection
	_, err := redisClient.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("redis: Failed to connect to Redis: %v", err)
	}

	// Initialize SQLite database
	sqliteDB, err = storage.NewDB()
	if err != nil {
		log.Fatalf("sqlite: Failed to initialize database: %v", err)
	}

	// Initialize chat handler
	chatHandler = NewChatHandler(sqliteDB, redisClient)

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
	emoteCache = make(map[string]Emote, len(emoteCacheTmp))
	for name, emote := range emoteCacheTmp {
		emoteCache[name] = Emote{
			ID:        emote.ID,
			Name:      emote.Name,
			Locations: emote.Locations,
			Images:    []Image{Image(emote.Images[0])},
		}
	}
	// DEBUG
	// log.Println("3P EMOTES SUPPORTED")
	// for _, e := range emoteCache {
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

		// Find third party emotes in chat message
		uniqueEmotes := map[string]struct{}{}
		tokens := strings.Fields(msg.Message)
		for _, tok := range tokens {
			if e, ok := emoteCache[tok]; ok {
				uniqueEmotes[e.Name] = struct{}{}
			}
		}
		for name := range uniqueEmotes {
			msg.Emotes = append(msg.Emotes, emoteCache[name])
		}

		// Re-marshal the message with the Source set.
		modifiedMessage, err := json.Marshal(msg)
		if err != nil {
			log.Printf("chat: Failed to marshal message: %v, Message: %#v\n", err, msg)
			continue
		}

		// Process the message using the chat handler
		if err := chatHandler.ProcessMessage(modifiedMessage); err != nil {
			log.Printf("chat: Failed to process message: %v\n", err)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Println("chat: Error reading standard output:", err)
	}
}

// StreamChat initializes a WebSocket connection and streams chat messages
func StreamChat(w http.ResponseWriter, r *http.Request) {
	chatHandler.HandleWebSocket(w, r)
}

func ImageProxy(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	url := vars["url"]
	if url == "" {
		http.Error(w, "URL parameter is required", http.StatusBadRequest)
		return
	}

	// Make a request to the image URL
	resp, err := http.Get(url)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch image: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Copy the response headers
	for k, v := range resp.Header {
		w.Header()[k] = v
	}

	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")

	// Copy the status code
	w.WriteHeader(resp.StatusCode)

	// Copy the body
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("Failed to copy image response: %v", err)
	}
}

// StopChatFetches stops all ongoing chat fetch commands
func StopChatFetches(w http.ResponseWriter, r *http.Request) {
	chatFetchCmds.Range(func(url string, cmd *exec.Cmd) bool {
		if err := cmd.Process.Kill(); err != nil {
			log.Printf("Failed to kill chat fetch process for %s: %v", url, err)
		}
		return true
	})
	w.WriteHeader(http.StatusOK)
}

// SetupChatRoutes sets up WebSocket routes
func SetupChatRoutes(router *mux.Router) {
	// Public routes
	router.HandleFunc("/ws", StreamChat)
	router.HandleFunc("/image/{url:.*}", ImageProxy).Methods("GET", "OPTIONS")
	router.HandleFunc("/stop", StopChatFetches).Methods("POST")

	// Subrouter for chat routes that require authentication
	protectedRoutes := router.PathPrefix("").Subrouter()
	protectedRoutes.Use(SessionMiddleware)

	// Add protected chat routes to protectedRoutes
	protectedRoutes.HandleFunc("/restart-server", StopChatFetches).Methods("POST")
}
