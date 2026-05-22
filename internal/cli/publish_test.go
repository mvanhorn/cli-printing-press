package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/govulncheck"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipIfRootCannotSimulateUnreadable skips tests that rely on chmod 0
// making a file unreadable. Root bypasses file-mode checks on Linux, so
// these tests can't produce the expected copy failure when euid == 0
// (CI sandboxes, devcontainers, some cloud runners).
func skipIfRootCannotSimulateUnreadable(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root; chmod 0 does not block reads — cannot simulate an unreadable-file failure")
	}
}

func publishCheckByName(t *testing.T, result ValidateResult, name string) CheckResult {
	t.Helper()
	for _, check := range result.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("missing %q check in %#v", name, result.Checks)
	return CheckResult{}
}

func TestPublishValidateMissingManifest(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"validate", "--dir", cliDir, "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	// Should fail with ExitPublishError
	require.Error(t, err)

	var result ValidateResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.False(t, result.Passed)

	// Find the manifest check
	var manifestCheck *CheckResult
	for i := range result.Checks {
		if result.Checks[i].Name == "manifest" {
			manifestCheck = &result.Checks[i]
			break
		}
	}
	require.NotNil(t, manifestCheck)
	assert.False(t, manifestCheck.Passed)
	assert.Contains(t, manifestCheck.Error, "missing")
}

func TestPublishValidateManifestMissingFields(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	// Write a manifest missing required fields
	writeTestManifest(t, cliDir, pipeline.CLIManifest{SchemaVersion: 1})

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"validate", "--dir", cliDir, "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.Error(t, err)

	var result ValidateResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.False(t, result.Passed)

	var manifestCheck *CheckResult
	for i := range result.Checks {
		if result.Checks[i].Name == "manifest" {
			manifestCheck = &result.Checks[i]
			break
		}
	}
	require.NotNil(t, manifestCheck)
	assert.False(t, manifestCheck.Passed)
	assert.Contains(t, manifestCheck.Error, "missing required manifest fields")
}

func TestPublishValidateRejectsStaleAttributionManifest(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "openrouter-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	writeTestManifest(t, cliDir, pipeline.CLIManifest{
		SchemaVersion:        0,
		PrintingPressVersion: "4.2.0",
		APIName:              "openrouter",
		CLIName:              "openrouter-pp-cli",
		RunID:                "20260509-165428",
		Printer:              "rvdlaar",
	})

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"validate", "--dir", cliDir, "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.Error(t, err)

	var result ValidateResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.False(t, result.Passed)

	manifestCheck := publishCheckByName(t, result, "manifest")
	assert.False(t, manifestCheck.Passed)
	assert.Contains(t, manifestCheck.Error, "schema_version must be 1")
	assert.Contains(t, manifestCheck.Error, "printer_name")
}

func TestPublishManifestContractRejectsPrinterSentinel(t *testing.T) {
	issues := validatePublishManifestContract(t.TempDir(), pipeline.CLIManifest{
		SchemaVersion:        pipeline.CurrentCLIManifestSchemaVersion,
		PrintingPressVersion: "4.2.1",
		APIName:              "test",
		CLIName:              "test-pp-cli",
		RunID:                "20260509-000000",
		Printer:              "USER",
		PrinterName:          "Test User",
	})

	require.Len(t, issues, 1)
	assert.Contains(t, issues[0], "literal sentinel")
}

func TestPublishManifestContractRequiresMCPMetadataFiles(t *testing.T) {
	issues := validatePublishManifestContract(t.TempDir(), pipeline.CLIManifest{
		SchemaVersion:        pipeline.CurrentCLIManifestSchemaVersion,
		PrintingPressVersion: "4.2.1",
		APIName:              "test",
		CLIName:              "test-pp-cli",
		RunID:                "20260509-000000",
		Printer:              "tmchow",
		PrinterName:          "Trevin Chow",
		MCPBinary:            "test-pp-mcp",
	})

	assert.Contains(t, strings.Join(issues, "\n"), "manifest.json")
	assert.Contains(t, strings.Join(issues, "\n"), "tools-manifest.json")
}

func TestPublishValidateMissingDirFlag(t *testing.T) {
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"validate", "--json"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--dir is required")
}

func TestPublishValidateManuscriptsWarnOnly(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	writeTestManifest(t, cliDir, pipeline.CLIManifest{
		SchemaVersion: 1,
		APIName:       "test",
		CLIName:       "test-pp-cli",
	})

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"validate", "--dir", cliDir, "--json"})

	output, _ := runWithCapturedStdout(t, cmd.Execute)

	var result ValidateResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	// Find the manuscripts check
	var msCheck *CheckResult
	for i := range result.Checks {
		if result.Checks[i].Name == "manuscripts" {
			msCheck = &result.Checks[i]
			break
		}
	}
	require.NotNil(t, msCheck, "manuscripts check should always be present")
	// Manuscripts missing should be a warning, not a failure
	assert.True(t, msCheck.Passed, "manuscripts check should pass (warn-only)")
	assert.NotEmpty(t, msCheck.Warning, "should have a warning about missing manuscripts")
}

