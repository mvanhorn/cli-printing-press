package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExportTemplate_PropagatesPersistenceErrors(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("templates", "export.go.tmpl"))
	require.NoError(t, err)
	content := string(src)

	require.NotContains(t, content, "defer writer.Flush()",
		"export must check flush errors explicitly, not defer-discard them")
	require.Contains(t, content, "finishExport")
	require.NotContains(t, content, "defer outFile.Close()",
		"export must check close errors in finishExport before success, not defer-discard them")
	require.Contains(t, content, `fmt.Errorf("closing export file: %w", err)`)
	require.Contains(t, content, "if err != nil && outFile != nil")
	require.Contains(t, content, `fmt.Errorf("flushing export: %w", err)`)
	require.Contains(t, content, `fmt.Errorf("writing export: %w", err)`)
}

func TestSyncTemplates_DoNotDiscardSaveSyncStateErrors(t *testing.T) {
	t.Parallel()

	for _, tmpl := range []string{"sync.go.tmpl", "graphql_sync.go.tmpl"} {
		t.Run(tmpl, func(t *testing.T) {
			t.Parallel()
			src, err := os.ReadFile(filepath.Join("templates", tmpl))
			require.NoError(t, err)
			content := string(src)
			require.NotContains(t, content, "_ = db.SaveSyncState",
				"%s must propagate SaveSyncState errors", tmpl)
			require.True(t, strings.Contains(content, "saving sync state for"),
				"%s should wrap final SaveSyncState failures", tmpl)
		})
	}
}

func TestSyncTemplate_TreatsPersistenceErrorsAsCritical(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("templates", "sync.go.tmpl"))
	require.NoError(t, err)
	content := string(src)

	require.Contains(t, content, "isSyncStatePersistenceError")
	require.Contains(t, content, "isSyncStatePersistenceError(res.Err) || criticalResources[res.Resource]")
}

func TestWorkflowArchiveTemplate_FailsOnPersistenceErrors(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("templates", "channel_workflow.go.tmpl"))
	require.NoError(t, err)
	content := string(src)

	require.Contains(t, content, "isSyncStatePersistenceError(res.Err)")
	require.Contains(t, content, `return fmt.Errorf("archiving %s: %w", resource, res.Err)`)
}

func TestGeneratedExport_FinishExportBeforeSuccessMessage(t *testing.T) {
	t.Parallel()

	outputDir := generatePetstore(t)
	exportGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "export.go"))
	require.NoError(t, err)
	content := string(exportGo)
	require.Contains(t, content, "finishExport")
	require.Contains(t, content, `fmt.Errorf("closing export file: %w", err)`)
	require.NotContains(t, content, "defer writer.Flush()")
	successIdx := strings.Index(content, "Exported %d records")
	finishIdx := strings.LastIndex(content, "finishExport()")
	require.Greater(t, successIdx, finishIdx, "success message must follow finishExport in generated export")
}
