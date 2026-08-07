package generator

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenerateStorePlaybooksRunsWithRaceDetector compiles and runs the
// store-layer playbook tests under -race against the emitted CLI. This
// is the U4 acceptance gate: the concurrent-append test must pass with
// the atomic single-transaction AppendPlaybookNotes implementation.
func TestGenerateStorePlaybooksRunsWithRaceDetector(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("emitted store playbooks race test skipped in -short mode")
	}
	apiSpec := minimalSpec("playbooks-race")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "playbooks-race-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())
	runGoCommand(t, outputDir, "test", "./internal/store", "-run", "TestAppendPlaybookNotes_ConcurrentNoLoss|TestUpsertPlaybook|TestGetPlaybookByFamily|TestListPlaybooks|TestPlaybooksTable", "-race", "-count=1")
}
