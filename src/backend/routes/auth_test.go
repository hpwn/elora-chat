package routes

import (
	"context"
	"database/sql"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
	"golang.org/x/oauth2"
)

func TestTwitchStartRedirect(t *testing.T) {
	prevStore := chatStore
	chatStore = newStubStore()
	t.Cleanup(func() {
		chatStore = prevStore
	})

	prevConfig := twitchOAuthConfig
	t.Cleanup(func() {
		twitchOAuthConfig = prevConfig
	})

	t.Setenv("TWITCH_OAUTH_CLIENT_ID", "test-client")
	t.Setenv("TWITCH_OAUTH_CLIENT_SECRET", "test-secret")
	t.Setenv("TWITCH_OAUTH_REDIRECT_URL", "https://example.com/callback")

	twitchOAuthConfig = newTwitchOAuthConfigFromEnv()

	router := mux.NewRouter()
	SetupAuthRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/auth/twitch/start", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusTemporaryRedirect {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusTemporaryRedirect)
	}

	loc := rr.Result().Header.Get("Location")
	if loc == "" {
		t.Fatalf("missing Location header")
	}

	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("failed to parse redirect location: %v", err)
	}

	values := parsed.Query()

	if got := values.Get("client_id"); got != "test-client" {
		t.Fatalf("unexpected client_id: got %q want %q", got, "test-client")
	}

	if got := values.Get("redirect_uri"); got != "https://example.com/callback" {
		t.Fatalf("unexpected redirect_uri: got %q want %q", got, "https://example.com/callback")
	}

	if got := values.Get("access_type"); got != "offline" {
		t.Fatalf("unexpected access_type: got %q want %q", got, "offline")
	}

	scope := values.Get("scope")
	parts := strings.Fields(scope)
	wantScopes := map[string]bool{"chat:read": false, "chat:edit": false}
	for _, p := range parts {
		if _, ok := wantScopes[p]; ok {
			wantScopes[p] = true
		}
	}
	for scopeName, present := range wantScopes {
		if !present {
			t.Fatalf("missing scope %q in %q", scopeName, scope)
		}
	}
}

func TestTwitchCallbackWritesGnastyTokens(t *testing.T) {
	prevStore := chatStore
	store := newStubStore()
	chatStore = store
	t.Cleanup(func() {
		chatStore = prevStore
	})

	state := "abc123"
	if err := storeOAuthState(context.Background(), state); err != nil {
		t.Fatalf("storeOAuthState: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"access_token":"abc123","refresh_token":"ref456","expires_in":3600,"token_type":"bearer"}`)
		case "/helix/users":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"data":[{"id":"1"}]}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	prevConfig := twitchOAuthConfig
	twitchOAuthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "https://example.com/callback",
		Endpoint: oauth2.Endpoint{
			AuthURL:  server.URL + "/oauth2/authorize",
			TokenURL: server.URL + "/oauth2/token",
		},
	}
	t.Cleanup(func() { twitchOAuthConfig = prevConfig })

	prevUserInfo := twitchUserInfoURL
	twitchUserInfoURL = server.URL + "/helix/users"
	t.Cleanup(func() { twitchUserInfoURL = prevUserInfo })

	prevHTTPClient := twitchHTTPClient
	twitchHTTPClient = server.Client()
	t.Cleanup(func() { twitchHTTPClient = prevHTTPClient })

	calls := 0
	prevGnastyClient := gnastyHTTPClient
	gnastyHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.String() != "http://gnasty:8765/admin/twitch/reload" {
			t.Fatalf("unexpected gnasty url: %s", req.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})}
	t.Cleanup(func() { gnastyHTTPClient = prevGnastyClient })

	dataDir := t.TempDir()
	t.Setenv("ELORA_DATA_DIR", dataDir)
	t.Setenv("ELORA_TWITCH_WRITE_GNASTY_TOKENS", "1")

	router := mux.NewRouter()
	SetupAuthRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/callback/twitch?state="+state+"&code=code123", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusOK)
	}
	if body := rr.Body.String(); !strings.Contains(body, "redirecting back to Elora") || !strings.Contains(body, "window.location.href") {
		t.Fatalf("unexpected body: %q", body)
	}

	accessPath := filepath.Join(dataDir, "twitch_irc.pass")
	refreshPath := filepath.Join(dataDir, "twitch_refresh.pass")

	if data, err := os.ReadFile(accessPath); err != nil {
		t.Fatalf("access token read: %v", err)
	} else if string(data) != "oauth:abc123\n" {
		t.Fatalf("access content = %q", string(data))
	}

	if data, err := os.ReadFile(refreshPath); err != nil {
		t.Fatalf("refresh token read: %v", err)
	} else if string(data) != "ref456\n" {
		t.Fatalf("refresh content = %q", string(data))
	}

	if calls != 1 {
		t.Fatalf("expected 1 gnasty call, got %d", calls)
	}

	svc, err := store.GetSession(context.Background(), twitchServiceTokenKey)
	if err != nil {
		t.Fatalf("service token record missing: %v", err)
	}
	parsedSvc, err := parseServiceToken(svc.DataJSON)
	if err != nil {
		t.Fatalf("parseServiceToken: %v", err)
	}
	if parsedSvc.AccessToken != "abc123" {
		t.Fatalf("service access token = %q, want %q", parsedSvc.AccessToken, "abc123")
	}
	if parsedSvc.RefreshToken != "ref456" {
		t.Fatalf("service refresh token = %q, want %q", parsedSvc.RefreshToken, "ref456")
	}
}

