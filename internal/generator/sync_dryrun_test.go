package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGeneratedSyncDryRunDoesNotMutateSyncState guards the fix for #2935: a
// `sync --dry-run` must not write the sync_state table. The flat (main-loop)
// path already short-circuited on the {"dry_run":true} sentinel, but the
// dependent-resource loop called db.SaveSyncState unconditionally at its tail,
// and the --full / --latest-only cursor clears ran regardless of --dry-run.
// This asserts the generated sync.go carries all three guards.
func TestGeneratedSyncDryRunDoesNotMutateSyncState(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "syncdryrun",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"parents": {
				Description: "Parents",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/parents", Description: "List parents", Response: spec.ResponseDef{Type: "array"}},
				},
			},
			"children": {
				Description: "Children",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/parents/{parentId}/children", Description: "List children", Response: spec.ResponseDef{Type: "array"}},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	gen.profile = &profiler.APIProfile{
		SyncableResources: []profiler.SyncableResource{
			{Name: "parents", Path: "/parents", Method: "GET"},
		},
		DependentSyncResources: []profiler.DependentResource{
			{Name: "children", ParentResource: "parents", ParentIDParam: "parentId", Path: "/parents/{parentId}/children", Method: "GET"},
		},
	}
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")

	// Both the flat loop and the dependent loop must short-circuit on the
	// dry-run sentinel before any SaveSyncState. The dependent guard is the
	// #2935 fix; the flat guard pre-existed. Two occurrences == both guarded.
	if n := strings.Count(syncSrc, "if isDryRunResponse(data) {"); n < 2 {
		t.Fatalf("expected the dry-run sentinel guard in both the flat and dependent sync loops (>=2 occurrences), found %d", n)
	}
	// The --full and --latest-only cursor clears must skip under --dry-run.
	require.Contains(t, syncSrc, "if full && !c.DryRun {",
		"--full cursor clear must be guarded by !c.DryRun")
	latestOnlyIdx := strings.Index(syncSrc, "if latestOnly {")
	require.NotEqual(t, -1, latestOnlyIdx, "expected latestOnly block in generated sync")
	latestOnlyBlock := syncSrc[latestOnlyIdx:]
	require.Contains(t, latestOnlyBlock, "Skip under --dry-run:",
		"--latest-only cursor clear must document the dry-run guard")
	require.Contains(t, latestOnlyBlock, "if !c.DryRun {",
		"--latest-only cursor clear must be guarded by !c.DryRun")
}
