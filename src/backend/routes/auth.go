package routes

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
	"github.com/hpwn/EloraChat/src/backend/internal/tokenfile"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/twitch"
)

// Twitch OAuth configuration
func newTwitchOAuthConfigFromEnv() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     os.Getenv("TWITCH_OAUTH_CLIENT_ID"),
		ClientSecret: os.Getenv("TWITCH_OAUTH_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("TWITCH_OAUTH_REDIRECT_URL"),
		Scopes:       []string{"chat:edit", "chat:read"}, // Updated scopes
		Endpoint:     twitch.Endpoint,                    // Make sure to import "golang.org/x/oauth2/twitch"
	}
}

var (
	twitchOAuthConfig = newTwitchOAuthConfigFromEnv()
	twitchUserInfoURL = "https://api.twitch.tv/helix/users"
	twitchHTTPClient  = http.DefaultClient
	gnastyHTTPClient  = &http.Client{Timeout: 2 * time.Second}
)

// loginHandler to initiate OAuth with Twitch
func loginHandler(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(r.URL.Path, "/twitch") {
		http.Error(w, "Unsupported platform", http.StatusBadRequest)
		return
	}

	// Generate a new random state for the OAuth flow
	state, err := generateState()
	if err != nil {
		log.Printf("auth: failed to generate oauth state: %v", err)
		writeTwitchLoginFailure(w, r)
		return
	}

	// Store the state via the backing store with an expiration window.
	if err := storeOAuthState(r.Context(), state); err != nil {
		log.Printf("auth: failed to persist oauth state via main db: %v", err)
		writeTwitchLoginFailure(w, r)
		return
	}

	// Construct the OAuth URL and redirect the user to the Twitch authentication page
	url := twitchOAuthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func callbackHandler(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(r.URL.Path, "/twitch") {
		http.Error(w, "Unsupported platform", http.StatusBadRequest)
		return
	}

	var oauthConfig *oauth2.Config = twitchOAuthConfig
	var service string = "twitch"

	// Check if an error query parameter is present
	if errorReason := r.FormValue("error"); errorReason != "" {
		fmt.Printf("OAuth error: %s, Description: %s\n", errorReason, r.FormValue("error_description"))
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	// Verify the state parameter matches
	receivedState := r.FormValue("state")
	if receivedState == "" || !validateState(receivedState) {
		http.Error(w, "State mismatch or missing state", http.StatusBadRequest)
		return
	}

	// Exchange the auth code for an access token
	token, err := oauthConfig.Exchange(context.Background(), r.FormValue("code"))
	if err != nil {
		log.Printf("auth: twitch token exchange failed: %v", err)
	}

	var req *http.Request
	var res *http.Response

	if token != nil {
		// For Twitch, manually set the headers and create the request
		req, err = http.NewRequest(http.MethodGet, twitchUserInfoURL, nil)
		if err != nil {
			http.Error(w, "Failed to create request: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// Set necessary headers for Twitch API
		req.Header.Set("Client-ID", oauthConfig.ClientID)
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		res, err = twitchHTTPClient.Do(req)

		if err != nil || res.StatusCode != 200 {
			http.Error(w, "Failed to get user info", http.StatusInternalServerError)
			return
		}
		defer res.Body.Close()

		body, err := io.ReadAll(res.Body)
		if err != nil {
			http.Error(w, "Failed to read response body", http.StatusInternalServerError)
			return
		}

		// Unmarshal the user data
		var userData map[string]any
		if err = json.Unmarshal(body, &userData); err != nil {
			http.Error(w, "Failed to parse user data: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Before generating a new session token, check if an existing session token is present
		var sessionToken string
		if cookie, err := r.Cookie("session_token"); err == nil {
			// Use the existing session token if present
			sessionToken = cookie.Value
		} else {
			// Generate a new session token if it does not exist
			sessionToken = generateSessionToken()
		}

		// Include the service in the userData map
		userData["service"] = service

		// Assuming user data fetching was successful, include OAuth token in userData.
		// The key for storing the token should match what you'll use in sendMessage functions.
		if service == "twitch" {
			userData["twitch_token"] = token.AccessToken
		} else if service == "youtube" {
			userData["youtube_token"] = token.AccessToken
		}

		// Store refresh token and expiry time if available
		if token.RefreshToken != "" {
			userData["refresh_token"] = token.RefreshToken
		}
		userData["token_expiry"] = token.Expiry.Unix() // Store as Unix timestamp for simplicity

		// Now, use a function to update the session data with this service login
		// This should include setting the session token in a cookie
		updateSessionDataForService(w, userData, service, sessionToken)
	}

	if shouldWriteGnastyTokens() {
		wrote, err := persistGnastyTokens(token)
		if err != nil {
			log.Printf("auth: failed to persist gnasty tokens: %v", err)
		}
		if wrote || strings.TrimSpace(os.Getenv("ELORA_GNASTY_RELOAD_URL")) != "" {
			reloadCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			if err := triggerGnastyReload(reloadCtx); err != nil {
				if isDialOrHostError(err) {
					log.Printf("auth: gnasty reload warning: %v", err)
				} else {
					log.Printf("auth: gnasty reload failed: %v", err)
				}
			}
		}
	}

	writeTwitchConnectedPage(w)
}

func updateSessionDataForService(w http.ResponseWriter, userData map[string]any, service string, sessionToken string) {
	if chatStore == nil {
		log.Println("auth: storage not configured; cannot persist session")
		return
	}

	existingSessionData := make(map[string]any)
	var currentExpiry time.Time

	sess, err := chatStore.GetSession(ctx, sessionToken)
	if err == nil && sess != nil {
		if sess.DataJSON != "" {
			if unmarshalErr := json.Unmarshal([]byte(sess.DataJSON), &existingSessionData); unmarshalErr != nil {
				log.Printf("auth: error unmarshalling existing session data: %v", unmarshalErr)
				existingSessionData = make(map[string]any)
			}
		}
		currentExpiry = sess.TokenExpiry
	} else if err != nil && !isSessionNotFound(err) {
		log.Printf("auth: failed to load existing session data: %v", err)
	}

	services, ok := existingSessionData["services"].([]any)
	if !ok {
		services = []any{}
	}
	if !contains(services, service) {
		services = append(services, service)
	}
	existingSessionData["services"] = services

	for key, value := range userData {
		if key != "services" {
			existingSessionData[key] = value
		}
	}

	now := time.Now().UTC()
	existingSessionData["updated_at"] = now.Unix()

	expiry := now.Add(24 * time.Hour)
	if ts, ok := toUnixSeconds(existingSessionData["token_expiry"]); ok {
		expiry = time.Unix(ts, 0).UTC()
	} else if !currentExpiry.IsZero() {
		expiry = currentExpiry
	}
	if expiry.Before(now) {
		expiry = now.Add(5 * time.Minute)
	}

	payload, err := json.Marshal(existingSessionData)
	if err != nil {
		log.Printf("auth: error marshalling updated session data: %v", err)
		return
	}

	record := &storage.Session{
		Token:       sessionToken,
		Service:     service,
		DataJSON:    string(payload),
		TokenExpiry: expiry,
		UpdatedAt:   now,
	}

	if err := chatStore.UpsertSession(ctx, record); err != nil {
		log.Printf("auth: failed to store updated session data: %v", err)
		return
	}

	if service == "twitch" && tokenfile.PathFromEnv() != "" {
		if tok, _ := existingSessionData["twitch_token"].(string); strings.TrimSpace(tok) != "" {
			if err := tokenfile.Save(tok); err != nil {
				if !errors.Is(err, tokenfile.ErrEmptyToken) {
					log.Printf("auth: twitch token export skipped (%v)", err)
				}
			} else {
				log.Printf("auth: twitch token exported to file")
			}
		}
	}

	if w != nil {
		setSessionCookie(w, sessionToken)
	}
}

func shouldWriteGnastyTokens() bool {
	v := strings.TrimSpace(os.Getenv("ELORA_TWITCH_WRITE_GNASTY_TOKENS"))
	if v == "" {
		return true
	}
	v = strings.ToLower(v)
	switch v {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func persistGnastyTokens(token *oauth2.Token) (bool, error) {
	if token == nil {
		return false, nil
	}

	dataDir := strings.TrimSpace(os.Getenv("ELORA_DATA_DIR"))
	if dataDir == "" {
		dataDir = "/data"
	}

	wrote := false

	if access := strings.TrimSpace(token.AccessToken); access != "" {
		accessPath := filepath.Join(dataDir, "twitch_irc.pass")
		if err := tokenfile.WriteAccessToken(accessPath, access); err != nil {
			return wrote, err
		}
		wrote = true
	}

	if refresh := strings.TrimSpace(token.RefreshToken); refresh != "" {
		refreshPath := filepath.Join(dataDir, "twitch_refresh.pass")
		if err := tokenfile.WriteRefreshToken(refreshPath, refresh); err != nil {
			return wrote, err
		}
		wrote = true
	}

	return wrote, nil
}

func triggerGnastyReload(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	url := strings.TrimSpace(os.Getenv("ELORA_GNASTY_RELOAD_URL"))
	if url == "" {
		port := strings.TrimSpace(os.Getenv("GNASTY_HTTP_PORT"))
		if port == "" {
			port = "8765"
		}
		url = fmt.Sprintf("http://gnasty:%s/admin/twitch/reload", port)
	}
	if url == "" {
		return nil
	}

	client := gnastyHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("auth: failed to build gnasty reload request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("auth: gnasty reload request failed: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("auth: gnasty reload returned %s", resp.Status)
	}
	return nil
}

// Helper function to check if a service is already in the services slice
func contains(slice []any, str string) bool {
	for _, v := range slice {
		if s, ok := v.(string); ok && s == str {
			return true
		}
	}
	return false
}

// Function to set session cookie
func setSessionCookie(w http.ResponseWriter, sessionToken string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    sessionToken,
		Path:     "/",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
		Secure:   true,                 // Adjust based on your deployment (HTTPS)
		SameSite: http.SameSiteLaxMode, // Or adjust based on your cross-origin policy
	})
}

// generateState creates a new random state for OAuth flow.
func generateState() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	state := hex.EncodeToString(b)
	return state, nil
}

// validateState checks the provided state against the persisted session store.
func validateState(state string) bool {
	if chatStore == nil {
		return false
	}

	key := oauthStateKey(state)
	sess, err := chatStore.GetSession(ctx, key)
	if err != nil {
		return false
	}
	defer func() {
		if delErr := chatStore.DeleteSession(ctx, key); delErr != nil {
			log.Printf("auth: failed to delete oauth state %s: %v", key, delErr)
		}
	}()

	if sess.TokenExpiry.Before(time.Now().UTC()) {
		return false
	}

	if sess.DataJSON == "" {
		return true
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(sess.DataJSON), &payload); err != nil {
		return false
	}
	value, _ := payload["state"].(string)
	return strings.HasPrefix(value, "valid")
}

// generateSessionToken creates a new secure, random session token.
func generateSessionToken() string {
	b := make([]byte, 32) // 32 bytes results in a 44-character base64 encoded string
	_, err := rand.Read(b)
	if err != nil {
		// Handle error; it's crucial to securely generate a random token.
		return "" // Return empty string or handle it appropriately.
	}
	return base64.URLEncoding.EncodeToString(b)
}

// SessionMiddleware checks for a valid session token in the request cookies.
func SessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Attempt to retrieve the session token cookie
		cookie, err := r.Cookie("session_token")
		if err != nil {
			log.Printf("No session token cookie found: %v\n", err)
			http.Error(w, "Unauthorized: No session token", http.StatusUnauthorized)
			return
		}

		sessionToken := cookie.Value

		_, sessionData, err := loadSession(r.Context(), sessionToken)
		if err != nil {
			if isSessionNotFound(err) {
				log.Printf("Session token not found or expired: %v\n", err)
				http.Error(w, "Unauthorized: Invalid session token", http.StatusUnauthorized)
				return
			}
			log.Printf("Error retrieving session data: %v\n", err)
			http.Error(w, "Error processing session data", http.StatusInternalServerError)
			return
		}

		services, ok := sessionData["services"].([]any)
		if !ok {
			log.Println("Services array missing or incorrect format in session data")
			http.Error(w, "Unauthorized: Required services not found", http.StatusUnauthorized)
			return
		}

		var hasTwitch bool
		for _, service := range services {
			if serviceName, ok := service.(string); ok && serviceName == "twitch" {
				hasTwitch = true
				// Refresh Twitch token if necessary
				if err := refreshToken("twitch", sessionToken, sessionData); err != nil {
					log.Printf("Error refreshing Twitch token: %v", err)
				}
				break // Since we're only interested in Twitch, we can break early
			}
		}

		if !hasTwitch {
			log.Println("User has not logged in with Twitch")
			http.Error(w, "Unauthorized: Twitch service not logged in", http.StatusUnauthorized)
			return
		}

		// Proceed with the request
		next.ServeHTTP(w, r)
	})
}

func sessionCheckHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_token")
	if err != nil {
		// If the session token is not found, it means the user is not logged in.
		// Instead of returning an error, return a response indicating no session is active.
		// log.Println("Session token not found:", err)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"services": []}`)) // Indicate no services are logged in.
		return
	}

	sessionToken := cookie.Value
	sess, _, err := loadSession(r.Context(), sessionToken)
	if err != nil || sess == nil {
		// If session data is not found in the store, it's likely the session has expired or is invalid.
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"services": []}`)) // Similarly, indicate no services are logged in.
		return
	}

	// If we reach this point, we have valid session data.
	// Send the session data back to the client.
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(sess.DataJSON))
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_token")
	if err == nil && cookie != nil {
		// Delete the session token from the backing store
		sessionToken := cookie.Value
		if chatStore != nil {
			if err := chatStore.DeleteSession(ctx, sessionToken); err != nil {
				log.Printf("auth: error deleting session during logout: %v", err)
			}
		}
	}

	// Invalidate the cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   true, // Set to true if you're serving your site over HTTPS
		SameSite: http.SameSiteLaxMode,
	})
	// Redirect to home or login page
	http.Redirect(w, r, "/", http.StatusFound)
}

func refreshToken(service string, sessionToken string, sessionData map[string]any) error {
	if sessionData == nil {
		return errors.New("session data unavailable")
	}

	expiryUnix, expiryOk := toUnixSeconds(sessionData["token_expiry"])
	refreshTokenValue, refreshTokenOk := sessionData["refresh_token"].(string)
	if !refreshTokenOk {
		return nil
	}
	if expiryOk && time.Now().Unix() < expiryUnix {
		// Token hasn't expired yet; nothing to do.
		return nil
	}

	var oauthConfig *oauth2.Config = twitchOAuthConfig

	token := &oauth2.Token{
		RefreshToken: refreshTokenValue,
	}
	ts := oauthConfig.TokenSource(context.Background(), token)
	newToken, err := ts.Token() // This refreshes the token
	if err != nil {
		return fmt.Errorf("failed to refresh token: %v", err)
	}

	userData := map[string]any{
		fmt.Sprintf("%s_token", service): newToken.AccessToken,
		"refresh_token":                  newToken.RefreshToken,
		"token_expiry":                   newToken.Expiry.Unix(),
	}

	// Use the existing function to update the session data without resetting cookies.
	updateSessionDataForService(nil, userData, service, sessionToken)

	return nil
}

func storeOAuthState(c context.Context, state string) error {
	if chatStore == nil {
		return errors.New("storage not configured")
	}

	ctx, cancel := durableContext(c, 5*time.Second)
	defer cancel()

	payload, err := json.Marshal(map[string]any{"state": "valid"})
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	return chatStore.UpsertSession(ctx, &storage.Session{
		Token:       oauthStateKey(state),
		Service:     "oauth",
		DataJSON:    string(payload),
		TokenExpiry: now.Add(10 * time.Minute),
		UpdatedAt:   now,
	})
}

func durableContext(parent context.Context, minTimeout time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		return context.WithTimeout(context.Background(), minTimeout)
	}

	if deadline, ok := parent.Deadline(); ok && time.Until(deadline) >= minTimeout {
		return parent, func() {}
	}

	return context.WithTimeout(context.Background(), minTimeout)
}

func oauthStateKey(state string) string {
	return "oauth-state:" + state
}

func writeTwitchLoginFailure(w http.ResponseWriter, r *http.Request) {
	message := "Twitch login failed, please try again."
	status := http.StatusBadGateway

	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": message})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte("<!DOCTYPE html><html><body><p>Twitch login failed, please try again.</p></body></html>"))
}

func writeTwitchConnectedPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!DOCTYPE html><html><body>Connected ✔, redirecting back to Elora…<script>setTimeout(function(){ window.location.href = "/"; }, 1500);</script></body></html>`))
}

