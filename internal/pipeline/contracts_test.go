package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
			assert.Contains(t, block, `PRESS_HOME="${PRINTING_PRESS_HOME:-$HOME/printing-press}"`)
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

func TestSkillsEnforceCurrencyFloor(t *testing.T) {
	const floorURL = "https://raw.githubusercontent.com/mvanhorn/cli-printing-press/main/supported-versions.txt"

	// The published floor file exists and declares a parseable semver minimum
	// plus a reason the skills surface to the user.
	floor := readContractFile(t, filepath.Join("..", "..", "supported-versions.txt"))
	assert.Regexp(t, regexp.MustCompile(`(?m)^min_supported=\d+\.\d+\.\d+$`), floor,
		"supported-versions.txt must declare min_supported=<major.minor.patch>")
	assert.Regexp(t, regexp.MustCompile(`(?m)^reason=\S`), floor,
		"supported-versions.txt must declare a non-empty reason")

	// printing-press: the floor fetch is throttled by the version-check TTL, but
	// the installed-vs-floor comparison runs every invocation (outside the
	// _should_check gate) and only fires for a floor that is itself <= latest, so
	// a bad floor above the newest release cannot brick every install.
	pp := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "SKILL.md"))
	ppBlock := extractContractBlock(t, pp)
	assert.Contains(t, ppBlock, floorURL)
	assert.Contains(t, ppBlock, "[upgrade-required] printing-press")
	assert.Contains(t, ppBlock, "PRESS_REQUIRED_MIN=")
	assert.Contains(t, ppBlock, "PRESS_REQUIRED_INSTALLED=")
	assert.Contains(t, ppBlock, "PRESS_REQUIRED_REASON=")
	assert.Contains(t, ppBlock, `[ "$_press_repo" != "true" ] && [ -f "$PRESS_VERCHECK_FILE" ]`)
	assert.Contains(t, ppBlock, `! _semver_lt "$_floor_latest" "$_floor_min"`)

	// setup-checks.md documents the hard gate as upgrade-or-abort, distinct from
	// the soft [upgrade-available] advisory.
	checks := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "references", "setup-checks.md"))
	assert.Contains(t, checks, "[upgrade-required]")
	assert.Contains(t, checks, "PRESS_REQUIRED_MIN")
	assert.Contains(t, checks, "Update required")
	assert.Contains(t, checks, "no skip-and-continue")

	// amend regenerates too, so it carries the same hard floor. Assert the full
	// signal set inside the contract block (parity with printing-press) so the
	// two independent copies cannot drift; the prose hard-gate is checked too.
	amend := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press-amend", "SKILL.md"))
	amendBlock := extractContractBlock(t, amend)
	assert.Contains(t, amendBlock, floorURL)
	assert.Contains(t, amendBlock, "[upgrade-required] printing-press")
	assert.Contains(t, amendBlock, "PRESS_REQUIRED_MIN=")
	assert.Contains(t, amendBlock, "PRESS_REQUIRED_INSTALLED=")
	assert.Contains(t, amendBlock, "PRESS_REQUIRED_REASON=")
	assert.Contains(t, amendBlock, `! _semver_lt "$_floor_latest" "$_floor_min"`)
	assert.Contains(t, amend, "no skip-and-continue")
}

func TestSkillFilesHonorPrintingPressHomeEnv(t *testing.T) {
	skillPaths, err := filepath.Glob(filepath.Join("..", "..", "skills", "*", "SKILL.md"))
	require.NoError(t, err)
	require.NotEmpty(t, skillPaths)

	referencePaths, err := filepath.Glob(filepath.Join("..", "..", "skills", "*", "references", "*"))
	require.NoError(t, err)

	for _, path := range append(skillPaths, referencePaths...) {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			full := readContractFile(t, path)
			assert.NotContains(t, full, `PRESS_HOME="$HOME/printing-press"`)
			assert.NotContains(t, full, `$HOME/printing-press/`)
			assert.NotContains(t, full, `"$HOME/printing-press"`)
			assert.NotContains(t, full, `~/printing-press/library/`)
			assert.NotContains(t, full, `~/printing-press/manuscripts/`)
		})
	}
}