func TestPublishValidateJSONHasAllChecks(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	writeTestManifest(t, cliDir, pipeline.CLIManifest{
		SchemaVersion: 1,
		APIName:       "test",
		CLIName:       "test-pp-cli",
	})

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"validate", "--dir", cliDir, "--json"})

	output, _ := runWithCapturedStdout(t, cmd.Execute)

	var result ValidateResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	// Should have all publish validation check names
	checkNames := make(map[string]bool)
	for _, c := range result.Checks {
		checkNames[c.Name] = true
	}

	// All checks should be present (they may fail in test env, but must exist)
	expectedChecks := []string{"manifest", "transcendence", "phase5", "go mod tidy", "govulncheck", "go vet", "go build", "--help", "--version", "verify-skill", "manuscripts"}
	for _, name := range expectedChecks {
		assert.True(t, checkNames[name], "should have %q check", name)
	}
	assert.Len(t, result.Checks, len(expectedChecks), "should have exactly the expected checks")
}

func TestPublishValidateFailsWithoutPhase5Marker(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)
	writeTestManifest(t, cliDir, pipeline.CLIManifest{
		SchemaVersion:        pipeline.CurrentCLIManifestSchemaVersion,
		PrintingPressVersion: "test-version",
		APIName:              "test",
		CLIName:              "test-pp-cli",
		RunID:                "run-missing-phase5",
		Printer:              "tmchow",
		PrinterName:          "Trevin Chow",
		AuthType:             "api_key",
		NovelFeatures: []pipeline.NovelFeatureManifest{
			{Name: "Insight", Command: "insight", Description: "Show test insight."},
		},
	})

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"validate", "--dir", cliDir, "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.Error(t, err)

	var result ValidateResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.False(t, result.Passed)

	var phase5Check *CheckResult
	for i := range result.Checks {
		if result.Checks[i].Name == "phase5" {
			phase5Check = &result.Checks[i]
			break
		}
	}
	require.NotNil(t, phase5Check)
	assert.False(t, phase5Check.Passed)
	assert.Contains(t, phase5Check.Error, "missing")
}

func TestPublishValidateRequiresTranscendenceFeatures(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	data, err := json.MarshalIndent(pipeline.CLIManifest{
		SchemaVersion:        pipeline.CurrentCLIManifestSchemaVersion,
		PrintingPressVersion: "test-version",
		APIName:              "test",
		CLIName:              "test-pp-cli",
		RunID:                "20260301-000000",
		Printer:              "tmchow",
		PrinterName:          "Trevin Chow",
	}, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, pipeline.CLIManifestFilename), data, 0o644))

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"validate", "--dir", cliDir, "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.Error(t, err)

	var result ValidateResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.False(t, result.Passed)

	var check *CheckResult
	for i := range result.Checks {
		if result.Checks[i].Name == "transcendence" {
			check = &result.Checks[i]
			break
		}
	}
	require.NotNil(t, check)
	assert.False(t, check.Passed)
	assert.Contains(t, check.Error, "no novel features recorded")
}

func TestPublishValidateExitCode(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	// No manifest -> validation fails

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"validate", "--dir", cliDir, "--json"})

	_, err := runWithCapturedStdout(t, cmd.Execute)
	require.Error(t, err)

	var exitErr *ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, ExitPublishError, exitErr.Code, "should use ExitPublishError exit code")
}

func TestRunGoVulnCheckRequiresGoMod(t *testing.T) {
	result := runGoVulnCheck(t.TempDir())
	assert.False(t, result.Passed)
	assert.Equal(t, govulncheck.Name, result.Name)
	assert.Equal(t, "go.mod not found", result.Error)
}

func TestRunGoVulnCheckUsesPinnedDefaultCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell go binary is Unix-only")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.26.3\n"), 0o644))

	fakeBin := t.TempDir()
	callsPath := filepath.Join(t.TempDir(), "go-calls.txt")
	fakeGo := filepath.Join(fakeBin, "go")
	require.NoError(t, os.WriteFile(fakeGo, []byte(`#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_GO_CALLS"
echo "fake govulncheck failure" >&2
exit 42
`), 0o755))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GO_CALLS", callsPath)

	result := runGoVulnCheck(dir)
	assert.False(t, result.Passed)
	assert.Equal(t, govulncheck.Name, result.Name)
	assert.Contains(t, result.Error, "fake govulncheck failure")

	calls, err := os.ReadFile(callsPath)
	require.NoError(t, err)
	assert.Equal(t, "run "+govulncheck.ToolModule+" ./...\n", string(calls))
	assert.NotContains(t, string(calls), "-show")
	assert.NotContains(t, string(calls), "verbose")
}

func TestPublishPackageMissingDirFlag(t *testing.T) {
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--json"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--dir is required")
}

func TestPublishPackageMissingCategoryFlag(t *testing.T) {
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", "/tmp/fake", "--json"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--category is required")
}

func TestPublishPackageMissingTargetAndDestFlags(t *testing.T) {
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", "/tmp/fake", "--category", "ai", "--json"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--target or --dest is required")
}

