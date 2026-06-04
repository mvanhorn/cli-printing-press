package generator

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionCommandUsesRootNameInGeneratedRoot(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("version-cmd-name")
	outputDir := filepath.Join(t.TempDir(), "version-cmd-name-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	// The version command lives in the shared internal/cli/version.go file, emitted
	// from version.go.tmpl by both the HTTP and device generators.
	versionPath := filepath.Join(outputDir, "internal", "cli", "version.go")
	content, err := os.ReadFile(versionPath)
	require.NoError(t, err)
	src := string(content)

	re := regexp.MustCompile(`(?s)func newVersionCmd\(\) \*cobra.Command \{.*?\n\}`)
	fn := re.FindString(src)
	require.NotEmpty(t, fn, "expected generated version.go to contain newVersionCmd")

	require.Contains(t, fn, `fmt.Printf("%s %s\n", cmd.Root().Name(), version)`,
		"newVersionCmd must print the root command name at runtime")
	require.NotContains(t, fn, `fmt.Printf("version-cmd-name-pp-cli %s\n", version)`,
		"newVersionCmd must not hardcode the generated binary literal")
}
