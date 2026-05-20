// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package browsersniff

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// syntheticJWT builds a JWT-shaped string (3 base64url segments separated by
// dots, header ≥ 20 chars, total ≥ 150 chars) so the JWT-shape gate fires.
// Real signature math is irrelevant; we only test the shape check.
func syntheticJWT() string {
	header := strings.Repeat("a", 32)               // > minHeader
	payload := strings.Repeat("b", 80)              // pad payload
	signature := strings.Repeat("c", 50)            // pad signature
	return header + "." + payload + "." + signature // total > minTotal
}

// TestDetectAuth0SPAInMemory_Positive — oauth/token response with
// access_token in the body and no JWT-shaped Set-Cookie on the same
// response. Detector must fire.
func TestDetectAuth0SPAInMemory_Positive(t *testing.T) {
	t.Parallel()

	entries := []EnrichedEntry{
		{
			Method:       "POST",
			URL:          "https://example.auth0.com/oauth/token",
			ResponseBody: `{"access_token":"abc.def.ghi","token_type":"Bearer","expires_in":3600}`,
			ResponseHeaders: map[string]string{
				"Set-Cookie": "__cf_bm=short; Path=/; HttpOnly",
			},
		},
	}
	assert.True(t, detectAuth0SPAInMemory(entries),
		"oauth/token with access_token body and no JWT-shaped Set-Cookie should trip Auth0 SPA detector")
}

// TestDetectAuth0SPAInMemory_JWTCookiePresent — same shape but the
// response also sets a JWT-shaped cookie. Detector must NOT fire because
// the existing cookie-jar extractor can reach the token.
func TestDetectAuth0SPAInMemory_JWTCookiePresent(t *testing.T) {
	t.Parallel()

	jwt := syntheticJWT()
	entries := []EnrichedEntry{
		{
			Method:       "POST",
			URL:          "https://example.auth0.com/oauth/token",
			ResponseBody: `{"access_token":"abc.def.ghi","token_type":"Bearer","expires_in":3600}`,
			ResponseHeaders: map[string]string{
				"Set-Cookie": "session_token=" + jwt + "; Path=/; HttpOnly",
			},
		},
	}
	assert.False(t, detectAuth0SPAInMemory(entries),
		"JWT-shaped Set-Cookie on the same response should suppress the in-memory subtype")
}

// TestDetectAuth0SPAInMemory_NoTokenCall — capture has no oauth/token
// call at all. Detector must NOT fire (returns absent-case).
func TestDetectAuth0SPAInMemory_NoTokenCall(t *testing.T) {
	t.Parallel()

	entries := []EnrichedEntry{
		{
			Method: "GET",
			URL:    "https://api.example.com/users",
			ResponseHeaders: map[string]string{
				"Content-Type": "application/json",
			},
			ResponseBody: `{"items": []}`,
		},
		{
			Method: "GET",
			URL:    "https://api.example.com/orders",
			ResponseHeaders: map[string]string{
				"Content-Type": "application/json",
			},
			ResponseBody: `{"items": [{"id":1}]}`,
		},
	}
	assert.False(t, detectAuth0SPAInMemory(entries),
		"capture without oauth/token calls should not trip the detector")
}

// TestDetectAuth0SPAInMemory_TokenCallNoAccessToken — has oauth/token but
// body lacks access_token (e.g. error response). Detector must NOT fire.
func TestDetectAuth0SPAInMemory_TokenCallNoAccessToken(t *testing.T) {
	t.Parallel()

	entries := []EnrichedEntry{
		{
			Method:       "POST",
			URL:          "https://example.auth0.com/oauth/token",
			ResponseBody: `{"error":"invalid_grant","error_description":"refresh token expired"}`,
		},
	}
	assert.False(t, detectAuth0SPAInMemory(entries),
		"oauth/token without access_token in body should not trip the detector")
}

