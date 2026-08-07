// Redaction helpers for samples written to <spec-stem>-samples/. Headers,
// JSON body keys, and string values matching JWT / email / E.164 phone
// patterns are replaced with the sentinel value below. The original
// EnrichedCapture stays untouched — only sample-file output is redacted.
package browsersniff

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
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
	redactJWTPattern   = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
	redactEmailPattern = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	// Phone redaction is intentionally conservative: only matches numbers
	// written in canonical E.164 form (leading `+`, country code, 7-14
	// trailing digits). Plain long digit runs without a `+` are skipped to
	// avoid false-positive redaction of timestamps, IDs, and order numbers.
	redactPhonePattern = regexp.MustCompile(`\+[1-9]\d{6,14}`)
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

// RedactJSONBody returns the body with sensitive JSON keys and value
// patterns replaced, plus a sorted list of dotted paths where redactions
// occurred. If the body parses as JSON, the structure is preserved and
// values are replaced in place. If the body is not JSON, the raw string is
// regex-swept for JWT / email / phone patterns and the list contains the
// pattern names that hit (`pattern:jwt`, `pattern:email`, `pattern:phone`).
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
			if isRedactBodyKey(key) {
				v[key] = RedactedSentinel
				paths[childPath] = true
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

// isRedactBodyKey normalizes a key to lowercase with separators stripped
// (so api_key, apiKey, and api-key all collapse to "apikey") and looks up
// against the redact list.
func isRedactBodyKey(name string) bool {
	normalized := strings.ToLower(name)
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	return redactBodyKeys[normalized]
}

func redactStringValue(s string) (string, string) {
	switch {
	case redactJWTPattern.MatchString(s):
		return RedactedSentinel, "jwt"
	case redactEmailPattern.MatchString(s):
		return RedactedSentinel, "email"
	case redactPhonePattern.MatchString(s):
		return RedactedSentinel, "phone"
	}
	return s, ""
}

// redactStringPatterns applies the same JWT / email / phone sweep against a
// raw (non-JSON) body. Returns the body with matching substrings replaced
// and a list of pattern names that fired.
func redactStringPatterns(body string) (string, []string) {
	patterns := map[string]bool{}
	out := body
	if redactJWTPattern.MatchString(out) {
		out = redactJWTPattern.ReplaceAllString(out, RedactedSentinel)
		patterns["pattern:jwt"] = true
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
