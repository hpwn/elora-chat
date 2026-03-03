package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/hpwn/EloraChat/src/backend/internal/authutil"
	"github.com/hpwn/EloraChat/src/backend/internal/runtimeconfig"
	"github.com/hpwn/EloraChat/src/backend/internal/storage"
	"github.com/hpwn/EloraChat/src/backend/internal/tokenfile"
	"github.com/hpwn/EloraChat/src/backend/internal/ws"
	"github.com/jdavasligil/emodl"
)

var chatStore storage.Store
var ctx = context.Background()
var subscribersMu sync.Mutex
var subscribers map[chan []byte]struct{}

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return originAllowed(r.Header.Get("Origin"))
		},
	}

	allowedOriginsMu sync.RWMutex
	allowAllOrigins  = true
	allowedOrigins   = map[string]struct{}{}
)

type websocketConfig struct {
	pingInterval  time.Duration
	pongWait      time.Duration
	writeDeadline time.Duration
	maxBytes      int64
}

// WebsocketRuntimeConfig exposes the runtime websocket tuning knobs.
type WebsocketRuntimeConfig struct {
	PingInterval  time.Duration
	PongWait      time.Duration
	WriteDeadline time.Duration
	MaxMessage    int64
}

type uiConfig struct {
	hideYouTubeAt bool
	showBadges    bool
}

var (
	activeWebsocketConfig = websocketConfig{
		pingInterval:  25 * time.Second,
		pongWait:      30 * time.Second,
		writeDeadline: 5 * time.Second,
		maxBytes:      131072,
	}
	activeUIConfig = uiConfig{
		hideYouTubeAt: true,
		showBadges:    true,
	}
	overrideWSEnvelope  *bool
	overrideWSDropEmpty *bool
)

var (
	twitchHelixBaseURL    = "https://api.twitch.tv/helix"
	twitchOAuthTokenURL   = "https://id.twitch.tv/oauth2/token"
	youtubeDataAPIBaseURL = "https://www.googleapis.com/youtube/v3"
	twitchBadgeHTTPClient = &http.Client{
		Timeout: 3 * time.Second,
	}
	youtubeDataHTTPClient = &http.Client{
		Timeout: 3 * time.Second,
	}
	twitchBadgeCacheState = struct {
		mu       sync.RWMutex
		global   twitchBadgeCacheEntry
		channels map[string]twitchBadgeCacheEntry
	}{
		channels: map[string]twitchBadgeCacheEntry{},
	}
	twitchBadgeTokenState = struct {
		mu        sync.Mutex
		token     string
		expiresAt time.Time
	}{}
	twitchBadgeWarnState = struct {
		mu   sync.Mutex
		seen map[string]struct{}
	}{
		seen: map[string]struct{}{},
	}
	twitchBroadcasterIDCacheState = struct {
		mu      sync.RWMutex
		byLogin map[string]twitchBroadcasterIDCacheEntry
	}{
		byLogin: map[string]twitchBroadcasterIDCacheEntry{},
	}
)

const twitchBadgeCacheTTL = 30 * time.Minute

var tokenizer Tokenizer
var emoteCacheMu sync.RWMutex

var commandParser CommandParser

// TODO: replace with table in SQLite
var userColorMap map[string]string = make(map[string]string)

type Image struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	ID     string `json:"id"`
}

type Emote struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Locations []string `json:"locations"`
	Images    []Image  `json:"images"`
}

type Badge struct {
	ID       string  `json:"id"`
	Platform string  `json:"platform,omitempty"`
	Version  string  `json:"version,omitempty"`
	Images   []Image `json:"images,omitempty"`
}

type twitchHelixBadgeResponse struct {
	Data []twitchHelixBadgeSet `json:"data"`
}

type twitchHelixBadgeSet struct {
	SetID    string                    `json:"set_id"`
	Versions []twitchHelixBadgeVersion `json:"versions"`
}

type twitchHelixBadgeVersion struct {
	ID         string `json:"id"`
	ImageURL1x string `json:"image_url_1x"`
	ImageURL2x string `json:"image_url_2x"`
	ImageURL4x string `json:"image_url_4x"`
}

type twitchAppAccessTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

type twitchBadgeCacheEntry struct {
	expiresAt time.Time
	badges    map[string]map[string][]Image
}

type twitchBroadcasterIDCacheEntry struct {
	expiresAt     time.Time
	broadcasterID string
}

type emodlTargets struct {
	twitchLogin         string
	twitchBroadcasterID string
	youTubeSourceURL    string
	youTubeChannelID    string
}

func tokenizerSnapshot() Tokenizer {
	emoteCacheMu.RLock()
	defer emoteCacheMu.RUnlock()
	return tokenizer
}

func emoteCacheSnapshot() map[string]Emote {
	emoteCacheMu.RLock()
	defer emoteCacheMu.RUnlock()
	if len(tokenizer.EmoteCache) == 0 {
		return map[string]Emote{}
	}
	clone := make(map[string]Emote, len(tokenizer.EmoteCache))
	for k, v := range tokenizer.EmoteCache {
		clone[k] = v
	}
	return clone
}

func replaceEmoteCache(cache map[string]Emote) {
	emoteCacheMu.Lock()
	defer emoteCacheMu.Unlock()
	if cache == nil {
		tokenizer.EmoteCache = map[string]Emote{}
		return
	}
	tokenizer.EmoteCache = cache
}

func upsertEmoteCache(emotes []Emote) {
	if len(emotes) == 0 {
		return
	}
	next := emoteCacheSnapshot()
	for _, e := range emotes {
		if strings.TrimSpace(e.Name) == "" {
			continue
		}
		next[e.Name] = e
	}
	replaceEmoteCache(next)
}

func normalizeYouTubeChannelIDIdentity(raw string) string {
	id := strings.TrimSpace(raw)
	if len(id) != 24 || !strings.HasPrefix(id, "UC") {
		return ""
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return ""
	}
	return id
}