func TestPublishPackageTargetAndDestMutuallyExclusive(t *testing.T) {
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", "/tmp/fake", "--category", "ai", "--target", "/tmp/a", "--dest", "/tmp/b", "--json"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestPublishPackageTargetExists(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	writeTestManifest(t, cliDir, pipeline.CLIManifest{
		SchemaVersion: 1,
		APIName:       "test",
		CLIName:       "test-pp-cli",
	})

	// Create target directory (already exists)
	target := filepath.Join(home, "staging")
	require.NoError(t, os.MkdirAll(target, 0o755))

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "developer-tools", "--target", target, "--json"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestPublishPackageCategoryPathTraversal(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	writeTestManifest(t, cliDir, pipeline.CLIManifest{
		SchemaVersion: 1,
		APIName:       "test",
		CLIName:       "test-pp-cli",
	})

	tests := []struct {
		name     string
		category string
		wantErr  string
	}{
		{"dotdot traversal", "../../../escape", "simple slug"},
		{"forward slash", "foo/bar", "simple slug"},
		{"backslash", "foo\\bar", "simple slug"},
		{"dotdot only", "..", "simple slug"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "staging")
			cmd := newPublishCmd()
			cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", tt.category, "--target", target, "--json"})

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestPublishPackageRejectsUnknownCategory(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)

	target := filepath.Join(t.TempDir(), "staging")
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "banana", "--target", target, "--json"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--category must be one of:")
}

func TestPublishPackageFailsWhenSkillReferencesUnknownCommand(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)

	// Regression guard: library CI caught SKILL.md references such as
	// `wikipedia-pp-cli feed get-on-this-day` where the shipped CLI only had
	// `feed`. publish package should fail locally before staging that PR.
	skillPath := filepath.Join(cliDir, "SKILL.md")
	f, err := os.OpenFile(skillPath, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString("\n```bash\ntest-pp-cli hallucinated-command --agent\n```\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	target := filepath.Join(t.TempDir(), "staging")
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--target", target, "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")

	var result ValidateResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.False(t, result.Passed)

	var skillCheck *CheckResult
	for i := range result.Checks {
		if result.Checks[i].Name == "verify-skill" {
			skillCheck = &result.Checks[i]
			break
		}
	}
	require.NotNil(t, skillCheck)
	assert.False(t, skillCheck.Passed)
	assert.Contains(t, skillCheck.Error, "unknown-command")

	_, statErr := os.Stat(target)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "failed verification should not create staging target")
}

func TestPublishPackageDoesNotStageCompiledBinary(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)

	target := filepath.Join(t.TempDir(), "staging")
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--target", target, "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var result PackageResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	_, sourceErr := os.Stat(filepath.Join(cliDir, "test-pp-cli"))
	assert.ErrorIs(t, sourceErr, os.ErrNotExist, "validation should not leave a root binary behind")

	_, stagedErr := os.Stat(filepath.Join(result.StagedDir, "test-pp-cli"))
	assert.ErrorIs(t, stagedErr, os.ErrNotExist, "packaged source should not include a compiled binary")
}

func TestPublishPackageStripsBuildDir(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)

	// Simulate autoBundleForHost output: build/ exists in the source
	// dir with a host-platform .mcpb and a staged binary copy.
	buildDir := filepath.Join(cliDir, "build")
	require.NoError(t, os.MkdirAll(filepath.Join(buildDir, "stage", "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(buildDir, "test-pp-mcp-darwin-arm64.mcpb"), []byte("zip-bytes"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(buildDir, "stage", "bin", "test-pp-mcp"), []byte("staged-binary"), 0o755))

	target := filepath.Join(t.TempDir(), "staging")
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--target", target, "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var result PackageResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	// Source build/ stays intact — we don't touch the user's working tree.
	_, sourceErr := os.Stat(buildDir)
	assert.NoError(t, sourceErr, "package must not delete the source build/ dir")

	// Staged tree must have no build/ — CI is canonical for distribution.
	_, stagedErr := os.Stat(filepath.Join(result.StagedDir, "build"))
	assert.ErrorIs(t, stagedErr, os.ErrNotExist, "staged dir must not include build/")
}

func TestPublishPackageStripsRootBinaries(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)

	// Simulate an operator who ran `go build ./cmd/...` (or `make build` /
	// `make build-mcp` without `-o bin/...`): binaries land at the CLI
	// dir root, named per the generator convention. None of these should
	// reach the staged tree, regardless of which one the publish skill's
	// rm -f line happened to enumerate.
	for _, name := range []string{
		"test",        // phantom bare-slug binary
		"test-pp-cli", // primary CLI binary
		"test-pp-mcp", // MCP peer binary (now covered by both code-level strip and skill cleanup)
	} {
		require.NoError(t, os.WriteFile(filepath.Join(cliDir, name), []byte("\x7fELF"), 0o755))
	}

	target := filepath.Join(t.TempDir(), "staging")
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--target", target, "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var result PackageResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	// Source binaries stay — we don't mutate the operator's working tree.
	for _, name := range []string{"test", "test-pp-cli", "test-pp-mcp"} {
		_, srcErr := os.Stat(filepath.Join(cliDir, name))
		assert.NoError(t, srcErr, "package must not delete source-tree binary %s", name)
	}

	// Staged tree must have none of them.
	for _, name := range []string{"test", "test-pp-cli", "test-pp-mcp"} {
		_, stagedErr := os.Stat(filepath.Join(result.StagedDir, name))
		assert.ErrorIs(t, stagedErr, os.ErrNotExist, "staged dir must not include root-level binary %s", name)
	}
}

func TestStagedBinaryNamesDeduplicates(t *testing.T) {
	t.Parallel()

	got := stagedBinaryNames("foo-pp-cli", "foo")
	want := []string{"foo", "foo-pp-cli", "foo-pp-mcp"}
	assert.Equal(t, want, got)

	// Empty slug returns only the cliName (no -pp-cli/-pp-mcp suffix expansion).
	got = stagedBinaryNames("custom-binary", "")
	want = []string{"custom-binary"}
	assert.Equal(t, want, got)

	// Empty both is empty.
	assert.Empty(t, stagedBinaryNames("", ""))
}

func TestPublishPackageFailsWhenManuscriptsCopyFails(t *testing.T) {
	skipIfRootCannotSimulateUnreadable(t)
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)

	runID := "20260328-132022"
	manuscriptFile := filepath.Join(home, "manuscripts", "test", runID, "research", "brief.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(manuscriptFile), 0o755))
	require.NoError(t, os.WriteFile(manuscriptFile, []byte("brief"), 0o600))
	require.NoError(t, os.Chmod(manuscriptFile, 0))
	defer func() {
		_ = os.Chmod(manuscriptFile, 0o600)
	}()

	target := filepath.Join(t.TempDir(), "staging")
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--target", target, "--json"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "copying manuscripts")

	_, statErr := os.Stat(target)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "failed packaging should clean up the staging target")
}

