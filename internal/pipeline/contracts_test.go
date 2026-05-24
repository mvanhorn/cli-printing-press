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
		{path: filepath.Join("..", "..", "skills", "printing-press-amend", "SKILL.md"), expectsManuscripts: true},
	}

	for _, tt := range tests {
		t.Run(filepath.Base(filepath.Dir(tt.path)), func(t *testing.T) {
			full := readContractFile(t, tt.path)
			block := extractContractBlock(t, full)

			// Binary on PATH check
			assert.Contains(t, block, `command -v cli-printing-press`)
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
			// but must not hardcode go build or use ./cli-printing-press as default
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

func TestPrintingPressSkillPreflightChecksGoToolchain(t *testing.T) {
	skillPath := filepath.Join("..", "..", "skills", "printing-press", "SKILL.md")
	full := readContractFile(t, skillPath)
	block := extractContractBlock(t, full)

	// The Go-toolchain presence check fires after the binary detection block
	// exits cleanly (binary found or PATH-augmented). It catches binary-present
	// + Go-absent and fails fast instead of crashing 5+ minutes later in the
	// post-generation `go mod tidy` quality gate.
	assert.Contains(t, block, `if ! command -v go >/dev/null 2>&1; then`)
	assert.Contains(t, block, `[setup-error] Go toolchain not found.`)
	assert.Contains(t, block, `https://go.dev/dl/`)
}

func TestAgentBrowserInstallRequiresPostInstallSetup(t *testing.T) {
	setup := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "references", "setup-checks.md"))
	capture := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "references", "browser-sniff-capture.md"))

	tests := []struct {
		name    string
		content string
	}{
		{name: "setup-checks", content: setup},
		{name: "browser-sniff-capture", content: capture},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, tt.content, "! agent-browser install")
			assert.Contains(t, tt.content, "The leading `!` is intentional")
			assert.Contains(t, tt.content, "Do not treat `command -v agent-browser` alone as a complete install")
		})
	}

	assert.Contains(t, setup, "Only do this post-install step when this section just installed `agent-browser`; if `PRESS_AGENT_BROWSER_MISSING=false`, skip redundant setup for the already-present binary.")
	assert.Contains(t, setup, "If the user declines the manual step, it fails, or completion is unclear, do not run it through the agent shell")
	assert.Contains(t, setup, "If `PRESS_AGENT_BROWSER_MISSING=false`, do not require post-install confirmation for the already-installed binary.")
	assert.Contains(t, setup, "if a later browser-sniff step reports missing browser binaries, surface `! agent-browser install` then")

	assert.Contains(t, capture, "If the user declines the manual step or completion is unclear, do not run it yourself; fall back to manual HAR.")
	assert.Contains(t, capture, "do not let a second detection pass select the half-installed binary")
	assert.Contains(t, capture, "If a pre-existing agent-browser later reports missing browser binaries, surface `! agent-browser install`")
}

func TestPrintingPressSkillUsesRunstateForBuilds(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "SKILL.md"))

	// Phase 2-5 should use $CLI_WORK_DIR, not $PRESS_LIBRARY/<api>-pp-cli for --output.
	assert.Contains(t, skill, `CLI_WORK_DIR="$API_RUN_DIR/working/<api>-pp-cli"`)
	assert.Contains(t, skill, `--output "$CLI_WORK_DIR"`)
	assert.NotContains(t, skill, `--output "$PRESS_LIBRARY/<api>-pp-cli"`)

	// Lock acquire should appear before generation.
	assert.Contains(t, skill, `cli-printing-press lock acquire --cli <api>-pp-cli --scope "$PRESS_SCOPE"`)

	// Lock promote should appear in Phase 5.5.
	assert.Contains(t, skill, `cli-printing-press lock promote --cli <api>-pp-cli --dir "$CLI_WORK_DIR"`)

	// Phase 6 should still reference $PRESS_LIBRARY (reads from promoted location, slug-keyed).
	assert.Contains(t, skill, `$PRESS_LIBRARY/<api>`)
}