func resolveYouTubeChannelIDBySource(sourceURL string) (string, error) {
	sourceURL = strings.TrimSpace(sourceURL)
	if channelID := normalizeYouTubeChannelIDIdentity(sourceURL); channelID != "" {
		return channelID, nil
	}
	normalized := normalizeYouTubeSourceIdentity(sourceURL)
	if normalized == "" {
		return "", fmt.Errorf("missing youtube source identity")
	}
	if channelID := normalizeYouTubeChannelIDIdentity(normalized); channelID != "" {
		return channelID, nil
	}

	apiKey := strings.TrimSpace(os.Getenv("YOUTUBE_API_KEY"))
	if apiKey == "" {
		return "", fmt.Errorf("missing youtube data api key (YOUTUBE_API_KEY)")
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", err
	}

	base := strings.TrimRight(strings.TrimSpace(youtubeDataAPIBaseURL), "/")
	if base == "" {
		return "", fmt.Errorf("missing youtube data api base url")
	}

	doRequest := func(endpoint string, query url.Values, out any) error {
		query.Set("key", apiKey)
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
		if reqErr != nil {
			return reqErr
		}
		resp, reqErr := youtubeDataHTTPClient.Do(req)
		if reqErr != nil {
			return reqErr
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			return fmt.Errorf("youtube data api http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}

	videoID := normalizeYouTubeVideoIDIdentity(parsed.Query().Get("v"))
	if videoID != "" {
		var payload struct {
			Items []struct {
				Snippet struct {
					ChannelID string `json:"channelId"`
				} `json:"snippet"`
			} `json:"items"`
		}
		q := url.Values{}
		q.Set("part", "snippet")
		q.Set("id", videoID)
		if err := doRequest(base+"/videos", q, &payload); err != nil {
			return "", err
		}
		if len(payload.Items) == 0 {
			return "", fmt.Errorf("youtube videos returned no items for id=%s", videoID)
		}
		channelID := normalizeYouTubeChannelIDIdentity(payload.Items[0].Snippet.ChannelID)
		if channelID == "" {
			return "", fmt.Errorf("youtube videos returned empty channel id for id=%s", videoID)
		}
		return channelID, nil
	}

	path := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	if strings.HasPrefix(path, "@") {
		handle := normalizeYouTubeHandleIdentity(strings.TrimPrefix(strings.Split(path, "/")[0], "@"))
		if handle == "" {
			return "", fmt.Errorf("invalid youtube handle in source identity")
		}
		var payload struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		q := url.Values{}
		q.Set("part", "id")
		q.Set("forHandle", handle)
		if err := doRequest(base+"/channels", q, &payload); err != nil {
			return "", err
		}
		if len(payload.Items) == 0 {
			return "", fmt.Errorf("youtube channels returned no items for handle=%s", handle)
		}
		channelID := normalizeYouTubeChannelIDIdentity(payload.Items[0].ID)
		if channelID == "" {
			return "", fmt.Errorf("youtube channels returned invalid channel id for handle=%s", handle)
		}
		return channelID, nil
	}

	return "", fmt.Errorf("unsupported youtube source identity")
}

func buildEmodlOptions(cfg runtimeconfig.Config) (emodl.DownloaderOptions, emodlTargets, error) {
	targets := emodlTargets{
		twitchLogin:      normalizeTwitchChannelIdentity(cfg.TwitchChannel),
		youTubeSourceURL: normalizeYouTubeSourceIdentity(cfg.YouTubeSourceURL),
	}
	options := emodl.DownloaderOptions{}
	var errs []error

	if targets.twitchLogin != "" {
		id, err := resolveTwitchBroadcasterIDByLogin(targets.twitchLogin)
		if err != nil {
			errs = append(errs, fmt.Errorf("twitch broadcaster id resolve failed (channel=%s): %w", targets.twitchLogin, err))
		} else {
			targets.twitchBroadcasterID = id
		}
	}

	if targets.youTubeSourceURL != "" {
		id, err := resolveYouTubeChannelIDBySource(targets.youTubeSourceURL)
		if err != nil {
			errs = append(errs, fmt.Errorf("youtube channel id resolve failed (source=%s): %w", targets.youTubeSourceURL, err))
		} else {
			targets.youTubeChannelID = id
		}
	}

	// emodl only accepts one platform-scoped ID per provider in a single load.
	// Prefer Twitch for the primary scoped pass; YouTube is loaded in a second pass when present.
	if targets.twitchBroadcasterID != "" {
		options.SevenTV = &emodl.SevenTVOptions{Platform: "twitch", PlatformID: targets.twitchBroadcasterID}
		options.BTTV = &emodl.BTTVOptions{Platform: "twitch", PlatformID: targets.twitchBroadcasterID}
		options.FFZ = &emodl.FFZOptions{Platform: "twitch", PlatformID: targets.twitchBroadcasterID}
	} else if targets.youTubeChannelID != "" {
		options.SevenTV = &emodl.SevenTVOptions{Platform: "youtube", PlatformID: targets.youTubeChannelID}
		options.BTTV = &emodl.BTTVOptions{Platform: "youtube", PlatformID: targets.youTubeChannelID}
		options.FFZ = &emodl.FFZOptions{Platform: "youtube", PlatformID: targets.youTubeChannelID}
	}

	if len(errs) > 0 {
		return options, targets, errors.Join(errs...)
	}
	return options, targets, nil
}

func loadEmoteCache(options emodl.DownloaderOptions) (map[string]Emote, error) {
	downloader := emodl.NewDownloader(options)
	emoteCacheTmp, err := downloader.Load()
	if err != nil {
		return nil, err
	}

	out := make(map[string]Emote, len(emoteCacheTmp))
	for name, emote := range emoteCacheTmp {
		next := Emote{
			ID:        emote.ID,
			Name:      emote.Name,
			Locations: emote.Locations,
			Images:    []Image{},
		}
		if len(emote.Images) > 0 {
			next.Images = append(next.Images, Image(emote.Images[0]))
		}
		out[name] = next
	}
	return out, nil
}

func reloadThirdPartyEmotes(cfg runtimeconfig.Config) error {
	options, targets, buildErr := buildEmodlOptions(cfg)
	if buildErr != nil {
		log.Printf("emodl: target resolution warning: %v", buildErr)
	}

	log.Printf(
		"emodl: reload targets twitch_login=%q twitch_id=%q youtube_source=%q youtube_channel_id=%q",
		targets.twitchLogin,
		targets.twitchBroadcasterID,
		targets.youTubeSourceURL,
		targets.youTubeChannelID,
	)

	next, err := loadEmoteCache(options)
	if err != nil {
		return fmt.Errorf("failed to load third party emotes: %w", err)
	}

	if targets.twitchBroadcasterID != "" && targets.youTubeChannelID != "" {
		ytOptions := emodl.DownloaderOptions{
			SevenTV: &emodl.SevenTVOptions{Platform: "youtube", PlatformID: targets.youTubeChannelID},
			BTTV:    &emodl.BTTVOptions{Platform: "youtube", PlatformID: targets.youTubeChannelID},
			FFZ:     &emodl.FFZOptions{Platform: "youtube", PlatformID: targets.youTubeChannelID},
		}
		ytCache, ytErr := loadEmoteCache(ytOptions)
		if ytErr != nil {
			log.Printf("emodl: youtube scoped load warning (channel_id=%s): %v", targets.youTubeChannelID, ytErr)
		} else {
			for name, emote := range ytCache {
				next[name] = emote
			}
		}
	}

	replaceEmoteCache(next)
	log.Printf("emodl: cache size = %d", len(next))
	return nil
}

func (b *Badge) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*b = Badge{}
		return nil
	}

	if trimmed[0] == '"' {
		var entry string
		if err := json.Unmarshal(trimmed, &entry); err != nil {
			return err
		}
		entry = strings.TrimSpace(entry)
		if entry == "" {
			*b = Badge{}
			return nil
		}
		badge := Badge{ID: entry}
		if idx := strings.Index(entry, "/"); idx >= 0 {
			badge.ID = strings.TrimSpace(entry[:idx])
			badge.Version = strings.TrimSpace(entry[idx+1:])
		}
		*b = badge
		return nil
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		*b = Badge{}
		return nil
	}

	get := func(keys ...string) string {
		for _, key := range keys {
			if v, ok := obj[key]; ok {
				switch val := v.(type) {
				case string:
					if s := strings.TrimSpace(val); s != "" {
						return s
					}
				case json.Number:
					if s := strings.TrimSpace(val.String()); s != "" {
						return s
					}
				}
			}
		}
		return ""
	}

	parseImages := func(raw any) []Image {
		arr, ok := raw.([]any)
		if !ok {
			return nil
		}
		out := make([]Image, 0, len(arr))
		for _, entry := range arr {
			if entry == nil {
				continue
			}
			rec, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			url, _ := rec["url"].(string)
			url = strings.TrimSpace(url)
			if url == "" {
				continue
			}

			width := 0
			height := 0
			switch v := rec["width"].(type) {
			case json.Number:
				if i, err := v.Int64(); err == nil {
					width = int(i)
				}
			case float64:
				width = int(v)
			case int64:
				width = int(v)
			case int:
				width = v
			}
			switch v := rec["height"].(type) {
			case json.Number:
				if i, err := v.Int64(); err == nil {
					height = int(i)
				}
			case float64:
				height = int(v)
			case int64:
				height = int(v)
			case int:
				height = v
			}

			id := ""
			if rawID, ok := rec["id"].(string); ok {
				id = strings.TrimSpace(rawID)
			}

			out = append(out, Image{URL: url, Width: width, Height: height, ID: id})
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}

	id := get("id", "badge_id", "name", "slug", "_id")
	version := get("version", "badge_version")
	platform := get("platform")
	images := parseImages(obj["images"])
	if len(images) == 0 {
		*b = Badge{ID: id, Platform: platform, Version: version}
		return nil
	}
	*b = Badge{ID: id, Platform: platform, Version: version, Images: images}
	return nil
}

type Message struct {
	Author        string  `json:"author"` // Adjusted to directly receive the author's name as a string
	Message       string  `json:"message"`
	Tokens        []Token `json:"fragments"`
	Emotes        []Emote `json:"emotes"`
	Badges        []Badge `json:"badges"`
	BadgesRaw     any     `json:"badges_raw,omitempty"`
	Source        string  `json:"source"`
	SourceChannel string  `json:"source_channel,omitempty"`
	SourceURL     string  `json:"source_url,omitempty"`
	Colour        string  `json:"colour"`
	UsernameColor string  `json:"username_color,omitempty"`
}

var errDropMessage = errors.New("chat: drop empty message")

func normalizeSource(src string) string {
	src = strings.TrimSpace(src)
	switch strings.ToLower(src) {
	case "twitch":
		return "Twitch"
	case "youtube":
		return "YouTube"
	default:
		return src
	}
}

func rawJSONLooksLikeChatMessage(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return false
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return false
	}

	isJSONString := func(v json.RawMessage) bool {
		trimmed := bytes.TrimSpace(v)
		return len(trimmed) > 0 && trimmed[0] == '"'
	}
	isJSONArray := func(v json.RawMessage) bool {
		trimmed := bytes.TrimSpace(v)
		return len(trimmed) > 0 && trimmed[0] == '['
	}

	messageRaw := obj["message"]
	if isJSONString(messageRaw) {
		return true
	}

	if isJSONArray(obj["fragments"]) {
		return true
	}

	if isJSONString(obj["author"]) && isJSONString(obj["source"]) && isJSONString(messageRaw) {
		return true
	}

	return false
}

func wsDropEmptyEnabled() bool {
	if overrideWSDropEmpty != nil {
		return *overrideWSDropEmpty
	}

	raw := strings.TrimSpace(os.Getenv("ELORA_WS_DROP_EMPTY"))
	if raw == "" {
		return true
	}

	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func (m *Message) normalize() {
	if m == nil {
		return
	}

	m.Author = strings.TrimSpace(m.Author)
	m.Message = strings.TrimSpace(m.Message)
	m.Source = normalizeSource(m.Source)

	if strings.EqualFold(m.Source, "youtube") && len(m.Emotes) > 0 && len(m.Tokens) == 0 {
		if fragments := buildYouTubeFragments(m.Message, m.Emotes); len(fragments) > 0 {
			m.Tokens = fragments
		}
	}

	if m.Tokens == nil {
		m.Tokens = []Token{}
	}
	if m.Emotes == nil {
		m.Emotes = []Emote{}
	}
	if m.Badges == nil {
		m.Badges = []Badge{}
	}
	if len(m.Badges) > 0 {
		filtered := m.Badges[:0]
		for _, badge := range m.Badges {
			if strings.EqualFold(strings.TrimSpace(badge.Platform), "youtube") &&
				isYouTubeOwnerBadgeID(badge.ID) &&
				!badgeHasUsableImage(badge.Images) {
				continue
			}
			filtered = append(filtered, badge)
		}
		if len(filtered) == 0 {
			m.Badges = []Badge{}
		} else {
			m.Badges = filtered
		}
	}
	if !activeUIConfig.showBadges {
		m.Badges = []Badge{}
		m.BadgesRaw = nil
	}

	if activeUIConfig.hideYouTubeAt && strings.EqualFold(m.Source, "YouTube") {
		if strings.HasPrefix(m.Author, "@") {
			m.Author = strings.TrimPrefix(m.Author, "@")
			m.Author = strings.TrimSpace(m.Author)
		}
	}
}

func isYouTubeOwnerBadgeID(id string) bool {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "owner", "broadcaster", "channel_owner":
		return true
	default:
		return false
	}
}

func badgeHasUsableImage(images []Image) bool {
	for _, img := range images {
		if strings.TrimSpace(img.URL) != "" {
			return true
		}
	}
	return false
}

type emoteSpan struct {
	start int
	end   int
	emote Emote
}

func decodeEmotesJSON(raw string) []Emote {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []Emote{}
	}

	var emotes []Emote
	if err := json.Unmarshal([]byte(raw), &emotes); err == nil {
		if emotes == nil {
			return []Emote{}
		}
		return emotes
	}

	var entries []map[string]any
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return []Emote{}
	}

	out := make([]Emote, 0, len(entries))
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		emote := Emote{
			ID:        strings.TrimSpace(getString(entry, "id")),
			Name:      strings.TrimSpace(firstNonEmptyString(entry, "name", "shortcode", "text")),
			Locations: parseEmoteLocations(entry["locations"]),
			Images:    parseEmoteImages(entry),
		}
		out = append(out, emote)
	}
	if len(out) == 0 {
		return []Emote{}
	}
	return out
}

func getString(record map[string]any, key string) string {
	if record == nil {
		return ""
	}
	if raw, ok := record[key]; ok {
		if s, ok := raw.(string); ok {
			return s
		}
	}
	return ""
}

func firstNonEmptyString(record map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(getString(record, key)); value != "" {
			return value
		}
	}
	return ""
}

