package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnsureSafeXText exercises the post-tidy x/text pin against real modules.
// It needs network (go get / go mod tidy), so it skips in -short.
func TestEnsureSafeXText(t *testing.T) {
	if testing.Short() {
		t.Skip("ensureSafeXText runs go get / go mod tidy (network)")
	}

	t.Run("bumps a transitively-old x/text to the safe version", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
			[]byte("module xtextbump\n\ngo 1.23\n\nrequire golang.org/x/text v0.38.0\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
			[]byte("package main\n\nimport _ \"golang.org/x/text/unicode/norm\"\n\nfunc main() {}\n"), 0o644))
		if _, err := runCommand(dir, qualityGateTimeout, "go", "mod", "tidy"); err != nil {
			t.Skipf("initial go mod tidy failed (offline?): %v", err)
		}

		require.NoError(t, ensureSafeXText(dir))

		out, err := runCommand(dir, qualityGateTimeout, "go", "list", "-m", "-f", "{{.Version}}", "golang.org/x/text")
		require.NoError(t, err)
		assert.Equal(t, safeXTextVersion, out, "x/text should be bumped to the safe version")

		// The bump must leave the module tidy so the publish `go mod tidy`
		// gate stays a no-op.
		before, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		require.NoError(t, err)
		_, err = runCommand(dir, qualityGateTimeout, "go", "mod", "tidy")
		require.NoError(t, err)
		after, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		require.NoError(t, err)
		assert.Equal(t, string(before), string(after), "go.mod must be tidy after the bump")
	})

	t.Run("no-op when x/text is not a dependency", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
			[]byte("module noxtext\n\ngo 1.23\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
			[]byte("package main\n\nfunc main() {}\n"), 0o644))

		// Must not error when go list -m reports x/text is unknown.
		require.NoError(t, ensureSafeXText(dir))

		gomod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		require.NoError(t, err)
		assert.NotContains(t, string(gomod), "golang.org/x/text",
			"absent x/text must not gain an unused require")
	})
}
