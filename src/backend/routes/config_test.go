package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/hpwn/EloraChat/src/backend/internal/runtimeconfig"
	"github.com/hpwn/EloraChat/src/backend/internal/storage"
)

type configAPIResponse struct {
	Config          runtimeconfig.Config         `json:"config"`
	EnvOnlySecrets  runtimeconfig.EnvOnlySecrets `json:"envOnlySecrets"`
	Changed         bool                         `json:"changed"`
	RestartRequired []string                     `json:"restartRequired"`
	ReconnectWS     bool                         `json:"reconnectWs"`
}

func newConfigRouter() *mux.Router {
	r := mux.NewRouter()
	SetupConfigRoutes(r)
	return r
}

func resetRuntimeConfigForTest(t *testing.T) {
	t.Helper()
	t.Setenv("ELORA_GNASTY_ADMIN_BASE", "")
	t.Cleanup(func() {
		overrideWSEnvelope = nil
		overrideWSDropEmpty = nil
		RegisterTailerConfigApplier(nil)
	})
}

func TestGetConfigReturnsDefaultsWhenMissing(t *testing.T) {
	resetRuntimeConfigForTest(t)
	cleanup := withSQLiteStore(t)
	defer cleanup()

	InitRoutes(chatStore)

	router := newConfigRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var payload configAPIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if payload.Config.SchemaVersion != runtimeconfig.SchemaVersion {
		t.Fatalf("unexpected schema version: %d", payload.Config.SchemaVersion)
	}
	if strings.TrimSpace(payload.Config.APIBaseURL) == "" {
		t.Fatalf("expected apiBaseUrl default")
	}
	if strings.TrimSpace(payload.Config.WSURL) == "" {
		t.Fatalf("expected wsUrl default")
	}
}

func TestPutConfigPersistsAndNormalizes(t *testing.T) {
	resetRuntimeConfigForTest(t)
	t.Setenv("TWITCH_OAUTH_CLIENT_SECRET", "super-secret")
	cleanup := withSQLiteStore(t)
	defer cleanup()

	InitRoutes(chatStore)

	router := newConfigRouter()
	requestPayload := runtimeconfig.DefaultsFromEnv()
	requestPayload.APIBaseURL = "https://example.com///"
	requestPayload.WSURL = "wss://example.com/ws/chat///"
	requestPayload.TwitchChannel = " https://www.twitch.tv/DayoMan "
	requestPayload.YouTubeSourceURL = "abcdefghijk"
	requestPayload.Features = runtimeconfig.FeatureConfig{
		ShowBadges:    false,
		HideYouTubeAt: false,
		WSEnvelope:    true,
		WSDropEmpty:   true,
	}
	requestPayload.Tailer = runtimeconfig.TailerConfig{
		Enabled:        true,
		PollIntervalMS: 250,
		MaxBatch:       300,
		MaxLagMS:       100,
		PersistOffsets: true,
		OffsetPath:     " /data/custom.offset.json ",
	}
	requestPayload.Websocket = runtimeconfig.WebsocketConfig{
		PingIntervalMS:  30000,
		PongWaitMS:      35000,
		WriteDeadlineMS: 4000,
		MaxMessageBytes: 131072,
	}
	requestPayload.Ingest = runtimeconfig.IngestConfig{
		GnastyBin:     " /usr/local/bin/gnasty ",
		GnastyArgs:    []string{" --foo ", "", "bar"},
		BackoffBaseMS: 1000,
		BackoffMaxMS:  20000,
	}
	body, _ := json.Marshal(requestPayload)

	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var updated configAPIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to decode put response: %v", err)
	}

	if updated.Config.TwitchChannel != "dayoman" {
		t.Fatalf("expected normalized twitch channel, got %q", updated.Config.TwitchChannel)
	}
	if updated.Config.YouTubeSourceURL != "https://www.youtube.com/watch?v=abcdefghijk" {
		t.Fatalf("expected normalized youtube URL, got %q", updated.Config.YouTubeSourceURL)
	}
	if updated.Config.APIBaseURL != "https://example.com" {
		t.Fatalf("expected normalized apiBaseUrl, got %q", updated.Config.APIBaseURL)
	}
	if updated.Config.Tailer.OffsetPath != "/data/custom.offset.json" {
		t.Fatalf("expected trimmed offsetPath, got %q", updated.Config.Tailer.OffsetPath)
	}
	if updated.EnvOnlySecrets.TwitchClientSecret.Value != "[redacted]" {
		t.Fatalf("expected redacted secret, got %+v", updated.EnvOnlySecrets.TwitchClientSecret)
	}
	if !updated.Changed {
		t.Fatalf("expected changed=true for non-noop put")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("expected get 200, got %d body=%s", getRR.Code, getRR.Body.String())
	}

	var fetched configAPIResponse
	if err := json.Unmarshal(getRR.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("failed to decode get response: %v", err)
	}
	if fetched.Config.TwitchChannel != "dayoman" {
		t.Fatalf("expected persisted twitch channel, got %q", fetched.Config.TwitchChannel)
	}

	record, err := chatStore.GetConfig(req.Context(), runtimeconfig.StorageKey)
	if err != nil {
		t.Fatalf("expected persisted config row: %v", err)
	}
	if record == nil || strings.TrimSpace(record.ValueJSON) == "" {
		t.Fatalf("expected persisted config JSON row")
	}
	if strings.Contains(record.ValueJSON, "super-secret") {
		t.Fatalf("persisted config should not include env secret")
	}
}

