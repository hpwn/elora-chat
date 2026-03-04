package routes

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hpwn/EloraChat/src/backend/internal/runtimeconfig"
	"github.com/hpwn/EloraChat/src/backend/internal/storage"
)

func resetTwitchBadgeResolverForTests() {
	twitchBadgeCacheState.mu.Lock()
	twitchBadgeCacheState.global = twitchBadgeCacheEntry{}
	twitchBadgeCacheState.channels = map[string]twitchBadgeCacheEntry{}
	twitchBadgeCacheState.mu.Unlock()

	twitchBadgeTokenState.mu.Lock()
	twitchBadgeTokenState.token = ""
	twitchBadgeTokenState.expiresAt = time.Time{}
	twitchBadgeTokenState.mu.Unlock()

	twitchBadgeWarnState.mu.Lock()
	twitchBadgeWarnState.seen = map[string]struct{}{}
	twitchBadgeWarnState.mu.Unlock()

	twitchBroadcasterIDCacheState.mu.Lock()
	twitchBroadcasterIDCacheState.byLogin = map[string]twitchBroadcasterIDCacheEntry{}
	twitchBroadcasterIDCacheState.mu.Unlock()
}

func TestBadgeUnmarshalJSONCompat(t *testing.T) {
	var badges []Badge
	payload := []byte(`[{"name":"moderator","badge_version":"2"},{"badge_id":"subscriber","version":"12"}]`)
	if err := json.Unmarshal(payload, &badges); err != nil {
		t.Fatalf("failed to unmarshal badges: %v", err)
	}
	if len(badges) != 2 {
		t.Fatalf("expected 2 badges, got %d", len(badges))
	}
	if badges[0].ID != "moderator" || badges[0].Version != "2" {
		t.Fatalf("unexpected first badge: %#v", badges[0])
	}
	if badges[1].ID != "subscriber" || badges[1].Version != "12" {
		t.Fatalf("unexpected second badge: %#v", badges[1])
	}

	payload = []byte(`["vip/1","founder"]`)
	badges = nil
	if err := json.Unmarshal(payload, &badges); err != nil {
		t.Fatalf("failed to unmarshal string badges: %v", err)
	}
	if len(badges) != 2 {
		t.Fatalf("expected 2 string badges, got %d", len(badges))
	}
	if badges[0].ID != "vip" || badges[0].Version != "1" {
		t.Fatalf("unexpected first string badge: %#v", badges[0])
	}
	if badges[1].ID != "founder" || badges[1].Version != "" {
		t.Fatalf("unexpected second string badge: %#v", badges[1])
	}
}

