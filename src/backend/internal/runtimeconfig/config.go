package runtimeconfig

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	SchemaVersion = 2
	StorageKey    = "runtime_config"
)

type Config struct {
	SchemaVersion    int             `json:"schemaVersion"`
	APIBaseURL       string          `json:"apiBaseUrl"`
	WSURL            string          `json:"wsUrl"`
	AllowedOrigins   []string        `json:"allowedOrigins"`
	TwitchChannel    string          `json:"twitchChannel"`
	YouTubeSourceURL string          `json:"youtubeSourceUrl"`
	Features         FeatureConfig   `json:"features"`
	Tailer           TailerConfig    `json:"tailer"`
	Websocket        WebsocketConfig `json:"websocket"`
	Ingest           IngestConfig    `json:"ingest"`
	Gnasty           GnastyConfig    `json:"gnasty"`
}

type FeatureConfig struct {
	ShowBadges    bool `json:"showBadges"`
	HideYouTubeAt bool `json:"hideYouTubeAt"`
	WSEnvelope    bool `json:"wsEnvelope"`
	WSDropEmpty   bool `json:"wsDropEmpty"`
}

type TailerConfig struct {
	Enabled        bool   `json:"enabled"`
	PollIntervalMS int    `json:"pollIntervalMs"`
	MaxBatch       int    `json:"maxBatch"`
	MaxLagMS       int    `json:"maxLagMs"`
	PersistOffsets bool   `json:"persistOffsets"`
	OffsetPath     string `json:"offsetPath"`
}

type WebsocketConfig struct {
	PingIntervalMS  int   `json:"pingIntervalMs"`
	PongWaitMS      int   `json:"pongWaitMs"`
	WriteDeadlineMS int   `json:"writeDeadlineMs"`
	MaxMessageBytes int64 `json:"maxMessageBytes"`
}

type IngestConfig struct {
	GnastyBin     string   `json:"gnastyBin"`
	GnastyArgs    []string `json:"gnastyArgs"`
	BackoffBaseMS int      `json:"backoffBaseMs"`
	BackoffMaxMS  int      `json:"backoffMaxMs"`
}

type GnastyConfig struct {
	Sinks   GnastySinkConfig    `json:"sinks"`
	Twitch  GnastyTwitchConfig  `json:"twitch"`
	YouTube GnastyYouTubeConfig `json:"youtube"`
}

type GnastySinkConfig struct {
	Enabled    []string `json:"enabled"`
	BatchSize  int      `json:"batchSize"`
	FlushMaxMS int      `json:"flushMaxMs"`
}

type GnastyTwitchConfig struct {
	Nick                string `json:"nick"`
	TLS                 bool   `json:"tls"`
	DebugDrops          bool   `json:"debugDrops"`
	BackoffMinMS        int    `json:"backoffMinMs"`
	BackoffMaxMS        int    `json:"backoffMaxMs"`
	RefreshBackoffMinMS int    `json:"refreshBackoffMinMs"`
	RefreshBackoffMaxMS int    `json:"refreshBackoffMaxMs"`
}

type GnastyYouTubeConfig struct {
	RetrySeconds    int  `json:"retrySeconds"`
	DumpUnhandled   bool `json:"dumpUnhandled"`
	PollTimeoutSecs int  `json:"pollTimeoutSecs"`
	PollIntervalMS  int  `json:"pollIntervalMs"`
	Debug           bool `json:"debug"`
}

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type EnvOnlySecrets struct {
	TwitchClientSecret SecretState `json:"twitchClientSecret"`
	YouTubeAPIKey      SecretState `json:"youtubeApiKey"`
}

type SecretState struct {
	Configured bool   `json:"configured"`
	Value      string `json:"value"`
	Source     string `json:"source"`
}

