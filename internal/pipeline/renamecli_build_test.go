package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenameCLIGeneratedTreeBuildsAndKeepsResearchName(t *testing.T) {
	setPressTestEnv(t)

	apiSpec := &spec.APISpec{
		Name:      "subject",
		Version:   "0.1.0",
		BaseURL:   "https://api.example.com",
		Owner:     "test-owner",
		OwnerName: "Test Author",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"SUBJECT_TOKEN"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/subject-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}

	root := t.TempDir()
	cliDir := filepath.Join(root, "subject-pp-cli")
	require.NoError(t, generator.New(apiSpec, cliDir).Generate())
	require.NoError(t, writeResearchJSON(&ResearchResult{
		APIName: "subject",
		NovelFeatures: []NovelFeature{{
			Name:        "List items",
			Command:     "items list",
			Description: "List items from the API.",
		}},
		Narrative: &ReadmeNarrative{
			AuthNarrative: "Export the API token.",
		},
	}, cliDir))

	_, err := RenameCLI(cliDir, "subject-pp-cli", "overpass-pp-cli", "subject")
	require.NoError(t, err)

	newDir := filepath.Join(root, naming.LibraryDirName("overpass-pp-cli"))
	gomod, err := os.ReadFile(filepath.Join(newDir, "go.mod"))
	require.NoError(t, err)
	assert.Contains(t, string(gomod), "module overpass-pp-cli")
	assert.NotContains(t, string(gomod), "subject-pp-cli")

	runRenameGoCommand(t, newDir, "mod", "tidy")
	runRenameGoCommand(t, newDir, "build", "./...")

	leftovers := collectRenameLeftovers(t, newDir, []string{"subject-pp-cli", "module subject-pp-cli"})
	assert.Empty(t, leftovers, "renamed tree still mentions the old module path")

	_, err = RunDogfood(newDir, "", WithResearchDir(newDir))
	require.NoError(t, err)

	research, err := LoadResearch(newDir)
	require.NoError(t, err)
	assert.Equal(t, "overpass", research.APIName)

	leftovers = collectRenameLeftovers(t, newDir, []string{"subject-pp-cli"})
	assert.Empty(t, leftovers, "dogfood --research-dir resurrected the old CLI name")
}

func runRenameGoCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(dir, ".cache", "go-build"))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
}

func collectRenameLeftovers(t *testing.T, dir string, needles []string) []string {
	t.Helper()
	var leftovers []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == ".manuscripts" || d.Name() == ".cache" {
				return filepath.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			rel = path
		}
		for _, needle := range needles {
			if strings.Contains(text, needle) {
				leftovers = append(leftovers, rel+": "+needle)
			}
		}
		return nil
	})
	require.NoError(t, err)
	return leftovers
}
