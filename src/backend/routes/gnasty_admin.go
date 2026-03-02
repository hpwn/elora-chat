package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hpwn/EloraChat/src/backend/internal/runtimeconfig"
)

const (
	defaultGnastyAdminBase = "http://gnasty-harvester:8765"
	gnastyAdminTimeout     = 2 * time.Second
	gnastyAdminMaxAttempts = 3
	gnastyAdminRetryDelay  = 300 * time.Millisecond
)

type gnastyConfigPatch struct {
	Sinks   gnastySinksPatch   `json:"sinks"`
	Twitch  gnastyTwitchPatch  `json:"twitch"`
	YouTube gnastyYouTubePatch `json:"youtube"`
}

type gnastySinksPatch struct {
	Enabled    *[]string `json:"enabled,omitempty"`
	BatchSize  *int      `json:"batch_size,omitempty"`
	FlushMaxMS *int      `json:"flush_max_ms,omitempty"`
}

type gnastyTwitchPatch struct {
	Channel             *string `json:"channel,omitempty"`
	Nick                *string `json:"nick,omitempty"`
	TLS                 *bool   `json:"tls,omitempty"`
	DebugDrops          *bool   `json:"debug_drops,omitempty"`
	BackoffMinMS        *int    `json:"backoff_min_ms,omitempty"`
	BackoffMaxMS        *int    `json:"backoff_max_ms,omitempty"`
	RefreshBackoffMinMS *int    `json:"refresh_backoff_min_ms,omitempty"`
	RefreshBackoffMaxMS *int    `json:"refresh_backoff_max_ms,omitempty"`
}

type gnastyYouTubePatch struct {
	URL             *string `json:"url,omitempty"`
	RetrySeconds    *int    `json:"retry_seconds,omitempty"`
	DumpUnhandled   *bool   `json:"dump_unhandled,omitempty"`
	PollTimeoutSecs *int    `json:"poll_timeout_secs,omitempty"`
	PollIntervalMS  *int    `json:"poll_interval_ms,omitempty"`
	Debug           *bool   `json:"debug,omitempty"`
}

type gnastySyncStatus struct {
	mu          sync.RWMutex
	lastAttempt time.Time
	lastSuccess time.Time
	lastError   string
	targetBase  string
}

var gnastySyncState gnastySyncStatus

// GnastySyncSnapshot is the redacted gnasty admin sync status exposed via /configz.
type GnastySyncSnapshot struct {
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	TargetBase    string     `json:"target_base"`
}

func syncGnastyConfigBestEffort(cfg runtimeconfig.Config, reason string) {
	patch := gnastyPatchFromRuntimeConfig(cfg)

	base := gnastyAdminBaseURL()
	if base == "" {
		return
	}
	gnastySyncState.setAttempt(base)

	if err := postGnastyAdminJSON(base, "/admin/config", patch); err != nil {
		gnastySyncState.setError(err.Error())
		log.Printf("config: gnasty bulk sync warning (%s): %v", reason, err)
		return
	}
	gnastySyncState.setSuccess()
}

func gnastyAdminBaseURL() string {
	base := strings.TrimSpace(os.Getenv("ELORA_GNASTY_ADMIN_BASE"))
	if base == "" {
		base = defaultGnastyAdminBase
	}
	return strings.TrimRight(base, "/")
}

func postGnastyAdminJSON(base, path string, payload any) error {
	if strings.TrimSpace(base) == "" {
		return nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload for %s: %w", path, err)
	}

	client := gnastyHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}

	var lastErr error
	for attempt := 1; attempt <= gnastyAdminMaxAttempts; attempt++ {
		lastErr = postGnastyAdminJSONOnce(client, base, path, body)
		if lastErr == nil {
			return nil
		}
		if attempt < gnastyAdminMaxAttempts {
			time.Sleep(gnastyAdminRetryDelay)
		}
	}
	if lastErr != nil {
		return fmt.Errorf("%s request failed after %d attempts: %w", path, gnastyAdminMaxAttempts, lastErr)
	}
	return nil
}

func postGnastyAdminJSONOnce(client *http.Client, base, path string, body []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), gnastyAdminTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request for %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s request failed: %w", path, err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	bodyText := strings.TrimSpace(string(rawBody))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if bodyText == "" {
			bodyText = "<empty>"
		}
		return fmt.Errorf("%s returned %s body=%q", path, resp.Status, bodyText)
	}
	return nil
}

