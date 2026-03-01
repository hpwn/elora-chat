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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
	"github.com/hpwn/EloraChat/src/backend/internal/tokenfile"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/twitch"
)

var refreshLocks sync.Map // map[string]*sync.Mutex
var serviceTokenMaintainerOnce sync.Once

const twitchServiceTokenKey = "service:twitch"

func lockRefresh(sessionToken string) func() {
	if strings.TrimSpace(sessionToken) == "" {
		return func() {}
	}
	muAny, _ := refreshLocks.LoadOrStore(sessionToken, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

var gnastyReloadMu sync.Mutex
var gnastyReloadUntil time.Time

func triggerGnastyReloadThrottled(ctx context.Context) error {
	gnastyReloadMu.Lock()
	now := time.Now()
	if now.Before(gnastyReloadUntil) {
		gnastyReloadMu.Unlock()
		return nil
	}
	gnastyReloadUntil = now.Add(1500 * time.Millisecond)
	gnastyReloadMu.Unlock()
	return triggerGnastyReload(ctx)
}

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
	twitchOAuthConfig     = newTwitchOAuthConfigFromEnv()
	twitchUserInfoURL     = "https://api.twitch.tv/helix/users"
	twitchHTTPClient      = http.DefaultClient
	gnastyHTTPClient      = &http.Client{Timeout: 2 * time.Second}
	tokenSourceFromConfig = func(cfg *oauth2.Config, c context.Context, token *oauth2.Token) oauth2.TokenSource {
		return cfg.TokenSource(c, token)
	}
	writeAccessTokenFile  = tokenfile.WriteAccessToken
	writeRefreshTokenFile = tokenfile.WriteRefreshToken
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
		if service == "twitch" {
			if err := upsertServiceTokenRecord(context.Background(), token); err != nil {
				log.Printf("auth: failed to persist twitch service token: %v", err)
			}
		}
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

	// never shorten an existing TTL if it's already further out
	if !currentExpiry.IsZero() && currentExpiry.After(expiry) {
		expiry = currentExpiry
	}

	// safety floor
	if expiry.Before(now.Add(5 * time.Minute)) {
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

	if service == "twitch" && tokenfile.PathFromEnv() != "" && !shouldWriteGnastyTokens() {
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
		desired := desiredAccessTokenFileContent(access)
		same, err := fileContentEquals(accessPath, desired)
		if err != nil {
			return wrote, err
		}
		if !same {
			if err := writeAccessTokenFile(accessPath, access); err != nil {
				return wrote, err
			}
			wrote = true
		}
	}

	if refresh := strings.TrimSpace(token.RefreshToken); refresh != "" {
		refreshPath := filepath.Join(dataDir, "twitch_refresh.pass")
		desired := desiredRefreshTokenFileContent(refresh)
		same, err := fileContentEquals(refreshPath, desired)
		if err != nil {
			return wrote, err
		}
		if !same {
			if err := writeRefreshTokenFile(refreshPath, refresh); err != nil {
				return wrote, err
			}
			wrote = true
		}
	}

	return wrote, nil
}

func desiredAccessTokenFileContent(access string) string {
	access = strings.TrimSpace(access)
	if access == "" {
		return ""
	}
	if !strings.HasPrefix(access, "oauth:") {
		access = "oauth:" + access
	}
	return access + "\n"
}

func desiredRefreshTokenFileContent(refresh string) string {
	refresh = strings.TrimSpace(refresh)
	if refresh == "" {
		return ""
	}
	return refresh + "\n"
}

func fileContentEquals(path, expected string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return string(data) == expected, nil
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
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"services": []}`))
		return
	}

	sessionToken := cookie.Value
	sess, sessionData, expired, err := loadSessionAllowExpired(r.Context(), sessionToken)
	if err != nil || sess == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"services": []}`))
		return
	}

	// If user has Twitch, refresh if needed.
	if services, ok := sessionData["services"].([]any); ok {
		for _, s := range services {
			if name, ok := s.(string); ok && name == "twitch" {
				if expired {
					log.Printf("auth: check-session saw expired session ttl; attempting refresh + ttl extend token=%s…", sessionToken[:8])
				}
				if err := refreshToken("twitch", sessionToken, sessionData); err != nil {
					log.Printf("auth: refreshToken in check-session failed: %v", err)
				}
				// reload so response is current (even if old ttl was expired)
				if s2, _, _, err2 := loadSessionAllowExpired(r.Context(), sessionToken); err2 == nil && s2 != nil {
					sess = s2
				}
				break
			}
		}
	}

	// Keep session TTL healthy (separate from OAuth token expiry).
	if err := maybeExtendSessionTTL(r.Context(), sess, 24*time.Hour); err != nil {
		log.Printf("auth: failed to extend session ttl: %v", err)
	} else {
		// reload once more if ttl update wrote
		if s2, _, _, err2 := loadSessionAllowExpired(r.Context(), sessionToken); err2 == nil && s2 != nil {
			sess = s2
		}
	}

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

	now := time.Now().Unix()
	if expiryUnix, ok := toUnixSeconds(sessionData["token_expiry"]); ok && now < expiryUnix {
		return nil
	}

	unlock := lockRefresh(service + ":" + sessionToken)
	defer unlock()

	sess, fresh, err := loadSession(context.Background(), sessionToken)
	if err != nil {
		return err
	}
	if sess == nil {
		return nil
	}
	sessionData = fresh

	now = time.Now().Unix()
	if expiryUnix, ok := toUnixSeconds(sessionData["token_expiry"]); ok && now < expiryUnix {
		return nil
	}

	refreshTokenValue, ok := sessionData["refresh_token"].(string)
	if !ok || strings.TrimSpace(refreshTokenValue) == "" {
		return nil
	}

	oauthConfig := twitchOAuthConfig
	token := &oauth2.Token{RefreshToken: refreshTokenValue}
	newToken, err := tokenSourceFromConfig(oauthConfig, context.Background(), token).Token()
	if err != nil {
		return fmt.Errorf("failed to refresh token: %v", err)
	}

	if strings.TrimSpace(newToken.RefreshToken) == "" {
		newToken.RefreshToken = refreshTokenValue
	}

	userData := map[string]any{
		fmt.Sprintf("%s_token", service): newToken.AccessToken,
		"token_expiry":                   newToken.Expiry.Unix(),
		"refresh_token":                  newToken.RefreshToken,
	}
	updateSessionDataForService(nil, userData, service, sessionToken)

	sessionData[fmt.Sprintf("%s_token", service)] = newToken.AccessToken
	sessionData["token_expiry"] = newToken.Expiry.Unix()
	sessionData["refresh_token"] = newToken.RefreshToken

	log.Printf("auth: refreshed %s token new_expiry=%d", service, newToken.Expiry.Unix())
	if service == "twitch" {
		if err := upsertServiceTokenRecord(context.Background(), newToken); err != nil {
			log.Printf("auth: failed to persist twitch service token (session refresh): %v", err)
		}
	}

	if service == "twitch" && shouldWriteGnastyTokens() {
		_, perr := persistGnastyTokens(newToken)
		if perr != nil {
			log.Printf("auth: failed to persist gnasty tokens (refresh): %v", perr)
		}
	}

	return nil
}

