package artifacts

import "regexp"

type secretReplacement struct {
	pattern     *regexp.Regexp
	replacement string
}

var archivedSpecSecretPatterns = []secretReplacement{
	{
		pattern:     regexp.MustCompile(`secret-token:[A-Za-z0-9][A-Za-z0-9:_-]{20,}`),
		replacement: `secret-token:<REDACTED_TOKEN_EXAMPLE>`,
	},
	{
		pattern:     regexp.MustCompile(`Bearer sk_(?:live|test)_[A-Za-z0-9]{8,}`),
		replacement: `Bearer <REDACTED_STRIPE_TOKEN_EXAMPLE>`,
	},
	{
		pattern:     regexp.MustCompile(`Bearer (?:ghp|gho)_[A-Za-z0-9]{20,}`),
		replacement: `Bearer <REDACTED_GITHUB_TOKEN_EXAMPLE>`,
	},
	{
		pattern:     regexp.MustCompile(`Bearer github_pat_[A-Za-z0-9_]{40,}`),
		replacement: `Bearer <REDACTED_GITHUB_TOKEN_EXAMPLE>`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)(access[_-]?token|api[_-]?key|secret)=([A-Za-z0-9._+/=-]{20,})`),
		replacement: `${1}=<REDACTED_CREDENTIAL_EXAMPLE>`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)(Authorization:\s*Bearer\s+)[A-Za-z0-9._~+/=-]{20,}`),
		replacement: `${1}<REDACTED_BEARER_TOKEN_EXAMPLE>`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)((?:X-API-Key|API-Key):\s*)[A-Za-z0-9._+/=-]{20,}`),
		replacement: `${1}<REDACTED_CREDENTIAL_EXAMPLE>`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)((?:"?(?:access[_-]?token|api[_-]?key|apikey|secret|client[_-]?secret)"?\s*:\s*)"?)([A-Za-z0-9._+/=-]{20,})("?)`),
		replacement: `${1}<REDACTED_CREDENTIAL_EXAMPLE>${3}`,
	},
}

// RedactArchivedSpecSecrets removes credential-shaped examples from archived
// specs while keeping surrounding auth documentation intact.
func RedactArchivedSpecSecrets(data []byte) []byte {
	out := append([]byte(nil), data...)
	for _, rule := range archivedSpecSecretPatterns {
		out = rule.pattern.ReplaceAll(out, []byte(rule.replacement))
	}
	return out
}