func parseEmoteLocations(raw any) []string {
	arr, ok := raw.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(arr))
	for _, entry := range arr {
		switch v := entry.(type) {
		case string:
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				out = append(out, trimmed)
			}
		case map[string]any:
			start, okStart := coerceInt(v["start"])
			end, okEnd := coerceInt(v["end"])
			endExclusive := false
			if !okStart {
				start, okStart = coerceInt(v["startIndex"])
				endExclusive = endExclusive || okStart
			}
			if !okStart {
				start, okStart = coerceInt(v["start_index"])
				endExclusive = endExclusive || okStart
			}
			if !okEnd {
				end, okEnd = coerceInt(v["endIndex"])
				endExclusive = endExclusive || okEnd
			}
			if !okEnd {
				end, okEnd = coerceInt(v["end_index"])
				endExclusive = endExclusive || okEnd
			}
			if !okStart || !okEnd {
				continue
			}
			if endExclusive && end > start {
				end--
			}
			if end < start {
				continue
			}
			out = append(out, fmt.Sprintf("%d-%d", start, end))
		}
	}
	if len(out) == 0 {
		return []string{}
	}
	return out
}

func parseEmoteImages(entry map[string]any) []Image {
	rawImages, ok := entry["images"].([]any)
	if !ok {
		rawImages = nil
	}
	out := make([]Image, 0, len(rawImages))
	for _, img := range rawImages {
		rec, ok := img.(map[string]any)
		if !ok {
			continue
		}
		url := strings.TrimSpace(firstNonEmptyString(rec, "url", "imageUrl", "image_url", "src"))
		if url == "" {
			continue
		}
		width, _ := coerceInt(rec["width"])
		height, _ := coerceInt(rec["height"])
		id := strings.TrimSpace(getString(rec, "id"))
		out = append(out, Image{URL: url, Width: width, Height: height, ID: id})
	}

	if len(out) == 0 {
		url := strings.TrimSpace(firstNonEmptyString(entry, "url", "imageUrl", "image_url", "src"))
		if url != "" {
			width, _ := coerceInt(entry["width"])
			height, _ := coerceInt(entry["height"])
			out = append(out, Image{URL: url, Width: width, Height: height})
		}
	}

	if len(out) == 0 {
		return []Image{}
	}
	return out
}

func coerceInt(raw any) (int, bool) {
	switch v := raw.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i), true
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return i, true
		}
	}
	return 0, false
}

func buildYouTubeFragments(message string, emotes []Emote) []Token {
	if len(emotes) == 0 {
		return nil
	}

	runes := []rune(message)
	if len(runes) == 0 {
		return nil
	}

	spans := make([]emoteSpan, 0, len(emotes))
	for _, emote := range emotes {
		for _, loc := range emote.Locations {
			loc = strings.TrimSpace(loc)
			if loc == "" {
				continue
			}
			bounds := strings.SplitN(loc, "-", 2)
			if len(bounds) != 2 {
				continue
			}
			start, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
			end, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err1 != nil || err2 != nil {
				continue
			}
			if start < 0 {
				start = 0
			}
			if end < start {
				continue
			}
			if start >= len(runes) {
				continue
			}
			if end >= len(runes) {
				end = len(runes) - 1
			}
			spans = append(spans, emoteSpan{start: start, end: end, emote: emote})
		}
	}
	if len(spans) == 0 {
		return nil
	}

	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end < spans[j].end
	})

	tokens := make([]Token, 0, len(spans)*2+1)
	cursor := 0
	for _, span := range spans {
		if span.start > cursor {
			text := string(runes[cursor:span.start])
			if text != "" {
				tokens = append(tokens, Token{
					Type: TokenTypeText,
					Text: text,
					Emote: Emote{
						Locations: []string{},
						Images:    []Image{},
					},
				})
			}
		} else if span.start < cursor {
			if span.end < cursor {
				continue
			}
			span.start = cursor
		}

		if span.end < span.start {
			continue
		}
		text := string(runes[span.start : span.end+1])
		tokens = append(tokens, Token{
			Type:  TokenTypeEmote,
			Text:  text,
			Emote: span.emote,
		})
		cursor = span.end + 1
	}

	if cursor < len(runes) {
		text := string(runes[cursor:])
		if text != "" {
			tokens = append(tokens, Token{
				Type: TokenTypeText,
				Text: text,
				Emote: Emote{
					Locations: []string{},
					Images:    []Image{},
				},
			})
		}
	}

	return tokens
}

func (m Message) toChatPayload() ws.ChatPayload {
	m.normalize()

	fragments := make([]any, len(m.Tokens))
	for i, token := range m.Tokens {
		fragments[i] = token
	}

	emotes := make([]any, len(m.Emotes))
	for i, emote := range m.Emotes {
		emotes[i] = emote
	}

	badges := make([]any, len(m.Badges))
	for i, badge := range m.Badges {
		badges[i] = badge
	}

	return ws.ChatPayload{
		Author:        m.Author,
		Message:       m.Message,
		Fragments:     fragments,
		Emotes:        emotes,
		Badges:        badges,
		BadgesRaw:     m.BadgesRaw,
		Source:        m.Source,
		SourceChannel: m.SourceChannel,
		SourceURL:     m.SourceURL,
		Colour:        m.Colour,
		UsernameColor: m.UsernameColor,
	}
}

var fallbackColourPalette = []string{
	"#0000FF", // blue
	"#8A2BE2", // blue_violet
	"#5F9EA0", // cadet_blue
	"#D2691E", // chocolate
	"#FF7F50", // coral
	"#1E90FF", // dodger_blue
	"#B22222", // firebrick
	"#DAA520", // golden_rod
	"#008000", // green
	"#FF69B4", // hot_pink
	"#FF4500", // orange_red
	"#FF0000", // red
	"#2E8B57", // sea_green
	"#00FF7F", // spring_green
	"#9ACD32", // yellow_green
}

var hexUsernameColourRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

const (
	usernameColourDarkBGMinLuminance = 0.10
	usernameColourBlendTowardWhite   = 0.60
)

func colorFromName(name string) string {
	if name == "" {
		return "#94a3b8"
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(name)))
	sum := h.Sum32()
	idx := int(sum % uint32(len(fallbackColourPalette)))
	return fallbackColourPalette[idx]
}

const (
	youtubeMemberColour    = "#0F9D58"
	youtubeModeratorColour = "#5E84F1"
	youtubeOwnerColour     = "#FFD600"
)

func normalizeHexUsernameColour(raw string) string {
	raw = strings.TrimSpace(raw)
	if !hexUsernameColourRe.MatchString(raw) {
		return ""
	}
	return strings.ToUpper(raw)
}

func parseHexRGB(hex string) (uint8, uint8, uint8, bool) {
	normalized := normalizeHexUsernameColour(hex)
	if normalized == "" {
		return 0, 0, 0, false
	}
	r, err := strconv.ParseUint(normalized[1:3], 16, 8)
	if err != nil {
		return 0, 0, 0, false
	}
	g, err := strconv.ParseUint(normalized[3:5], 16, 8)
	if err != nil {
		return 0, 0, 0, false
	}
	b, err := strconv.ParseUint(normalized[5:7], 16, 8)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint8(r), uint8(g), uint8(b), true
}

func srgbToLinear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func usernameColourRelativeLuminance(r, g, b uint8) float64 {
	rl := srgbToLinear(float64(r) / 255.0)
	gl := srgbToLinear(float64(g) / 255.0)
	bl := srgbToLinear(float64(b) / 255.0)
	return 0.2126*rl + 0.7152*gl + 0.0722*bl
}

func blendChannelTowardWhite(v uint8, amount float64) uint8 {
	blended := float64(v) + (255.0-float64(v))*amount
	if blended < 0 {
		blended = 0
	}
	if blended > 255 {
		blended = 255
	}
	return uint8(math.Round(blended))
}

func sanitizeUsernameColorForDarkBG(hex string) string {
	normalized := normalizeHexUsernameColour(hex)
	if normalized == "" {
		return ""
	}
	r, g, b, ok := parseHexRGB(normalized)
	if !ok {
		return ""
	}
	if usernameColourRelativeLuminance(r, g, b) >= usernameColourDarkBGMinLuminance {
		return normalized
	}

	// Keep hue direction while lifting readability on a dark background.
	for i := 0; i < 4 && usernameColourRelativeLuminance(r, g, b) < usernameColourDarkBGMinLuminance; i++ {
		r = blendChannelTowardWhite(r, usernameColourBlendTowardWhite)
		g = blendChannelTowardWhite(g, usernameColourBlendTowardWhite)
		b = blendChannelTowardWhite(b, usernameColourBlendTowardWhite)
	}
	return fmt.Sprintf("#%02X%02X%02X", r, g, b)
}

func parseRawJSONObject(raw string) map[string]json.RawMessage {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw[0] != '{' {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil
	}
	return obj
}

func parseRawJSONObjectValue(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, false
	}
	return obj, true
}

func parseRawJSONStringValue(raw json.RawMessage) (string, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '"' {
		return "", false
	}
	var out string
	if err := json.Unmarshal(trimmed, &out); err != nil {
		return "", false
	}
	return strings.TrimSpace(out), true
}

func parseRawJSONBoolValue(raw json.RawMessage) (bool, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false, false
	}
	switch trimmed[0] {
	case 't', 'f':
		var out bool
		if err := json.Unmarshal(trimmed, &out); err != nil {
			return false, false
		}
		return out, true
	case '"':
		var out string
		if err := json.Unmarshal(trimmed, &out); err != nil {
			return false, false
		}
		switch strings.ToLower(strings.TrimSpace(out)) {
		case "1", "true", "yes", "on":
			return true, true
		case "0", "false", "no", "off":
			return false, true
		}
	}
	return false, false
}

func getRawString(obj map[string]json.RawMessage, key string) string {
	if obj == nil {
		return ""
	}
	raw, ok := obj[key]
	if !ok {
		return ""
	}
	s, ok := parseRawJSONStringValue(raw)
	if !ok {
		return ""
	}
	return s
}

func getRawBool(obj map[string]json.RawMessage, key string) (bool, bool) {
	if obj == nil {
		return false, false
	}
	raw, ok := obj[key]
	if !ok {
		return false, false
	}
	return parseRawJSONBoolValue(raw)
}

func extractTwitchRawUsernameColour(rawJSON string) string {
	obj := parseRawJSONObject(rawJSON)
	if obj == nil {
		return ""
	}
	if colour := normalizeHexUsernameColour(getRawString(obj, "color")); colour != "" {
		return colour
	}
	if tagsRaw, ok := obj["tags"]; ok {
		if tagsObj, ok := parseRawJSONObjectValue(tagsRaw); ok {
			if colour := normalizeHexUsernameColour(getRawString(tagsObj, "color")); colour != "" {
				return colour
			}
		}
	}
	if authorRaw, ok := obj["author"]; ok {
		if authorObj, ok := parseRawJSONObjectValue(authorRaw); ok {
			if colour := normalizeHexUsernameColour(getRawString(authorObj, "color")); colour != "" {
				return colour
			}
		}
	}
	return ""
}

