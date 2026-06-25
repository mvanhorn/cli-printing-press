package reachability

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyResponse(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		headers     http.Header
		body        string
		wantLabels  []string
		wantBody    []string
		emptyResult bool
	}{
		{
			name:        "plain 200",
			status:      200,
			headers:     http.Header{"Content-Type": {"application/json"}},
			body:        `{"ok": true}`,
			emptyResult: true,
		},
		{
			name:   "vercel challenge header",
			status: 429,
			headers: http.Header{
				"X-Vercel-Mitigated": {"challenge"},
				"Content-Type":       {"text/html"},
			},
			body:       "<html>Vercel Security Checkpoint</html>",
			wantLabels: []string{"bot_challenge", "vercel_challenge", "vercel_challenge"},
			wantBody:   []string{"vercel_security_checkpoint"},
		},
		{
			name:   "cloudflare cf-mitigated",
			status: 403,
			headers: http.Header{
				"Cf-Mitigated": {"challenge"},
				"Cf-Ray":       {"abc123"},
				"Server":       {"cloudflare"},
			},
			body:       "<html>Just a moment...</html>",
			wantLabels: []string{"bot_challenge", "cloudflare"},
			wantBody:   []string{"cloudflare_challenge"},
		},
		{
			// Regression: cf-ray + server:cloudflare on a normal 200 means
			// Cloudflare CDN, not a challenge. allrecipes.com (and tens of
			// millions of other sites) front through Cloudflare; treating
			// every CDN-fronted 200 as a protection signal would force
			// every Cloudflare-protected site through the cookie-clearance
			// flow even when Surf has already cleared the actual challenge.
			name:   "cloudflare CDN on 200 is not a protection signal",
			status: 200,
			headers: http.Header{
				"Cf-Ray":       {"abc123"},
				"Server":       {"cloudflare"},
				"Content-Type": {"text/html"},
			},
			body:        "<html>recipe content with no challenge markers</html>",
			emptyResult: true,
		},
		{
			name:   "cloudflare forwarding notice without turnstile is not a protection signal",
			status: 200,
			headers: http.Header{
				"Cf-Ray":       {"abc123"},
				"Server":       {"cloudflare"},
				"Content-Type": {"text/html"},
			},
			body:        "<html><p>You will be forwarded to the requested page shortly.</p></html>",
			emptyResult: true,
		},
		{
			name:   "cloudflare CDN on error response is a protection signal",
			status: 503,
			headers: http.Header{
				"Cf-Ray":       {"abc123"},
				"Server":       {"cloudflare"},
				"Content-Type": {"text/html"},
			},
			body:       "<html>error</html>",
			wantLabels: []string{"cloudflare"},
		},
		{
			name:   "cloudflare turnstile interstitial on 200 is a protection signal",
			status: 200,
			headers: http.Header{
				"Cf-Ray":       {"abc123"},
				"Server":       {"cloudflare"},
				"Content-Type": {"text/html"},
			},
			body: `<html><script src="https://challenges.cloudflare.com/turnstile/v0/api.js"></script>
				<p>You will be forwarded to the requested page shortly.</p></html>`,
			wantLabels: []string{"cloudflare", "captcha"},
			wantBody:   []string{"cloudflare_challenge", "cloudflare_turnstile"},
		},
		{
			name:   "imperva incapsula hcaptcha shell on 200 is a protection signal",
			status: 200,
			headers: http.Header{
				"Content-Type": {"text/html; charset=utf-8"},
			},
			body: `<html><head><script src="/_Incapsula_Resource?SWKMTFSR=1"></script></head>
				<body><iframe src="https://newassets.hcaptcha.com/captcha/v1/index.html"></iframe></body></html>`,
			wantLabels: []string{"imperva", "captcha"},
			wantBody:   []string{"imperva", "hcaptcha"},
		},
		{
			name:   "imperva vendor mention on 200 is not a protection signal",
			status: 200,
			headers: http.Header{
				"Content-Type": {"text/html"},
			},
			body:        "<html><p>Imperva Incapsula migration notes and vendor comparison.</p></html>",
			emptyResult: true,
		},
		{
			name:   "cf-turnstile widget markup without script is not a protection signal",
			status: 200,
			headers: http.Header{
				"Cf-Ray":       {"abc123"},
				"Server":       {"cloudflare"},
				"Content-Type": {"text/html"},
			},
			body:        `<html><div class="cf-turnstile" data-sitekey="site"></div></html>`,
			emptyResult: true,
		},
		{
			name:   "captcha unblock shell on 200 is a protection signal",
			status: 200,
			headers: http.Header{
				"Content-Type": {"text/html"},
			},
			body:       "<html><p>Fill out the captcha to unblock this page.</p></html>",
			wantLabels: []string{"captcha"},
			wantBody:   []string{"captcha"},
		},
		{
			name:        "turnstile word in normal json is not a protection signal",
			status:      200,
			headers:     http.Header{"Content-Type": {"application/json"}},
			body:        `{"turnstile":false,"captcha_required":false}`,
			emptyResult: true,
		},
		{
			name:        "cf-turnstile key in normal json is not a protection signal",
			status:      200,
			headers:     http.Header{"Content-Type": {"application/json"}},
			body:        `{"cf-turnstile":false,"captcha_required":false}`,
			emptyResult: true,
		},
		{
			name:        "hcaptcha key in normal json is not a protection signal",
			status:      200,
			headers:     http.Header{"Content-Type": {"application/json"}},
			body:        `{"hcaptcha_required":false,"captcha_provider":"hcaptcha"}`,
			emptyResult: true,
		},
		{
			name:   "aws waf token",
			status: 403,
			headers: http.Header{
				"X-Amzn-Waf-Action": {"challenge"},
				"Content-Type":      {"text/html"},
			},
			body:       "<html>access denied</html>",
			wantLabels: []string{"aws_waf"},
		},
		{
			name:   "datadome via header",
			status: 403,
			headers: http.Header{
				"X-Datadome": {"protected"},
			},
			body:       "datadome challenge",
			wantLabels: []string{"datadome"},
		},
		{
			name:   "captcha widget",
			status: 200,
			headers: http.Header{
				"Content-Type": {"text/html"},
			},
			body:       `<div class="g-recaptcha"></div>`,
			wantLabels: []string{"captcha"},
			wantBody:   []string{"recaptcha"},
		},
		{
			name:   "403 html with no markers becomes protected_web",
			status: 403,
			headers: http.Header{
				"Content-Type": {"text/html"},
			},
			body:       "<html>access denied</html>",
			wantLabels: []string{"protected_web"},
		},
		{
			name:   "401 no markers does not classify as protection",
			status: 401,
			headers: http.Header{
				"Content-Type":     {"application/json"},
				"Www-Authenticate": {"Bearer"},
			},
			body:        `{"error": "unauthorized"}`,
			emptyResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyResponse(tt.status, tt.headers, tt.body)
			if tt.emptyResult {
				assert.Empty(t, got, "expected no protections")
				return
			}
			labels := make([]string, len(got))
			for i, p := range got {
				labels[i] = p.Label
			}
			assert.ElementsMatch(t, tt.wantLabels, labels)
			assert.ElementsMatch(t, tt.wantBody, protectionsToBodyEvidence(got))
		})
	}
}

func TestIsClear(t *testing.T) {
	tests := []struct {
		status      int
		protections []Protection
		want        bool
	}{
		{200, nil, true},
		{301, nil, true},
		{401, nil, true},
		{403, nil, true}, // 403 without protection markers — assumed authz, not bot
		{404, nil, true},
		{429, nil, false},
		{500, nil, false},
		{200, []Protection{{Label: "vercel_challenge"}}, false},
		{403, []Protection{{Label: "cloudflare"}}, false},
	}
	for _, tt := range tests {
		got := isClear(tt.status, tt.protections)
		assert.Equal(t, tt.want, got, "status=%d protections=%v", tt.status, tt.protections)
	}
}
