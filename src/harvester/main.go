package main

import (
	"context"
	"log"
	"os"

	"github.com/hpwn/EloraChat/src/harvester/yt"
)

func main() {
	ctx := context.Background()
	dumpUnhandled := os.Getenv("GNASTY_YT_DUMP_UNHANDLED") == "1"
	logger := log.Default()

	cfg := yt.Config{DumpUnhandled: dumpUnhandled}
	worker := yt.NewLiveWorker(logger, cfg)
	if err := worker.Run(ctx); err != nil {
		logger.Printf("ytlive: worker stopped: %v", err)
	}
}