func extractAuthorIdentity(rawJSON string) string {
	obj := parseRawJSONObject(rawJSON)
	if obj == nil {
		return ""
	}
	candidates := []string{
		"authorId", "author_id", "authorExternalChannelId", "userId", "user_id", "channelId", "channel_id", "sender_id", "id",
	}
	for _, key := range candidates {
		if value := getRawString(obj, key); value != "" {
			return strings.ToLower(value)
		}
	}
	for _, nested := range []string{"author", "tags"} {
		nestedRaw, ok := obj[nested]
		if !ok {
			continue
		}
		nestedObj, ok := parseRawJSONObjectValue(nestedRaw)
		if !ok {
			continue
		}
		for _, key := range append(candidates, "user-id") {
			if value := getRawString(nestedObj, key); value != "" {
				return strings.ToLower(value)
			}
		}
	}
	return ""
}

type youtubeRole int

const (
	youtubeRoleNone youtubeRole = iota
	youtubeRoleMember
	youtubeRoleModerator
	youtubeRoleOwner
)

func mergeYouTubeRole(role youtubeRole, next youtubeRole) youtubeRole {
	if next > role {
		return next
	}
	return role
}

func detectYouTubeRole(msg Message, rawJSON string) youtubeRole {
	role := youtubeRoleNone

	for _, badge := range msg.Badges {
		id := strings.ToLower(strings.TrimSpace(badge.ID))
		switch id {
		case "owner", "broadcaster", "channel_owner":
			role = mergeYouTubeRole(role, youtubeRoleOwner)
		case "moderator":
			role = mergeYouTubeRole(role, youtubeRoleModerator)
		case "member", "sponsor":
			role = mergeYouTubeRole(role, youtubeRoleMember)
		}
	}

	obj := parseRawJSONObject(rawJSON)
	if obj == nil {
		return role
	}

	type roleProbe struct {
		keys []string
		role youtubeRole
	}
	probes := []roleProbe{
		{keys: []string{"isChatOwner", "is_chat_owner", "isOwner", "is_owner", "isBroadcaster", "is_broadcaster"}, role: youtubeRoleOwner},
		{keys: []string{"isChatModerator", "is_chat_moderator", "isModerator", "is_moderator"}, role: youtubeRoleModerator},
		{keys: []string{"isChatSponsor", "is_chat_sponsor", "isMember", "is_member"}, role: youtubeRoleMember},
	}
	applyProbes := func(in map[string]json.RawMessage) {
		for _, probe := range probes {
			for _, key := range probe.keys {
				if value, ok := getRawBool(in, key); ok && value {
					role = mergeYouTubeRole(role, probe.role)
				}
			}
		}
	}
	applyProbes(obj)
	if authorRaw, ok := obj["author"]; ok {
		if authorObj, ok := parseRawJSONObjectValue(authorRaw); ok {
			applyProbes(authorObj)
		}
	}

	return role
}

func computeUsernameColor(msg Message, row storage.Message) string {
	author := strings.TrimSpace(msg.Author)

	source := normalizeSource(msg.Source)
	if source == "" {
		source = normalizeSource(row.Platform)
	}
	if strings.EqualFold(source, "youtube") {
		switch detectYouTubeRole(msg, row.RawJSON) {
		case youtubeRoleOwner:
			return sanitizeUsernameColorForDarkBG(youtubeOwnerColour)
		case youtubeRoleModerator:
			return sanitizeUsernameColorForDarkBG(youtubeModeratorColour)
		case youtubeRoleMember:
			return sanitizeUsernameColorForDarkBG(youtubeMemberColour)
		}
	}
	if author != "" {
		if colour, ok := userColorMap[author]; ok {
			if normalized := normalizeHexUsernameColour(colour); normalized != "" {
				return sanitizeUsernameColorForDarkBG(normalized)
			}
		}
	}

	switch strings.ToLower(source) {
	case "twitch":
		if colour := normalizeHexUsernameColour(msg.UsernameColor); colour != "" {
			return sanitizeUsernameColorForDarkBG(colour)
		}
		if colour := normalizeHexUsernameColour(msg.Colour); colour != "" {
			return sanitizeUsernameColorForDarkBG(colour)
		}
		if colour := extractTwitchRawUsernameColour(row.RawJSON); colour != "" {
			return sanitizeUsernameColorForDarkBG(colour)
		}
	default:
		if colour := normalizeHexUsernameColour(msg.UsernameColor); colour != "" {
			return sanitizeUsernameColorForDarkBG(colour)
		}
		if colour := normalizeHexUsernameColour(msg.Colour); colour != "" {
			return sanitizeUsernameColorForDarkBG(colour)
		}
	}

	if identity := extractAuthorIdentity(row.RawJSON); identity != "" {
		return sanitizeUsernameColorForDarkBG(colorFromName(identity))
	}
	if author != "" {
		return sanitizeUsernameColorForDarkBG(colorFromName(strings.ToLower(author)))
	}
	if username := strings.TrimSpace(row.Username); username != "" {
		return sanitizeUsernameColorForDarkBG(colorFromName(strings.ToLower(username)))
	}
	return sanitizeUsernameColorForDarkBG(colorFromName(""))
}

func normalizeTwitchChannelIdentity(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, ".") && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		if !strings.HasSuffix(strings.ToLower(parsed.Hostname()), "twitch.tv") {
			return ""
		}
		raw = parsed.Path
	}

	raw = strings.Trim(raw, "/")
	raw = strings.TrimPrefix(raw, "@")
	if raw == "" {
		return ""
	}
	login := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case '/', '?', '#':
			return true
		default:
			return false
		}
	})
	if len(login) == 0 {
		return ""
	}
	out := strings.ToLower(strings.TrimSpace(login[0]))
	if out == "" {
		return ""
	}
	for _, r := range out {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return ""
	}
	return out
}

func normalizeYouTubeVideoIDIdentity(raw string) string {
	id := strings.TrimSpace(raw)
	if len(id) != 11 {
		return ""
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return ""
	}
	return id
}

func normalizeYouTubeHandleIdentity(raw string) string {
	handle := strings.TrimSpace(raw)
	handle = strings.TrimPrefix(handle, "@")
	if handle == "" {
		return ""
	}
	for _, r := range handle {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return ""
	}
	return handle
}

func canonicalYouTubeWatchIdentity(videoID string) string {
	return "https://www.youtube.com/watch?v=" + videoID
}

func canonicalYouTubeLiveIdentity(handle string) string {
	return "https://www.youtube.com/@" + handle + "/live"
}

func normalizeYouTubeSourceIdentity(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if id := normalizeYouTubeVideoIDIdentity(raw); id != "" {
		return canonicalYouTubeWatchIdentity(id)
	}
	if handle := normalizeYouTubeHandleIdentity(raw); handle != "" {
		return canonicalYouTubeLiveIdentity(handle)
	}
	candidate := raw
	if strings.Contains(raw, ".") && !strings.Contains(raw, "://") {
		candidate = "https://" + raw
	}
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if strings.HasSuffix(host, "youtu.be") {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) == 0 {
			return ""
		}
		if id := normalizeYouTubeVideoIDIdentity(parts[0]); id != "" {
			return canonicalYouTubeWatchIdentity(id)
		}
		return ""
	}
	if !strings.HasSuffix(host, "youtube.com") {
		return ""
	}

	path := strings.Trim(parsed.Path, "/")
	if strings.HasPrefix(path, "@") {
		parts := strings.Split(path, "/")
		if len(parts) > 0 {
			if handle := normalizeYouTubeHandleIdentity(strings.TrimPrefix(parts[0], "@")); handle != "" {
				return canonicalYouTubeLiveIdentity(handle)
			}
		}
	}
	if id := normalizeYouTubeVideoIDIdentity(parsed.Query().Get("v")); id != "" {
		return canonicalYouTubeWatchIdentity(id)
	}
	return ""
}

func normalizeRawKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(key))
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func extractRawStringByKeys(rawJSON string, keys []string) string {
	rawJSON = strings.TrimSpace(rawJSON)
	if rawJSON == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(rawJSON), &decoded); err != nil {
		return ""
	}

	lookup := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if normalized := normalizeRawKey(key); normalized != "" {
			lookup[normalized] = struct{}{}
		}
	}

	var walk func(node any, depth int) string
	walk = func(node any, depth int) string {
		if depth > 6 || node == nil {
			return ""
		}
		switch current := node.(type) {
		case map[string]any:
			directKeys := make([]string, 0, len(current))
			for key := range current {
				directKeys = append(directKeys, key)
			}
			sort.Strings(directKeys)
			for _, key := range directKeys {
				if _, ok := lookup[normalizeRawKey(key)]; !ok {
					continue
				}
				if value, ok := current[key].(string); ok {
					value = strings.TrimSpace(value)
					if value != "" {
						return value
					}
				}
			}
			for _, key := range directKeys {
				if found := walk(current[key], depth+1); found != "" {
					return found
				}
			}
		case []any:
			for _, entry := range current {
				if found := walk(entry, depth+1); found != "" {
					return found
				}
			}
		}
		return ""
	}

	return walk(decoded, 0)
}

func resolveMessageSourceIdentity(msg *Message, row storage.Message) {
	if msg == nil {
		return
	}

	source := strings.ToLower(strings.TrimSpace(msg.Source))
	if source == "" {
		source = strings.ToLower(strings.TrimSpace(row.Platform))
	}

	switch source {
	case "twitch":
		channel := normalizeTwitchChannelIdentity(msg.SourceChannel)
		if channel == "" {
			channel = normalizeTwitchChannelIdentity(extractRawStringByKeys(row.RawJSON, []string{
				"channel", "channel_name", "channel_login", "broadcaster", "broadcaster_login", "room", "room_name",
			}))
		}
		if channel == "" {
			channel = normalizeTwitchChannelIdentity(msg.SourceURL)
		}
		msg.SourceChannel = channel
		msg.SourceURL = ""
	case "youtube":
		sourceURL := normalizeYouTubeSourceIdentity(msg.SourceURL)
		if sourceURL == "" {
			sourceURL = normalizeYouTubeSourceIdentity(extractRawStringByKeys(row.RawJSON, []string{
				"url", "source_url", "sourceurl", "watch_url", "watchurl", "video_url", "videourl",
			}))
		}
		if sourceURL == "" {
			if videoID := normalizeYouTubeVideoIDIdentity(extractRawStringByKeys(row.RawJSON, []string{
				"video_id", "videoid", "live_id", "stream_id",
			})); videoID != "" {
				sourceURL = canonicalYouTubeWatchIdentity(videoID)
			}
		}
		if sourceURL == "" {
			if handle := normalizeYouTubeHandleIdentity(extractRawStringByKeys(row.RawJSON, []string{
				"channel_handle", "channelhandle", "handle",
			})); handle != "" {
				sourceURL = canonicalYouTubeLiveIdentity(handle)
			}
		}
		msg.SourceURL = sourceURL
		msg.SourceChannel = ""
	default:
		msg.SourceChannel = normalizeTwitchChannelIdentity(msg.SourceChannel)
		msg.SourceURL = normalizeYouTubeSourceIdentity(msg.SourceURL)
	}

	applyRuntimeSourceIdentity(msg)
}

