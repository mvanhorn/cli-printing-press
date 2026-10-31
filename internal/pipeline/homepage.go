package pipeline

import (
	"net"
	"net/url"
	"strings"
)

// NormalizeHomepage returns raw trimmed when it is a plausible public product
// homepage, and "" otherwise. Catalog clients render the value as a favicon
// and an "Open website" link, so a wrong URL is worse than none.
func NormalizeHomepage(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "{}<> ") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return ""
	}
	host := u.Hostname()
	if host == "" || strings.EqualFold(host, "localhost") {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil && (!ip.IsGlobalUnicast() || ip.IsPrivate()) {
		return ""
	}
	return raw
}
