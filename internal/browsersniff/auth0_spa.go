// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package browsersniff

import (
	"encoding/json"
	"strings"
)

// detectAuth0SPAInMemory reports whether the captured traffic looks like an
// Auth0 SPA SDK deployment using the default in-memory cache
// (`cacheLocation: memory`). The signature: at least one `/oauth/token`
// response carries an `access_token` value in the JSON body, and no
// JWT-shaped Set-Cookie header is observed on the same response.
//
// Tokens that match this shape live in JS heap only — never in cookies,
// never in localStorage — so the cookie-jar extractor in the generated
// `auth login --chrome` flow has no path to them. The detection feeds a
// spec annotation (auth.subtype: "auth0_spa_in_memory") that routes the
// generator to emit a CDP-based runtime extractor instead.
//
// The check is intentionally conservative: a missing JWT-shaped Set-Cookie
// alone is not enough (most APIs don't set credential cookies on
// /oauth/token even when they aren't Auth0 SPA), so the signal requires the
// access_token to actually appear in the body. The JWT cookie absence rules
// out the rarer "we put the access token in a cookie too" pattern, which
// the existing cookie-jar extractor already handles.
func detectAuth0SPAInMemory(entries []EnrichedEntry) bool {
	for _, entry := range entries {
		if !isOAuthTokenPath(entry.URL) {
			continue
		}
		if !responseBodyCarriesAccessToken(entry.ResponseBody) {
			continue
		}
		if responseHasJWTShapedSetCookie(entry.ResponseHeaders) {
			// Tokens are also being persisted to a cookie; the existing
			// cookie-jar flow can reach them. Not an in-memory case.
			continue
		}
		return true
	}
	return false
}

// isOAuthTokenPath matches the Auth0 token endpoint path. Auth0 SPA SDK
// always POSTs to `/oauth/token` against the tenant domain; other OAuth
// providers (Google, Microsoft, GitHub) use the same path shape, which is
// fine — the cookie-vs-in-memory question is the same regardless of
// vendor. We match on the path suffix to ignore tenant-shaped hostnames
// like `dev-xyz.us.auth0.com`.
func isOAuthTokenPath(rawURL string) bool {
	// A bare URL or HAR-recorded URL; the path may or may not have a query
	// string. Split on the first '?' before checking the suffix so
	// `/oauth/token?...` still matches.
	pathOnly := rawURL
	if i := strings.IndexByte(pathOnly, '?'); i >= 0 {
		pathOnly = pathOnly[:i]
	}
	pathOnly = strings.TrimRight(pathOnly, "/")
	return strings.HasSuffix(pathOnly, "/oauth/token")
}

// responseBodyCarriesAccessToken decodes the body as JSON and looks for a
// non-empty `access_token` field. Falls back to a substring check when JSON
// decoding fails so partial/HAR-truncated captures still trip the detector
// — the substring `"access_token"` followed by a colon is specific enough
// that false positives on non-OAuth responses are vanishingly unlikely.
func responseBodyCarriesAccessToken(body string) bool {
	body = strings.TrimSpace(body)
	if body == "" {
		return false
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err == nil {
		if v, ok := decoded["access_token"]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return true
			}
		}
		return false
	}
	// JSON decode failed — fall back to a textual marker. Require the key
	// in JSON-shaped quotes followed by a value indicator (`":`) so prose
	// mentions of "access_token" in HTML error pages don't match.
	return strings.Contains(body, `"access_token":`)
}

// responseHasJWTShapedSetCookie scans Set-Cookie headers for a value whose
// cookie value (the bit after the first `=`) looks like a JWT — three
// base64url segments separated by dots, with a minimum length that rules
// out short tracking cookies like Cloudflare's `__cf_bm`.
//
// EnrichedEntry stores headers as a `map[string]string`. Multi-valued
// Set-Cookie headers (the common case for token issuance responses) are
// folded by the HAR parser into a single comma-separated string, which we
// re-split here. This is a heuristic — both `,` and `, ` are legal inside
// a cookie value's `Expires=...` attribute, so we look at each comma-split
// piece and accept the result whenever any piece's value half satisfies
// the JWT shape check. The opposite failure mode (false positive) is
// preferable to the false negative: a false positive only blocks the
// auth0-spa-in-memory subtype from firing, leaving the cookie-jar path
// intact.
func responseHasJWTShapedSetCookie(headers map[string]string) bool {
	for name, value := range headers {
		if !strings.EqualFold(name, "Set-Cookie") {
			continue
		}
		// Split on comma to handle the HAR-folded multi-header form.
		for raw := range strings.SplitSeq(value, ",") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			// The cookie name=value pair is the part before the first `;`.
			pair := raw
			if i := strings.IndexByte(pair, ';'); i >= 0 {
				pair = pair[:i]
			}
			_, cookieValue, ok := strings.Cut(pair, "=")
			if !ok {
				continue
			}
			cookieValue = strings.TrimSpace(cookieValue)
			if looksLikeJWTShape(cookieValue) {
				return true
			}
		}
	}
	return false
}

// looksLikeJWTShape is the sniff-time mirror of cliutil.LooksLikeJWT (the
// runtime helper emitted into every printed CLI). Lives here as a copy
// because the generator package can't import the template-side runtime
// helper at compile time. Length and segment-shape values match the
// runtime gate so a token that fails this check would also fail in the
// generated CLI — keeping the detection symmetric.
//
// See internal/generator/templates/cliutil_jwtshape.go.tmpl for the
// canonical version and the empirical-floor rationale.
func looksLikeJWTShape(s string) bool {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "Bearer ")
	const (
		minTotal  = 150
		minHeader = 20
	)
	if len(s) < minTotal {
		return false
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	if len(parts[0]) < minHeader {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			isAlnum := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
			if !isAlnum && r != '-' && r != '_' && r != '=' {
				return false
			}
		}
	}
	return true
}
