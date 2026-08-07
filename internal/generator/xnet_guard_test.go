package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnsureSafeXNet exercises the post-tidy x/net pin against real modules.
// It needs network (go get / go mod tidy), so it skips in -short.
func TestEnsureSafeXNet(t *testing.T) {
	if testing.Short() {
		t.Skip("ensureSafeXNet runs go get / go mod tidy (network)")
	}

	t.Run("bumps a transitively-old x/net to the safe version", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
			[]byte("module xnetbump\n\ngo 1.23\n\nrequire golang.org/x/net v0.43.0\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
			[]byte("package main\n\nimport _ \"golang.org/x/net/html\"\n\nfunc main() {}\n"), 0o644))
		if _, err := runCommand(dir, qualityGateTimeout, "go", "mod", "tidy"); err != nil {
			t.Skipf("initial go mod tidy failed (offline?): %v", err)
		}

		require.NoError(t, ensureSafeXNet(dir))

		out, err := runCommand(dir, qualityGateTimeout, "go", "list", "-m", "-f", "{{.Version}}", "golang.org/x/net")
		require.NoError(t, err)
		assert.Equal(t, safeXNetVersion, out, "x/net should be bumped to the safe version")

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

	t.Run("no-op when x/net is not a dependency", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
			[]byte("module noxnet\n\ngo 1.23\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
			[]byte("package main\n\nfunc main() {}\n"), 0o644))

		// Must not error when go list -m reports x/net is unknown.
		require.NoError(t, ensureSafeXNet(dir))
	})
}
