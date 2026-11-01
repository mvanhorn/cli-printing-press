package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncUses_ReferenceShapes(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "helpers.go"), []byte(`package cli

func init() { use(); fromOther() }

func use() {
	useArgs(liveArg)
	var f func() = liveVar
	_ = f
	m := map[string]func(){"a": liveMap}
	_ = m
	_ = give()
	liveSameFile()
	liveSplit(
	)
	liveGeneric(1)
	myhelper()
	helperExtra()
	_ = box{helper: 1}
	t := T{}
	t.helper()
	mv := t.helper
	_ = mv
	shadowed()
}

func useArgs(fn func()) { fn() }

func give() func() { return liveReturn }

func liveArg()           {}
func liveVar()           {}
func liveMap()           {}
func liveReturn()        {}
func liveSameFile()      {}
func liveSplit()         {}
func liveCrossFile()     {}
func liveFromMethod()    {}
func liveFromAllowlisted() {}
func liveGeneric[T any](v T) T { return v }
func trulyDead()         {}
func helper()            {}
func Extra()             {}
func myhelper()          {}
func helperExtra()       {}

type T struct{}
type box struct{ helper int }

func (t T) M() { liveFromMethod() }
func (t T) helper() {}

func paginatedGet() { liveFromAllowlisted() }

func shadowed() {
	trulyDead := 1
	_ = trulyDead
}

// trulyDead() is named only in this comment.
const mention = "trulyDead()"
`), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "other.go"), []byte(`package cli

func fromOther() { liveCrossFile() }
`), 0o644))

	wantDead := []string{"Extra", "helper", "trulyDead"}
	assert.Equal(t, wantDead, findAllDeadFunctions(cliDir))

	got := checkDeadFunctions(dir)
	assert.Equal(t, wantDead, got.Items)
	assert.Equal(t, len(wantDead), got.Dead)
	assert.NotContains(t, got.Items, "liveArg")
	assert.NotContains(t, got.Items, "liveFromMethod")
	assert.NotContains(t, got.Items, "liveFromAllowlisted")
	assert.NotContains(t, got.Items, "paginatedGet")
}

func TestFuncUses_ValueRemovedIsDead(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "helpers.go"), []byte(`package cli

func init() { use() }

func use() { apply(helper) }

func apply(fn func()) { fn() }

func helper() {}
`), 0o644))

	assert.NotContains(t, findAllDeadFunctions(dir), "helper")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "helpers.go"), []byte(`package cli

func init() { use() }

func use() {}

func apply(fn func()) { fn() }

func helper() {}
`), 0o644))
	assert.Contains(t, findAllDeadFunctions(dir), "helper")
}

func TestFuncUses_CrossPackageSelectors(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	cmdDir := filepath.Join(dir, "cmd", "tool")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	require.NoError(t, os.MkdirAll(cmdDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "helpers.go"), []byte(`package cli

func ExitCode() int { return 0 }
func Helper() {}
func dead() {}
`), 0o644))

	t.Run("alias import", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(`package main

import appcli "example.com/acme/internal/cli"

func main() { appcli.ExitCode() }
`), 0o644))
		dead := findAllDeadFunctions(cliDir, filepath.Join(dir, "cmd"))
		assert.NotContains(t, dead, "ExitCode")
		assert.Contains(t, dead, "Helper")
		assert.Contains(t, dead, "dead")
	})

	t.Run("dot import", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(`package main

import . "example.com/acme/internal/cli"

func main() { ExitCode() }
`), 0o644))
		dead := findAllDeadFunctions(cliDir, filepath.Join(dir, "cmd"))
		assert.NotContains(t, dead, "ExitCode")
		assert.Contains(t, dead, "Helper")
	})

	t.Run("unresolved package selector", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(`package main

func main() { cli.ExitCode() }
`), 0o644))
		assert.NotContains(t, findAllDeadFunctions(cliDir, filepath.Join(dir, "cmd")), "ExitCode")
	})

	t.Run("different package path does not count", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(`package main

import "example.com/acme/internal/cliutil"

func main() { cliutil.Helper() }
`), 0o644))
		dead := findAllDeadFunctions(cliDir, filepath.Join(dir, "cmd"))
		assert.Contains(t, dead, "Helper")
		assert.Contains(t, dead, "ExitCode")
	})

	t.Run("dot import selector is not a function use", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(`package main

import . "example.com/acme/internal/cli"

func main() {
	var obj struct{ ExitCode int }
	_ = obj.ExitCode
}
`), 0o644))
		assert.Contains(t, findAllDeadFunctions(cliDir, filepath.Join(dir, "cmd")), "ExitCode")
	})

	t.Run("one-element import path does not match", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(`package main

import "cli"

func main() { cli.Helper() }
`), 0o644))
		dead := findAllDeadFunctions(cliDir, filepath.Join(dir, "cmd"))
		assert.Contains(t, dead, "Helper")
		assert.Contains(t, dead, "ExitCode")
	})
}

