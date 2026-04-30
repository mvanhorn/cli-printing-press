package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

// LiveStatus is the outcome of one feature's live check.
type LiveStatus string

const (
	StatusPass LiveStatus = "pass"
	StatusFail LiveStatus = "fail"
	StatusSkip LiveStatus = "skip"
)

// Default bounds for RunLiveCheck. Exported so callers can override via
// LiveCheckOptions without hard-coding magic numbers.
const (
	DefaultLiveCheckTimeout     = 10 * time.Second
	DefaultLiveCheckConcurrency = 4
	// MaxOutputBytes caps the stdout captured from each feature invocation.
	// Relevance matching only needs a few hundred bytes; a 1 MiB cap keeps a
	// misbehaving feature from exhausting the scorecard process's memory.
	MaxOutputBytes = 1 << 20
)

// LiveCheckResult summarizes a live-behavior sampling of a printed CLI's
// novel features. For each novel feature with an Example command, the check
// runs the command against real targets (the CLI's actual configured API or
// sites) and asserts the output has the shape a working feature would produce:
// non-empty, query-relevant when the command encodes a query.
//
// Produced by RunLiveCheck. Consumed by the scorecard to apply a behavioral
// correctness cap on the Insight dimension — a Grade A scorecard with a
// flagship feature returning wrong data shouldn't be possible.
type LiveCheckResult struct {
	Passed   int                 `json:"passed"`
	Failed   int                 `json:"failed"`
	Skipped  int                 `json:"skipped"`
	PassRate float64             `json:"-"` // exposed via pass_rate_pct in MarshalJSON
	Features []LiveFeatureResult `json:"features"`
	Unable   bool                `json:"unable,omitempty"`
	Reason   string              `json:"reason,omitempty"`
	RanAt    time.Time           `json:"ran_at"`
}

// Checked returns the total number of features that were sampled.
// Derived; not persisted to avoid the three-counters-for-three-states
// redundancy that makes invariants easy to drift.
func (r *LiveCheckResult) Checked() int {
	if r == nil {
		return 0
	}
	return r.Passed + r.Failed + r.Skipped
}

// LiveFeatureResult is one feature's outcome.
//
// OutputSample carries a bounded snapshot of the captured stdout so
// downstream consumers (Phase 4.85's agentic output review in particular)
// can inspect what the command actually produced without re-invoking the
// CLI. Re-invocation is brittle for stochastic endpoints, expensive for
// rate-limited ones, and auth-dependent for most — persisting a sample
// avoids all three.
//
// Warnings carries advisory findings that don't flip the feature's Status
// — the plan's Wave B ships output-quality checks (like raw HTML entities)
// as warnings for a 2-week calibration window before Wave C escalates them
// to failures. Consumers should surface warnings in reports but not factor
// them into pass-rate math until Wave C lands.
type LiveFeatureResult struct {
	Name         string     `json:"name"`
	Command      string     `json:"command"`
	Example      string     `json:"example"`
	Status       LiveStatus `json:"status"`
	Reason       string     `json:"reason,omitempty"`
	Warnings     []string   `json:"warnings,omitempty"`
	OutputSample string     `json:"output_sample,omitempty"`
}

// outputSampleMaxBytes caps the captured-output snapshot stored on each
// LiveFeatureResult. The raw capture buffer allows up to MaxOutputBytes
// (1 MiB) but the serialized sample is bounded much tighter so scorecard
// JSON files stay readable and agentic reviewers don't blow through their
// context window on one feature's output.
const outputSampleMaxBytes = 4096

// LiveCheckOptions bundles the optional knobs for RunLiveCheck. CLIDir is
// required; every other field has a sensible zero-value default.
type LiveCheckOptions struct {
	// CLIDir is the printed CLI's root (which holds the built binary).
	CLIDir string
	// ResearchDir, when non-empty, names a directory that holds research.json
	// instead of CLIDir. Use this when invoking live-check from a working
	// directory the run state owns (where research.json lives next to the
	// run's manuscripts) and the printed CLI hasn't been promoted to its
	// final library location. Leave blank to keep the historical behavior
	// of looking for research.json under CLIDir.
	ResearchDir string
	// BinaryName, when non-empty, names the executable to run. Leave blank
	// to let RunLiveCheck derive it from CLIDir (tries `<base>-pp-cli`,
	// falls back to `<base>`).
	BinaryName string
	// Timeout bounds each feature invocation. Zero uses DefaultLiveCheckTimeout.
	Timeout time.Duration
	// Concurrency sets the parallel-feature worker count. Zero uses
	// DefaultLiveCheckConcurrency. Set to 1 to force serial execution.
	Concurrency int
}