func TestMessagePayloadFromStorageFallback(t *testing.T) {
	userColorMap["tester"] = "#112233"

	payload, err := messagePayloadFromStorage(storage.Message{
		Username:   "tester",
		Text:       "hello",
		Platform:   "Twitch",
		BadgesJSON: "[]",
	})
	if err != nil {
		t.Fatalf("messagePayloadFromStorage returned error: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if msg.Author != "tester" {
		t.Fatalf("expected author to be tester, got %q", msg.Author)
	}
	if msg.Message != "hello" {
		t.Fatalf("expected message 'hello', got %q", msg.Message)
	}
	if msg.Source != "Twitch" {
		t.Fatalf("expected source 'Twitch', got %q", msg.Source)
	}
	if msg.Colour != sanitizeUsernameColorForDarkBG("#112233") {
		t.Fatalf("expected colour %q, got %q", sanitizeUsernameColorForDarkBG("#112233"), msg.Colour)
	}
	if msg.Tokens == nil || len(msg.Tokens) != 0 {
		t.Fatalf("expected empty tokens slice, got %#v", msg.Tokens)
	}
	if msg.Emotes == nil || len(msg.Emotes) != 0 {
		t.Fatalf("expected empty emotes slice, got %#v", msg.Emotes)
	}
	if msg.Badges == nil || len(msg.Badges) != 0 {
		t.Fatalf("expected empty badges slice, got %#v", msg.Badges)
	}
}

func TestMessagePayloadFromStorageBadges(t *testing.T) {
	payload, err := messagePayloadFromStorage(storage.Message{
		Username:   "tester",
		Text:       "hello",
		Platform:   "Twitch",
		BadgesJSON: `{"not":"array"}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if len(msg.Badges) != 0 {
		t.Fatalf("expected malformed badges to be ignored, got %#v", msg.Badges)
	}

	payload, err = messagePayloadFromStorage(storage.Message{
		Username:   "tester",
		Text:       "hello",
		Platform:   "Twitch",
		BadgesJSON: ` [ "subscriber/42" , "bits/100" , "vip" ] `,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	want := []Badge{{ID: "subscriber", Platform: "twitch", Version: "42"}, {ID: "bits", Platform: "twitch", Version: "100"}, {ID: "vip", Platform: "twitch"}}
	if len(msg.Badges) != len(want) {
		t.Fatalf("expected %d badges, got %d", len(want), len(msg.Badges))
	}
	for i, badge := range msg.Badges {
		if badge.ID != want[i].ID || badge.Version != want[i].Version || badge.Platform != want[i].Platform {
			t.Fatalf("badge[%d] mismatch: got %#v want %#v", i, badge, want[i])
		}
	}

	payload, err = messagePayloadFromStorage(storage.Message{
		Username:   "tester",
		Text:       "hello",
		Platform:   "Twitch",
		BadgesJSON: `{"badges":[{"platform":"twitch","id":"subscriber","version":"17"},{"platform":"twitch","id":"premium","version":"1"}],"raw":{"t1":true}}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if len(msg.Badges) != 2 {
		t.Fatalf("expected 2 badges, got %d", len(msg.Badges))
	}
	if msg.Badges[0].Platform != "twitch" || msg.Badges[0].ID != "subscriber" || msg.Badges[0].Version != "17" {
		t.Fatalf("unexpected first badge: %#v", msg.Badges[0])
	}
	if msg.Badges[1].Platform != "twitch" || msg.Badges[1].ID != "premium" || msg.Badges[1].Version != "1" {
		t.Fatalf("unexpected second badge: %#v", msg.Badges[1])
	}
	if msg.BadgesRaw == nil {
		t.Fatalf("expected badges_raw to be populated")
	}
}

func TestParseStoredBadgesPrefersTwitchSubscriberVersion(t *testing.T) {
	raw := `{"badges":[{"platform":"twitch","id":"subscriber","version":"19"},{"platform":"twitch","id":"premium","version":"1"}],"raw":{"twitch":{"badges":"subscriber/12,premium/1","badge_info":"subscriber/19"}}}`
	badges, rawAny := parseStoredBadges(raw)
	if badges == nil {
		t.Fatalf("expected badges to be parsed")
	}
	if rawAny == nil {
		t.Fatalf("expected raw payload to be parsed")
	}
	if len(badges) != 2 {
		t.Fatalf("expected 2 badges, got %d", len(badges))
	}
	if badges[0].ID != "subscriber" || badges[0].Platform != "twitch" || badges[0].Version != "12" {
		t.Fatalf("unexpected subscriber badge: %#v", badges[0])
	}
	if badges[1].ID != "premium" || badges[1].Platform != "twitch" || badges[1].Version != "1" {
		t.Fatalf("unexpected premium badge: %#v", badges[1])
	}
}

func TestMessagePayloadFromStorageRawBadges(t *testing.T) {
	payload, err := messagePayloadFromStorage(storage.Message{
		Username:   "tester",
		Text:       "hello",
		Platform:   "Twitch",
		RawJSON:    `{"author":"tester","message":"hello","fragments":[],"emotes":[],"source":"Twitch"}`,
		BadgesJSON: `{"badges":[{"platform":"twitch","id":"subscriber","version":"17"},{"platform":"twitch","id":"premium","version":"1"}],"raw":{"extra":123}}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if len(msg.Badges) != 2 {
		t.Fatalf("expected 2 badges, got %d", len(msg.Badges))
	}
	if msg.BadgesRaw == nil {
		t.Fatalf("expected badges_raw to be propagated")
	}
	if msg.Badges[0].ID != "subscriber" || msg.Badges[0].Platform != "twitch" || msg.Badges[0].Version != "17" {
		t.Fatalf("unexpected badge[0]: %#v", msg.Badges[0])
	}
	if msg.Badges[1].ID != "premium" || msg.Badges[1].Platform != "twitch" || msg.Badges[1].Version != "1" {
		t.Fatalf("unexpected badge[1]: %#v", msg.Badges[1])
	}
}

func TestMessagePayloadFromStorageOverridesEmptyRawBadges(t *testing.T) {
	payload, err := messagePayloadFromStorage(storage.Message{
		Username:   "tester",
		Text:       "hello",
		Platform:   "Twitch",
		RawJSON:    `{"author":"tester","message":"hello","badges":[],"source":"Twitch"}`,
		BadgesJSON: `{"badges":[{"platform":"twitch","id":"subscriber","version":"17"},{"platform":"twitch","id":"premium","version":"1"}],"raw":{"extra":123}}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if len(msg.Badges) != 2 {
		t.Fatalf("expected 2 badges, got %d", len(msg.Badges))
	}
	if msg.BadgesRaw == nil {
		t.Fatalf("expected badges_raw to be propagated")
	}
	if msg.Badges[0].ID != "subscriber" || msg.Badges[0].Platform != "twitch" || msg.Badges[0].Version != "17" {
		t.Fatalf("unexpected badge[0]: %#v", msg.Badges[0])
	}
	if msg.Badges[1].ID != "premium" || msg.Badges[1].Platform != "twitch" || msg.Badges[1].Version != "1" {
		t.Fatalf("unexpected badge[1]: %#v", msg.Badges[1])
	}
}

func TestMessagePayloadFromStoragePreservesBadgeImages(t *testing.T) {
	payload, err := messagePayloadFromStorage(storage.Message{
		Username:   "tester",
		Text:       "hello",
		Platform:   "Twitch",
		BadgesJSON: `{"badges":[{"platform":"twitch","id":"subscriber","version":"2","images":[{"url":"https://static.twitchcdn.net/badges/v1/subscriber_1x.png","width":18,"height":18},{"url":"https://static.twitchcdn.net/badges/v1/subscriber_2x.png","width":36,"height":36}]}]}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var msg struct {
		Badges []Badge `json:"badges"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if len(msg.Badges) != 1 {
		t.Fatalf("expected 1 badge, got %d", len(msg.Badges))
	}
	badge := msg.Badges[0]
	if badge.ID != "subscriber" || badge.Platform != "twitch" || badge.Version != "2" {
		t.Fatalf("unexpected badge metadata: %#v", badge)
	}
	if len(badge.Images) != 2 {
		t.Fatalf("expected 2 badge images, got %d", len(badge.Images))
	}
	if badge.Images[0].URL != "https://static.twitchcdn.net/badges/v1/subscriber_1x.png" || badge.Images[1].URL != "https://static.twitchcdn.net/badges/v1/subscriber_2x.png" {
		t.Fatalf("unexpected badge images: %#v", badge.Images)
	}
}

func TestMessagePayloadFromStorageEnrichesTwitchBadgeImages(t *testing.T) {
	const (
		testClientID     = "test-client-id"
		testClientSecret = "test-client-secret"
		testToken        = "test-token"
	)
	t.Setenv("TWITCH_OAUTH_CLIENT_ID", testClientID)
	t.Setenv("TWITCH_OAUTH_CLIENT_SECRET", testClientSecret)

	tokenRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oauth2/token":
			tokenRequests++
			if got := r.Header.Get("Content-Type"); !strings.Contains(got, "application/x-www-form-urlencoded") {
				t.Fatalf("expected token request content-type form-encoded, got %q", got)
			}
			body, _ := io.ReadAll(r.Body)
			values, err := url.ParseQuery(string(body))
			if err != nil {
				t.Fatalf("expected parseable token body, got error: %v", err)
			}
			if values.Get("client_id") != testClientID || values.Get("client_secret") != testClientSecret {
				t.Fatalf("unexpected token credentials in body: %q", string(body))
			}
			_, _ = w.Write([]byte(`{"access_token":"` + testToken + `","expires_in":3600,"token_type":"bearer"}`))
		case "/helix/chat/badges/global":
			if got := r.Header.Get("Client-Id"); got != testClientID {
				t.Fatalf("expected Client-Id header %q, got %q", testClientID, got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
				t.Fatalf("expected Authorization header %q, got %q", "Bearer "+testToken, got)
			}
			_, _ = w.Write([]byte(`{"data":[{"set_id":"subscriber","versions":[{"id":"12","image_url_1x":"https://img.example/sub12-1x.png","image_url_2x":"https://img.example/sub12-2x.png"}]}]}`))
		case "/helix/chat/badges":
			if got := r.Header.Get("Client-Id"); got != testClientID {
				t.Fatalf("expected Client-Id header %q, got %q", testClientID, got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
				t.Fatalf("expected Authorization header %q, got %q", "Bearer "+testToken, got)
			}
			if got := r.URL.Query().Get("broadcaster_id"); got != "40934651" {
				t.Fatalf("expected broadcaster_id 40934651, got %q", got)
			}
			_, _ = w.Write([]byte(`{"data":[{"set_id":"vip","versions":[{"id":"1","image_url_1x":"https://img.example/vip1-1x.png"}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldHelixBase := twitchHelixBaseURL
	oldTokenURL := twitchOAuthTokenURL
	oldClient := twitchBadgeHTTPClient
	twitchHelixBaseURL = server.URL + "/helix"
	twitchOAuthTokenURL = server.URL + "/oauth2/token"
	twitchBadgeHTTPClient = server.Client()
	resetTwitchBadgeResolverForTests()
	t.Cleanup(func() {
		twitchHelixBaseURL = oldHelixBase
		twitchOAuthTokenURL = oldTokenURL
		twitchBadgeHTTPClient = oldClient
		resetTwitchBadgeResolverForTests()
	})

	payload, err := messagePayloadFromStorage(storage.Message{
		Username: "tester",
		Text:     "hello",
		Platform: "Twitch",
		BadgesJSON: `{"badges":[{"platform":"twitch","id":"subscriber","version":"12"},{"platform":"twitch","id":"vip","version":"1"}],` +
			`"raw":{"twitch":{"badges":"subscriber/12,vip/1","room_id":"40934651"}}}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var msg struct {
		Badges []Badge `json:"badges"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if len(msg.Badges) != 2 {
		t.Fatalf("expected 2 badges, got %d", len(msg.Badges))
	}
	if len(msg.Badges[0].Images) == 0 || msg.Badges[0].Images[0].URL != "https://img.example/sub12-1x.png" {
		t.Fatalf("expected subscriber badge images, got %#v", msg.Badges[0].Images)
	}
	if len(msg.Badges[1].Images) == 0 || msg.Badges[1].Images[0].URL != "https://img.example/vip1-1x.png" {
		t.Fatalf("expected vip badge images, got %#v", msg.Badges[1].Images)
	}
	if tokenRequests != 1 {
		t.Fatalf("expected one token request due to cache reuse, got %d", tokenRequests)
	}
}

func TestMessagePayloadFromStorageTwitchBadgeResolveFailOpen(t *testing.T) {
	t.Setenv("TWITCH_OAUTH_CLIENT_ID", "")
	t.Setenv("TWITCH_OAUTH_CLIENT_SECRET", "")

	oldHelixBase := twitchHelixBaseURL
	oldTokenURL := twitchOAuthTokenURL
	oldClient := twitchBadgeHTTPClient
	twitchHelixBaseURL = "http://127.0.0.1:1/helix"
	twitchOAuthTokenURL = "http://127.0.0.1:1/oauth2/token"
	twitchBadgeHTTPClient = &http.Client{}
	resetTwitchBadgeResolverForTests()
	t.Cleanup(func() {
		twitchHelixBaseURL = oldHelixBase
		twitchOAuthTokenURL = oldTokenURL
		twitchBadgeHTTPClient = oldClient
		resetTwitchBadgeResolverForTests()
	})

	payload, err := messagePayloadFromStorage(storage.Message{
		Username:   "tester",
		Text:       "hello",
		Platform:   "Twitch",
		BadgesJSON: `{"badges":[{"platform":"twitch","id":"subscriber","version":"12"}],"raw":{"twitch":{"badges":"subscriber/12","room_id":"40934651"}}}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var msg struct {
		Badges []Badge `json:"badges"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if len(msg.Badges) != 1 {
		t.Fatalf("expected 1 badge, got %d", len(msg.Badges))
	}
	if len(msg.Badges[0].Images) != 0 {
		t.Fatalf("expected fail-open without images, got %#v", msg.Badges[0].Images)
	}
}

func TestEnrichTwitchBadgesWithImagesSkipsNonTwitch(t *testing.T) {
	t.Setenv("TWITCH_OAUTH_CLIENT_ID", "unused")
	t.Setenv("TWITCH_OAUTH_CLIENT_SECRET", "unused")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected resolver request for non-twitch badge: %s", r.URL.Path)
	}))
	defer server.Close()

	oldHelixBase := twitchHelixBaseURL
	oldTokenURL := twitchOAuthTokenURL
	oldClient := twitchBadgeHTTPClient
	twitchHelixBaseURL = server.URL + "/helix"
	twitchOAuthTokenURL = server.URL + "/oauth2/token"
	twitchBadgeHTTPClient = server.Client()
	resetTwitchBadgeResolverForTests()
	t.Cleanup(func() {
		twitchHelixBaseURL = oldHelixBase
		twitchOAuthTokenURL = oldTokenURL
		twitchBadgeHTTPClient = oldClient
		resetTwitchBadgeResolverForTests()
	})

	in := []Badge{{Platform: "youtube", ID: "moderator", Version: "1"}}
	out := enrichTwitchBadgesWithImages(in, map[string]any{"twitch": map[string]any{"room_id": "40934651"}}, "dayoman")
	if len(out) != 1 {
		t.Fatalf("expected single badge, got %d", len(out))
	}
	if len(out[0].Images) != 0 {
		t.Fatalf("expected non-twitch badge images unchanged, got %#v", out[0].Images)
	}
}

func TestGetTwitchHelixAppTokenUsesCacheUntilExpiryWindow(t *testing.T) {
	t.Setenv("TWITCH_OAUTH_CLIENT_ID", "cid")
	t.Setenv("TWITCH_OAUTH_CLIENT_SECRET", "secret")

	tokenRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/token" {
			http.NotFound(w, r)
			return
		}
		tokenRequests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"cache-token","expires_in":3600,"token_type":"bearer"}`))
	}))
	defer server.Close()

	oldTokenURL := twitchOAuthTokenURL
	oldClient := twitchBadgeHTTPClient
	twitchOAuthTokenURL = server.URL + "/oauth2/token"
	twitchBadgeHTTPClient = server.Client()
	resetTwitchBadgeResolverForTests()
	t.Cleanup(func() {
		twitchOAuthTokenURL = oldTokenURL
		twitchBadgeHTTPClient = oldClient
		resetTwitchBadgeResolverForTests()
	})

	token1, err := getTwitchHelixAppToken()
	if err != nil {
		t.Fatalf("first token fetch failed: %v", err)
	}
	token2, err := getTwitchHelixAppToken()
	if err != nil {
		t.Fatalf("second token fetch failed: %v", err)
	}
	if token1 != "cache-token" || token2 != "cache-token" {
		t.Fatalf("expected cached token, got %q and %q", token1, token2)
	}
	if tokenRequests != 1 {
		t.Fatalf("expected one token request, got %d", tokenRequests)
	}
}

func TestGetTwitchHelixAppTokenRefreshesNearExpiry(t *testing.T) {
	t.Setenv("TWITCH_OAUTH_CLIENT_ID", "cid")
	t.Setenv("TWITCH_OAUTH_CLIENT_SECRET", "secret")

	tokenRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/token" {
			http.NotFound(w, r)
			return
		}
		tokenRequests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"refreshed-token","expires_in":3600,"token_type":"bearer"}`))
	}))
	defer server.Close()

	oldTokenURL := twitchOAuthTokenURL
	oldClient := twitchBadgeHTTPClient
	twitchOAuthTokenURL = server.URL + "/oauth2/token"
	twitchBadgeHTTPClient = server.Client()
	resetTwitchBadgeResolverForTests()
	t.Cleanup(func() {
		twitchOAuthTokenURL = oldTokenURL
		twitchBadgeHTTPClient = oldClient
		resetTwitchBadgeResolverForTests()
	})

	twitchBadgeTokenState.mu.Lock()
	twitchBadgeTokenState.token = "stale-token"
	twitchBadgeTokenState.expiresAt = time.Now().Add(30 * time.Second)
	twitchBadgeTokenState.mu.Unlock()

	token, err := getTwitchHelixAppToken()
	if err != nil {
		t.Fatalf("token refresh failed: %v", err)
	}
	if token != "refreshed-token" {
		t.Fatalf("expected refreshed token, got %q", token)
	}
	if tokenRequests != 1 {
		t.Fatalf("expected refresh token request, got %d", tokenRequests)
	}
}

func TestResolveYouTubeChannelIDBySourceDirectID(t *testing.T) {
	got, err := resolveYouTubeChannelIDBySource("UCmbSGFM9OU8FwjxZCevr6zw")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "UCmbSGFM9OU8FwjxZCevr6zw" {
		t.Fatalf("unexpected channel id %q", got)
	}
}

func TestResolveYouTubeChannelIDBySourceHandle(t *testing.T) {
	t.Setenv("YOUTUBE_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/channels" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("forHandle"); got != "ludwig" {
			t.Fatalf("expected forHandle=ludwig, got %q", got)
		}
		if got := r.URL.Query().Get("key"); got != "test-key" {
			t.Fatalf("expected key=test-key, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"UCmbSGFM9OU8FwjxZCevr6zw"}]}`))
	}))
	defer server.Close()

	oldBase := youtubeDataAPIBaseURL
	oldClient := youtubeDataHTTPClient
	youtubeDataAPIBaseURL = server.URL
	youtubeDataHTTPClient = server.Client()
	t.Cleanup(func() {
		youtubeDataAPIBaseURL = oldBase
		youtubeDataHTTPClient = oldClient
	})

	got, err := resolveYouTubeChannelIDBySource("https://www.youtube.com/@ludwig/live")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "UCmbSGFM9OU8FwjxZCevr6zw" {
		t.Fatalf("unexpected channel id %q", got)
	}
}

func TestResolveYouTubeChannelIDBySourceVideo(t *testing.T) {
	t.Setenv("YOUTUBE_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/videos" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("id"); got != "abcdefghijk" {
			t.Fatalf("expected id=abcdefghijk, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"snippet":{"channelId":"UCmbSGFM9OU8FwjxZCevr6zw"}}]}`))
	}))
	defer server.Close()

	oldBase := youtubeDataAPIBaseURL
	oldClient := youtubeDataHTTPClient
	youtubeDataAPIBaseURL = server.URL
	youtubeDataHTTPClient = server.Client()
	t.Cleanup(func() {
		youtubeDataAPIBaseURL = oldBase
		youtubeDataHTTPClient = oldClient
	})

	got, err := resolveYouTubeChannelIDBySource("https://www.youtube.com/watch?v=abcdefghijk")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "UCmbSGFM9OU8FwjxZCevr6zw" {
		t.Fatalf("unexpected channel id %q", got)
	}
}

func TestResolveYouTubeChannelIDBySourceMissingAPIKey(t *testing.T) {
	t.Setenv("YOUTUBE_API_KEY", "")
	_, err := resolveYouTubeChannelIDBySource("https://www.youtube.com/watch?v=abcdefghijk")
	if err == nil {
		t.Fatalf("expected missing key error")
	}
	if !strings.Contains(err.Error(), "YOUTUBE_API_KEY") {
		t.Fatalf("expected YOUTUBE_API_KEY error, got %v", err)
	}
}

func TestMessagePayloadFromStorageEnrichesTwitchSubscriberViaSourceChannelFallback(t *testing.T) {
	const (
		testClientID     = "test-client-id"
		testClientSecret = "test-client-secret"
		testToken        = "test-token"
	)
	t.Setenv("TWITCH_OAUTH_CLIENT_ID", testClientID)
	t.Setenv("TWITCH_OAUTH_CLIENT_SECRET", testClientSecret)

	tokenRequests := 0
	userResolveRequests := 0
	channelBadgeRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oauth2/token":
			tokenRequests++
			_, _ = w.Write([]byte(`{"access_token":"` + testToken + `","expires_in":3600,"token_type":"bearer"}`))
		case "/helix/users":
			userResolveRequests++
			if got := r.URL.Query().Get("login"); got != "dayoman" {
				t.Fatalf("expected login=dayoman, got %q", got)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"40934651"}]}`))
		case "/helix/chat/badges/global":
			_, _ = w.Write([]byte(`{"data":[{"set_id":"subscriber","versions":[{"id":"1","image_url_1x":"https://img.example/global-sub-1x.png"}]}]}`))
		case "/helix/chat/badges":
			channelBadgeRequests++
			if got := r.URL.Query().Get("broadcaster_id"); got != "40934651" {
				t.Fatalf("expected broadcaster_id=40934651, got %q", got)
			}
			_, _ = w.Write([]byte(`{"data":[{"set_id":"subscriber","versions":[` +
				`{"id":"0","image_url_1x":"https://img.example/ch-sub0-1x.png"},` +
				`{"id":"3","image_url_1x":"https://img.example/ch-sub3-1x.png"},` +
				`{"id":"6","image_url_1x":"https://img.example/ch-sub6-1x.png"},` +
				`{"id":"12","image_url_1x":"https://img.example/ch-sub12-1x.png"},` +
				`{"id":"24","image_url_1x":"https://img.example/ch-sub24-1x.png"}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldHelixBase := twitchHelixBaseURL
	oldTokenURL := twitchOAuthTokenURL
	oldClient := twitchBadgeHTTPClient
	twitchHelixBaseURL = server.URL + "/helix"
	twitchOAuthTokenURL = server.URL + "/oauth2/token"
	twitchBadgeHTTPClient = server.Client()
	resetTwitchBadgeResolverForTests()
	t.Cleanup(func() {
		twitchHelixBaseURL = oldHelixBase
		twitchOAuthTokenURL = oldTokenURL
		twitchBadgeHTTPClient = oldClient
		resetTwitchBadgeResolverForTests()
	})

	payload, err := messagePayloadFromStorage(storage.Message{
		Username: "tester",
		Text:     "hello",
		Platform: "Twitch",
		RawJSON:  `{"channel":"dayoman"}`,
		BadgesJSON: `{"badges":[{"platform":"twitch","id":"subscriber","version":"17"}],` +
			`"raw":{"twitch":{"badges":"subscriber/17"}}}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var msg struct {
		Badges []Badge `json:"badges"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if len(msg.Badges) != 1 {
		t.Fatalf("expected 1 badge, got %d", len(msg.Badges))
	}
	if len(msg.Badges[0].Images) == 0 || msg.Badges[0].Images[0].URL != "https://img.example/ch-sub12-1x.png" {
		t.Fatalf("expected channel increment badge image (12-month), got %#v", msg.Badges[0].Images)
	}
	if tokenRequests != 1 {
		t.Fatalf("expected one token request, got %d", tokenRequests)
	}
	if userResolveRequests != 1 {
		t.Fatalf("expected one user resolve request, got %d", userResolveRequests)
	}
	if channelBadgeRequests != 1 {
		t.Fatalf("expected one channel badge request, got %d", channelBadgeRequests)
	}
}

func TestSelectTwitchBadgeImagesSubscriberUsesChannelIncrements(t *testing.T) {
	versions := map[string][]Image{
		"0":  {{URL: "u0"}},
		"3":  {{URL: "u3"}},
		"6":  {{URL: "u6"}},
		"12": {{URL: "u12"}},
		"24": {{URL: "u24"}},
		"36": {{URL: "u36"}},
	}

	imgs := selectTwitchBadgeImages("subscriber", "17", versions)
	if len(imgs) == 0 || imgs[0].URL != "u12" {
		t.Fatalf("expected floor match u12 for version 17, got %#v", imgs)
	}
	imgs = selectTwitchBadgeImages("subscriber", "36", versions)
	if len(imgs) == 0 || imgs[0].URL != "u36" {
		t.Fatalf("expected exact u36 for version 36, got %#v", imgs)
	}
	imgs = selectTwitchBadgeImages("subscriber", "1", versions)
	if len(imgs) == 0 || imgs[0].URL != "u0" {
		t.Fatalf("expected smallest increment u0 for version 1, got %#v", imgs)
	}

	nonSub := map[string][]Image{
		"1": {{URL: "v1"}},
	}
	imgs = selectTwitchBadgeImages("vip", "2", nonSub)
	if len(imgs) == 0 || imgs[0].URL != "v1" {
		t.Fatalf("expected non-subscriber fallback to version 1, got %#v", imgs)
	}
}

func TestMaybeEnvelope(t *testing.T) {
	t.Setenv("ELORA_WS_ENVELOPE", "true")

	payload := []byte(`{"message":"hi"}`)
	enveloped := maybeEnvelope(payload)

	var env struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}
	if err := json.Unmarshal(enveloped, &env); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}

	if env.Type != "chat" {
		t.Fatalf("expected envelope type 'chat', got %q", env.Type)
	}
	if env.Data != string(payload) {
		t.Fatalf("expected envelope data to equal payload, got %q", env.Data)
	}

	// Ensure disabling the flag returns the payload unchanged.
	t.Setenv("ELORA_WS_ENVELOPE", "off")
	raw := maybeEnvelope(payload)
	if string(raw) != string(payload) {
		t.Fatalf("expected raw payload when envelope disabled, got %s", string(raw))
	}

	// Default (unset) should enable the envelope.
	if err := os.Unsetenv("ELORA_WS_ENVELOPE"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	enveloped = maybeEnvelope(payload)
	if bytes.Equal(enveloped, payload) {
		t.Fatalf("expected envelope to be applied by default")
	}
}

func TestSanitizeMessagePayloadDrop(t *testing.T) {
	if err := os.Unsetenv("ELORA_WS_DROP_EMPTY"); err != nil {
		t.Fatalf("failed to unset drop env: %v", err)
	}

	payload := []byte(`{"author":"tester","message":"   ","fragments":[],"emotes":[],"badges":[],"source":""}`)
	if _, err := sanitizeMessagePayload(payload); !errors.Is(err, errDropMessage) {
		t.Fatalf("expected errDropMessage, got %v", err)
	}
}

func TestSanitizeMessagePayloadNormalizes(t *testing.T) {
	t.Setenv("ELORA_WS_DROP_EMPTY", "false")

	payload := []byte(`{"author":"tester","message":" hello ","fragments":null,"emotes":null,"badges":null,"source":" twitch "}`)
	sanitized, err := sanitizeMessagePayload(payload)
	if err != nil {
		t.Fatalf("sanitizeMessagePayload returned error: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(sanitized, &msg); err != nil {
		t.Fatalf("failed to unmarshal sanitized payload: %v", err)
	}

	if msg.Source != "Twitch" {
		t.Fatalf("expected source to normalize to Twitch, got %q", msg.Source)
	}
	if msg.Message != "hello" {
		t.Fatalf("expected message to be trimmed to 'hello', got %q", msg.Message)
	}
	if msg.Tokens == nil {
		t.Fatalf("expected tokens slice to be initialized")
	}
	if msg.Emotes == nil {
		t.Fatalf("expected emotes slice to be initialized")
	}
	if msg.Badges == nil {
		t.Fatalf("expected badges slice to be initialized")
	}
}

func TestEnrichTailerMessage(t *testing.T) {
	tokenizer.TextEffectSep = ':'
	tokenizer.TextCommandPrefix = '!'
	tokenizer.EmoteCache = make(map[string]Emote)

	raw := `{"author":"TailerUser","message":"hello Pog"}`
	msg := enrichTailerMessage(storage.Message{
		Username:   "TailerUser",
		Platform:   "YouTube",
		RawJSON:    raw,
		EmotesJSON: "[]",
	})

	if msg.Author != "TailerUser" {
		t.Fatalf("expected author TailerUser, got %q", msg.Author)
	}
	if msg.Message != "hello Pog" {
		t.Fatalf("expected message 'hello Pog', got %q", msg.Message)
	}
	if msg.Source != "YouTube" {
		t.Fatalf("expected source YouTube, got %q", msg.Source)
	}
	if msg.Colour == "" {
		t.Fatalf("expected colour to be populated")
	}
	if msg.Tokens == nil {
		t.Fatalf("expected tokens slice to be initialized")
	}
	if msg.Badges == nil {
		t.Fatalf("expected badges slice to be initialized")
	}
	if msg.Emotes == nil {
		t.Fatalf("expected emotes slice to be initialized")
	}
}

func TestBroadcastFromTailerEnrichesPayload(t *testing.T) {
	tokenizer.TextEffectSep = ':'
	tokenizer.TextCommandPrefix = '!'
	tokenizer.EmoteCache = make(map[string]Emote)

	runtimeState.mu.Lock()
	prev := runtimeState.current
	runtimeState.current = runtimeconfig.Merge(runtimeState.current, runtimeconfig.Config{
		TwitchChannel: "rocketleague",
	})
	runtimeState.mu.Unlock()
	t.Cleanup(func() {
		runtimeState.mu.Lock()
		runtimeState.current = prev
		runtimeState.mu.Unlock()
	})

	subscribersMu.Lock()
	subscribers = nil
	subscribersMu.Unlock()

	ch := addSubscriber()
	defer removeSubscriber(ch)

	BroadcastFromTailer(storage.Message{
		Username: "SampleUser",
		Platform: "Twitch",
		RawJSON:  `{"author":"SampleUser","message":"hi"}`,
	})

	select {
	case payload := <-ch:
		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("failed to unmarshal broadcast payload: %v", err)
		}

		if msg.Colour == "" {
			t.Fatalf("expected colour to be populated")
		}
		if msg.Emotes == nil {
			t.Fatalf("expected emotes slice to be initialized")
		}
		if msg.Badges == nil {
			t.Fatalf("expected badges slice to be initialized")
		}
		if msg.Tokens == nil {
			t.Fatalf("expected tokens slice to be initialized")
		}
		if msg.SourceChannel != "rocketleague" {
			t.Fatalf("expected source_channel to default to runtime twitch channel, got %q", msg.SourceChannel)
		}
	default:
		t.Fatalf("expected broadcast payload but channel was empty")
	}
}

func TestBroadcastFromTailerConvertsLegacyBadges(t *testing.T) {
	tokenizer.TextEffectSep = ':'
	tokenizer.TextCommandPrefix = '!'
	tokenizer.EmoteCache = make(map[string]Emote)

	subscribersMu.Lock()
	subscribers = nil
	subscribersMu.Unlock()

	ch := addSubscriber()
	defer removeSubscriber(ch)

	BroadcastFromTailer(storage.Message{
		Username:   "BadgeUser",
		Platform:   "Twitch",
		Text:       "hello",
		BadgesJSON: ` [ "subscriber/12" , "premium/1" , "subscriber/17" ] `,
	})

	select {
	case payload := <-ch:
		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("failed to unmarshal broadcast payload: %v", err)
		}

		if len(msg.Badges) != 3 {
			t.Fatalf("expected 3 badges, got %d", len(msg.Badges))
		}
		if msg.Badges[0].ID != "subscriber" || msg.Badges[0].Platform != "twitch" || msg.Badges[0].Version != "12" {
			t.Fatalf("unexpected first badge: %#v", msg.Badges[0])
		}
		if msg.Badges[1].ID != "premium" || msg.Badges[1].Platform != "twitch" || msg.Badges[1].Version != "1" {
			t.Fatalf("unexpected second badge: %#v", msg.Badges[1])
		}
		if msg.Badges[2].ID != "subscriber" || msg.Badges[2].Platform != "twitch" || msg.Badges[2].Version != "17" {
			t.Fatalf("unexpected third badge: %#v", msg.Badges[2])
		}
		if msg.BadgesRaw != nil {
			t.Fatalf("expected badges_raw to be empty")
		}
	default:
		t.Fatalf("expected broadcast payload but channel was empty")
	}
}

func TestBroadcastFromTailerStructuredBadgesAndRaw(t *testing.T) {
	tokenizer.TextEffectSep = ':'
	tokenizer.TextCommandPrefix = '!'
	tokenizer.EmoteCache = make(map[string]Emote)

	subscribersMu.Lock()
	subscribers = nil
	subscribersMu.Unlock()

	ch := addSubscriber()
	defer removeSubscriber(ch)

	BroadcastFromTailer(storage.Message{
		Username:   "BadgeUser",
		Platform:   "Twitch",
		Text:       "hi",
		RawJSON:    `{"author":"BadgeUser","message":"hi","badges":[],"source":"Twitch"}`,
		BadgesJSON: `{"badges":[{"platform":"twitch","id":"subscriber","version":"17"},{"platform":"twitch","id":"premium","version":"1"}],"raw":{"twitch":{"badge_info":"subscriber/17","badges":"subscriber/12,premium/1"}}}`,
	})

	select {
	case payload := <-ch:
		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("failed to unmarshal broadcast payload: %v", err)
		}

		if len(msg.Badges) != 2 {
			t.Fatalf("expected 2 badges, got %d", len(msg.Badges))
		}
		if msg.BadgesRaw == nil {
			t.Fatalf("expected badges_raw to be populated")
		}
		if msg.Badges[0].ID != "subscriber" || msg.Badges[0].Platform != "twitch" || msg.Badges[0].Version != "12" {
			t.Fatalf("unexpected first badge: %#v", msg.Badges[0])
		}
		if msg.Badges[1].ID != "premium" || msg.Badges[1].Platform != "twitch" || msg.Badges[1].Version != "1" {
			t.Fatalf("unexpected second badge: %#v", msg.Badges[1])
		}
	default:
		t.Fatalf("expected broadcast payload but channel was empty")
	}
}

func TestBroadcastFromTailerDropsYouTubeOwnerBadgeWithoutImages(t *testing.T) {
	tokenizer.TextEffectSep = ':'
	tokenizer.TextCommandPrefix = '!'
	tokenizer.EmoteCache = make(map[string]Emote)

	subscribersMu.Lock()
	subscribers = nil
	subscribersMu.Unlock()

	ch := addSubscriber()
	defer removeSubscriber(ch)

	badgesJSON := `{"badges":[{"platform":"youtube","id":"owner"},{"platform":"youtube","id":"moderator","images":[{"url":"https://example.com/mod.png","width":16,"height":16}]}],"raw":{"youtube":{"badges":[{"id":"owner","title":"Owner"}]}}}`

	BroadcastFromTailer(storage.Message{
		Username:   "YTBadgeUser",
		Platform:   "YouTube",
		Text:       "hi",
		BadgesJSON: badgesJSON,
	})

	select {
	case payload := <-ch:
		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("failed to unmarshal broadcast payload: %v", err)
		}

		if msg.BadgesRaw == nil {
			t.Fatalf("expected badges_raw to be populated")
		}
		if len(msg.Badges) != 1 {
			t.Fatalf("expected 1 badge, got %d", len(msg.Badges))
		}
		if msg.Badges[0].ID != "moderator" || msg.Badges[0].Platform != "youtube" {
			t.Fatalf("unexpected remaining badge: %#v", msg.Badges[0])
		}
	default:
		t.Fatalf("expected broadcast payload but channel was empty")
	}
}

func TestBroadcastFromTailerYouTubeEmoteFragments(t *testing.T) {
	tokenizer.TextEffectSep = ':'
	tokenizer.TextCommandPrefix = '!'
	tokenizer.EmoteCache = make(map[string]Emote)

	subscribersMu.Lock()
	subscribers = nil
	subscribersMu.Unlock()

	ch := addSubscriber()
	defer removeSubscriber(ch)

	emotesJSON := `[
		{"id":"a","name":":a:","locations":["0-2"],"images":[{"url":"https://example.com/a.png","width":24,"height":24}]},
		{"id":"b","name":":b:","locations":["3-5"],"images":[{"url":"https://example.com/b.png","width":24,"height":24}]}
	]`

	BroadcastFromTailer(storage.Message{
		Username:   "YTUser",
		Platform:   "YouTube",
		Text:       ":a::b:",
		EmotesJSON: emotesJSON,
	})

	select {
	case payload := <-ch:
		sanitized, err := sanitizeMessagePayload(payload)
		if err != nil {
			t.Fatalf("sanitizeMessagePayload returned error: %v", err)
		}
		var msg Message
		if err := json.Unmarshal(sanitized, &msg); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}

		emoteCount := 0
		for _, token := range msg.Tokens {
			if token.Type == TokenTypeEmote {
				emoteCount++
				if len(token.Emote.Images) == 0 || token.Emote.Images[0].URL == "" {
					t.Fatalf("expected emote images to be populated, got %#v", token.Emote.Images)
				}
			}
		}
		if emoteCount != 2 {
			t.Fatalf("expected 2 emote fragments, got %d", emoteCount)
		}
	default:
		t.Fatalf("expected broadcast payload but channel was empty")
	}
}

