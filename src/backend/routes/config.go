package routes

import (
	"encoding/json"
	"log"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/hpwn/EloraChat/src/backend/internal/runtimeconfig"
	"github.com/hpwn/EloraChat/src/backend/internal/storage"
)

type configEnvelope struct {
	Config          runtimeconfig.Config         `json:"config"`
	EnvOnlySecrets  runtimeconfig.EnvOnlySecrets `json:"envOnlySecrets"`
	Changed         bool                         `json:"changed"`
	RestartRequired []string                     `json:"restartRequired,omitempty"`
	ReconnectWS     bool                         `json:"reconnectWs"`
}

type configValidationError struct {
	Error   string                          `json:"error"`
	Message string                          `json:"message"`
	Details []runtimeconfig.ValidationError `json:"details"`
}

var runtimeState = struct {
	mu       sync.RWMutex
	current  runtimeconfig.Config
	defaults runtimeconfig.Config
	store    storage.Store
}{
	defaults: runtimeconfig.DefaultsFromEnv(),
	current:  runtimeconfig.DefaultsFromEnv(),
}

func initRuntimeConfig(store storage.Store) {
	defaults := runtimeconfig.DefaultsFromEnv()
	current := defaults
	needsMigrationPersist := false

	if store != nil {
		rec, err := store.GetConfig(ctx, runtimeconfig.StorageKey)
		if err != nil {
			log.Printf("config: failed loading persisted config: %v", err)
		} else if rec != nil {
			var persisted runtimeconfig.Config
			if err := json.Unmarshal([]byte(rec.ValueJSON), &persisted); err != nil {
				log.Printf("config: persisted config is invalid JSON, using defaults: %v", err)
			} else {
				merged := runtimeconfig.Merge(defaults, persisted)
				if normalized, errs := runtimeconfig.Normalize(merged); len(errs) == 0 {
					current = normalized
					needsMigrationPersist = rec.Version < runtimeconfig.SchemaVersion || persisted.SchemaVersion < runtimeconfig.SchemaVersion
				} else {
					log.Printf("config: persisted config failed validation (%d issues), using defaults", len(errs))
				}
			}
		}
	}

	applyRuntimeConfig(current)

	runtimeState.mu.Lock()
	runtimeState.defaults = defaults
	runtimeState.current = current
	runtimeState.store = store
	runtimeState.mu.Unlock()

	if needsMigrationPersist && store != nil {
		persisted, err := json.Marshal(current)
		if err != nil {
			log.Printf("config: migration encode warning: %v", err)
		} else if err := store.UpsertConfig(ctx, &storage.ConfigRecord{
			Key:       runtimeconfig.StorageKey,
			Version:   runtimeconfig.SchemaVersion,
			ValueJSON: string(persisted),
			UpdatedAt: time.Now().UTC(),
		}); err != nil {
			log.Printf("config: migration persist warning: %v", err)
		} else {
			log.Printf("config: migrated persisted runtime config to schemaVersion=%d", runtimeconfig.SchemaVersion)
		}
	}

	syncGnastyConfigBestEffort(current, "startup")
}

func currentRuntimeConfig() runtimeconfig.Config {
	runtimeState.mu.RLock()
	defer runtimeState.mu.RUnlock()
	return runtimeState.current
}

func defaultRuntimeConfig() runtimeconfig.Config {
	runtimeState.mu.RLock()
	defer runtimeState.mu.RUnlock()
	return runtimeState.defaults
}

// EffectiveRuntimeConfig exposes the active effective runtime config for other packages.
func EffectiveRuntimeConfig() runtimeconfig.Config {
	return currentRuntimeConfig()
}

func applyRuntimeConfig(cfg runtimeconfig.Config) {
	SetUIConfig(cfg.Features.HideYouTubeAt, cfg.Features.ShowBadges)
	SetWSMessageBehavior(cfg.Features.WSEnvelope, cfg.Features.WSDropEmpty)
	SetAllowedOrigins(cfg.AllowedOrigins)
	SetWebsocketConfig(WebsocketRuntimeConfig{
		PingInterval:  time.Duration(cfg.Websocket.PingIntervalMS) * time.Millisecond,
		PongWait:      time.Duration(cfg.Websocket.PongWaitMS) * time.Millisecond,
		WriteDeadline: time.Duration(cfg.Websocket.WriteDeadlineMS) * time.Millisecond,
		MaxMessage:    cfg.Websocket.MaxMessageBytes,
	})
}

func SetupConfigRoutes(r *mux.Router) {
	r.HandleFunc("/api/config", handleGetConfig).Methods(http.MethodGet)
	r.HandleFunc("/api/config/defaults", handleGetConfigDefaults).Methods(http.MethodGet)
	r.HandleFunc("/api/config", handlePutConfig).Methods(http.MethodPut)
}

func handleGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, configEnvelope{
		Config:         currentRuntimeConfig(),
		EnvOnlySecrets: runtimeconfig.RedactedSecretsFromEnv(),
		Changed:        false,
		ReconnectWS:    false,
	})
}

func handleGetConfigDefaults(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, configEnvelope{
		Config:         defaultRuntimeConfig(),
		EnvOnlySecrets: runtimeconfig.RedactedSecretsFromEnv(),
		Changed:        false,
		ReconnectWS:    false,
	})
}

func handlePutConfig(w http.ResponseWriter, r *http.Request) {
	payload := currentRuntimeConfig()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		message := "request body must be a raw runtime config object"
		if strings.Contains(err.Error(), `unknown field "config"`) {
			message = "request body must be the raw config object (send GET /api/config response .config only)"
		}
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_payload",
			"message": message,
		})
		return
	}

	normalized, validationErrors := runtimeconfig.Normalize(payload)
	if len(validationErrors) > 0 {
		writeJSONStatus(w, http.StatusBadRequest, configValidationError{
			Error:   "validation_failed",
			Message: "config validation failed",
			Details: validationErrors,
		})
		return
	}

	runtimeState.mu.RLock()
	store := runtimeState.store
	previous := runtimeState.current
	runtimeState.mu.RUnlock()

	changed := !reflect.DeepEqual(previous, normalized)
	restartRequired := changedSubsystemsRequiringRestart(previous, normalized)
	reconnectWS := previous.APIBaseURL != normalized.APIBaseURL || previous.WSURL != normalized.WSURL
	if !changed {
		// Best-effort re-sync even on no-op PUT so operators can recover from
		// transient startup sync failures without changing values.
		syncGnastyConfigBestEffort(normalized, "put-nochange")
		writeJSON(w, configEnvelope{
			Config:          normalized,
			EnvOnlySecrets:  runtimeconfig.RedactedSecretsFromEnv(),
			Changed:         false,
			RestartRequired: restartRequired,
			ReconnectWS:     reconnectWS,
		})
		return
	}

	persisted, err := json.Marshal(normalized)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{
			"error":   "persist_failed",
			"message": "failed to encode normalized config",
		})
		return
	}

	if store != nil {
		err = store.UpsertConfig(r.Context(), &storage.ConfigRecord{
			Key:       runtimeconfig.StorageKey,
			Version:   runtimeconfig.SchemaVersion,
			ValueJSON: string(persisted),
			UpdatedAt: time.Now().UTC(),
		})
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{
				"error":   "persist_failed",
				"message": "failed to persist config",
			})
			return
		}
	}

	applyRuntimeConfig(normalized)

	log.Printf(
		"config: applied twitch_channel=%q youtube_source_url=%q api_base=%q ws_url=%q",
		normalized.TwitchChannel,
		normalized.YouTubeSourceURL,
		normalized.APIBaseURL,
		normalized.WSURL,
	)
	if previous.TwitchChannel != normalized.TwitchChannel {
		log.Printf("config: switch twitch channel %q -> %q", previous.TwitchChannel, normalized.TwitchChannel)
	}
	if previous.YouTubeSourceURL != normalized.YouTubeSourceURL {
		log.Printf("config: switch youtube source %q -> %q", previous.YouTubeSourceURL, normalized.YouTubeSourceURL)
	}

	runtimeState.mu.Lock()
	runtimeState.current = normalized
	runtimeState.mu.Unlock()

	syncGnastyConfigBestEffort(normalized, "put")

	for _, item := range restartRequired {
		log.Printf("config: %s changed; restart required for full effect", item)
	}

	writeJSON(w, configEnvelope{
		Config:          normalized,
		EnvOnlySecrets:  runtimeconfig.RedactedSecretsFromEnv(),
		Changed:         true,
		RestartRequired: restartRequired,
		ReconnectWS:     reconnectWS,
	})
}

func changedSubsystemsRequiringRestart(before, after runtimeconfig.Config) []string {
	restart := make([]string, 0, 2)
	if before.Tailer != after.Tailer {
		restart = append(restart, "tailer")
	}
	if before.Ingest.GnastyBin != after.Ingest.GnastyBin ||
		before.Ingest.BackoffBaseMS != after.Ingest.BackoffBaseMS ||
		before.Ingest.BackoffMaxMS != after.Ingest.BackoffMaxMS ||
		!reflect.DeepEqual(before.Ingest.GnastyArgs, after.Ingest.GnastyArgs) {
		restart = append(restart, "ingest")
	}
	return restart
}