// RunLiveCheck samples novel feature Example commands against the real CLI.
// When novel features are absent, it falls back to generated leaf commands
// discovered from agent-context. Returns an Unable=true result (not an error)
// when research.json, the binary, or any sampleable command is missing — the
// scorecard treats those as "could not run" rather than failure, so an absent
// check doesn't penalize the CLI.
func RunLiveCheck(opts LiveCheckOptions) *LiveCheckResult {
	out := &LiveCheckResult{RanAt: time.Now().UTC()}

	if opts.CLIDir == "" {
		out.Unable = true
		out.Reason = "CLIDir is required"
		return out
	}

	researchDir := opts.ResearchDir
	if researchDir == "" {
		researchDir = opts.CLIDir
	}
	research, err := LoadResearch(researchDir)
	if err != nil {
		out.Unable = true
		out.Reason = "no research.json: " + err.Error()
		return out
	}

	binaryPath, binErr := resolveBinaryPath(opts.CLIDir, opts.BinaryName)
	if binErr != nil {
		out.Unable = true
		out.Reason = binErr.Error()
		return out
	}
	features := pickFeatures(research)
	if len(features) == 0 {
		var fallbackErr error
		features, fallbackErr = pickGeneratedCommandFeatures(binaryPath)
		if fallbackErr != nil {
			out.Unable = true
			out.Reason = "no novel features with Example commands and no generated command fallback: " + fallbackErr.Error()
			return out
		}
		if len(features) == 0 {
			out.Unable = true
			out.Reason = "no novel features with Example commands and no generated command leaves to sample"
			return out
		}
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultLiveCheckTimeout
	}
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = DefaultLiveCheckConcurrency
	}
	if concurrency > len(features) {
		concurrency = len(features)
	}

	results := runFeaturesConcurrent(opts.CLIDir, binaryPath, features, timeout, concurrency)
	out.Features = results
	for _, r := range results {
		switch r.Status {
		case StatusPass:
			out.Passed++
		case StatusFail:
			out.Failed++
		default:
			out.Skipped++
		}
	}
	if total := out.Checked(); total > 0 {
		out.PassRate = float64(out.Passed) / float64(total)
	}
	return out
}

// resolveBinaryPath returns the absolute path to the CLI binary. When name
// is non-empty it's used verbatim; otherwise RunLiveCheck tries the common
// `<base>-pp-cli` naming convention and falls back to `<base>`.
func resolveBinaryPath(cliDir, name string) (string, error) {
	candidates := []string{name}
	if name == "" {
		base := filepath.Base(cliDir)
		candidates = []string{base + "-pp-cli", base}
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		path := filepath.Join(cliDir, candidate)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.Mode()&0o111 == 0 {
			return "", fmt.Errorf("binary %q is not executable", path)
		}
		return path, nil
	}
	return "", fmt.Errorf("no runnable binary found in %q (tried %v)", cliDir, candidates)
}

// runFeaturesConcurrent distributes the per-feature checks across a worker
// pool. Results are collected in-order so LiveCheckResult.Features stays
// stable across runs.
func runFeaturesConcurrent(cliDir, binaryPath string, features []NovelFeature, timeout time.Duration, concurrency int) []LiveFeatureResult {
	results := make([]LiveFeatureResult, len(features))
	type job struct{ idx int }
	jobs := make(chan job, len(features))
	for i := range features {
		jobs <- job{idx: i}
	}
	close(jobs)

	var wg sync.WaitGroup
	for range concurrency {
		wg.Go(func() {
			for j := range jobs {
				results[j.idx] = runOneFeatureCheck(cliDir, binaryPath, features[j.idx], timeout)
			}
		})
	}
	wg.Wait()
	return results
}

// pickFeatures returns the novel features to sample. Prefers NovelFeaturesBuilt
// (the verified subset) when dogfood has run; falls back to NovelFeatures.
// Only features with a non-empty Example are usable — the rest are skipped
// silently since we have no invocation to run.
func pickFeatures(r *ResearchResult) []NovelFeature {
	source := r.NovelFeatures
	if r.NovelFeaturesBuilt != nil && len(*r.NovelFeaturesBuilt) > 0 {
		source = *r.NovelFeaturesBuilt
	}
	var out []NovelFeature
	for _, f := range source {
		if strings.TrimSpace(f.Example) != "" {
			out = append(out, f)
		}
	}
	return out
}

