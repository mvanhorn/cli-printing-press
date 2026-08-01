package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSyncShortMentionsSearchOnlyWhenGenerated pins the sync command's Short
// line to whether a search command actually exists. Long is already gated on
// .VisionSet.Search; Short was not, so a sync-only CLI advertised a command
// its own --help does not list.
func TestSyncShortMentionsSearchOnlyWhenGenerated(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		search    bool
		wantShort string
	}{
		{name: "with search", search: true, wantShort: "for offline search and analysis"},
		{name: "sync only", search: false, wantShort: "for offline analysis"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			outputDir := filepath.Join(t.TempDir(), "syncshort-pp-cli")
			gen := New(minimalSpec("syncshort"), outputDir)
			gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: tc.search, MCP: true}
			require.NoError(t, gen.Generate())

			sync, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
			require.NoError(t, err)
			got := string(sync)

			assert.Contains(t, got, tc.wantShort)
			// Long carries the same guard; the two must stay in step.
			if tc.search {
				assert.Contains(t, got, "use the 'search' command")
			} else {
				assert.NotContains(t, got, "for offline search and analysis")
				assert.NotContains(t, got, "use the 'search' command")
			}
		})
	}
}
