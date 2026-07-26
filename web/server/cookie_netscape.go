package server

import "strings"

// normalizeCookieInput accepts either a browser "name=value; name2=value2"
// Cookie header or a Netscape cookies.txt export (as produced by the
// "Get cookies.txt" / cookies.txt browser extensions) and returns a
// "name=value; ..." string that the per-platform cookie importers understand.
// Non-Netscape input (no tabs) is returned unchanged, so pasting a plain Cookie
// header keeps working exactly as before.
//
// Netscape format is one tab-separated record per line:
//
//	domain  includeSubdomains  path  secure  expiry  name  value
//
// Lines beginning with '#' are comments, except the "#HttpOnly_" marker some
// exporters prepend to the domain of HttpOnly cookies.
func normalizeCookieInput(raw string) string {
	if !strings.Contains(raw, "\t") {
		return raw
	}
	pairs := make([]string, 0, 16)
	netscape := false
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(strings.TrimSpace(line), "\r")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#HttpOnly_") {
			line = strings.TrimPrefix(line, "#HttpOnly_")
		} else if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			continue
		}
		netscape = true
		name := strings.TrimSpace(fields[5])
		if name == "" {
			continue
		}
		pairs = append(pairs, name+"="+strings.TrimSpace(fields[6]))
	}
	if netscape && len(pairs) > 0 {
		return strings.Join(pairs, "; ")
	}
	return raw
}
