package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

func TestPathsResolverEmitsAndGeneratedCLIBuilds(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("paths-emission")
	outputDir := filepath.Join(t.TempDir(), "paths-emission-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	pathsSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "paths.go")
	require.Contains(t, pathsSrc, "type PathKind int")
	require.Contains(t, pathsSrc, `const envPrefix = "PATHS_EMISSION"`)
	require.Contains(t, pathsSrc, `return envPrefix + "_" + suffix`)
	require.Contains(t, pathsSrc, `envPrefix + "_HOME"`)
	for _, suffix := range naming.PathKindEnvSuffixes()[1:] {
		require.Contains(t, pathsSrc, `return "`+suffix+`"`)
	}

	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	require.Contains(t, rootSrc, `StringVar(&flags.homePath, "home"`)
	require.Contains(t, rootSrc, "cliutil.SetHomeOverride(flags.homePath)")

	requireGeneratedCompiles(t, outputDir)
}

func TestPathEnvPrefixDerivationCoversEdgeNames(t *testing.T) {
	t.Parallel()

	require.Equal(t, "CAL_COM", naming.EnvPrefix("Cal.com"))
	require.Equal(t, "API_1PASSWORD", naming.EnvPrefix("1Password"))
}

func TestDBFlagHelpIsStandardized(t *testing.T) {
	t.Parallel()

	const want = `cmd.Flags().StringVar(&dbPath, "db", "", "SQLite database file path (default: resolved data directory data.db)")`
	matches := 0
	for _, tmpl := range []string{
		"sync.go.tmpl",
		"search.go.tmpl",
		"graphql_sync.go.tmpl",
		"channel_workflow.go.tmpl",
		"analytics.go.tmpl",
		"live_ws.go.tmpl",
		"teach.go.tmpl",
		"insights/similar.go.tmpl",
		"insights/health_score.go.tmpl",
		"workflows/pm_load.go.tmpl",
		"workflows/pm_orphans.go.tmpl",
		"workflows/pm_stale.go.tmpl",
	} {
		data, err := templateFS.ReadFile(filepath.Join("templates", tmpl))
		require.NoError(t, err)
		matches += strings.Count(string(data), want)
	}
	require.Equal(t, 18, matches)
}

func TestTemplatesDoNotConstructRuntimeHomeRootsOutsideResolver(t *testing.T) {
	t.Parallel()

	allowed := map[string]bool{
		"auth_browser.go.tmpl":             true, // Chrome profile discovery and config-relative legacy sidecars.
		"cliutil_paths.go.tmpl":            true, // Owns the platform defaults.
		"cliutil_paths_test.go.tmpl":       true, // Asserts the platform defaults.
		"cliutil_credentials_test.go.tmpl": true, // Seeds legacy config states.
		"config.go.tmpl":                   true, // Config legacy fallback.
		"helpers.go.tmpl":                  true, // defaultDBPath legacy fallback after data-dir resolution failure.
		"jobs.go.tmpl":                     true, // Jobs legacy dotdir fallback.
		"profile.go.tmpl":                  true, // Profile legacy dotdir fallback.
		"readme.md.tmpl":                   true, // U7 owns user-facing docs.
		"skill.md.tmpl":                    true, // U7 owns user-facing docs.
		"learn/teach_log.go.tmpl":          true, // Teach-log legacy data-dir fallback.
		"learn/teach_log_test.go.tmpl":     true, // Resolver default assertions.
		"mcp_tools_test.go.tmpl":           true, // MCP parity test asserts platform defaults.
	}
	var offenders []string
	root := "templates"
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() || !strings.HasSuffix(path, ".tmpl") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)
		rel = filepath.ToSlash(rel)
		if allowed[rel] {
			return nil
		}
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		src := string(data)
		if strings.Contains(src, `filepath.Join(home, ".config"`) ||
			strings.Contains(src, `filepath.Join(home, ".local"`) ||
			strings.Contains(src, `filepath.Join(home, ".cache"`) ||
			strings.Contains(src, `filepath.Join(homeDir, ".config"`) ||
			strings.Contains(src, `filepath.Join(homeDir, ".local"`) ||
			strings.Contains(src, `filepath.Join(homeDir, ".cache"`) {
			offenders = append(offenders, rel)
		}
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, offenders)
}
