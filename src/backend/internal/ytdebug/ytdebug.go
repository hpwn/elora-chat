package ytdebug

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FingerprintInput captures the minimal fields used to compute a stable fingerprint
// for YouTube chat messages.
type FingerprintInput struct {
	Platform  string
	ChannelID string
	Username  string
	Text      string
	Timestamp time.Time
}

var (
	enabledOnce sync.Once
	enabled     bool
)

// Enabled reports whether verbose YouTube debugging is enabled via ELORA_YT_DEBUG.
// The flag is disabled in production environments to avoid noisy logs.
func Enabled() bool {
	enabledOnce.Do(func() {
		raw := strings.TrimSpace(os.Getenv("ELORA_YT_DEBUG"))
		if raw == "" {
			return
		}
		ok, err := strconv.ParseBool(raw)
		if err != nil || !ok {
			return
		}

		env := strings.ToLower(strings.TrimSpace(os.Getenv("ELORA_ENV")))
		if env == "" {
			env = strings.ToLower(strings.TrimSpace(os.Getenv("GO_ENV")))
		}
		if env == "" {
			env = strings.ToLower(strings.TrimSpace(os.Getenv("ENVIRONMENT")))
		}
		if env == "production" || env == "prod" {
			log.Printf("ytdebug: refusing to enable in environment %q", env)
			return
		}

		enabled = true
	})
	return enabled
}

// Fingerprint returns a stable hex digest for the provided YouTube message fields.
// ChannelID is optional but should be provided when available to minimize collisions.
func Fingerprint(in FingerprintInput) string {
	// sha1 is sufficient for a compact, deterministic fingerprint here.
	h := sha1.New() // nolint:gosec
	writeComponent(h, strings.ToLower(strings.TrimSpace(in.Platform)))
	writeComponent(h, strings.TrimSpace(in.ChannelID))
	writeComponent(h, strings.TrimSpace(in.Username))
	writeComponent(h, strings.TrimSpace(in.Text))
	writeComponent(h, strconv.FormatInt(in.Timestamp.UTC().UnixMilli(), 10))
	return hex.EncodeToString(h.Sum(nil))
}

func writeComponent(h hashWriter, component string) {
	if component == "" {
		component = "-"
	}
	_, _ = h.Write([]byte(component))
	_, _ = h.Write([]byte{0})
}

type hashWriter interface {
	Write([]byte) (int, error)
}

// ChannelIDFromRaw best-effort extracts a channel identifier from a raw JSON payload.
// Unknown or malformed payloads return an empty string.
func ChannelIDFromRaw(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	// Handle both flat and nested encodings without failing on new fields.
	var tmp map[string]any
	if err := json.Unmarshal([]byte(raw), &tmp); err != nil {
		return ""
	}

	if v, ok := tmp["channel_id"].(string); ok {
		return strings.TrimSpace(v)
	}
	if v, ok := tmp["channelId"].(string); ok {
		return strings.TrimSpace(v)
	}

	if nested, ok := tmp["channel"].(map[string]any); ok {
		if v, ok := nested["id"].(string); ok {
			return strings.TrimSpace(v)
		}
	}

	return ""
}

// LogMessage prints a structured debug line for a YouTube message when enabled.
func LogMessage(stage string, in FingerprintInput, extra map[string]any) {
	if !Enabled() {
		return
	}

	fp := Fingerprint(in)

	payload := map[string]any{
		"stage":       stage,
		"platform":    in.Platform,
		"channel_id":  in.ChannelID,
		"username":    in.Username,
		"ts_ms":       in.Timestamp.UTC().UnixMilli(),
		"fingerprint": fp,
	}
	for k, v := range extra {
		payload[k] = v
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		log.Printf("ytdebug: %s platform=%s user=%s ts=%d fingerprint=%s", stage, in.Platform, in.Username, in.Timestamp.UTC().UnixMilli(), fp)
		return
	}
	log.Printf("ytdebug: %s", string(raw))
}
