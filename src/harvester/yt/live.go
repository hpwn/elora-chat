package yt

import "log"

// Config controls runtime behavior for the YouTube live worker.
type Config struct {
	DumpUnhandled bool
}

// LiveWorker handles YouTube live actions streamed from gnasty.
type LiveWorker struct {
	logger *log.Logger
	cfg    Config
}

// NewLiveWorker constructs a LiveWorker.
func NewLiveWorker(logger *log.Logger, cfg Config) *LiveWorker {
	return &LiveWorker{logger: logger, cfg: cfg}
}

// LogUnhandledAction dumps the raw action when enabled.
func (w *LiveWorker) LogUnhandledAction(action string) {
	if !w.cfg.DumpUnhandled {
		return
	}

	w.logger.Printf("unhandled action dump: %s", action)
}