func storeOAuthState(c context.Context, state string) error {
	if chatStore == nil {
		return errors.New("storage not configured")
	}

	ctx, cancel := durableContext(c, 30*time.Second)
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

func loadSessionAllowExpired(c context.Context, token string) (*storage.Session, map[string]any, bool, error) {
	if chatStore == nil {
		return nil, nil, false, errors.New("storage not configured")
	}
	if c == nil {
		c = context.Background()
	}

	sess, err := chatStore.GetSession(c, token)
	if err != nil {
		return nil, nil, false, err
	}
	if sess == nil {
		return nil, nil, false, nil
	}

	now := time.Now().UTC()
	expired := !sess.TokenExpiry.IsZero() && !sess.TokenExpiry.After(now)

	if sess.DataJSON == "" {
		return sess, map[string]any{}, expired, nil
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(sess.DataJSON), &data); err != nil {
		return sess, nil, expired, err
	}
	return sess, data, expired, nil
}

func maybeExtendSessionTTL(c context.Context, sess *storage.Session, ttl time.Duration) error {
	if chatStore == nil || sess == nil {
		return nil
	}
	now := time.Now().UTC()

	target := now.Add(ttl)
	// throttle writes: only extend when we’re within 6h of expiry (or expiry is zero/expired)
	if !sess.TokenExpiry.IsZero() && sess.TokenExpiry.After(now.Add(6*time.Hour)) {
		return nil
	}
	if sess.TokenExpiry.After(target) {
		return nil
	}

	sess.TokenExpiry = target
	sess.UpdatedAt = now
	return chatStore.UpsertSession(c, sess)
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

func startServiceTokenMaintainer() {
	serviceTokenMaintainerOnce.Do(func() {
		interval := twitchServiceRefreshInterval()
		log.Printf("auth: twitch service token maintainer started (interval=%s, refresh_before=%s)", interval, twitchServiceRefreshSkew())
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				if err := maintainTwitchServiceToken(context.Background()); err != nil {
					log.Printf("auth: twitch service token maintenance failed: %v", err)
				}
				<-ticker.C
			}
		}()
	})
}

