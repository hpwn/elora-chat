package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"

	"github.com/hpwn/EloraChat/src/backend/internal/storage/sqlite"
	"github.com/hpwn/EloraChat/src/backend/routes" // Ensure this is the correct path to your routes package
)

// Config holds the structure for the configuration JSON
type Config struct {
	DeployedUrl string `json:"deployedUrl"`
}

// serveConfig sends the application configuration as JSON
func serveConfig(w http.ResponseWriter, r *http.Request) {
	config := Config{
		DeployedUrl: os.Getenv("DEPLOYED_URL"), // Make sure DEPLOYED_URL is set in your environment
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

func main() {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080" // Default to port 8080 if not specified
	}

	baseCtx := context.Background()

	sqliteCfg := sqlite.Config{
		Mode:            getEnvOrDefault("ELORA_DB_MODE", "ephemeral"),
		Path:            strings.TrimSpace(os.Getenv("ELORA_DB_PATH")),
		MaxConns:        getEnvAsInt("ELORA_DB_MAX_CONNS", 16),
		BusyTimeoutMS:   getEnvAsInt("ELORA_DB_BUSY_TIMEOUT_MS", 5000),
		PragmasExtraCSV: getEnvOrDefault("ELORA_DB_PRAGMAS_EXTRA", "mmap_size=268435456,cache_size=-100000,temp_store=MEMORY"),
	}

	store := sqlite.New(sqliteCfg)

	if err := store.Init(baseCtx); err != nil {
		log.Fatalf("storage: failed to initialize sqlite store: %v", err)
	}
	defer func() {
		if err := store.Close(context.Background()); err != nil {
			log.Printf("storage: close error: %v", err)
		}
	}()

	log.Printf("storage: using sqlite store (mode=%s)", sqliteCfg.Mode)

	routes.InitRoutes(store)

	r := mux.NewRouter()

	// Register the dynamic config serving route
	r.HandleFunc("/config.json", serveConfig)

	// Set up WebSocket chat routes
	routes.SetupChatRoutes(r)
	routes.SetupAuthRoutes(r)
	routes.SetupSendRoutes(r)
	routes.SetupMessageRoutes(r)

	// Serve static files from the "public" directory
	fs := http.FileServer(http.Dir("public"))
	r.PathPrefix("/").Handler(http.StripPrefix("/", fs))

	// Start fetching chat messages from env (comma-separated)
	rawChat := os.Getenv("CHAT_URLS")
	chatURLs := []string{}
	if rawChat != "" {
		for _, u := range strings.Split(rawChat, ",") {
			u = strings.TrimSpace(u)
			if u != "" {
				chatURLs = append(chatURLs, u)
			}
		}
	}
	log.Printf("Starting chat fetch for %d URLs: %v", len(chatURLs), chatURLs)
	if len(chatURLs) > 0 {
		routes.StartChatFetch(chatURLs)
	} else {
		log.Printf("No CHAT_URLS provided; skipping chat fetch")
	}

	// Create server
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	// Start the server in a goroutine
	go func() {
		log.Printf("Starting server on :%s\n", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server start error: %v", err)
		}
	}()

	// Graceful shutdown
	c := make(chan os.Signal, 1)
	// Accept graceful shutdowns when quit via SIGINT (Ctrl+C) or SIGTERM (Kubernetes pod shutdown)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	<-c

	// Create a deadline to wait for.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
	log.Println("shutting down")
	os.Exit(0)
}

func getEnvOrDefault(key, def string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return def
}

func getEnvAsInt(key string, def int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		log.Printf("config: invalid %s=%q, using default %d", key, value, def)
		return def
	}
	return parsed
}
