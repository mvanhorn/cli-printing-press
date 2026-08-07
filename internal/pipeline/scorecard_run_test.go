package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestScorecardOnRealCLI(t *testing.T) {
	outputDir := os.Getenv("SCORECARD_CLI_DIR")
	if outputDir == "" {
		t.Skip("Set SCORECARD_CLI_DIR to run scorecard on a real CLI")
	}
	pipelineDir := os.Getenv("SCORECARD_PIPELINE_DIR")
	if pipelineDir == "" {
		pipelineDir = t.TempDir()
	}

	sc, err := RunScorecard(outputDir, pipelineDir, "", nil)
	if err != nil {
		t.Fatalf("RunScorecard: %v", err)
	}

	fmt.Printf("\n=== STEINBERGER SCORECARD ===\n")
	fmt.Printf("Score: %d/80 (%d%%)\n", sc.Steinberger.Total, sc.Steinberger.Percentage)
	fmt.Printf("Grade: %s\n\n", sc.OverallGrade)
	fmt.Printf("  Output Modes:   %d/10\n", sc.Steinberger.OutputModes)
	fmt.Printf("  Auth:           %d/10\n", sc.Steinberger.Auth)
	fmt.Printf("  Error Handling: %d/10\n", sc.Steinberger.ErrorHandling)
	fmt.Printf("  Terminal UX:    %d/10\n", sc.Steinberger.TerminalUX)
	fmt.Printf("  README:         %d/10\n", sc.Steinberger.README)
	fmt.Printf("  Doctor:         %d/10\n", sc.Steinberger.Doctor)
	fmt.Printf("  Agent Native:   %d/10\n", sc.Steinberger.AgentNative)
	fmt.Printf("  Local Cache:    %d/10\n", sc.Steinberger.LocalCache)
	if len(sc.GapReport) > 0 {
		fmt.Println("\nGaps:")
		for _, g := range sc.GapReport {
			fmt.Printf("  - %s\n", g)
		}
	}
	fmt.Println()
}

func TestRunScorecardReportsNovelFeatureDepthMismatches(t *testing.T) {
	outputDir := t.TempDir()
	cliDir := filepath.Join(outputDir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
func Execute() {
	rootCmd.AddCommand(newAssetsCmd())
}
`)
	writeFile(t, filepath.Join(cliDir, "assets.go"), `package cli
import "github.com/spf13/cobra"
func newAssetsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "assets"}
	cmd.AddCommand(newAssetsGrabCmd())
	return cmd
}
func newAssetsGrabCmd() *cobra.Command {
	return &cobra.Command{Use: "grab"}
}
`)

	pipelineDir := t.TempDir()
	if err := writeResearchJSON(&ResearchResult{
		APIName: "test",
		NovelFeatures: []NovelFeature{
			{Name: "Grab asset", Command: "grab", Example: `test-pp-cli grab "sunset"`},
		},
	}, pipelineDir); err != nil {
		t.Fatal(err)
	}

	sc, err := RunScorecard(outputDir, pipelineDir, "", nil)
	if err != nil {
		t.Fatalf("RunScorecard: %v", err)
	}
	if len(sc.NovelFeatureDepthMismatches) != 1 {
		t.Fatalf("expected one depth mismatch, got %#v", sc.NovelFeatureDepthMismatches)
	}
	want := "novel feature command-depth mismatch: grab advertised as grab but registered as assets grab"
	if !slices.Contains(sc.GapReport, want) {
		t.Fatalf("expected gap %q, got %#v", want, sc.GapReport)
	}
}
