package ingest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// InsertFn is an optional hook to deliver raw decoded NDJSON payloads to the app.
// It will be wired to the real storage pipeline in a subsequent slice.
type InsertFn func(ctx context.Context, raw json.RawMessage) error

type GnastyConfig struct {
	Bin         string
	Args        []string
	BackoffBase time.Duration
	BackoffMax  time.Duration
	Logger      *log.Logger
	Insert      InsertFn
}

type GnastyProcess struct {
	cfg  GnastyConfig
	urls []string

	wg sync.WaitGroup
}

func NewGnasty(cfg GnastyConfig, urls []string) (*GnastyProcess, error) {
	if strings.TrimSpace(cfg.Bin) == "" {
		return nil, errors.New("gnasty: Bin is required")
	}
	if cfg.BackoffBase <= 0 {
		cfg.BackoffBase = time.Second
	}
	if cfg.BackoffMax <= 0 {
		cfg.BackoffMax = 30 * time.Second
	}
	if cfg.BackoffBase > cfg.BackoffMax {
		cfg.BackoffMax = cfg.BackoffBase
	}
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	cleanURLs := make([]string, 0, len(urls))
	for _, u := range urls {
		if trimmed := strings.TrimSpace(u); trimmed != "" {
			cleanURLs = append(cleanURLs, trimmed)
		}
	}
	return &GnastyProcess{cfg: cfg, urls: cleanURLs}, nil
}

func (g *GnastyProcess) Start(ctx context.Context) {
	for _, url := range g.urls {
		g.wg.Add(1)
		go g.run(ctx, url)
	}
}

func (g *GnastyProcess) Wait() {
	g.wg.Wait()
}

func (g *GnastyProcess) run(ctx context.Context, url string) {
	defer g.wg.Done()
	logger := g.cfg.Logger
	base := g.cfg.BackoffBase
	max := g.cfg.BackoffMax
	backoff := base

	for {
		select {
		case <-ctx.Done():
			logger.Printf("ingest[gnasty]: context canceled for url=%s", url)
			return
		default:
		}

		args := append([]string{}, g.cfg.Args...)
		args = append(args, url)

		cmd := exec.CommandContext(ctx, g.cfg.Bin, args...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			logger.Printf("ingest[gnasty]: url=%s: stdout pipe error: %v", url, err)
			if !sleepWithContext(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff, base, max)
			continue
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			logger.Printf("ingest[gnasty]: url=%s: stderr pipe error: %v", url, err)
			if !sleepWithContext(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff, base, max)
			continue
		}

		logger.Printf("ingest[gnasty]: starting: bin=%q args=%q url=%s", g.cfg.Bin, strings.Join(g.cfg.Args, " "), url)
		if err := cmd.Start(); err != nil {
			logger.Printf("ingest[gnasty]: url=%s: start error: %v", url, err)
			if !sleepWithContext(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff, base, max)
			continue
		}
		backoff = base

		errCh := make(chan struct{})
		go func() {
			defer close(errCh)
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line != "" {
					logger.Printf("ingest[gnasty][stderr]: url=%s: %s", url, line)
				}
			}
		}()

		reader := bufio.NewReaderSize(stdout, 1<<20)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				g.handleLine(ctx, url, bytes.TrimSpace(line))
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				logger.Printf("ingest[gnasty]: url=%s: read error: %v", url, err)
				break
			}
		}

		<-errCh
		if err := cmd.Wait(); err != nil {
			logger.Printf("ingest[gnasty]: url=%s: exited err: %v", url, err)
		} else {
			logger.Printf("ingest[gnasty]: url=%s: exited ok", url)
		}

		select {
		case <-ctx.Done():
			logger.Printf("ingest[gnasty]: context canceled post-exit for url=%s", url)
			return
		default:
		}

		logger.Printf("ingest[gnasty]: url=%s: restarting in %s", url, backoff)
		if !sleepWithContext(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff, base, max)
	}
}

func (g *GnastyProcess) handleLine(ctx context.Context, url string, line []byte) {
	logger := g.cfg.Logger
	if len(line) == 0 {
		return
	}
	var tmp json.RawMessage
	if err := json.Unmarshal(line, &tmp); err != nil {
		logger.Printf("ingest[gnasty]: url=%s: decode error: %v; line=%s", url, err, ellipsis(string(line), 240))
		return
	}

	if g.cfg.Insert != nil {
		if err := g.cfg.Insert(ctx, json.RawMessage(append([]byte(nil), tmp...))); err != nil {
			logger.Printf("ingest[gnasty]: url=%s: insert error: %v", url, err)
		}
		return
	}
	logger.Printf("ingest[gnasty]: url=%s: line ok (len=%d)", url, len(tmp))
}

func nextBackoff(cur, base, max time.Duration) time.Duration {
	if cur < base {
		return base
	}
	next := cur * 2
	if next > max {
		return max
	}
	return next
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func ellipsis(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return fmt.Sprintf("%s…(+%d)", s[:limit], len(s)-limit)
}