func TestBroadcastFromTailerTwitchFirstPartyRepeatedEmoteFragments(t *testing.T) {
	tokenizer.TextEffectSep = ':'
	tokenizer.TextCommandPrefix = '!'
	tokenizer.EmoteCache = make(map[string]Emote)

	subscribersMu.Lock()
	subscribers = nil
	subscribersMu.Unlock()

	ch := addSubscriber()
	defer removeSubscriber(ch)

	BroadcastFromTailer(storage.Message{
		Username:   "TWUser",
		Platform:   "Twitch",
		Text:       "Kappa Kappa",
		EmotesJSON: `["25:0-4,6-10"]`,
	})

	select {
	case payload := <-ch:
		sanitized, err := sanitizeMessagePayload(payload)
		if err != nil {
			t.Fatalf("sanitizeMessagePayload returned error: %v", err)
		}

		var msg Message
		if err := json.Unmarshal(sanitized, &msg); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}

		emoteCount := 0
		for _, token := range msg.Tokens {
			if token.Type == TokenTypeEmote {
				emoteCount++
				if token.Emote.ID != "25" {
					t.Fatalf("expected emote id 25, got %q", token.Emote.ID)
				}
				if len(token.Emote.Images) == 0 || token.Emote.Images[0].URL == "" {
					t.Fatalf("expected emote images to be populated, got %#v", token.Emote.Images)
				}
			}
		}
		if emoteCount != 2 {
			t.Fatalf("expected 2 emote fragments, got %d", emoteCount)
		}
	default:
		t.Fatalf("expected broadcast payload but channel was empty")
	}
}