func TestPublishPackageIncludesManuscripts(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)

	// Create manuscripts at the archived location where publish package looks
	runID := "20260329-100000"
	researchDir := filepath.Join(home, "manuscripts", "test", runID, "research")
	proofsDir := filepath.Join(home, "manuscripts", "test", runID, "proofs")
	require.NoError(t, os.MkdirAll(researchDir, 0o755))
	require.NoError(t, os.MkdirAll(proofsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(researchDir, "brief.md"), []byte("# Research Brief"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(proofsDir, "shipcheck.md"), []byte("# Shipcheck"), 0o644))

	target := filepath.Join(t.TempDir(), "staging")
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--target", target, "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var result PackageResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.True(t, result.ManuscriptsIncluded, "manuscripts should be included")
	assert.Equal(t, runID, result.RunID, "run ID should match the most recent run")

	// Verify manuscripts are in the staged package
	stagedResearch := filepath.Join(result.StagedDir, ".manuscripts", runID, "research", "brief.md")
	stagedProofs := filepath.Join(result.StagedDir, ".manuscripts", runID, "proofs", "shipcheck.md")

	_, err = os.Stat(stagedResearch)
	assert.NoError(t, err, "research brief should be in staged package")

	_, err = os.Stat(stagedProofs)
	assert.NoError(t, err, "shipcheck proofs should be in staged package")
}

func TestPublishPackageRejectsVendorPrefixSecretsInStagedCLI(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "spec.json"), []byte("{\n  \"token\": \""+testSecret("sk", "-or-v1-", "abcdefghijklmnopqrstuvwxyz1234567890")+"\"\n}\n"), 0o644))

	target := filepath.Join(t.TempDir(), "staging")
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--target", target, "--json"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vendor-prefix tokens detected")
	assert.Contains(t, err.Error(), "spec.json:2 openrouter-api-key")

	_, statErr := os.Stat(target)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "failed packaging should clean up the staging target")
}

func TestPublishPackageRejectsSpecDeclaredCookieValuesInStagedCLI(t *testing.T) {
	for _, authType := range []string{"cookie", "composed"} {
		t.Run(authType, func(t *testing.T) {
			home := setLibraryTestEnv(t)
			cliDir := filepath.Join(home, "library", "test-pp-cli")
			writePublishableTestCLI(t, cliDir)
			require.NoError(t, os.WriteFile(filepath.Join(cliDir, "tools-manifest.json"), []byte(`{"auth":{"type":"`+authType+`","cookies":["session-id"]}}`+"\n"), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(cliDir, "README.md"), []byte("Cookie: session-id=actuallyrealcookievaluexyz\n"), 0o644))

			target := filepath.Join(t.TempDir(), "staging")
			cmd := newPublishCmd()
			cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--target", target, "--json"})

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "vendor-prefix tokens detected")
			assert.Contains(t, err.Error(), "README.md:1 cookie-value:session-id")
			assert.NotContains(t, err.Error(), "actuallyrealcookievaluexyz")

			_, statErr := os.Stat(target)
			assert.ErrorIs(t, statErr, os.ErrNotExist, "failed packaging should clean up the staging target")
		})
	}
}

func TestPublishPackageRejectsPIIInStagedCLI(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)
	// Plant real-shaped PII in a high-risk file
	require.NoError(t, os.WriteFile(
		filepath.Join(cliDir, "data.json"),
		[]byte(`{"customer_email": "alice@example.com"}`+"\n"),
		0o644,
	))

	target := filepath.Join(t.TempDir(), "staging")
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--target", target, "--json"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "customer PII detected")
	assert.Contains(t, err.Error(), "email")

	_, statErr := os.Stat(target)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "failed packaging should clean up the staging target")
}

func TestPublishPackageRejectsPIIInManuscripts(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)
	runID := "20260329-100000"
	researchFile := filepath.Join(home, "manuscripts", "test", runID, "research", "captured.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(researchFile), 0o755))
	require.NoError(t, os.WriteFile(researchFile, []byte(`{"recipient_email": "leak@example.com"}`+"\n"), 0o644))

	target := filepath.Join(t.TempDir(), "staging")
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--target", target, "--json"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "customer PII detected")
	assert.Contains(t, err.Error(), "captured.json")

	_, statErr := os.Stat(target)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "failed packaging should clean up the staging target")
}

func TestPublishPackageCombinesSecretAndPIIReports(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)
	// Plant both a secret AND PII
	require.NoError(t, os.WriteFile(
		filepath.Join(cliDir, "spec.json"),
		[]byte("{\n  \"token\": \""+testSecret("sk", "-or-v1-", "abcdefghijklmnopqrstuvwxyz1234567890")+"\"\n}\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(cliDir, "data.json"),
		[]byte(`{"customer_email": "alice@example.com"}`+"\n"),
		0o644,
	))

	target := filepath.Join(t.TempDir(), "staging")
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--target", target, "--json"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vendor-prefix tokens detected")
	assert.Contains(t, err.Error(), "customer PII detected")

	_, statErr := os.Stat(target)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "failed packaging should clean up the staging target even on combined-error path")
}