func TestTwitchCallbackHandlesExchangeFailure(t *testing.T) {
	prevStore := chatStore
	store := newStubStore()
	chatStore = store
	t.Cleanup(func() { chatStore = prevStore })

	state := "fail"
	if err := storeOAuthState(context.Background(), state); err != nil {
		t.Fatalf("storeOAuthState: %v", err)
	}

	userInfoCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			w.WriteHeader(http.StatusBadRequest)
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"error":"invalid_grant"}`)
		case "/helix/users":
			userInfoCalled = true
			t.Fatalf("user info should not be requested on exchange failure")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	prevConfig := twitchOAuthConfig
	twitchOAuthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "https://example.com/callback",
		Endpoint: oauth2.Endpoint{
			AuthURL:  server.URL + "/oauth2/authorize",
			TokenURL: server.URL + "/oauth2/token",
		},
	}
	t.Cleanup(func() { twitchOAuthConfig = prevConfig })

	prevUserInfo := twitchUserInfoURL
	twitchUserInfoURL = server.URL + "/helix/users"
	t.Cleanup(func() { twitchUserInfoURL = prevUserInfo })

	prevHTTPClient := twitchHTTPClient
	twitchHTTPClient = server.Client()
	t.Cleanup(func() { twitchHTTPClient = prevHTTPClient })

	prevGnastyClient := gnastyHTTPClient
	gnastyHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("gnasty reload should not be triggered")
		return nil, nil
	})}
	t.Cleanup(func() { gnastyHTTPClient = prevGnastyClient })

	dataDir := t.TempDir()
	t.Setenv("ELORA_DATA_DIR", dataDir)
	t.Setenv("ELORA_TWITCH_WRITE_GNASTY_TOKENS", "1")

	router := mux.NewRouter()
	SetupAuthRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/callback/twitch?state="+state+"&code=bad", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusOK)
	}
	if body := rr.Body.String(); !strings.Contains(body, "redirecting back to Elora") || !strings.Contains(body, "window.location.href") {
		t.Fatalf("unexpected body: %q", body)
	}

	if userInfoCalled {
		t.Fatalf("user info should not be fetched on exchange failure")
	}

	if _, err := os.Stat(filepath.Join(dataDir, "twitch_irc.pass")); !os.IsNotExist(err) {
		t.Fatalf("access token file should not exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "twitch_refresh.pass")); !os.IsNotExist(err) {
		t.Fatalf("refresh token file should not exist: %v", err)
	}
}

func TestTwitchCallbackReloadsWhenURLSetOnExchangeFailure(t *testing.T) {
	prevStore := chatStore
	store := newStubStore()
	chatStore = store
	t.Cleanup(func() { chatStore = prevStore })

	state := "reload"
	if err := storeOAuthState(context.Background(), state); err != nil {
		t.Fatalf("storeOAuthState: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			w.WriteHeader(http.StatusBadRequest)
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"error":"invalid_grant"}`)
		case "/helix/users":
			t.Fatalf("user info should not be requested on exchange failure")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	prevConfig := twitchOAuthConfig
	twitchOAuthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "https://example.com/callback",
		Endpoint: oauth2.Endpoint{
			AuthURL:  server.URL + "/oauth2/authorize",
			TokenURL: server.URL + "/oauth2/token",
		},
	}
	t.Cleanup(func() { twitchOAuthConfig = prevConfig })

	prevUserInfo := twitchUserInfoURL
	twitchUserInfoURL = server.URL + "/helix/users"
	t.Cleanup(func() { twitchUserInfoURL = prevUserInfo })

	prevHTTPClient := twitchHTTPClient
	twitchHTTPClient = server.Client()
	t.Cleanup(func() { twitchHTTPClient = prevHTTPClient })

	reloadURL := "http://example.com/reload"
	calls := 0
	prevGnastyClient := gnastyHTTPClient
	gnastyHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.String() != reloadURL {
			t.Fatalf("unexpected gnasty url: %s", req.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})}
	t.Cleanup(func() { gnastyHTTPClient = prevGnastyClient })

	dataDir := t.TempDir()
	t.Setenv("ELORA_DATA_DIR", dataDir)
	t.Setenv("ELORA_TWITCH_WRITE_GNASTY_TOKENS", "1")
	t.Setenv("ELORA_GNASTY_RELOAD_URL", reloadURL)

	router := mux.NewRouter()
	SetupAuthRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/callback/twitch?state="+state+"&code=bad", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusOK)
	}
	if body := rr.Body.String(); !strings.Contains(body, "redirecting back to Elora") || !strings.Contains(body, "window.location.href") {
		t.Fatalf("unexpected body: %q", body)
	}

	if calls != 1 {
		t.Fatalf("expected 1 gnasty call, got %d", calls)
	}
}

