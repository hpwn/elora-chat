package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hpwn/EloraChat/src/harvester/yt"
)

const (
	envDumpUnhandled = "GNASTY_YT_DUMP_UNHANDLED"
	envPollTimeout   = "GNASTY_YT_POLL_TIMEOUT_SECS"
	envPollInterval  = "GNASTY_YT_POLL_INTERVAL_MS"
	envLiveURL       = "GNASTY_YT_URL"
	defaultTimeout   = 20 * time.Second
	defaultInterval  = 3000
)

func main() {
	ctx := context.Background()
	logger := log.Default()

	dumpUnhandled := getBoolEnv(envDumpUnhandled, false)

	pollTimeout := defaultTimeout
	if raw := strings.TrimSpace(os.Getenv(envPollTimeout)); raw != "" {
		if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
			pollTimeout = time.Duration(secs) * time.Second
		} else {
			logger.Printf("yt config: invalid %s=%q, using default %s", envPollTimeout, raw, defaultTimeout)
		}
	}

	pollIntervalMS := defaultInterval
	if raw := strings.TrimSpace(os.Getenv(envPollInterval)); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
			pollIntervalMS = ms
		} else {
			logger.Printf("yt config: invalid %s=%q, using default %d", envPollInterval, raw, defaultInterval)
		}
	}

	liveURL := strings.TrimSpace(os.Getenv(envLiveURL))

	ytCfg := yt.Config{
		DumpUnhandled:  dumpUnhandled,
		PollTimeout:    pollTimeout,
		PollIntervalMS: pollIntervalMS,
		LiveURL:        liveURL,
	}

	logger.Printf(
		"yt: settings dump_unhandled=%v poll_timeout=%s poll_interval_ms=%d url=%s",
		ytCfg.DumpUnhandled,
		ytCfg.PollTimeout,
		ytCfg.PollIntervalMS,
		ytCfg.LiveURL,
	)

	worker := yt.NewLiveWorker(logger, ytCfg)
	if err := worker.Run(ctx); err != nil {
		logger.Printf("ytlive: worker stopped: %v", err)
	}
}

func getBoolEnv(name string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}

	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}