func pickGeneratedCommandFeatures(binaryPath string) ([]NovelFeature, error) {
	out, err := runStdoutOnly(binaryPath, 15*time.Second, "agent-context")
	if err != nil {
		return nil, fmt.Errorf("agent-context failed: %w", err)
	}
	paths, err := dogfoodExampleCommandPathsFromAgentContext(out)
	if err != nil {
		return nil, err
	}
	if len(paths) > 5 {
		paths = sampleEvenlyCommandPaths(paths, 5)
	}
	binaryName := filepath.Base(binaryPath)
	features := make([]NovelFeature, 0, len(paths))
	for _, path := range paths {
		command := strings.Join(path, " ")
		features = append(features, NovelFeature{
			Name:        command,
			Command:     command,
			Description: "Generated command " + command,
			Example:     binaryName + " " + command + " --json",
		})
	}
	return features, nil
}

// runOneFeatureCheck parses the Example invocation, runs it against the real
// binary, and evaluates the output shape. The Example is expected to start
// with the binary name (e.g., "recipe-goat-pp-cli goat \"brownies\" --limit 5");
// we drop that prefix and replace it with the absolute binary path so the
// check works regardless of the caller's PATH.
//
// Note: runCLIWithOutput in runtime.go uses CombinedOutput (stdout+stderr
// merged) and wraps non-zero exits as a generic error. This check instead
// separates stdout (for the relevance pass) from stderr (for failure
// messaging) and needs structured access to *exec.ExitError +
// DeadlineExceeded, so it runs exec inline.
func runOneFeatureCheck(cliDir, binaryPath string, f NovelFeature, timeout time.Duration) LiveFeatureResult {
	result := LiveFeatureResult{Name: f.Name, Command: f.Command, Example: f.Example}
	fail := func(reason string) LiveFeatureResult {
		result.Status = StatusFail
		result.Reason = reason
		return result
	}

	args, err := parseExampleArgs(f.Example)
	if err != nil {
		result.Status = StatusSkip
		result.Reason = "could not parse example: " + err.Error()
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = cliDir
	// Capture stdout into a bounded buffer. An unbounded `cmd.Output()` call
	// would let a misbehaving feature exhaust the scorecard's memory.
	stdoutCap := &bytes.Buffer{}
	stderrCap := &bytes.Buffer{}
	cmd.Stdout = &limitedWriter{w: stdoutCap, remaining: MaxOutputBytes}
	cmd.Stderr = &limitedWriter{w: stderrCap, remaining: MaxOutputBytes}
	runErr := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fail(fmt.Sprintf("timed out after %s", timeout))
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return fail(fmt.Sprintf("exit %d: %s", exitErr.ExitCode(), trimOutput(stderrCap.String())))
		}
		return fail("run error: " + runErr.Error())
	}
	if strings.TrimSpace(stdoutCap.String()) == "" {
		return fail("empty output")
	}

	if query := extractQueryToken(args, f.Command); query != "" {
		if !outputMentionsQuery(stdoutCap.String(), query) {
			return fail(fmt.Sprintf("output does not contain any token from query %q", query))
		}
	}

	stdout := stdoutCap.String()
	result.Status = StatusPass
	result.OutputSample = sampleOutput(stdout)
	if msg := detectRawHTMLEntities(stdout, args); msg != "" {
		result.Warnings = append(result.Warnings, msg)
	}
	return result
}

// sampleOutput truncates captured stdout to outputSampleMaxBytes for
// persistence on LiveFeatureResult.OutputSample. An ellipsis marker at the
// boundary tells downstream readers the snapshot is truncated.
func sampleOutput(s string) string {
	if len(s) <= outputSampleMaxBytes {
		return s
	}
	return s[:outputSampleMaxBytes] + "…[truncated]"
}

// rawHTMLEntityRe matches numeric HTML character references, both decimal
// (&#39;) and hex (&#x27;). Named entities (&amp;, &quot;, &lt;) are
// intentionally excluded in Wave B — they false-positive on legitimate JSON
// strings ("AT&amp;T") and on documentation text, so we calibrate the
// stricter numeric-only rule first. Wave C may broaden to named entities
// after observing the library's actual output patterns.
//
// The digit count is bounded at 10 to prevent a malicious output like
// "&#99999999999...;" from forcing the regex engine into a large matched
// span that then propagates into the warning message. Valid Unicode code
// points fit in 7 decimal digits (10FFFF = 1,114,111), so 10 is a generous
// upper bound that stays well inside regex budget.
var rawHTMLEntityRe = regexp.MustCompile(`&#[xX]?[0-9a-fA-F]{1,10};`)

