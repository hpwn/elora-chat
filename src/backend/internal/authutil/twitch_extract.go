package authutil

import "encoding/json"

// ExtractTwitchToken attempts to locate a Twitch OAuth token from a stored session payload.
// It searches common locations used by various auth flows:
//  1. top-level field: {"twitch_token":"..."}
//  2. nested providers map with access_token
//  3. nested providers map with token
func ExtractTwitchToken(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	if v, ok := payload["twitch_token"].(string); ok && v != "" {
		return v
	}
	providers, ok := payload["providers"].(map[string]any)
	if !ok {
		return ""
	}
	twitch, ok := providers["twitch"].(map[string]any)
	if !ok {
		return ""
	}
	if v, ok := twitch["access_token"].(string); ok && v != "" {
		return v
	}
	if v, ok := twitch["token"].(string); ok && v != "" {
		return v
	}
	return ""
}
