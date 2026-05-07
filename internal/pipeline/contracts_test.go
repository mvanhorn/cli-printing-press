package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedOutputContractSupportsClaimedDirs(t *testing.T) {
	setPressTestEnv(t)

	apiSpec := loadContractPetstoreSpec(t)
	baseDir := DefaultOutputDir(apiSpec.Name)

	firstDir, err := ClaimOutputDir(baseDir)
	require.NoError(t, err)
	secondDir, err := ClaimOutputDir(baseDir)
	require.NoError(t, err)

	assert.Equal(t, baseDir, firstDir)
	assert.Equal(t, baseDir+"-2", secondDir)

	for _, dir := range []string{firstDir, secondDir} {
		gen := generator.New(apiSpec, dir)
		require.NoError(t, gen.Generate())
		runGoContractCommand(t, dir, "mod", "tidy")
		assert.DirExists(t, filepath.Join(dir, "cmd", naming.CLI(apiSpec.Name)))
	}

	report, err := RunVerify(VerifyConfig{Dir: secondDir})
	require.NoError(t, err)
	assert.NotEqual(t, "FAIL", report.Verdict)
	assert.Greater(t, report.Total, 0)
	assert.FileExists(t, report.Binary)
}

func TestSkillSetupBlocksMatchWorkspaceContract(t *testing.T) {
	tests := []struct {
		path               string
		expectsManuscripts bool
	}{
		{path: filepath.Join("..", "..", "skills", "printing-press", "SKILL.md"), expectsManuscripts: true},
		{path: filepath.Join("..", "..", "skills", "printing-press-score", "SKILL.md"), expectsManuscripts: true},
		{path: filepath.Join("..", "..", "skills", "printing-press-catalog", "SKILL.md"), expectsManuscripts: false},
		{path: filepath.Join("..", "..", "skills", "printing-press-publish", "SKILL.md"), expectsManuscripts: true},
	}

	for _, tt := range tests {
		t.Run(filepath.Base(filepath.Dir(tt.path)), func(t *testing.T) {
			full := readContractFile(t, tt.path)
			block := extractContractBlock(t, full)

			// Binary on PATH check
			assert.Contains(t, block, `command -v printing-press`)
			// Version comment for frontmatter parity
			assert.Contains(t, block, `# min-binary-version:`)
			// Symlink-safe canonicalization
			assert.Contains(t, block, `pwd -P`)

			// Core workspace variables
			assert.Contains(t, block, `PRESS_HOME="$HOME/printing-press"`)
			assert.Contains(t, block, `PRESS_SCOPE=`)
			assert.Contains(t, block, `PRESS_RUNSTATE="$PRESS_HOME/.runstate/$PRESS_SCOPE"`)
			assert.Contains(t, block, `PRESS_LIBRARY="$PRESS_HOME/library"`)

			// May reference local build for repo-internal development,
			// but must not hardcode go build or use ./printing-press as default
			assert.NotContains(t, block, `go build`)
			// Must NOT contain REPO_ROOT or cd to repo
			assert.NotContains(t, block, `REPO_ROOT`)
			assert.NotContains(t, block, `cd "$REPO_ROOT"`)

			assert.NotContains(t, full, "~/cli-printing-press")

			if tt.expectsManuscripts {
				assert.Contains(t, block, `PRESS_MANUSCRIPTS="$PRESS_HOME/manuscripts"`)
			}
		})
	}
}

func TestPrintingPressSkillUsesRunRootStateFile(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "SKILL.md"))

	assert.Contains(t, skill, `STATE_FILE="$API_RUN_DIR/state.json"`)
	assert.NotContains(t, skill, `STATE_FILE="$PIPELINE_DIR/state.json"`)
	assert.Contains(t, skill, `"working_dir": "$CLI_WORK_DIR"`)
}

func TestPrintingPressSkillUsesRunstateForBuilds(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "SKILL.md"))

	// Phase 2-5 should use $CLI_WORK_DIR, not $PRESS_LIBRARY/<api>-pp-cli for --output.
	assert.Contains(t, skill, `CLI_WORK_DIR="$API_RUN_DIR/working/<api>-pp-cli"`)
	assert.Contains(t, skill, `--output "$CLI_WORK_DIR"`)
	assert.NotContains(t, skill, `--output "$PRESS_LIBRARY/<api>-pp-cli"`)

	// Lock acquire should appear before generation.
	assert.Contains(t, skill, `printing-press lock acquire --cli <api>-pp-cli --scope "$PRESS_SCOPE"`)

	// Lock promote should appear in Phase 5.5.
	assert.Contains(t, skill, `printing-press lock promote --cli <api>-pp-cli --dir "$CLI_WORK_DIR"`)

	// Phase 6 should still reference $PRESS_LIBRARY (reads from promoted location, slug-keyed).
	assert.Contains(t, skill, `$PRESS_LIBRARY/<api>`)
}