func DefaultsFromEnv() Config {
	httpPort := strings.TrimSpace(os.Getenv("ELORA_HTTP_PORT"))
	if httpPort == "" {
		httpPort = strings.TrimSpace(os.Getenv("PORT"))
	}
	if httpPort == "" {
		httpPort = "8080"
	}

	apiBase := strings.TrimSpace(os.Getenv("VITE_PUBLIC_API_BASE"))
	if apiBase == "" {
		apiBase = "http://localhost:" + httpPort
	}
	apiBase = strings.TrimRight(apiBase, "/")
	if apiBase == "" {
		apiBase = "http://localhost:" + httpPort
	}

	wsURL := strings.TrimSpace(os.Getenv("VITE_PUBLIC_WS_URL"))
	if wsURL == "" {
		if parsed, err := url.Parse(apiBase); err == nil {
			scheme := "ws"
			switch strings.ToLower(parsed.Scheme) {
			case "https":
				scheme = "wss"
			case "ws", "wss":
				scheme = strings.ToLower(parsed.Scheme)
			}
			host := parsed.Host
			if host == "" {
				host = "localhost:" + httpPort
			}
			wsURL = scheme + "://" + host + "/ws/chat"
		}
	}
	if wsURL == "" {
		wsURL = "ws://localhost:" + httpPort + "/ws/chat"
	}

	cfg := Config{
		SchemaVersion:    SchemaVersion,
		APIBaseURL:       apiBase,
		WSURL:            wsURL,
		AllowedOrigins:   splitCSV(getEnvFirst([]string{"ELORA_WS_ALLOWED_ORIGINS", "ELORA_ALLOWED_ORIGINS"})),
		TwitchChannel:    strings.TrimSpace(strings.ToLower(os.Getenv("TWITCH_CHANNEL"))),
		YouTubeSourceURL: strings.TrimSpace(os.Getenv("YOUTUBE_URL")),
		Features: FeatureConfig{
			ShowBadges:    envBool("ELORA_UI_SHOW_BADGES", true),
			HideYouTubeAt: !envBool("ELORA_UI_YT_PREFIX_AT", false),
			WSEnvelope:    envBool("ELORA_WS_ENVELOPE", true),
			WSDropEmpty:   envBool("ELORA_WS_DROP_EMPTY", true),
		},
		Tailer: TailerConfig{
			Enabled:        envBoolAny([]string{"ELORA_TAILER_ENABLED", "ELORA_DB_TAIL_ENABLED"}, false),
			PollIntervalMS: envIntAny([]string{"ELORA_TAILER_POLL_MS", "ELORA_DB_TAIL_INTERVAL_MS", "ELORA_DB_TAIL_POLL_MS"}, 1000),
			MaxBatch:       envIntAny([]string{"ELORA_TAILER_MAX_BATCH", "ELORA_DB_TAIL_BATCH"}, 200),
			MaxLagMS:       envInt("ELORA_TAILER_MAX_LAG_MS", 0),
			PersistOffsets: envBool("ELORA_TAILER_PERSIST_OFFSETS", false),
			OffsetPath:     strings.TrimSpace(os.Getenv("ELORA_TAILER_OFFSET_PATH")),
		},
		Websocket: WebsocketConfig{
			PingIntervalMS:  envInt("ELORA_WS_PING_INTERVAL_MS", 25000),
			PongWaitMS:      envInt("ELORA_WS_PONG_WAIT_MS", 30000),
			WriteDeadlineMS: envInt("ELORA_WS_WRITE_DEADLINE_MS", 5000),
			MaxMessageBytes: envInt64("ELORA_WS_MAX_MESSAGE_BYTES", 131072),
		},
		Ingest: IngestConfig{
			GnastyBin:     strings.TrimSpace(os.Getenv("GNASTY_BIN")),
			GnastyArgs:    splitCSV(strings.TrimSpace(os.Getenv("GNASTY_ARGS"))),
			BackoffBaseMS: envInt("GNASTY_BACKOFF_BASE_MS", 1000),
			BackoffMaxMS:  envInt("GNASTY_BACKOFF_MAX_MS", 30000),
		},
		Gnasty: GnastyConfig{
			Sinks: GnastySinkConfig{
				Enabled:    splitListCSV(getEnvFirst([]string{"GNASTY_SINKS", "GNASTY_RECEIVERS"}), []string{"sqlite"}),
				BatchSize:  envInt("GNASTY_SINK_BATCH_SIZE", 1),
				FlushMaxMS: envInt("GNASTY_SINK_FLUSH_MAX_MS", 0),
			},
			Twitch: GnastyTwitchConfig{
				Nick:                strings.TrimSpace(getEnvFirst([]string{"GNASTY_TWITCH_NICK", "TWITCH_NICK"})),
				TLS:                 envBoolAny([]string{"GNASTY_TWITCH_TLS", "TWITCH_TLS"}, true),
				DebugDrops:          envBool("GNASTY_TWITCH_DEBUG_DROPS", false),
				BackoffMinMS:        envInt("GNASTY_TWITCH_BACKOFF_MIN_MS", 1000),
				BackoffMaxMS:        envInt("GNASTY_TWITCH_BACKOFF_MAX_MS", 60000),
				RefreshBackoffMinMS: envInt("GNASTY_TWITCH_REFRESH_BACKOFF_MIN_MS", 1000),
				RefreshBackoffMaxMS: envInt("GNASTY_TWITCH_REFRESH_BACKOFF_MAX_MS", 60000),
			},
			YouTube: GnastyYouTubeConfig{
				RetrySeconds:    envInt("GNASTY_YT_RETRY_SECS", 30),
				DumpUnhandled:   envBool("GNASTY_YT_DUMP_UNHANDLED", false),
				PollTimeoutSecs: envInt("GNASTY_YT_POLL_TIMEOUT_SECS", 15),
				PollIntervalMS:  envInt("GNASTY_YT_POLL_INTERVAL_MS", 10000),
				Debug:           envBool("GNASTY_YT_DEBUG", false),
			},
		},
	}
	if normalized, errs := Normalize(cfg); len(errs) == 0 {
		return normalized
	}
	return cfg
}