func applyRuntimeSourceIdentity(msg *Message) {
	if msg == nil {
		return
	}

	cfg := currentRuntimeConfig()
	source := strings.ToLower(strings.TrimSpace(msg.Source))

	switch source {
	case "twitch":
		msg.SourceChannel = normalizeTwitchChannelIdentity(msg.SourceChannel)
		if msg.SourceChannel == "" {
			msg.SourceChannel = normalizeTwitchChannelIdentity(cfg.TwitchChannel)
		}
		msg.SourceURL = ""
	case "youtube":
		msg.SourceURL = normalizeYouTubeSourceIdentity(msg.SourceURL)
		if msg.SourceURL == "" {
			msg.SourceURL = normalizeYouTubeSourceIdentity(cfg.YouTubeSourceURL)
		}
		msg.SourceChannel = ""
	default:
		msg.SourceChannel = normalizeTwitchChannelIdentity(msg.SourceChannel)
		msg.SourceURL = normalizeYouTubeSourceIdentity(msg.SourceURL)
	}
}

func parseChatMessageFromRawJSON(raw string) (Message, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !rawJSONLooksLikeChatMessage(raw) {
		return Message{}, false
	}
	var msg Message
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		return Message{}, false
	}
	return msg, true
}

func parseStoredBadges(raw string) ([]Badge, any) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	decodeBadges := func(in []Badge) []Badge {
		out := make([]Badge, 0, len(in))
		for _, badge := range in {
			badge.ID = strings.TrimSpace(badge.ID)
			badge.Platform = strings.TrimSpace(badge.Platform)
			badge.Version = strings.TrimSpace(badge.Version)
			if len(badge.Images) > 0 {
				filtered := make([]Image, 0, len(badge.Images))
				for _, img := range badge.Images {
					img.URL = strings.TrimSpace(img.URL)
					if img.URL == "" {
						continue
					}
					filtered = append(filtered, img)
				}
				if len(filtered) == 0 {
					badge.Images = nil
				} else {
					badge.Images = filtered
				}
			}
			if badge.ID == "" {
				continue
			}
			out = append(out, badge)
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}

	if strings.HasPrefix(raw, "{") {
		var container struct {
			Badges []Badge         `json:"badges"`
			Raw    json.RawMessage `json:"raw"`
		}
		if err := json.Unmarshal([]byte(raw), &container); err == nil {
			badges := decodeBadges(container.Badges)
			var rawAny any
			if len(container.Raw) > 0 {
				if err := json.Unmarshal(container.Raw, &rawAny); err != nil {
					rawAny = json.RawMessage(container.Raw)
				}
			}
			if badges != nil {
				applyStoredBadgeOverrides(badges, rawAny)
				return badges, rawAny
			}
			if rawAny != nil {
				return nil, rawAny
			}
		}
	}

	var entries []string
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, nil
	}
	out := make([]Badge, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		badge := Badge{Platform: "twitch"}
		if idx := strings.Index(entry, "/"); idx >= 0 {
			badge.ID = strings.TrimSpace(entry[:idx])
			badge.Version = strings.TrimSpace(entry[idx+1:])
		} else {
			badge.ID = entry
		}
		if badge.ID == "" {
			continue
		}
		badge.Version = strings.TrimSpace(badge.Version)
		out = append(out, badge)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func applyStoredBadgeOverrides(badges []Badge, raw any) {
	if len(badges) == 0 || raw == nil {
		return
	}
	rawMap, ok := raw.(map[string]any)
	if !ok {
		return
	}
	twitchRaw, ok := rawMap["twitch"].(map[string]any)
	if !ok {
		return
	}
	badgesRaw, ok := twitchRaw["badges"].(string)
	if !ok {
		return
	}
	versions := parseTwitchBadgeVersions(badgesRaw)
	subscriberVersion := strings.TrimSpace(versions["subscriber"])
	if subscriberVersion == "" {
		return
	}
	for i := range badges {
		if strings.EqualFold(badges[i].Platform, "twitch") && strings.EqualFold(badges[i].ID, "subscriber") {
			badges[i].Version = subscriberVersion
		}
	}
}

func parseTwitchBadgeVersions(raw string) map[string]string {
	out := make(map[string]string)
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		id := entry
		version := ""
		if idx := strings.Index(entry, "/"); idx >= 0 {
			id = strings.TrimSpace(entry[:idx])
			version = strings.TrimSpace(entry[idx+1:])
		}
		if id == "" {
			continue
		}
		out[id] = version
	}
	return out
}

