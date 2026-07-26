package server

import "testing"

func TestNormalizeCookieInput(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain cookie header unchanged",
			in:   "MUSIC_U=abc123; __csrf=deadbeef",
			want: "MUSIC_U=abc123; __csrf=deadbeef",
		},
		{
			name: "single value unchanged",
			in:   "abc123",
			want: "abc123",
		},
		{
			name: "netscape multi-line",
			in: "# Netscape HTTP Cookie File\n" +
				".music.163.com\tTRUE\t/\tFALSE\t1800639353\tMUSIC_U\tabc123\n" +
				".music.163.com\tTRUE\t/\tFALSE\t1786383363\t__csrf\tdeadbeef\n",
			want: "MUSIC_U=abc123; __csrf=deadbeef",
		},
		{
			name: "httponly marker stripped",
			in:   "#HttpOnly_.music.163.com\tTRUE\t/\tTRUE\t1800639353\tMUSIC_U\txyz",
			want: "MUSIC_U=xyz",
		},
		{
			name: "crlf and blank lines",
			in:   "\r\n.music.163.com\tTRUE\t/\tFALSE\t0\tHMACCOUNT\thm1\r\n\r\n",
			want: "HMACCOUNT=hm1",
		},
		{
			name: "malformed netscape falls back to raw",
			in:   "not\tenough\tfields",
			want: "not\tenough\tfields",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeCookieInput(tc.in); got != tc.want {
				t.Errorf("normalizeCookieInput() = %q, want %q", got, tc.want)
			}
		})
	}
}
