package generator

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWhichIndexSeedsPromotedCommands(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("which-promoted")
	apiSpec.Resources["awards"] = spec.Resource{
		Description: "Award availability",
		Endpoints: map[string]spec.Endpoint{
			"search": {
				Method:      "GET",
				Path:        "/awards",
				Description: "Search award availability for a cabin and date.",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "which-promoted-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	whichSrc := readGeneratedFile(t, outputDir, "internal", "cli", "which.go")
	assert.Contains(t, whichSrc, `Command: "awards"`)
	assert.Contains(t, whichSrc, `Search award availability for a cabin and date.`)
	assert.Contains(t, whichSrc, `pp:which-promoted`)
	assert.Contains(t, whichSrc, `Command: "items"`)
	assert.NotContains(t, whichSrc, `Command: "awards search"`)
}

func TestWhichIndexNovelsStayAheadOfPromoted(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("which-novel-first")
	gen := New(apiSpec, filepath.Join(t.TempDir(), "which-novel-first-pp-cli"))
	gen.NovelFeatures = []NovelFeature{{
		Command:      "digest",
		Description:  "Summarize overnight changes",
		Group:        "Analysis",
		WhyItMatters: "Hero digest path",
	}}
	require.NoError(t, gen.Generate())

	entries := gen.whichIndexEntries()
	require.GreaterOrEqual(t, len(entries), 2)
	assert.Equal(t, "digest", entries[0].Command)
	assert.False(t, entries[0].Promoted)
	assert.Equal(t, "items", entries[1].Command)
	assert.True(t, entries[1].Promoted)
}

func TestWhichIndexNovelWinsCommandDedupe(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("which-dedupe")
	gen := New(apiSpec, filepath.Join(t.TempDir(), "which-dedupe-pp-cli"))
	gen.NovelFeatures = []NovelFeature{{
		Command:      "items",
		Description:  "Hero items search",
		WhyItMatters: "Prefer the novel leaf",
	}}
	require.NoError(t, gen.Generate())

	entries := gen.whichIndexEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "items", entries[0].Command)
	assert.Equal(t, "Hero items search", entries[0].Description)
	assert.False(t, entries[0].Promoted)
}

func TestWhichQueryRanksPromotedByDescription(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("which-rank")
	apiSpec.Auth.Type = "none"
	apiSpec.Auth.EnvVars = nil
	apiSpec.Resources = map[string]spec.Resource{
		"awards": {
			Description: "Award availability",
			Endpoints: map[string]spec.Endpoint{
				"search": {
					Method:      "GET",
					Path:        "/awards",
					Description: "Search award availability for a cabin and date.",
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "which-rank-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	runGoCommand(t, outputDir, "mod", "tidy")
	binaryPath := filepath.Join(outputDir, "which-rank-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/which-rank-pp-cli")

	cmd := exec.Command(binaryPath, "which", "Search award availability for a cabin and date.", "--json", "--limit", "1")
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
	out, err := cmd.Output()
	require.NoError(t, err, string(out))

	var payload struct {
		Matches []struct {
			Entry struct {
				Command string `json:"command"`
			} `json:"entry"`
		} `json:"matches"`
	}
	require.NoError(t, json.Unmarshal(out, &payload), string(out))
	require.NotEmpty(t, payload.Matches)
	assert.Equal(t, "awards", payload.Matches[0].Entry.Command)
}
