package routes

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/gorilla/mux"
	// ytbot "github.com/ketan-10/ytLiveChatBot"
	// "golang.org/x/oauth2"
	// "golang.org/x/oauth2/google"
	// "google.golang.org/api/option"
	// "google.golang.org/api/youtube/v3"
	// ytbot "github.com/ketan-10/ytLiveChatBot"
	// "golang.org/x/oauth2"
	// "golang.org/x/oauth2/google"
	// "google.golang.org/api/option"
	// "google.golang.org/api/youtube/v3"
)

// // Global variables
// var (
// 	chatBot   *ytbot.LiveChatBot
// 	config    *oauth2.Config
// 	apiKey    = os.Getenv("YOUTUBE_API_KEY")
// 	channelID = "UC2c4NxvHnbXs3NLpCm641ew" // Dayoman
// 	// channelID = "UCHToAogHtFnv2uksbDzKsYA" // hp_az
// 	// channelID = "UCSJ4gkVC6NrvII8umztf0Ow" // lofigirl
// )

// func init() {
// 	// Load OAuth2 configuration
// 	var err error
// 	config, err = loadConfig("client_secret.json")
// 	if err != nil {
// 		log.Fatalf("Unable to load OAuth2 configuration: %v", err)
// 	}
// func init() {
// 	// Load OAuth2 configuration
// 	var err error
// 	config, err = loadConfig("client_secret.json")
// 	if err != nil {
// 		log.Fatalf("Unable to load OAuth2 configuration: %v", err)
// 	}

// 	// Initialize bot with current live stream URL
// 	if err := cacheLiveStreamURL(apiKey, channelID); err == nil {
// 		startChatBot(liveURL)
// 	}
// 	// Initialize bot with current live stream URL
// 	if err := cacheLiveStreamURL(apiKey, channelID); err == nil {
// 		startChatBot(liveURL)
// 	}

// 	// Start periodic refresh of YouTube Auth Token every 30 minutes
// 	// go refreshYouTubeAuthTokenEvery(30 * time.Minute)
// }
// 	// Start periodic refresh of YouTube Auth Token every 30 minutes
// 	// go refreshYouTubeAuthTokenEvery(30 * time.Minute)
// }

// func loadConfig(credentialFileName string) (*oauth2.Config, error) {
// 	b, err := ioutil.ReadFile(credentialFileName)
// 	if err != nil {
// 		return nil, fmt.Errorf("unable to read client secret file: %v", err)
// 	}
// 	config, err := google.ConfigFromJSON(b, youtube.YoutubeForceSslScope)
// 	if err != nil {
// 		return nil, fmt.Errorf("unable to parse client secret file to config: %v", err)
// 	}
// 	return config, nil
// }
// func loadConfig(credentialFileName string) (*oauth2.Config, error) {
// 	b, err := ioutil.ReadFile(credentialFileName)
// 	if err != nil {
// 		return nil, fmt.Errorf("unable to read client secret file: %v", err)
// 	}
// 	config, err := google.ConfigFromJSON(b, youtube.YoutubeForceSslScope)
// 	if err != nil {
// 		return nil, fmt.Errorf("unable to parse client secret file to config: %v", err)
// 	}
// 	return config, nil
// }

// func startChatBot(url string) {
// 	chatBot = ytbot.NewLiveChatBot(&ytbot.LiveChatBotInput{
// 		Urls: []string{url},
// 	})
// }
// func startChatBot(url string) {
// 	chatBot = ytbot.NewLiveChatBot(&ytbot.LiveChatBotInput{
// 		Urls: []string{url},
// 	})
// }

// func refreshYouTubeAuthTokenEvery(interval time.Duration) {
// 	ticker := time.NewTicker(interval)
// 	defer ticker.Stop()
// func refreshYouTubeAuthTokenEvery(interval time.Duration) {
// 	ticker := time.NewTicker(interval)
// 	defer ticker.Stop()

