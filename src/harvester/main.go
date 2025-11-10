package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/hpwn/EloraChat/src/harvester/yt"
)

const (
	envDumpUnhandled = "GNASTY_YT_DUMP_UNHANDLED"
	envPollTimeout   = "GNASTY_YT_POLL_TIMEOUT_SECS"
	defaultTimeout   = 20 * time.Second
)

func main() {
	ctx := context.Background()
	logger := log.Default()

	dumpUnhandled := os.Getenv(envDumpUnhandled) == "1"

	pollTimeout := defaultTimeout
	if raw := os.Getenv(envPollTimeout); raw != "" {
		if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
			pollTimeout = time.Duration(secs) * time.Second
		} else {
			logger.Printf("yt config: invalid %s=%q, using default %s", envPollTimeout, raw, defaultTimeout)
		}
	}

	logger.Printf("yt config: dump_unhandled=%v poll_timeout=%s", dumpUnhandled, pollTimeout)

	cfg := yt.Config{
		DumpUnhandled: dumpUnhandled,
		PollTimeout:   pollTimeout,
	}

	worker := yt.NewLiveWorker(logger, cfg)
	if err := worker.Run(ctx); err != nil {
		logger.Printf("ytlive: worker stopped: %v", err)
	}
}