func TestPrintingPressImportScriptsHonorPrintingPressHomeEnv(t *testing.T) {
	pressHome := t.TempDir()
	home := t.TempDir()
	apiSlug := "fixture-api"

	staging := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(staging, "README.md"), []byte("# fixture\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(staging, ".manuscripts", "run-1"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(staging, ".manuscripts", "run-1", "research.json"), []byte("{}\n"), 0o644))

	placeScript := filepath.Join("..", "..", "skills", "printing-press-import", "references", "import-place.sh")
	runContractScript(t, placeScript, []string{
		"PRINTING_PRESS_HOME=" + pressHome,
		"HOME=" + home,
	}, staging, apiSlug)

	assert.FileExists(t, filepath.Join(pressHome, "library", apiSlug, "README.md"))
	assert.FileExists(t, filepath.Join(pressHome, "manuscripts", apiSlug, "run-1", "research.json"))
	assert.NoDirExists(t, filepath.Join(home, "printing-press"))

	defaultHome := t.TempDir()
	defaultStaging := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(defaultStaging, "README.md"), []byte("# default\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(defaultStaging, ".manuscripts", "run-1"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(defaultStaging, ".manuscripts", "run-1", "research.json"), []byte("{}\n"), 0o644))
	runContractScript(t, placeScript, []string{
		"PRINTING_PRESS_HOME=",
		"HOME=" + defaultHome,
	}, defaultStaging, apiSlug)

	assert.FileExists(t, filepath.Join(defaultHome, "printing-press", "library", apiSlug, "README.md"))
	assert.FileExists(t, filepath.Join(defaultHome, "printing-press", "manuscripts", apiSlug, "run-1", "research.json"))

	require.NoError(t, os.WriteFile(filepath.Join(pressHome, "library", apiSlug, "state.json"), []byte("{}\n"), 0o644))
	fakeBin := t.TempDir()
	fakeZip := filepath.Join(fakeBin, "zip")
	require.NoError(t, os.WriteFile(fakeZip, []byte("#!/usr/bin/env bash\nset -euo pipefail\ntouch \"$2\"\n"), 0o755))

	backupScript := filepath.Join("..", "..", "skills", "printing-press-import", "references", "import-backup.sh")
	out := runContractScript(t, backupScript, []string{
		"PRINTING_PRESS_HOME=" + pressHome,
		"HOME=" + home,
		"PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
	}, apiSlug)

	assert.Contains(t, out, "/tmp/printing-press/"+apiSlug+"-")
	assert.NoDirExists(t, filepath.Join(home, "printing-press"))

	defaultBackupHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(defaultBackupHome, "printing-press", "library", apiSlug), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(defaultBackupHome, "printing-press", "library", apiSlug, "state.json"), []byte("{}\n"), 0o644))
	defaultFakeBin := t.TempDir()
	defaultFakeZip := filepath.Join(defaultFakeBin, "zip")
	require.NoError(t, os.WriteFile(defaultFakeZip, []byte("#!/usr/bin/env bash\nset -euo pipefail\ntouch \"$2\"\n"), 0o755))
	defaultOut := runContractScript(t, backupScript, []string{
		"PRINTING_PRESS_HOME=",
		"HOME=" + defaultBackupHome,
		"PATH=" + defaultFakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
	}, apiSlug)

	assert.Contains(t, defaultOut, "/tmp/printing-press/"+apiSlug+"-")
}

func TestPrintingPressSetupChecksSkipSnippetIsSelfContained(t *testing.T) {
	setupChecks := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "references", "setup-checks.md"))
	skipSnippet := substringBetween(t, setupChecks, "If the user picks **Skip**", "Prompt again only when")

	assert.Contains(t, skipSnippet, `PRESS_HOME="${PRINTING_PRESS_HOME:-$HOME/printing-press}"`)
	assert.Contains(t, skipSnippet, `> "$PRESS_HOME/.version-check"`)
	assert.NotContains(t, skipSnippet, `> "$HOME/printing-press/.version-check"`)
}

func TestPrintingPressSkillUsesRunRootStateFile(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "SKILL.md"))

	assert.Contains(t, skill, `STATE_FILE="$API_RUN_DIR/state.json"`)
	assert.NotContains(t, skill, `STATE_FILE="$PIPELINE_DIR/state.json"`)
	assert.Contains(t, skill, `"working_dir": "$CLI_WORK_DIR"`)
}

