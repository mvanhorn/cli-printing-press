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

		removed, err := removeFunctionsFromFile(path, []string{"deadFunc"})
		require.NoError(t, err)
		assert.Equal(t, []string{"deadFunc"}, removed)

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

		removed, err := removeFunctionsFromFile(path, []string{"dead1", "dead2"})
		require.NoError(t, err)
		assert.Equal(t, []string{"dead1", "dead2"}, removed)

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

		removed, err := removeFunctionsFromFile(path, []string{})
		require.NoError(t, err)
		assert.Empty(t, removed)

		result, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(result), "func liveFunc()")
	})

	t.Run("removes generic functions and keeps methods", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "helpers.go")
		content := `package cli

func unused[T any](v T) T { return v }

func split[
	T any,
](v T) T { return v }

type Box[T any] struct{}

func (b Box[T]) M() {}

func keep() {}
`
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

		removed, err := removeFunctionsFromFile(path, []string{"unused", "split", "M"})
		require.NoError(t, err)
		assert.Equal(t, []string{"unused", "split"}, removed)

		result, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.NotContains(t, string(result), "func unused[")
		assert.NotContains(t, string(result), "func split[")
		assert.Contains(t, string(result), "func (b Box[T]) M()")
		assert.Contains(t, string(result), "func keep()")
	})

	t.Run("leaves the file untouched when nothing matches", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "helpers.go")
		content := []byte("package cli\n\nfunc keep() {}\n")
		require.NoError(t, os.WriteFile(path, content, 0o644))

		removed, err := removeFunctionsFromFile(path, []string{"missing"})
		require.NoError(t, err)
		assert.Empty(t, removed)

		result, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, content, result)
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

		removed, err := removeFunctionsFromFile(path, []string{"deadTopLevel", "Method"})
		require.NoError(t, err)
		assert.Equal(t, []string{"deadTopLevel"}, removed)

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

		removed, err := removeFunctionsFromFile(path, []string{"deadFunc"})
		require.NoError(t, err)
		assert.Equal(t, []string{"deadFunc"}, removed)

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

	t.Run("finds a generic function", func(t *testing.T) {
		generic := filepath.Join(dir, "generic.go")
		require.NoError(t, os.WriteFile(generic, []byte("package cli\n\nfunc unused[T any](v T) T { return v }\n"), 0o644))
		path, found := findFunctionFile(dir, "unused")
		assert.True(t, found)
		assert.True(t, strings.HasSuffix(path, "generic.go"))
	})

	t.Run("finds a multiline generic function", func(t *testing.T) {
		split := filepath.Join(dir, "split.go")
		require.NoError(t, os.WriteFile(split, []byte("package cli\n\nfunc split[\n\tT any,\n](v T) T { return v }\n"), 0o644))
		path, found := findFunctionFile(dir, "split")
		assert.True(t, found)
		assert.True(t, strings.HasSuffix(path, "split.go"))
	})

	t.Run("ignores a comment that only looks like a declaration", func(t *testing.T) {
		comment := filepath.Join(dir, "comment.go")
		require.NoError(t, os.WriteFile(comment, []byte("package cli\n\n// func commented(v int) int\nfunc other() {}\n"), 0o644))
		_, found := findFunctionFile(dir, "commented")
		assert.False(t, found)
	})

	t.Run("does not match a method", func(t *testing.T) {
		method := filepath.Join(dir, "method.go")
		require.NoError(t, os.WriteFile(method, []byte("package cli\n\ntype T struct{}\nfunc (t T) Method() {}\n"), 0o644))
		_, found := findFunctionFile(dir, "Method")
		assert.False(t, found)
	})

	t.Run("skips a file that does not parse", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.go"), []byte("package cli\nfunc (\n"), 0o644))
		path, found := findFunctionFile(dir, "helperFunc")
		assert.True(t, found)
		assert.True(t, strings.HasSuffix(path, "helpers.go"))
	})

	t.Run("returns false when the path is not a directory", func(t *testing.T) {
		_, found := findFunctionFile(helpers, "helperFunc")
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

	t.Run("keeps generated extension point helpers", func(t *testing.T) {
		dir := t.TempDir()

		_ = os.WriteFile(filepath.Join(dir, "helpers.go"), []byte(`package cli

func boundCtx() {}
func writeHarnessRefusal() {}
func novelAuthHeader() {}
func declarePlatformAnalytics() {}
func resolvePlatformWindow() {}
func filterFields() {}
func pathParamSegmentValue() {}
func replaceDependentPathParam() {}
func replaceURLIDPathParam() {}
func resourceURLIDPathParam() {}
func urlIDFieldName() {}

func deadHelper() {}
`), 0o644)

		dead := findAllDeadFunctions(dir)
		assert.Equal(t, []string{"deadHelper"}, dead)
	})

	t.Run("empty directory", func(t *testing.T) {
		dir := t.TempDir()
		dead := findAllDeadFunctions(dir)
		assert.Empty(t, dead)
	})
}
