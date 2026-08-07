package artifacts

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/piiplaceholders"
)

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
		pattern:     regexp.MustCompile(`sk-or-v1-[A-Za-z0-9_-]{24,}`),
		replacement: `<REDACTED_OPENROUTER_TOKEN_EXAMPLE>`,
	},
	{
		pattern:     regexp.MustCompile(`Bearer (?:ghp|gho|ghs)_[A-Za-z0-9]{20,}`),
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

type VendorPrefixSecretFinding struct {
	Path        string
	Line        int
	Kind        string
	Fingerprint string
}

type ReviewedSecretSuppression struct {
	Path        string `json:"path"`
	Line        int    `json:"line"`
	Kind        string `json:"kind"`
	Fingerprint string `json:"fingerprint"`
	Reason      string `json:"reason"`
}

type SecretScanResult struct {
	Findings     []VendorPrefixSecretFinding
	Suppressions []ReviewedSecretSuppression
}

type vendorPrefixSecretPattern struct {
	kind                    string
	pattern                 *regexp.Regexp
	accept                  func(string) bool
	candidateFromSubmatches func([]string) string
	allowPublicSuppression  bool
}

var vendorPrefixSecretPatterns = []vendorPrefixSecretPattern{
	vendorSecretPattern("openrouter-api-key", `sk-or-v1-[A-Za-z0-9_-]{24,}`),
	vendorSecretPattern("stripe-secret-key", `sk_(?:live|test)_[A-Za-z0-9]{16,}`),
	vendorSecretPattern("calcom-api-key", `cal_(?:live|test)_[A-Za-z0-9]{16,}`),
	vendorSecretPattern("github-token", `(?:ghp|gho|ghs)_[A-Za-z0-9]{36,}`),
	vendorSecretPattern("github-fine-grained-token", `github_pat_[A-Za-z0-9_]{60,}`),
	vendorSecretPattern("slack-token", `xox[abprs]-[A-Za-z0-9-]{32,}`),
	vendorSecretPattern("slack-app-token", `xapp-[A-Za-z0-9-]{32,}`),
	vendorSecretPattern("google-api-key", `AIza[A-Za-z0-9_-]{20,}`),
	vendorSecretPattern("mailchimp-api-key", `\b[a-f0-9]{32}-us\d{1,2}\b`),
	vendorSecretPattern("linear-api-key", `\blin_api_[A-Za-z0-9]{40,}`),
	vendorSecretPattern("anthropic-api-key", `\bsk-ant-api03-[A-Za-z0-9_-]{40,}`),
	{
		kind:                   "aws-access-key",
		pattern:                regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
		allowPublicSuppression: true,
		accept: func(candidate string) bool {
			return !strings.Contains(candidate, "EXAMPLE")
		},
	},
}

var opaqueCredentialPatterns = []vendorPrefixSecretPattern{
	opaqueCredentialPattern("opaque-credential:api-key", `api[_-]?key|apiKey|apikey`),
	opaqueCredentialPattern("opaque-credential:secret", `secret|client[_-]?secret|clientSecret`),
	opaqueCredentialPattern("opaque-credential:token", `token|access[_-]?token|refresh[_-]?token|accessToken|refreshToken`),
	opaqueCredentialPattern("opaque-credential:password", `password|passcode`),
	opaqueCredentialPattern("opaque-credential:session", `session|session[_-]?id|session[_-]?token`),
}

const structuredCookieLineWindow = 5

var structuredCookieValueLineRE = regexp.MustCompile(`(?i)["']value["']\s*:\s*["']([^"']{8,})["']`)
var uuidCredentialValueRE = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b`)
var opaqueCredentialLowerRE = regexp.MustCompile(`[a-z]`)
var opaqueCredentialUpperRE = regexp.MustCompile(`[A-Z]`)
var opaqueCredentialDigitRE = regexp.MustCompile(`[0-9]`)
var opaqueCredentialSymbolRE = regexp.MustCompile(`[._~+/=-]`)

const publicSecretMarker = "pp:public-secret"

func vendorSecretPattern(kind, pattern string) vendorPrefixSecretPattern {
	return vendorPrefixSecretPattern{
		kind:                   kind,
		pattern:                regexp.MustCompile(pattern),
		allowPublicSuppression: true,
	}
}

func opaqueCredentialPattern(kind, keyPattern string) vendorPrefixSecretPattern {
	return vendorPrefixSecretPattern{
		kind:    kind,
		pattern: regexp.MustCompile(`(?i)(?:"(?:` + keyPattern + `)"|(?:` + keyPattern + `))\s*:\s*["']?([A-Za-z0-9._~+/=-]{16,})`),
		candidateFromSubmatches: func(matches []string) string {
			if len(matches) > 1 {
				return matches[1]
			}
			return ""
		},
		accept: looksLikeOpaqueCredentialValue,
	}
}