// detectRawHTMLEntities returns a short human-readable reason when output
// contains raw numeric HTML entities, or "" when output is clean. Gated to
// non-JSON output so JSON-mode features (whose output legitimately contains
// escape sequences) don't trip the check.
//
// JSON-mode detection:
//   - any arg that is `--json` or begins with `--json=` (cobra accepts both)
//   - first non-whitespace character of output is `{` or `[`
//
// Both heuristics are conservative: a feature that renders JSON inside a
// human table would still be checked, which is the right behavior.
func detectRawHTMLEntities(output string, args []string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return ""
	}
	// Skip JSON-mode: agent-facing output legitimately contains escape
	// sequences and isn't rendered to a human terminal.
	for _, a := range args {
		if a == "--json" || strings.HasPrefix(a, "--json=") {
			return ""
		}
	}
	if first := trimmed[0]; first == '{' || first == '[' {
		return ""
	}
	match := rawHTMLEntityRe.FindString(output)
	if match == "" {
		return ""
	}
	// Cap the match-echo so a pathological output (e.g., a huge
	// numeric ref — guarded by the regex's 10-digit bound, but the
	// warning message still benefits from a defensive cap) can't
	// produce megabyte-sized warning strings in the scorecard JSON.
	if len(match) > 64 {
		match = match[:64] + "…"
	}
	return fmt.Sprintf("raw HTML entity %q in output (decode with cliutil.CleanText or equivalent)", match)
}

// limitedWriter caps the bytes forwarded to w at `remaining`; further writes
// are discarded (but still report as successful, so the subprocess doesn't
// SIGPIPE). Intentionally tolerant of truncation — the live check only needs
// enough output to run a relevance match.
type limitedWriter struct {
	w         io.Writer
	remaining int
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.remaining <= 0 {
		return len(p), nil
	}
	n := min(len(p), lw.remaining)
	if _, err := lw.w.Write(p[:n]); err != nil {
		return 0, err
	}
	lw.remaining -= n
	return len(p), nil
}

// parseExampleArgs takes an Example like:
//
//	recipe-goat-pp-cli goat "chicken tikka masala" --limit 5
//
// and returns the subcommand arguments (everything after the binary name).
// Respects double-quoted tokens so queries with spaces stay intact.
func parseExampleArgs(example string) ([]string, error) {
	tokens, err := shellSplit(example)
	if err != nil {
		return nil, err
	}
	if len(tokens) < 2 {
		return nil, fmt.Errorf("example has no subcommand: %q", example)
	}
	return tokens[1:], nil
}

// shellSplit handles double-quoted tokens — enough for the Example formats
// the absorb phase produces. No shell metacharacter interpolation is done.
// Single quotes, escaped characters, and backslashes are not recognized;
// Examples using those will need updating.
func shellSplit(s string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuote = !inQuote
		case c == ' ' && !inQuote:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(c)
		}
	}
	if inQuote {
		return nil, fmt.Errorf("unclosed quote in %q", s)
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens, nil
}

// extractQueryToken returns a positional argument that looks like a human-
// readable search query — the kind of token a search/filter command would
// mention in its output. Returns "" when no such argument exists, in which
// case no relevance check is performed.
//
// URLs and IDs are intentionally excluded: the CLI's output for a URL-based
// command (recipe get <url>) wouldn't contain the URL itself, so matching
// against it produces false negatives. commandPath is the cobra command
// path being exercised (e.g. "leaderboard top"); positionals that match a
// word from it are treated as subcommand names, not queries — without
// this, `leaderboard top` would treat `top` as a search query and fail
// the relevance heuristic against output that has no reason to echo it.
//
// Examples:
//
//	args=["goat", "brownies", "--limit", "5"], cmd="goat"           → "brownies"
//	args=["sub", "buttermilk"], cmd="sub"                            → "buttermilk"
//	args=["recipe", "get", "https://foo/bar"], cmd="recipe get"      → "" (URL, skip)
//	args=["cookbook", "list", "--json"], cmd="cookbook list"         → "" (no query)
//	args=["leaderboard", "top"], cmd="leaderboard top"               → "" (subcommand name)
func extractQueryToken(args []string, commandPath string) string {
	var positionals []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			break
		}
		positionals = append(positionals, arg)
	}
	if len(positionals) < 2 {
		return ""
	}
	candidate := positionals[len(positionals)-1]
	candidateLower := strings.ToLower(candidate)
	for word := range strings.FieldsSeq(strings.ToLower(commandPath)) {
		if word == candidateLower {
			return ""
		}
	}
	if looksLikeURLOrID(candidate) {
		return ""
	}
	if commonCommandVerb(candidate) {
		return ""
	}
	if len(candidate) < 3 {
		return ""
	}
	return candidate
}