func Normalize(cfg Config) (Config, []ValidationError) {
	cfg.SchemaVersion = SchemaVersion
	errs := make([]ValidationError, 0)
	cfg.AllowedOrigins = normalizeOrigins(cfg.AllowedOrigins)

	apiBase, err := normalizeHTTPURL(cfg.APIBaseURL, false)
	if err != nil {
		errs = append(errs, ValidationError{Field: "apiBaseUrl", Message: err.Error()})
	} else {
		cfg.APIBaseURL = apiBase
	}

	wsURL, err := normalizeWSURL(cfg.WSURL)
	if err != nil {
		errs = append(errs, ValidationError{Field: "wsUrl", Message: err.Error()})
	} else {
		cfg.WSURL = wsURL
	}

	channel, err := normalizeTwitchChannel(cfg.TwitchChannel)
	if err != nil {
		errs = append(errs, ValidationError{Field: "twitchChannel", Message: err.Error()})
	} else {
		cfg.TwitchChannel = channel
	}

	yt, err := normalizeYouTubeURL(cfg.YouTubeSourceURL)
	if err != nil {
		errs = append(errs, ValidationError{Field: "youtubeSourceUrl", Message: err.Error()})
	} else {
		cfg.YouTubeSourceURL = yt
	}

	cfg.Tailer.OffsetPath = strings.TrimSpace(cfg.Tailer.OffsetPath)
	cfg.Ingest.GnastyBin = strings.TrimSpace(cfg.Ingest.GnastyBin)
	cfg.Ingest.GnastyArgs = normalizeCSV(cfg.Ingest.GnastyArgs)
	cfg.Gnasty.Sinks.Enabled = normalizeSinks(cfg.Gnasty.Sinks.Enabled)
	cfg.Gnasty.Twitch.Nick = strings.TrimSpace(cfg.Gnasty.Twitch.Nick)

	if cfg.Tailer.PollIntervalMS < 25 || cfg.Tailer.PollIntervalMS > 60000 {
		errs = append(errs, ValidationError{Field: "tailer.pollIntervalMs", Message: "must be between 25 and 60000"})
	}
	if cfg.Tailer.MaxBatch < 1 || cfg.Tailer.MaxBatch > 5000 {
		errs = append(errs, ValidationError{Field: "tailer.maxBatch", Message: "must be between 1 and 5000"})
	}
	if cfg.Tailer.MaxLagMS < 0 || cfg.Tailer.MaxLagMS > 3600000 {
		errs = append(errs, ValidationError{Field: "tailer.maxLagMs", Message: "must be between 0 and 3600000"})
	}
	if cfg.Websocket.PingIntervalMS < 500 || cfg.Websocket.PingIntervalMS > 120000 {
		errs = append(errs, ValidationError{Field: "websocket.pingIntervalMs", Message: "must be between 500 and 120000"})
	}
	if cfg.Websocket.PongWaitMS < 500 || cfg.Websocket.PongWaitMS > 120000 {
		errs = append(errs, ValidationError{Field: "websocket.pongWaitMs", Message: "must be between 500 and 120000"})
	}
	if cfg.Websocket.WriteDeadlineMS < 0 || cfg.Websocket.WriteDeadlineMS > 60000 {
		errs = append(errs, ValidationError{Field: "websocket.writeDeadlineMs", Message: "must be between 0 and 60000"})
	}
	if cfg.Websocket.MaxMessageBytes < 1024 || cfg.Websocket.MaxMessageBytes > 16777216 {
		errs = append(errs, ValidationError{Field: "websocket.maxMessageBytes", Message: "must be between 1024 and 16777216"})
	}
	if cfg.Ingest.BackoffBaseMS < 100 || cfg.Ingest.BackoffBaseMS > 60000 {
		errs = append(errs, ValidationError{Field: "ingest.backoffBaseMs", Message: "must be between 100 and 60000"})
	}
	if cfg.Ingest.BackoffMaxMS < 1000 || cfg.Ingest.BackoffMaxMS > 600000 {
		errs = append(errs, ValidationError{Field: "ingest.backoffMaxMs", Message: "must be between 1000 and 600000"})
	}
	if cfg.Ingest.BackoffMaxMS < cfg.Ingest.BackoffBaseMS {
		errs = append(errs, ValidationError{Field: "ingest.backoffMaxMs", Message: "must be greater than or equal to ingest.backoffBaseMs"})
	}
	if len(cfg.Gnasty.Sinks.Enabled) == 0 {
		errs = append(errs, ValidationError{Field: "gnasty.sinks.enabled", Message: "must include at least one sink"})
	}
	for _, sink := range cfg.Gnasty.Sinks.Enabled {
		if sink != "sqlite" {
			errs = append(errs, ValidationError{Field: "gnasty.sinks.enabled", Message: "contains unsupported sink " + sink})
			break
		}
	}
	if cfg.Gnasty.Sinks.BatchSize < 1 {
		errs = append(errs, ValidationError{Field: "gnasty.sinks.batchSize", Message: "must be >= 1"})
	}
	if cfg.Gnasty.Sinks.FlushMaxMS < 0 {
		errs = append(errs, ValidationError{Field: "gnasty.sinks.flushMaxMs", Message: "must be >= 0"})
	}
	if cfg.Gnasty.Twitch.BackoffMinMS < 1 {
		errs = append(errs, ValidationError{Field: "gnasty.twitch.backoffMinMs", Message: "must be >= 1"})
	}
	if cfg.Gnasty.Twitch.BackoffMaxMS < 1 {
		errs = append(errs, ValidationError{Field: "gnasty.twitch.backoffMaxMs", Message: "must be >= 1"})
	}
	if cfg.Gnasty.Twitch.BackoffMinMS > cfg.Gnasty.Twitch.BackoffMaxMS {
		errs = append(errs, ValidationError{Field: "gnasty.twitch.backoffMaxMs", Message: "must be >= gnasty.twitch.backoffMinMs"})
	}
	if cfg.Gnasty.Twitch.RefreshBackoffMinMS < 1 {
		errs = append(errs, ValidationError{Field: "gnasty.twitch.refreshBackoffMinMs", Message: "must be >= 1"})
	}
	if cfg.Gnasty.Twitch.RefreshBackoffMaxMS < 1 {
		errs = append(errs, ValidationError{Field: "gnasty.twitch.refreshBackoffMaxMs", Message: "must be >= 1"})
	}
	if cfg.Gnasty.Twitch.RefreshBackoffMinMS > cfg.Gnasty.Twitch.RefreshBackoffMaxMS {
		errs = append(errs, ValidationError{Field: "gnasty.twitch.refreshBackoffMaxMs", Message: "must be >= gnasty.twitch.refreshBackoffMinMs"})
	}
	if cfg.Gnasty.YouTube.RetrySeconds < 1 {
		errs = append(errs, ValidationError{Field: "gnasty.youtube.retrySeconds", Message: "must be >= 1"})
	}
	if cfg.Gnasty.YouTube.PollTimeoutSecs < 0 {
		errs = append(errs, ValidationError{Field: "gnasty.youtube.pollTimeoutSecs", Message: "must be >= 0"})
	}
	if cfg.Gnasty.YouTube.PollIntervalMS < 0 {
		errs = append(errs, ValidationError{Field: "gnasty.youtube.pollIntervalMs", Message: "must be >= 0"})
	}
	return cfg, errs
}