func FindVendorPrefixSecrets(root string) ([]VendorPrefixSecretFinding, error) {
	result, err := findSecrets(root, vendorPrefixSecretPatterns)
	return result.Findings, err
}

func FindPackageSecrets(root string, cookieNames []string) ([]VendorPrefixSecretFinding, error) {
	result, err := FindPackageSecretsWithSuppressions(root, cookieNames)
	return result.Findings, err
}

func FindPackageSecretsWithSuppressions(root string, cookieNames []string) (SecretScanResult, error) {
	patterns := append([]vendorPrefixSecretPattern(nil), vendorPrefixSecretPatterns...)
	patterns = append(patterns, opaqueCredentialPatterns...)
	patterns = append(patterns, cookieSecretPatterns(cookieNames)...)
	return findSecrets(root, patterns)
}

func FindSpecDeclaredCookieSecrets(root string, cookieNames []string) ([]VendorPrefixSecretFinding, error) {
	result, err := findSecrets(root, cookieSecretPatterns(cookieNames))
	return result.Findings, err
}

func cookieSecretPatterns(cookieNames []string) []vendorPrefixSecretPattern {
	if len(cookieNames) == 0 {
		return nil
	}

	patterns := make([]vendorPrefixSecretPattern, 0, len(cookieNames)*3)
	for _, name := range cookieNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		quotedName := regexp.QuoteMeta(name)
		patterns = append(patterns, vendorPrefixSecretPattern{
			kind:    "cookie-value:" + name,
			pattern: regexp.MustCompile("(?:^|[\\s\"'`;,{:])" + quotedName + "=([^\\s;\"'&,}]{8,})"),
			accept: func(candidate string) bool {
				_, value, ok := strings.Cut(candidate, "=")
				return ok && !piiplaceholders.IsSyntheticCookieValue(value)
			},
		})
		patterns = append(patterns,
			structuredCookieSecretPattern(name, regexp.MustCompile(`(?i)["']name["']\s*:\s*["']`+quotedName+`["'][^{}\n]*["']value["']\s*:\s*["']([^"']{8,})["']`)),
			structuredCookieSecretPattern(name, regexp.MustCompile(`(?i)["']value["']\s*:\s*["']([^"']{8,})["'][^{}\n]*["']name["']\s*:\s*["']`+quotedName+`["']`)),
		)
	}
	return patterns
}

func structuredCookieSecretPattern(name string, pattern *regexp.Regexp) vendorPrefixSecretPattern {
	return vendorPrefixSecretPattern{
		kind:    "cookie-value:" + name,
		pattern: pattern,
		accept: func(candidate string) bool {
			matches := pattern.FindStringSubmatch(candidate)
			return len(matches) == 2 && !piiplaceholders.IsSyntheticCookieValue(matches[1])
		},
	}
}

func FormatVendorPrefixSecretFindings(findings []VendorPrefixSecretFinding) string {
	lines := make([]string, 0, len(findings))
	for _, finding := range findings {
		lines = append(lines, fmt.Sprintf("%s:%d %s", finding.Path, finding.Line, finding.Kind))
	}
	return strings.Join(lines, "\n")
}

func findSecrets(root string, patterns []vendorPrefixSecretPattern) (SecretScanResult, error) {
	var result SecretScanResult
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		fileFindings, err := scanSecretFile(root, path, patterns)
		if err != nil {
			return err
		}
		result.Findings = append(result.Findings, fileFindings.Findings...)
		result.Suppressions = append(result.Suppressions, fileFindings.Suppressions...)
		return nil
	})
	return result, err
}