func TestTwitchCallbackReloadDialErrorDoesNotFail(t *testing.T) {
	prevStore := chatStore
	store := newStubStore()
	chatStore = store
	t.Cleanup(func() { chatStore = prevStore })

	state := "dialerror"
	if err := storeOAuthState(context.Background(), state); err != nil {
		t.Fatalf("storeOAuthState: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"access_token":"abc","refresh_token":"ref","expires_in":3600,"token_type":"bearer"}`)
		case "/helix/users":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"data":[{"id":"1"}]}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	prevConfig := twitchOAuthConfig
	twitchOAuthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "https://example.com/callback",
		Endpoint: oauth2.Endpoint{
			AuthURL:  server.URL + "/oauth2/authorize",
			TokenURL: server.URL + "/oauth2/token",
		},
	}
	t.Cleanup(func() { twitchOAuthConfig = prevConfig })

	prevUserInfo := twitchUserInfoURL
	twitchUserInfoURL = server.URL + "/helix/users"
	t.Cleanup(func() { twitchUserInfoURL = prevUserInfo })

	prevHTTPClient := twitchHTTPClient
	twitchHTTPClient = server.Client()
	t.Cleanup(func() { twitchHTTPClient = prevHTTPClient })

	prevGnastyClient := gnastyHTTPClient
	gnastyHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, &net.DNSError{Err: "no such host", Name: "gnasty", IsNotFound: true}
	})}
	t.Cleanup(func() { gnastyHTTPClient = prevGnastyClient })

	dataDir := t.TempDir()
	t.Setenv("ELORA_DATA_DIR", dataDir)
	t.Setenv("ELORA_TWITCH_WRITE_GNASTY_TOKENS", "1")

	router := mux.NewRouter()
	SetupAuthRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/callback/twitch?state="+state+"&code=ok", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusOK)
	}

	if _, err := os.Stat(filepath.Join(dataDir, "twitch_irc.pass")); err != nil {
		t.Fatalf("access token file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "twitch_refresh.pass")); err != nil {
		t.Fatalf("refresh token file: %v", err)
	}
}