func TestPrintingPressSkillExamplesUseCurrentCLINaming(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "SKILL.md"))

	assert.Contains(t, skill, "/printing-press emboss notion")
	assert.NotContains(t, skill, "/printing-press emboss notion-cli")
	assert.Contains(t, skill, "discord-pp-cli/internal/store/store.go")
	assert.NotContains(t, skill, "discord-cli/internal/store/store.go")
	assert.Contains(t, skill, "linear-pp-cli stale --days 30 --team ENG")
	assert.NotContains(t, skill, "linear-cli stale --days 30 --team ENG")
	assert.Contains(t, skill, "github.com/mvanhorn/discord-pp-cli")
	assert.NotContains(t, skill, "github.com/mvanhorn/discord-cli")
}

func TestPublishSkillTracksCanonicalUpstreamAndOverwriteFlow(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press-publish", "SKILL.md"))

	assert.Contains(t, skill, "git remote add upstream")
	assert.Contains(t, skill, "mvanhorn/printing-press-library")
	assert.Contains(t, skill, "git fetch upstream")
	assert.Contains(t, skill, "git reset --hard upstream/main")
	assert.Contains(t, skill, "git push --force-with-lease")
}

func TestREADMEOutputContract(t *testing.T) {
	readme := readContractFile(t, filepath.Join("..", "..", "README.md"))

	assert.Contains(t, readme, "~/printing-press/.runstate/<scope>/runs/<run-id>/working/<api>-pp-cli")
	assert.Contains(t, readme, "~/printing-press/library/<api>")
	assert.Contains(t, readme, "~/printing-press/manuscripts/<api>/<run-id>/")
	assert.Contains(t, readme, "`research/`, `proofs/`, `discovery/`, and `pipeline/`")
	assert.NotContains(t, readme, "cd ~/cli-printing-press")
}

func TestGenerateHelpMentionsPublishedLibraryDefault(t *testing.T) {
	root := readContractFile(t, filepath.Join("..", "..", "internal", "cli", "root.go"))

	assert.Contains(t, root, "Output directory (default: ~/printing-press/library/<name>)")
	assert.Contains(t, root, "Overwrite the base output directory (e.g. ~/printing-press/library/notion)")
	assert.NotContains(t, root, "~/printing-press/workspaces/<scope>/library")
}

func TestOnboardingReflectsCurrentPipelinePhaseCount(t *testing.T) {
	onboarding := readContractFile(t, filepath.Join("..", "..", "ONBOARDING.md"))

	assert.Contains(t, onboarding, "9-phase pipeline")
	assert.Contains(t, onboarding, "agent-readiness")
	assert.Contains(t, onboarding, "~/printing-press/.runstate/<scope>/runs/<run-id>/")
	assert.Contains(t, onboarding, "~/printing-press/library/<name>/")
	assert.Contains(t, onboarding, "~/printing-press/manuscripts/<api>/<run-id>/")
	assert.NotContains(t, onboarding, "8-phase pipeline")
}

func loadContractPetstoreSpec(t *testing.T) *spec.APISpec {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "petstore.yaml"))
	require.NoError(t, err)

	apiSpec, err := openapi.Parse(data)
	require.NoError(t, err)
	return apiSpec
}

func readContractFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func extractContractBlock(t *testing.T, content string) string {
	t.Helper()

	const start = "<!-- PRESS_SETUP_CONTRACT_START -->"
	const end = "<!-- PRESS_SETUP_CONTRACT_END -->"

	startIdx := strings.Index(content, start)
	require.NotEqual(t, -1, startIdx, "missing contract start marker")
	startIdx += len(start)

	endIdx := strings.Index(content[startIdx:], end)
	require.NotEqual(t, -1, endIdx, "missing contract end marker")

	return content[startIdx : startIdx+endIdx]
}

func runGoContractCommand(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(dir, ".cache", "go-build"))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
}
