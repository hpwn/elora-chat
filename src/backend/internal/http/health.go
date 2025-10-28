package httpapi

import (
	"context"
	"net/http"
)

// ReadyChecker describes a dependency that can report readiness via Ping.
type ReadyChecker interface {
	Ping(ctx context.Context) error
}

// RegisterHealth attaches health and readiness endpoints to the provided ServeMux.
func RegisterHealth(mux *http.ServeMux, checker ReadyChecker) {
	if mux == nil {
		return
	}

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if checker == nil {
			http.Error(w, "db not ready", http.StatusServiceUnavailable)
			return
		}
		if err := checker.Ping(r.Context()); err != nil {
			http.Error(w, "db not ready", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
}
