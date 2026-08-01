package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyFixRecognizesResponsePathPagination(t *testing.T) {
	t.Parallel()

	cliDir := filepath.Join(t.TempDir(), "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "helpers.go"), []byte("func printDryRun() {}\n"), 0o644))
	commandPath := filepath.Join(cliDir, "items.go")
	require.NoError(t, os.WriteFile(commandPath, []byte(`package cli

func run() error {
	data, err := paginatedGetWithResponsePath(cmd.Context(), c, path, map[string]string{}, nil, flagAll, "cursor", "cursor", "limit", 100, "", "", "results")
	return data, err
}
`), 0o644))

	require.NoError(t, applyFix(Fix{Issue: "dryrun_fail", File: commandPath}, filepath.Dir(filepath.Dir(cliDir))))
	updated, err := os.ReadFile(commandPath)
	require.NoError(t, err)
	require.Contains(t, string(updated), `if flags.dryRun {
			method := "GET"
			return printDryRun(cmd, method, path)
		}

			data, err := paginatedGetWithResponsePath(`)
}
