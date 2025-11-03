package routes

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
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

func (s *stubStore) GetSession(context.Context, string) (*storage.Session, error) {
	return nil, sql.ErrNoRows
}

func (s *stubStore) UpsertSession(_ context.Context, sess *storage.Session) error {
	if sess == nil {
		return nil
	}
	s.sessions[sess.Token] = sess
	return nil
}

func (s *stubStore) DeleteSession(context.Context, string) error { return nil }

func (s *stubStore) LatestSessionByService(context.Context, string) (*storage.Session, error) {
	return nil, nil
}

func (s *stubStore) LatestSession(context.Context) (*storage.Session, error) {
	return nil, nil
}

func (s *stubStore) Close(context.Context) error { return nil }
