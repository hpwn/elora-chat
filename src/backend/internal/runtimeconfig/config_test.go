package runtimeconfig

import "testing"

func TestNormalizeTwitchChannelVariants(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "login", input: "dagnel", want: "dagnel"},
		{name: "at login", input: "@dagnel", want: "dagnel"},
		{name: "schemeless url", input: "twitch.tv/dagnel", want: "dagnel"},
		{name: "absolute url", input: "https://www.twitch.tv/dagnel", want: "dagnel"},
	}

	for _, tt := range tests {
		got, err := normalizeTwitchChannel(tt.input)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
		if got != tt.want {
			t.Fatalf("%s: expected %q, got %q", tt.name, tt.want, got)
		}
	}
}

func TestNormalizeYouTubeVariants(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "handle", input: "lofigirl", want: "https://www.youtube.com/@lofigirl/live"},
		{name: "at handle", input: "@lofigirl", want: "https://www.youtube.com/@lofigirl/live"},
		{name: "schemeless live", input: "youtube.com/@lofigirl/live", want: "https://www.youtube.com/@lofigirl/live"},
		{name: "absolute live", input: "https://www.youtube.com/@lofigirl/live", want: "https://www.youtube.com/@lofigirl/live"},
		{name: "watch url", input: "https://www.youtube.com/watch?v=abcdefghijk", want: "https://www.youtube.com/watch?v=abcdefghijk"},
		{name: "youtu short", input: "https://youtu.be/abcdefghijk", want: "https://www.youtube.com/watch?v=abcdefghijk"},
		{name: "video id", input: "abcdefghijk", want: "https://www.youtube.com/watch?v=abcdefghijk"},
	}

	for _, tt := range tests {
		got, err := normalizeYouTubeURL(tt.input)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
		if got != tt.want {
			t.Fatalf("%s: expected %q, got %q", tt.name, tt.want, got)
		}
	}
}

func TestNormalizeYouTubeAddsSchemeForSchemelessURL(t *testing.T) {
	got, err := normalizeYouTubeURL("youtube.com/watch?v=abcdefghijk")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://www.youtube.com/watch?v=abcdefghijk" {
		t.Fatalf("expected canonical watch URL, got %q", got)
	}
}

func TestNormalizeReturnsFieldSpecificErrorsForEmptyAndMalformed(t *testing.T) {
	_, twitchEmptyErr := normalizeTwitchChannel("   ")
	if twitchEmptyErr == nil {
		t.Fatalf("expected twitch empty error")
	}

	_, ytEmptyErr := normalizeYouTubeURL("   ")
	if ytEmptyErr == nil {
		t.Fatalf("expected youtube empty error")
	}

	_, ytMalformedErr := normalizeYouTubeURL("https://www.youtube.com/watch?v=bad")
	if ytMalformedErr == nil {
		t.Fatalf("expected malformed youtube URL error")
	}
}