func TestPrintingPressSkillWarnsOnMultiSpecDirectories(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "SKILL.md"))
	block := substringBetween(t, skill, "#### Directory spec-source guard", "2. Check for prior research")

	assert.Contains(t, block, "If any resolved spec source is a local directory")
	assert.Contains(t, block, "do not silently pick the first")
	assert.Contains(t, block, `find "$SPEC_SOURCE_DIR" -type f`)
	assert.Contains(t, block, "When the filtered candidate list is empty")
	assert.Contains(t, block, "No OpenAPI/Swagger spec found under <directory>")
	assert.Contains(t, block, "Do not continue with the raw directory as the spec source")
	assert.Contains(t, block, "N OpenAPI/Swagger specs found under <directory>")
	assert.Contains(t, block, "`spec_candidates` is the sorted list")
	assert.Contains(t, block, "After the user confirms the selection")
	assert.Contains(t, block, "`selected_spec_paths` set to the list that will be generated")
	assert.Contains(t, block, "stop after printing the warning")
	assert.Contains(t, block, "one independent printed CLI per")
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

func TestPrintingPressSkillPreflightSmokeTestsGoStdlib(t *testing.T) {
	skillPath := filepath.Join("..", "..", "skills", "printing-press", "SKILL.md")
	full := readContractFile(t, skillPath)
	block := extractContractBlock(t, full)

	smokeBlock := substringBetween(t, block, `_go_smoke_root=`, `# Resolve and emit the absolute path`)

	assert.Contains(t, smokeBlock, `$HOME/.printing-press-smoke`)
	assert.Contains(t, smokeBlock, `mktemp -d "$_go_smoke_root/stdlib.XXXXXX"`)
	assert.Contains(t, smokeBlock, `GOFLAGS= GOWORK=off go run .`)
	assert.Contains(t, smokeBlock, `"fmt"`)
	assert.Contains(t, smokeBlock, `"io"`)
	assert.Contains(t, smokeBlock, `"net/http"`)
	assert.Contains(t, smokeBlock, `"encoding/json"`)
	assert.Contains(t, smokeBlock, `"regexp"`)
	assert.Contains(t, smokeBlock, `"context"`)
	assert.Contains(t, smokeBlock, `[setup-error] Go std library is incomplete (truncated or corrupted install).`)
	assert.Contains(t, smokeBlock, `Reinstall Go from https://go.dev/dl/`)
	assert.Contains(t, smokeBlock, `rm -rf "$_go_smoke_dir"`)
	assert.NotContains(t, smokeBlock, `${TMPDIR`)
	assert.NotContains(t, smokeBlock, `/tmp`)
}

func TestPrintingPressSkillDistinguishesBearerFromRawAPIKey(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "SKILL.md"))
	block := substringBetween(t, skill, "### Pre-Generation Auth Enrichment", "**Why enrich before generation")

	assert.Contains(t, block, "choose the security scheme by wire format")
	assert.Contains(t, block, "Authorization: Bearer <token>")
	assert.Contains(t, block, "model it as `http` bearer")
	assert.Contains(t, block, "no scheme prefix")
	assert.Contains(t, block, "model it")
	assert.Contains(t, block, "as `apiKey`")
	assert.Contains(t, block, "Do not")
	assert.Contains(t, block, "switch to `apiKey` just to attach the richer metadata.")
	assert.Contains(t, block, "type: http")
	assert.Contains(t, block, "scheme: bearer")
	assert.Contains(t, block, "bearerFormat: xoxp")
	assert.Contains(t, block, "rawHeaderKey:")
	assert.Contains(t, block, "name: X-API-Key")
}