func TestPublishPackageRejectsVendorPrefixSecretsInManuscripts(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)
	runID := "20260329-100000"
	researchFile := filepath.Join(home, "manuscripts", "test", runID, "research", "openapi.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(researchFile), 0o755))
	require.NoError(t, os.WriteFile(researchFile, []byte("Authorization: Bearer "+testSecret("sk", "_live_", "1234567890abcdefghijklmnop")+"\n"), 0o644))

	target := filepath.Join(t.TempDir(), "staging")
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--target", target, "--json"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vendor-prefix tokens detected")
	assert.Contains(t, err.Error(), ".manuscripts/20260329-100000/research/openapi.json:1 stripe-secret-key")

	_, statErr := os.Stat(target)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "failed packaging should clean up the staging target")
}

func TestFindMostRecentRun(t *testing.T) {
	dir := t.TempDir()

	// Create run directories with timestamp-prefixed names and content
	for _, run := range []string{"20260327-100000", "20260328-132022", "20260326-090000"} {
		researchDir := filepath.Join(dir, run, "research")
		require.NoError(t, os.MkdirAll(researchDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(researchDir, "brief.md"), []byte("test"), 0o644))
	}

	runID, err := findMostRecentRun(dir)
	require.NoError(t, err)
	assert.Equal(t, "20260328-132022", runID, "should pick the most recent by lexicographic sort")
}

func TestFindMostRecentRunSkipsEmptyDirectories(t *testing.T) {
	dir := t.TempDir()

	// Most recent run is empty (interrupted archive)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "20260329-100000"), 0o755))

	// Older run has actual content
	researchDir := filepath.Join(dir, "20260328-132022", "research")
	require.NoError(t, os.MkdirAll(researchDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(researchDir, "brief.md"), []byte("test"), 0o644))

	runID, err := findMostRecentRun(dir)
	require.NoError(t, err)
	assert.Equal(t, "20260328-132022", runID, "should skip empty run and use older one with content")
}

func TestFindMostRecentRunAllEmpty(t *testing.T) {
	dir := t.TempDir()

	// All runs are empty (no actual manuscript content)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "20260328-132022"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "20260327-100000"), 0o755))

	runID, err := findMostRecentRun(dir)
	require.NoError(t, err)
	assert.Empty(t, runID, "should return empty when all runs are empty directories")
}

func TestFindMostRecentRunEmpty(t *testing.T) {
	dir := t.TempDir()

	runID, err := findMostRecentRun(dir)
	require.NoError(t, err)
	assert.Empty(t, runID)
}

func TestFindMostRecentRunNonexistentDir(t *testing.T) {
	_, err := findMostRecentRun("/nonexistent/path")
	assert.Error(t, err)
}

func TestPublishPackageDestWritesDirectly(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)

	// Create manuscripts
	runID := "20260329-100000"
	researchDir := filepath.Join(home, "manuscripts", "test", runID, "research")
	require.NoError(t, os.MkdirAll(researchDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(researchDir, "brief.md"), []byte("# Brief"), 0o644))

	// Create a dest directory (simulating the publish repo)
	destDir := filepath.Join(t.TempDir(), "publish-repo")
	require.NoError(t, os.MkdirAll(destDir, 0o755))

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--dest", destDir, "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var result PackageResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.True(t, result.ManuscriptsIncluded, "manuscripts should be included")
	assert.Equal(t, runID, result.RunID)

	// Verify CLI is at dest/library/<category>/<api-slug>/
	cliOut := filepath.Join(destDir, "library", "other", "test")
	assert.Equal(t, cliOut, result.StagedDir)

	_, err = os.Stat(filepath.Join(cliOut, "go.mod"))
	assert.NoError(t, err, "go.mod should exist in dest")

	// Verify .manuscripts is written directly (not in a staging dir)
	msPath := filepath.Join(cliOut, ".manuscripts", runID, "research", "brief.md")
	_, err = os.Stat(msPath)
	assert.NoError(t, err, ".manuscripts should be written into dest")
}

func TestPublishPackageDestRemovesOldCLI(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)

	// Create a dest with an existing CLI in a different category (slug-keyed)
	destDir := filepath.Join(t.TempDir(), "publish-repo")
	oldCLIDir := filepath.Join(destDir, "library", "productivity", "test")
	require.NoError(t, os.MkdirAll(oldCLIDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldCLIDir, "old-file.go"), []byte("old"), 0o644))

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--dest", destDir, "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var result PackageResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	// Old CLI directory should be gone (both original and .old stash)
	_, err = os.Stat(oldCLIDir)
	assert.ErrorIs(t, err, os.ErrNotExist, "old CLI in different category should be removed")
	_, err = os.Stat(oldCLIDir + ".old")
	assert.ErrorIs(t, err, os.ErrNotExist, "stash dir should be cleaned up after success")

	// New CLI should exist at new category (slug-keyed)
	newCLIDir := filepath.Join(destDir, "library", "other", "test")
	_, err = os.Stat(filepath.Join(newCLIDir, "go.mod"))
	assert.NoError(t, err, "new CLI should exist at new category")
}

