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
	"github.com/redis/go-redis/v9"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
	redisstore "github.com/hpwn/EloraChat/src/backend/internal/storage/redis"
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

	storeKind := strings.ToLower(strings.TrimSpace(getEnvOrDefault("ELORA_STORE", "redis")))
	var (
		store       storage.Store
		redisClient *redis.Client
	)

	newRedisClient := func() *redis.Client {
		return redis.NewClient(&redis.Options{
			Addr:            os.Getenv("REDIS_ADDR"),
			Password:        os.Getenv("REDIS_PASSWORD"),
			DB:              0,
			PoolSize:        200,
			MinIdleConns:    10,
			ConnMaxIdleTime: 5 * time.Minute,
			ConnMaxLifetime: 30 * time.Minute,
		})
	}
	switch storeKind {
	case "sqlite":
		sqliteCfg := sqlite.Config{
			Mode:            getEnvOrDefault("ELORA_DB_MODE", "ephemeral"),
			Path:            strings.TrimSpace(os.Getenv("ELORA_DB_PATH")),
			MaxConns:        getEnvAsInt("ELORA_DB_MAX_CONNS", 16),
			BusyTimeoutMS:   getEnvAsInt("ELORA_DB_BUSY_TIMEOUT_MS", 5000),
			PragmasExtraCSV: getEnvOrDefault("ELORA_DB_PRAGMAS_EXTRA", "mmap_size=268435456,cache_size=-100000,temp_store=MEMORY"),
		}
		store = sqlite.New(sqliteCfg)
	case "redis", "":
		redisClient = newRedisClient()
		store = redisstore.New(redisstore.Config{
			Client: redisClient,
			Stream: "chatMessages",
			MaxLen: 100,
		})
		storeKind = "redis"
	default:
		log.Printf("storage: unknown store %q, defaulting to redis", storeKind)
		redisClient = newRedisClient()
		store = redisstore.New(redisstore.Config{
			Client: redisClient,
			Stream: "chatMessages",
			MaxLen: 100,
		})
		storeKind = "redis"
	}

	if redisClient != nil {
		defer func() {
			if err := redisClient.Close(); err != nil {
				log.Printf("redis: close error: %v", err)
			}
		}()
	}

	if err := store.Init(baseCtx); err != nil {
		log.Fatalf("storage: failed to initialize %s store: %v", storeKind, err)
	}
	defer func() {
		if err := store.Close(context.Background()); err != nil {
			log.Printf("storage: close error: %v", err)
		}
	}()

	log.Printf("storage: using %s store", storeKind)

	routes.InitRoutes(2*time.Second, redisClient, store)

	r := mux.NewRouter()

	// Register the dynamic config serving route
	r.HandleFunc("/config.json", serveConfig)

	// Set up WebSocket chat routes
	routes.SetupChatRoutes(r)
	routes.SetupAuthRoutes(r)
	routes.SetupSendRoutes(r)

	// Serve static files from the "public" directory
	fs := http.FileServer(http.Dir("public"))
	r.PathPrefix("/").Handler(http.StripPrefix("/", fs))

	// Start fetching chat messages
	chatURLs := []string{
		// "https://www.twitch.tv/hp_az",
		// "https://www.youtube.com/channel/UCHToAogHtFnv2uksbDzKsYA/live", // my channel link
		// "https://www.youtube.com/@hp_az/live", // crude live link
		// "https://www.youtube.com/watch?v=_oMKOh8skrM", // viewer link
		// "https://youtube.com/live/7NA555IYE24?feature=share",
		// "https://youtube.com/live/7455sVTXUPU?feature=share",
		// "https://www.youtube.com/watch?v=jfKfPfyJRdk", // lofi girl live
		// "http://youtube.com/channel/UCSJ4gkVC6NrvII8umztf0Ow/live",
		// "https://www.twitch.tv/rifftrax",
		// "https://www.youtube.com/watch?v=39VeO9p7Vn0", // dayo live test stream
		// "https://www.youtube.com/live/6sjf7R0o-ss?si=WkdXIOu83_7Sglk2&t=7500", // ludwig live test stream
		// "https://www.twitch.tv/Johnstone",
		// "https://www.twitch.tv/Hypnoshark",
		// "https://www.twitch.tv/QTCinderella",
		// "https://www.twitch.tv/Quin69",
		// "https://www.twitch.tv/jakenbakeLIVE",
		// "https://www.twitch.tv/Knut",
		// "https://www.youtube.com/@dayoman/live",
		"http://youtube.com/channel/UC2c4NxvHnbXs3NLpCm641ew/live",
		"http://youtube.com/channel/UC20nA0vpbiKZ1DwX2sFR9dQ/live",
		"https://www.twitch.tv/dayoman",
		// "https://www.youtube.com/watch?v=4xDzrJKXOOY",
		// "https://www.twitch.tv/forsen", // basically a hard code for constant chats
		// "https://www.twitch.tv/jynxzi", // basically a hard code for constant chats
		// "https://www.youtube.com/watch?v=Gtqw9b8g2wk",
		// "https://www.twitch.tv/abel",
		// "https://www.youtube.com/watch?v=VebOqD00Zj8",
		// "https://www.youtube.com/watch?v=pCTxDYFdEOk",
		// "https://youtube.com/live/JHqR9hq70No?feature=share",
		// "https://www.twitch.tv/papaplatte",
		// "https://www.youtube.com/watch?v=REmPV-EPwPc", // ninja yt
		// "https://www.twitch.tv/nutty",
		// "https://www.youtube.com/@nuttylmao/live",
	}
	routes.StartChatFetch(chatURLs)

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
