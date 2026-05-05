package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createRunDir creates a manuscripts/<key>/<runID>/research/ structure with a dummy file.
func createRunDir(t *testing.T, msRoot, key, runID string) {
	t.Helper()
	dir := filepath.Join(msRoot, key, runID, "research")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "brief.md"), []byte("test"), 0o644))
}

func TestResolveManuscriptDir(t *testing.T) {
	t.Run("exact match on API name", func(t *testing.T) {
		msRoot := t.TempDir()
		createRunDir(t, msRoot, "steam-web", "20260401-120000")

		dir, runID := resolveManuscriptDir(msRoot, "steam-web")
		assert.Equal(t, filepath.Join(msRoot, "steam-web"), dir)
		assert.Equal(t, "20260401-120000", runID)
	})

	t.Run("suffix strip: steam-web from steam-web-api", func(t *testing.T) {
		msRoot := t.TempDir()
		createRunDir(t, msRoot, "steam-web", "20260401-120000")

		dir, runID := resolveManuscriptDir(msRoot, "steam-web-api")
		assert.Equal(t, filepath.Join(msRoot, "steam-web"), dir)
		assert.Equal(t, "20260401-120000", runID)
	})

	t.Run("suffix strip: steam from steam-web", func(t *testing.T) {
		msRoot := t.TempDir()
		createRunDir(t, msRoot, "steam", "20260401-120000")

		dir, runID := resolveManuscriptDir(msRoot, "steam-web")
		assert.Equal(t, filepath.Join(msRoot, "steam"), dir)
		assert.Equal(t, "20260401-120000", runID)
	})

	t.Run("prefix match: steam dir matches steam-web lookup", func(t *testing.T) {
		msRoot := t.TempDir()
		createRunDir(t, msRoot, "steam", "20260401-120000")

		// steam-web-service doesn't strip to "steam" directly,
		// but "steam" is a prefix of "steam-web-service"
		dir, runID := resolveManuscriptDir(msRoot, "steam-web-service")
		assert.Equal(t, filepath.Join(msRoot, "steam"), dir)
		assert.Equal(t, "20260401-120000", runID)
	})

	t.Run("no match at all", func(t *testing.T) {
		msRoot := t.TempDir()
		createRunDir(t, msRoot, "notion", "20260401-120000")

		dir, runID := resolveManuscriptDir(msRoot, "steam-web")
		assert.Empty(t, dir)
		assert.Empty(t, runID)
	})

	t.Run("empty manuscripts root", func(t *testing.T) {
		msRoot := t.TempDir()

		dir, runID := resolveManuscriptDir(msRoot, "steam-web")
		assert.Empty(t, dir)
		assert.Empty(t, runID)
	})

	t.Run("directory exists but no runs (empty)", func(t *testing.T) {
		msRoot := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(msRoot, "steam"), 0o755))

		dir, runID := resolveManuscriptDir(msRoot, "steam-web")
		// Directory exists but findMostRecentRun returns "" — no match
		assert.Empty(t, dir)
		assert.Empty(t, runID)
	})

	t.Run("ADVERSARIAL: prefix collision — steam should NOT match steamgames", func(t *testing.T) {
		msRoot := t.TempDir()
		createRunDir(t, msRoot, "steam", "20260401-120000")

		// "steamgames" is NOT prefixed by "steam-" — no hyphen boundary
		dir, runID := resolveManuscriptDir(msRoot, "steamgames")
		assert.Empty(t, dir, "steam should NOT match steamgames (no hyphen boundary)")
		assert.Empty(t, runID)
	})

	t.Run("prefix WITH hyphen boundary works: steam matches steam-web", func(t *testing.T) {
		msRoot := t.TempDir()
		createRunDir(t, msRoot, "steam", "20260401-120000")

		// "steam-web" IS prefixed by "steam-" — hyphen boundary present
		dir, runID := resolveManuscriptDir(msRoot, "steam-web")
		assert.NotEmpty(t, dir, "steam should match steam-web (hyphen boundary)")
		assert.Equal(t, "20260401-120000", runID)
	})

	t.Run("ADVERSARIAL: multiple candidates — picks first alphabetically", func(t *testing.T) {
		msRoot := t.TempDir()
		createRunDir(t, msRoot, "steam", "20260401-120000")
		createRunDir(t, msRoot, "steam-web", "20260401-130000")

		// With API name "steam-web-api", suffix strip finds "steam-web" first
		dir, runID := resolveManuscriptDir(msRoot, "steam-web-api")
		assert.Equal(t, filepath.Join(msRoot, "steam-web"), dir)
		assert.Equal(t, "20260401-130000", runID)
	})

	t.Run("picks most recent run when multiple exist", func(t *testing.T) {
		msRoot := t.TempDir()
		createRunDir(t, msRoot, "steam", "20260331-120000")
		createRunDir(t, msRoot, "steam", "20260401-120000")
		createRunDir(t, msRoot, "steam", "20260330-120000")

		dir, runID := resolveManuscriptDir(msRoot, "steam-web")
		assert.Equal(t, filepath.Join(msRoot, "steam"), dir)
		assert.Equal(t, "20260401-120000", runID) // most recent by lexicographic sort
	})
}