func TestPublishPackageDestRestoresOldCLIOnFailure(t *testing.T) {
	skipIfRootCannotSimulateUnreadable(t)
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)

	// Create manuscripts with an unreadable file to trigger copy failure
	runID := "20260329-100000"
	manuscriptFile := filepath.Join(home, "manuscripts", "test", runID, "research", "brief.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(manuscriptFile), 0o755))
	require.NoError(t, os.WriteFile(manuscriptFile, []byte("brief"), 0o600))
	require.NoError(t, os.Chmod(manuscriptFile, 0))
	defer func() { _ = os.Chmod(manuscriptFile, 0o600) }()

	// Create dest with existing CLI in a different category (slug-keyed)
	destDir := filepath.Join(t.TempDir(), "publish-repo")
	oldCLIDir := filepath.Join(destDir, "library", "productivity", "test")
	require.NoError(t, os.MkdirAll(oldCLIDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldCLIDir, "old-file.go"), []byte("old"), 0o644))

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--dest", destDir, "--json"})

	err := cmd.Execute()
	require.Error(t, err, "should fail due to unreadable manuscript")

	// Old CLI should be restored to its original location
	_, err = os.Stat(filepath.Join(oldCLIDir, "old-file.go"))
	assert.NoError(t, err, "old CLI should be restored after failure")

	// No stash leftovers
	_, err = os.Stat(oldCLIDir + ".old")
	assert.ErrorIs(t, err, os.ErrNotExist, "stash dir should not remain after restore")

	// New CLI dir should be cleaned up (slug-keyed)
	newCLIDir := filepath.Join(destDir, "library", "other", "test")
	_, err = os.Stat(newCLIDir)
	assert.ErrorIs(t, err, os.ErrNotExist, "failed new CLI dir should be cleaned up")
}

func TestPublishPackageDestNonexistent(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--dest", "/nonexistent/path", "--json"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestPublishRenameMissingFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"missing dir", []string{"rename", "--old-name", "a-pp-cli", "--new-name", "b-pp-cli", "--json"}, "--dir is required"},
		{"missing old-name", []string{"rename", "--dir", "/tmp/x", "--new-name", "b-pp-cli", "--json"}, "--old-name is required"},
		{"missing new-name", []string{"rename", "--dir", "/tmp/x", "--old-name", "a-pp-cli", "--json"}, "--new-name is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newPublishCmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestPublishRenameJSONSuccess(t *testing.T) {
	root := t.TempDir()
	oldName := "test-pp-cli"
	newName := "test-alt-pp-cli"
	cliDir := filepath.Join(root, oldName)
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "cmd", oldName), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "cmd", oldName, "main.go"), []byte(`package main
func main() {}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "README.md"), []byte("# "+oldName+"\n"), 0o644))

	writeTestManifest(t, cliDir, pipeline.CLIManifest{
		SchemaVersion: 1,
		APIName:       "test",
		CLIName:       oldName,
	})

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"rename", "--dir", cliDir, "--old-name", oldName, "--new-name", newName, "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var result RenameResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.True(t, result.Success)
	assert.Equal(t, oldName, result.OldName)
	assert.Equal(t, newName, result.NewName)
	assert.Equal(t, filepath.Join(root, naming.LibraryDirName(newName)), result.NewDir)
	assert.Greater(t, result.FilesModified, 0)
}

func TestPublishRenameAPINameTracksNewName(t *testing.T) {
	root := t.TempDir()
	oldName := "test-pp-cli"
	newName := "test-alt-pp-cli"
	cliDir := filepath.Join(root, oldName)
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	writeTestManifest(t, cliDir, pipeline.CLIManifest{
		SchemaVersion: 1,
		APIName:       "test",
		CLIName:       oldName,
	})

	cmd := newPublishCmd()
	// The legacy flag is accepted for old callers but no longer controls
	// metadata; the final public slug follows --new-name.
	cmd.SetArgs([]string{"rename", "--dir", cliDir, "--old-name", oldName, "--new-name", newName, "--api-name", "test", "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var result RenameResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.True(t, result.Success)

	// Verify manifest has the final public API slug after rename.
	newDir := filepath.Join(root, naming.LibraryDirName(newName))
	mData, err := os.ReadFile(filepath.Join(newDir, pipeline.CLIManifestFilename))
	require.NoError(t, err)
	var m pipeline.CLIManifest
	require.NoError(t, json.Unmarshal(mData, &m))
	assert.Equal(t, "test-alt", m.APIName, "api_name should track the final public slug")
	assert.Equal(t, newName, m.CLIName)
}

func TestPublishRenameJSONError(t *testing.T) {
	root := t.TempDir()
	cliDir := filepath.Join(root, "test-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	cmd := newPublishCmd()
	// Invalid new name — will fail validation
	cmd.SetArgs([]string{"rename", "--dir", cliDir, "--old-name", "test-pp-cli", "--new-name", "bad-name", "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.Error(t, err)

	var result RenameResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.False(t, result.Success)
	assert.NotEmpty(t, result.Error)
}

func writePublishableTestCLI(t *testing.T, dir string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "test-pp-cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(`module example.com/test-pp-cli

go 1.24

require github.com/spf13/cobra v1.10.2

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
)
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", "test-pp-cli", "main.go"), []byte(`package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--help":
			fmt.Println("help")
			return
		case "--version":
			fmt.Println("v0.0.0")
			return
		}
	}
	fmt.Println("ok")
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.sum"), []byte(`github.com/cpuguy83/go-md2man/v2 v2.0.6/go.mod h1:oOW0eioCTA6cOiMLiUPZOpcVxMig6NIQQ7OS05n1F4g=
github.com/inconshreveable/mousetrap v1.1.0 h1:wN+x4NVGpMsO7ErUn/mUI3vEoE6Jt13X2s0bqwp9tc8=
github.com/inconshreveable/mousetrap v1.1.0/go.mod h1:vpF70FUmC8bwa3OWnCshd2FqLfsEA9PFc4w1p2J65bw=
github.com/russross/blackfriday/v2 v2.1.0/go.mod h1:+Rmxgy9KzJVeS9/2gXHxylqXiyQDYRxCVz55jmeOWTM=
github.com/spf13/cobra v1.10.2 h1:DMTTonx5m65Ic0GOoRY2c16WCbHxOOw6xxezuLaBpcU=
github.com/spf13/cobra v1.10.2/go.mod h1:7C1pvHqHw5A4vrJfjNwvOdzYu0Gml16OCs2GRiTUUS4=
github.com/spf13/pflag v1.0.9 h1:9exaQaMOCwffKiiiYk6/BndUBv+iRViNW+4lEMi0PvY=
github.com/spf13/pflag v1.0.9/go.mod h1:McXfInJRrz4CZXVZOBLb0bTZqETkiAhM9Iw0y3An2Bg=
go.yaml.in/yaml/v3 v3.0.4/go.mod h1:DhzuOOF2ATzADvBadXxruRBLzYTpT36CKvDb3+aBEFg=
gopkg.in/check.v1 v0.0.0-20161208181325-20d25e280405/go.mod h1:Co6ibVJAznAaIkqp8huTwlJQCZ016jof/cbN4VW5Yz0=
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "root.go"), []byte(`package cli

import "github.com/spf13/cobra"

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "test-pp-cli"}
	cmd.AddCommand(newInsightCmd())
	return cmd
}

func newInsightCmd() *cobra.Command {
	return &cobra.Command{Use: "insight", Short: "Show test insight"}
}
`), 0o644))
	skillInstall := generator.CanonicalSkillInstallSection("test", "")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Test CLI\n\n"+skillInstall+"\n## Command Reference\n\n- `test-pp-cli insight` — Show test insight\n\n## Usage\n\n```bash\ntest-pp-cli insight --agent\n```\n"), 0o644))

	writeTestManifest(t, dir, pipeline.CLIManifest{
		SchemaVersion:        pipeline.CurrentCLIManifestSchemaVersion,
		PrintingPressVersion: "test-version",
		APIName:              "test",
		CLIName:              "test-pp-cli",
		RunID:                "20260301-000000",
		Printer:              "tmchow",
		PrinterName:          "Trevin Chow",
		AuthType:             "none",
		NovelFeatures: []pipeline.NovelFeatureManifest{
			{Name: "Insight", Command: "insight", Description: "Show test insight."},
		},
	})
	writePublishablePhase5Pass(t)
}