func TestPutConfigValidationFailure(t *testing.T) {
	resetRuntimeConfigForTest(t)
	cleanup := withSQLiteStore(t)
	defer cleanup()

	InitRoutes(chatStore)
	router := newConfigRouter()

	invalid := runtimeconfig.DefaultsFromEnv()
	invalid.APIBaseURL = "not-a-url"
	invalid.WSURL = "http://bad"
	invalid.TwitchChannel = "bad channel"
	invalid.YouTubeSourceURL = ""
	invalid.Tailer.PollIntervalMS = 1
	invalid.Tailer.MaxBatch = 0
	invalid.Websocket.PingIntervalMS = 1
	invalid.Websocket.PongWaitMS = 1
	invalid.Websocket.WriteDeadlineMS = 1
	invalid.Websocket.MaxMessageBytes = 1
	invalid.Ingest.BackoffBaseMS = 50
	invalid.Ingest.BackoffMaxMS = 10
	invalid.Gnasty.Sinks.BatchSize = 0
	body, _ := json.Marshal(invalid)

	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}

	var validation configValidationError
	if err := json.Unmarshal(rr.Body.Bytes(), &validation); err != nil {
		t.Fatalf("failed to decode validation error: %v", err)
	}
	if validation.Error != "validation_failed" {
		t.Fatalf("expected validation_failed error, got %q", validation.Error)
	}
	if len(validation.Details) == 0 {
		t.Fatalf("expected validation details")
	}
}

func TestPutConfigRejectsWrappedConfigPayload(t *testing.T) {
	resetRuntimeConfigForTest(t)
	cleanup := withSQLiteStore(t)
	defer cleanup()

	InitRoutes(chatStore)
	router := newConfigRouter()

	getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("expected get 200, got %d body=%s", getRR.Code, getRR.Body.String())
	}

	var current configAPIResponse
	if err := json.Unmarshal(getRR.Body.Bytes(), &current); err != nil {
		t.Fatalf("decode current config: %v", err)
	}

	wrapped := map[string]any{
		"config":  current.Config,
		"changed": true,
	}
	configMap, ok := wrapped["config"].(runtimeconfig.Config)
	if !ok {
		t.Fatalf("expected runtime config in wrapper payload")
	}
	configMap.Gnasty.YouTube.PollIntervalMS = configMap.Gnasty.YouTube.PollIntervalMS + 1234
	wrapped["config"] = configMap
	body, _ := json.Marshal(wrapped)

	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for wrapped payload, got %d body=%s", rr.Code, rr.Body.String())
	}

	var errPayload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &errPayload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if errPayload["error"] != "invalid_payload" {
		t.Fatalf("expected invalid_payload error, got %+v", errPayload)
	}

	afterReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	afterRR := httptest.NewRecorder()
	router.ServeHTTP(afterRR, afterReq)
	if afterRR.Code != http.StatusOK {
		t.Fatalf("expected get 200, got %d body=%s", afterRR.Code, afterRR.Body.String())
	}

	var after configAPIResponse
	if err := json.Unmarshal(afterRR.Body.Bytes(), &after); err != nil {
		t.Fatalf("decode after config: %v", err)
	}
	if after.Config.Gnasty.YouTube.PollIntervalMS != current.Config.Gnasty.YouTube.PollIntervalMS {
		t.Fatalf("expected wrapped payload rejection with no mutation, before=%d after=%d",
			current.Config.Gnasty.YouTube.PollIntervalMS,
			after.Config.Gnasty.YouTube.PollIntervalMS,
		)
	}
}

