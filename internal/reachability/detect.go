package reachability

import (
	"net/http"
	"strings"
)

// Protection is one detected bot-protection signal in a probe response.
// Label vocabulary matches internal/browsersniff/analysis.go's
// ProtectionObservation labels so probe and post-capture analyzer agree.
type Protection struct {
	Label    string
	Evidence string
}

// classifyResponse returns protection signals detected in the response.
// bodySnippet is expected to be a bounded lowercased read of the
// response body — full body reads would be wasteful for large pages.
func classifyResponse(status int, headers http.Header, bodySnippet string) []Protection {
	var out []Protection
	body := strings.ToLower(bodySnippet)
	h := lowerHeaders(headers)
	server := h["server"]

	if v := h["cf-mitigated"]; v == "challenge" {
		out = append(out, Protection{Label: "bot_challenge", Evidence: "cf-mitigated: challenge"})
	}
	if v := h["x-vercel-mitigated"]; v == "challenge" {
		out = append(out, Protection{Label: "bot_challenge", Evidence: "x-vercel-mitigated: challenge"})
		out = append(out, Protection{Label: "vercel_challenge", Evidence: "x-vercel-mitigated: challenge"})
	}
	if h["x-vercel-challenge-token"] != "" {
		out = append(out, Protection{Label: "vercel_challenge", Evidence: "x-vercel-challenge-token present"})
	}
	if strings.Contains(body, "vercel security checkpoint") {
		out = append(out, Protection{Label: "vercel_challenge", Evidence: "Vercel Security Checkpoint page"})
	}

	if h["aws-waf-token"] != "" || hasHeaderPrefix(h, "x-amzn-waf") ||
		strings.Contains(body, "awswaf") || strings.Contains(body, "aws-waf") {
		out = append(out, Protection{Label: "aws_waf", Evidence: "AWS WAF marker"})
	}

	// CDN fingerprints (cf-ray, server: cloudflare, x-akamai-transformed)
	// are NOT protection signals on their own — Cloudflare and Akamai front
	// huge swaths of the internet, and a normal 200 served through their
	// edge looks identical to a challenge response except for the body and
	// status. Only fire CDN-as-protection when:
	//   (a) status is 4xx/5xx — error response from the CDN, OR
	//   (b) body contains a challenge marker (cf-chl, "just a moment",
	//       "checking your browser", "ddos protection by cloudflare").
	// DataDome and PerimeterX header presence stays a strong signal — those
	// products only ship as bot mitigation, not as plain CDN.
	cfFingerprint := strings.Contains(server, "cloudflare") || h["cf-ray"] != ""
	contentType := h["content-type"]
	htmlBody := strings.Contains(contentType, "html") ||
		strings.Contains(body, "<html") ||
		strings.Contains(body, "<script") ||
		strings.Contains(body, "<div")
	turnstileBody := htmlBody && strings.Contains(body, "challenges.cloudflare.com/turnstile")
	cfChallengeBody := strings.Contains(body, "cf-chl") ||
		turnstileBody ||
		strings.Contains(body, "just a moment") ||
		strings.Contains(body, "checking your browser") ||
		strings.Contains(body, "ddos protection by cloudflare")
	akamaiFingerprint := h["x-akamai-transformed"] != ""

	switch {
	case cfChallengeBody:
		out = append(out, Protection{Label: "cloudflare", Evidence: "Cloudflare challenge marker in body"})
	case cfFingerprint && status >= 400:
		out = append(out, Protection{Label: "cloudflare", Evidence: "Cloudflare error response"})
	case akamaiFingerprint && status >= 400:
		out = append(out, Protection{Label: "akamai", Evidence: "Akamai error response"})
	case h["x-datadome"] != "" || strings.Contains(body, "datadome"):
		out = append(out, Protection{Label: "datadome", Evidence: "DataDome marker"})
	case strings.Contains(body, "perimeterx") || strings.Contains(body, "_px"):
		out = append(out, Protection{Label: "perimeterx", Evidence: "PerimeterX marker"})
	}

	switch {
	case turnstileBody:
		out = append(out, Protection{Label: "captcha", Evidence: "Cloudflare Turnstile interstitial"})
	case strings.Contains(body, "fill out the captcha to unblock"):
		out = append(out, Protection{Label: "captcha", Evidence: "CAPTCHA unblock shell"})
	case strings.Contains(body, "recaptcha") || strings.Contains(body, "hcaptcha") ||
		strings.Contains(body, "g-recaptcha"):
		out = append(out, Protection{Label: "captcha", Evidence: "CAPTCHA widget"})
	}

	if (status == 403 || status == 429) && len(out) == 0 {
		ct := strings.ToLower(headers.Get("Content-Type"))
		if strings.Contains(ct, "html") || strings.Contains(body, "access denied") ||
			strings.Contains(body, "too many requests") {
			out = append(out, Protection{Label: "protected_web", Evidence: "403/429 HTML or access-denied"})
		}
	}

	return out
}

// isClear returns true when the response is a successful, non-protected
// reach. 2xx, 3xx, and 401/403/404 without protection markers all count —
// they prove the URL is reachable; auth and routing are downstream concerns.
func isClear(status int, protections []Protection) bool {
	if len(protections) > 0 {
		return false
	}
	if status >= 200 && status < 400 {
		return true
	}
	// 401 and 404 are reachable: server reached us, transport works,
	// the response is just auth-gated or wrong path. 403 without
	// protection markers is the ambiguous one — we treat it as clear,
	// matching analysis.go's logic that 403 + no protection is just an
	// authz gate rather than a transport problem.
	if status == 401 || status == 403 || status == 404 {
		return true
	}
	return false
}

func lowerHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) == 0 {
			continue
		}
		out[strings.ToLower(k)] = strings.ToLower(v[0])
	}
	return out
}

func hasHeaderPrefix(h map[string]string, prefix string) bool {
	for k := range h {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

func protectionsToEvidence(protections []Protection) []string {
	if len(protections) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range protections {
		if seen[p.Evidence] {
			continue
		}
		seen[p.Evidence] = true
		out = append(out, p.Evidence)
	}
	return out
}
