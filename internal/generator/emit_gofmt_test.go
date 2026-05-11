package generator

import (
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
)

// TestEmittedGoFilesAreGofmtClean walks the freshly generated tree and asserts
// every `.go` file matches `gofmt -l`. Without an emit-phase format pass,
// hand-aligned struct literals in templates surface as hundreds of phantom
// diffs on the first `gofmt -w` in any printed CLI.
func TestEmittedGoFilesAreGofmtClean(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("gofmt-emit")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	dirty := findGofmtDirtyFiles(t, outputDir)
	require.Empty(t, dirty,
		"generator emit phase must produce gofmt-clean Go files; dirty:\n  %s",
		strings.Join(dirty, "\n  "))
}

// TestGenerateFromPlanGoFilesAreGofmtClean asserts the plan-based generation
// path produces gofmt-clean output too. GenerateFromPlan has its own render
// closure separate from Generator.Generate, so the format pass has to be
// wired through both paths.
func TestGenerateFromPlanGoFilesAreGofmtClean(t *testing.T) {
	t.Parallel()

	planSpec := &PlanSpec{
		CLIName:     "gofmt-plan",
		Description: "Plan-path gofmt regression",
		Commands: []PlanCommand{
			{Name: "record", Description: "Record"},
			{Name: "screenshot", Description: "Screenshot"},
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(planSpec.CLIName))
	require.NoError(t, os.MkdirAll(outputDir, 0o755))
	require.NoError(t, GenerateFromPlan(planSpec, outputDir))

	dirty := findGofmtDirtyFiles(t, outputDir)
	require.Empty(t, dirty,
		"GenerateFromPlan must produce gofmt-clean Go files; dirty:\n  %s",
		strings.Join(dirty, "\n  "))
}

func findGofmtDirtyFiles(t *testing.T, root string) []string {
	t.Helper()
	var dirty []string
	require.NoError(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(p) != ".go" {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		formatted, err := format.Source(src)
		if err != nil {
			// Unparseable Go would also surface as a build failure
			// downstream; surface it here so this test catches both
			// classes of regression at once.
			rel, _ := filepath.Rel(root, p)
			dirty = append(dirty, rel+" (unparseable: "+err.Error()+")")
			return nil
		}
		if string(formatted) != string(src) {
			rel, _ := filepath.Rel(root, p)
			dirty = append(dirty, rel)
		}
		return nil
	}))
	return dirty
}
