package ws

// ChatPayload represents the JSON payload delivered over WebSocket chat frames.
type ChatPayload struct {
	Author        string `json:"author"`
	Message       string `json:"message"`
	Fragments     []any  `json:"fragments"`
	Emotes        []any  `json:"emotes"`
	Badges        []any  `json:"badges"`
	BadgesRaw     any    `json:"badges_raw,omitempty"`
	Source        string `json:"source"`
	Colour        string `json:"colour"`
	UsernameColor string `json:"username_color,omitempty"`
}
