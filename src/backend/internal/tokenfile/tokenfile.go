package tokenfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	mu        sync.Mutex
	lastPath  string
	lastToken string
)

// ErrEmptyToken indicates that the provided token was blank after trimming.
var ErrEmptyToken = errors.New("tokenfile: empty token")

// PathFromEnv returns the configured file path for exporting the Twitch token.
// If exporting is disabled, an empty string is returned.
func PathFromEnv() string {
	file := strings.TrimSpace(os.Getenv("ELORA_TWITCH_TOKEN_FILE"))
	if file == "" {
		file = strings.TrimSpace(os.Getenv("TWITCH_TOKEN_FILE"))
	}
	if file == "" {
		return ""
	}
	dir := strings.TrimSpace(os.Getenv("ELORA_TWITCH_TOKEN_DIR"))
	if dir != "" && !filepath.IsAbs(file) {
		file = filepath.Join(dir, file)
	}
	return filepath.Clean(file)
}

func dirFromEnv(path string) string {
	if path == "" {
		return ""
	}
	dir := strings.TrimSpace(os.Getenv("ELORA_TWITCH_TOKEN_DIR"))
	if dir == "" {
		return filepath.Clean(filepath.Dir(path))
	}
	return filepath.Clean(dir)
}

func ensureOAuthPrefix(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return token
	}
	if strings.HasPrefix(token, "oauth:") {
		return token
	}
	return "oauth:" + token
}

// Save writes the Twitch token to the configured file atomically with strict
// permissions. When exporting is disabled, Save returns nil.
func Save(token string) error {
	path := PathFromEnv()
	if path == "" {
		return nil
	}

	token = ensureOAuthPrefix(token)
	if strings.TrimSpace(token) == "" {
		return ErrEmptyToken
	}

	mu.Lock()
	defer mu.Unlock()

	if lastPath == path && lastToken == token {
		return nil
	}

	dirCfg := dirFromEnv(path)
	actualDir := filepath.Clean(filepath.Dir(path))

	// Ensure directories exist with strict permissions where possible.
	if dirCfg != "" && dirCfg != "." {
		if err := os.MkdirAll(dirCfg, 0o700); err != nil {
			return fmt.Errorf("tokenfile: mkdir %s: %w", dirCfg, err)
		}
	}
	if actualDir != "" && actualDir != "." && actualDir != dirCfg {
		if err := os.MkdirAll(actualDir, 0o700); err != nil {
			return fmt.Errorf("tokenfile: mkdir %s: %w", actualDir, err)
		}
	}

	tmp := filepath.Join(actualDir, "."+filepath.Base(path)+".tmp")
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmp)
		}
	}()

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("tokenfile: open tmp: %w", err)
	}

	if _, err := f.WriteString(token + "\n"); err != nil {
		_ = f.Close()
		return fmt.Errorf("tokenfile: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("tokenfile: fsync file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("tokenfile: close: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("tokenfile: rename: %w", err)
	}
	cleanup = false

	syncDir := func(dir string) {
		if dir == "" {
			return
		}
		d, err := os.Open(dir)
		if err != nil {
			return
		}
		_ = d.Sync()
		_ = d.Close()
	}

	if actualDir != "" {
		syncDir(actualDir)
	}
	if dirCfg != "" && dirCfg != actualDir {
		syncDir(dirCfg)
	}

	lastPath = path
	lastToken = token
	return nil
}
