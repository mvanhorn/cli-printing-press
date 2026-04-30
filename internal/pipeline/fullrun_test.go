package pipeline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v3/internal/llmpolish"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFullRun(t *testing.T) {
	if os.Getenv("FULL_RUN") == "" {
		t.Skip("Set FULL_RUN=1 to run full press test")
	}

	// Build the press binary first
	pressBinary := filepath.Join(t.TempDir(), "printing-press")
	repoRoot := findRepoRoot()
	cmd := exec.Command("go", "build", "-o", pressBinary, "./cmd/printing-press")
	cmd.Dir = repoRoot
	require.NoError(t, cmd.Run(), "failed to build printing-press")

	baseDir := filepath.Join(os.TempDir(), "press-fullrun-"+time.Now().Format("150405"))
	os.MkdirAll(baseDir, 0755)

	apis := []struct {
		name, level, flag, url string
	}{
		{"petstore", "EASY", "--spec", "https://petstore3.swagger.io/api/v3/openapi.json"},
		{"plaid", "MEDIUM", "--spec", "https://raw.githubusercontent.com/plaid/plaid-openapi/master/2020-09-14.yml"},
		{"notion", "HARD", "--docs", "https://developers.notion.com/reference"},
	}

	var results []*FullRunResult
	for _, api := range apis {
		t.Run(api.name, func(t *testing.T) {
			outputDir := filepath.Join(baseDir, api.name+"-cli")
			result := MakeBestCLI(api.name, api.level, api.flag, api.url, outputDir, pressBinary)
			results = append(results, result)

			assert.Equal(t, 7, result.GatesPassed, "%s: all 7 gates should pass", api.name)
			assert.True(t, result.CommandCount > 0, "%s: should have commands", api.name)
			assert.NotNil(t, result.Scorecard, "%s: should have scorecard", api.name)
		})
	}

	// Print comparison table
	table := PrintComparisonTable(results)
	fmt.Println(table)

	// Write learnings plan
	learningsPath := filepath.Join(baseDir, "learnings-plan.md")
	GenerateLearningsPlan(results, learningsPath)
	fmt.Printf("Learnings plan: %s\n", learningsPath)

	// Also write results to file
	os.WriteFile(filepath.Join(baseDir, "comparison-table.txt"), []byte(table), 0644)
	fmt.Printf("Full results at: %s\n", baseDir)
}

func TestFullRunQualitySpecPath(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "https://example.com/openapi.yaml", fullRunQualitySpecPath("--spec", "https://example.com/openapi.yaml"))
	assert.Equal(t, "", fullRunQualitySpecPath("--docs", "https://example.com/docs"))
}

func TestPrintComparisonTableRows(t *testing.T) {
	t.Parallel()

	table := PrintComparisonTable([]*FullRunResult{{
		APIName:         "demo",
		Level:           "EASY",
		GatesPassed:     6,
		CommandCount:    9,
		ResourceCount:   5,
		CoveragePercent: 83,
		Research:        &ResearchResult{Alternatives: []Alternative{{Name: "alt"}}},
		Dogfood:         &DogfoodReport{Verdict: "PASS", PathCheck: PathCheckResult{Tested: 2, Pct: 100}},
		Verification:    &VerificationReport{Verdict: "WARN", HallucinatedPaths: 1, DeadFlags: 2, GhostTables: 3},
		WorkflowVerify:  &WorkflowVerifyReport{Verdict: WorkflowVerdictPass},
		PolishResult:    &llmpolish.PolishResult{HelpTextsImproved: 2, ExamplesAdded: 3, READMERewritten: true},
		FixPlans:        []string{"fix-auth", "fix-docs"},
		Duration:        2 * time.Second,
		Errors:          []string{"boom"},
		Scorecard: &Scorecard{
			OverallGrade: "B",
			Steinberger: SteinerScore{
				OutputModes: 8, Auth: 7, ErrorHandling: 6, TerminalUX: 5,
				README: 4, Doctor: 3, AgentNative: 2, LocalCache: 1,
				Total: 72, Percentage: 72,
			},
			CompetitorScores: []CompScore{{WeWin: true}, {WeWin: false}},
		},
	}})

	expectedOrder := []string{
		"Quality Gates", "Commands", "Resources", "API Coverage",
		"Output Modes", "Auth", "Error Handling", "Terminal UX", "README", "Doctor", "Agent Native", "Local Cache",
		"Steinberger Total", "Grade", "Competitors Found", "We Win?", "Dogfood", "Verification", "Workflow Verify", "LLM Polish", "Fix Plans", "Duration", "Errors",
	}
	last := -1
	for _, label := range expectedOrder {
		idx := strings.Index(table, label)
		require.NotEqual(t, -1, idx, "missing row %q in:\n%s", label, table)
		require.Greater(t, idx, last, "row %q out of order in:\n%s", label, table)
		last = idx
	}

	assert.Contains(t, table, "6/7 PASS")
	assert.Contains(t, table, "72/80 (72%)")
	assert.Contains(t, table, "1/2")
	assert.Contains(t, table, "PASS 100%")
	assert.Contains(t, table, "WARN 1p 2f 3g")
	assert.Contains(t, table, string(WorkflowVerdictPass))
	assert.Contains(t, table, "2h/3e/true")
	assert.Contains(t, table, "2s")
}

func findRepoRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}
