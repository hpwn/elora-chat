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
	"time"

	"github.com/hpwn/EloraChat/src/backend/internal/runtimeconfig"
)

const (
	defaultGnastyAdminBase = "http://gnasty-harvester:8765"
	gnastyAdminTimeout     = 750 * time.Millisecond
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

func syncGnastyConfigBestEffort(cfg runtimeconfig.Config, reason string) {
	patch := gnastyPatchFromRuntimeConfig(cfg)

	base := gnastyAdminBaseURL()
	if base == "" {
		return
	}

	if err := postGnastyAdminJSON(base, "/admin/config", patch); err != nil {
		log.Printf("config: gnasty bulk sync warning (%s): %v", reason, err)
	}
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