func writePublishablePhase5Pass(t *testing.T) {
	t.Helper()
	home := os.Getenv("PRINTING_PRESS_HOME")
	require.NotEmpty(t, home)
	proofsDir := filepath.Join(home, "manuscripts", "test", "20260301-000000", "proofs")
	writeTestPhase5GateMarker(t, proofsDir, pipeline.Phase5AcceptanceFilename, pipeline.Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "20260301-000000",
		Status:        "pass",
		Level:         "full",
		MatrixSize:    1,
		TestsPassed:   1,
		TestsFailed:   0,
		AuthContext:   pipeline.Phase5AuthContext{Type: "none"},
	})
}

func testSecret(parts ...string) string {
	return strings.Join(parts, "")
}

func TestPublishPackageAllowMirrorDeletionsRequiresDest(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)

	target := filepath.Join(t.TempDir(), "staging")
	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--target", target, "--allow-mirror-deletions", "--json"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--allow-mirror-deletions requires --dest")
}

func TestListMirrorOnlyFiles(t *testing.T) {
	srcDir := t.TempDir()
	mirrorDir := t.TempDir()

	// Files in both.
	for _, p := range []string{"go.mod", "README.md", filepath.Join("internal", "cli", "root.go")} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(srcDir, p)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, p), []byte("src"), 0o644))
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(mirrorDir, p)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(mirrorDir, p), []byte("mirror"), 0o644))
	}

	// Files only in mirror (the deletion risk).
	for _, p := range []string{
		filepath.Join("internal", "source", "resy", "client.go"),
		filepath.Join("internal", "source", "resy", "types.go"),
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(mirrorDir, p)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(mirrorDir, p), []byte("mirror-only"), 0o644))
	}

	// Files only in mirror but under excluded prefixes (should be ignored).
	require.NoError(t, os.MkdirAll(filepath.Join(mirrorDir, ".manuscripts", "20260101-000000", "research"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mirrorDir, ".manuscripts", "20260101-000000", "research", "brief.md"), []byte("old"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(mirrorDir, "build"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mirrorDir, "build", "host.tar.gz"), []byte("artifact"), 0o644))

	// File only in source (not a deletion risk).
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "NEW.md"), []byte("new in source"), 0o644))

	got, err := listMirrorOnlyFiles(mirrorDir, srcDir)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"internal/source/resy/client.go",
		"internal/source/resy/types.go",
	}, got)
}

