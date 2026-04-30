package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v3/internal/pipeline"
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
	assert.Contains(t, manifestCheck.Error, "required fields")
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
	expectedChecks := []string{"manifest", "transcendence", "go mod tidy", "go vet", "go build", "--help", "--version", "verify-skill", "manuscripts"}
	for _, name := range expectedChecks {
		assert.True(t, checkNames[name], "should have %q check", name)
	}
	assert.Len(t, result.Checks, len(expectedChecks), "should have exactly the expected checks")
}

func TestPublishValidateRequiresTranscendenceFeatures(t *testing.T) {
	home := setLibraryTestEnv(t)
	cliDir := filepath.Join(home, "library", "test-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	data, err := json.MarshalIndent(pipeline.CLIManifest{
		SchemaVersion: 1,
		APIName:       "test",
		CLIName:       "test-pp-cli",
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

func TestPublishRenameAPINameFallback(t *testing.T) {
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
	// No --api-name flag — should fall back to TrimCLISuffix("test-pp-cli") = "test"
	cmd.SetArgs([]string{"rename", "--dir", cliDir, "--old-name", oldName, "--new-name", newName, "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var result RenameResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.True(t, result.Success)

	// Verify manifest has correct api_name from fallback
	newDir := filepath.Join(root, naming.LibraryDirName(newName))
	mData, err := os.ReadFile(filepath.Join(newDir, pipeline.CLIManifestFilename))
	require.NoError(t, err)
	var m pipeline.CLIManifest
	require.NoError(t, json.Unmarshal(mData, &m))
	assert.Equal(t, "test", m.APIName, "api_name should come from TrimCLISuffix fallback")
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Test CLI\n\n## Command Reference\n\n- `test-pp-cli insight` — Show test insight\n\n## Usage\n\n```bash\ntest-pp-cli insight --agent\n```\n"), 0o644))

	writeTestManifest(t, dir, pipeline.CLIManifest{
		SchemaVersion: 1,
		APIName:       "test",
		CLIName:       "test-pp-cli",
	})
}