func scanSecretFile(root, path string, patterns []vendorPrefixSecretPattern) (SecretScanResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return SecretScanResult{}, err
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReaderSize(file, 8192)
	probe, err := reader.Peek(8192)
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return SecretScanResult{}, err
	}
	if bytes.Contains(probe, []byte{0}) {
		return SecretScanResult{}, nil
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		return SecretScanResult{}, err
	}
	rel = filepath.ToSlash(rel)

	var result SecretScanResult
	lineNumber := 0
	cookieNames := declaredCookieNamesFromPatterns(patterns)
	pendingCookieNames := map[string]int{}
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil && readErr != io.EOF {
			return SecretScanResult{}, readErr
		}
		if line == "" && readErr == io.EOF {
			break
		}
		lineNumber++
		lineCandidateFingerprints := map[string]bool{}
		for _, pattern := range patterns {
			for _, candidate := range vendorPrefixSecretLineMatches(pattern, line) {
				fingerprint := secretFingerprint(candidate)
				if lineCandidateFingerprints[fingerprint] {
					continue
				}
				lineCandidateFingerprints[fingerprint] = true
				if pattern.allowPublicSuppression {
					if reason, ok := publicSecretSuppressionReason(line); ok {
						result.Suppressions = append(result.Suppressions, ReviewedSecretSuppression{
							Path:        rel,
							Line:        lineNumber,
							Kind:        pattern.kind,
							Fingerprint: fingerprint,
							Reason:      reason,
						})
						continue
					}
				}
				result.Findings = append(result.Findings, VendorPrefixSecretFinding{
					Path:        rel,
					Line:        lineNumber,
					Kind:        pattern.kind,
					Fingerprint: fingerprint,
				})
			}
		}
		for name, nameLine := range pendingCookieNames {
			if lineNumber-nameLine > structuredCookieLineWindow {
				delete(pendingCookieNames, name)
			}
		}
		for _, name := range cookieNames {
			if structuredCookieNameLineMatch(name, line) {
				pendingCookieNames[name] = lineNumber
			}
		}
		if matches := structuredCookieValueLineRE.FindStringSubmatch(line); len(matches) == 2 && !piiplaceholders.IsSyntheticCookieValue(matches[1]) {
			for name, nameLine := range pendingCookieNames {
				if nameLine == lineNumber {
					continue
				}
				result.Findings = append(result.Findings, VendorPrefixSecretFinding{
					Path:        rel,
					Line:        lineNumber,
					Kind:        "cookie-value:" + name,
					Fingerprint: secretFingerprint(matches[1]),
				})
				delete(pendingCookieNames, name)
			}
		}
		if readErr == io.EOF {
			break
		}
	}
	return result, nil
}

func declaredCookieNamesFromPatterns(patterns []vendorPrefixSecretPattern) []string {
	seen := map[string]struct{}{}
	for _, pattern := range patterns {
		name, ok := strings.CutPrefix(pattern.kind, "cookie-value:")
		if !ok || name == "" {
			continue
		}
		seen[name] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	return names
}

func structuredCookieNameLineMatch(name, line string) bool {
	pattern := regexp.MustCompile(`(?i)["']name["']\s*:\s*["']` + regexp.QuoteMeta(name) + `["']`)
	return pattern.MatchString(line)
}

func vendorPrefixSecretLineMatches(pattern vendorPrefixSecretPattern, line string) []string {
	var candidates []string
	for _, matches := range pattern.pattern.FindAllStringSubmatch(line, -1) {
		candidate := matches[0]
		if pattern.candidateFromSubmatches != nil {
			candidate = pattern.candidateFromSubmatches(matches)
		}
		if pattern.accept == nil || pattern.accept(candidate) {
			candidates = append(candidates, strings.TrimRight(candidate, ".,;)}]"))
		}
	}
	return candidates
}

func looksLikeOpaqueCredentialValue(candidate string) bool {
	candidate = strings.TrimSpace(strings.Trim(candidate, `"'`))
	if candidate == "" {
		return false
	}
	lower := strings.ToLower(candidate)
	for _, token := range []string{"<redacted", "redacted", "example", "placeholder", "your-", "your_", "insert-", "changeme"} {
		if strings.Contains(lower, token) {
			return false
		}
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return false
	}
	if uuidCredentialValueRE.MatchString(candidate) {
		return true
	}
	if len(candidate) < 32 {
		return false
	}
	classes := 0
	if opaqueCredentialLowerRE.MatchString(candidate) {
		classes++
	}
	if opaqueCredentialUpperRE.MatchString(candidate) {
		classes++
	}
	if opaqueCredentialDigitRE.MatchString(candidate) {
		classes++
	}
	if opaqueCredentialSymbolRE.MatchString(candidate) {
		classes++
	}
	return classes >= 2
}

func publicSecretSuppressionReason(line string) (string, bool) {
	_, reason, ok := strings.Cut(line, publicSecretMarker)
	if !ok {
		return "", false
	}
	reason = strings.TrimSpace(reason)
	reason = strings.TrimLeft(reason, ":=- \t")
	lowerReason := strings.ToLower(reason)
	if strings.HasPrefix(lowerReason, "reason=") || strings.HasPrefix(lowerReason, "reason:") {
		reason = strings.TrimLeft(reason[len("reason"):], ":= \t")
	}
	reason = strings.Trim(reason, `"'`)
	if len(strings.Fields(reason)) < 3 {
		return "", false
	}
	return reason, true
}

func secretFingerprint(candidate string) string {
	sum := sha256.Sum256([]byte(candidate))
	return "sha256:" + hex.EncodeToString(sum[:])
}
