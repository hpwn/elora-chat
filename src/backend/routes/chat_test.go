package routes

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
)

func TestMessagePayloadFromStorageFallback(t *testing.T) {
	userColorMap["tester"] = "#112233"

	payload, err := messagePayloadFromStorage(storage.Message{
		Username: "tester",
		Text:     "hello",
		Platform: "Twitch",
	})
	if err != nil {
		t.Fatalf("messagePayloadFromStorage returned error: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if msg.Author != "tester" {
		t.Fatalf("expected author to be tester, got %q", msg.Author)
	}
	if msg.Message != "hello" {
		t.Fatalf("expected message 'hello', got %q", msg.Message)
	}
	if msg.Source != "Twitch" {
		t.Fatalf("expected source 'Twitch', got %q", msg.Source)
	}
	if msg.Colour != "#112233" {
		t.Fatalf("expected colour '#112233', got %q", msg.Colour)
	}
	if msg.Tokens == nil || len(msg.Tokens) != 0 {
		t.Fatalf("expected empty tokens slice, got %#v", msg.Tokens)
	}
	if msg.Emotes == nil || len(msg.Emotes) != 0 {
		t.Fatalf("expected empty emotes slice, got %#v", msg.Emotes)
	}
	if msg.Badges == nil || len(msg.Badges) != 0 {
		t.Fatalf("expected empty badges slice, got %#v", msg.Badges)
	}
}

func TestMaybeEnvelope(t *testing.T) {
	t.Setenv("ELORA_WS_ENVELOPE", "true")

	payload := []byte(`{"message":"hi"}`)
	enveloped := maybeEnvelope(payload)

	var env struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}
	if err := json.Unmarshal(enveloped, &env); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}

	if env.Type != "chat" {
		t.Fatalf("expected envelope type 'chat', got %q", env.Type)
	}
	if env.Data != string(payload) {
		t.Fatalf("expected envelope data to equal payload, got %q", env.Data)
	}

	// Ensure disabling the flag returns the payload unchanged.
	if err := os.Unsetenv("ELORA_WS_ENVELOPE"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	raw := maybeEnvelope(payload)
	if string(raw) != string(payload) {
		t.Fatalf("expected raw payload when envelope disabled, got %s", string(raw))
	}
}

func TestEnrichTailerMessage(t *testing.T) {
	tokenizer.TextEffectSep = ':'
	tokenizer.TextCommandPrefix = '!'
	tokenizer.EmoteCache = make(map[string]Emote)

	raw := `{"author":"TailerUser","message":"hello Pog"}`
	msg := enrichTailerMessage(storage.Message{
		Username:   "TailerUser",
		Platform:   "YouTube",
		RawJSON:    raw,
		EmotesJSON: "[]",
	})

	if msg.Author != "TailerUser" {
		t.Fatalf("expected author TailerUser, got %q", msg.Author)
	}
	if msg.Message != "hello Pog" {
		t.Fatalf("expected message 'hello Pog', got %q", msg.Message)
	}
	if msg.Source != "YouTube" {
		t.Fatalf("expected source YouTube, got %q", msg.Source)
	}
	if msg.Colour == "" {
		t.Fatalf("expected colour to be populated")
	}
	if msg.Tokens == nil {
		t.Fatalf("expected tokens slice to be initialized")
	}
	if msg.Badges == nil {
		t.Fatalf("expected badges slice to be initialized")
	}
	if msg.Emotes == nil {
		t.Fatalf("expected emotes slice to be initialized")
	}
}

func TestBroadcastFromTailerEnrichesPayload(t *testing.T) {
	tokenizer.TextEffectSep = ':'
	tokenizer.TextCommandPrefix = '!'
	tokenizer.EmoteCache = make(map[string]Emote)

	subscribersMu.Lock()
	subscribers = nil
	subscribersMu.Unlock()

	ch := addSubscriber()
	defer removeSubscriber(ch)

	BroadcastFromTailer(storage.Message{
		Username: "SampleUser",
		Platform: "Twitch",
		RawJSON:  `{"author":"SampleUser","message":"hi"}`,
	})

	select {
	case payload := <-ch:
		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("failed to unmarshal broadcast payload: %v", err)
		}

		if msg.Colour == "" {
			t.Fatalf("expected colour to be populated")
		}
		if msg.Emotes == nil {
			t.Fatalf("expected emotes slice to be initialized")
		}
		if msg.Badges == nil {
			t.Fatalf("expected badges slice to be initialized")
		}
		if msg.Tokens == nil {
			t.Fatalf("expected tokens slice to be initialized")
		}
	default:
		t.Fatalf("expected broadcast payload but channel was empty")
	}
}
