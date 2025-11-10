package main

import (
	"log"
	"os"

	"github.com/hpwn/EloraChat/src/harvester/yt"
)

func main() {
	dumpUnhandled := os.Getenv("GNASTY_YT_DUMP_UNHANDLED") == "1"
	logger := log.Default()

	cfg := yt.Config{DumpUnhandled: dumpUnhandled}
	worker := yt.NewLiveWorker(logger, cfg)
	_ = worker
}