func commonCommandVerb(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	// CRUD / action verbs.
	case "list", "get", "show", "fetch", "query", "search", "create", "update", "delete", "remove", "set", "run":
		return true
	// Time / state words used as command names rather than user-supplied
	// queries. Commands like `slices today`, `store tonight`, `events now`
	// are verb-shaped: the trailing word names the view, not a search term.
	// Without this branch, the rendered output (which has no reason to
	// echo "today" as text — it's structured data) would fail the
	// outputMentionsQuery heuristic and produce a spurious live-check
	// failure on otherwise-correct output.
	case "today", "tonight", "now", "current", "latest", "recent", "upcoming",
		"pending", "active", "expired", "stale", "all", "live", "open", "closed":
		return true
	default:
		return false
	}
}

// looksLikeURLOrID returns true for tokens that shouldn't be used as search-
// relevance queries: URLs, numeric IDs, UUIDs. The CLI output for a
// get-by-id command won't contain the ID as text.
func looksLikeURLOrID(s string) bool {
	if strings.Contains(s, "://") || strings.HasPrefix(s, "/") {
		return true
	}
	allDigits := true
	for _, c := range s {
		if c < '0' || c > '9' {
			allDigits = false
			break
		}
	}
	return allDigits
}

// outputMentionsQuery is case-insensitive; splits the query on whitespace
// and commas, then succeeds if any token (with singular/plural tolerance)
// appears in the output. Mirrors the permissive relevance check used
// inside generated CLIs. Splitting on commas catches comma-separated
// list queries like `pikachu,charizard,blastoise` whose individual names
// appear in JSON-array outputs separated by quote+comma+quote.
func outputMentionsQuery(output, query string) bool {
	lowered := strings.ToLower(output)
	splitOnQueryDelim := func(r rune) bool { return unicode.IsSpace(r) || r == ',' }
	for _, tok := range strings.FieldsFunc(strings.ToLower(query), splitOnQueryDelim) {
		tok = strings.TrimFunc(tok, func(r rune) bool { return r == '"' || r == '\'' })
		if len(tok) < 3 {
			continue
		}
		if strings.Contains(lowered, tok) {
			return true
		}
		if strings.HasSuffix(tok, "s") && len(tok) > 3 {
			if strings.Contains(lowered, tok[:len(tok)-1]) {
				return true
			}
		}
	}
	return false
}

func trimOutput(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}

// InsightCapFromLiveCheck returns the maximum Insight score a CLI should
// receive given its live-check pass rate. A CLI whose flagships return
// broken output shouldn't earn a Grade A scorecard.
//
//   - Unable or zero checked: no cap (nil return)
//   - PassRate >= 0.8: no cap
//   - PassRate >= 0.5: cap at 7
//   - PassRate <  0.5: cap at 4
func InsightCapFromLiveCheck(r *LiveCheckResult) *int {
	if r == nil || r.Unable || r.Checked() == 0 {
		return nil
	}
	var cap int
	switch {
	case r.PassRate >= 0.8:
		return nil
	case r.PassRate >= 0.5:
		cap = 7
	default:
		cap = 4
	}
	return &cap
}

// MarshalJSON emits a rounded pass_rate_pct alongside the raw counters so
// JSON consumers don't have to deal with floating-point noise. PassRate is
// hidden via json:"-" on the struct; this method computes the percentage
// once using an alias to avoid infinite recursion.
func (r *LiveCheckResult) MarshalJSON() ([]byte, error) {
	type alias LiveCheckResult
	return json.Marshal(&struct {
		*alias
		Checked     int `json:"checked"`
		PassRatePct int `json:"pass_rate_pct"`
	}{
		alias:       (*alias)(r),
		Checked:     r.Checked(),
		PassRatePct: int(r.PassRate*100 + 0.5),
	})
}