func TestPrintingPressSkillRunERequiredInputContract(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "SKILL.md"))
	template := substringBetween(t, skill, "#### Verify-friendly RunE template", "If the command reads a file or directory")
	starters := substringBetween(t, skill, "**Starter templates for novel commands.**", "For flat-only resources")

	assert.Contains(t, template, "if len(args) == 0 && cmd.Flags().NFlag() == 0 {")
	assert.Regexp(t, regexp.MustCompile(`if len\(args\) == 0 && cmd\.Flags\(\)\.NFlag\(\) == 0 \{\s+return cmd\.Help\(\)\s+\}`), template)
	assert.Contains(t, template, "if dryRunOK(flags) {")
	assert.Contains(t, template, "_ = cmd.Usage()")
	assert.Contains(t, template, `return usageErr(fmt.Errorf("<flag-or-arg> is required"))`)
	assert.Contains(t, template, "Do not collapse the first and third branches")
	assert.Contains(t, template, "Multi-positional commands (N >= 2 required args) must use a two-check shape")
	assert.Contains(t, template, "if len(args) < N {")
	assert.Contains(t, template, `return usageErr(fmt.Errorf("missing required positional argument"))`)
	// The multi-positional block specifically must print usage before the
	// error (the bare Contains for "_ = cmd.Usage()" above is satisfied by the
	// single-positional block, so scope this assertion to the new branch).
	assert.Regexp(t, regexp.MustCompile(`if len\(args\) < N \{\s+_ = cmd\.Usage\(\)\s+return usageErr\(fmt\.Errorf\("missing required positional argument"\)\)`), template,
		"multi-positional block must call cmd.Usage() before returning the usage error")

	assert.Equal(t, 3, strings.Count(starters, "if len(args) == 0 && cmd.Flags().NFlag() == 0 {"))
	assert.Equal(t, 3, strings.Count(starters, "return cmd.Help()"))
	assert.Equal(t, 3, strings.Count(starters, "if dryRunOK(flags) {"))
	assert.Equal(t, 3, strings.Count(starters, "_ = cmd.Usage()"))
	assert.Equal(t, 3, strings.Count(starters, `return usageErr(fmt.Errorf("<flag-or-arg> is required"))`))
	assert.Contains(t, starters, "**RunE skeleton — parallel-fetch aggregation shape**")
	assert.Contains(t, starters, "successfulItems = append(successfulItems, entry)")
	assert.Contains(t, starters, "Items:         successfulItems")
	assert.Contains(t, starters, `json tag: `+"`json:\"fetch_failures,omitempty\"`")
	assert.Contains(t, starters, "averages computed over the remaining %d items")
	assert.Contains(t, starters, "partial results: %d of %d fetches failed; average computed over %d items")
}

func TestPrintingPressSkillRequiresScanAndFilterCaps(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "SKILL.md"))
	block := substringBetween(t, skill, "12. **Scan-and-filter caps**", "#### Verify-friendly RunE template")

	assert.Contains(t, block, `"list, filter locally, fan out to detail"`)
	assert.Contains(t, block, "**`--max-scan-pages int`**")
	assert.Contains(t, block, "**`scanned_<unit>` in the JSON envelope**")
	assert.Contains(t, block, "**`note` in zero-match JSON output**")
	assert.Contains(t, block, "**Clear separation between output and scan caps**")
	assert.Contains(t, block, "`--limit` controls how")
	assert.Contains(t, block, "`--max-scan-pages` controls how")
	assert.Contains(t, block, "cliutil.IsDogfoodEnv()")
	assert.Contains(t, block, "raise --max-scan-pages to widen the search")
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

func TestBrowserSniffEscalates200ChallengeShells(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "SKILL.md"))
	capture := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "references", "browser-sniff-capture.md"))

	assert.Contains(t, skill, "HTTP `200` but only a content-less shell, interstitial, or deterministic-size truncation")
	assert.Contains(t, skill, "Do not conclude `IP-blocked`, `rate-limited`, or `wait it out`")
	assert.Contains(t, skill, "Use chrome-MCP to understand the wall")

	assert.Contains(t, capture, "HTTP `200` responses that only contain a content-less shell, interstitial, deterministic-size truncation")
	assert.Contains(t, capture, "Do not treat a 200-served shell as evidence for `IP-blocked`, `rate-limited`, or `wait it out`")
	assert.Contains(t, capture, "HTTP 200 challenge shells or truncations")
}

func TestBrowserSniffManualHARGuidesReliableBodyCapture(t *testing.T) {
	capture := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "references", "browser-sniff-capture.md"))

	assert.Contains(t, capture, "Chrome can export page responses from disk cache as `206` partial-content entries with empty `response.content.text`")
	assert.Contains(t, capture, "check **Disable cache**, then hard-reload each page while DevTools stays open before exporting the HAR")
	assert.Contains(t, capture, "ask for a Firefox HAR export instead")
	assert.Contains(t, capture, "Hard-reload each page, then reproduce the user flow")
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

