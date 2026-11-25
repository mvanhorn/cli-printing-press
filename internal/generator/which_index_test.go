package generator

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

type whichRunPayload struct {
	Matches []struct {
		Entry struct {
			Command string `json:"command"`
		} `json:"entry"`
	} `json:"matches"`
}

func whichDecoyEnv(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	drop := map[string]bool{
		"HOME": true, "USERPROFILE": true,
		"XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true,
		"XDG_STATE_HOME": true, "XDG_CACHE_HOME": true,
		"PRINTING_PRESS_VERIFY": true, "PRINTING_PRESS_DOGFOOD": true,
		"PRINTING_PRESS_VERIFY_LIVE_HTTP": true,
	}
	filtered := make([]string, 0, len(os.Environ())+6)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if drop[name] {
			continue
		}
		filtered = append(filtered, kv)
	}
	return append(filtered,
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
	)
}

func runWhich(t *testing.T, binary string, args ...string) ([]byte, error) {
	t.Helper()
	argv := make([]string, 0, len(args)+1)
	argv = append(argv, "which")
	argv = append(argv, args...)
	cmd := exec.Command(binary, argv...)
	cmd.Env = whichDecoyEnv(t)
	return cmd.Output()
}

func runWhichJSON(t *testing.T, binary string, args ...string) whichRunPayload {
	t.Helper()
	out, err := runWhich(t, binary, args...)
	require.NoError(t, err, string(out))
	var payload whichRunPayload
	require.NoError(t, json.Unmarshal(out, &payload), string(out))
	return payload
}

func TestWhichFindsUnpromotedEndpoint(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("which-surface")
	apiSpec.Auth.Type = "none"
	apiSpec.Auth.EnvVars = nil
	apiSpec.Resources["widgets"] = spec.Resource{
		Description: "Widget operations that are not promoted",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/widgets",
				Description: "List widgets",
			},
			"trend": {
				Method:      "GET",
				Path:        "/widgets/trend",
				Description: "Show the widget trend",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "which-surface-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{{
		Name:         "Widget breakouts",
		Command:      "widget breakouts",
		Description:  "Scan widget breakouts",
		Group:        "widget",
		WhyItMatters: "Find widget breakouts before the move fades",
	}}
	require.NoError(t, gen.Generate())

	whichSrc := readGeneratedFile(t, outputDir, "internal", "cli", "which.go")
	assert.Contains(t, whichSrc, `Command: "widget breakouts"`)
	assert.NotContains(t, whichSrc, `Command: "widgets trend"`)
	assert.Contains(t, whichSrc, "whichCommandIndex")

	runGoCommand(t, outputDir, "test", "-count=1", "-timeout", "300s", "-run", "TestWhich|TestRankWhich", "./internal/cli/")

	binaryPath := filepath.Join(outputDir, "which-surface-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/which-surface-pp-cli")

	trend := runWhichJSON(t, binaryPath, "widgets", "trend", "--json", "--limit", "1")
	require.NotEmpty(t, trend.Matches)
	assert.Equal(t, "widgets trend", trend.Matches[0].Entry.Command)

	byShort := runWhichJSON(t, binaryPath, "Show the widget trend", "--json", "--limit", "1")
	require.NotEmpty(t, byShort.Matches)
	assert.Equal(t, "widgets trend", byShort.Matches[0].Entry.Command)

	curated := runWhichJSON(t, binaryPath, "widget", "breakouts", "--json", "--limit", "1")
	require.NotEmpty(t, curated.Matches)
	assert.Equal(t, "widget breakouts", curated.Matches[0].Entry.Command)

	miss, err := runWhich(t, binaryPath, "teleport", "a", "rhinoceros", "--json")
	require.Error(t, err)
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 2, exitErr.ExitCode())
	var payload whichRunPayload
	require.NoError(t, json.Unmarshal(miss, &payload), string(miss))
	assert.Empty(t, payload.Matches)

	for _, query := range []string{"help", "completion"} {
		out, qerr := runWhich(t, binaryPath, query, "--json", "--limit", "20")
		if qerr != nil {
			var exitErr *exec.ExitError
			require.ErrorAs(t, qerr, &exitErr)
			assert.Equal(t, 2, exitErr.ExitCode(), query)
			continue
		}
		var got whichRunPayload
		require.NoError(t, json.Unmarshal(out, &got), string(out))
		for _, match := range got.Matches {
			assert.NotEqual(t, query, match.Entry.Command)
		}
	}
}