func extractTwitchRoomID(raw any) string {
	rawMap, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	twitchRaw, ok := rawMap["twitch"].(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"room-id", "room_id", "roomid"} {
		value, _ := twitchRaw[key].(string)
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func cloneBadgeImageMap(in map[string]map[string][]Image) map[string]map[string][]Image {
	if len(in) == 0 {
		return map[string]map[string][]Image{}
	}
	out := make(map[string]map[string][]Image, len(in))
	for setID, versions := range in {
		if len(versions) == 0 {
			continue
		}
		versionOut := make(map[string][]Image, len(versions))
		for version, images := range versions {
			if len(images) == 0 {
				continue
			}
			copied := make([]Image, len(images))
			copy(copied, images)
			versionOut[version] = copied
		}
		if len(versionOut) > 0 {
			out[setID] = versionOut
		}
	}
	return out
}

func logTwitchBadgeWarnOnce(key string, format string, args ...any) {
	key = strings.TrimSpace(strings.ToLower(key))
	if key == "" {
		key = "unknown"
	}
	twitchBadgeWarnState.mu.Lock()
	_, exists := twitchBadgeWarnState.seen[key]
	if !exists {
		twitchBadgeWarnState.seen[key] = struct{}{}
	}
	twitchBadgeWarnState.mu.Unlock()
	if !exists {
		log.Printf(format, args...)
	}
}

func twitchHelixCredentials() (clientID string, clientSecret string) {
	return strings.TrimSpace(os.Getenv("TWITCH_OAUTH_CLIENT_ID")), strings.TrimSpace(os.Getenv("TWITCH_OAUTH_CLIENT_SECRET"))
}

func getTwitchHelixAppToken() (string, error) {
	now := time.Now()
	twitchBadgeTokenState.mu.Lock()
	if strings.TrimSpace(twitchBadgeTokenState.token) != "" && now.Add(60*time.Second).Before(twitchBadgeTokenState.expiresAt) {
		token := twitchBadgeTokenState.token
		twitchBadgeTokenState.mu.Unlock()
		return token, nil
	}
	twitchBadgeTokenState.mu.Unlock()

	clientID, clientSecret := twitchHelixCredentials()
	if clientID == "" || clientSecret == "" {
		return "", fmt.Errorf("missing twitch helix credentials (TWITCH_OAUTH_CLIENT_ID/TWITCH_OAUTH_CLIENT_SECRET)")
	}

	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, twitchOAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := twitchBadgeHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("twitch helix token http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var doc twitchAppAccessTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", err
	}

	token := strings.TrimSpace(doc.AccessToken)
	if token == "" {
		return "", fmt.Errorf("twitch helix token response missing access_token")
	}
	expiresIn := doc.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}

	twitchBadgeTokenState.mu.Lock()
	twitchBadgeTokenState.token = token
	twitchBadgeTokenState.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	twitchBadgeTokenState.mu.Unlock()
	return token, nil
}

func clearTwitchHelixAppToken() {
	twitchBadgeTokenState.mu.Lock()
	twitchBadgeTokenState.token = ""
	twitchBadgeTokenState.expiresAt = time.Time{}
	twitchBadgeTokenState.mu.Unlock()
}

func fetchTwitchHelixBadgeDisplay(path string, query url.Values) (map[string]map[string][]Image, error) {
	base := strings.TrimRight(strings.TrimSpace(twitchHelixBaseURL), "/")
	if base == "" {
		return map[string]map[string][]Image{}, nil
	}
	clientID, _ := twitchHelixCredentials()
	if clientID == "" {
		return nil, fmt.Errorf("missing twitch helix client id (TWITCH_OAUTH_CLIENT_ID)")
	}

	token, err := getTwitchHelixAppToken()
	if err != nil {
		return nil, err
	}

	endpoint := base + path
	if encoded := strings.TrimSpace(query.Encode()); encoded != "" {
		endpoint += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Client-Id", clientID)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := twitchBadgeHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		clearTwitchHelixAppToken()

		token, err = getTwitchHelixAppToken()
		if err != nil {
			return nil, err
		}

		req, err = http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Client-Id", clientID)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err = twitchBadgeHTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("twitch helix badges http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var doc twitchHelixBadgeResponse
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}

	out := make(map[string]map[string][]Image, len(doc.Data))
	for _, set := range doc.Data {
		setID := strings.TrimSpace(set.SetID)
		if setID == "" || len(set.Versions) == 0 {
			continue
		}
		versions := make(map[string][]Image, len(set.Versions))
		for _, version := range set.Versions {
			versionID := strings.TrimSpace(version.ID)
			if versionID == "" {
				continue
			}
			images := make([]Image, 0, 3)
			if url := strings.TrimSpace(version.ImageURL1x); url != "" {
				images = append(images, Image{URL: url, Width: 18, Height: 18, ID: versionID + "-1x"})
			}
			if url := strings.TrimSpace(version.ImageURL2x); url != "" {
				images = append(images, Image{URL: url, Width: 36, Height: 36, ID: versionID + "-2x"})
			}
			if url := strings.TrimSpace(version.ImageURL4x); url != "" {
				images = append(images, Image{URL: url, Width: 72, Height: 72, ID: versionID + "-4x"})
			}
			if len(images) > 0 {
				versions[versionID] = images
			}
		}
		if len(versions) > 0 {
			out[setID] = versions
		}
	}
	return out, nil
}

func resolveTwitchBroadcasterIDByLogin(login string) (string, error) {
	login = normalizeTwitchChannelIdentity(login)
	if login == "" {
		return "", fmt.Errorf("missing twitch channel login")
	}

	now := time.Now()
	twitchBroadcasterIDCacheState.mu.RLock()
	if cached, ok := twitchBroadcasterIDCacheState.byLogin[login]; ok && now.Before(cached.expiresAt) && strings.TrimSpace(cached.broadcasterID) != "" {
		id := cached.broadcasterID
		twitchBroadcasterIDCacheState.mu.RUnlock()
		return id, nil
	}
	twitchBroadcasterIDCacheState.mu.RUnlock()

	base := strings.TrimRight(strings.TrimSpace(twitchHelixBaseURL), "/")
	if base == "" {
		return "", fmt.Errorf("missing twitch helix base url")
	}
	clientID, _ := twitchHelixCredentials()
	if clientID == "" {
		return "", fmt.Errorf("missing twitch helix client id (TWITCH_OAUTH_CLIENT_ID)")
	}
	token, err := getTwitchHelixAppToken()
	if err != nil {
		return "", err
	}

	query := url.Values{}
	query.Set("login", login)
	endpoint := base + "/users?" + query.Encode()

	do := func(accessToken string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Client-Id", clientID)
		req.Header.Set("Authorization", "Bearer "+accessToken)
		return twitchBadgeHTTPClient.Do(req)
	}

	resp, err := do(token)
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		clearTwitchHelixAppToken()
		token, err = getTwitchHelixAppToken()
		if err != nil {
			return "", err
		}
		resp, err = do(token)
		if err != nil {
			return "", err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("twitch helix users http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var users struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return "", err
	}
	if len(users.Data) == 0 || strings.TrimSpace(users.Data[0].ID) == "" {
		return "", fmt.Errorf("twitch helix users returned no id for login=%s", login)
	}

	id := strings.TrimSpace(users.Data[0].ID)
	twitchBroadcasterIDCacheState.mu.Lock()
	twitchBroadcasterIDCacheState.byLogin[login] = twitchBroadcasterIDCacheEntry{
		expiresAt:     now.Add(twitchBadgeCacheTTL),
		broadcasterID: id,
	}
	twitchBroadcasterIDCacheState.mu.Unlock()
	return id, nil
}

func selectTwitchBadgeImages(badgeID string, requestedVersion string, versions map[string][]Image) []Image {
	requestedVersion = strings.TrimSpace(requestedVersion)
	if imgs := versions[requestedVersion]; len(imgs) > 0 {
		return imgs
	}

	// Only subscriber needs increment mapping; other badge sets retain legacy exact->"1" fallback.
	if !strings.EqualFold(strings.TrimSpace(badgeID), "subscriber") {
		if imgs := versions["1"]; len(imgs) > 0 {
			return imgs
		}
		return nil
	}

	requested, err := strconv.Atoi(requestedVersion)
	if err == nil {
		bestFloor := -int(^uint(0)>>1) - 1
		hasFloor := false
		smallest := int(^uint(0) >> 1)
		hasSmallest := false
		for key := range versions {
			v, convErr := strconv.Atoi(strings.TrimSpace(key))
			if convErr != nil {
				continue
			}
			if v <= requested && (!hasFloor || v > bestFloor) {
				bestFloor = v
				hasFloor = true
			}
			if !hasSmallest || v < smallest {
				smallest = v
				hasSmallest = true
			}
		}
		if hasFloor {
			if imgs := versions[strconv.Itoa(bestFloor)]; len(imgs) > 0 {
				return imgs
			}
		}
		if hasSmallest {
			if imgs := versions[strconv.Itoa(smallest)]; len(imgs) > 0 {
				return imgs
			}
		}
	}

	if imgs := versions["1"]; len(imgs) > 0 {
		return imgs
	}
	return nil
}

func getTwitchBadgeDisplay(roomID string, sourceChannel string) (map[string]map[string][]Image, error) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		channel := normalizeTwitchChannelIdentity(sourceChannel)
		if channel != "" {
			resolvedID, err := resolveTwitchBroadcasterIDByLogin(channel)
			if err != nil {
				logTwitchBadgeWarnOnce("helix-users-"+channel, "badges: twitch helix broadcaster resolve failed (channel=%s): %v", channel, err)
			} else {
				roomID = resolvedID
			}
		} else {
			logTwitchBadgeWarnOnce("helix-channel-missing", "badges: twitch helix channel lookup skipped: missing room_id and source_channel")
		}
	}

	now := time.Now()

	twitchBadgeCacheState.mu.RLock()
	globalCached := now.Before(twitchBadgeCacheState.global.expiresAt) && len(twitchBadgeCacheState.global.badges) > 0
	var global map[string]map[string][]Image
	if globalCached {
		global = cloneBadgeImageMap(twitchBadgeCacheState.global.badges)
	}
	var channel map[string]map[string][]Image
	channelCached := false
	if roomID != "" {
		if entry, ok := twitchBadgeCacheState.channels[roomID]; ok && now.Before(entry.expiresAt) && len(entry.badges) > 0 {
			channel = cloneBadgeImageMap(entry.badges)
			channelCached = true
		}
	}
	twitchBadgeCacheState.mu.RUnlock()

	if !globalCached {
		freshGlobal, err := fetchTwitchHelixBadgeDisplay("/chat/badges/global", nil)
		if err != nil {
			return nil, fmt.Errorf("global helix badge lookup failed: %w", err)
		}
		twitchBadgeCacheState.mu.Lock()
		twitchBadgeCacheState.global = twitchBadgeCacheEntry{
			expiresAt: now.Add(twitchBadgeCacheTTL),
			badges:    cloneBadgeImageMap(freshGlobal),
		}
		twitchBadgeCacheState.mu.Unlock()
		global = freshGlobal
	}

	if roomID != "" && !channelCached {
		query := url.Values{}
		query.Set("broadcaster_id", roomID)
		freshChannel, err := fetchTwitchHelixBadgeDisplay("/chat/badges", query)
		if err != nil {
			// Fail open: keep global badges even if channel badge lookup fails.
			logTwitchBadgeWarnOnce("helix-channel-"+roomID, "badges: twitch helix channel badge lookup failed (room_id=%s): %v", roomID, err)
			channel = map[string]map[string][]Image{}
		} else {
			twitchBadgeCacheState.mu.Lock()
			twitchBadgeCacheState.channels[roomID] = twitchBadgeCacheEntry{
				expiresAt: now.Add(twitchBadgeCacheTTL),
				badges:    cloneBadgeImageMap(freshChannel),
			}
			twitchBadgeCacheState.mu.Unlock()
			channel = freshChannel
		}
	}

	merged := cloneBadgeImageMap(global)
	for setID, versions := range channel {
		if len(versions) == 0 {
			continue
		}
		if _, ok := merged[setID]; !ok {
			merged[setID] = map[string][]Image{}
		}
		for versionID, images := range versions {
			if len(images) == 0 {
				continue
			}
			copied := make([]Image, len(images))
			copy(copied, images)
			merged[setID][versionID] = copied
		}
	}
	return merged, nil
}

func enrichTwitchBadgesWithImages(badges []Badge, badgesRaw any, sourceChannel string) []Badge {
	if len(badges) == 0 {
		return badges
	}

	needsLookup := false
	for i := range badges {
		if !strings.EqualFold(badges[i].Platform, "twitch") {
			continue
		}
		if badgeHasUsableImage(badges[i].Images) {
			continue
		}
		needsLookup = true
		break
	}
	if !needsLookup {
		return badges
	}

	roomID := extractTwitchRoomID(badgesRaw)
	display, err := getTwitchBadgeDisplay(roomID, sourceChannel)
	if err != nil {
		logTwitchBadgeWarnOnce("helix-global", "badges: twitch helix badge resolve failed: %v", err)
		return badges
	}

	for i := range badges {
		badge := &badges[i]
		if !strings.EqualFold(strings.TrimSpace(badge.Platform), "twitch") {
			continue
		}
		if badgeHasUsableImage(badge.Images) {
			continue
		}
		setID := strings.TrimSpace(badge.ID)
		if setID == "" {
			continue
		}
		versions := display[setID]
		if len(versions) == 0 {
			continue
		}
		images := selectTwitchBadgeImages(setID, badge.Version, versions)
		if len(images) == 0 {
			continue
		}
		badge.Images = make([]Image, len(images))
		copy(badge.Images, images)
	}

	return badges
}

func encodeStoredBadges(badges []Badge) string {
	if len(badges) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(badges))
	for _, badge := range badges {
		id := strings.TrimSpace(badge.ID)
		if id == "" {
			continue
		}
		version := strings.TrimSpace(badge.Version)
		if version != "" {
			parts = append(parts, fmt.Sprintf("%s/%s", id, version))
		} else {
			parts = append(parts, id)
		}
	}
	if len(parts) == 0 {
		return "[]"
	}
	data, err := json.Marshal(parts)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func wsEnvelopeEnabled() bool {
	if overrideWSEnvelope != nil {
		return *overrideWSEnvelope
	}

	raw := strings.TrimSpace(os.Getenv("ELORA_WS_ENVELOPE"))
	if raw == "" {
		return true
	}

	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func maybeEnvelope(b []byte) []byte {
	if !wsEnvelopeEnabled() {
		return b
	}

	env := struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}{
		Type: "chat",
		Data: string(b),
	}

	out, err := json.Marshal(env)
	if err != nil {
		log.Printf("ws: failed to marshal envelope: %v", err)
		return b
	}

	return out
}

func messagePayloadFromStorage(m storage.Message) ([]byte, error) {
	if msg, ok := parseChatMessageFromRawJSON(m.RawJSON); ok {
		looksLikeMessage := strings.TrimSpace(msg.Author) != "" ||
			strings.TrimSpace(msg.Message) != "" ||
			strings.TrimSpace(msg.Source) != ""
		if looksLikeMessage {
			if msg.Author == "" {
				msg.Author = m.Username
			}
			if msg.Message == "" {
				msg.Message = m.Text
			}
			if msg.Source == "" {
				msg.Source = m.Platform
			}
			if len(msg.Emotes) == 0 && strings.TrimSpace(m.EmotesJSON) != "" {
				msg.Emotes = decodeEmotesJSON(m.EmotesJSON)
			}
			if badges, raw := parseStoredBadges(m.BadgesJSON); badges != nil || raw != nil {
				if msg.Badges == nil || len(msg.Badges) == 0 {
					msg.Badges = badges
					msg.BadgesRaw = raw
				} else if msg.BadgesRaw == nil {
					msg.BadgesRaw = raw
				}
			}
			resolveMessageSourceIdentity(&msg, m)
			if len(msg.Badges) > 0 {
				msg.Badges = enrichTwitchBadgesWithImages(msg.Badges, msg.BadgesRaw, msg.SourceChannel)
			}
			msg.UsernameColor = computeUsernameColor(msg, m)
			msg.Colour = msg.UsernameColor
			msg.normalize()
			return json.Marshal(msg.toChatPayload())
		}
	}

	badges, badgesRaw := parseStoredBadges(m.BadgesJSON)
	if badges == nil {
		badges = []Badge{}
	}

	emotes := decodeEmotesJSON(m.EmotesJSON)

	fallback := Message{
		Author:    m.Username,
		Message:   m.Text,
		Tokens:    []Token{},
		Emotes:    emotes,
		Badges:    badges,
		BadgesRaw: badgesRaw,
		Source:    m.Platform,
	}
	resolveMessageSourceIdentity(&fallback, m)
	if len(fallback.Badges) > 0 {
		fallback.Badges = enrichTwitchBadgesWithImages(fallback.Badges, fallback.BadgesRaw, fallback.SourceChannel)
	}
	fallback.UsernameColor = computeUsernameColor(fallback, m)
	fallback.Colour = fallback.UsernameColor
	fallback.normalize()

	data, err := json.Marshal(fallback.toChatPayload())
	if err != nil {
		return nil, err
	}

	return data, nil
}