func TestPrintingPressSkillSetsNonCatalogCategoryBeforeGenerate(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "SKILL.md"))
	block := substringBetween(t, skill, "### Pre-Generation Category Enrichment", "### Pre-Generation Auth Enrichment")
	generateBlocks := substringBetween(t, skill, "OpenAPI / internal YAML:", "GraphQL-only APIs:")

	assert.Contains(t, block, "non-catalog CLI")
	assert.Contains(t, block, "set the spec's top-level `category` before")
	assert.Contains(t, block, "`docs/CATALOG.md`")
	assert.Contains(t, block, "before the final `generate` invocation")
	assert.Contains(t, block, "`--category <catalog-category>`")
	assert.Contains(t, block, "Catalog-mode runs skip this step")
	assert.Contains(t, block, "verify-skill canonical-sections")
	assert.GreaterOrEqual(t, strings.Count(generateBlocks, "--category <catalog-category>"), 7)
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

func TestPublishSkillSkipsCliSkillsMirrorRegen(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press-publish", "SKILL.md"))

	assert.Contains(t, skill, "Do not\nedit `registry.json`, README catalog cells, or `cli-skills/pp-<api-slug>/SKILL.md`")
	// Post mvanhorn/printing-press-library#659, the library's verify
	// workflow replaced the Guard + auto-fix + fork-only drift trio
	// with a single `Fail on changes to generated artifacts` check.
	// The publish skill must reference the current gate name so an
	// agent reading it knows what failure to expect, and must still
	// tell the agent not to regenerate or commit either generated
	// file (cli-skills/pp-*/SKILL.md or registry.json) — the library
	// no longer has an in-PR auto-fix path for either.
	assert.Contains(t, skill, "Fail on changes to generated artifacts")
	assert.Contains(t, skill, "Do NOT regenerate or commit `cli-skills/pp-<api-slug>/SKILL.md` or")
	assert.Contains(t, skill, "git add library/\ngit commit")
	assert.NotContains(t, skill, "git add library/ cli-skills/")
	assert.NotContains(t, skill, "git add library/ cli-skills/ registry.json")
	assert.NotContains(t, skill, "REGISTRY_HAS_ENTRY")
	assert.NotContains(t, skill, "seed one registry")
	assert.NotContains(t, skill, "go run ./tools/generate-skills/main.go")

	copyIntoLibrary := strings.Index(skill, `cp -r "$STAGING_DIR/library/<category>/<api-slug>"`)
	require.NotEqual(t, -1, copyIntoLibrary)
}

func TestPublishSkillDocumentsPatchesIndexContract(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press-publish", "SKILL.md"))

	step65 := strings.Index(skill, "## Step 6.5: Record Customizations")
	step7 := strings.Index(skill, "## Step 7: Collision Detection & Resolution")
	require.NotEqual(t, -1, step65)
	require.NotEqual(t, -1, step7)
	assert.Less(t, step65, step7)

	block := skill[step65:step7]
	assert.Contains(t, block, ".printing-press-patches.json")
	assert.Contains(t, block, "if ! jq -e")
	assert.Contains(t, block, `(.schema_version | type == "number")`)
	assert.Contains(t, block, `(.patches | type == "array")`)
	assert.Contains(t, block, "Reprint with a current cli-printing-press binary before publishing")
	assert.Contains(t, block, "malformed .printing-press-patches.json")
	assert.Contains(t, block, "rather than synthesizing the")
	assert.Contains(t, block, "deterministic provenance fields by hand")
	assert.Contains(t, block, "one concise entry per customization")
	assert.Contains(t, block, "`patches[]`")
	assert.Contains(t, block, "README/SKILL.md-only polish does not need a patch")
	assert.Contains(t, block, "manifest entry")
	assert.Contains(t, block, "Inline `// PATCH(...)` source comments are optional navigation aids")
	assert.Contains(t, block, "does not require a marker/comment pairing")
}

