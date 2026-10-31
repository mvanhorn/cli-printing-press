package pipeline

import (
	"net"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
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
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil {
		if !ip.IsGlobalUnicast() || ip.IsPrivate() {
			return ""
		}
		return raw
	}
	// Browsers resolve shortened, decimal, and hex IPv4 forms such as 127.1 or
	// 2130706433. A real domain's top-level label starts with a letter.
	tld, _ := utf8.DecodeRuneInString(host[strings.LastIndex(host, ".")+1:])
	if !unicode.IsLetter(tld) {
		return ""
	}
	return raw
}