func TestMessagePayloadFromStorageIncludesTwitchSourceChannel(t *testing.T) {
	payload, err := messagePayloadFromStorage(storage.Message{
		Username: "tester",
		Text:     "hello",
		Platform: "Twitch",
		RawJSON:  `{"channel":"dagnel"}`,
	})
	if err != nil {
		t.Fatalf("messagePayloadFromStorage returned error: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if msg.SourceChannel != "dagnel" {
		t.Fatalf("expected source_channel=dagnel, got %q", msg.SourceChannel)
	}
}

func TestMessagePayloadFromStorageIncludesCanonicalYouTubeSourceURL(t *testing.T) {
	payload, err := messagePayloadFromStorage(storage.Message{
		Username: "tester",
		Text:     "hello",
		Platform: "YouTube",
		RawJSON:  `{"video_id":"abcdefghijk"}`,
	})
	if err != nil {
		t.Fatalf("messagePayloadFromStorage returned error: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if msg.SourceURL != "https://www.youtube.com/watch?v=abcdefghijk" {
		t.Fatalf("expected canonical source_url, got %q", msg.SourceURL)
	}
}

func TestComputeUsernameColorTwitchExtractionAndFallback(t *testing.T) {
	row := storage.Message{
		Username: "tw-user",
		Platform: "twitch",
		RawJSON:  `{"tags":{"color":"#33CC66"}}`,
	}
	msg := Message{Author: "tw-user", Source: "twitch"}
	if got := computeUsernameColor(msg, row); got != "#33CC66" {
		t.Fatalf("expected twitch color extraction, got %q", got)
	}

	row.RawJSON = `{"tags":{"color":""}}`
	if got := computeUsernameColor(msg, row); got != sanitizeUsernameColorForDarkBG(colorFromName("tw-user")) {
		t.Fatalf("expected fallback color for empty twitch color, got %q", got)
	}

	row.RawJSON = `{"foo":"bar"}`
	if got := computeUsernameColor(msg, row); got != sanitizeUsernameColorForDarkBG(colorFromName("tw-user")) {
		t.Fatalf("expected fallback color for missing twitch color, got %q", got)
	}
}

func TestComputeUsernameColorYouTubeRoleOverridesFallback(t *testing.T) {
	base := Message{Author: "yt-role", Source: "youtube"}

	member := storage.Message{
		Username: "yt-role",
		Platform: "youtube",
		RawJSON:  `{"isChatSponsor":true}`,
	}
	if got := computeUsernameColor(base, member); got != youtubeMemberColour {
		t.Fatalf("expected youtube member color %q, got %q", youtubeMemberColour, got)
	}

	mod := storage.Message{
		Username: "yt-role",
		Platform: "youtube",
		RawJSON:  `{"isChatModerator":true}`,
	}
	if got := computeUsernameColor(base, mod); got != youtubeModeratorColour {
		t.Fatalf("expected youtube moderator color %q, got %q", youtubeModeratorColour, got)
	}

	owner := storage.Message{
		Username: "yt-role",
		Platform: "youtube",
		RawJSON:  `{"author":{"isChatOwner":true}}`,
	}
	if got := computeUsernameColor(base, owner); got != youtubeOwnerColour {
		t.Fatalf("expected youtube owner color %q, got %q", youtubeOwnerColour, got)
	}
}

func TestComputeUsernameColorYouTubeRoleOverridesAnyExtractedColor(t *testing.T) {
	userColorMap["yt-override"] = "#112233"
	t.Cleanup(func() { delete(userColorMap, "yt-override") })

	msg := Message{
		Author:        "yt-override",
		Source:        "youtube",
		UsernameColor: "#33CC66",
		Colour:        "#AA00AA",
		Badges: []Badge{
			{ID: "member"},
			{ID: "moderator"},
		},
	}
	row := storage.Message{
		Username: "yt-override",
		Platform: "youtube",
		RawJSON:  `{"isChatOwner":true,"isChatModerator":true,"isChatSponsor":true,"author":{"isChatModerator":true}}`,
	}

	if got := computeUsernameColor(msg, row); got != youtubeOwnerColour {
		t.Fatalf("expected owner precedence color %q, got %q", youtubeOwnerColour, got)
	}
}

func TestComputeUsernameColorInvalidTwitchColorFallsBack(t *testing.T) {
	row := storage.Message{
		Username: "tw-invalid",
		Platform: "twitch",
		RawJSON:  `{"color":"#12GGFF"}`,
	}
	msg := Message{Author: "tw-invalid", Source: "twitch"}
	want := sanitizeUsernameColorForDarkBG(colorFromName("tw-invalid"))
	if got := computeUsernameColor(msg, row); got != want {
		t.Fatalf("expected fallback color %q for invalid twitch color, got %q", want, got)
	}
}

func TestComputeUsernameColorDarkTwitchColorSanitized(t *testing.T) {
	row := storage.Message{
		Username: "tw-dark",
		Platform: "twitch",
		RawJSON:  `{"tags":{"color":"#000000"}}`,
	}
	msg := Message{Author: "tw-dark", Source: "twitch"}
	got := computeUsernameColor(msg, row)

	if got == "#000000" {
		t.Fatalf("expected dark twitch color to be sanitized, got %q", got)
	}
	if !hexUsernameColourRe.MatchString(got) {
		t.Fatalf("expected sanitized color to be valid hex, got %q", got)
	}
	r, g, b, ok := parseHexRGB(got)
	if !ok {
		t.Fatalf("expected parseHexRGB to parse %q", got)
	}
	if usernameColourRelativeLuminance(r, g, b) < usernameColourDarkBGMinLuminance {
		t.Fatalf("expected sanitized color %q to be readable on dark bg", got)
	}
	if strings.ToUpper(got) != got {
		t.Fatalf("expected sanitized color to be uppercase hex, got %q", got)
	}
}
