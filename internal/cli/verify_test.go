package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyCmdHelpDescribesEnvVarLiveMode(t *testing.T) {
	cmd := newVerifyCmd()
	assert.Contains(t, cmd.Long, "--env-var names a non-empty environment variable")
	assert.Contains(t, cmd.Example, "--env-var GITHUB_TOKEN")
	assert.NotContains(t, cmd.Example, "--api-key $GITHUB_TOKEN")
}

func TestPrintVerifyReportIncludesModeDetail(t *testing.T) {
	output, err := runWithCapturedStdout(t, func() error {
		printVerifyReport(&pipeline.VerifyReport{
			Binary:     "sample-cli",
			Mode:       "mock",
			ModeDetail: "--env-var FOO is unset or empty; running in mock mode",
			Verdict:    "PASS",
		})
		return nil
	})
	require.NoError(t, err)
	assert.Contains(t, output, "Mode: mock")
	assert.Contains(t, output, "--env-var FOO is unset or empty; running in mock mode")
}

func TestCleanupVerifyArtifacts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sample-cli")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".cache", "go-build"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "library", "sample-cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sample-cli"), []byte("bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sample-cli-validation"), []byte("bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sample-cli-dogfood"), []byte("bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("finder"), 0o644))

	require.NoError(t, cleanupVerifyArtifacts(dir, true))

	assert.NoFileExists(t, filepath.Join(dir, "sample-cli"))
	assert.NoFileExists(t, filepath.Join(dir, "sample-cli-validation"))
	assert.NoFileExists(t, filepath.Join(dir, "sample-cli-dogfood"))
	assert.NoFileExists(t, filepath.Join(dir, ".DS_Store"))
	assert.NoDirExists(t, filepath.Join(dir, ".cache"))
	assert.NoDirExists(t, filepath.Join(dir, "cmd", "library"))
}

func TestCleanupVerifyArtifacts_NoOpWhenDisabled(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sample-cli")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sample-cli"), []byte("bin"), 0o755))

	require.NoError(t, cleanupVerifyArtifacts(dir, false))

	assert.FileExists(t, filepath.Join(dir, "sample-cli"))
}

