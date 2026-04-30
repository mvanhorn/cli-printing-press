package pipeline

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// PrintJSONFilteredCheckResult flags hand-written novel commands that
// emit JSON via flags.printJSON(cmd, v) instead of the
// printJSONFiltered(cmd.OutOrStdout(), v, flags) helper. The former
// silently drops --select, --compact, --csv, and --quiet — agents
// requesting narrower output still receive the full payload, which
// burns context and corrupts pipelines that expect filtered fields.
//
// The check is structural: it greps internal/cli/*.go for the
// antipattern and reports each call site. It does not parse Go or try
// to verify the surrounding command is novel-feature vs. endpoint-
// mirror — generator-emitted endpoint-mirror commands route through
// printOutputWithFlags directly, never flags.printJSON, so any hit is
// either a hand-written novel command or a regression in a template.
type PrintJSONFilteredCheckResult struct {
	// Checked is the number of internal/cli/*.go files scanned (excludes
	// _test.go and the helpers.go file where flags.printJSON is defined).
	Checked int `json:"checked"`
	// Findings lists every antipattern call site discovered.
	Findings []PrintJSONFilteredFinding `json:"findings,omitempty"`
	// Skipped is true when the check could not run (no internal/cli dir).
	Skipped bool `json:"skipped,omitempty"`
}

// PrintJSONFilteredFinding names a single antipattern call site.
type PrintJSONFilteredFinding struct {
	// File is the path under internal/cli/ (e.g. "internal/cli/jobs.go")
	// so a finding self-locates without needing the cliDir prefix.
	File string `json:"file"`
	// Line is the 1-indexed line number of the offending call.
	Line int `json:"line"`
	// Snippet is the trimmed source line so a reviewer can see the
	// exact call shape without opening the file.
	Snippet string `json:"snippet"`
}

// flagsPrintJSONRe matches `flags.printJSON(cmd,` — the exact
// invocation shape that bypasses the filter pipeline. The trailing
// comma anchors against a real call (not a method-receiver definition,
// not a struct field, not a comment fragment).
var flagsPrintJSONRe = regexp.MustCompile(`\bflags\.printJSON\s*\(\s*cmd\s*,`)

// helpersFile is excluded from the scan because it carries the
// printJSON method definition itself; the regex above doesn't match
// the definition (no `cmd,` immediately after `(`), but the exclusion
// is a fast-path that also skips the helper's own callers if any.
const printJSONFilteredHelpersFile = "helpers.go"

func checkPrintJSONFiltered(cliDir string) PrintJSONFilteredCheckResult {
	cliPkgDir := filepath.Join(cliDir, "internal", "cli")
	entries, err := os.ReadDir(cliPkgDir)
	if err != nil {
		return PrintJSONFilteredCheckResult{Skipped: true}
	}

	result := PrintJSONFilteredCheckResult{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if name == printJSONFilteredHelpersFile {
			continue
		}
		path := filepath.Join(cliPkgDir, name)
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		result.Checked++

		// Walk line-by-line so each finding carries its own line number
		// and snippet. The number of files is small (low tens at most)
		// and the per-file size is small (low thousands of bytes), so
		// scanning by line is cheap and keeps the check output rich.
		for lineIdx, line := range strings.Split(string(data), "\n") {
			if !flagsPrintJSONRe.MatchString(line) {
				continue
			}
			result.Findings = append(result.Findings, PrintJSONFilteredFinding{
				File:    filepath.ToSlash(filepath.Join("internal", "cli", name)),
				Line:    lineIdx + 1,
				Snippet: strings.TrimSpace(line),
			})
		}
	}
	return result
}
