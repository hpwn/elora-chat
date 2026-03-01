package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

type twitchTopSource struct {
	Login       string `json:"login"`
	DisplayName string `json:"display_name"`
	ViewerCount int    `json:"viewer_count"`
	URL         string `json:"url"`
}

type youtubeTopSource struct {
	DisplayName string `json:"display_name"`
	URL         string `json:"url"`
}

const sourcesTopCacheTTL = 30 * time.Second

var sourceHTTPClient = &http.Client{Timeout: 8 * time.Second}

var twitchTopCache = struct {
	mu      sync.Mutex
	expires time.Time
	items   []twitchTopSource
}{}

var twitchAppTokenCache = struct {
	mu      sync.Mutex
	token   string
	expires time.Time
}{}

func SetupSourceRoutes(r *mux.Router) {
	r.HandleFunc("/api/sources/top/twitch", handleTopTwitchSources).Methods(http.MethodGet)
	r.HandleFunc("/api/sources/top/youtube", handleTopYouTubeSources).Methods(http.MethodGet)
}

func handleTopTwitchSources(w http.ResponseWriter, r *http.Request) {
	if cached, ok := loadCachedTwitchTop(); ok {
		writeJSON(w, cached)
		return
	}

	items, err := fetchTopTwitch(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	storeCachedTwitchTop(items)
	writeJSON(w, items)
}

func handleTopYouTubeSources(w http.ResponseWriter, r *http.Request) {
	items, hint := youtubeSuggestions()
	if len(items) == 0 && hint != "" {
		w.Header().Set("X-Suggestions-Hint", hint)
	}
	writeJSON(w, items)
}

func fetchTopTwitch(ctx context.Context) ([]twitchTopSource, error) {
	clientID := strings.TrimSpace(os.Getenv("TWITCH_OAUTH_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("TWITCH_OAUTH_CLIENT_SECRET"))
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("missing twitch oauth client credentials")
	}

	token, err := getTwitchAppToken(ctx, clientID, clientSecret)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.twitch.tv/helix/streams?first=10", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build twitch request: %w", err)
	}
	request.Header.Set("Client-Id", clientID)
	request.Header.Set("Authorization", "Bearer "+token)

	response, err := sourceHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("twitch api request failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("twitch api returned %s", response.Status)
	}

	var payload struct {
		Data []struct {
			UserLogin   string `json:"user_login"`
			UserName    string `json:"user_name"`
			ViewerCount int    `json:"viewer_count"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("failed to decode twitch response: %w", err)
	}

	out := make([]twitchTopSource, 0, len(payload.Data))
	for _, stream := range payload.Data {
		login := strings.TrimSpace(stream.UserLogin)
		if login == "" {
			continue
		}
		displayName := strings.TrimSpace(stream.UserName)
		if displayName == "" {
			displayName = login
		}

		out = append(out, twitchTopSource{
			Login:       login,
			DisplayName: displayName,
			ViewerCount: stream.ViewerCount,
			URL:         "https://www.twitch.tv/" + login,
		})
	}
	return out, nil
}

func getTwitchAppToken(ctx context.Context, clientID, clientSecret string) (string, error) {
	twitchAppTokenCache.mu.Lock()
	if twitchAppTokenCache.token != "" && time.Now().Before(twitchAppTokenCache.expires.Add(-1*time.Minute)) {
		token := twitchAppTokenCache.token
		twitchAppTokenCache.mu.Unlock()
		return token, nil
	}
	twitchAppTokenCache.mu.Unlock()

	values := url.Values{}
	values.Set("client_id", clientID)
	values.Set("client_secret", clientSecret)
	values.Set("grant_type", "client_credentials")

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://id.twitch.tv/oauth2/token", strings.NewReader(values.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to build twitch token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := sourceHTTPClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("twitch token request failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= 400 {
		return "", fmt.Errorf("twitch token endpoint returned %s", response.Status)
	}

	var payload struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("failed to decode twitch token response: %w", err)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return "", fmt.Errorf("twitch token response missing access token")
	}

	expiresIn := payload.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}

	twitchAppTokenCache.mu.Lock()
	twitchAppTokenCache.token = payload.AccessToken
	twitchAppTokenCache.expires = time.Now().Add(time.Duration(expiresIn) * time.Second)
	token := twitchAppTokenCache.token
	twitchAppTokenCache.mu.Unlock()
	return token, nil
}

func youtubeSuggestions() ([]youtubeTopSource, string) {
	env := strings.TrimSpace(os.Getenv("GNASTY_YT_SUGGESTIONS_JSON"))
	if env != "" {
		type suggestionJSON struct {
			DisplayName string `json:"display_name"`
			URL         string `json:"url"`
		}
		var raw []suggestionJSON
		if err := json.Unmarshal([]byte(env), &raw); err == nil {
			out := make([]youtubeTopSource, 0, len(raw))
			for _, item := range raw {
				display := strings.TrimSpace(item.DisplayName)
				url := strings.TrimSpace(item.URL)
				if display == "" || url == "" {
					continue
				}
				out = append(out, youtubeTopSource{DisplayName: display, URL: url})
			}
			return out, "using GNASTY_YT_SUGGESTIONS_JSON"
		}
	}

	return []youtubeTopSource{
		{DisplayName: "Lofi Girl", URL: "https://www.youtube.com/@LofiGirl/live"},
		{DisplayName: "Chillhop Radio", URL: "https://www.youtube.com/@ChillhopMusic/live"},
		{DisplayName: "STEEZY Coffee Shop Radio", URL: "https://www.youtube.com/@STEEZYASFUCK/live"},
		{DisplayName: "Square Enix Lofi", URL: "https://www.youtube.com/@squareenixmusicchannel/live"},
	}, "using built-in defaults"
}

func loadCachedTwitchTop() ([]twitchTopSource, bool) {
	twitchTopCache.mu.Lock()
	defer twitchTopCache.mu.Unlock()
	if time.Now().After(twitchTopCache.expires) || len(twitchTopCache.items) == 0 {
		return nil, false
	}
	return append([]twitchTopSource(nil), twitchTopCache.items...), true
}

func storeCachedTwitchTop(items []twitchTopSource) {
	twitchTopCache.mu.Lock()
	defer twitchTopCache.mu.Unlock()
	twitchTopCache.items = append([]twitchTopSource(nil), items...)
	twitchTopCache.expires = time.Now().Add(sourcesTopCacheTTL)
}

func writeJSON(w http.ResponseWriter, payload any) {
	writeJSONStatus(w, http.StatusOK, payload)
}

func writeJSONStatus(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