// TestDetectAuth0SPAInMemory_PathWithQueryString — oauth/token URL with
// a query string still matches the suffix check.
func TestDetectAuth0SPAInMemory_PathWithQueryString(t *testing.T) {
	t.Parallel()

	entries := []EnrichedEntry{
		{
			Method:       "POST",
			URL:          "https://auth.example.com/oauth/token?audience=api.example.com",
			ResponseBody: `{"access_token":"abc.def.ghi","token_type":"Bearer"}`,
		},
	}
	assert.True(t, detectAuth0SPAInMemory(entries),
		"oauth/token with a query string should still match the path suffix")
}

// TestDetectAuth0SPAInMemory_PartialJSONFallback — body is technically
// invalid JSON (truncated capture) but contains the access_token key.
// Substring fallback must still trip the detector.
func TestDetectAuth0SPAInMemory_PartialJSONFallback(t *testing.T) {
	t.Parallel()

	entries := []EnrichedEntry{
		{
			Method:       "POST",
			URL:          "https://example.auth0.com/oauth/token",
			ResponseBody: `{"access_token":"abc.def.ghi","token_type":"Bear`, // truncated
		},
	}
	assert.True(t, detectAuth0SPAInMemory(entries),
		"truncated body with access_token key should still trip via the substring fallback")
}

// TestAuth0SPADetector_SetsAuthSubtype — full path: traffic with the
// signature → AuthAnalysis.Auth0SPAInMemory true and TrafficAnalysis
// carries the auth0_spa_in_memory generation hint.
func TestAuth0SPADetector_SetsAuthSubtype(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://app.example.com",
		Entries: []EnrichedEntry{
			{
				Method:       "POST",
				URL:          "https://example.auth0.com/oauth/token",
				ResponseBody: `{"access_token":"x.y.z","token_type":"Bearer"}`,
			},
			{
				Method: "GET",
				URL:    "https://api.example.com/v1/me",
				RequestHeaders: map[string]string{
					"Authorization": "Bearer abc.def.ghi",
				},
			},
		},
	}

	analysis, err := AnalyzeTraffic(capture)
	if err != nil {
		t.Fatalf("AnalyzeTraffic: %v", err)
	}
	assert.True(t, analysis.Auth.Auth0SPAInMemory,
		"AuthAnalysis should mark Auth0 SPA in-memory when the signature trips")
	assert.Contains(t, analysis.GenerationHints, "auth0_spa_in_memory",
		"GenerationHints should carry the auth0_spa_in_memory marker")
}

// TestSpecgenDetectAuth_AppliesAuth0SPASubtype — the specgen detectAuth
// wrapper must annotate bearer_token auth with the auth0_spa_in_memory
// subtype when the detector fires.
func TestSpecgenDetectAuth_AppliesAuth0SPASubtype(t *testing.T) {
	t.Parallel()

	entries := []EnrichedEntry{
		{
			Method:       "POST",
			URL:          "https://example.auth0.com/oauth/token",
			ResponseBody: `{"access_token":"abc.def.ghi","token_type":"Bearer"}`,
		},
		{
			Method: "GET",
			URL:    "https://api.example.com/v1/me",
			RequestHeaders: map[string]string{
				"Authorization": "Bearer abc.def.ghi",
			},
		},
	}
	auth := detectAuth(nil, entries, "example")
	assert.Equal(t, "bearer_token", auth.Type)
	assert.Equal(t, spec.AuthSubtypeAuth0SPAInMemory, auth.Subtype,
		"detectAuth should annotate bearer_token auth with the auth0_spa_in_memory subtype")
}

// TestSpecgenDetectAuth_NoSubtypeWithoutSignature — bearer_token traffic
// without the oauth/token + no-JWT-cookie signature must not get the
// subtype annotation.
func TestSpecgenDetectAuth_NoSubtypeWithoutSignature(t *testing.T) {
	t.Parallel()

	entries := []EnrichedEntry{
		{
			Method: "GET",
			URL:    "https://api.example.com/v1/me",
			RequestHeaders: map[string]string{
				"Authorization": "Bearer abc.def.ghi",
			},
		},
	}
	auth := detectAuth(nil, entries, "example")
	assert.Equal(t, "bearer_token", auth.Type)
	assert.Empty(t, auth.Subtype,
		"detectAuth must not annotate bearer_token auth without the in-memory signature")
}