func maintainTwitchServiceToken(c context.Context) error {
	statusMissing := false
	statusSeeded := false
	statusRefreshed := false
	statusWroteFiles := false
	statusReloadSent := false
	var tickErr error
	defer func() {
		log.Printf("auth: twitch service token tick missing=%t seeded=%t refreshed=%t wrote_files=%t reload_sent=%t err=%v", statusMissing, statusSeeded, statusRefreshed, statusWroteFiles, statusReloadSent, tickErr)
	}()

	if chatStore == nil {
		tickErr = errors.New("storage not configured")
		return nil
	}
	if c == nil {
		c = context.Background()
	}

	sess, err := chatStore.GetSession(c, twitchServiceTokenKey)
	var token *oauth2.Token
	if err != nil {
		if isSessionNotFound(err) {
			statusMissing = true
			token, err = seedServiceTokenFromRefreshFile(c)
			if err != nil {
				tickErr = err
				return err
			}
			if token == nil {
				return nil
			}
			statusSeeded = true
		} else {
			tickErr = err
			return err
		}
	} else {
		if sess == nil || strings.TrimSpace(sess.DataJSON) == "" {
			statusMissing = true
			token, err = seedServiceTokenFromRefreshFile(c)
			if err != nil {
				tickErr = err
				return err
			}
			if token == nil {
				return nil
			}
			statusSeeded = true
		} else {
			token, err = parseServiceToken(sess.DataJSON)
			if err != nil {
				tickErr = fmt.Errorf("parse service token: %w", err)
				return tickErr
			}
		}
	}

	needsRefresh := shouldRefreshServiceToken(token, twitchServiceRefreshSkew())
	if needsRefresh {
		refreshValue := strings.TrimSpace(token.RefreshToken)
		if refreshValue == "" {
			tickErr = errors.New("service token missing refresh_token")
			return tickErr
		}

		newToken, refreshErr := tokenSourceFromConfig(twitchOAuthConfig, c, &oauth2.Token{RefreshToken: refreshValue}).Token()
		if refreshErr != nil {
			tickErr = fmt.Errorf("refresh service token: %w", refreshErr)
			return tickErr
		}
		if strings.TrimSpace(newToken.RefreshToken) == "" {
			newToken.RefreshToken = refreshValue
		}
		if err := upsertServiceTokenRecord(c, newToken); err != nil {
			tickErr = fmt.Errorf("persist refreshed service token: %w", err)
			return tickErr
		}
		token = newToken
		statusRefreshed = true
	}

	if shouldWriteGnastyTokens() {
		wrote, perr := persistGnastyTokens(token)
		if perr != nil {
			tickErr = fmt.Errorf("persist gnasty token files: %w", perr)
			return tickErr
		}
		statusWroteFiles = wrote
		if wrote {
			statusReloadSent = true
			reloadCtx, cancel := context.WithTimeout(c, 3*time.Second)
			defer cancel()
			if err := triggerGnastyReloadThrottled(reloadCtx); err != nil {
				if isDialOrHostError(err) {
					log.Printf("auth: gnasty reload warning (service token): %v", err)
				} else {
					log.Printf("auth: gnasty reload failed (service token): %v", err)
				}
			}
		}
	}

	return nil
}