func TestTwitchCallbackSkipsGnastyWritesWhenDisabled(t *testing.T) {
	prevStore := chatStore
	store := newStubStore()
	chatStore = store
	t.Cleanup(func() { chatStore = prevStore })

	state := "disabled"
	if err := storeOAuthState(context.Background(), state); err != nil {
		t.Fatalf("storeOAuthState: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"access_token":"abc","refresh_token":"ref","expires_in":3600,"token_type":"bearer"}`)
		case "/helix/users":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"data":[{"id":"1"}]}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	prevConfig := twitchOAuthConfig
	twitchOAuthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "https://example.com/callback",
		Endpoint: oauth2.Endpoint{
			AuthURL:  server.URL + "/oauth2/authorize",
			TokenURL: server.URL + "/oauth2/token",
		},
	}
	t.Cleanup(func() { twitchOAuthConfig = prevConfig })

	prevUserInfo := twitchUserInfoURL
	twitchUserInfoURL = server.URL + "/helix/users"
	t.Cleanup(func() { twitchUserInfoURL = prevUserInfo })

	prevHTTPClient := twitchHTTPClient
	twitchHTTPClient = server.Client()
	t.Cleanup(func() { twitchHTTPClient = prevHTTPClient })

	prevGnastyClient := gnastyHTTPClient
	gnastyHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("gnasty client should not be called")
		return nil, nil
	})}
	t.Cleanup(func() { gnastyHTTPClient = prevGnastyClient })

	dataDir := t.TempDir()
	t.Setenv("ELORA_DATA_DIR", dataDir)
	t.Setenv("ELORA_TWITCH_WRITE_GNASTY_TOKENS", "false")

	router := mux.NewRouter()
	SetupAuthRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/callback/twitch?state="+state+"&code=code", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusOK)
	}

	if _, err := os.Stat(filepath.Join(dataDir, "twitch_irc.pass")); !os.IsNotExist(err) {
		t.Fatalf("access token file should not exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "twitch_refresh.pass")); !os.IsNotExist(err) {
		t.Fatalf("refresh token file should not exist: %v", err)
	}
}

