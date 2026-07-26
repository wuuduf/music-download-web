package util

import "net/url"

// RedactURL strips the query string (and any userinfo) from a URL so it is safe
// to log. Platform download URLs routinely embed time-limited credentials in
// query parameters (QQ Music vkey, signed CDN params, tokens); logging the full
// URL leaks those secrets into log files. The scheme, host and path are kept
// for troubleshooting, with the query replaced by a marker when present.
func RedactURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		// Unparseable input might itself be a token; never echo it verbatim.
		return "[unparsable-url]"
	}
	parsed.User = nil
	if parsed.RawQuery != "" || parsed.ForceQuery {
		parsed.RawQuery = "REDACTED"
		parsed.ForceQuery = false
	}
	parsed.Fragment = ""
	return parsed.String()
}
