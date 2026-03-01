package authutil

import "testing"

func TestExtractTwitchToken(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{name: "top-level", json: `{"twitch_token":"abc123"}`, want: "abc123"},
		{name: "nested_access_token", json: `{"providers":{"twitch":{"access_token":"xyz"}}}`, want: "xyz"},
		{name: "nested_token", json: `{"providers":{"twitch":{"token":"tok"}}}`, want: "tok"},
		{name: "missing", json: `{"service":"youtube"}`, want: ""},
		{name: "invalid_json", json: `{"providers":1`, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractTwitchToken([]byte(tc.json))
			if got != tc.want {
				t.Fatalf("ExtractTwitchToken(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}
