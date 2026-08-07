package generator

import (
	"os/exec"
	"path/filepath"
	"runtime"
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
			"find": {
				Method:      "GET",
				Path:        "/items/find",
				Description: "Find an item",
				Params: []spec.Param{
					{Name: "query", Type: "string", Required: true},
				},
			},
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
	apiSpec.Resources["subscribe"] = spec.Resource{
		Description: "Manage subscriptions",
		Endpoints: map[string]spec.Endpoint{
			"create": {
				Method:      "POST",
				Path:        "/subscribe",
				Description: "Create a subscription",
				Body: []spec.Param{
					{Name: "email", Type: "string", Required: true},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "help-exit-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	binPath := filepath.Join(outputDir, "help-exit-pp-cli"+exe)
	runGoCommandRequired(t, outputDir, "build", "-o", "./help-exit-pp-cli"+exe, "./cmd/help-exit-pp-cli")

	assertExitCode(t, 0, binPath, "items", "list", "--help")
	assertExitCode(t, 0, binPath, "auth", "status", "--help")
	assertExitCode(t, 2, binPath, "--bogus-flag")
	assertExitCode(t, 2, binPath, "items", "compare", "left-only", "--json")

	// A missing required positional is a usage error (exit 2) in every output
	// mode, human included — not exit-0 help. Before the fix a bare leaf
	// invocation fell through to cobra help and exited 0, so agents read a
	// usage error as a binary bug (#3632). --json adds a structured envelope on
	// stdout; the exit code is 2 either way.
	humanMissing := assertExitCode(t, 2, binPath, "items", "compare")
	require.Contains(t, humanMissing, "missing required argument")
	jsonMissing := assertExitCode(t, 2, binPath, "items", "compare", "--json")
	require.Contains(t, jsonMissing, `"missing required argument"`)

	for _, args := range [][]string{
		{"items", "find", "--json"},
		{"items", "find", "--agent"},
		{"subscribe", "--json"},
		{"subscribe", "--agent"},
	} {
		missingInput := assertExitCode(t, 2, binPath, args...)
		require.Contains(t, missingInput, `"requires input"`, "args %v output:\n%s", args, missingInput)
	}

	// An unknown/misspelled subcommand on a parent group is a usage error in
	// every output mode. Before the fix human mode fell through to exit-0 help
	// (#2955); machine mode already exited 2.
	humanUnknown := assertExitCode(t, 2, binPath, "items", "bogus")
	require.Contains(t, humanUnknown, `unknown subcommand "bogus"`)
	jsonUnknown := assertExitCode(t, 2, binPath, "items", "bogus", "--json")
	require.Contains(t, jsonUnknown, `"unknown subcommand"`)

	// A genuine bare parent invocation still prints help and exits 0 for humans
	// (only a leftover/typo'd token is an error), preserving the friendly UX.
	assertExitCode(t, 0, binPath, "items")
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

func assertExitCode(t *testing.T, want int, binaryPath string, args ...string) string {
	t.Helper()

	cmd := exec.Command(binaryPath, args...)
	output, err := cmd.CombinedOutput()
	if want == 0 {
		require.NoError(t, err, "args %v output:\n%s", args, string(output))
		return string(output)
	}
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "args %v output:\n%s", args, string(output))
	require.Equal(t, want, exitErr.ExitCode(), "args %v output:\n%s", args, string(output))
	return string(output)
}
