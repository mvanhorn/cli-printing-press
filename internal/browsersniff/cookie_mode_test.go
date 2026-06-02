package browsersniff

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// TestDetectCookieMode_SessionHeader pins that a captured Cookie request
// header carrying >= 2 name=value pairs (the shape of a real browser/native
// session) promotes the spec to CookieModeSessionHeader so the generated
// client emits req.Header.Set("Cookie", ...) instead of req.AddCookie —
// which net/http rejects when the value contains a ';'.
func TestDetectCookieMode_SessionHeader(t *testing.T) {
	t.Parallel()

	entries := []EnrichedEntry{
		{RequestHeaders: map[string]string{
			"Cookie": "d=ABC123; d-s=1717286400; x=u",
		}},
	}
	if got := detectCookieMode(entries); got != spec.CookieModeSessionHeader {
		t.Fatalf("detectCookieMode multi-pair header = %q, want %q", got, spec.CookieModeSessionHeader)
	}
}

// TestDetectCookieMode_NamedTokenDefault pins that a single name=value
// Cookie header (or no Cookie header at all) leaves CookieMode empty,
// preserving the existing AddCookie-based named-token codegen path.
func TestDetectCookieMode_NamedTokenDefault(t *testing.T) {
	t.Parallel()

	cases := map[string][]EnrichedEntry{
		"single-pair": {
			{RequestHeaders: map[string]string{"Cookie": "api_key=secret"}},
		},
		"no-cookie-header": {
			{RequestHeaders: map[string]string{"Authorization": "Bearer x"}},
		},
		"empty-cookie": {
			{RequestHeaders: map[string]string{"Cookie": ""}},
		},
		"semicolon-without-value": {
			{RequestHeaders: map[string]string{"Cookie": "stale;"}},
		},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			if got := detectCookieMode(entries); got != "" {
				t.Fatalf("detectCookieMode = %q, want empty (named_token default)", got)
			}
		})
	}
}

// TestDetectCapturedAuth_CookiePropagatesMode pins that the cookie-typed
// capture path threads detectCookieMode into the AuthConfig it returns, so
// downstream codegen reaches the session_header template branch.
func TestDetectCapturedAuth_CookiePropagatesMode(t *testing.T) {
	t.Parallel()

	capture := &AuthCapture{
		Headers:     map[string]string{"Cookie": "d=ABC; d-s=1"},
		Type:        "cookie",
		BoundDomain: ".example.com",
	}
	entries := []EnrichedEntry{
		{RequestHeaders: map[string]string{"Cookie": "d=ABC; d-s=1"}},
	}
	auth := detectCapturedAuth(capture, entries, "FOO")
	if auth.Type != "cookie" {
		t.Fatalf("Type = %q, want cookie", auth.Type)
	}
	if auth.In != "cookie" {
		t.Fatalf("In = %q, want cookie", auth.In)
	}
	if auth.CookieMode != spec.CookieModeSessionHeader {
		t.Fatalf("CookieMode = %q, want %q", auth.CookieMode, spec.CookieModeSessionHeader)
	}
}
