package routes

import (
	"context"
	"database/sql"
	"io"
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
	if body := rr.Body.String(); !strings.Contains(body, "Connected") {
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
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

func (s *stubStore) Close(context.Context) error { return nil }
