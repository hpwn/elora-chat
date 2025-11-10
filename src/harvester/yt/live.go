package yt

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"
)

// Config controls runtime behavior for the YouTube live worker.
type Config struct {
	DumpUnhandled bool
	PollTimeout   time.Duration
}

// LiveWorker handles YouTube live actions streamed from gnasty.
type LiveWorker struct {
	logger *log.Logger
	cfg    Config
	client *http.Client

	pollInterval time.Duration
}

// NewLiveWorker constructs a LiveWorker.
func NewLiveWorker(logger *log.Logger, cfg Config) *LiveWorker {
	if cfg.PollTimeout <= 0 {
		cfg.PollTimeout = 20 * time.Second
	}

	return &LiveWorker{
		logger:       logger,
		cfg:          cfg,
		client:       &http.Client{},
		pollInterval: 3 * time.Second,
	}
}

// LogUnhandledAction dumps the raw action when enabled.
func (w *LiveWorker) LogUnhandledAction(action string) {
	if !w.cfg.DumpUnhandled {
		return
	}

	w.logger.Printf("unhandled action dump: %s", action)
}

// Run starts the polling loop until the provided context is cancelled.
func (w *LiveWorker) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	var continuation string
	for {
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}

		pollCtx, cancel := context.WithTimeout(ctx, w.cfg.PollTimeout)
		summary, nextContinuation, err := w.pollOnce(pollCtx, continuation)
		cancel()

		if err != nil {
			// Treat context cancellation specially so we exit cleanly when shutting down.
			if errors.Is(err, context.Canceled) {
				return nil
			}

			if errors.Is(err, context.DeadlineExceeded) {
				w.logger.Printf("ytlive: poll timed out after %s", w.cfg.PollTimeout)
			} else {
				w.logger.Printf("ytlive: poll error: %v", err)
			}

			if !w.wait(ctx, w.pollInterval) {
				return nil
			}

			continue
		}

		if summary != "" {
			w.logger.Printf("ytlive: poll summary: %s", summary)
		}

		continuation = nextContinuation

		if !w.wait(ctx, w.pollInterval) {
			return nil
		}
	}
}

func (w *LiveWorker) pollOnce(ctx context.Context, continuation string) (string, string, error) {
	// Placeholder implementation. Actual YouTube polling will make HTTP requests using the
	// provided context and return a summary string when new events are received.
	select {
	case <-ctx.Done():
		return "", continuation, ctx.Err()
	case <-time.After(500 * time.Millisecond):
		return "", continuation, nil
	}
}

func (w *LiveWorker) wait(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