func TestAmendSkillRequiresUpstreamBreadcrumbsForTemporaryPatches(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press-amend", "SKILL.md"))
	patchContract := substringBetween(t, skill, "### Step 3 — Execute the plan", "### Step 4 — Validate")

	assert.Contains(t, patchContract, `"id": "<api-slug>-refresh-token-expiry"`)
	assert.Contains(t, patchContract, `"reason": "The generated CLI hid an expired refresh token`)
	assert.Contains(t, patchContract, `"validated_outcome": "publish validate passed`)
	assert.Contains(t, skill, `"deferred_to_upstream": [`)
	assert.Contains(t, skill, `"upstream_issue": "https://github.com/mvanhorn/cli-printing-press/issues/<n>"`)
	assert.Contains(t, skill, "Do not leave a machine-level or API-publication dependency only in the PR body")
	assert.Contains(t, skill, "Inline `// PATCH(...)` source comments are optional navigation aids")
	assert.Contains(t, skill, "the public library verifier no longer enforces a marker/comment pairing")
	assert.NotContains(t, skill, "source comments AND `.printing-press-patches.json` entries")
	assert.NotContains(t, skill, "workflow rejects PRs where one is present without the other")
}

func TestGeneratedAgentsTemplateDocumentsUpstreamPatchHandoff(t *testing.T) {
	template := readContractFile(t, filepath.Join("..", "generator", "templates", "agents.md.tmpl"))
	minimumShape := substringBetween(t, template, "Minimum shape:", "Use `deferred_to_upstream`")

	assert.Contains(t, template, `"deferred_to_upstream": [`)
	assert.Contains(t, template, `"upstream_issue": "https://github.com/mvanhorn/cli-printing-press/issues/<n>"`)
	assert.Contains(t, template, "Use `deferred_to_upstream` when a local patch is a temporary bridge")
	assert.NotContains(t, minimumShape, "deferred_to_upstream")
	assert.NotContains(t, minimumShape, "upstream_issue")
}

func TestPolishSkillHardGatesPublishValidate(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press-polish", "SKILL.md"))

	assert.Contains(t, skill, `cli-printing-press publish validate --dir "$CLI_DIR" --json`)
	assert.Contains(t, skill, "Publish validation failures")
	assert.Contains(t, skill, "The publish-validate leg is a hard ship-gate")
	assert.Contains(t, skill, "phase5 acceptance")
	assert.Contains(t, skill, "ship cannot fire while publish validate fails")
}

func TestPublishSkillPRBodyIncludesStableNovelCommands(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press-publish", "SKILL.md"))

	snapshotState := strings.Index(skill, "PREEXISTING_MERGED_PATHS=$(ls")
	packageCopy := strings.Index(skill, `cp -r "$STAGING_DIR/library/<category>/<api-slug>"`)
	require.NotEqual(t, -1, snapshotState)
	require.NotEqual(t, -1, packageCopy)
	assert.Less(t, snapshotState, packageCopy)

	assert.Contains(t, skill, "The manifest's `novel_features` array from the packaged CLI after Step 6")
	assert.Contains(t, skill, "Do not derive\nthis section from README prose, SKILL prose, root help, or memory of the run")
	assert.Contains(t, skill, "Step 6 has already copied the\nnew package into that path")
	assert.Contains(t, skill, "PREEXISTING_MERGED_COLLISION=true")
	assert.Contains(t, skill, "### Publication Path")
	assert.Contains(t, skill, "### Novel Commands")
	assert.Contains(t, skill, "| Command | Name | Description |")
	assert.Contains(t, skill, "`New print`")
	assert.Contains(t, skill, "`Update existing PR #<N>`")
	assert.Contains(t, skill, "`Reprint/replace`")
	assert.Contains(t, skill, "`Alongside print`")
	assert.Contains(t, skill, "--body-file \"$PR_BODY_FILE\"")
	assert.NotContains(t, skill, "--body \"<constructed PR body>\"")
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
	assert.Contains(t, root, "Recreate the base output directory while preserving hand-edits to generated files via AST-based merge")
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

func substringBetween(t *testing.T, content, start, end string) string {
	t.Helper()

	startIdx := strings.Index(content, start)
	require.NotEqual(t, -1, startIdx, "missing start marker %q", start)
	startIdx += len(start)

	endIdx := strings.Index(content[startIdx:], end)
	require.NotEqual(t, -1, endIdx, "missing end marker %q", end)

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