func TestPrintingPressSkillReprintPromoteRoutingHandlesRebuiltNovels(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press", "SKILL.md"))
	reprint := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press-reprint", "SKILL.md"))

	promote := substringBetween(t, skill, "### Promote to Library", "`ship-with-gaps` is promoted")

	assert.Contains(t, promote, "Before choosing Path B for `NOVEL_COUNT > 0`, distinguish preservation")
	assert.Contains(t, promote, "from-scratch reprint whose fresh tree reimplements all prior novels")
	assert.Contains(t, promote, "REGEN_DRY_RUN_REPORT=\"$PROOFS_DIR/regen-merge-dry-run-report.json\"")
	assert.Contains(t, promote, "regen-merge dry-run failed; see $REGEN_DRY_RUN_REPORT")
	assert.Contains(t, promote, `DRY_RUN_BLOCKERS=$(jq '[.files[]? | select(.verdict == "NOVEL"`)
	assert.Contains(t, promote, `or .verdict == "NOVEL-COLLISION")] | length' "$REGEN_DRY_RUN_REPORT")`)
	assert.Contains(t, promote, `MISSING_REFERENTS=$(jq '[.lost_registrations[]?`)
	assert.Contains(t, promote, `select((.skipped_for_missing_referent // []) | length > 0)] | length'`)
	assert.Contains(t, promote, "Treat")
	assert.Contains(t, promote, "generated-file `TEMPLATED-BODY-DRIFT`, `TEMPLATED-VALUE-DRIFT`, and stale")
	assert.Contains(t, promote, "templated-helper `TEMPLATED-WITH-ADDITIONS` as expected overwrite noise")
	assert.Contains(t, promote, "any prior novel file still reports `NOVEL`")
	assert.Contains(t, promote, "any file reports `NOVEL-COLLISION`")
	assert.Contains(t, promote, "`lost_registrations[].skipped_for_missing_referent` is non-empty")
	assert.Contains(t, promote, "A false Path A clobbers hand work; a")
	assert.Contains(t, promote, "false Path B only asks for review")

	assert.Contains(t, reprint, "Phase 5.6 first\ndry-runs `cli-printing-press regen-merge")
	assert.Contains(t, reprint, "fresh tree contains all prior novel work")
	assert.Contains(t, reprint, "genuine `NOVEL-COLLISION` / missing-referent cases halt")
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

func TestGeneratedAgentsTemplatePointsToCatalogForPatchMechanics(t *testing.T) {
	template := readContractFile(t, filepath.Join("..", "generator", "templates", "agents.md.tmpl"))

	// The per-CLI guide keeps CLI-local orientation plus a pointer to where
	// customizations are recorded, but must NOT duplicate the patch-entry
	// mechanics (schema, deferred_to_upstream, upstream_issue) -- those live once
	// in the source catalog's AGENTS.md, the single source of truth. Duplicating
	// ecosystem schema into every generated CLI is what let published AGENTS.md
	// drift to the legacy patch form; a stable pointer cannot rot.
	assert.Contains(t, template, "## Local Customizations")
	assert.Contains(t, template, ".printing-press-patches/")
	assert.Contains(t, template, "source catalog's `AGENTS.md`")

	// Mechanics must not be re-inlined into the per-CLI template.
	assert.NotContains(t, template, "deferred_to_upstream")
	assert.NotContains(t, template, "upstream_issue")
	assert.NotContains(t, template, "schema_version")
	assert.NotContains(t, template, "Minimum shape:")
}

func TestPolishSkillHardGatesPublishValidate(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press-polish", "SKILL.md"))

	assert.Contains(t, skill, `cli-printing-press publish validate --dir "$CLI_DIR" --json`)
	assert.Contains(t, skill, "Publish validation failures")
	assert.Contains(t, skill, "The publish-validate leg is a hard ship-gate")
	assert.Contains(t, skill, "phase5 acceptance")
	assert.Contains(t, skill, "ship cannot fire while publish validate fails")
}

func TestPolishSkillPinsGo126CompatibleGosecFallback(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press-polish", "SKILL.md"))
	fallback := "go run github.com/securego/gosec/v2/cmd/gosec@v2.26.1"

	assert.Equal(t, 3, strings.Count(skill, fallback))
	assert.Contains(t, skill, fallback+" -fmt=json -out=/tmp/polish-gosec-before.json ./...")
	assert.Contains(t, skill, fallback+" -fmt=json -out=/tmp/polish-gosec-after.json ./...")
	assert.NotContains(t, skill, "github.com/securego/gosec/v2/cmd/gosec@v2.21.4")
}

func TestPublishSkillRerunsLiveGateBeforeManagedClone(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press-publish", "SKILL.md"))
	validateStart := strings.Index(skill, "## Step 4: Validate")
	liveGateStart := strings.Index(skill, "## Step 4.5: Live End-to-End Gate")
	cloneStart := strings.Index(skill, "## Step 5: Managed Clone")
	require.NotEqual(t, -1, validateStart)
	require.NotEqual(t, -1, liveGateStart)
	require.NotEqual(t, -1, cloneStart)
	require.Less(t, validateStart, liveGateStart)
	require.Less(t, liveGateStart, cloneStart)

	liveGateBlock := skill[liveGateStart:cloneStart]
	assert.Contains(t, liveGateBlock, `dogfood`)
	assert.Contains(t, liveGateBlock, `--live`)
	assert.Contains(t, liveGateBlock, `--level full`)
	assert.Contains(t, liveGateBlock, `--timeout 120s`)
	assert.Contains(t, liveGateBlock, `--write-acceptance "$PROOFS_DIR/phase5-acceptance.json"`)
	assert.Contains(t, liveGateBlock, `"$PRINTING_PRESS_BIN" publish validate --dir "$CLI_DIR" --json`)
	assert.Contains(t, liveGateBlock, `--skip-live-test=<reason>`)
	assert.Contains(t, liveGateBlock, `auth_type=none during a known upstream outage or LAN-unreachable hardware case`)
	assert.Contains(t, liveGateBlock, `local_network_only = true`)
	assert.Contains(t, liveGateBlock, `API_KEY_AVAILABLE=true`)
	assert.Contains(t, liveGateBlock, `api_key_available: $api_key_available`)
	assert.Contains(t, skill, "### Publish Live Gate")
}

func TestPolishPublishOfferRequiresFreshUserTurn(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press-polish", "SKILL.md"))
	reference := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press-polish", "references", "publish-turn-boundary.md"))

	assert.Contains(t, skill, "references/publish-turn-boundary.md")
	assert.Contains(t, skill, "fresh user-authored message")
	assert.Contains(t, skill, "Do not invoke `/printing-press-publish <cli-name>` from this same turn")
	assert.Contains(t, skill, "After printing the handoff, stop")
	assert.Contains(t, skill, "**Publish separately** (recommended)")
	assert.Contains(t, skill, "show the publish command for the next user message")
	assert.Contains(t, skill, "/printing-press-publish <cli-name> --from-polish")
	assert.Contains(t, skill, "post-publish retro offer")
	assert.Contains(t, reference, "--from-polish")
	assert.Contains(t, reference, "Treat the menu answer as intent to hand off, not permission to execute")
	assert.NotContains(t, skill, "Then invoke `/printing-press-publish <cli-name>`")
	assert.NotContains(t, skill, "**Publish now** (recommended)")
	assert.NotContains(t, skill, "validate, package, and open a PR")
	assert.NotContains(t, reference, "Publish now")
}

func TestPublishSkillRejectsChainedPublishInvocations(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press-publish", "SKILL.md"))
	guardStart := strings.Index(skill, "## Direct User Invocation Required")
	setupStart := strings.Index(skill, "## Setup")
	require.NotEqual(t, -1, guardStart)
	require.NotEqual(t, -1, setupStart)
	require.Less(t, guardStart, setupStart)
	guardBlock := skill[guardStart:setupStart]

	assert.Contains(t, guardBlock, "chained continuation from `printing-press-polish`'s")
	assert.Contains(t, guardBlock, "auto-resolved")
	assert.Contains(t, guardBlock, "recommendation")
	assert.Contains(t, guardBlock, "stop immediately")
	assert.Contains(t, guardBlock, "fresh user-authored")
	assert.Contains(t, guardBlock, "--from-polish")
	assert.Contains(t, guardBlock, "POLISH_HANDOFF=true")
	assert.Contains(t, guardBlock, "ignore that marker when")
}

func TestPublishSkillOffersRetroForPolishHandoff(t *testing.T) {
	skill := readContractFile(t, filepath.Join("..", "..", "skills", "printing-press-publish", "SKILL.md"))
	terminalStart := strings.Index(skill, "### Terminal state")
	require.NotEqual(t, -1, terminalStart)
	terminalBlock := skill[terminalStart:]

	assert.Contains(t, terminalBlock, "direct human invocation without `--from-polish` just ends here")
	assert.Contains(t, terminalBlock, "If `POLISH_HANDOFF=true`, offer retro")
	assert.Contains(t, terminalBlock, "standalone polish")
	assert.Contains(t, terminalBlock, "AskUserQuestion")
	assert.Contains(t, terminalBlock, "/printing-press-retro")
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

func runContractScript(t *testing.T, path string, env []string, args ...string) string {
	t.Helper()

	cmd := exec.Command(path, args...)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	return string(output)
}