func seedServiceTokenFromRefreshFile(c context.Context) (*oauth2.Token, error) {
	if chatStore == nil {
		return nil, nil
	}
	refreshPath := twitchRefreshTokenPath()
	data, err := os.ReadFile(refreshPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read refresh token file %s: %w", refreshPath, err)
	}
	refresh := strings.TrimSpace(strings.TrimPrefix(string(data), "oauth:"))
	if refresh == "" {
		return nil, nil
	}

	token := &oauth2.Token{
		RefreshToken: refresh,
	}
	if err := upsertServiceTokenRecordWithOptions(c, token, true); err != nil {
		return nil, fmt.Errorf("seed service token from refresh file: %w", err)
	}
	return token, nil
}

func shouldRefreshServiceToken(token *oauth2.Token, skew time.Duration) bool {
	if token == nil {
		return false
	}
	if token.Expiry.IsZero() {
		return true
	}
	return !token.Expiry.After(time.Now().UTC().Add(skew))
}

func twitchServiceRefreshInterval() time.Duration {
	mins := getEnvIntWithDefault("ELORA_TWITCH_SERVICE_REFRESH_INTERVAL_MINUTES", 5)
	if mins < 1 {
		mins = 1
	}
	return time.Duration(mins) * time.Minute
}

func twitchServiceRefreshSkew() time.Duration {
	mins := getEnvIntWithDefault("ELORA_TWITCH_SERVICE_REFRESH_BEFORE_EXPIRY_MINUTES", 10)
	if mins < 0 {
		mins = 0
	}
	return time.Duration(mins) * time.Minute
}

func getEnvIntWithDefault(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("auth: invalid %s=%q, using default %d", key, raw, def)
		return def
	}
	return v
}

func upsertServiceTokenRecord(c context.Context, token *oauth2.Token) error {
	return upsertServiceTokenRecordWithOptions(c, token, false)
}

func upsertServiceTokenRecordWithOptions(c context.Context, token *oauth2.Token, allowZeroExpiry bool) error {
	if chatStore == nil || token == nil {
		return nil
	}
	if c == nil {
		c = context.Background()
	}

	now := time.Now().UTC()
	expiry := token.Expiry.UTC()
	expiryUnix := token.Expiry.Unix()
	if expiry.IsZero() {
		if allowZeroExpiry {
			expiry = time.Unix(0, 0).UTC()
			expiryUnix = 0
		} else {
			expiry = now.Add(5 * time.Minute)
			expiryUnix = 0
		}
	}

	payload, err := json.Marshal(map[string]any{
		"access_token":  token.AccessToken,
		"refresh_token": token.RefreshToken,
		"expiry":        expiryUnix,
		"updated_at":    now.Unix(),
	})
	if err != nil {
		return err
	}

	return chatStore.UpsertSession(c, &storage.Session{
		Token:       twitchServiceTokenKey,
		Service:     "service_token",
		DataJSON:    string(payload),
		TokenExpiry: expiry,
		UpdatedAt:   now,
	})
}

func parseServiceToken(data string) (*oauth2.Token, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return nil, err
	}

	token := &oauth2.Token{}
	if v, _ := payload["access_token"].(string); strings.TrimSpace(v) != "" {
		token.AccessToken = strings.TrimSpace(v)
	}
	if v, _ := payload["refresh_token"].(string); strings.TrimSpace(v) != "" {
		token.RefreshToken = strings.TrimSpace(v)
	}
	if unix, ok := toUnixSeconds(payload["expiry"]); ok && unix > 0 {
		token.Expiry = time.Unix(unix, 0).UTC()
	}
	return token, nil
}

func twitchRefreshTokenPath() string {
	dataDir := strings.TrimSpace(os.Getenv("ELORA_DATA_DIR"))
	if dataDir == "" {
		dataDir = "/data"
	}
	return filepath.Join(filepath.Clean(dataDir), "twitch_refresh.pass")
}
