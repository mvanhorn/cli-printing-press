package generator

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGenerateShareRoundtripAcrossSnapshotTables proves the emitted share
// suite passes for an allowlist that includes sync_state and one that does
// not. The roundtrip must actually run; a skip would hide an empty assertion.
func TestGenerateShareRoundtripAcrossSnapshotTables(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("generated CLI compile tests run in the full generated-test CI lane")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is required to prove the share roundtrip")
	}

	cases := []struct {
		name   string
		tables []string
		full   bool
	}{
		{name: "without_sync_state", tables: []string{"users"}, full: true},
		{name: "with_sync_state", tables: []string{"users", "sync_state"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			apiSpec := shareRoundtripSpec(t, tc.tables)
			outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
			gen := New(apiSpec, outputDir)
			require.NoError(t, gen.Generate())

			out, err := runGoCommandOutput(t, outputDir, "test", "-count=1", "-v", "./internal/share/...")
			require.NoError(t, err, out)
			require.Contains(t, out, "--- PASS: TestShare_ExportThenImportRoundtrip")
			require.NotContains(t, out, "--- SKIP: TestShare_ExportThenImportRoundtrip")
			if tc.full {
				runGoCommand(t, outputDir, "test", "-count=1", "./...")
			}
		})
	}
}

func shareRoundtripSpec(t *testing.T, tables []string) *spec.APISpec {
	t.Helper()
	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "stytch.yaml"))
	require.NoError(t, err)
	apiSpec.Cache = spec.CacheConfig{Enabled: true, StaleAfter: "6h"}
	apiSpec.Share = spec.ShareConfig{
		Enabled:        true,
		SnapshotTables: tables,
	}
	return apiSpec
}

func TestShareAllowlistOmittingSyncStateValidates(t *testing.T) {
	t.Parallel()
	apiSpec := shareRoundtripSpec(t, []string{"users"})
	require.NotContains(t, apiSpec.Share.SnapshotTables, "sync_state")
	require.NoError(t, apiSpec.Validate())

	apiSpec.Share.SnapshotTables = nil
	err := apiSpec.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-empty share.snapshot_tables")
}
