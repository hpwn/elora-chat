package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hpwn/EloraChat/src/backend/internal/configreporter"
)

func TestRegisterConfigz(t *testing.T) {
	mux := http.NewServeMux()
	called := false
	snapshot := func() configreporter.Snapshot {
		called = true
		return configreporter.Snapshot{
			Ingest: configreporter.IngestSnapshot{Driver: "chatdownloader"},
			Auth: configreporter.AuthSnapshot{Twitch: configreporter.TwitchAuthSnapshot{
				ClientID:          "[redacted]",
				RedirectURL:       "https://example.com/callback",
				WriteGnastyTokens: true,
				AccessTokenPath:   "/data/twitch_irc.pass",
				RefreshTokenPath:  "/data/twitch_refresh.pass",
			}},
		}
	}
	RegisterConfigz(mux, snapshot)

	req := httptest.NewRequest(http.MethodGet, "/configz", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}
	if !called {
		t.Fatalf("expected snapshot to be invoked")
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("unexpected content-type: %s", ct)
	}

	var payload configreporter.Snapshot
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if payload.Auth.Twitch.ClientID != "[redacted]" {
		t.Fatalf("expected twitch client id to be redacted, got %q", payload.Auth.Twitch.ClientID)
	}
	if payload.Auth.Twitch.RedirectURL != "https://example.com/callback" {
		t.Fatalf("unexpected redirect url: %s", payload.Auth.Twitch.RedirectURL)
	}
	if !payload.Auth.Twitch.WriteGnastyTokens {
		t.Fatalf("expected write gnasty tokens to be true")
	}
	if payload.Auth.Twitch.AccessTokenPath != "/data/twitch_irc.pass" {
		t.Fatalf("unexpected access token path: %s", payload.Auth.Twitch.AccessTokenPath)
	}
	if payload.Auth.Twitch.RefreshTokenPath != "/data/twitch_refresh.pass" {
		t.Fatalf("unexpected refresh token path: %s", payload.Auth.Twitch.RefreshTokenPath)
	}

	// Ensure method not allowed for POST.
	called = false
	req = httptest.NewRequest(http.MethodPost, "/configz", nil)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
	if called {
		t.Fatalf("snapshot should not have been called for POST")
	}
}
