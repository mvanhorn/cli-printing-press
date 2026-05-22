package artifacts

import (
	"bufio"
	"bytes"
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
	Path string
	Line int
	Kind string
}

type vendorPrefixSecretPattern struct {
	kind    string
	pattern *regexp.Regexp
	accept  func(string) bool
}

var vendorPrefixSecretPatterns = []vendorPrefixSecretPattern{
	{kind: "openrouter-api-key", pattern: regexp.MustCompile(`sk-or-v1-[A-Za-z0-9_-]{24,}`)},
	{kind: "stripe-secret-key", pattern: regexp.MustCompile(`sk_(?:live|test)_[A-Za-z0-9]{16,}`)},
	{kind: "calcom-api-key", pattern: regexp.MustCompile(`cal_(?:live|test)_[A-Za-z0-9]{16,}`)},
	{kind: "github-token", pattern: regexp.MustCompile(`(?:ghp|gho|ghs)_[A-Za-z0-9]{36,}`)},
	{kind: "github-fine-grained-token", pattern: regexp.MustCompile(`github_pat_[A-Za-z0-9_]{60,}`)},
	{kind: "slack-token", pattern: regexp.MustCompile(`xox[abprs]-[A-Za-z0-9-]{32,}`)},
	{kind: "slack-app-token", pattern: regexp.MustCompile(`xapp-[A-Za-z0-9-]{32,}`)},
	{kind: "google-api-key", pattern: regexp.MustCompile(`AIza[A-Za-z0-9_-]{20,}`)},
	{
		kind:    "aws-access-key",
		pattern: regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
		accept: func(candidate string) bool {
			return !strings.Contains(candidate, "EXAMPLE")
		},
	},
}

const structuredCookieLineWindow = 5

var structuredCookieValueLineRE = regexp.MustCompile(`(?i)["']value["']\s*:\s*["']([^"']{8,})["']`)

func FindVendorPrefixSecrets(root string) ([]VendorPrefixSecretFinding, error) {
	return findSecrets(root, vendorPrefixSecretPatterns)
}

func FindPackageSecrets(root string, cookieNames []string) ([]VendorPrefixSecretFinding, error) {
	patterns := append([]vendorPrefixSecretPattern(nil), vendorPrefixSecretPatterns...)
	patterns = append(patterns, cookieSecretPatterns(cookieNames)...)
	return findSecrets(root, patterns)
}

func FindSpecDeclaredCookieSecrets(root string, cookieNames []string) ([]VendorPrefixSecretFinding, error) {
	return findSecrets(root, cookieSecretPatterns(cookieNames))
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

func findSecrets(root string, patterns []vendorPrefixSecretPattern) ([]VendorPrefixSecretFinding, error) {
	var findings []VendorPrefixSecretFinding
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
		findings = append(findings, fileFindings...)
		return nil
	})
	return findings, err
}

func scanSecretFile(root, path string, patterns []vendorPrefixSecretPattern) ([]VendorPrefixSecretFinding, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReaderSize(file, 8192)
	probe, err := reader.Peek(8192)
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return nil, err
	}
	if bytes.Contains(probe, []byte{0}) {
		return nil, nil
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(rel)

	var findings []VendorPrefixSecretFinding
	lineNumber := 0
	cookieNames := declaredCookieNamesFromPatterns(patterns)
	pendingCookieNames := map[string]int{}
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil && readErr != io.EOF {
			return nil, readErr
		}
		if line == "" && readErr == io.EOF {
			break
		}
		lineNumber++
		for _, pattern := range patterns {
			if vendorPrefixSecretLineMatch(pattern, line) {
				findings = append(findings, VendorPrefixSecretFinding{
					Path: rel,
					Line: lineNumber,
					Kind: pattern.kind,
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
				findings = append(findings, VendorPrefixSecretFinding{
					Path: rel,
					Line: lineNumber,
					Kind: "cookie-value:" + name,
				})
				delete(pendingCookieNames, name)
			}
		}
		if readErr == io.EOF {
			break
		}
	}
	return findings, nil
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

func vendorPrefixSecretLineMatch(pattern vendorPrefixSecretPattern, line string) bool {
	for _, candidate := range pattern.pattern.FindAllString(line, -1) {
		if pattern.accept == nil || pattern.accept(candidate) {
			return true
		}
	}
	return false
}
