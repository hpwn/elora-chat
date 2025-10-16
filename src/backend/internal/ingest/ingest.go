package ingest

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DriverChatDownloader = "chatdownloader"
	DriverGnasty         = "gnasty"
)

type Env struct {
	Driver        string
	GnastyBin     string
	GnastyArgs    []string
	BackoffBaseMS int
	BackoffMaxMS  int
}

func FromEnv() Env {
	return Env{
		Driver:        getEnvTrim("ELORA_INGEST_DRIVER", DriverChatDownloader),
		GnastyBin:     getEnvTrim("GNASTY_BIN", ""),
		GnastyArgs:    splitCSV(getEnvTrim("GNASTY_ARGS", "")),
		BackoffBaseMS: getEnvInt("GNASTY_BACKOFF_BASE_MS", 1000),
		BackoffMaxMS:  getEnvInt("GNASTY_BACKOFF_MAX_MS", 30000),
	}
}

// New returns an Env configured for the requested driver.
func New(driver string) (Env, error) {
	trimmed := strings.TrimSpace(driver)
	if trimmed == "" {
		trimmed = DriverChatDownloader
	}
	switch trimmed {
	case DriverChatDownloader:
		return Env{Driver: DriverChatDownloader}, nil
	case DriverGnasty:
		return Env{Driver: DriverGnasty}, nil
	default:
		return Env{}, fmt.Errorf("ingest: unsupported driver %q", driver)
	}
}

func (e Env) BuildGnasty(insert InsertFn, urls []string, logger *log.Logger) (*GnastyProcess, error) {
	cfg := GnastyConfig{
		Bin:         e.GnastyBin,
		Args:        e.GnastyArgs,
		BackoffBase: time.Duration(e.BackoffBaseMS) * time.Millisecond,
		BackoffMax:  time.Duration(e.BackoffMaxMS) * time.Millisecond,
		Logger:      logger,
		Insert:      insert,
	}
	return NewGnasty(cfg, urls)
}

func getEnvTrim(key, def string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	return val
}

func getEnvInt(key string, def int) int {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return def
	}
	return n
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
