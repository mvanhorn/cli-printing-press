package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveFunctionsFromFile(t *testing.T) {
	t.Run("removes dead function and preserves live ones", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "helpers.go")

		content := `package cli

import "fmt"

func liveFunc() string {
	return "I am used"
}

func deadFunc() string {
	return "nobody calls me"
}

func anotherLive() {
	fmt.Println(liveFunc())
}
`
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

		err := removeFunctionsFromFile(path, []string{"deadFunc"})
		require.NoError(t, err)

		result, err := os.ReadFile(path)
		require.NoError(t, err)

		assert.Contains(t, string(result), "func liveFunc()")
		assert.Contains(t, string(result), "func anotherLive()")
		assert.NotContains(t, string(result), "func deadFunc()")
		assert.NotContains(t, string(result), "nobody calls me")
	})

	t.Run("removes multiple dead functions", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "helpers.go")

		content := `package cli

func keepMe() string { return "keep" }

func dead1() string { return "dead" }

func dead2() int { return 0 }

func alsoKeep() string { return keepMe() }
`
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

		err := removeFunctionsFromFile(path, []string{"dead1", "dead2"})
		require.NoError(t, err)

		result, err := os.ReadFile(path)
		require.NoError(t, err)

		assert.Contains(t, string(result), "func keepMe()")
		assert.Contains(t, string(result), "func alsoKeep()")
		assert.NotContains(t, string(result), "func dead1()")
		assert.NotContains(t, string(result), "func dead2()")
	})

	t.Run("no-op when no dead functions", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "helpers.go")

		content := `package cli

func liveFunc() string { return "alive" }
`
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

		err := removeFunctionsFromFile(path, []string{})
		require.NoError(t, err)

		result, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(result), "func liveFunc()")
	})

	t.Run("skips method receivers", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "helpers.go")

		content := `package cli

type Foo struct{}

func (f *Foo) Method() string { return "method" }

func deadTopLevel() string { return "dead" }
`
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

		err := removeFunctionsFromFile(path, []string{"deadTopLevel", "Method"})
		require.NoError(t, err)

		result, err := os.ReadFile(path)
		require.NoError(t, err)

		assert.NotContains(t, string(result), "func deadTopLevel()")
		assert.Contains(t, string(result), "func (f *Foo) Method()")
	})

	t.Run("removes doc comments with dead function", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "helpers.go")

		content := `package cli

// liveFunc does something useful.
func liveFunc() string { return "alive" }

// deadFunc is no longer needed.
// It used to do something important.
func deadFunc() string { return "dead" }
`
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

		err := removeFunctionsFromFile(path, []string{"deadFunc"})
		require.NoError(t, err)

		result, err := os.ReadFile(path)
		require.NoError(t, err)

		assert.Contains(t, string(result), "// liveFunc does something useful.")
		assert.NotContains(t, string(result), "// deadFunc is no longer needed.")
		assert.NotContains(t, string(result), "It used to do something important.")
		assert.NotContains(t, string(result), "func deadFunc()")
	})
}

func TestFindFunctionFile(t *testing.T) {
	dir := t.TempDir()

	helpers := filepath.Join(dir, "helpers.go")
	_ = os.WriteFile(helpers, []byte("package cli\n\nfunc helperFunc() {}\n"), 0o644)

	commands := filepath.Join(dir, "commands.go")
	_ = os.WriteFile(commands, []byte("package cli\n\nfunc commandFunc() {}\n"), 0o644)

	t.Run("finds function in helpers.go", func(t *testing.T) {
		path, found := findFunctionFile(dir, "helperFunc")
		assert.True(t, found)
		assert.True(t, strings.HasSuffix(path, "helpers.go"))
	})

	t.Run("finds function in other file", func(t *testing.T) {
		path, found := findFunctionFile(dir, "commandFunc")
		assert.True(t, found)
		assert.True(t, strings.HasSuffix(path, "commands.go"))
	})

	t.Run("returns false for missing function", func(t *testing.T) {
		_, found := findFunctionFile(dir, "nonexistent")
		assert.False(t, found)
	})
}

func TestFindAllDeadFunctions(t *testing.T) {
	t.Run("finds dead functions across multiple files", func(t *testing.T) {
		dir := t.TempDir()

		// helpers.go: deadHelper is defined but never called anywhere
		_ = os.WriteFile(filepath.Join(dir, "helpers.go"), []byte(`package cli

func liveHelper() string { return "used" }

func deadHelper() string { return "unused" }
`), 0o644)

		// command.go: calls liveHelper (making it live), defines deadCommand (never called)
		_ = os.WriteFile(filepath.Join(dir, "command.go"), []byte(`package cli

func init() {
	runCommand()
}

func runCommand() {
	liveHelper()
}

func deadCommand() string { return "also unused" }
`), 0o644)

		dead := findAllDeadFunctions(dir)

		assert.Contains(t, dead, "deadHelper")
		assert.Contains(t, dead, "deadCommand")
		assert.NotContains(t, dead, "liveHelper")
		assert.NotContains(t, dead, "runCommand")
	})

	t.Run("no dead functions when all are called", func(t *testing.T) {
		dir := t.TempDir()

		_ = os.WriteFile(filepath.Join(dir, "a.go"), []byte(`package cli

func main() { funcA() }

func funcA() { funcB() }
`), 0o644)

		_ = os.WriteFile(filepath.Join(dir, "b.go"), []byte(`package cli

func funcB() { funcA() }
`), 0o644)

		dead := findAllDeadFunctions(dir)
		assert.Empty(t, dead)
	})

	t.Run("empty directory", func(t *testing.T) {
		dir := t.TempDir()
		dead := findAllDeadFunctions(dir)
		assert.Empty(t, dead)
	})
}
