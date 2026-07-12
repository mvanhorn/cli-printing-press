package generator

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedNestedHelpExitsZeroAndUsageErrorsExitTwo(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("help-exit")
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Manage items",
		Endpoints: map[string]spec.Endpoint{
			"list": {Method: "GET", Path: "/items", Description: "List items"},
			"compare": {
				Method:      "GET",
				Path:        "/items/{left_id}/compare/{right_id}",
				Description: "Compare two items",
				Params: []spec.Param{
					{Name: "left_id", Type: "string", Required: true, Positional: true, PathParam: true},
					{Name: "right_id", Type: "string", Required: true, Positional: true, PathParam: true},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "help-exit-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	binPath := filepath.Join(outputDir, "help-exit-pp-cli")
	runGoCommandRequired(t, outputDir, "build", "-o", "./help-exit-pp-cli", "./cmd/help-exit-pp-cli")

	assertExitCode(t, 0, binPath, "items", "list", "--help")
	assertExitCode(t, 0, binPath, "auth", "status", "--help")
	assertExitCode(t, 2, binPath, "--bogus-flag")
	assertExitCode(t, 2, binPath, "items", "compare", "left-only", "--json")
}

func TestGeneratedRootTreatsPflagHelpSentinelAsSuccess(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("help-sentinel")
	outputDir := filepath.Join(t.TempDir(), "help-sentinel-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	goMod := readGeneratedFile(t, outputDir, "go.mod")
	require.Contains(t, rootSrc, `"errors"`)
	require.Contains(t, rootSrc, `"github.com/spf13/pflag"`)
	require.Contains(t, rootSrc, "errors.Is(err, pflag.ErrHelp)")
	require.Contains(t, goMod, "github.com/spf13/pflag v1.0.6")
	require.Less(t,
		strings.Index(rootSrc, "errors.Is(err, pflag.ErrHelp)"),
		strings.Index(rootSrc, "isCobraUsageError(err)"),
		"help sentinel must be handled before Cobra usage errors are wrapped as exit code 2",
	)
}

func assertExitCode(t *testing.T, want int, binaryPath string, args ...string) {
	t.Helper()

	cmd := exec.Command(binaryPath, args...)
	output, err := cmd.CombinedOutput()
	if want == 0 {
		require.NoError(t, err, "args %v output:\n%s", args, string(output))
		return
	}
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "args %v output:\n%s", args, string(output))
	require.Equal(t, want, exitErr.ExitCode(), "args %v output:\n%s", args, string(output))
}
