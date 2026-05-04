package pipeline

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SourceClientCheckResult flags hand-written sibling internal packages that
// make outbound HTTP calls without using a rate limiter or surfacing a typed
// 429 error. Empty-on-throttle is indistinguishable from "no data exists" and
// silently corrupts downstream queries.
type SourceClientCheckResult struct {
	Checked  int                   `json:"checked"`
	Findings []SourceClientFinding `json:"findings,omitempty"`
	Skipped  bool                  `json:"skipped,omitempty"`
}

type SourceClientFinding struct {
	// Package is the path under internal/ containing the offending file
	// (e.g. "source/sec"), so a finding self-locates without needing to
	// reconcile the Package and File fields.
	Package string `json:"package"`
	File    string `json:"file"`
	Reason  string `json:"reason"`
}

// limiterUseRe matches an invocation or construction of any limiter — the
// cliutil type, the constructor, or any of the lifecycle methods. Restricted
// to actual call sites so a struct field or unrelated identifier ending in
// "Limiter" does not satisfy the check.
var limiterUseRe = regexp.MustCompile(
	`cliutil\.(?:AdaptiveLimiter|NewAdaptiveLimiter)\b|` +
		`\.OnRateLimit\s*\(\s*\)|` +
		`\.OnSuccess\s*\(\s*\)|` +
		`\.Wait\s*\(\s*\)`)

// rateLimitErrorRe matches the typed-error contract: cliutil.RateLimitError,
// any locally-named RateLimitError type, or an explicit 429 status check
// whose then-branch the caller can route to a typed error path.
var rateLimitErrorRe = regexp.MustCompile(
	`cliutil\.RateLimitError\b|` +
		`\bRateLimitError\b|` +
		`StatusCode\s*==\s*(?:http\.StatusTooManyRequests|429)`)

// sourceCheckExtraSkip is the set of packages source_client_check skips that
// are not in reservedInternalPackages. internal/cli has its own treatment in
// reimplementation_check, so source_client_check leaves it alone.
var sourceCheckExtraSkip = map[string]bool{"cli": true}

// checkSourceClients walks internal/<pkg>/*.go for every <pkg> not covered by
// other dogfood checks and not generator-emitted. For each file that makes
// outbound HTTP calls, it requires both a limiter signal and a typed-error
// signal; missing either produces a finding.
func checkSourceClients(cliDir string) SourceClientCheckResult {
	internalDir := filepath.Join(cliDir, "internal")
	entries, err := os.ReadDir(internalDir)
	if err != nil {
		return SourceClientCheckResult{Skipped: true}
	}

	result := SourceClientCheckResult{}
	for _, pkgEntry := range entries {
		if !pkgEntry.IsDir() {
			continue
		}
		pkgName := pkgEntry.Name()
		if reservedInternalPackages[pkgName] || sourceCheckExtraSkip[pkgName] {
			continue
		}
		walkSourcePackage(cliDir, filepath.Join(internalDir, pkgName), &result)
	}

	if result.Checked == 0 && len(result.Findings) == 0 {
		result.Skipped = true
	}
	return result
}

func walkSourcePackage(cliDir, pkgDir string, result *SourceClientCheckResult) {
	_ = filepath.WalkDir(pkgDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		content := string(data)
		result.Checked++

		if !outboundHTTPCallRe.MatchString(content) {
			return nil
		}
		if hasGeneratedClientMediatedCall(content) && !rawOutboundHTTPCallRe.MatchString(content) {
			return nil
		}

		hasLimiter := limiterUseRe.MatchString(content)
		hasTypedError := rateLimitErrorRe.MatchString(content)
		if hasLimiter && hasTypedError {
			return nil
		}

		rel, _ := filepath.Rel(cliDir, path)
		pkgRel, _ := filepath.Rel(filepath.Join(cliDir, "internal"), filepath.Dir(path))
		finding := SourceClientFinding{
			Package: filepath.ToSlash(pkgRel),
			File:    filepath.ToSlash(rel),
		}
		switch {
		case !hasLimiter && !hasTypedError:
			finding.Reason = "outbound HTTP without rate limiter or typed 429 handling"
		case !hasLimiter:
			finding.Reason = "outbound HTTP without rate limiter (cliutil.AdaptiveLimiter or equivalent)"
		default:
			finding.Reason = "outbound HTTP without typed RateLimitError or 429 status check (swallows throttling as empty results)"
		}
		result.Findings = append(result.Findings, finding)
		return nil
	})
}

func hasGeneratedClientMediatedCall(content string) bool {
	return clientImportRe.MatchString(content) &&
		generatedClientParamRe.MatchString(content) &&
		generatedClientReceiverCallRe.MatchString(content)
}
