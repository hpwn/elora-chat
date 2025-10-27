package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
)

type seedResponse struct {
	Inserted int `json:"inserted"`
}

var (
	devSeedOnce    sync.Once
	devSeedAllowed bool
)

// SetupDevRoutes registers development helper endpoints.
func SetupDevRoutes(r *mux.Router) {
	if r == nil {
		return
	}
	if !enableDevSeedRoutes() {
		return
	}

	seed := r.PathPrefix("/api/dev/seed").Subrouter()
	seed.Use(SessionMiddleware)
	seed.HandleFunc("/marker", handleSeedMarker).Methods(http.MethodPost)
	seed.HandleFunc("/burst", handleSeedBurst).Methods(http.MethodPost)
}

func enableDevSeedRoutes() bool {
	devSeedOnce.Do(func() {
		raw := strings.TrimSpace(os.Getenv("ELORA_DEV_SEED_ENABLED"))
		if raw == "" {
			devSeedAllowed = false
			return
		}
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			log.Printf("dev: invalid ELORA_DEV_SEED_ENABLED=%q, disabling seeding routes", raw)
			devSeedAllowed = false
			return
		}
		if !enabled {
			devSeedAllowed = false
			return
		}

		env := strings.ToLower(strings.TrimSpace(os.Getenv("ELORA_ENV")))
		if env == "" {
			env = strings.ToLower(strings.TrimSpace(os.Getenv("GO_ENV")))
		}
		if env == "" {
			env = strings.ToLower(strings.TrimSpace(os.Getenv("ENVIRONMENT")))
		}
		if env == "production" || env == "prod" {
			log.Printf("dev: refusing to enable seeding routes in environment %q", env)
			devSeedAllowed = false
			return
		}

		devSeedAllowed = true
	})
	return devSeedAllowed
}

func handleSeedMarker(w http.ResponseWriter, r *http.Request) {
	count, err := seedMarker(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondSeed(w, count)
}

func handleSeedBurst(w http.ResponseWriter, r *http.Request) {
	count, err := seedBurst(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondSeed(w, count)
}

func respondSeed(w http.ResponseWriter, count int) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(seedResponse{Inserted: count})
}

func seedMarker(ctx context.Context) (int, error) {
	msg := Message{
		Author:  "Elora Marker",
		Message: fmt.Sprintf("=== Marker %s ===", time.Now().Format(time.RFC3339)),
		Tokens:  []Token{},
		Emotes:  []Emote{},
		Badges:  []Badge{},
		Source:  "Marker",
		Colour:  "#FFCD05",
	}
	if err := persistSeedMessage(ctx, msg, time.Now().UTC()); err != nil {
		return 0, err
	}
	return 1, nil
}

func seedBurst(ctx context.Context) (int, error) {
	now := time.Now().UTC()
	samples := []Message{
		{
			Author:  "Dayoman",
			Message: "Welcome to the stream!",
			Source:  "Twitch",
			Colour:  "#9146FF",
			Badges:  []Badge{{ID: "broadcaster", Version: "1"}},
		},
		{
			Author:  "hp_az",
			Message: "Mods are standing by.",
			Source:  "Twitch",
			Colour:  "#1F8B4C",
			Badges:  []Badge{{ID: "moderator", Version: "1"}},
		},
		{
			Author:  "@LoFiBot",
			Message: "New beats dropping right now!",
			Source:  "YouTube",
			Colour:  "#FF0000",
			Badges:  []Badge{{ID: "moderator", Version: "1"}},
		},
		{
			Author:  "SevenTVEnjoyer",
			Message: "Check these emotes PogChamp",
			Source:  "Twitch",
			Colour:  "#FF7F50",
			Emotes: []Emote{
				{
					ID:   "pogchamp",
					Name: "PogChamp",
					Images: []Image{
						{ID: "pogchamp-1", URL: "https://static-cdn.jtvnw.net/emoticons/v2/305954156/static/light/3.0", Width: 112, Height: 112},
					},
				},
			},
		},
		{
			Author:  "@MusicFan",
			Message: "That solo was incredible!",
			Source:  "YouTube",
			Colour:  "#4285F4",
		},
		{
			Author:  "BurstTester",
			Message: "Chat is lit tonight!",
			Source:  "Twitch",
			Colour:  "#B19CD9",
		},
	}

	inserted := 0
	for i, sample := range samples {
		ts := now.Add(time.Duration(i) * 250 * time.Millisecond)
		if err := persistSeedMessage(ctx, sample, ts); err != nil {
			return inserted, err
		}
		inserted++
	}
	return inserted, nil
}

func persistSeedMessage(ctx context.Context, msg Message, ts time.Time) error {
	if chatStore == nil {
		return errors.New("storage not initialized")
	}
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	msg.normalize()
	payload := msg.toChatPayload()

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("seed: marshal payload: %w", err)
	}
	emotesJSON, err := json.Marshal(msg.Emotes)
	if err != nil {
		return fmt.Errorf("seed: marshal emotes: %w", err)
	}
	badgesJSON, err := json.Marshal(msg.Badges)
	if err != nil {
		return fmt.Errorf("seed: marshal badges: %w", err)
	}

	record := storage.Message{
		ID:         uuid.NewString(),
		Timestamp:  ts.UTC(),
		Username:   payload.Author,
		Platform:   payload.Source,
		Text:       payload.Message,
		EmotesJSON: string(emotesJSON),
		BadgesJSON: string(badgesJSON),
		RawJSON:    string(rawPayload),
	}

	if err := chatStore.InsertMessage(ctx, &record); err != nil {
		return fmt.Errorf("seed: insert message: %w", err)
	}
	BroadcastFromTailer(record)
	return nil
}
