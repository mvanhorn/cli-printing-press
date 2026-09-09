package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnsureSafeEnetxHTTP exercises the post-tidy enetx/http pin against real
// modules. It needs network (go get / go mod tidy), so it skips in -short.
func TestEnsureSafeEnetxHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("ensureSafeEnetxHTTP runs go get / go mod tidy (network)")
	}

	t.Run("bumps a transitively-old enetx/http to the safe version", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
			[]byte("module enetxhttpbump\n\ngo 1.24.0\n\nrequire github.com/enetx/http v1.0.28\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
			[]byte("package main\n\nimport _ \"github.com/enetx/http\"\n\nfunc main() {}\n"), 0o644))
		if _, err := runCommand(dir, qualityGateTimeout, "go", "mod", "tidy"); err != nil {
			t.Skipf("initial go mod tidy failed (offline?): %v", err)
		}

		require.NoError(t, ensureSafeEnetxHTTP(dir))

		out, err := runCommand(dir, qualityGateTimeout, "go", "list", "-m", "-f", "{{.Version}}", "github.com/enetx/http")
		require.NoError(t, err)
		assert.Equal(t, safeEnetxHTTPVersion, out, "enetx/http should be bumped to the safe version")

		before, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		require.NoError(t, err)
		_, err = runCommand(dir, qualityGateTimeout, "go", "mod", "tidy")
		require.NoError(t, err)
		after, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		require.NoError(t, err)
		assert.Equal(t, string(before), string(after), "go.mod must be tidy after the bump")
	})

	t.Run("no-op when enetx/http is not a dependency", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
			[]byte("module noenetxhttp\n\ngo 1.24.0\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
			[]byte("package main\n\nfunc main() {}\n"), 0o644))

		require.NoError(t, ensureSafeEnetxHTTP(dir))

		gomod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		require.NoError(t, err)
		assert.NotContains(t, string(gomod), "github.com/enetx/http",
			"absent enetx/http must not gain an unused require")
	})
}

func TestSafeEnetxHTTPVersionMatchesGoModTemplate(t *testing.T) {
	tmpl, err := os.ReadFile(filepath.Join("templates", "go.mod.tmpl"))
	require.NoError(t, err)
	assert.Contains(t, string(tmpl), "github.com/enetx/http "+safeEnetxHTTPVersion,
		"templates/go.mod.tmpl pin must stay in sync with safeEnetxHTTPVersion")
}