func (s *gnastySyncStatus) setAttempt(target string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.lastAttempt = now
	s.targetBase = target
}

func (s *gnastySyncStatus) setSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.lastSuccess = now
	s.lastError = ""
}

func (s *gnastySyncStatus) setError(raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastError = truncateSyncError(raw)
}

func truncateSyncError(raw string) string {
	const max = 512
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if len(raw) <= max {
		return raw
	}
	return raw[:max] + "...(truncated)"
}

// GnastySyncStatusSnapshot returns the current gnasty sync telemetry.
func GnastySyncStatusSnapshot() GnastySyncSnapshot {
	gnastySyncState.mu.RLock()
	defer gnastySyncState.mu.RUnlock()

	out := GnastySyncSnapshot{
		LastError:  gnastySyncState.lastError,
		TargetBase: gnastySyncState.targetBase,
	}
	if !gnastySyncState.lastAttempt.IsZero() {
		ts := gnastySyncState.lastAttempt
		out.LastAttemptAt = &ts
	}
	if !gnastySyncState.lastSuccess.IsZero() {
		ts := gnastySyncState.lastSuccess
		out.LastSuccessAt = &ts
	}
	return out
}

func gnastyPatchFromRuntimeConfig(cfg runtimeconfig.Config) gnastyConfigPatch {
	channel := strings.TrimSpace(cfg.TwitchChannel)
	if normalized, err := runtimeconfig.NormalizeTwitchChannelInput(channel); err == nil {
		channel = normalized
	} else {
		channel = strings.ToLower(strings.TrimPrefix(channel, "#"))
	}

	ytURL := strings.TrimSpace(cfg.YouTubeSourceURL)
	if ytURL != "" {
		if normalized, err := runtimeconfig.NormalizeYouTubeSourceInput(ytURL); err == nil {
			ytURL = normalized
		}
	}

	sinkEnabled := append([]string(nil), cfg.Gnasty.Sinks.Enabled...)
	sinkBatch := cfg.Gnasty.Sinks.BatchSize
	sinkFlush := cfg.Gnasty.Sinks.FlushMaxMS
	tls := cfg.Gnasty.Twitch.TLS
	debugDrops := cfg.Gnasty.Twitch.DebugDrops
	backoffMin := cfg.Gnasty.Twitch.BackoffMinMS
	backoffMax := cfg.Gnasty.Twitch.BackoffMaxMS
	refreshBackoffMin := cfg.Gnasty.Twitch.RefreshBackoffMinMS
	refreshBackoffMax := cfg.Gnasty.Twitch.RefreshBackoffMaxMS
	retrySeconds := cfg.Gnasty.YouTube.RetrySeconds
	dumpUnhandled := cfg.Gnasty.YouTube.DumpUnhandled
	pollTimeout := cfg.Gnasty.YouTube.PollTimeoutSecs
	pollInterval := cfg.Gnasty.YouTube.PollIntervalMS
	ytDebug := cfg.Gnasty.YouTube.Debug

	patch := gnastyConfigPatch{
		Sinks: gnastySinksPatch{
			Enabled:    &sinkEnabled,
			BatchSize:  &sinkBatch,
			FlushMaxMS: &sinkFlush,
		},
		Twitch: gnastyTwitchPatch{
			Channel:             &channel,
			TLS:                 &tls,
			DebugDrops:          &debugDrops,
			BackoffMinMS:        &backoffMin,
			BackoffMaxMS:        &backoffMax,
			RefreshBackoffMinMS: &refreshBackoffMin,
			RefreshBackoffMaxMS: &refreshBackoffMax,
		},
		YouTube: gnastyYouTubePatch{
			URL:             &ytURL,
			RetrySeconds:    &retrySeconds,
			DumpUnhandled:   &dumpUnhandled,
			PollTimeoutSecs: &pollTimeout,
			PollIntervalMS:  &pollInterval,
			Debug:           &ytDebug,
		},
	}

	nick := strings.TrimSpace(cfg.Gnasty.Twitch.Nick)
	if nick != "" {
		patch.Twitch.Nick = &nick
	}

	return patch
}
