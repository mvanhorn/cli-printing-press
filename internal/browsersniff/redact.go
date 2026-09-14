// Redaction helpers for samples written to <spec-stem>-samples/. Headers,
// nested header maps, JSON body keys, and string values matching JWT /
// email / E.164 phone / auth-scheme patterns are replaced with the
// sentinel value below. The original EnrichedCapture stays untouched —
// only sample-file output is redacted.
package browsersniff

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// RedactedSentinel is the replacement string written in place of any
// redacted header value or JSON value. Kept as a string (not nil) so the
// surrounding structural type is preserved for schema inference and so
// reviewers can tell at a glance that the field existed.
const RedactedSentinel = "<redacted>"

var (
	redactHeaderExact = map[string]bool{
		"authorization":       true,
		"cookie":              true,
		"set-cookie":          true,
		"x-csrf-token":        true,
		"x-xsrf-token":        true,
		"x-api-key":           true,
		"proxy-authorization": true,
	}
	redactHeaderContains = []string{"token", "secret", "signature", "api-key", "api_key"}
	redactBodyKeys       = map[string]bool{
		"password":     true,
		"token":        true,
		"secret":       true,
		"apikey":       true,
		"accesstoken":  true,
		"refreshtoken": true,
		"creditcard":   true,
		"ssn":          true,
	}
	urlLikeBodyKeys = map[string]bool{
		"url":        true,
		"uri":        true,
		"href":       true,
		"link":       true,
		"path":       true,
		"host":       true,
		"hostname":   true,
		"origin":     true,
		"rawurl":     true,
		"requesturl": true,
		"baseurl":    true,
	}
	redactJWTPattern   = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
	redactEmailPattern = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	// Phone redaction is intentionally conservative: only matches numbers
	// written in canonical E.164 form (leading `+`, country code, 7-14
	// trailing digits). Plain long digit runs without a `+` are skipped to
	// avoid false-positive redaction of timestamps, IDs, and order numbers.
	redactPhonePattern      = regexp.MustCompile(`\+[1-9]\d{6,14}`)
	redactAuthSchemePattern = regexp.MustCompile(`(?i)\b(?:Basic|Bearer|Token)\s+[A-Za-z0-9._+/=-]{8,}`)
	redactURLQueryNames     = map[string]bool{
		"access_token":  true,
		"api_key":       true,
		"api-key":       true,
		"apikey":        true,
		"x-api-key":     true,
		"x_api_key":     true,
		"token":         true,
		"auth_token":    true,
		"refresh_token": true,
		"id_token":      true,
		"key":           true,
		"signature":     true,
		"auth":          true,
		"password":      true,
		"secret":        true,
		"authorization": true,
		"client_secret": true,
	}
)

// RedactHeaders returns a redacted copy of headers plus the sorted set of
// lowercased header names whose values were replaced. Headers not matching
// the auth-shape patterns are copied through unchanged. Returns a non-nil
// map even when no redactions fire so callers don't have to nil-check.
func RedactHeaders(headers map[string]string) (map[string]string, []string) {
	out := make(map[string]string, len(headers))
	redacted := map[string]bool{}
	for name, value := range headers {
		lower := strings.ToLower(strings.TrimSpace(name))
		if isRedactHeaderName(lower) {
			out[name] = RedactedSentinel
			redacted[lower] = true
			continue
		}
		out[name] = value
	}
	if len(redacted) == 0 {
		return out, nil
	}
	return out, sortedBoolKeys(redacted)
}

func isRedactHeaderName(lowerName string) bool {
	if redactHeaderExact[lowerName] {
		return true
	}
	for _, contains := range redactHeaderContains {
		if strings.Contains(lowerName, contains) {
			return true
		}
	}
	return false
}

// Header-contains matching would treat pagination fields like token_field
// as credentials; only exact header names and x-* auth headers qualify here.
func isRedactNestedHeaderKey(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if redactHeaderExact[lower] {
		return true
	}
	if strings.HasPrefix(lower, "x-") || strings.HasPrefix(lower, "x_") {
		for _, contains := range []string{"token", "key", "auth", "session", "secret"} {
			if strings.Contains(lower, contains) {
				return true
			}
		}
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(lower, "_", "-"), " ", "")
	switch normalized {
	case "api-key", "apikey":
		return true
	default:
		return false
	}
}

// RedactJSONBody returns the body with sensitive JSON keys and value
// patterns replaced, plus a sorted list of dotted paths where redactions
// occurred. If the body parses as JSON, the structure is preserved and
// values are replaced in place. Credential-bearing header values nested
// in maps (Authorization, x-api-key, Basic/Bearer blobs) are redacted
// even when the key is not an apiKey-shaped field name. URL / host /
// path fields keep opaque path segments; query and userinfo credentials
// in those scalars are still stripped. If the body is not JSON, the raw
// string is regex-swept for JWT / email / phone / auth-scheme patterns
// and the list contains the pattern names that hit.
func RedactJSONBody(body string) (string, []string) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return body, nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
		paths := map[string]bool{}
		redacted := redactJSONValue(parsed, "", paths)
		marshaled, err := json.Marshal(redacted)
		if err == nil {
			return string(marshaled), sortedBoolKeys(paths)
		}
	}
	return redactStringPatterns(body)
}