func TestServiceTokenMaintenanceRefreshesWritesChangedFilesAndReloadsOnce(t *testing.T) {
	prevStore := chatStore
	store := newStubStore()
	chatStore = store
	t.Cleanup(func() { chatStore = prevStore })

	prevTokenSource := tokenSourceFromConfig
	tokenSourceFromConfig = func(_ *oauth2.Config, _ context.Context, token *oauth2.Token) oauth2.TokenSource {
		if token.RefreshToken != "stale-refresh" {
			t.Fatalf("unexpected refresh token: %q", token.RefreshToken)
		}
		return tokenSourceFunc(func() (*oauth2.Token, error) {
			return &oauth2.Token{
				AccessToken:  "new-access",
				RefreshToken: "stale-refresh",
				Expiry:       time.Now().UTC().Add(2 * time.Hour),
			}, nil
		})
	}
	t.Cleanup(func() { tokenSourceFromConfig = prevTokenSource })

	prevWriteAccess := writeAccessTokenFile
	prevWriteRefresh := writeRefreshTokenFile
	accessWrites := 0
	refreshWrites := 0
	writeAccessTokenFile = func(path, token string) error {
		accessWrites++
		return os.WriteFile(path, []byte("oauth:"+token+"\n"), 0o600)
	}
	writeRefreshTokenFile = func(path, token string) error {
		refreshWrites++
		return os.WriteFile(path, []byte(token+"\n"), 0o600)
	}
	t.Cleanup(func() {
		writeAccessTokenFile = prevWriteAccess
		writeRefreshTokenFile = prevWriteRefresh
	})

	reloadCalls := 0
	prevGnastyClient := gnastyHTTPClient
	gnastyHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		reloadCalls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})}
	t.Cleanup(func() { gnastyHTTPClient = prevGnastyClient })

	dataDir := t.TempDir()
	t.Setenv("ELORA_DATA_DIR", dataDir)
	t.Setenv("ELORA_TWITCH_WRITE_GNASTY_TOKENS", "1")

	if err := upsertServiceTokenRecord(context.Background(), &oauth2.Token{
		AccessToken:  "old-access",
		RefreshToken: "stale-refresh",
		Expiry:       time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("upsertServiceTokenRecord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "twitch_refresh.pass"), []byte("stale-refresh\n"), 0o600); err != nil {
		t.Fatalf("seed refresh file: %v", err)
	}

	if err := maintainTwitchServiceToken(context.Background()); err != nil {
		t.Fatalf("maintainTwitchServiceToken: %v", err)
	}

	accessPath := filepath.Join(dataDir, "twitch_irc.pass")
	refreshPath := filepath.Join(dataDir, "twitch_refresh.pass")

	if data, err := os.ReadFile(accessPath); err != nil {
		t.Fatalf("access token read: %v", err)
	} else if string(data) != "oauth:new-access\n" {
		t.Fatalf("access content = %q", string(data))
	}

	if data, err := os.ReadFile(refreshPath); err != nil {
		t.Fatalf("refresh token read: %v", err)
	} else if string(data) != "stale-refresh\n" {
		t.Fatalf("refresh content = %q", string(data))
	}
	if accessWrites != 1 {
		t.Fatalf("access writes = %d, want 1", accessWrites)
	}
	if refreshWrites != 0 {
		t.Fatalf("refresh writes = %d, want 0", refreshWrites)
	}
	if reloadCalls != 1 {
		t.Fatalf("reload calls = %d, want 1", reloadCalls)
	}

	svc, err := store.GetSession(context.Background(), twitchServiceTokenKey)
	if err != nil {
		t.Fatalf("service token record missing: %v", err)
	}
	token, err := parseServiceToken(svc.DataJSON)
	if err != nil {
		t.Fatalf("parseServiceToken: %v", err)
	}
	if token.AccessToken != "new-access" {
		t.Fatalf("service access token = %q, want %q", token.AccessToken, "new-access")
	}
	if token.RefreshToken != "stale-refresh" {
		t.Fatalf("service refresh token = %q, want %q", token.RefreshToken, "stale-refresh")
	}
}

func TestServiceTokenMaintenanceNoopDoesNotRewriteOrReload(t *testing.T) {
	prevStore := chatStore
	store := newStubStore()
	chatStore = store
	t.Cleanup(func() { chatStore = prevStore })

	prevWriteAccess := writeAccessTokenFile
	prevWriteRefresh := writeRefreshTokenFile
	writeAccessTokenFile = func(path, token string) error {
		t.Fatalf("unexpected access write for %s", path)
		return nil
	}
	writeRefreshTokenFile = func(path, token string) error {
		t.Fatalf("unexpected refresh write for %s", path)
		return nil
	}
	t.Cleanup(func() {
		writeAccessTokenFile = prevWriteAccess
		writeRefreshTokenFile = prevWriteRefresh
	})

	reloadCalls := 0
	prevGnastyClient := gnastyHTTPClient
	gnastyHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		reloadCalls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})}
	t.Cleanup(func() { gnastyHTTPClient = prevGnastyClient })

	dataDir := t.TempDir()
	t.Setenv("ELORA_DATA_DIR", dataDir)
	t.Setenv("ELORA_TWITCH_WRITE_GNASTY_TOKENS", "1")

	if err := upsertServiceTokenRecord(context.Background(), &oauth2.Token{
		AccessToken:  "stable-access",
		RefreshToken: "stable-refresh",
		Expiry:       time.Now().UTC().Add(2 * time.Hour),
	}); err != nil {
		t.Fatalf("upsertServiceTokenRecord: %v", err)
	}

	accessPath := filepath.Join(dataDir, "twitch_irc.pass")
	refreshPath := filepath.Join(dataDir, "twitch_refresh.pass")
	if err := os.WriteFile(accessPath, []byte("oauth:stable-access\n"), 0o600); err != nil {
		t.Fatalf("seed access file: %v", err)
	}
	if err := os.WriteFile(refreshPath, []byte("stable-refresh\n"), 0o600); err != nil {
		t.Fatalf("seed refresh file: %v", err)
	}

	beforeAccess, err := os.Stat(accessPath)
	if err != nil {
		t.Fatalf("access stat before: %v", err)
	}
	beforeRefresh, err := os.Stat(refreshPath)
	if err != nil {
		t.Fatalf("refresh stat before: %v", err)
	}

	if err := maintainTwitchServiceToken(context.Background()); err != nil {
		t.Fatalf("maintainTwitchServiceToken: %v", err)
	}

	afterAccess, err := os.Stat(accessPath)
	if err != nil {
		t.Fatalf("access stat after: %v", err)
	}
	afterRefresh, err := os.Stat(refreshPath)
	if err != nil {
		t.Fatalf("refresh stat after: %v", err)
	}

	if !afterAccess.ModTime().Equal(beforeAccess.ModTime()) {
		t.Fatalf("access mtime changed: before=%v after=%v", beforeAccess.ModTime(), afterAccess.ModTime())
	}
	if !afterRefresh.ModTime().Equal(beforeRefresh.ModTime()) {
		t.Fatalf("refresh mtime changed: before=%v after=%v", beforeRefresh.ModTime(), afterRefresh.ModTime())
	}
	if reloadCalls != 0 {
		t.Fatalf("reload calls = %d, want 0", reloadCalls)
	}
}