func TestFuncUses_MissingFileAddsNoCandidate(t *testing.T) {
	dir := t.TempDir()
	candidates, dead := scanFuncUses([]string{filepath.Join(dir, "missing.go")}, nil)
	assert.Empty(t, candidates)
	assert.Empty(t, dead)
}

func TestFuncUses_UnparsableUseFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "helpers.go"), []byte(`package cli

func live() {}
func dead() {}
`), 0o644))

	t.Run("mention keeps the function live", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.go"), []byte("package cli\nfunc ( { live\n"), 0o644))
		dead := findAllDeadFunctions(dir)
		assert.NotContains(t, dead, "live")
		assert.Contains(t, dead, "dead")
	})

	t.Run("no mention stays dead", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.go"), []byte("package cli\nfunc ( {\n"), 0o644))
		dead := findAllDeadFunctions(dir)
		assert.Contains(t, dead, "live")
		assert.Contains(t, dead, "dead")
	})

	t.Run("longer name is not a mention", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.go"), []byte("package cli\nfunc ( { liveExtra\n"), 0o644))
		assert.Contains(t, findAllDeadFunctions(dir), "live")
	})
}

func TestFuncUses_UnparsableDefFileAddsNoCandidate(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "helpers.go"), []byte("package cli\nfunc ( {\n"), 0o644))
	assert.Empty(t, findAllDeadFunctions(dir))
}

func TestRemoveDeadCode_RemovesPlantedDeadKeepsFunctionValue(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "app"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/acme\n\ngo 1.22\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "helpers.go"), []byte(`package cli

func init() { keep() }

func keep() { apply(liveValue) }

func apply(fn func()) { fn() }

func liveValue() {}

type T struct{}

func (t T) M() { fromMethod() }

func fromMethod() {}

func plantedDead() { println("nobody calls me") }
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", "app", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))

	result, err := RemoveDeadCode(dir, false)
	require.NoError(t, err)
	require.True(t, result.BuildVerified, "build_error=%s", result.BuildError)
	assert.Empty(t, result.Restored)
	assert.Equal(t, []string{"plantedDead"}, result.Removed)
	assert.NotContains(t, result.Removed, "liveValue")
	assert.NotContains(t, result.Removed, "fromMethod")

	src, err := os.ReadFile(filepath.Join(cliDir, "helpers.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(src), "func plantedDead(")
	assert.Contains(t, string(src), "func liveValue(")
	assert.Contains(t, string(src), "func fromMethod(")
	assert.Contains(t, string(src), "apply(liveValue)")
}

func TestScoreDeadCode_ValueAndDistinctNames(t *testing.T) {
	dir := t.TempDir()
	writeScorecardFixture(t, dir, "internal/cli/root.go", "package cli\n")
	writeScorecardFixture(t, dir, "internal/cli/helpers.go", `package cli

func helper() {}
func myhelper() {}
func helperExtra() {}
func Extra() {}
func onlyAsValue() {}

func use() {
	myhelper()
	helperExtra()
	fns := map[string]func(){"a": onlyAsValue}
	_ = fns
}

func init() { use() }
`)
	assert.Equal(t, 3, scoreDeadCode(dir))
}

func TestGoldenCLI_WriteThroughDecoderAndDryRunHelperAreLive(t *testing.T) {
	golden := filepath.Join("..", "..", "testdata", "golden", "expected", "generate-golden-api", "printing-press-golden")
	cliDir := filepath.Join(golden, "internal", "cli")
	src, err := os.ReadFile(filepath.Join(cliDir, "data_source.go"))
	require.NoError(t, err)
	require.Contains(t, string(src), "func decodeWriteThroughNonEmptyArray(")
	useFiles := append(listGoFiles(cliDir), listGoTestFiles(cliDir)...)
	useFiles = append(useFiles, listGoFilesRecursive(filepath.Join(golden, "cmd"))...)
	useFiles = append(useFiles, listGoFilesRecursive(filepath.Join(golden, "internal", "mcp"))...)
	candidates, dead := scanFuncUses(listGoFiles(cliDir), useFiles)
	require.Contains(t, candidates, "decodeWriteThroughNonEmptyArray")
	assert.NotContains(t, dead, "decodeWriteThroughNonEmptyArray")
	require.Contains(t, candidates, "isDryRunResponseForClient")
	assert.NotContains(t, dead, "isDryRunResponseForClient")

	// This fixture's only calls are in helpers.go, inside a method and paginatedGet.
	publicParam := filepath.Join("..", "..", "testdata", "golden", "expected", "generate-public-param-names", "public-param-golden")
	helpers, err := os.ReadFile(filepath.Join(publicParam, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	require.Contains(t, string(helpers), "func isDryRunResponseForClient(")
	got := checkDeadFunctions(publicParam)
	require.NotZero(t, got.Total)
	assert.NotContains(t, got.Items, "isDryRunResponseForClient")
}