func sanitizeMessagePayload(payload []byte) ([]byte, error) {
	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		return nil, err
	}
	msg.normalize()
	applyRuntimeSourceIdentity(&msg)

	if wsDropEmptyEnabled() && (msg.Source == "" || msg.Message == "") {
		return nil, errDropMessage
	}

	return json.Marshal(msg.toChatPayload())
}

func InitRoutes(store storage.Store) {
	if store == nil {
		log.Fatalf("storage: store is nil")
	}

	chatStore = store
	maybeExportStoredTwitchToken(store)
	startServiceTokenMaintainer()
	subscribersMu.Lock()
	if subscribers == nil {
		subscribers = make(map[chan []byte]struct{})
	}
	subscribersMu.Unlock()

	initRuntimeConfig(store)
	RegisterThirdPartyEmoteReloader(reloadThirdPartyEmotes)

	// Initialize tokenizer
	tokenizer.TextEffectSep = ':'
	tokenizer.TextCommandPrefix = '!'

	// Initialize command parser
	// TODO: Replace hardcoded timer duration with config setting
	commandParser = CommandParser{
		HelpTimer:         time.NewTimer(10 * time.Second),
		HelpResetDuration: 10 * time.Second,
	}

	if err := reloadThirdPartyEmotes(currentRuntimeConfig()); err != nil {
		log.Printf("emodl: failed to load third party emotes: %v", err)
	}
}

func maybeExportStoredTwitchToken(store storage.Store) {
	if store == nil || tokenfile.PathFromEnv() == "" {
		return
	}

	export := func(sess *storage.Session) bool {
		if sess == nil || strings.TrimSpace(sess.DataJSON) == "" {
			return false
		}
		token := strings.TrimSpace(authutil.ExtractTwitchToken([]byte(sess.DataJSON)))
		if token == "" {
			return false
		}
		if err := tokenfile.Save(token); err != nil {
			if !errors.Is(err, tokenfile.ErrEmptyToken) {
				log.Printf("auth: twitch token export skipped (%v)", err)
			}
			return true
		}
		log.Printf("auth: twitch token exported to file")
		return true
	}

	if sess, err := store.LatestSessionByService(ctx, "twitch"); err != nil {
		log.Printf("auth: twitch token preload failed: %v", err)
	} else if export(sess) {
		return
	}

	if sess, err := store.LatestSession(ctx); err != nil {
		log.Printf("auth: twitch token preload fallback failed: %v", err)
		return
	} else {
		_ = export(sess)
	}
}

func broadcastChatMessage(msg []byte) {
	subscribersMu.Lock()
	if len(subscribers) == 0 {
		subscribersMu.Unlock()
		return
	}
	targets := make([]chan []byte, 0, len(subscribers))
	for ch := range subscribers {
		targets = append(targets, ch)
	}
	subscribersMu.Unlock()

	for _, ch := range targets {
		payload := make([]byte, len(msg))
		copy(payload, msg)

		select {
		case ch <- payload:
		case <-time.After(2 * time.Second):
			log.Printf("ws: subscriber stalled; dropping connection")
			removeSubscriber(ch)
		}
	}
}

func enrichTailerMessage(m storage.Message) Message {
	var msg Message
	raw := strings.TrimSpace(m.RawJSON)
	// Keep provider payload opaque unless the payload is already chat-shaped.
	if rawMsg, ok := parseChatMessageFromRawJSON(raw); ok {
		msg = rawMsg
	}

	if msg.Author == "" {
		msg.Author = m.Username
	}
	if msg.Message == "" {
		msg.Message = m.Text
	}
	if msg.Source == "" {
		msg.Source = m.Platform
	}

	if msg.Emotes == nil || len(msg.Emotes) == 0 {
		if strings.TrimSpace(m.EmotesJSON) != "" {
			msg.Emotes = decodeEmotesJSON(m.EmotesJSON)
		}
	}
	if msg.Emotes == nil {
		msg.Emotes = []Emote{}
	}

	upsertEmoteCache(msg.Emotes)

	if parsed, raw := parseStoredBadges(m.BadgesJSON); parsed != nil || raw != nil {
		if msg.Badges == nil || len(msg.Badges) == 0 {
			msg.Badges = parsed
			msg.BadgesRaw = raw
		} else if msg.BadgesRaw == nil {
			msg.BadgesRaw = raw
		}
	}
	if msg.Badges == nil {
		msg.Badges = []Badge{}
	}

	if msg.Tokens == nil {
		msg.Tokens = make([]Token, 0, 8)
	}

	if len(msg.Tokens) == 0 && strings.EqualFold(msg.Source, "youtube") && len(msg.Emotes) > 0 {
		if fragments := buildYouTubeFragments(msg.Message, msg.Emotes); len(fragments) > 0 {
			msg.Tokens = fragments
		}
	}

	if len(msg.Tokens) == 0 {
		// Fallback: decode Twitch first-party emote spans -> Emote slice, then seed cache.
		// This runs only if we don't already have emotes on the message.
		if strings.EqualFold(m.Platform, "twitch") && len(msg.Emotes) == 0 {
			if dec := decodeTwitchSpans(m.Text, m.EmotesJSON); len(dec) > 0 {
				// Seed tokenizer cache by NAME so tokenization emits emote fragments.
				upsertEmoteCache(dec)
				msg.Emotes = append(msg.Emotes, dec...)
			}
		}

		for token := range tokenizerSnapshot().Iter(msg.Message) {
			msg.Tokens = append(msg.Tokens, token)
		}
	}

	msg.UsernameColor = computeUsernameColor(msg, m)
	msg.Colour = msg.UsernameColor

	if msg.Source == "" {
		msg.Source = m.Platform
	}

	msg.Source = normalizeSource(msg.Source)
	resolveMessageSourceIdentity(&msg, m)
	if len(msg.Badges) > 0 {
		msg.Badges = enrichTwitchBadgesWithImages(msg.Badges, msg.BadgesRaw, msg.SourceChannel)
	}
	msg.normalize()

	return msg
}

// BroadcastFromTailer enqueues a stored message onto the WebSocket broadcast loop.
func BroadcastFromTailer(m storage.Message) {
	msg := enrichTailerMessage(m)
	if wsDropEmptyEnabled() && (msg.Source == "" || msg.Message == "") {
		return
	}
	payload, err := json.Marshal(msg.toChatPayload())
	if err != nil {
		log.Printf("dbtailer: failed to marshal enriched message: %v", err)
		return
	}

	broadcastChatMessage(payload)
}

func addSubscriber() chan []byte {
	ch := make(chan []byte, 64)
	subscribersMu.Lock()
	if subscribers == nil {
		subscribers = make(map[chan []byte]struct{})
	}
	subscribers[ch] = struct{}{}
	subscribersMu.Unlock()
	return ch
}

func removeSubscriber(ch chan []byte) {
	subscribersMu.Lock()
	if subscribers != nil {
		if _, ok := subscribers[ch]; ok {
			delete(subscribers, ch)
			close(ch)
		}
	}
	subscribersMu.Unlock()
}

func replayEnabled(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// StreamChat initializes a WebSocket connection and streams chat messages
func StreamChat(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("ws: WebSocket upgrade error:", err)
		return
	}
	defer conn.Close()

	cfg := activeWebsocketConfig
	sourceFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source")))
	shouldReplay := replayEnabled(r.URL.Query().Get("replay"))
	if cfg.maxBytes > 0 {
		conn.SetReadLimit(cfg.maxBytes)
	}
	if cfg.pongWait > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(cfg.pongWait))
	}
	conn.SetPongHandler(func(string) error {
		if cfg.pongWait > 0 {
			return conn.SetReadDeadline(time.Now().Add(cfg.pongWait))
		}
		return nil
	})
	conn.SetPingHandler(func(appData string) error {
		deadline := time.Now().Add(cfg.writeDeadline)
		if cfg.writeDeadline <= 0 {
			deadline = time.Now().Add(5 * time.Second)
		}
		if cfg.pongWait > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(cfg.pongWait))
		}
		return conn.WriteControl(websocket.PongMessage, []byte(appData), deadline)
	})

	// Channel to signal closure of WebSocket connection
	done := make(chan struct{})
	messageChan := addSubscriber()
	defer removeSubscriber(messageChan)

	// Read the last 100 messages from the backing store to send to the client immediately.
	if shouldReplay && chatStore != nil {
		history, err := chatStore.GetRecent(ctx, storage.QueryOpts{Limit: 100})
		if err != nil {
			log.Printf("storage: Failed to read messages from store: %v\n", err)
		} else {
			for i := len(history) - 1; i >= 0; i-- {
				payload, marshalErr := messagePayloadFromStorage(history[i])
				if marshalErr != nil {
					log.Printf("chat: Failed to marshal history message: %v\n", marshalErr)
					continue
				}
				sanitized, marshalErr := sanitizeMessagePayload(payload)
				if marshalErr != nil {
					if errors.Is(marshalErr, errDropMessage) {
						continue
					}
					log.Printf("chat: Failed to sanitize history message: %v\n", marshalErr)
					continue
				}
				if shouldSkipSource(sanitized, sourceFilter) {
					continue
				}
				if err := conn.WriteMessage(websocket.TextMessage, maybeEnvelope(sanitized)); err != nil {
					log.Println("ws: WebSocket write error:", err)
					return
				}
			}
		}
	}

	// Websocket writer
	go func() {
		ticker := time.NewTicker(cfg.pingInterval)
		defer ticker.Stop()

		for {
			select {
			case m, ok := <-messageChan:
				if !ok {
					return
				}
				sanitized, err := sanitizeMessagePayload(m)
				if err != nil {
					if errors.Is(err, errDropMessage) {
						continue
					}
					log.Println("json: ", err)
					continue
				}
				if shouldSkipSource(sanitized, sourceFilter) {
					continue
				}
				if err := writeWSMessage(conn, websocket.TextMessage, maybeEnvelope(sanitized), cfg.writeDeadline); err != nil {
					log.Println("ws: WebSocket write error:", err)
					return
				}
			case <-ticker.C:
				if err := writeWSMessage(conn, websocket.TextMessage, []byte("__keepalive__"), cfg.writeDeadline); err != nil {
					log.Println("ws: Failed to send keep-alive message:", err)
					return
				}
				deadline := time.Now().Add(cfg.writeDeadline)
				if cfg.writeDeadline <= 0 {
					deadline = time.Now().Add(5 * time.Second)
				}
				if err := conn.WriteControl(websocket.PingMessage, []byte{}, deadline); err != nil {
					log.Println("ws: Failed to send ping:", err)
					return
				}
			case <-done:
				return
			}
		}
	}()

	// Read loop to keep connection alive and detect close
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNoStatusReceived) {
				log.Println("ws: WebSocket read error, closing connection:", err)
			}
			close(done)
			break
		}
		if cfg.pongWait > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(cfg.pongWait))
		}
	}
}

