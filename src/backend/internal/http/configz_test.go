package httpapi

import (
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
