package generator

import (
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// meShapedPathTails enumerates path suffixes that conventionally identify a
// small auth-only GET endpoint suitable as a reachability probe (the same
// shape Auth.VerifyPath calls out in spec.go). Order is meaningful only when
// multiple endpoints qualify in the same spec — earliest match wins so that
// the more specific `users/me` beats a bare `me` on APIs that ship both.
var meShapedPathTails = []string{
	"users/me.json",
	"users/me",
	"users/@me",
	"current_user",
	"me.json",
	"whoami",
	"viewer",
	"account",
	"self",
	"user",
	"me",
}

// deriveHealthCheckPath chooses the path the generated `doctor` command should
// hit for its unauthenticated reachability probe.
//
// Priority:
//  1. spec.HealthCheckPath when set (explicit user override, never clobbered).
//  2. Auth.VerifyPath when set (already vetted by the operator as a known-good
//     authenticated GET; an unauthenticated probe against it returns 401 from
//     the real API surface, which the doctor classifies as "reachable").
//  3. A heuristic me-shaped GET endpoint discovered in the spec (no required
//     path/query params and a tail matching meShapedPathTails).
//  4. Empty string. The template falls back to probing `/`, preserving the
//     pre-derivation behavior for specs with nothing better to offer.
//
// Returning empty in case 4 (rather than `"/"`) keeps the existing template
// branch in doctor.go.tmpl authoritative for the fallback — one source of
// truth for the probe path the runtime sends.
func deriveHealthCheckPath(s *spec.APISpec) string {
	if s == nil {
		return ""
	}
	if s.HealthCheckPath != "" {
		return s.HealthCheckPath
	}
	if s.Auth.VerifyPath != "" {
		return s.Auth.VerifyPath
	}
	for _, tail := range meShapedPathTails {
		if e, ok := findEndpointMatch(s, func(e spec.Endpoint) bool {
			return isMeShapedEndpoint(e, tail)
		}); ok {
			return e.Path
		}
	}
	return ""
}

func isMeShapedEndpoint(e spec.Endpoint, tail string) bool {
	if !strings.EqualFold(e.Method, "GET") {
		return false
	}
	if e.Path == "" || strings.Contains(e.Path, "{") {
		return false
	}
	for _, p := range e.Params {
		if p.Required {
			return false
		}
	}
	// Match the bare tail or any "/<tail>" suffix so prefixed paths like
	// `/api/v2/users/me.json` resolve cleanly against `users/me.json`.
	lower := strings.ToLower(strings.TrimSuffix(e.Path, "/"))
	target := strings.ToLower(tail)
	return lower == target || strings.HasSuffix(lower, "/"+target)
}
