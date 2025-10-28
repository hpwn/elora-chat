package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"

	httpapi "github.com/hpwn/EloraChat/src/backend/internal/http"
	"github.com/hpwn/EloraChat/src/backend/internal/ingest"
	"github.com/hpwn/EloraChat/src/backend/internal/storage/sqlite"
	"github.com/hpwn/EloraChat/src/backend/internal/tailer"
	"github.com/hpwn/EloraChat/src/backend/routes"
)

// Config holds the structure for the configuration JSON
type Config struct {
	DeployedUrl   string `json:"deployedUrl"`
	APIBase       string `json:"apiBase"`
	WebsocketURL  string `json:"wsUrl"`
	HideYouTubeAt bool   `json:"hideYouTubeAt"`
	ShowBadges    bool   `json:"showBadges"`
}

var (
	exportedAPIBase   string
	exportedWebsocket string
	exportedDeployed  string
)

// serveConfig sends the application configuration as JSON
func serveConfig(w http.ResponseWriter, r *http.Request) {
	hideYT, showBadges := routes.UIConfig()

	config := Config{
		DeployedUrl:   exportedDeployed,
		APIBase:       exportedAPIBase,
		WebsocketURL:  exportedWebsocket,
		HideYouTubeAt: hideYT,
		ShowBadges:    showBadges,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

func main() {
	httpAddr := getEnvOrDefault("ELORA_HTTP_ADDR", "0.0.0.0")
	httpPort := strings.TrimSpace(os.Getenv("ELORA_HTTP_PORT"))
	if httpPort == "" {
		httpPort = strings.TrimSpace(os.Getenv("PORT"))
	}
	if httpPort == "" {
		httpPort = "8080"
	}

	exportedAPIBase = computeAPIBase(httpPort)
	exportedWebsocket = computeWebsocketURL(httpPort, exportedAPIBase)
	exportedDeployed = getEnvOrDefault("DEPLOYED_URL", exportedAPIBase)

	baseCtx := context.Background()

	sqliteCfg := sqlite.Config{
		Mode:            getEnvOrDefault("ELORA_DB_MODE", "ephemeral"),
		Path:            strings.TrimSpace(os.Getenv("ELORA_DB_PATH")),
		MaxConns:        getEnvAsInt("ELORA_DB_MAX_CONNS", 16),
		BusyTimeoutMS:   getEnvAsIntFallback([]string{"ELORA_SQLITE_BUSY_TIMEOUT_MS", "ELORA_DB_BUSY_TIMEOUT_MS"}, 5000),
		PragmasExtraCSV: getEnvOrDefault("ELORA_DB_PRAGMAS_EXTRA", "mmap_size=268435456,cache_size=-100000,temp_store=MEMORY"),
		JournalMode:     sanitizeJournalMode(getEnvOrDefault("ELORA_SQLITE_JOURNAL_MODE", "wal")),
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
	routes.SetupDevRoutes(r)

	rootMux := http.NewServeMux()
	httpapi.RegisterHealth(rootMux, store)
	rootMux.Handle("/", r)

	allowedOrigins := parseCSV(os.Getenv("ELORA_ALLOWED_ORIGINS"))
	routes.SetAllowedOrigins(allowedOrigins)
	corsHandler := handlers.CORS(
		handlers.AllowedMethods([]string{http.MethodGet, http.MethodPost, http.MethodOptions}),
		handlers.AllowedHeaders([]string{"Authorization", "Content-Type"}),
		handlers.ExposedHeaders([]string{"Link"}),
		handlers.AllowCredentials(),
	)
	if len(allowedOrigins) > 0 {
		corsHandler = handlers.CORS(
			handlers.AllowedMethods([]string{http.MethodGet, http.MethodPost, http.MethodOptions}),
			handlers.AllowedHeaders([]string{"Authorization", "Content-Type"}),
			handlers.ExposedHeaders([]string{"Link"}),
			handlers.AllowCredentials(),
			handlers.AllowedOrigins(allowedOrigins),
		)
	}

	handler := corsHandler(rootMux)

	tailerCfg := tailer.Config{
		Enabled:        getEnvAsBoolFallback([]string{"ELORA_TAILER_ENABLED", "ELORA_DB_TAIL_ENABLED"}, false),
		Interval:       time.Duration(getEnvAsIntFallback([]string{"ELORA_TAILER_POLL_MS", "ELORA_DB_TAIL_INTERVAL_MS", "ELORA_DB_TAIL_POLL_MS"}, 1000)) * time.Millisecond,
		Batch:          getEnvAsIntFallback([]string{"ELORA_TAILER_MAX_BATCH", "ELORA_DB_TAIL_BATCH"}, 200),
		MaxLag:         time.Duration(getEnvAsInt("ELORA_TAILER_MAX_LAG_MS", 0)) * time.Millisecond,
		PersistOffsets: getEnvAsBool("ELORA_TAILER_PERSIST_OFFSETS", false),
		OffsetPath:     strings.TrimSpace(os.Getenv("ELORA_TAILER_OFFSET_PATH")),
	}
	if tailerCfg.PersistOffsets {
		if tailerCfg.OffsetPath == "" {
			if sqliteCfg.Path != "" {
				tailerCfg.OffsetPath = filepath.Clean(sqliteCfg.Path + ".offset.json")
			} else {
				tailerCfg.PersistOffsets = false
			}
		} else {
			tailerCfg.OffsetPath = filepath.Clean(tailerCfg.OffsetPath)
		}
	}
	dbTailer := tailer.New(tailerCfg, store)
	if err := dbTailer.Start(baseCtx); err != nil {
		log.Printf("dbtailer: error: %v", err)
	}
	defer dbTailer.Stop()

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
	if len(chatURLs) == 0 {
		log.Printf("No CHAT_URLS provided; skipping chat fetch")
	} else {
		env := ingest.FromEnv()
		log.Printf("chat seed URLs: %d", len(chatURLs))
		log.Printf("ingest: selected driver=%q", env.Driver)

		switch env.Driver {
		case ingest.DriverChatDownloader:
			routes.StartChatFetch(chatURLs)
		case ingest.DriverGnasty:
			gn, err := env.BuildGnasty(nil, chatURLs, log.Default())
			if err != nil {
				log.Fatalf("ingest: gnasty config error: %v", err)
			}
			gn.Start(baseCtx)
		default:
			log.Printf("ingest: unknown driver %q; defaulting to chatdownloader", env.Driver)
			routes.StartChatFetch(chatURLs)
		}
	}

	// Create server
	listenAddr := net.JoinHostPort(httpAddr, httpPort)

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: handler,
	}

	// Start the server in a goroutine
	go func() {
		log.Printf("Starting server on %s", listenAddr)
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

func getEnvAsBoolFallback(keys []string, def bool) bool {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			log.Printf("config: invalid %s=%q, ignoring", key, value)
			continue
		}
		return parsed
	}
	return def
}

func getEnvAsBool(key string, def bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		log.Printf("config: invalid %s=%q, using default %t", key, value, def)
		return def
	}
	return parsed
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

func getEnvAsIntFallback(keys []string, def int) int {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			log.Printf("config: invalid %s=%q, ignoring", key, value)
			continue
		}
		return parsed
	}
	return def
}

func parseCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func sanitizeJournalMode(mode string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return "WAL"
	}
	upper := strings.ToUpper(mode)
	for _, r := range upper {
		if r < 'A' || r > 'Z' {
			return "WAL"
		}
	}
	return upper
}

func computeAPIBase(port string) string {
	base := strings.TrimSpace(os.Getenv("VITE_PUBLIC_API_BASE"))
	if base == "" {
		base = "http://localhost:" + port
	}
	base = strings.TrimRight(base, "/")
	if base == "" {
		return "http://localhost:" + port
	}
	return base
}

func computeWebsocketURL(port, apiBase string) string {
	raw := strings.TrimSpace(os.Getenv("VITE_PUBLIC_WS_URL"))
	if raw != "" {
		return raw
	}

	parsed, err := url.Parse(apiBase)
	if err != nil {
		return "ws://localhost:" + port + "/ws/chat"
	}
	scheme := "ws"
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		scheme = "wss"
	case "wss", "ws":
		scheme = strings.ToLower(parsed.Scheme)
	}
	host := parsed.Host
	if host == "" {
		host = "localhost:" + port
	}
	return scheme + "://" + host + "/ws/chat"
}
