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

	rootPath := filepath.Join(outputDir, "internal", "cli", "root.go")
	content, err := os.ReadFile(rootPath)
	require.NoError(t, err)
	src := string(content)

	re := regexp.MustCompile(`(?s)func newVersionCliCmd\(\) \*cobra.Command \{.*?\n\}`)
	fn := re.FindString(src)
	require.NotEmpty(t, fn, "expected generated root.go to contain newVersionCliCmd")

	require.Contains(t, fn, `fmt.Printf("%s %s\n", cmd.Root().Name(), version)`,
		"newVersionCliCmd must print the root command name at runtime")
	require.NotContains(t, fn, `fmt.Printf("version-cmd-name-pp-cli %s\n", version)`,
		"newVersionCliCmd must not hardcode the generated binary literal")
}
