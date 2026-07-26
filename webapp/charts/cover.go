package charts

import (
	"net/url"
	"regexp"
	"strings"
)

// Chart pages render ~50 covers at ~140px. Platforms hand out originals that
// can exceed 600 KB each, which is ~30 MB per page view through the image
// proxy — far too slow on a small VPS and the reason covers appeared to hang.
// Every platform exposes a size knob in the cover URL, so ask for a thumbnail.
const coverThumbPx = "300"

var (
	appleCoverSize = regexp.MustCompile(`/\d+x\d+(?:bb|cc)?\.`)
	qqCoverSize    = regexp.MustCompile(`R\d+x\d+M`)
	// Matches the "T002" style prefix so a size segment can be inserted.
	qqCoverPrefix  = regexp.MustCompile(`(/T\d{3})M`)
	googleUserSize = regexp.MustCompile(`=w\d+-h\d+.*$`)
)

// thumbnailCoverURL rewrites a full-size cover URL to a ~300px variant.
// Unknown hosts are returned unchanged so nothing breaks.
func thumbnailCoverURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return raw
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case strings.HasSuffix(host, "music.126.net"), strings.HasSuffix(host, "music.163.com"):
		q := parsed.Query()
		q.Set("param", coverThumbPx+"y"+coverThumbPx)
		parsed.RawQuery = q.Encode()
	case strings.HasSuffix(host, "mzstatic.com"):
		parsed.Path = appleCoverSize.ReplaceAllString(parsed.Path, "/"+coverThumbPx+"x"+coverThumbPx+"bb.")
	case strings.HasSuffix(host, "y.qq.com"), strings.HasSuffix(host, "gtimg.cn"):
		// QQ encodes the size inside the filename as "T002R<W>x<H>M...".
		// Some URLs arrive without the R…M segment (e.g. "T002M000..."), and
		// those serve a multi-MB original — bigger than the "full size" one —
		// so insert the segment when it is missing instead of only replacing.
		if qqCoverSize.MatchString(parsed.Path) {
			parsed.Path = qqCoverSize.ReplaceAllString(parsed.Path, "R"+coverThumbPx+"x"+coverThumbPx+"M")
		} else {
			parsed.Path = qqCoverPrefix.ReplaceAllString(parsed.Path,
				"${1}R"+coverThumbPx+"x"+coverThumbPx+"M")
		}
	case strings.HasSuffix(host, "googleusercontent.com"), strings.HasSuffix(host, "ytimg.com"):
		parsed.Path = googleUserSize.ReplaceAllString(parsed.Path, "=w"+coverThumbPx+"-h"+coverThumbPx)
	}
	return parsed.String()
}
