package routes

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

func TestAlertsProxyListForwardsQueryAndBody(t *testing.T) {
	t.Setenv("ELORA_GNASTY_ALERT_BASE", "http://alerts-upstream.local")

	previousClient := gnastyHTTPClient
	gnastyHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "http://alerts-upstream.local/alerts?platform=twitch&limit=2" {
			t.Fatalf("unexpected upstream URL: %s", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"items":[{"id":"a1"}]}`)),
		}, nil
	})}
	t.Cleanup(func() { gnastyHTTPClient = previousClient })

	router := mux.NewRouter()
	SetupAlertRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/alerts?platform=twitch&limit=2", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := strings.TrimSpace(rr.Body.String()); got != `{"items":[{"id":"a1"}]}` {
		t.Fatalf("unexpected body: %s", got)
	}
}

func TestAlertsProxyCountPassesStatusCode(t *testing.T) {
	t.Setenv("ELORA_GNASTY_ALERT_BASE", "http://alerts-upstream.local")

	previousClient := gnastyHTTPClient
	gnastyHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "http://alerts-upstream.local/alerts/count" {
			t.Fatalf("unexpected upstream URL: %s", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":"upstream failure"}`)),
		}, nil
	})}
	t.Cleanup(func() { gnastyHTTPClient = previousClient })

	router := mux.NewRouter()
	SetupAlertRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/alerts/count", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
	if got := strings.TrimSpace(rr.Body.String()); got != `{"error":"upstream failure"}` {
		t.Fatalf("unexpected body: %s", got)
	}
}

func TestAlertsProxyStreamPassesSSEData(t *testing.T) {
	t.Setenv("ELORA_GNASTY_ALERT_BASE", "http://alerts-upstream.local")

	previousClient := gnastyHTTPClient
	previousStreamClient := gnastyAlertStreamHTTPClient
	gnastyHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("stream endpoint should not use gnastyHTTPClient")
		return nil, nil
	})}
	gnastyAlertStreamHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "http://alerts-upstream.local/alerts/stream?platform=youtube" {
			t.Fatalf("unexpected upstream URL: %s", req.URL.String())
		}
		body := "event: alert\ndata: {\"id\":\"evt-1\"}\n\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}
	t.Cleanup(func() {
		gnastyHTTPClient = previousClient
		gnastyAlertStreamHTTPClient = previousStreamClient
	})

	router := mux.NewRouter()
	SetupAlertRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/alerts/stream?platform=youtube", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("expected event stream content type, got %q", got)
	}

	scanner := bufio.NewScanner(strings.NewReader(rr.Body.String()))
	lines := make([]string, 0, 2)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) < 2 {
		t.Fatalf("expected sse lines, got %q", rr.Body.String())
	}
	if lines[0] != "event: alert" || lines[1] != "data: {\"id\":\"evt-1\"}" {
		t.Fatalf("unexpected sse payload: %q", rr.Body.String())
	}
}