func TestServiceTokenMaintenanceBootstrapsFromRefreshFile(t *testing.T) {
	prevStore := chatStore
	store := newStubStore()
	chatStore = store
	t.Cleanup(func() { chatStore = prevStore })

	prevTokenSource := tokenSourceFromConfig
	tokenSourceFromConfig = func(_ *oauth2.Config, _ context.Context, token *oauth2.Token) oauth2.TokenSource {
		if token.RefreshToken != "bootstrap-refresh" {
			t.Fatalf("unexpected refresh token: %q", token.RefreshToken)
		}
		return tokenSourceFunc(func() (*oauth2.Token, error) {
			return &oauth2.Token{
				AccessToken:  "boot-access",
				RefreshToken: "boot-refresh-rotated",
				Expiry:       time.Now().UTC().Add(90 * time.Minute),
			}, nil
		})
	}
	t.Cleanup(func() { tokenSourceFromConfig = prevTokenSource })

	prevWriteAccess := writeAccessTokenFile
	prevWriteRefresh := writeRefreshTokenFile
	writeAccessTokenFile = func(path, token string) error {
		return os.WriteFile(path, []byte("oauth:"+token+"\n"), 0o600)
	}
	writeRefreshTokenFile = func(path, token string) error {
		return os.WriteFile(path, []byte(token+"\n"), 0o600)
	}
	t.Cleanup(func() {
		writeAccessTokenFile = prevWriteAccess
		writeRefreshTokenFile = prevWriteRefresh
	})

	prevGnastyClient := gnastyHTTPClient
	gnastyHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})}
	t.Cleanup(func() { gnastyHTTPClient = prevGnastyClient })

	dataDir := t.TempDir()
	t.Setenv("ELORA_DATA_DIR", dataDir)
	t.Setenv("ELORA_TWITCH_WRITE_GNASTY_TOKENS", "1")

	seedRefreshPath := filepath.Join(dataDir, "twitch_refresh.pass")
	if err := os.WriteFile(seedRefreshPath, []byte("bootstrap-refresh\n"), 0o600); err != nil {
		t.Fatalf("seed refresh token: %v", err)
	}

	if err := maintainTwitchServiceToken(context.Background()); err != nil {
		t.Fatalf("maintainTwitchServiceToken: %v", err)
	}

	svc, err := store.GetSession(context.Background(), twitchServiceTokenKey)
	if err != nil {
		t.Fatalf("service token record missing: %v", err)
	}
	token, err := parseServiceToken(svc.DataJSON)
	if err != nil {
		t.Fatalf("parseServiceToken: %v", err)
	}
	if token.AccessToken != "boot-access" {
		t.Fatalf("service access token = %q, want %q", token.AccessToken, "boot-access")
	}
	if token.RefreshToken != "boot-refresh-rotated" {
		t.Fatalf("service refresh token = %q, want %q", token.RefreshToken, "boot-refresh-rotated")
	}

	accessPath := filepath.Join(dataDir, "twitch_irc.pass")
	refreshPath := filepath.Join(dataDir, "twitch_refresh.pass")
	if data, err := os.ReadFile(accessPath); err != nil {
		t.Fatalf("access token read: %v", err)
	} else if string(data) != "oauth:boot-access\n" {
		t.Fatalf("access content = %q", string(data))
	}
	if data, err := os.ReadFile(refreshPath); err != nil {
		t.Fatalf("refresh token read: %v", err)
	} else if string(data) != "boot-refresh-rotated\n" {
		t.Fatalf("refresh content = %q", string(data))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type tokenSourceFunc func() (*oauth2.Token, error)

func (f tokenSourceFunc) Token() (*oauth2.Token, error) {
	return f()
}

type stubStore struct {
	sessions map[string]*storage.Session
}

func newStubStore() *stubStore {
	return &stubStore{sessions: make(map[string]*storage.Session)}
}

func (s *stubStore) Init(context.Context) error { return nil }

func (s *stubStore) Ping(context.Context) error { return nil }

func (s *stubStore) InsertMessage(context.Context, *storage.Message) error { return nil }

func (s *stubStore) GetRecent(context.Context, storage.QueryOpts) ([]storage.Message, error) {
	return nil, nil
}

func (s *stubStore) PurgeBefore(context.Context, time.Time) (int, error) { return 0, nil }

func (s *stubStore) PurgeAll(context.Context) error { return nil }

func (s *stubStore) GetSession(_ context.Context, token string) (*storage.Session, error) {
	sess, ok := s.sessions[token]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return sess, nil
}

func (s *stubStore) UpsertSession(_ context.Context, sess *storage.Session) error {
	if sess == nil {
		return nil
	}
	s.sessions[sess.Token] = sess
	return nil
}

func (s *stubStore) DeleteSession(_ context.Context, token string) error {
	delete(s.sessions, token)
	return nil
}

func (s *stubStore) LatestSessionByService(context.Context, string) (*storage.Session, error) {
	return nil, nil
}

func (s *stubStore) LatestSession(context.Context) (*storage.Session, error) {
	return nil, nil
}

func (s *stubStore) GetConfig(context.Context, string) (*storage.ConfigRecord, error) {
	return nil, nil
}

func (s *stubStore) UpsertConfig(context.Context, *storage.ConfigRecord) error {
	return nil
}

func (s *stubStore) Close(context.Context) error { return nil }