func Merge(defaults, persisted Config) Config {
	merged := defaults
	if strings.TrimSpace(persisted.APIBaseURL) != "" {
		merged.APIBaseURL = persisted.APIBaseURL
	}
	if strings.TrimSpace(persisted.WSURL) != "" {
		merged.WSURL = persisted.WSURL
	}
	if strings.TrimSpace(persisted.TwitchChannel) != "" {
		merged.TwitchChannel = persisted.TwitchChannel
	}
	if strings.TrimSpace(persisted.YouTubeSourceURL) != "" {
		merged.YouTubeSourceURL = persisted.YouTubeSourceURL
	}

	merged.Features = persisted.Features
	merged.Tailer = persisted.Tailer
	merged.Websocket = persisted.Websocket
	merged.Ingest = persisted.Ingest
	if persisted.SchemaVersion >= SchemaVersion {
		merged.AllowedOrigins = persisted.AllowedOrigins
		merged.Gnasty = persisted.Gnasty
	}
	merged.SchemaVersion = SchemaVersion
	return merged
}

func RedactedSecretsFromEnv() EnvOnlySecrets {
	redact := func(name string) SecretState {
		configured := strings.TrimSpace(os.Getenv(name)) != ""
		value := ""
		if configured {
			value = "[redacted]"
		}
		return SecretState{Configured: configured, Value: value, Source: "env"}
	}
	ytConfigured := strings.TrimSpace(os.Getenv("YOUTUBE_API_KEY")) != "" || strings.TrimSpace(os.Getenv("GNASTY_YT_API_KEY")) != ""
	ytValue := ""
	if ytConfigured {
		ytValue = "[redacted]"
	}

	return EnvOnlySecrets{
		TwitchClientSecret: redact("TWITCH_OAUTH_CLIENT_SECRET"),
		YouTubeAPIKey: SecretState{
			Configured: ytConfigured,
			Value:      ytValue,
			Source:     "env",
		},
	}
}

