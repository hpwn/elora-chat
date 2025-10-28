package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/hpwn/EloraChat/src/backend/internal/configreporter"
)

// RegisterConfigz installs a /configz handler that returns the redacted runtime configuration.
func RegisterConfigz(mux *http.ServeMux, snapshot func() configreporter.Snapshot) {
	if mux == nil || snapshot == nil {
		return
	}

	mux.HandleFunc("/configz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		data := snapshot()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(data); err != nil {
			http.Error(w, "failed to encode config", http.StatusInternalServerError)
			return
		}
	})
}
