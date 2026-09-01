package generator

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

var testGoRuntimeVersionRE = regexp.MustCompile(`go([0-9]+\.[0-9]+)(?:\.([0-9]+))?`)

func TestSelectEmittedGoDirectiveIgnoresHostToolchain(t *testing.T) {
	t.Parallel()

	// Older printers froze stdlib CVEs; newer printers (and release binaries
	// built ahead of CI) exceeded library GOTOOLCHAIN=local.
	hosts := []string{
		"go1.26.5",
		"go1.26.6",
		"go1.26.7",
		"go1.26",
		"go1.27.0",
		"devel go1.27-abc123",
		"go1.26.5 X:cacheprog",
		runtime.Version(),
	}
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, librarySafeGoDirective, selectEmittedGoDirective(host))
		})
	}
}

func TestLibrarySafeGoDirectiveShape(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1.26.6", librarySafeGoDirective)
	assert.Regexp(t, `^\d+\.\d+\.\d+$`, librarySafeGoDirective)
	assert.GreaterOrEqual(t, semver.Compare("v"+librarySafeGoDirective, "v1.26.6"), 0)
}

func TestLibrarySafeGoDirectiveDoesNotExceedPressGoMod(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../go.mod")
	require.NoError(t, err)
	mod, err := modfile.Parse("go.mod", data, nil)
	require.NoError(t, err)
	require.NotNil(t, mod.Go)
	assert.LessOrEqual(t, semver.Compare("v"+librarySafeGoDirective, "v"+mod.Go.Version), 0,
		"printed CLIs must not demand a newer Go than the Printing Press itself")
}

func TestGeneratedGoModUsesLibrarySafeFloor(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("library-safe-directive")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	goMod := readGeneratedFile(t, outputDir, "go.mod")
	assert.Contains(t, goMod, "\ngo "+librarySafeGoDirective+"\n")
	assert.Contains(t, goMod, "\ntoolchain go"+librarySafeGoDirective+"\n")
	assert.NotContains(t, goMod, "\ngo 1.26\n")
	assert.NotContains(t, goMod, "\ngo 1.26.5\n")
	assert.NotContains(t, goMod, "\ngo 1.26.7\n")
	assert.NotContains(t, goMod, "\ntoolchain go1.26.5\n")
	assert.NotContains(t, goMod, "\ntoolchain go1.26.7\n")

	if copied := goPatchFromRuntime(runtime.Version()); copied != "" && copied != librarySafeGoDirective {
		assert.NotContains(t, goMod, "\ngo "+copied+"\n")
		assert.NotContains(t, goMod, "\ntoolchain go"+copied+"\n")
	}

	requireGeneratedCompiles(t, outputDir)
}

func TestResolveGoDirectiveVersionIsTheFloor(t *testing.T) {
	t.Parallel()

	got, err := resolveCurrentGoDirectiveVersion()
	require.NoError(t, err)
	assert.Equal(t, librarySafeGoDirective, got)
	assert.Equal(t, librarySafeGoDirective, currentGoDirectiveVersion())
	assert.Equal(t, "go"+librarySafeGoDirective, currentGoToolchainVersion())
	toolchain, err := resolveCurrentGoToolchainVersion()
	require.NoError(t, err)
	assert.Equal(t, "go"+librarySafeGoDirective, toolchain)
}

func goPatchFromRuntime(version string) string {
	match := testGoRuntimeVersionRE.FindStringSubmatch(version)
	if match == nil {
		return ""
	}
	if match[2] == "" {
		return match[1] + ".0"
	}
	return match[1] + "." + match[2]
}