func isDialOrHostError(err error) bool {
	if err == nil {
		return false
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	var opErr *net.OpError
	return errors.As(err, &opErr)
}

func loadSession(c context.Context, token string) (*storage.Session, map[string]any, error) {
	if chatStore == nil {
		return nil, nil, errors.New("storage not configured")
	}
	if c == nil {
		c = context.Background()
	}

	sess, err := chatStore.GetSession(c, token)
	if err != nil {
		return nil, nil, err
	}

	if sess == nil {
		return nil, nil, nil
	}

	now := time.Now().UTC()
	if !sess.TokenExpiry.IsZero() && !sess.TokenExpiry.After(now) {
		return nil, nil, nil
	}

	if sess.DataJSON == "" {
		return sess, map[string]any{}, nil
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(sess.DataJSON), &data); err != nil {
		return sess, nil, err
	}
	return sess, data, nil
}

func isSessionNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, sql.ErrNoRows)
}

func toUnixSeconds(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		return int64(v), true
	case float32:
		return int64(v), true
	case int:
		return int64(v), true
	case int64:
		return v, true
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func SetupAuthRoutes(router *mux.Router) {
	// Existing setup...
	router.HandleFunc("/auth/twitch/start", loginHandler).Methods("GET")
	router.HandleFunc("/login/twitch", loginHandler).Methods("GET") // legacy alias
	router.HandleFunc("/callback/twitch", callbackHandler)
	router.HandleFunc("/logout", logoutHandler).Methods("POST")

	// This route is now outside of the authRoutes subrouter to be accessible without both services logged in
	router.HandleFunc("/check-session", sessionCheckHandler).Methods("GET")

	// Subrouter for routes that require authentication
	authRoutes := router.PathPrefix("/auth").Subrouter()
	authRoutes.Use(SessionMiddleware)
}