func TestPutConfigNormalizesTwitchAndYouTubeInputs(t *testing.T) {
	resetRuntimeConfigForTest(t)
	cleanup := withSQLiteStore(t)
	defer cleanup()

	InitRoutes(chatStore)
	router := newConfigRouter()

	currentReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	currentRR := httptest.NewRecorder()
	router.ServeHTTP(currentRR, currentReq)
	if currentRR.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", currentRR.Code)
	}
	var current configAPIResponse
	if err := json.Unmarshal(currentRR.Body.Bytes(), &current); err != nil {
		t.Fatalf("decode current config: %v", err)
	}

	current.Config.TwitchChannel = "twitch.tv/dagnel"
	current.Config.YouTubeSourceURL = "@lofigirl"
	body, _ := json.Marshal(current.Config)

	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var updated configAPIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated config: %v", err)
	}
	if updated.Config.TwitchChannel != "dagnel" {
		t.Fatalf("expected twitch login dagnel, got %q", updated.Config.TwitchChannel)
	}
	if updated.Config.YouTubeSourceURL != "https://www.youtube.com/@lofigirl/live" {
		t.Fatalf("expected canonical youtube live URL, got %q", updated.Config.YouTubeSourceURL)
	}
}

func TestGetConfigNeverLeaksSecrets(t *testing.T) {
	resetRuntimeConfigForTest(t)
	t.Setenv("TWITCH_OAUTH_CLIENT_SECRET", "never-return-this")
	t.Setenv("YOUTUBE_API_KEY", "never-return-this-either")

	cleanup := withSQLiteStore(t)
	defer cleanup()

	InitRoutes(chatStore)
	router := newConfigRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()
	if strings.Contains(body, "never-return-this") {
		t.Fatalf("response leaked secret: %s", body)
	}
	if !strings.Contains(body, "[redacted]") {
		t.Fatalf("expected redacted marker in response")
	}
}

func TestPutConfigSyncsGnastyBulkConfigExactlyOnce(t *testing.T) {
	resetRuntimeConfigForTest(t)
	t.Setenv("TWITCH_CHANNEL", "")
	t.Setenv("YOUTUBE_URL", "")

	type gnastyCall struct {
		Path string
		Body map[string]any
	}
	var (
		mu    sync.Mutex
		calls []gnastyCall
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		_ = r.Body.Close()
		mu.Lock()
		calls = append(calls, gnastyCall{Path: r.URL.Path, Body: payload})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	t.Setenv("ELORA_GNASTY_ADMIN_BASE", server.URL)

	cleanup := withSQLiteStore(t)
	defer cleanup()

	InitRoutes(chatStore)
	router := newConfigRouter()
	mu.Lock()
	calls = nil
	mu.Unlock()

	getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("expected get 200, got %d body=%s", getRR.Code, getRR.Body.String())
	}

	var current configAPIResponse
	if err := json.Unmarshal(getRR.Body.Bytes(), &current); err != nil {
		t.Fatalf("decode current config: %v", err)
	}
	current.Config.TwitchChannel = "https://www.twitch.tv/DayoMan"
	current.Config.YouTubeSourceURL = "@lofigirl"
	body, _ := json.Marshal(current.Config)

	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 gnasty admin call, got %d", len(calls))
	}
	if calls[0].Path != "/admin/config" {
		t.Fatalf("expected /admin/config, got %s", calls[0].Path)
	}
	twitch, _ := calls[0].Body["twitch"].(map[string]any)
	channel, _ := twitch["channel"].(string)
	if channel != "dayoman" {
		t.Fatalf("expected normalized twitch channel in bulk payload, got %q", channel)
	}
	youtube, _ := calls[0].Body["youtube"].(map[string]any)
	ytURL, _ := youtube["url"].(string)
	if ytURL != "https://www.youtube.com/@lofigirl/live" {
		t.Fatalf("expected canonical youtube url in bulk payload, got %q", ytURL)
	}
}