func redactJSONValue(value any, path string, paths map[string]bool) any {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			childPath := joinPath(path, key)
			if isRedactNestedHeaderKey(key) || isRedactBodyKey(key) {
				v[key] = RedactedSentinel
				paths[childPath] = true
				continue
			}
			if isURLLikeBodyKey(key) {
				v[key] = redactJSONValuePreservingURL(child, childPath, paths)
				continue
			}
			v[key] = redactJSONValue(child, childPath, paths)
		}
		return v
	case []any:
		for i, child := range v {
			childPath := fmt.Sprintf("%s[%d]", path, i)
			v[i] = redactJSONValue(child, childPath, paths)
		}
		return v
	case string:
		if redacted, pattern := redactStringValue(v); pattern != "" {
			paths[joinPath(path, "pattern:"+pattern)] = true
			return redacted
		}
		return v
	default:
		return value
	}
}

// Nested maps under url/host/path still carry header values that must be
// scrubbed. Scalars keep path segments; only query/userinfo credentials go,
// because a whole-string JWT/base64 sweep is what destroys endpoint URLs.
func redactJSONValuePreservingURL(value any, path string, paths map[string]bool) any {
	switch v := value.(type) {
	case map[string]any:
		return redactJSONValue(v, path, paths)
	case []any:
		return redactJSONValue(v, path, paths)
	case string:
		if redacted, pattern := redactURLLikeScalar(v); pattern != "" {
			paths[joinPath(path, "pattern:"+pattern)] = true
			return redacted
		}
		return v
	default:
		return value
	}
}

func redactURLLikeScalar(s string) (string, string) {
	inner := unwrapQuotedJSONString(s)
	parsed, err := url.Parse(inner)
	if err != nil || (parsed.User == nil && parsed.RawQuery == "") {
		return s, ""
	}

	changed := false
	if parsed.User != nil {
		parsed.User = url.User(RedactedSentinel)
		changed = true
	}
	query := parsed.Query()
	for key, values := range query {
		if !redactURLQueryNames[strings.ToLower(strings.TrimSpace(key))] {
			continue
		}
		for i, value := range values {
			if value == "" || value == RedactedSentinel {
				continue
			}
			values[i] = RedactedSentinel
			changed = true
		}
		query[key] = values
	}
	if !changed {
		return s, ""
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), "url-credential"
}

// isRedactBodyKey normalizes a key to lowercase with separators stripped
// (so api_key, apiKey, and api-key all collapse to "apikey") and looks up
// against the redact list.
func isRedactBodyKey(name string) bool {
	normalized := strings.ToLower(name)
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	return redactBodyKeys[normalized]
}

func isURLLikeBodyKey(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	return urlLikeBodyKeys[normalized]
}

func redactStringValue(s string) (string, string) {
	inner := unwrapQuotedJSONString(s)
	switch {
	case redactJWTPattern.MatchString(inner):
		return RedactedSentinel, "jwt"
	case redactAuthSchemePattern.MatchString(inner):
		return RedactedSentinel, "auth-scheme"
	case looksLikeUserPassBase64(inner):
		return RedactedSentinel, "basic-credential"
	case redactEmailPattern.MatchString(inner):
		return RedactedSentinel, "email"
	case redactPhonePattern.MatchString(inner):
		return RedactedSentinel, "phone"
	}
	return s, ""
}

func unwrapQuotedJSONString(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

func looksLikeUserPassBase64(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 8 || strings.ContainsAny(s, " \t\r\n") || strings.Contains(s, "://") {
		return false
	}
	decoded, ok := decodeStrictBase64(s)
	if !ok {
		return false
	}
	user, pass, found := strings.Cut(string(decoded), ":")
	if !found || user == "" || pass == "" || strings.Contains(pass, ":") {
		return false
	}
	return isPrintableCredentialPart(user) && isPrintableCredentialPart(pass)
}

func decodeStrictBase64(s string) ([]byte, bool) {
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err == nil {
		return decoded, true
	}
	decoded, err = base64.RawStdEncoding.DecodeString(s)
	if err == nil {
		return decoded, true
	}
	return nil, false
}

func isPrintableCredentialPart(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 32 || r > 126 || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// redactStringPatterns applies the JWT / email / phone / auth-scheme sweep
// against a raw (non-JSON) body. Bare base64 user:pass blobs are not
// swept here — without JSON keys there is no URL-field scope guard, and a
// blind base64 pass is what corrupts path segments.
func redactStringPatterns(body string) (string, []string) {
	patterns := map[string]bool{}
	out := body
	if redactJWTPattern.MatchString(out) {
		out = redactJWTPattern.ReplaceAllString(out, RedactedSentinel)
		patterns["pattern:jwt"] = true
	}
	if redactAuthSchemePattern.MatchString(out) {
		out = redactAuthSchemePattern.ReplaceAllString(out, RedactedSentinel)
		patterns["pattern:auth-scheme"] = true
	}
	if redactEmailPattern.MatchString(out) {
		out = redactEmailPattern.ReplaceAllString(out, RedactedSentinel)
		patterns["pattern:email"] = true
	}
	if redactPhonePattern.MatchString(out) {
		out = redactPhonePattern.ReplaceAllString(out, RedactedSentinel)
		patterns["pattern:phone"] = true
	}
	if len(patterns) == 0 {
		return body, nil
	}
	return out, sortedBoolKeys(patterns)
}

func joinPath(parent string, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}
