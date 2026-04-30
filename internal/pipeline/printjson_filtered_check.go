package pipeline

import (
	"os"
	"path/filepath"
	"strings"
)

// PrintJSONFilteredCheckResult flags hand-written novel commands that
// emit JSON via flags.printJSON(cmd, v) instead of
// printJSONFiltered(cmd.OutOrStdout(), v, flags). The former silently
// drops --select, --compact, --csv, and --quiet — agents requesting
// narrower output still receive the full payload.
type PrintJSONFilteredCheckResult struct {
	Checked  int                        `json:"checked"`
	Findings []PrintJSONFilteredFinding `json:"findings,omitempty"`
	Skipped  bool                       `json:"skipped,omitempty"`
}

// PrintJSONFilteredFinding names a single antipattern call site.
type PrintJSONFilteredFinding struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

// printJSONFilteredAntipattern is the literal call shape we flag. gofmt
// normalizes spacing inside Go call expressions, so a substring match
// is sufficient — no need for whitespace-flexible regex.
const printJSONFilteredAntipattern = "flags.printJSON(cmd,"

// printJSONFilteredSnippetMax bounds the snippet stored per finding so
// a multi-hundred-char struct-literal call site doesn't bloat
// dogfood.json. Truncated snippets get an ellipsis suffix.
const printJSONFilteredSnippetMax = 120

func checkPrintJSONFiltered(cliDir string) PrintJSONFilteredCheckResult {
	cliPkgDir := filepath.Join(cliDir, "internal", "cli")
	if _, err := os.Stat(cliPkgDir); err != nil {
		return PrintJSONFilteredCheckResult{Skipped: true}
	}

	result := PrintJSONFilteredCheckResult{}
	for _, path := range listGoFiles(cliPkgDir) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		result.Checked++

		// Walk line-by-line so each finding carries its own line number
		// and snippet. The number of files is small (low tens at most)
		// and per-file size is small (low thousands of bytes), so the
		// strings.Split allocation is in the noise vs. the surrounding
		// os.ReadFile cost.
		for lineIdx, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(line, printJSONFilteredAntipattern) {
				continue
			}
			result.Findings = append(result.Findings, PrintJSONFilteredFinding{
				File:    filepath.ToSlash(filepath.Join("internal", "cli", filepath.Base(path))),
				Line:    lineIdx + 1,
				Snippet: truncateSnippet(strings.TrimSpace(line), printJSONFilteredSnippetMax),
			})
		}
	}
	return result
}

// truncateSnippet caps s at maxRunes runes (UTF-8 safe — splitting
// mid-rune would corrupt the dogfood.json output) and appends an
// ellipsis when truncation occurred.
func truncateSnippet(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}
