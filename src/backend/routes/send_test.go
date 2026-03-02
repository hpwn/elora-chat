package routes

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
)

func TestSendMessageHandlerUsesRuntimeTwitchChannelAndSessionNick(t *testing.T) {
	prevStore := chatStore
	store := newStubStore()
	chatStore = store
	t.Cleanup(func() { chatStore = prevStore })

	restoreCfg := setRuntimeTwitchChannelForTest("Jynxzi")
	t.Cleanup(restoreCfg)

	if err := seedSendSession(store, "sess-token", "HP_AZ", "token-123"); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	prevDial := twitchIRCDial
	clientConn, serverConn := net.Pipe()
	twitchIRCDial = func(network, address string) (net.Conn, error) {
		return clientConn, nil
	}
	t.Cleanup(func() { twitchIRCDial = prevDial })

	lineCh := make(chan []string, 1)
	go func() {
		defer serverConn.Close()
		reader := bufio.NewReader(serverConn)
		lines := make([]string, 0, 4)
		for i := 0; i < 4; i++ {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}
			lines = append(lines, strings.TrimSpace(line))
		}
		lineCh <- lines
	}()

	body := bytes.NewBufferString(`{"message":"hello from test"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/send-message", body)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "sess-token"})
	rr := httptest.NewRecorder()

	sendMessageHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusOK)
	}

	select {
	case lines := <-lineCh:
		joined := strings.Join(lines, "\n")
		assertContains(t, joined, "PASS oauth:token-123")
		assertContains(t, joined, "NICK hp_az")
		assertContains(t, joined, "JOIN #jynxzi")
		assertContains(t, joined, "PRIVMSG #jynxzi :hello from test")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for IRC lines")
	}
}

func TestSendMessageHandlerRejectsMissingConfiguredChannel(t *testing.T) {
	prevStore := chatStore
	store := newStubStore()
	chatStore = store
	t.Cleanup(func() { chatStore = prevStore })

	restoreCfg := setRuntimeTwitchChannelForTest("")
	t.Cleanup(restoreCfg)

	if err := seedSendSession(store, "sess-token", "hp_az", "token-123"); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	body := bytes.NewBufferString(`{"message":"hello from test"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/send-message", body)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "sess-token"})
	rr := httptest.NewRecorder()

	sendMessageHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestSendMessageHandlerReturnsUnauthorizedWhenTwitchTokenMissing(t *testing.T) {
	prevStore := chatStore
	store := newStubStore()
	chatStore = store
	t.Cleanup(func() { chatStore = prevStore })

	restoreCfg := setRuntimeTwitchChannelForTest("jynxzi")
	t.Cleanup(restoreCfg)

	if err := seedSendSession(store, "sess-token", "hp_az", ""); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	body := bytes.NewBufferString(`{"message":"hello from test"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/send-message", body)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "sess-token"})
	rr := httptest.NewRecorder()

	sendMessageHandler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestSendMessageHandlerReturnsBadGatewayOnDialFailure(t *testing.T) {
	prevStore := chatStore
	store := newStubStore()
	chatStore = store
	t.Cleanup(func() { chatStore = prevStore })

	restoreCfg := setRuntimeTwitchChannelForTest("jynxzi")
	t.Cleanup(restoreCfg)

	if err := seedSendSession(store, "sess-token", "hp_az", "token-123"); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	prevDial := twitchIRCDial
	twitchIRCDial = func(network, address string) (net.Conn, error) {
		return nil, net.UnknownNetworkError("test dial failure")
	}
	t.Cleanup(func() { twitchIRCDial = prevDial })

	body := bytes.NewBufferString(`{"message":"hello from test"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/send-message", body)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "sess-token"})
	rr := httptest.NewRecorder()

	sendMessageHandler(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusBadGateway)
	}
}

func seedSendSession(store *stubStore, token, login, twitchToken string) error {
	data := map[string]any{
		"data": []any{
			map[string]any{
				"login": login,
			},
		},
	}
	if twitchToken != "" {
		data["twitch_token"] = twitchToken
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}

	return store.UpsertSession(ctx, &storage.Session{
		Token:       token,
		Service:     "twitch",
		DataJSON:    string(raw),
		TokenExpiry: time.Now().UTC().Add(1 * time.Hour),
		UpdatedAt:   time.Now().UTC(),
	})
}

func setRuntimeTwitchChannelForTest(channel string) func() {
	runtimeState.mu.Lock()
	prev := runtimeState.current
	next := runtimeState.current
	next.TwitchChannel = channel
	runtimeState.current = next
	runtimeState.mu.Unlock()

	return func() {
		runtimeState.mu.Lock()
		runtimeState.current = prev
		runtimeState.mu.Unlock()
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected %q in %q", needle, haystack)
	}
}