func TestPutConfigGnastySyncFailureIsWarningOnly(t *testing.T) {
	resetRuntimeConfigForTest(t)
	t.Setenv("TWITCH_CHANNEL", "")
	t.Setenv("YOUTUBE_URL", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer server.Close()
	t.Setenv("ELORA_GNASTY_ADMIN_BASE", server.URL)

	var logBuf bytes.Buffer
	prevLogOut := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(prevLogOut) })

	cleanup := withSQLiteStore(t)
	defer cleanup()

	InitRoutes(chatStore)
	router := newConfigRouter()

	getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("expected get 200, got %d body=%s", getRR.Code, getRR.Body.String())
	}

	var current configAPIResponse
	if err := json.Unmarshal(getRR.Body.Bytes(), &current); err != nil {
		t.Fatalf("decode current config: %v", err)
	}
	current.Config.TwitchChannel = "dayoman"
	current.Config.YouTubeSourceURL = "https://www.youtube.com/@lofigirl/live"
	body, _ := json.Marshal(current.Config)

	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 despite gnasty 500, got %d body=%s", rr.Code, rr.Body.String())
	}

	logs := logBuf.String()
	if !strings.Contains(logs, "config: gnasty bulk sync warning") {
		t.Fatalf("expected gnasty warning log, got: %s", logs)
	}
	if !strings.Contains(logs, "body=\"boom\"") {
		t.Fatalf("expected gnasty response body in warning log, got: %s", logs)
	}
}

func TestPutConfigNoopReturnsChangedFalse(t *testing.T) {
	resetRuntimeConfigForTest(t)
	t.Setenv("TWITCH_CHANNEL", "dayoman")
	t.Setenv("YOUTUBE_URL", "@lofigirl")
	cleanup := withSQLiteStore(t)
	defer cleanup()

	InitRoutes(chatStore)
	router := newConfigRouter()

	getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("expected get 200, got %d body=%s", getRR.Code, getRR.Body.String())
	}

	var current configAPIResponse
	if err := json.Unmarshal(getRR.Body.Bytes(), &current); err != nil {
		t.Fatalf("decode current config: %v", err)
	}
	body, _ := json.Marshal(current.Config)

	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var updated configAPIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated config: %v", err)
	}
	if updated.Changed {
		t.Fatalf("expected changed=false for no-op put")
	}
}

func TestPutConfigNoopAllowsEmptyOptionalSources(t *testing.T) {
	resetRuntimeConfigForTest(t)
	t.Setenv("TWITCH_CHANNEL", "")
	t.Setenv("YOUTUBE_URL", "")
	cleanup := withSQLiteStore(t)
	defer cleanup()

	InitRoutes(chatStore)
	router := newConfigRouter()

	getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("expected get 200, got %d body=%s", getRR.Code, getRR.Body.String())
	}

	var current configAPIResponse
	if err := json.Unmarshal(getRR.Body.Bytes(), &current); err != nil {
		t.Fatalf("decode current config: %v", err)
	}
	if current.Config.TwitchChannel != "" {
		t.Fatalf("expected empty twitch source, got %q", current.Config.TwitchChannel)
	}
	if current.Config.YouTubeSourceURL != "" {
		t.Fatalf("expected empty youtube source, got %q", current.Config.YouTubeSourceURL)
	}

	body, _ := json.Marshal(current.Config)

	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var updated configAPIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated config: %v", err)
	}
	if updated.Changed {
		t.Fatalf("expected changed=false for no-op put")
	}
}

func TestPutConfigTailerHotApplySuccess(t *testing.T) {
	resetRuntimeConfigForTest(t)
	cleanup := withSQLiteStore(t)
	defer cleanup()

	InitRoutes(chatStore)
	router := newConfigRouter()

	var called atomic.Bool
	RegisterTailerConfigApplier(func(cfg runtimeconfig.TailerConfig) error {
		called.Store(true)
		if !cfg.Enabled {
			t.Fatalf("expected enabled tailer in hook")
		}
		return nil
	})

	getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("expected get 200, got %d body=%s", getRR.Code, getRR.Body.String())
	}

	var current configAPIResponse
	if err := json.Unmarshal(getRR.Body.Bytes(), &current); err != nil {
		t.Fatalf("decode current config: %v", err)
	}
	current.Config.Tailer.Enabled = true
	current.Config.Tailer.PollIntervalMS = 250
	current.Config.Tailer.MaxBatch = 300
	body, _ := json.Marshal(current.Config)

	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	if !called.Load() {
		t.Fatalf("expected tailer hot-apply hook to be called")
	}

	var updated configAPIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated config: %v", err)
	}
	for _, item := range updated.RestartRequired {
		if item == "tailer" {
			t.Fatalf("tailer should not require restart after hot-apply")
		}
	}
}

