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
	return findMeShapedEndpointPath(s)
}

// deriveAuthVerifyPath chooses the path the generated `doctor` command should
// hit for its authenticated credentials probe. Mirrors deriveHealthCheckPath
// but for the auth-required side of the doctor report: a verify path that
// returns 401 on bad credentials and 2xx on good ones lets doctor distinguish
// "credentials accepted" from "credentials silently present but never tested."
//
// Priority:
//  1. Auth.VerifyPath when set (explicit user override, never clobbered).
//  2. A heuristic me-shaped GET endpoint discovered in the spec. Me-shaped
//     endpoints (`/users/me`, `/account`, `/viewer`, etc.) describe the
//     calling identity and almost universally require auth, which is exactly
//     the contract the credentials probe needs.
//  3. Empty string. The doctor template falls back to reporting credentials
//     as "present (not verified — set auth.verify_path in spec for an API
//     acceptance check)" rather than fabricating a verdict from `/`.
//
// Returning empty in case 3 keeps the template's existing "not verified"
// branch authoritative; the goal is to skip that branch whenever the spec
// gives us a defensible me-shaped path to probe, not to invent one.
func deriveAuthVerifyPath(s *spec.APISpec) string {
	if s == nil {
		return ""
	}
	if s.Auth.VerifyPath != "" {
		return s.Auth.VerifyPath
	}
	return findMeShapedEndpointPath(s)
}

// findMeShapedEndpointPath walks the spec for the first GET endpoint whose
// path matches one of meShapedPathTails (most-specific tail first) and whose
// path still composes cleanly against base_url. Returns "" when no candidate
// qualifies; used as the shared heuristic step inside the two derivation
// helpers above.
//
// The composability filter matters because the derived path is emitted as
// Auth.VerifyPath, which the runtime appends to base_url. A candidate whose
// path re-enters the base_url path prefix (a host-root path masquerading as
// base-relative) would double-prefix at probe time; skipping it here leaves
// the doctor's honest "present, not verified" branch authoritative rather
// than emitting a knowingly-404 probe path.
func findMeShapedEndpointPath(s *spec.APISpec) string {
	for _, tail := range meShapedPathTails {
		if e, ok := findEndpointMatch(s, func(e spec.Endpoint) bool {
			return isMeShapedEndpoint(e, tail)
		}); ok {
			if composableAgainstBase(s.BaseURL, e.Path) {
				return e.Path
			}
		}
	}
	return ""
}

// composableAgainstBase reports whether path, appended to baseURL, reaches the
// endpoint the spec author meant. Endpoint paths are base_url-relative by
// contract (the runtime concatenates base_url + path), so a path that re-enters
// the base_url's own leading path segment is a host-root path masquerading as
// base-relative: appending it double-prefixes the shared segment (base
// `/api/v1/workspaces/{slug}` + `/api/v1/users/me` -> `.../workspaces/{slug}/
// api/v1/users/me`, a 404). When base_url carries no path prefix (the common
// case where it equals the server root), every path composes cleanly.
func composableAgainstBase(baseURL, path string) bool {
	baseSeg := firstPathSegment(baseURLPathPrefix(baseURL))
	if baseSeg == "" {
		return true
	}
	return !strings.EqualFold(baseSeg, firstPathSegment(path))
}

// baseURLPathPrefix returns the path component of baseURL (everything from the
// first "/" after the scheme+authority), or "" when baseURL is the server root.
// Avoids url.Parse so template-var authorities like `https://{region}.host.com`
// don't trip the parser; only the path portion is needed here.
func baseURLPathPrefix(baseURL string) string {
	b := baseURL
	for _, scheme := range []string{"https://", "http://"} {
		if len(b) >= len(scheme) && strings.EqualFold(b[:len(scheme)], scheme) {
			rest := b[len(scheme):]
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				return rest[i:]
			}
			return ""
		}
	}
	// Scheme-less base (rare): a leading slash is itself the path prefix.
	if strings.HasPrefix(b, "/") {
		return b
	}
	return ""
}

// firstPathSegment returns the first non-empty "/"-delimited segment of p
// (e.g. "/api/v1/users" -> "api"), or "" when p has no segment.
func firstPathSegment(p string) string {
	p = strings.TrimLeft(p, "/")
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return p
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
