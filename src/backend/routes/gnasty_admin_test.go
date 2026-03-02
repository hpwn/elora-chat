package routes

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hpwn/EloraChat/src/backend/internal/runtimeconfig"
)

func resetGnastySyncStateForTest() {
	gnastySyncState.mu.Lock()
	defer gnastySyncState.mu.Unlock()
	gnastySyncState.lastAttempt = time.Time{}
	gnastySyncState.lastSuccess = time.Time{}
	gnastySyncState.lastError = ""
	gnastySyncState.targetBase = ""
}

func TestPostGnastyAdminJSONRetriesAndSucceeds(t *testing.T) {
	resetGnastySyncStateForTest()
	prevClient := gnastyHTTPClient
	var calls atomic.Int32
	gnastyHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempt := calls.Add(1)
		if attempt < 3 {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Status:     "500 Internal Server Error",
				Body:       io.NopCloser(strings.NewReader("boom")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(bytes.NewReader([]byte("ok"))),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}
	t.Cleanup(func() { gnastyHTTPClient = prevClient })

	err := postGnastyAdminJSON("http://gnasty.local", "/admin/config", map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestSyncGnastyConfigBestEffortTracksFailure(t *testing.T) {
	resetGnastySyncStateForTest()
	prevClient := gnastyHTTPClient
	gnastyHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Status:     "500 Internal Server Error",
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", 2048))),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}
	t.Cleanup(func() { gnastyHTTPClient = prevClient })
	t.Setenv("ELORA_GNASTY_ADMIN_BASE", "http://gnasty.local")

	syncGnastyConfigBestEffort(runtimeconfig.DefaultsFromEnv(), "test")
	s := GnastySyncStatusSnapshot()

	if s.LastAttemptAt == nil {
		t.Fatalf("expected last_attempt_at to be set")
	}
	if s.LastSuccessAt != nil {
		t.Fatalf("did not expect success timestamp on failure")
	}
	if s.TargetBase != "http://gnasty.local" {
		t.Fatalf("unexpected target base: %q", s.TargetBase)
	}
	if strings.TrimSpace(s.LastError) == "" {
		t.Fatalf("expected error message to be captured")
	}
	if len(s.LastError) > 530 {
		t.Fatalf("expected truncated error, got length %d", len(s.LastError))
	}
}