func normalizeHTTPURL(raw string, allowEmpty bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" && allowEmpty {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("must be an absolute http(s) URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("must use http or https")
	}
	parsed.Scheme = scheme
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if parsed.Path == "" {
		parsed.Path = ""
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func normalizeWSURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("must be a non-empty ws/wss URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("must be an absolute ws/wss URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "ws" && scheme != "wss" {
		return "", fmt.Errorf("must use ws or wss")
	}
	parsed.Scheme = scheme
	if strings.TrimSpace(parsed.Path) == "" {
		parsed.Path = "/ws/chat"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if parsed.Path == "" {
		parsed.Path = "/ws/chat"
	}
	return parsed.String(), nil
}

func normalizeTwitchChannel(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("must be a non-empty twitch channel or URL")
	}

	if strings.Contains(raw, ".") && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("malformed URL")
		}
		if !isHostOrSubdomain(parsed.Hostname(), "twitch.tv") {
			return "", fmt.Errorf("must be a twitch.tv URL or channel login")
		}
		raw = parsed.Path
	}

	raw = strings.Trim(raw, "/")
	raw = strings.TrimPrefix(raw, "@")
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case '/', '?', '#':
			return true
		default:
			return false
		}
	})
	if len(parts) == 0 {
		return "", fmt.Errorf("must be a non-empty twitch channel or URL")
	}
	login := strings.ToLower(strings.TrimSpace(parts[0]))
	if login == "" {
		return "", fmt.Errorf("must be a non-empty twitch channel or URL")
	}
	for _, r := range login {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return "", fmt.Errorf("contains invalid characters")
	}
	return login, nil
}

func normalizeYouTubeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("must be a non-empty YouTube handle or URL")
	}

	if id, ok := normalizeYouTubeVideoID(raw); ok {
		return canonicalYouTubeWatchURL(id), nil
	}

	if handle, ok := normalizeYouTubeHandle(raw); ok {
		return canonicalYouTubeLiveURL(handle), nil
	}

	candidate := raw
	if strings.Contains(raw, ".") && !strings.Contains(raw, "://") {
		candidate = "https://" + raw
	}

	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("malformed URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("must use http or https")
	}

	host := strings.ToLower(parsed.Hostname())
	path := strings.TrimSpace(parsed.EscapedPath())
	if path == "" {
		path = parsed.Path
	}

	if isHostOrSubdomain(host, "youtu.be") {
		id := firstPathSegment(path)
		if videoID, ok := normalizeYouTubeVideoID(id); ok {
			return canonicalYouTubeWatchURL(videoID), nil
		}
		return "", fmt.Errorf("malformed URL")
	}

	if !isHostOrSubdomain(host, "youtube.com") {
		return "", fmt.Errorf("must be a youtube.com URL, youtu.be URL, handle, or video id")
	}

	trimmedPath := strings.Trim(strings.TrimSpace(path), "/")
	if strings.HasPrefix(trimmedPath, "@") {
		handle := strings.TrimPrefix(firstPathSegment(trimmedPath), "@")
		if normalizedHandle, ok := normalizeYouTubeHandle(handle); ok {
			return canonicalYouTubeLiveURL(normalizedHandle), nil
		}
		return "", fmt.Errorf("malformed URL")
	}

	videoID := strings.TrimSpace(parsed.Query().Get("v"))
	if id, ok := normalizeYouTubeVideoID(videoID); ok {
		return canonicalYouTubeWatchURL(id), nil
	}

	return "", fmt.Errorf("malformed URL")
}

// NormalizeTwitchChannelInput canonicalizes Twitch source inputs for admin/runtime sync.
func NormalizeTwitchChannelInput(raw string) (string, error) {
	return normalizeTwitchChannel(raw)
}

// NormalizeYouTubeSourceInput canonicalizes YouTube source inputs for admin/runtime sync.
func NormalizeYouTubeSourceInput(raw string) (string, error) {
	return normalizeYouTubeURL(raw)
}

func normalizeYouTubeHandle(raw string) (string, bool) {
	handle := strings.TrimSpace(raw)
	handle = strings.TrimPrefix(handle, "@")
	if handle == "" {
		return "", false
	}
	for _, r := range handle {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return "", false
	}
	return handle, true
}

func normalizeYouTubeVideoID(raw string) (string, bool) {
	id := strings.TrimSpace(raw)
	if len(id) != 11 {
		return "", false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return "", false
	}
	return id, true
}

func canonicalYouTubeWatchURL(videoID string) string {
	return "https://www.youtube.com/watch?v=" + videoID
}

func canonicalYouTubeLiveURL(handle string) string {
	return "https://www.youtube.com/@" + handle + "/live"
}

func firstPathSegment(path string) string {
	path = strings.Trim(path, "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	return strings.TrimSpace(parts[0])
}

func isHostOrSubdomain(host, domain string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")
	domain = strings.ToLower(strings.TrimSpace(domain))
	if host == domain {
		return true
	}
	return strings.HasSuffix(host, "."+domain)
}

func normalizeCSV(values []string) []string {
	out := make([]string, 0, len(values))
	for _, item := range values {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func splitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func splitListCSV(raw string, def []string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return append([]string(nil), def...)
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', ';', ' ', '\t', '\n':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), def...)
	}
	return out
}

func normalizeSinks(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func normalizeOrigins(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimRight(strings.TrimSpace(value), "/")
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func getEnvFirst(keys []string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func envBoolAny(keys []string, def bool) bool {
	for _, key := range keys {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		return envBool(key, def)
	}
	return def
}

func envBool(key string, def bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on", "show", "enable":
		return true
	case "0", "false", "no", "off", "hide", "disable":
		return false
	default:
		return def
	}
}

func envIntAny(keys []string, def int) int {
	for _, key := range keys {
		if v := envInt(key, -1); v >= 0 {
			return v
		}
	}
	return def
}

func envInt(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}

func envInt64(key string, def int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return def
	}
	return v
}
