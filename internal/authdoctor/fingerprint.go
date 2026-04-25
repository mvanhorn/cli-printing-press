package authdoctor

import (
	"strings"
	"unicode"
)

// fingerprintLen is the number of characters included in the fingerprint.
// Four characters is enough to distinguish token families (pat-, xoxb-,
// lin_, dub_) without narrowing to a user-specific secret.
const fingerprintLen = 4

// Fingerprint returns a redacted preview of a token value: the first
// few characters followed by an ellipsis. Non-printable and control
// characters are replaced with "?" so a malicious terminal-escape in
// a token value cannot rewrite the user's terminal.
//
// Empty input returns an empty string.
func Fingerprint(value string) string {
	if value == "" {
		return ""
	}

	runes := []rune(value)
	var b strings.Builder
	b.Grow(fingerprintLen + 3)

	limit := min(len(runes), fingerprintLen)

	for i := range limit {
		r := runes[i]
		if !unicode.IsPrint(r) {
			b.WriteRune('?')
			continue
		}
		b.WriteRune(r)
	}

	if len(runes) > fingerprintLen {
		b.WriteString("...")
	}

	return b.String()
}
