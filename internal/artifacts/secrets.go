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

func FindVendorPrefixSecrets(root string) ([]VendorPrefixSecretFinding, error) {
	var findings []VendorPrefixSecretFinding
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		fileFindings, err := scanVendorPrefixSecretFile(root, path)
		if err != nil {
			return err
		}
		findings = append(findings, fileFindings...)
		return nil
	})
	return findings, err
}

func FormatVendorPrefixSecretFindings(findings []VendorPrefixSecretFinding) string {
	lines := make([]string, 0, len(findings))
	for _, finding := range findings {
		lines = append(lines, fmt.Sprintf("%s:%d %s", finding.Path, finding.Line, finding.Kind))
	}
	return strings.Join(lines, "\n")
}

func scanVendorPrefixSecretFile(root, path string) ([]VendorPrefixSecretFinding, error) {
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
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil && readErr != io.EOF {
			return nil, readErr
		}
		if line == "" && readErr == io.EOF {
			break
		}
		lineNumber++
		for _, pattern := range vendorPrefixSecretPatterns {
			if vendorPrefixSecretLineMatch(pattern, line) {
				findings = append(findings, VendorPrefixSecretFinding{
					Path: rel,
					Line: lineNumber,
					Kind: pattern.kind,
				})
			}
		}
		if readErr == io.EOF {
			break
		}
	}
	return findings, nil
}

func vendorPrefixSecretLineMatch(pattern vendorPrefixSecretPattern, line string) bool {
	for _, candidate := range pattern.pattern.FindAllString(line, -1) {
		if pattern.accept == nil || pattern.accept(candidate) {
			return true
		}
	}
	return false
}
