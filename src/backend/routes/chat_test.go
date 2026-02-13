package routes

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/hpwn/EloraChat/src/backend/internal/storage"
)

func TestBadgeUnmarshalJSONCompat(t *testing.T) {
	var badges []Badge
	payload := []byte(`[{"name":"moderator","badge_version":"2"},{"badge_id":"subscriber","version":"12"}]`)
	if err := json.Unmarshal(payload, &badges); err != nil {
		t.Fatalf("failed to unmarshal badges: %v", err)
	}
	if len(badges) != 2 {
		t.Fatalf("expected 2 badges, got %d", len(badges))
	}
	if badges[0].ID != "moderator" || badges[0].Version != "2" {
		t.Fatalf("unexpected first badge: %#v", badges[0])
	}
	if badges[1].ID != "subscriber" || badges[1].Version != "12" {
		t.Fatalf("unexpected second badge: %#v", badges[1])
	}

	payload = []byte(`["vip/1","founder"]`)
	badges = nil
	if err := json.Unmarshal(payload, &badges); err != nil {
		t.Fatalf("failed to unmarshal string badges: %v", err)
	}
	if len(badges) != 2 {
		t.Fatalf("expected 2 string badges, got %d", len(badges))
	}
	if badges[0].ID != "vip" || badges[0].Version != "1" {
		t.Fatalf("unexpected first string badge: %#v", badges[0])
	}
	if badges[1].ID != "founder" || badges[1].Version != "" {
		t.Fatalf("unexpected second string badge: %#v", badges[1])
	}
}

