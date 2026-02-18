package routes

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

type sourceRoundTripFunc func(*http.Request) (*http.Response, error)

func (f sourceRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func resetSourceCaches() {
	twitchTopCache.mu.Lock()
	twitchTopCache.expires = time.Time{}
	twitchTopCache.items = nil
	twitchTopCache.mu.Unlock()

	twitchAppTokenCache.mu.Lock()
	twitchAppTokenCache.token = ""
	twitchAppTokenCache.expires = time.Time{}
	twitchAppTokenCache.mu.Unlock()
}

func TestTopTwitchSourcesEndpoint(t *testing.T) {
	resetSourceCaches()
	t.Setenv("TWITCH_OAUTH_CLIENT_ID", "cid")
	t.Setenv("TWITCH_OAUTH_CLIENT_SECRET", "secret")

	previousClient := sourceHTTPClient
	sourceHTTPClient = &http.Client{Transport: sourceRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(r.URL.Host, "id.twitch.tv"):
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"access_token":"app-token","expires_in":3600}`)), Header: make(http.Header)}, nil
		case strings.Contains(r.URL.Host, "api.twitch.tv"):
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"user_login":"lirik","user_name":"LIRIK","viewer_count":12345}]}`)), Header: make(http.Header)}, nil
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
			return nil, nil
		}
	})}
	t.Cleanup(func() { sourceHTTPClient = previousClient })

	router := mux.NewRouter()
	SetupSourceRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/sources/top/twitch", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, `"login":"lirik"`) || !strings.Contains(body, `"viewer_count":12345`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestTopYouTubeSourcesEndpoint(t *testing.T) {
	resetSourceCaches()
	t.Setenv("GNASTY_YT_SUGGESTIONS_JSON", "")

	router := mux.NewRouter()
	SetupSourceRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/sources/top/youtube", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, `"display_name":"Lofi Girl"`) || !strings.Contains(body, `"url":"https://www.youtube.com/@LofiGirl/live"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestTopYouTubeSourcesEndpointUsesEnvSuggestions(t *testing.T) {
	resetSourceCaches()
	t.Setenv("GNASTY_YT_SUGGESTIONS_JSON", `[{"display_name":"My Custom Stream","url":"https://www.youtube.com/watch?v=abcdefghijk"}]`)

	router := mux.NewRouter()
	SetupSourceRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/sources/top/youtube", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, `"display_name":"My Custom Stream"`) || !strings.Contains(body, `"url":"https://www.youtube.com/watch?v=abcdefghijk"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}