// withManuscriptsRoot wires a temp dir as PRINTING_PRESS_HOME and returns the
// manuscripts/ path, giving tests deterministic control over what
// resolveManuscripts sees on disk.
func withManuscriptsRoot(t *testing.T) string {
	t.Helper()
	t.Setenv("PRINTING_PRESS_HOME", t.TempDir())
	msRoot := pipeline.PublishedManuscriptsRoot()
	require.NoError(t, os.MkdirAll(msRoot, 0o755))
	return msRoot
}

func TestManuscriptLookupPriority(t *testing.T) {
	// Exercises resolveManuscripts directly: API-slug (SKILL convention)
	// wins over CLI-name (legacy binary convention); fuzzy is the last resort.

	t.Run("prefers API name over CLI name when both exist", func(t *testing.T) {
		msRoot := withManuscriptsRoot(t)
		createRunDir(t, msRoot, "steam-web", "run-api-recent")
		createRunDir(t, msRoot, "steam-web-pp-cli", "run-cli-old")

		dir, runID := resolveManuscripts("steam-web-pp-cli", "steam-web")
		assert.Equal(t, filepath.Join(msRoot, "steam-web"), dir)
		assert.Equal(t, "run-api-recent", runID)
	})

	t.Run("falls back to CLI name when API name dir missing (legacy fallback)", func(t *testing.T) {
		msRoot := withManuscriptsRoot(t)
		createRunDir(t, msRoot, "steam-web-pp-cli", "run-cli")

		dir, runID := resolveManuscripts("steam-web-pp-cli", "steam-web")
		assert.Equal(t, filepath.Join(msRoot, "steam-web-pp-cli"), dir)
		assert.Equal(t, "run-cli", runID)
	})

	t.Run("returns API name dir when only API name exists", func(t *testing.T) {
		msRoot := withManuscriptsRoot(t)
		createRunDir(t, msRoot, "steam-web", "run-api")

		dir, runID := resolveManuscripts("steam-web-pp-cli", "steam-web")
		assert.Equal(t, filepath.Join(msRoot, "steam-web"), dir)
		assert.Equal(t, "run-api", runID)
	})

	t.Run("falls back to fuzzy when neither named key has runs", func(t *testing.T) {
		msRoot := withManuscriptsRoot(t)
		createRunDir(t, msRoot, "steam", "run-slug")

		dir, runID := resolveManuscripts("steam-web-pp-cli", "steam-web")
		assert.Equal(t, filepath.Join(msRoot, "steam"), dir)
		assert.Equal(t, "run-slug", runID)
	})

	t.Run("returns empty when nothing matches", func(t *testing.T) {
		msRoot := withManuscriptsRoot(t)
		createRunDir(t, msRoot, "notion", "run-notion")

		dir, runID := resolveManuscripts("steam-web-pp-cli", "steam-web")
		assert.Empty(t, dir)
		assert.Empty(t, runID)
	})

	t.Run("regression: cal-com prefers API-slug recent run over stale CLI-name run", func(t *testing.T) {
		// Pre-fix, resolveManuscripts returned the stale CLI-name run instead.
		msRoot := withManuscriptsRoot(t)
		createRunDir(t, msRoot, "cal-com", "20260504-205634")
		createRunDir(t, msRoot, "cal-com-pp-cli", "20260405-183800")

		dir, runID := resolveManuscripts("cal-com-pp-cli", "cal-com")
		assert.Equal(t, filepath.Join(msRoot, "cal-com"), dir)
		assert.Equal(t, "20260504-205634", runID)
	})

	t.Run("empty apiName falls back to TrimCLISuffix(cliName)", func(t *testing.T) {
		msRoot := withManuscriptsRoot(t)
		createRunDir(t, msRoot, "steam-web", "run-derived")

		dir, runID := resolveManuscripts("steam-web-pp-cli", "")
		assert.Equal(t, filepath.Join(msRoot, "steam-web"), dir)
		assert.Equal(t, "run-derived", runID)
	})

	t.Run("empty manuscripts root returns empty without erroring", func(t *testing.T) {
		_ = withManuscriptsRoot(t)

		dir, runID := resolveManuscripts("steam-web-pp-cli", "steam-web")
		assert.Empty(t, dir)
		assert.Empty(t, runID)
	})
}
