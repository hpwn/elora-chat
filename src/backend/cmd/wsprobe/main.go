package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

type chatEnvelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type chatPayload struct {
	Author  string `json:"author"`
	Message string `json:"message"`
	Source  string `json:"source"`
	Colour  string `json:"colour"`
}

type clientConfig struct {
	pongWait  time.Duration
	writeWait time.Duration
	maxBytes  int64
}

func main() {
	wsURLFlag := flag.String("ws-url", "", "WebSocket endpoint to connect to")
	platformFlag := flag.String("platform", "", "Filter messages by platform (optional)")
	limitFlag := flag.Int("limit", 0, "Stop after N messages (0 = unlimited)")
	timeoutFlag := flag.Duration("timeout", 90*time.Second, "Maximum inactivity before exit")
	flag.Parse()

	wsURL := strings.TrimSpace(*wsURLFlag)
	if wsURL == "" {
		wsURL = strings.TrimSpace(os.Getenv("VITE_PUBLIC_WS_URL"))
	}
	if wsURL == "" {
		wsURL = fmt.Sprintf("ws://localhost:%s/ws/chat", defaultHTTPPort())
	}

	log.Printf("wsprobe: dialing %s", wsURL)

	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 15 * time.Second,
	}

	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		if resp != nil {
			log.Fatalf("wsprobe: dial error: %v (status: %s)", err, resp.Status)
		}
		log.Fatalf("wsprobe: dial error: %v", err)
	}
	defer conn.Close()

	cfg := loadClientConfig()
	applyClientTuning(conn, cfg)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-interrupt
		log.Println("wsprobe: interrupt received, closing connection")
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"))
		_ = conn.Close()
		os.Exit(0)
	}()

	platformFilter := strings.TrimSpace(*platformFlag)
	inactivity := *timeoutFlag
	if inactivity <= 0 {
		inactivity = 90 * time.Second
	}

	messagesSeen := 0

	for {
		wait, err := setReadDeadline(conn, cfg, inactivity)
		if err != nil {
			log.Fatalf("wsprobe: failed to set read deadline: %v", err)
		}

		msgType, data, err := conn.ReadMessage()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				log.Printf("wsprobe: inactivity threshold reached after %s", wait)
				return
			}
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Fatalf("wsprobe: read error: %v", err)
			}
			log.Printf("wsprobe: connection closed: %v", err)
			return
		}

		payload, ok := decodePayload(msgType, data)
		if !ok {
			continue
		}

		if platformFilter != "" && !strings.EqualFold(payload.Source, platformFilter) {
			continue
		}

		if payload.Author == "" && payload.Message == "" {
			continue
		}

		fmt.Printf("[%s] %s: %s\n", payload.Source, payload.Author, payload.Message)
		messagesSeen++
		if *limitFlag > 0 && messagesSeen >= *limitFlag {
			return
		}
	}
}

func decodePayload(msgType int, data []byte) (chatPayload, bool) {
	if msgType != websocket.TextMessage {
		return chatPayload{}, false
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "__keepalive__" {
		return chatPayload{}, false
	}

	raw := data
	if strings.HasPrefix(trimmed, "{") {
		var env chatEnvelope
		if err := json.Unmarshal(data, &env); err == nil && len(env.Data) > 0 {
			raw = env.Data
		}
	}

	var payload chatPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("wsprobe: decode error: %v", err)
		return chatPayload{}, false
	}
	if payload.Source == "" {
		payload.Source = "unknown"
	}
	return payload, true
}

func defaultHTTPPort() string {
	if port := strings.TrimSpace(os.Getenv("ELORA_HTTP_PORT")); port != "" {
		return port
	}
	if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
		return port
	}
	return "8080"
}

func loadClientConfig() clientConfig {
	return clientConfig{
		pongWait:  durationFromEnv("ELORA_WS_PONG_WAIT_MS", 30*time.Second),
		writeWait: durationFromEnv("ELORA_WS_WRITE_DEADLINE_MS", 5*time.Second),
		maxBytes:  int64FromEnv("ELORA_WS_MAX_MESSAGE_BYTES", 131072),
	}
}

func applyClientTuning(conn *websocket.Conn, cfg clientConfig) {
	if cfg.maxBytes > 0 {
		conn.SetReadLimit(cfg.maxBytes)
	}
	if cfg.pongWait > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(cfg.pongWait))
	}
	conn.SetPongHandler(func(string) error {
		if cfg.pongWait > 0 {
			return conn.SetReadDeadline(time.Now().Add(cfg.pongWait))
		}
		return nil
	})
	conn.SetPingHandler(func(appData string) error {
		deadline := time.Now().Add(cfg.writeWait)
		if cfg.writeWait <= 0 {
			deadline = time.Now().Add(5 * time.Second)
		}
		return conn.WriteControl(websocket.PongMessage, []byte(appData), deadline)
	})
}

func setReadDeadline(conn *websocket.Conn, cfg clientConfig, inactivity time.Duration) (time.Duration, error) {
	wait := inactivity
	if cfg.pongWait > 0 && (wait <= 0 || cfg.pongWait < wait) {
		wait = cfg.pongWait
	}
	if wait <= 0 {
		return wait, conn.SetReadDeadline(time.Time{})
	}
	return wait, conn.SetReadDeadline(time.Now().Add(wait))
}

func durationFromEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	if strings.ContainsAny(raw, "hms") {
		if value, err := time.ParseDuration(raw); err == nil {
			return value
		}
	}
	if ms, err := strconv.Atoi(raw); err == nil {
		return time.Duration(ms) * time.Millisecond
	}
	return fallback
}

func int64FromEnv(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return value
}