// 	for range ticker.C {
// 		if err := refreshYouTubeToken(); err != nil {
// 			log.Printf("Error during scheduled token refresh: %v", err)
// 		}
// 	}
// }
// 	for range ticker.C {
// 		if err := refreshYouTubeToken(); err != nil {
// 			log.Printf("Error during scheduled token refresh: %v", err)
// 		}
// 	}
// }

// func refreshYouTubeToken() error {
// 	tokenFile := "/home/myuser/.credentials/youtube-go.json" // Ensure this path matches the Dockerfile path
// 	tok, err := tokenFromFile(tokenFile)
// 	if err != nil {
// 		return fmt.Errorf("unable to read token from file: %v", err)
// 	}
// func refreshYouTubeToken() error {
// 	tokenFile := "/home/myuser/.credentials/youtube-go.json" // Ensure this path matches the Dockerfile path
// 	tok, err := tokenFromFile(tokenFile)
// 	if err != nil {
// 		return fmt.Errorf("unable to read token from file: %v", err)
// 	}

// 	tokenSource := config.TokenSource(context.Background(), tok)
// 	newToken, err := tokenSource.Token()
// 	if err != nil {
// 		return fmt.Errorf("unable to refresh token: %v", err)
// 	}
// 	tokenSource := config.TokenSource(context.Background(), tok)
// 	newToken, err := tokenSource.Token()
// 	if err != nil {
// 		return fmt.Errorf("unable to refresh token: %v", err)
// 	}

// 	// Log token details for debugging
// 	log.Printf("Token details: AccessToken=%s, RefreshToken=%s, Expiry=%v", newToken.AccessToken, newToken.RefreshToken, newToken.Expiry)
// 	// Log token details for debugging
// 	log.Printf("Token details: AccessToken=%s, RefreshToken=%s, Expiry=%v", newToken.AccessToken, newToken.RefreshToken, newToken.Expiry)

// 	if newToken.AccessToken != tok.AccessToken {
// 		log.Printf("New token acquired. Old expiry: %v, New expiry: %v", tok.Expiry, newToken.Expiry)
// 	} else {
// 		log.Printf("Token appears to be the same or not in need of refresh. Current expiry: %v", tok.Expiry)
// 	}
// 	if newToken.AccessToken != tok.AccessToken {
// 		log.Printf("New token acquired. Old expiry: %v, New expiry: %v", tok.Expiry, newToken.Expiry)
// 	} else {
// 		log.Printf("Token appears to be the same or not in need of refresh. Current expiry: %v", tok.Expiry)
// 	}

// 	return saveToken(tokenFile, newToken)
// }
// 	return saveToken(tokenFile, newToken)
// }

// func tokenFromFile(file string) (*oauth2.Token, error) {
// 	f, err := os.Open(file)
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer f.Close()
// 	tok := &oauth2.Token{}
// 	err = json.NewDecoder(f).Decode(tok)
// 	return tok, err
// }
// func tokenFromFile(file string) (*oauth2.Token, error) {
// 	f, err := os.Open(file)
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer f.Close()
// 	tok := &oauth2.Token{}
// 	err = json.NewDecoder(f).Decode(tok)
// 	return tok, err
// }

// func saveToken(path string, token *oauth2.Token) error {
// 	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
// 	if err != nil {
// 		return fmt.Errorf("unable to cache OAuth token: %v", err)
// 	}
// 	defer f.Close()
// 	return json.NewEncoder(f).Encode(token)
// }
// func saveToken(path string, token *oauth2.Token) error {
// 	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
// 	if err != nil {
// 		return fmt.Errorf("unable to cache OAuth token: %v", err)
// 	}
// 	defer f.Close()
// 	return json.NewEncoder(f).Encode(token)
// }

// func cacheLiveStreamURL(apiKey, channelID string) error {
// 	url, err := fetchLiveStreamURL(apiKey, channelID)
// 	if err != nil {
// 		log.Printf("Error fetching YouTube live stream URL: %v", err)
// 		return err
// 	}
// func cacheLiveStreamURL(apiKey, channelID string) error {
// 	url, err := fetchLiveStreamURL(apiKey, channelID)
// 	if err != nil {
// 		log.Printf("Error fetching YouTube live stream URL: %v", err)
// 		return err
// 	}

