package charts

import "strings"
import "testing"

func TestThumbnailCoverURL(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"netease adds param", "https://p1.music.126.net/abc/123.jpg", "param=300y300"},
		{"apple resizes path", "https://is1-ssl.mzstatic.com/image/thumb/x/3000x3000bb.jpg", "300x300bb."},
		{"qq resizes path", "https://y.gtimg.cn/music/photo_new/T002R300x300M000x.jpg", "R300x300M"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := thumbnailCoverURL(tc.in)
			if !strings.Contains(got, tc.want) {
				t.Errorf("thumbnailCoverURL(%q) = %q, want it to contain %q", tc.in, got, tc.want)
			}
		})
	}
	if got := thumbnailCoverURL(""); got != "" {
		t.Errorf("empty input should stay empty, got %q", got)
	}
	// Unknown hosts must pass through untouched rather than break.
	const unknown = "https://cdn.example.com/cover.jpg"
	if got := thumbnailCoverURL(unknown); got != unknown {
		t.Errorf("unknown host rewritten: %q", got)
	}
}

func TestQQCoverWithoutSizeSegment(t *testing.T) {
	// Regression: URLs like ".../T002M000..." carry no R<W>x<H>M segment. The
	// first implementation left them untouched, so the proxy fetched a ~4.7 MB
	// original — larger than the nominal full-size image.
	got := thumbnailCoverURL("https://y.gtimg.cn/music/photo_new/T002M000000ji8iX05RUPt.jpg")
	if !strings.Contains(got, "R300x300M") {
		t.Errorf("size segment not inserted: %q", got)
	}
}
