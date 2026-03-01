package routes

import "testing"

func TestRawJSONLooksLikeChatMessage(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{
			name: "youtube provider payload",
			raw:  `{"authorName":{"simpleText":"@theps1jerseydevil"},"message":{"runs":[{"text":"Yea jersey devil fr"}]},"timestampUsec":"1770849214407745"}`,
			want: false,
		},
		{
			name: "elora chat payload",
			raw:  `{"author":"@theps1jerseydevil","message":"Yea jersey devil fr","fragments":[],"emotes":[],"badges":[],"source":"YouTube","colour":""}`,
			want: true,
		},
		{
			name: "random json",
			raw:  `{"foo":"bar","count":2}`,
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := rawJSONLooksLikeChatMessage(tt.raw)
			if got != tt.want {
				t.Fatalf("rawJSONLooksLikeChatMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}