// 	if err != nil {
// 		return err
// 	}
// 	return nil
// }
// 	if err != nil {
// 		return err
// 	}
// 	return nil
// }

// func fetchLiveStreamURL(apiKey, channelID string) (string, error) {
// 	ctx := context.Background()
// 	youtubeService, err := youtube.NewService(ctx, option.WithAPIKey(apiKey))
// 	if err != nil {
// 		return "", fmt.Errorf("error creating YouTube service: %v", err)
// 	}
// func fetchLiveStreamURL(apiKey, channelID string) (string, error) {
// 	ctx := context.Background()
// 	youtubeService, err := youtube.NewService(ctx, option.WithAPIKey(apiKey))
// 	if err != nil {
// 		return "", fmt.Errorf("error creating YouTube service: %v", err)
// 	}

// 	call := youtubeService.Search.List([]string{"id"}).ChannelId(channelID).EventType("live").Type("video").MaxResults(1)
// 	response, err := call.Do()
// 	if err != nil {
// 		return "", fmt.Errorf("error making search API call: %v", err)
// 	}
// 	call := youtubeService.Search.List([]string{"id"}).ChannelId(channelID).EventType("live").Type("video").MaxResults(1)
// 	response, err := call.Do()
// 	if err != nil {
// 		return "", fmt.Errorf("error making search API call: %v", err)
// 	}

// 	if len(response.Items) == 0 {
// 		return "", fmt.Errorf("no live streams currently on this channel")
// 	}
// 	if len(response.Items) == 0 {
// 		return "", fmt.Errorf("no live streams currently on this channel")
// 	}

// 	liveVideoID := response.Items[0].Id.VideoId
// 	return fmt.Sprintf("https://www.youtube.com/watch?v=%s", liveVideoID), nil
// }
// 	liveVideoID := response.Items[0].Id.VideoId
// 	return fmt.Sprintf("https://www.youtube.com/watch?v=%s", liveVideoID), nil
// }

// sendMessageHandler handles requests to send messages to both Twitch and YouTube chats.
func sendMessageHandler(w http.ResponseWriter, r *http.Request) {
	// Parse request body to get the message content
	var requestBody struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Retrieve session token from cookies
	sessionToken, err := getSessionTokenFromRequest(r)
	if err != nil {
		http.Error(w, "Unauthorized: No session token", http.StatusUnauthorized)
		return
	}

	// Retrieve the username associated with this session
	username, err := getUsernameFromSession(sessionToken)
	if err != nil {
		if isSessionNotFound(err) {
			http.Error(w, "Unauthorized: Invalid session token", http.StatusUnauthorized)
		} else {
			http.Error(w, "Failed to get username from session", http.StatusInternalServerError)
		}
		return
	}

	// Check if the username matches "Dayoman" or "hp_az", case-insensitive
	usernameLower := strings.ToLower(username)
	if usernameLower != "dayoman" && usernameLower != "hp_az" && usernameLower != "osrs_wiz" {
		http.Error(w, "Unauthorized: Incorrect user", http.StatusUnauthorized)
		return
	}

	// Send message to Twitch
	if err := sendMessageToTwitch(sessionToken, "Dayoman", requestBody.Message); err != nil {
		log.Printf("Error sending message to Twitch: %v", err)
	}

	// Attempt to send message to YouTube
	// TODO: uncomment this later once we have the secret(s)
	// if err := cacheLiveStreamURL(apiKey, channelID); err == nil {
	// 	if err == nil && liveURL != "" {
	// 		startChatBot(liveURL)
	// 	} else {
	// 		log.Println("No live URL available for YouTube.")
	// 	}
	// } else {
	// 	log.Println("Error caching YouTube live stream URL: ", err)
	// }

	// if chatBot != nil && len(chatBot.ChatWriters) > 0 {
	// 	for _, chatWriter := range chatBot.ChatWriters {
	// 		select {
	// 		case chatWriter <- requestBody.Message:
	// 			log.Println("Message sent to YouTube via ytLiveChatBot:", requestBody.Message)
	// 		case <-time.After(5 * time.Second):
	// 			log.Println("Timeout sending message to YouTube via ytLiveChatBot:", requestBody.Message)
	// 		}
	// 		break // Send to the first stream only for simplicity
	// 	}
	// } else {
	// 	log.Println("ytLiveChatBot is not connected to any YouTube live stream.")
	// }

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Message sent successfully to Twitch and YouTube"))
}