func TestPutConfigTailerHotApplyFailure(t *testing.T) {
	resetRuntimeConfigForTest(t)
	cleanup := withSQLiteStore(t)
	defer cleanup()

	InitRoutes(chatStore)
	router := newConfigRouter()

	RegisterTailerConfigApplier(func(runtimeconfig.TailerConfig) error {
		return errors.New("tailer boom")
	})

	getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("expected get 200, got %d body=%s", getRR.Code, getRR.Body.String())
	}

	var current configAPIResponse
	if err := json.Unmarshal(getRR.Body.Bytes(), &current); err != nil {
		t.Fatalf("decode current config: %v", err)
	}
	current.Config.Tailer.Enabled = true
	body, _ := json.Marshal(current.Config)

	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rr.Code, rr.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload["error"] != "tailer_apply_failed" {
		t.Fatalf("expected tailer_apply_failed, got %+v", payload)
	}
}

func TestPutConfigGnastyUnreachableIsWarningOnly(t *testing.T) {
	resetRuntimeConfigForTest(t)
	t.Setenv("ELORA_GNASTY_ADMIN_BASE", "http://127.0.0.1:1")
	t.Setenv("TWITCH_CHANNEL", "dayoman")
	t.Setenv("YOUTUBE_URL", "@lofigirl")

	var logBuf bytes.Buffer
	prevLogOut := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(prevLogOut) })

	cleanup := withSQLiteStore(t)
	defer cleanup()

	InitRoutes(chatStore)
	router := newConfigRouter()

	getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("expected get 200, got %d body=%s", getRR.Code, getRR.Body.String())
	}

	var current configAPIResponse
	if err := json.Unmarshal(getRR.Body.Bytes(), &current); err != nil {
		t.Fatalf("decode current config: %v", err)
	}
	current.Config.TwitchChannel = "dayoman"
	body, _ := json.Marshal(current.Config)

	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 despite unreachable gnasty, got %d body=%s", rr.Code, rr.Body.String())
	}

	logs := logBuf.String()
	if !strings.Contains(logs, "config: gnasty bulk sync warning") {
		t.Fatalf("expected gnasty warning log, got: %s", logs)
	}
}

func TestInitRuntimeConfigMigratesV1ToV2PreservingValues(t *testing.T) {
	resetRuntimeConfigForTest(t)
	t.Setenv("ELORA_GNASTY_ADMIN_BASE", "")
	cleanup := withSQLiteStore(t)
	defer cleanup()

	v1 := []byte(`{
		"schemaVersion":1,
		"apiBaseUrl":"https://example.com",
		"wsUrl":"wss://example.com/ws/chat",
		"twitchChannel":"dayoman",
		"youtubeSourceUrl":"https://www.youtube.com/@lofigirl/live",
		"features":{"showBadges":false,"hideYouTubeAt":false,"wsEnvelope":true,"wsDropEmpty":true},
		"tailer":{"enabled":true,"pollIntervalMs":250,"maxBatch":300,"maxLagMs":100,"persistOffsets":true,"offsetPath":"/data/offset.json"},
		"websocket":{"pingIntervalMs":25000,"pongWaitMs":30000,"writeDeadlineMs":5000,"maxMessageBytes":131072},
		"ingest":{"gnastyBin":"","gnastyArgs":[],"backoffBaseMs":1000,"backoffMaxMs":30000}
	}`)

	if err := chatStore.UpsertConfig(context.Background(), &storage.ConfigRecord{
		Key:       runtimeconfig.StorageKey,
		Version:   1,
		ValueJSON: string(v1),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed v1 config: %v", err)
	}

	InitRoutes(chatStore)

	rec, err := chatStore.GetConfig(context.Background(), runtimeconfig.StorageKey)
	if err != nil {
		t.Fatalf("load migrated config: %v", err)
	}
	if rec == nil {
		t.Fatalf("expected persisted config record")
	}
	if rec.Version != runtimeconfig.SchemaVersion {
		t.Fatalf("expected migrated version=%d, got %d", runtimeconfig.SchemaVersion, rec.Version)
	}
	var migrated runtimeconfig.Config
	if err := json.Unmarshal([]byte(rec.ValueJSON), &migrated); err != nil {
		t.Fatalf("decode migrated config: %v", err)
	}
	if migrated.TwitchChannel != "dayoman" {
		t.Fatalf("expected v1 twitch channel preserved, got %q", migrated.TwitchChannel)
	}
	if migrated.YouTubeSourceURL != "https://www.youtube.com/@lofigirl/live" {
		t.Fatalf("expected v1 youtube preserved, got %q", migrated.YouTubeSourceURL)
	}
	if len(migrated.Gnasty.Sinks.Enabled) == 0 {
		t.Fatalf("expected gnasty v2 defaults to be populated")
	}
}