func TestVerifyCmdJSONFailReturnsExitErrorAfterWritingReport(t *testing.T) {
	cmd := newVerifyCmdWithOptions(verifyCmdOptions{
		runVerify: func(cfg pipeline.VerifyConfig) (*pipeline.VerifyReport, error) {
			return &pipeline.VerifyReport{
				Mode:     "mock",
				Total:    1,
				Failed:   1,
				PassRate: 0,
				Verdict:  "FAIL",
				Binary:   filepath.Join(cfg.Dir, "sample-cli"),
			}, nil
		},
	})
	cmd.SetArgs([]string{"--dir", t.TempDir(), "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.Error(t, err)

	var exitErr *ExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, ExitGenerationError, exitErr.Code)
	assert.True(t, exitErr.Silent)

	var payload struct {
		Verify pipeline.VerifyReport `json:"verify"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &payload))
	assert.Equal(t, "FAIL", payload.Verify.Verdict)
}

func TestVerifyCmdJSONFixLoopKeepsStdoutPure(t *testing.T) {
	cmd := newVerifyCmdWithOptions(verifyCmdOptions{
		runVerify: func(cfg pipeline.VerifyConfig) (*pipeline.VerifyReport, error) {
			return &pipeline.VerifyReport{
				Mode:     "mock",
				Total:    2,
				Passed:   1,
				Failed:   1,
				PassRate: 50,
				Verdict:  "FAIL",
				Binary:   filepath.Join(cfg.Dir, "sample-cli"),
			}, nil
		},
		runFixLoop: func(cfg pipeline.VerifyConfig, initial *pipeline.VerifyReport, maxIterations int) (*pipeline.FixLoopReport, error) {
			assert.Equal(t, 3, maxIterations)
			final := &pipeline.VerifyReport{
				Mode:     "mock",
				Total:    2,
				Passed:   2,
				Failed:   0,
				PassRate: 100,
				Verdict:  "PASS",
				Binary:   filepath.Join(cfg.Dir, "sample-cli"),
			}
			return &pipeline.FixLoopReport{
				Iterations: []pipeline.FixIteration{{
					Number:     1,
					BeforeRate: initial.PassRate,
					AfterRate:  final.PassRate,
					Delta:      final.PassRate - initial.PassRate,
				}},
				FinalReport: final,
				Improved:    true,
			}, nil
		},
	})
	cmd.SetArgs([]string{"--dir", t.TempDir(), "--json", "--fix"})

	stdout, stderr, err := runWithCapturedStdoutAndStderr(t, cmd.Execute)
	require.NoError(t, err)

	assert.Contains(t, stderr, "Running fix loop")
	assert.NotContains(t, stdout, "Running fix loop")

	var payload struct {
		Verify  pipeline.VerifyReport   `json:"verify"`
		FixLoop *pipeline.FixLoopReport `json:"fix_loop"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
	assert.Equal(t, "PASS", payload.Verify.Verdict)
	require.NotNil(t, payload.FixLoop)
	assert.True(t, payload.FixLoop.Improved)
	assert.Equal(t, 1, len(payload.FixLoop.Iterations))
}

func TestVerifyCmdJSONFailSilencesRootCobraError(t *testing.T) {
	verifyCmd := newVerifyCmdWithOptions(verifyCmdOptions{
		runVerify: func(cfg pipeline.VerifyConfig) (*pipeline.VerifyReport, error) {
			return &pipeline.VerifyReport{
				Mode:     "mock",
				Total:    1,
				Failed:   1,
				PassRate: 0,
				Verdict:  "FAIL",
				Binary:   filepath.Join(cfg.Dir, "sample-cli"),
			}, nil
		},
	})
	root := &cobra.Command{Use: "printing-press", SilenceUsage: true}
	var stderr bytes.Buffer
	root.SetErr(&stderr)
	root.AddCommand(verifyCmd)
	root.SetArgs([]string{"verify", "--dir", t.TempDir(), "--json"})

	output, err := runWithCapturedStdout(t, root.Execute)
	require.Error(t, err)
	assert.Empty(t, stderr.String())

	var exitErr *ExitError
	require.True(t, errors.As(err, &exitErr))
	assert.True(t, exitErr.Silent)

	var payload struct {
		Verify pipeline.VerifyReport `json:"verify"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &payload))
	assert.Equal(t, "FAIL", payload.Verify.Verdict)
}

func TestVerifyCmdTextFailExitsWithLegacyCode(t *testing.T) {
	var exitCode *int
	cmd := newVerifyCmdWithOptions(verifyCmdOptions{
		runVerify: func(cfg pipeline.VerifyConfig) (*pipeline.VerifyReport, error) {
			return &pipeline.VerifyReport{
				Mode:     "mock",
				Total:    1,
				Failed:   1,
				PassRate: 0,
				Verdict:  "FAIL",
				Binary:   filepath.Join(cfg.Dir, "sample-cli"),
			}, nil
		},
		exitProcess: func(code int) {
			exitCode = &code
		},
	})
	cmd.SetArgs([]string{"--dir", t.TempDir()})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)
	require.NotNil(t, exitCode)
	assert.Equal(t, 1, *exitCode)
	assert.Contains(t, output, "Verdict: FAIL")
}

func TestVerifyCmdNormalizesRelativeDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var gotDir string
	cmd := newVerifyCmdWithOptions(verifyCmdOptions{
		runVerify: func(cfg pipeline.VerifyConfig) (*pipeline.VerifyReport, error) {
			gotDir = cfg.Dir
			return &pipeline.VerifyReport{
				Mode:     "mock",
				Total:    1,
				Passed:   1,
				PassRate: 100,
				Verdict:  "PASS",
				Binary:   filepath.Join(cfg.Dir, "sample-cli"),
			}, nil
		},
	})
	cmd.SetArgs([]string{"--dir", "."})

	_, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)
	assert.Equal(t, dir, gotDir)
}

func TestVerifyCmdWriteManifestPersistsSummary(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, pipeline.CLIManifestFilename)
	require.NoError(t, os.WriteFile(manifestPath, []byte(`{"schema_version":1,"api_name":"sample"}`+"\n"), 0o644))

	cmd := newVerifyCmdWithOptions(verifyCmdOptions{
		runVerify: func(cfg pipeline.VerifyConfig) (*pipeline.VerifyReport, error) {
			return &pipeline.VerifyReport{
				Mode:     "mock",
				Total:    4,
				Passed:   3,
				Failed:   1,
				PassRate: 75,
				Verdict:  "WARN",
				Binary:   filepath.Join(cfg.Dir, "sample-cli"),
			}, nil
		},
	})
	cmd.SetArgs([]string{"--dir", dir, "--write-manifest", manifestPath, "--json"})

	_, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	data, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	verify := raw["verify"].(map[string]any)
	assert.Equal(t, float64(75), verify["pass_rate"])
	assert.Equal(t, "WARN", verify["verdict"])
}

func TestVerifyCmdWriteManifestErrorUsesGenerationExitCode(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, pipeline.CLIManifestFilename)
	require.NoError(t, os.WriteFile(manifestPath, []byte(`not-json`), 0o644))

	cmd := newVerifyCmdWithOptions(verifyCmdOptions{
		runVerify: func(cfg pipeline.VerifyConfig) (*pipeline.VerifyReport, error) {
			return &pipeline.VerifyReport{
				Mode:     "mock",
				Total:    1,
				Passed:   1,
				PassRate: 100,
				Verdict:  "PASS",
				Binary:   filepath.Join(cfg.Dir, "sample-cli"),
			}, nil
		},
	})
	cmd.SetArgs([]string{"--dir", dir, "--write-manifest", manifestPath})

	_, err := runWithCapturedStdout(t, cmd.Execute)
	require.Error(t, err)

	var exitErr *ExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, ExitGenerationError, exitErr.Code)
	assert.Contains(t, exitErr.Error(), "writing verify summary to manifest")
}