// Helper function to retrieve the username from session data
func getUsernameFromSession(sessionToken string) (string, error) {
	_, sessionData, err := loadSession(ctx, sessionToken)
	if err != nil {
		return "", fmt.Errorf("load session: %w", err)
	}

	// Assuming the username is nested under "data" which is an array of user information
	userData, ok := sessionData["data"].([]any)
	if !ok || len(userData) == 0 {
		return "", fmt.Errorf("user data is not found or is not in the expected format")
	}

	userMap, ok := userData[0].(map[string]any)
	if !ok {
		return "", fmt.Errorf("user data format is incorrect or empty")
	}

	username, ok := userMap["login"].(string)
	if !ok {
		return "", fmt.Errorf("username not found in session data")
	}

	return username, nil
}

// getTwitchOAuthToken retrieves the OAuth token for Twitch from the session data.
func getTwitchOAuthToken(sessionToken string) (string, error) {
	_, sessionData, err := loadSession(ctx, sessionToken)
	if err != nil {
		return "", fmt.Errorf("load session: %w", err)
	}

	// Assuming the token is stored under a "twitch_token" key
	token, ok := sessionData["twitch_token"].(string)
	if !ok {
		return "", fmt.Errorf("twitch OAuth token not found in session data")
	}

	return token, nil
}

// sendMessageToTwitch sends a message to the Twitch chat channel associated with the provided sessionToken.
func sendMessageToTwitch(sessionToken string, channel string, message string) error {
	// Retrieve the OAuth token for Twitch using the session token
	oauthToken, err := getTwitchOAuthToken(sessionToken)
	if err != nil {
		if isSessionNotFound(err) {
			return fmt.Errorf("unauthorized: %w", err)
		}
		return fmt.Errorf("error retrieving Twitch OAuth token: %v", err)
	}

	// Twitch IRC server details
	server := "irc.chat.twitch.tv:6667"
	nickname := "Dayoman" // The streamer's Twitch username

	// Connect to Twitch IRC server
	conn, err := net.Dial("tcp", server)
	if err != nil {
		return fmt.Errorf("error connecting to Twitch IRC: %v", err)
	}
	defer conn.Close()

	// Authenticate and join the channel
	fmt.Fprintf(conn, "PASS oauth:%s\r\n", oauthToken)
	fmt.Fprintf(conn, "NICK %s\r\n", nickname)
	fmt.Fprintf(conn, "JOIN #%s\r\n", channel)

	ircConn := textproto.NewConn(conn)

	// Send the message
	if err := ircConn.PrintfLine("PRIVMSG #%s :%s", channel, message); err != nil {
		return fmt.Errorf("error sending message to Twitch chat: %v", err)
	}

	log.Printf("Message sent to Twitch channel #%s: %s", channel, message)

	return nil
}

// getSessionTokenFromRequest extracts the session token from the request cookies.
func getSessionTokenFromRequest(r *http.Request) (string, error) {
	cookie, err := r.Cookie("session_token")
	if err != nil {
		log.Printf("Session token cookie not found: %v", err)
		return "", fmt.Errorf("session token cookie not found")
	}
	return cookie.Value, nil
}

// Setup function to register the send message route
func SetupSendRoutes(router *mux.Router) {
	// Protect the send-message route with middleware that checks for authenticated session
	authRoutes := router.PathPrefix("/auth").Subrouter()
	authRoutes.HandleFunc("/send-message", sendMessageHandler).Methods("POST")
}
