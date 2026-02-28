package routes

import (
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"
)

const defaultGnastyAlertsBase = defaultGnastyAdminBase

var gnastyAlertStreamHTTPClient = &http.Client{Timeout: 0}

func SetupAlertRoutes(r *mux.Router) {
	r.HandleFunc("/api/alerts", handleAlertList).Methods(http.MethodGet)
	r.HandleFunc("/api/alerts/count", handleAlertCount).Methods(http.MethodGet)
	r.HandleFunc("/api/alerts/stream", handleAlertStream).Methods(http.MethodGet)
}

func handleAlertList(w http.ResponseWriter, r *http.Request) {
	proxyGnastyAlertsGET(w, r, "/alerts", false)
}

func handleAlertCount(w http.ResponseWriter, r *http.Request) {
	proxyGnastyAlertsGET(w, r, "/alerts/count", false)
}

func handleAlertStream(w http.ResponseWriter, r *http.Request) {
	proxyGnastyAlertsGET(w, r, "/alerts/stream", true)
}

func gnastyAlertsBaseURL() string {
	if base := strings.TrimSpace(os.Getenv("ELORA_GNASTY_ALERT_BASE")); base != "" {
		return strings.TrimRight(base, "/")
	}
	if base := strings.TrimSpace(os.Getenv("ELORA_GNASTY_ADMIN_BASE")); base != "" {
		return strings.TrimRight(base, "/")
	}
	return strings.TrimRight(defaultGnastyAlertsBase, "/")
}

func proxyGnastyAlertsGET(w http.ResponseWriter, r *http.Request, upstreamPath string, stream bool) {
	base := gnastyAlertsBaseURL()
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, base+upstreamPath, nil)
	if err != nil {
		http.Error(w, "failed to build alert upstream request", http.StatusInternalServerError)
		return
	}

	req.URL.RawQuery = r.URL.RawQuery
	if accept := strings.TrimSpace(r.Header.Get("Accept")); accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("User-Agent", "elora-chat/alerts-proxy")

	client := gnastyHTTPClient
	if stream {
		client = gnastyAlertStreamHTTPClient
	}
	if client == nil {
		client = &http.Client{}
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("alerts: upstream request failed path=%s err=%v", upstreamPath, err)
		http.Error(w, "alerts upstream unavailable", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	for _, header := range []string{"Content-Type", "Cache-Control", "Connection"} {
		if value := strings.TrimSpace(resp.Header.Get(header)); value != "" {
			w.Header().Set(header, value)
		}
	}
	w.WriteHeader(resp.StatusCode)

	if !stream || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(w, resp.Body)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		_, _ = io.Copy(w, resp.Body)
		return
	}

	buffer := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				return
			}
			flusher.Flush()
		}
		if readErr != nil {
			if readErr == io.EOF {
				return
			}
			return
		}
	}
}
