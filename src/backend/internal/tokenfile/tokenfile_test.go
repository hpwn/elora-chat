package tokenfile

import (
	"os"
	"path/filepath"
	"testing"
)

func resetCache() {
	mu.Lock()
	defer mu.Unlock()
	lastPath = ""
	lastToken = ""
}

func TestPathFromEnvWithDir(t *testing.T) {
	resetCache()
	t.Setenv("ELORA_TWITCH_TOKEN_FILE", "twitch_token")
	t.Setenv("ELORA_TWITCH_TOKEN_DIR", "/tmp/shared")

	got := PathFromEnv()
	want := filepath.Join("/tmp/shared", "twitch_token")
	if got != want {
		t.Fatalf("PathFromEnv = %q, want %q", got, want)
	}
}

func TestPathFromEnvAbsolute(t *testing.T) {
	resetCache()
	t.Setenv("ELORA_TWITCH_TOKEN_FILE", "/opt/tok")
	t.Setenv("ELORA_TWITCH_TOKEN_DIR", "/tmp/shared")

	got := PathFromEnv()
	if got != "/opt/tok" {
		t.Fatalf("PathFromEnv = %q, want %q", got, "/opt/tok")
	}
}

func TestSaveWritesAtomicWithPerms(t *testing.T) {
	resetCache()

	root := t.TempDir()
	dir := filepath.Join(root, "shared")
	file := "twitch_token"
	t.Setenv("ELORA_TWITCH_TOKEN_DIR", dir)
	t.Setenv("ELORA_TWITCH_TOKEN_FILE", file)

	if err := Save("abc123"); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	path := filepath.Join(dir, file)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "oauth:abc123\n" {
		t.Fatalf("content = %q, want oauth:abc123\\n", string(content))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file perm = %v, want 0600", got)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("dir perm = %v, want 0700", got)
	}

	tmpPath := filepath.Join(dir, "."+file+".tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("temporary file still exists: %v", err)
	}
}

func TestSaveDisabled(t *testing.T) {
	resetCache()

	if err := Save("abc123"); err != nil {
		t.Fatalf("Save returned error when disabled: %v", err)
	}
}

func TestSaveEmptyToken(t *testing.T) {
	resetCache()
	t.Setenv("ELORA_TWITCH_TOKEN_FILE", filepath.Join(t.TempDir(), "tok"))

	if err := Save(" \t\n"); err != ErrEmptyToken {
		t.Fatalf("Save error = %v, want ErrEmptyToken", err)
	}
}

func TestSavePrefersEloraEnvOverAlias(t *testing.T) {
	resetCache()
	root := t.TempDir()
	t.Setenv("ELORA_TWITCH_TOKEN_FILE", filepath.Join(root, "primary"))
	t.Setenv("TWITCH_TOKEN_FILE", filepath.Join(root, "alias"))

	if err := Save("xyz"); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "primary")); err != nil {
		t.Fatalf("primary file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "alias")); !os.IsNotExist(err) {
		t.Fatalf("alias file should not exist: %v", err)
	}
}

func TestSaveSkipsDuplicateToken(t *testing.T) {
	resetCache()
	root := t.TempDir()
	path := filepath.Join(root, "tok")
	t.Setenv("ELORA_TWITCH_TOKEN_FILE", path)

	if err := Save("abc"); err != nil {
		t.Fatalf("first save error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat error: %v", err)
	}

	// Second save should not modify the file; we expect mtime to remain the same.
	if err := Save("abc"); err != nil {
		t.Fatalf("second save error: %v", err)
	}
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat error after second save: %v", err)
	}
	if !info.ModTime().Equal(info2.ModTime()) {
		t.Fatalf("modtime changed: %v -> %v", info.ModTime(), info2.ModTime())
	}
}
