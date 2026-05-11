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
// every `.go` file matches `gofmt -l`. Issue #1080: without an emit-phase
// format pass, hand-aligned struct literals in templates surface as hundreds
// of phantom diffs on the first `gofmt -w` in any printed CLI.
func TestEmittedGoFilesAreGofmtClean(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("gofmt-emit")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	var dirty []string
	require.NoError(t, filepath.WalkDir(outputDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		formatted, err := format.Source(src)
		if err != nil {
			// Unparseable Go would also surface as a build failure
			// downstream; surface it here so this test catches both
			// classes of regression at once.
			rel, _ := filepath.Rel(outputDir, path)
			dirty = append(dirty, rel+" (unparseable: "+err.Error()+")")
			return nil
		}
		if string(formatted) != string(src) {
			rel, _ := filepath.Rel(outputDir, path)
			dirty = append(dirty, rel)
		}
		return nil
	}))

	require.Empty(t, dirty,
		"generator emit phase must produce gofmt-clean Go files; dirty:\n  %s",
		strings.Join(dirty, "\n  "))
}
