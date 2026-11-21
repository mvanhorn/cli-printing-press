package cli

import (
	"bytes"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline/regenmerge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegenMergeRegistersAllowDeclConflicts(t *testing.T) {
	t.Parallel()

	cmd := newRegenMergeCmd()
	flag := cmd.Flags().Lookup("allow-decl-conflicts")
	require.NotNil(t, flag)
	assert.Equal(t, "false", flag.DefValue)
}

func TestPrintHumanRegenReportShowsBothSides(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	report := &regenmerge.MergeReport{
		CLIDir:        "/cli",
		FreshDir:      "/fresh",
		BothSidesNote: "no base tree: a shared declaration changed in the published tree cannot be distinguished from a template change",
		Files: []regenmerge.FileClassification{{
			Path:    "internal/cli/root.go",
			Verdict: regenmerge.VerdictTemplatedValueDrift,
			BothSides: &regenmerge.BothSides{
				Merged: []string{"newRootCmd"},
				Conflicts: []regenmerge.DeclConflict{{
					Name:      "preRun",
					Base:      "base-line",
					Fresh:     "fresh-line",
					Published: "published-line",
				}},
			},
		}},
	}
	require.NoError(t, printHumanRegenReport(&buf, report, false))
	got := buf.String()
	assert.Contains(t, got, report.BothSidesNote)
	assert.Contains(t, got, "both_sides merged: newRootCmd")
	assert.Contains(t, got, "both_sides conflict: preRun")
	assert.Contains(t, got, "base-line")
	assert.Contains(t, got, "fresh-line")
	assert.Contains(t, got, "published-line")
}
