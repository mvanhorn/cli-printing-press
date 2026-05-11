package pipeline

import (
	"os"
	"path/filepath"
)

// PrintJSONFilteredCheckResult is retained for dogfood report compatibility.
// Before #826 it flagged hand-written novel commands that called
// flags.printJSON(cmd, v). That receiver-style helper now delegates through
// printJSONFiltered, so the check only records how many CLI files were scanned.
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

func checkPrintJSONFiltered(cliDir string) PrintJSONFilteredCheckResult {
	cliPkgDir := filepath.Join(cliDir, "internal", "cli")
	if _, err := os.Stat(cliPkgDir); err != nil {
		return PrintJSONFilteredCheckResult{Skipped: true}
	}

	result := PrintJSONFilteredCheckResult{}
	for range listGoFiles(cliPkgDir) {
		result.Checked++
	}
	return result
}
