package util

import "testing"

func TestRedactURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no query", "https://cdn.example.com/song.flac", "https://cdn.example.com/song.flac"},
		{"strips query with token", "https://cdn.example.com/song.flac?vkey=SECRET&t=123", "https://cdn.example.com/song.flac?REDACTED"},
		{"strips userinfo", "https://user:pass@cdn.example.com/a", "https://cdn.example.com/a"},
		{"strips fragment", "https://cdn.example.com/a#frag", "https://cdn.example.com/a"},
		{"unparsable", "://not a url\x7f", "[unparsable-url]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RedactURL(tc.in); got != tc.want {
				t.Errorf("RedactURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