func TestMessagePayloadFromStorageFallback(t *testing.T) {
	userColorMap["tester"] = "#112233"

	payload, err := messagePayloadFromStorage(storage.Message{
		Username:   "tester",
		Text:       "hello",
		Platform:   "Twitch",
		BadgesJSON: "[]",
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
	if msg.Colour != sanitizeUsernameColorForDarkBG("#112233") {
		t.Fatalf("expected colour %q, got %q", sanitizeUsernameColorForDarkBG("#112233"), msg.Colour)
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

func TestMessagePayloadFromStorageBadges(t *testing.T) {
	payload, err := messagePayloadFromStorage(storage.Message{
		Username:   "tester",
		Text:       "hello",
		Platform:   "Twitch",
		BadgesJSON: `{"not":"array"}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if len(msg.Badges) != 0 {
		t.Fatalf("expected malformed badges to be ignored, got %#v", msg.Badges)
	}

	payload, err = messagePayloadFromStorage(storage.Message{
		Username:   "tester",
		Text:       "hello",
		Platform:   "Twitch",
		BadgesJSON: ` [ "subscriber/42" , "bits/100" , "vip" ] `,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	want := []Badge{{ID: "subscriber", Platform: "twitch", Version: "42"}, {ID: "bits", Platform: "twitch", Version: "100"}, {ID: "vip", Platform: "twitch"}}
	if len(msg.Badges) != len(want) {
		t.Fatalf("expected %d badges, got %d", len(want), len(msg.Badges))
	}
	for i, badge := range msg.Badges {
		if badge.ID != want[i].ID || badge.Version != want[i].Version || badge.Platform != want[i].Platform {
			t.Fatalf("badge[%d] mismatch: got %#v want %#v", i, badge, want[i])
		}
	}

	payload, err = messagePayloadFromStorage(storage.Message{
		Username:   "tester",
		Text:       "hello",
		Platform:   "Twitch",
		BadgesJSON: `{"badges":[{"platform":"twitch","id":"subscriber","version":"17"},{"platform":"twitch","id":"premium","version":"1"}],"raw":{"t1":true}}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if len(msg.Badges) != 2 {
		t.Fatalf("expected 2 badges, got %d", len(msg.Badges))
	}
	if msg.Badges[0].Platform != "twitch" || msg.Badges[0].ID != "subscriber" || msg.Badges[0].Version != "17" {
		t.Fatalf("unexpected first badge: %#v", msg.Badges[0])
	}
	if msg.Badges[1].Platform != "twitch" || msg.Badges[1].ID != "premium" || msg.Badges[1].Version != "1" {
		t.Fatalf("unexpected second badge: %#v", msg.Badges[1])
	}
	if msg.BadgesRaw == nil {
		t.Fatalf("expected badges_raw to be populated")
	}
}

func TestParseStoredBadgesPrefersTwitchSubscriberVersion(t *testing.T) {
	raw := `{"badges":[{"platform":"twitch","id":"subscriber","version":"19"},{"platform":"twitch","id":"premium","version":"1"}],"raw":{"twitch":{"badges":"subscriber/12,premium/1","badge_info":"subscriber/19"}}}`
	badges, rawAny := parseStoredBadges(raw)
	if badges == nil {
		t.Fatalf("expected badges to be parsed")
	}
	if rawAny == nil {
		t.Fatalf("expected raw payload to be parsed")
	}
	if len(badges) != 2 {
		t.Fatalf("expected 2 badges, got %d", len(badges))
	}
	if badges[0].ID != "subscriber" || badges[0].Platform != "twitch" || badges[0].Version != "12" {
		t.Fatalf("unexpected subscriber badge: %#v", badges[0])
	}
	if badges[1].ID != "premium" || badges[1].Platform != "twitch" || badges[1].Version != "1" {
		t.Fatalf("unexpected premium badge: %#v", badges[1])
	}
}

func TestMessagePayloadFromStorageRawBadges(t *testing.T) {
	payload, err := messagePayloadFromStorage(storage.Message{
		Username:   "tester",
		Text:       "hello",
		Platform:   "Twitch",
		RawJSON:    `{"author":"tester","message":"hello","fragments":[],"emotes":[],"source":"Twitch"}`,
		BadgesJSON: `{"badges":[{"platform":"twitch","id":"subscriber","version":"17"},{"platform":"twitch","id":"premium","version":"1"}],"raw":{"extra":123}}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if len(msg.Badges) != 2 {
		t.Fatalf("expected 2 badges, got %d", len(msg.Badges))
	}
	if msg.BadgesRaw == nil {
		t.Fatalf("expected badges_raw to be propagated")
	}
	if msg.Badges[0].ID != "subscriber" || msg.Badges[0].Platform != "twitch" || msg.Badges[0].Version != "17" {
		t.Fatalf("unexpected badge[0]: %#v", msg.Badges[0])
	}
	if msg.Badges[1].ID != "premium" || msg.Badges[1].Platform != "twitch" || msg.Badges[1].Version != "1" {
		t.Fatalf("unexpected badge[1]: %#v", msg.Badges[1])
	}
}

func TestMessagePayloadFromStorageOverridesEmptyRawBadges(t *testing.T) {
	payload, err := messagePayloadFromStorage(storage.Message{
		Username:   "tester",
		Text:       "hello",
		Platform:   "Twitch",
		RawJSON:    `{"author":"tester","message":"hello","badges":[],"source":"Twitch"}`,
		BadgesJSON: `{"badges":[{"platform":"twitch","id":"subscriber","version":"17"},{"platform":"twitch","id":"premium","version":"1"}],"raw":{"extra":123}}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if len(msg.Badges) != 2 {
		t.Fatalf("expected 2 badges, got %d", len(msg.Badges))
	}
	if msg.BadgesRaw == nil {
		t.Fatalf("expected badges_raw to be propagated")
	}
	if msg.Badges[0].ID != "subscriber" || msg.Badges[0].Platform != "twitch" || msg.Badges[0].Version != "17" {
		t.Fatalf("unexpected badge[0]: %#v", msg.Badges[0])
	}
	if msg.Badges[1].ID != "premium" || msg.Badges[1].Platform != "twitch" || msg.Badges[1].Version != "1" {
		t.Fatalf("unexpected badge[1]: %#v", msg.Badges[1])
	}
}

func TestMessagePayloadFromStoragePreservesBadgeImages(t *testing.T) {
	payload, err := messagePayloadFromStorage(storage.Message{
		Username:   "tester",
		Text:       "hello",
		Platform:   "Twitch",
		BadgesJSON: `{"badges":[{"platform":"twitch","id":"subscriber","version":"2","images":[{"url":"https://static.twitchcdn.net/badges/v1/subscriber_1x.png","width":18,"height":18},{"url":"https://static.twitchcdn.net/badges/v1/subscriber_2x.png","width":36,"height":36}]}]}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var msg struct {
		Badges []Badge `json:"badges"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if len(msg.Badges) != 1 {
		t.Fatalf("expected 1 badge, got %d", len(msg.Badges))
	}
	badge := msg.Badges[0]
	if badge.ID != "subscriber" || badge.Platform != "twitch" || badge.Version != "2" {
		t.Fatalf("unexpected badge metadata: %#v", badge)
	}
	if len(badge.Images) != 2 {
		t.Fatalf("expected 2 badge images, got %d", len(badge.Images))
	}
	if badge.Images[0].URL != "https://static.twitchcdn.net/badges/v1/subscriber_1x.png" || badge.Images[1].URL != "https://static.twitchcdn.net/badges/v1/subscriber_2x.png" {
		t.Fatalf("unexpected badge images: %#v", badge.Images)
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
	t.Setenv("ELORA_WS_ENVELOPE", "off")
	raw := maybeEnvelope(payload)
	if string(raw) != string(payload) {
		t.Fatalf("expected raw payload when envelope disabled, got %s", string(raw))
	}

	// Default (unset) should enable the envelope.
	if err := os.Unsetenv("ELORA_WS_ENVELOPE"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	enveloped = maybeEnvelope(payload)
	if bytes.Equal(enveloped, payload) {
		t.Fatalf("expected envelope to be applied by default")
	}
}

func TestSanitizeMessagePayloadDrop(t *testing.T) {
	if err := os.Unsetenv("ELORA_WS_DROP_EMPTY"); err != nil {
		t.Fatalf("failed to unset drop env: %v", err)
	}

	payload := []byte(`{"author":"tester","message":"   ","fragments":[],"emotes":[],"badges":[],"source":""}`)
	if _, err := sanitizeMessagePayload(payload); !errors.Is(err, errDropMessage) {
		t.Fatalf("expected errDropMessage, got %v", err)
	}
}

func TestSanitizeMessagePayloadNormalizes(t *testing.T) {
	t.Setenv("ELORA_WS_DROP_EMPTY", "false")

	payload := []byte(`{"author":"tester","message":" hello ","fragments":null,"emotes":null,"badges":null,"source":" twitch "}`)
	sanitized, err := sanitizeMessagePayload(payload)
	if err != nil {
		t.Fatalf("sanitizeMessagePayload returned error: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(sanitized, &msg); err != nil {
		t.Fatalf("failed to unmarshal sanitized payload: %v", err)
	}

	if msg.Source != "Twitch" {
		t.Fatalf("expected source to normalize to Twitch, got %q", msg.Source)
	}
	if msg.Message != "hello" {
		t.Fatalf("expected message to be trimmed to 'hello', got %q", msg.Message)
	}
	if msg.Tokens == nil {
		t.Fatalf("expected tokens slice to be initialized")
	}
	if msg.Emotes == nil {
		t.Fatalf("expected emotes slice to be initialized")
	}
	if msg.Badges == nil {
		t.Fatalf("expected badges slice to be initialized")
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

func TestBroadcastFromTailerConvertsLegacyBadges(t *testing.T) {
	tokenizer.TextEffectSep = ':'
	tokenizer.TextCommandPrefix = '!'
	tokenizer.EmoteCache = make(map[string]Emote)

	subscribersMu.Lock()
	subscribers = nil
	subscribersMu.Unlock()

	ch := addSubscriber()
	defer removeSubscriber(ch)

	BroadcastFromTailer(storage.Message{
		Username:   "BadgeUser",
		Platform:   "Twitch",
		Text:       "hello",
		BadgesJSON: ` [ "subscriber/12" , "premium/1" , "subscriber/17" ] `,
	})

	select {
	case payload := <-ch:
		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("failed to unmarshal broadcast payload: %v", err)
		}

		if len(msg.Badges) != 3 {
			t.Fatalf("expected 3 badges, got %d", len(msg.Badges))
		}
		if msg.Badges[0].ID != "subscriber" || msg.Badges[0].Platform != "twitch" || msg.Badges[0].Version != "12" {
			t.Fatalf("unexpected first badge: %#v", msg.Badges[0])
		}
		if msg.Badges[1].ID != "premium" || msg.Badges[1].Platform != "twitch" || msg.Badges[1].Version != "1" {
			t.Fatalf("unexpected second badge: %#v", msg.Badges[1])
		}
		if msg.Badges[2].ID != "subscriber" || msg.Badges[2].Platform != "twitch" || msg.Badges[2].Version != "17" {
			t.Fatalf("unexpected third badge: %#v", msg.Badges[2])
		}
		if msg.BadgesRaw != nil {
			t.Fatalf("expected badges_raw to be empty")
		}
	default:
		t.Fatalf("expected broadcast payload but channel was empty")
	}
}

func TestBroadcastFromTailerStructuredBadgesAndRaw(t *testing.T) {
	tokenizer.TextEffectSep = ':'
	tokenizer.TextCommandPrefix = '!'
	tokenizer.EmoteCache = make(map[string]Emote)

	subscribersMu.Lock()
	subscribers = nil
	subscribersMu.Unlock()

	ch := addSubscriber()
	defer removeSubscriber(ch)

	BroadcastFromTailer(storage.Message{
		Username:   "BadgeUser",
		Platform:   "Twitch",
		Text:       "hi",
		RawJSON:    `{"author":"BadgeUser","message":"hi","badges":[],"source":"Twitch"}`,
		BadgesJSON: `{"badges":[{"platform":"twitch","id":"subscriber","version":"17"},{"platform":"twitch","id":"premium","version":"1"}],"raw":{"twitch":{"badge_info":"subscriber/17","badges":"subscriber/12,premium/1"}}}`,
	})

	select {
	case payload := <-ch:
		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("failed to unmarshal broadcast payload: %v", err)
		}

		if len(msg.Badges) != 2 {
			t.Fatalf("expected 2 badges, got %d", len(msg.Badges))
		}
		if msg.BadgesRaw == nil {
			t.Fatalf("expected badges_raw to be populated")
		}
		if msg.Badges[0].ID != "subscriber" || msg.Badges[0].Platform != "twitch" || msg.Badges[0].Version != "12" {
			t.Fatalf("unexpected first badge: %#v", msg.Badges[0])
		}
		if msg.Badges[1].ID != "premium" || msg.Badges[1].Platform != "twitch" || msg.Badges[1].Version != "1" {
			t.Fatalf("unexpected second badge: %#v", msg.Badges[1])
		}
	default:
		t.Fatalf("expected broadcast payload but channel was empty")
	}
}

func TestBroadcastFromTailerDropsYouTubeOwnerBadgeWithoutImages(t *testing.T) {
	tokenizer.TextEffectSep = ':'
	tokenizer.TextCommandPrefix = '!'
	tokenizer.EmoteCache = make(map[string]Emote)

	subscribersMu.Lock()
	subscribers = nil
	subscribersMu.Unlock()

	ch := addSubscriber()
	defer removeSubscriber(ch)

	badgesJSON := `{"badges":[{"platform":"youtube","id":"owner"},{"platform":"youtube","id":"moderator","images":[{"url":"https://example.com/mod.png","width":16,"height":16}]}],"raw":{"youtube":{"badges":[{"id":"owner","title":"Owner"}]}}}`

	BroadcastFromTailer(storage.Message{
		Username:   "YTBadgeUser",
		Platform:   "YouTube",
		Text:       "hi",
		BadgesJSON: badgesJSON,
	})

	select {
	case payload := <-ch:
		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("failed to unmarshal broadcast payload: %v", err)
		}

		if msg.BadgesRaw == nil {
			t.Fatalf("expected badges_raw to be populated")
		}
		if len(msg.Badges) != 1 {
			t.Fatalf("expected 1 badge, got %d", len(msg.Badges))
		}
		if msg.Badges[0].ID != "moderator" || msg.Badges[0].Platform != "youtube" {
			t.Fatalf("unexpected remaining badge: %#v", msg.Badges[0])
		}
	default:
		t.Fatalf("expected broadcast payload but channel was empty")
	}
}

func TestBroadcastFromTailerYouTubeEmoteFragments(t *testing.T) {
	tokenizer.TextEffectSep = ':'
	tokenizer.TextCommandPrefix = '!'
	tokenizer.EmoteCache = make(map[string]Emote)

	subscribersMu.Lock()
	subscribers = nil
	subscribersMu.Unlock()

	ch := addSubscriber()
	defer removeSubscriber(ch)

	emotesJSON := `[
		{"id":"a","name":":a:","locations":["0-2"],"images":[{"url":"https://example.com/a.png","width":24,"height":24}]},
		{"id":"b","name":":b:","locations":["3-5"],"images":[{"url":"https://example.com/b.png","width":24,"height":24}]}
	]`

	BroadcastFromTailer(storage.Message{
		Username:   "YTUser",
		Platform:   "YouTube",
		Text:       ":a::b:",
		EmotesJSON: emotesJSON,
	})

	select {
	case payload := <-ch:
		sanitized, err := sanitizeMessagePayload(payload)
		if err != nil {
			t.Fatalf("sanitizeMessagePayload returned error: %v", err)
		}
		var msg Message
		if err := json.Unmarshal(sanitized, &msg); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}

		emoteCount := 0
		for _, token := range msg.Tokens {
			if token.Type == TokenTypeEmote {
				emoteCount++
				if len(token.Emote.Images) == 0 || token.Emote.Images[0].URL == "" {
					t.Fatalf("expected emote images to be populated, got %#v", token.Emote.Images)
				}
			}
		}
		if emoteCount != 2 {
			t.Fatalf("expected 2 emote fragments, got %d", emoteCount)
		}
	default:
		t.Fatalf("expected broadcast payload but channel was empty")
	}
}

func TestComputeUsernameColorTwitchExtractionAndFallback(t *testing.T) {
	row := storage.Message{
		Username: "tw-user",
		Platform: "twitch",
		RawJSON:  `{"tags":{"color":"#33CC66"}}`,
	}
	msg := Message{Author: "tw-user", Source: "twitch"}
	if got := computeUsernameColor(msg, row); got != "#33CC66" {
		t.Fatalf("expected twitch color extraction, got %q", got)
	}

	row.RawJSON = `{"tags":{"color":""}}`
	if got := computeUsernameColor(msg, row); got != sanitizeUsernameColorForDarkBG(colorFromName("tw-user")) {
		t.Fatalf("expected fallback color for empty twitch color, got %q", got)
	}

	row.RawJSON = `{"foo":"bar"}`
	if got := computeUsernameColor(msg, row); got != sanitizeUsernameColorForDarkBG(colorFromName("tw-user")) {
		t.Fatalf("expected fallback color for missing twitch color, got %q", got)
	}
}

func TestComputeUsernameColorYouTubeRoleOverridesFallback(t *testing.T) {
	base := Message{Author: "yt-role", Source: "youtube"}

	member := storage.Message{
		Username: "yt-role",
		Platform: "youtube",
		RawJSON:  `{"isChatSponsor":true}`,
	}
	if got := computeUsernameColor(base, member); got != youtubeMemberColour {
		t.Fatalf("expected youtube member color %q, got %q", youtubeMemberColour, got)
	}

	mod := storage.Message{
		Username: "yt-role",
		Platform: "youtube",
		RawJSON:  `{"isChatModerator":true}`,
	}
	if got := computeUsernameColor(base, mod); got != youtubeModColour {
		t.Fatalf("expected youtube moderator color %q, got %q", youtubeModColour, got)
	}

	owner := storage.Message{
		Username: "yt-role",
		Platform: "youtube",
		RawJSON:  `{"author":{"isChatOwner":true}}`,
	}
	if got := computeUsernameColor(base, owner); got != youtubeOwnerColour {
		t.Fatalf("expected youtube owner color %q, got %q", youtubeOwnerColour, got)
	}
}

func TestComputeUsernameColorInvalidTwitchColorFallsBack(t *testing.T) {
	row := storage.Message{
		Username: "tw-invalid",
		Platform: "twitch",
		RawJSON:  `{"color":"#12GGFF"}`,
	}
	msg := Message{Author: "tw-invalid", Source: "twitch"}
	want := sanitizeUsernameColorForDarkBG(colorFromName("tw-invalid"))
	if got := computeUsernameColor(msg, row); got != want {
		t.Fatalf("expected fallback color %q for invalid twitch color, got %q", want, got)
	}
}

func TestComputeUsernameColorDarkTwitchColorSanitized(t *testing.T) {
	row := storage.Message{
		Username: "tw-dark",
		Platform: "twitch",
		RawJSON:  `{"tags":{"color":"#000000"}}`,
	}
	msg := Message{Author: "tw-dark", Source: "twitch"}
	got := computeUsernameColor(msg, row)

	if got == "#000000" {
		t.Fatalf("expected dark twitch color to be sanitized, got %q", got)
	}
	if !hexUsernameColourRe.MatchString(got) {
		t.Fatalf("expected sanitized color to be valid hex, got %q", got)
	}
	r, g, b, ok := parseHexRGB(got)
	if !ok {
		t.Fatalf("expected parseHexRGB to parse %q", got)
	}
	if usernameColourRelativeLuminance(r, g, b) < usernameColourDarkBGMinLuminance {
		t.Fatalf("expected sanitized color %q to be readable on dark bg", got)
	}
	if strings.ToUpper(got) != got {
		t.Fatalf("expected sanitized color to be uppercase hex, got %q", got)
	}
}