func TestListMirrorOnlyFilesNothingMissing(t *testing.T) {
	srcDir := t.TempDir()
	mirrorDir := t.TempDir()
	for _, p := range []string{"go.mod", "README.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, p), []byte("src"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(mirrorDir, p), []byte("mirror"), 0o644))
	}

	got, err := listMirrorOnlyFiles(mirrorDir, srcDir)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestListMirrorOnlyFilesTopLevelFileNamedLikeExcludedDir(t *testing.T) {
	srcDir := t.TempDir()
	mirrorDir := t.TempDir()

	// Mirror has a top-level FILE (not directory) literally named
	// .manuscripts. The exclusion targets the .manuscripts/ directory,
	// not files with that name, so this should be reported.
	require.NoError(t, os.WriteFile(filepath.Join(mirrorDir, ".manuscripts"), []byte("not a dir"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(mirrorDir, "build"), []byte("also not a dir"), 0o644))

	got, err := listMirrorOnlyFiles(mirrorDir, srcDir)
	require.NoError(t, err)
	assert.Equal(t, []string{".manuscripts", "build"}, got)
}

func TestPublishPackageDestDivergenceMessageTruncatesAndUsesSingular(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)

	destDir := filepath.Join(t.TempDir(), "publish-repo")
	mirrorCLIDir := filepath.Join(destDir, "library", "other", "test")
	require.NoError(t, os.MkdirAll(mirrorCLIDir, 0o755))

	// Singular case: exactly one mirror-only file.
	soloDir := filepath.Join(mirrorCLIDir, "internal", "extra")
	require.NoError(t, os.MkdirAll(soloDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(soloDir, "only.go"), []byte("package extra\n"), 0o644))

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--dest", destDir, "--json"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mirror has 1 file not present")

	// Truncation case: more mirror-only files than mirrorDivergenceExampleLimit.
	overflowDir := filepath.Join(mirrorCLIDir, "internal", "overflow")
	require.NoError(t, os.MkdirAll(overflowDir, 0o755))
	total := mirrorDivergenceExampleLimit + 3
	for i := range total {
		require.NoError(t, os.WriteFile(filepath.Join(overflowDir, fmt.Sprintf("f%02d.go", i)), []byte("package overflow\n"), 0o644))
	}

	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("mirror has %d files not present", total+1))
	assert.Contains(t, err.Error(), fmt.Sprintf("... and %d more", total+1-mirrorDivergenceExampleLimit))
}

func TestPublishPackageDestRefusesMirrorOnlyContent(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)

	destDir := filepath.Join(t.TempDir(), "publish-repo")
	mirrorCLIDir := filepath.Join(destDir, "library", "other", "test")
	require.NoError(t, os.MkdirAll(mirrorCLIDir, 0o755))

	// Mirror has a package the source does not. Without the guard, the
	// publish overlay would silently delete it.
	resyDir := filepath.Join(mirrorCLIDir, "internal", "source", "resy")
	require.NoError(t, os.MkdirAll(resyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(resyDir, "client.go"), []byte("package resy\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(resyDir, "types.go"), []byte("package resy\n"), 0o644))

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--dest", destDir, "--json"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mirror has 2 files not present in source library")
	assert.Contains(t, err.Error(), "internal/source/resy/client.go")
	assert.Contains(t, err.Error(), "internal/source/resy/types.go")
	assert.Contains(t, err.Error(), "--allow-mirror-deletions")

	// Mirror content must remain intact after refusal.
	_, err = os.Stat(filepath.Join(resyDir, "client.go"))
	assert.NoError(t, err, "mirror content should remain after refusal")
	_, err = os.Stat(mirrorCLIDir + ".old")
	assert.ErrorIs(t, err, os.ErrNotExist, "should not have stashed before failing")
}

func TestPublishPackageDestAllowMirrorDeletionsOverride(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)

	destDir := filepath.Join(t.TempDir(), "publish-repo")
	mirrorCLIDir := filepath.Join(destDir, "library", "other", "test")
	resyDir := filepath.Join(mirrorCLIDir, "internal", "source", "resy")
	require.NoError(t, os.MkdirAll(resyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(resyDir, "client.go"), []byte("package resy\n"), 0o644))

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--dest", destDir, "--allow-mirror-deletions", "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)
	var result PackageResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.Equal(t, filepath.Join(destDir, "library", "other", "test"), result.StagedDir)

	// Mirror-only file is gone after the override.
	_, err = os.Stat(filepath.Join(resyDir, "client.go"))
	assert.ErrorIs(t, err, os.ErrNotExist, "mirror-only file should be deleted with override")
}

func TestPublishPackageDestIgnoresManuscriptsDivergence(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	writePublishableTestCLI(t, cliDir)

	// Create manuscripts for the new run.
	runID := "20260329-100000"
	researchDir := filepath.Join(home, "manuscripts", "test", runID, "research")
	require.NoError(t, os.MkdirAll(researchDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(researchDir, "brief.md"), []byte("# Brief"), 0o644))

	destDir := filepath.Join(t.TempDir(), "publish-repo")
	mirrorCLIDir := filepath.Join(destDir, "library", "other", "test")

	// Mirror has an old manuscripts run and a stale build/ dir. Neither
	// represents real divergence; both are managed by the publish flow.
	oldMSDir := filepath.Join(mirrorCLIDir, ".manuscripts", "20260101-000000", "research")
	require.NoError(t, os.MkdirAll(oldMSDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldMSDir, "brief.md"), []byte("old"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(mirrorCLIDir, "build"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mirrorCLIDir, "build", "host.tar.gz"), []byte("artifact"), 0o644))

	cmd := newPublishCmd()
	cmd.SetArgs([]string{"package", "--dir", cliDir, "--category", "other", "--dest", destDir, "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err, "manuscripts/build divergence should not block the overlay")

	var result PackageResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.True(t, result.ManuscriptsIncluded)
}
