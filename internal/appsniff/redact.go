package appsniff

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
)

var (
	forbiddenPathSegments = []string{
		"/cookies",
		"/local storage",
		"/session storage",
		"/indexeddb",
		"/keychain",
		"/login data",
		"/web data",
		"/network persistent state",
		"/transportsecurity",
		"/preferences",
		"/local state",
		"/credential",
		"/secrets",
		"/.ssh/",
		"/.aws/",
		"/.gnupg/",
	}

	forbiddenFileExtensions = []string{
		".sqlite",
		".sqlite3",
		".sqlite-wal",
		".sqlite-shm",
		".ldb",
		".log",
		".keychain",
		".keychain-db",
		".p12",
		".pfx",
		".pem",
		".key",
		".jks",
		".keystore",
	}

	forbiddenBaseNames = map[string]bool{
		"cookies":                  true,
		"cookies-journal":          true,
		"login data":               true,
		"web data":                 true,
		"network persistent state": true,
		"local state":              true,
		".env":                     true,
		".env.local":               true,
		".env.production":          true,
		"credentials.json":         true,
		"token.json":               true,
		"auth.json":                true,
		"session.json":             true,
	}

	// Secret patterns caught in addition to browsersniff.RedactJSONBody.
	secretTokenPatterns = []*regexp.Regexp{
		regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
		regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`),
		regexp.MustCompile(`\bxoxc-[A-Za-z0-9-]{10,}\b`),
		regexp.MustCompile(`\bxoxd-[A-Za-z0-9%_=-]{10,}\b`),
		regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}\b`),
		regexp.MustCompile(`\b(?:sk_live|sk_test|pk_live|rk_live)_[A-Za-z0-9]{12,}\b`),
		regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{30,}\b`),
		regexp.MustCompile(`(?i)\b(?:Bearer|Basic|Token)\s+[A-Za-z0-9._+/=-]{12,}`),
	}

	sensitiveQueryKey = map[string]bool{
		"access_token":  true,
		"api_key":       true,
		"api-key":       true,
		"apikey":        true,
		"token":         true,
		"auth_token":    true,
		"refresh_token": true,
		"id_token":      true,
		"secret":        true,
		"client_secret": true,
		"password":      true,
		"signature":     true,
		"sig":           true,
		"key":           true,
		"auth":          true,
		"cookie":        true,
	}
)

// IsForbiddenSecretPath returns true for browser/app credential stores, cookie
// databases, keychains, and private key files that app-sniff must never read.
func IsForbiddenSecretPath(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	norm := "/" + strings.ToLower(filepath.ToSlash(strings.TrimLeft(path, "/\\")))
	base := strings.ToLower(filepath.Base(norm))
	if forbiddenBaseNames[base] {
		return true
	}
	if strings.HasPrefix(base, ".env.") {
		return true
	}
	for _, ext := range forbiddenFileExtensions {
		if strings.HasSuffix(base, ext) {
			return true
		}
	}
	for _, seg := range forbiddenPathSegments {
		if strings.Contains(norm, seg+"/") || strings.HasSuffix(norm, seg) {
			return true
		}
	}
	return false
}

// RedactString strips raw tokens, JWTs, auth headers, and emails from any
// diagnostic string, URL, or note before it can be emitted or logged.
func RedactString(s string) string {
	if s == "" {
		return s
	}
	out, _ := browsersniff.RedactJSONBody(s)
	for _, re := range secretTokenPatterns {
		out = re.ReplaceAllString(out, browsersniff.RedactedSentinel)
	}
	return out
}

// SanitizeURL removes userinfo and redacts any credential-shaped query parameter
// values from a URL while preserving scheme, host, and path.
func SanitizeURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return RedactString(trimmed)
	}
	u.User = nil
	if u.RawQuery != "" {
		q := u.Query()
		changed := false
		for k := range q {
			if sensitiveQueryKey[strings.ToLower(k)] {
				q.Set(k, browsersniff.RedactedSentinel)
				changed = true
			}
		}
		if changed {
			u.RawQuery = q.Encode()
		}
	}
	return RedactString(u.String())
}