func writeWSMessage(conn *websocket.Conn, messageType int, payload []byte, deadline time.Duration) error {
	if deadline > 0 {
		_ = conn.SetWriteDeadline(time.Now().Add(deadline))
	} else {
		_ = conn.SetWriteDeadline(time.Time{})
	}
	return conn.WriteMessage(messageType, payload)
}

func shouldSkipSource(payload []byte, filter string) bool {
	if filter == "" {
		return false
	}

	var msg ws.ChatPayload
	if err := json.Unmarshal(payload, &msg); err != nil {
		return false
	}

	source := strings.ToLower(strings.TrimSpace(msg.Source))
	return source != filter
}

func originAllowed(origin string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return true
	}
	origin = strings.TrimRight(origin, "/")

	allowedOriginsMu.RLock()
	defer allowedOriginsMu.RUnlock()

	if allowAllOrigins {
		return true
	}
	_, ok := allowedOrigins[origin]
	return ok
}

// SetAllowedOrigins updates the accepted Origin headers for WebSocket connections.
func SetAllowedOrigins(origins []string) {
	allowedOriginsMu.Lock()
	defer allowedOriginsMu.Unlock()

	if len(origins) == 0 {
		allowAllOrigins = true
		allowedOrigins = map[string]struct{}{}
		return
	}

	allowAllOrigins = false
	allowedOrigins = make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		if trimmed == "*" {
			allowAllOrigins = true
			allowedOrigins = map[string]struct{}{}
			return
		}
		trimmed = strings.TrimRight(trimmed, "/")
		allowedOrigins[trimmed] = struct{}{}
	}
	if len(allowedOrigins) == 0 {
		allowAllOrigins = true
	}
}

func loadWebsocketConfigFromEnv() websocketConfig {
	cfg := websocketConfig{
		pingInterval:  durationFromEnv("ELORA_WS_PING_INTERVAL_MS", 25000),
		pongWait:      durationFromEnv("ELORA_WS_PONG_WAIT_MS", 30000),
		writeDeadline: durationFromEnv("ELORA_WS_WRITE_DEADLINE_MS", 5000),
		maxBytes:      int64FromEnv("ELORA_WS_MAX_MESSAGE_BYTES", 131072),
	}
	if cfg.pingInterval <= 0 {
		cfg.pingInterval = 25 * time.Second
	}
	if cfg.pongWait <= 0 {
		cfg.pongWait = 30 * time.Second
	}
	if cfg.writeDeadline < 0 {
		cfg.writeDeadline = 0
	}
	if cfg.maxBytes <= 0 {
		cfg.maxBytes = 131072
	}
	return cfg
}

// WebsocketConfig returns the currently active websocket runtime configuration.
func WebsocketConfig() WebsocketRuntimeConfig {
	cfg := activeWebsocketConfig
	return WebsocketRuntimeConfig{
		PingInterval:  cfg.pingInterval,
		PongWait:      cfg.pongWait,
		WriteDeadline: cfg.writeDeadline,
		MaxMessage:    cfg.maxBytes,
	}
}

// SetWebsocketConfig applies runtime websocket tuning for newly opened connections.
func SetWebsocketConfig(cfg WebsocketRuntimeConfig) {
	activeWebsocketConfig = websocketConfig{
		pingInterval:  cfg.PingInterval,
		pongWait:      cfg.PongWait,
		writeDeadline: cfg.WriteDeadline,
		maxBytes:      cfg.MaxMessage,
	}
}

// AllowedOriginsConfig returns whether all origins are permitted along with the normalized allow-list.
func AllowedOriginsConfig() (allowAll bool, origins []string) {
	allowedOriginsMu.RLock()
	defer allowedOriginsMu.RUnlock()

	if allowAllOrigins {
		return true, nil
	}
	if len(allowedOrigins) == 0 {
		return false, nil
	}

	origins = make([]string, 0, len(allowedOrigins))
	for origin := range allowedOrigins {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	return false, origins
}

func loadUIConfigFromEnv() uiConfig {
	hide := true
	raw := strings.TrimSpace(os.Getenv("ELORA_UI_YT_PREFIX_AT"))
	if raw != "" {
		hide = !isTruthy(raw)
	}

	showBadges := true
	if val := strings.TrimSpace(os.Getenv("ELORA_UI_SHOW_BADGES")); val != "" {
		showBadges = isTruthy(val)
	}

	return uiConfig{
		hideYouTubeAt: hide,
		showBadges:    showBadges,
	}
}

// UIConfig returns the active presentation toggles for the frontend.
func UIConfig() (hideYouTubeAt bool, showBadges bool) {
	return activeUIConfig.hideYouTubeAt, activeUIConfig.showBadges
}

// SetUIConfig applies runtime message presentation toggles.
func SetUIConfig(hideYouTubeAt bool, showBadges bool) {
	activeUIConfig = uiConfig{
		hideYouTubeAt: hideYouTubeAt,
		showBadges:    showBadges,
	}
}

// SetWSMessageBehavior applies runtime toggles for envelope/drop behavior.
func SetWSMessageBehavior(envelope bool, dropEmpty bool) {
	overrideWSEnvelope = &envelope
	overrideWSDropEmpty = &dropEmpty
}

func durationFromEnv(key string, def int) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return time.Duration(def) * time.Millisecond
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("config: invalid %s=%q, using default %d", key, raw, def)
		return time.Duration(def) * time.Millisecond
	}
	return time.Duration(n) * time.Millisecond
}

func int64FromEnv(key string, def int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		log.Printf("config: invalid %s=%q, using default %d", key, raw, def)
		return def
	}
	return n
}

func isTruthy(raw string) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	switch raw {
	case "1", "true", "yes", "on", "show", "enable":
		return true
	default:
		return false
	}
}

func ImageProxy(w http.ResponseWriter, r *http.Request) {
	imageURL := r.URL.Query().Get("url")
	if imageURL == "" {
		http.Error(w, "Missing URL parameter", http.StatusBadRequest)
		return
	}

	resp, err := http.Get(imageURL)
	if err != nil || resp.StatusCode == 404 {
		// Log error and serve a default placeholder image
		log.Printf("http: Error fetching image or not found: %s", imageURL)
		http.ServeFile(w, r, "../public/refresh.png")
		return
	}
	defer resp.Body.Close()

	// Copy headers
	for key, value := range resp.Header {
		w.Header().Set(key, value[0])
	}

	// Stream the image content
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// StopChatFetches is retained for backward compatibility with the legacy UI control.
// The legacy Python harvester pipeline has been removed, so this endpoint now returns
// a no-op response while gnasty-chat handles harvesting via SQLite.
func StopChatFetches(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]string{
		"status":  "ok",
		"message": "gnasty-chat is the active harvester; no restart required",
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("http: failed to encode restart response: %v", err)
		http.Error(w, "restart response unavailable", http.StatusInternalServerError)
	}
}

// SetupChatRoutes sets up WebSocket routes
func SetupChatRoutes(router *mux.Router) {
	// Public routes
	router.HandleFunc("/ws/chat", StreamChat).Methods("GET")
	router.HandleFunc("/imageproxy", ImageProxy).Methods("GET")

	// Subrouter for chat routes that require authentication
	protectedRoutes := router.PathPrefix("").Subrouter()
	protectedRoutes.Use(SessionMiddleware)

	// Add protected chat routes to protectedRoutes
	protectedRoutes.HandleFunc("/restart-server", StopChatFetches).Methods("POST")
}

// decodeTwitchSpans converts Twitch first-party span strings (e.g. "425618:12-14")
// into Emote entries with a 1x image URL. It is ASCII-safe and bounds-checked.
func decodeTwitchSpans(text string, spansJSON string) []Emote {
	if strings.TrimSpace(spansJSON) == "" {
		return nil
	}
	var raw []string
	if err := json.Unmarshal([]byte(spansJSON), &raw); err != nil || len(raw) == 0 {
		return nil
	}

	out := make([]Emote, 0, len(raw))
	// NOTE: we index by bytes; Twitch spans for common ASCII emotes (e.g., "LUL") align with byte boundaries.
	for _, s := range raw {
		parts := strings.SplitN(s, ":", 2)
		if len(parts) != 2 {
			continue
		}
		id := strings.TrimSpace(parts[0])
		rng := parts[1]
		bounds := strings.SplitN(rng, "-", 2)
		if len(bounds) != 2 {
			continue
		}
		start, err1 := strconv.Atoi(bounds[0])
		end, err2 := strconv.Atoi(bounds[1])
		if err1 != nil || err2 != nil || start < 0 || end < start {
			continue
		}
		if start >= len(text) {
			continue
		}
		if end >= len(text) {
			end = len(text) - 1
		}
		name := text[start : end+1]
		if strings.TrimSpace(name) == "" {
			continue
		}

		// Build a single 1x image (28px nominal) for first-party Twitch emotes.
		img := Image{
			ID:     id + "-1x",
			URL:    "https://static-cdn.jtvnw.net/emoticons/v2/" + id + "/default/dark/1.0",
			Width:  28,
			Height: 28,
		}
		out = append(out, Emote{
			ID:        id,
			Name:      name,
			Locations: []string{bounds[0] + "-" + bounds[1]},
			Images:    []Image{img},
		})
	}
	return out
}
