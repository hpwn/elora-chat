package httpapi

import "net/http"

// RegisterHealth attaches the health check endpoint to the provided ServeMux.
func RegisterHealth(mux *http.ServeMux) {
	if mux == nil {
		return
	}

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}
