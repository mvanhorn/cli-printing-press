package browsersniff

import (
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// NormalizeHTTPVersion canonicalizes HAR-recorded httpVersion strings to
// a fixed set of labels: "h3", "h2", "http/1.1", or "" when the input
// is empty or unrecognized. HAR exporters spell HTTP/2 as "h2",
// "HTTP/2", "h2c", or even "HTTP/2.0"; HTTP/3 appears as "h3" or
// "HTTP/3". Normalizing here keeps the distribution map's keys stable
// regardless of which browser captured the trace.
func NormalizeHTTPVersion(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return ""
	}
	switch {
	case v == "h3", strings.HasPrefix(v, "http/3"):
		return "h3"
	case v == "h2", v == "h2c", strings.HasPrefix(v, "http/2"):
		return "h2"
	case strings.HasPrefix(v, "http/1.1"):
		return "http/1.1"
	}
	return ""
}

// browserTransportFromHARVersion picks the HTTPTransport enum value
// from the HAR's HTTP-version distribution. The most-frequent recognized
// version wins; ties go to the higher-version (H/3 > H/2 > H/1.1) so a
// 50/50 H/2-vs-H/3 origin biases toward the stricter wire protocol the
// origin already proved it can serve. An empty distribution returns
// HTTPTransportBrowserChrome (no version force) so callers fall back to
// Chrome's own version negotiation rather than guessing.
func browserTransportFromHARVersion(dist map[string]int) string {
	if len(dist) == 0 {
		return spec.HTTPTransportBrowserChrome
	}
	// Iterate in descending priority order; the first version with a
	// strict majority wins. When two versions tie for the lead, the
	// higher version wins by virtue of being checked first.
	type bucket struct {
		key       string
		transport string
	}
	priority := []bucket{
		{"h3", spec.HTTPTransportBrowserChromeH3},
		{"h2", spec.HTTPTransportBrowserChromeH2},
		{"http/1.1", spec.HTTPTransportBrowserChrome},
	}
	best := bucket{transport: spec.HTTPTransportBrowserChrome}
	bestCount := 0
	for _, b := range priority {
		if c := dist[b.key]; c > bestCount {
			best = b
			bestCount = c
		}
	}
	if bestCount == 0 {
		return spec.HTTPTransportBrowserChrome
	}
	return best.transport
}
