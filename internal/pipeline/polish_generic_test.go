package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeadFuncLocs_ReportsFileAndSkipsLiveOrAllowed(t *testing.T) {
	funcs := []definedFunc{
		{key: "live", file: "a.go", name: "helper"},
		{key: "twin", file: "b.go", name: "helper"},
		{key: "a1", file: "a.go", name: "alpha"},
		{key: "a2", file: "b.go", name: "alpha"},
		{key: "dead", file: "c.go", name: "unused"},
		{key: "dup", file: "c.go", name: "unused"},
		{key: "z", file: "z.go", name: "zeta"},
		{key: "main", file: "d.go", name: "main"},
	}
	locs := deadFuncLocs(funcs, map[string]struct{}{"live": {}})
	assert.Equal(t, []deadFuncLoc{
		{name: "alpha", file: "a.go"},
		{name: "alpha", file: "b.go"},
		{name: "unused", file: "c.go"},
		{name: "zeta", file: "z.go"},
	}, locs)

	assert.Nil(t, deadFuncLocs([]definedFunc{{key: "m", file: "a.go", name: "main"}}, map[string]struct{}{}))
	assert.Nil(t, deadFuncLocs(nil, nil))
}

func TestDeadFuncLocs_NilReferencedStillReports(t *testing.T) {
	locs := deadFuncLocs([]definedFunc{{key: "k", file: "a.go", name: "unused"}}, nil)
	assert.Equal(t, []deadFuncLoc{{name: "unused", file: "a.go"}}, locs)
}

func TestFindDeadFuncLocs_GenericFileAndDistinctPackages(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte(`package cli

func init() { live() }

func live() {}

func deadGeneric[T any](v T) T { return v }
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), []byte(`package other

func dead() {}
`), 0o644))

	locs := findDeadFuncLocs(dir)
	require.Len(t, locs, 2)
	assert.Equal(t, "dead", locs[0].name)
	assert.Equal(t, "b.go", filepath.Base(locs[0].file))
	assert.Equal(t, "deadGeneric", locs[1].name)
	assert.Equal(t, "a.go", filepath.Base(locs[1].file))
	assert.Equal(t, []string{"dead", "deadGeneric"}, deadFuncNames(locs))
}

func TestUnremovedDeadFuncsError(t *testing.T) {
	assert.Equal(t, "dead functions were not removed", unremovedDeadFuncsError(nil))
	assert.Equal(t, "dead functions were not removed", unremovedDeadFuncsError([]string{"", ""}))
	assert.Equal(t, "dead functions not removed: alpha, zeta", unremovedDeadFuncsError([]string{"zeta", "alpha", "alpha", ""}))
}

func TestRemoveDeadLocs_SkipsUnlocatedAndUnreadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "helpers.go")
	require.NoError(t, os.WriteFile(path, []byte(`package cli

type T struct{}

func (t T) Method() {}

func locatedDead() {}
`), 0o644))

	removed, skipped, backups, err := removeDeadLocs([]deadFuncLoc{
		{name: "ghost", file: ""},
		{name: "missing", file: filepath.Join(dir, "nope.go")},
		{name: "Method", file: path},
		{name: "locatedDead", file: path},
		{name: "notHere", file: path},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"locatedDead"}, removed)
	assert.Equal(t, []string{"ghost", "missing", "Method", "notHere"}, skipped)
	assert.Contains(t, backups, path)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(got), "func locatedDead(")
	assert.Contains(t, string(got), "func (t T) Method()")
}

func TestRemoveDeadLocs_ParseErrorRestoresEarlierFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.go")
	bad := filepath.Join(dir, "bad.go")
	original := []byte("package cli\n\nfunc locatedDead() { println(\"sentinel\") }\n")
	badSrc := []byte("package cli\n\nfunc (\n")
	require.NoError(t, os.WriteFile(good, original, 0o644))
	require.NoError(t, os.WriteFile(bad, badSrc, 0o644))

	removed, skipped, backups, err := removeDeadLocs([]deadFuncLoc{
		{name: "locatedDead", file: good},
		{name: "brokenDead", file: bad},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad.go")
	assert.Nil(t, removed)
	assert.Nil(t, skipped)
	assert.Nil(t, backups)

	got, err := os.ReadFile(good)
	require.NoError(t, err)
	assert.Equal(t, original, got)
	gotBad, err := os.ReadFile(bad)
	require.NoError(t, err)
	assert.Equal(t, badSrc, gotBad)
}

func TestRemoveDeadCode_RemovesDeadGenericsKeepsLiveAndMethods(t *testing.T) {
	const src = `package cli

func init() { keep() }

func keep() {
	println(liveGeneric(1))
	fn := liveGeneric[int]
	println(fn(2))
}

func liveGeneric[T any](v T) T { return v }

type Box[T any] struct{ V T }

func (b Box[T]) M() T { return b.V }

func (b Box[T]) Unused() {}

func deadPlain() { println("plain") }

func deadGeneric[T any](v T) T { return v }

func deadSplit[
	T any,
](v T) T { return v }

func deadOuter[T any](v T) T { return deadInner(v) }

func deadInner[T any](v T) T { return v }
`
	dir := writePolishModule(t, src, "")
	cliDir := filepath.Join(dir, "internal", "cli")

	dry, err := RemoveDeadCode(dir, true)
	require.NoError(t, err)
	assert.False(t, dry.BuildVerified)
	assert.Empty(t, dry.Removed)
	assert.ElementsMatch(t, []string{"deadGeneric", "deadOuter", "deadPlain", "deadSplit"}, dry.DeadFunctions)
	assert.NotContains(t, dry.DeadFunctions, "deadInner")
	assert.NotContains(t, dry.DeadFunctions, "liveGeneric")
	untouched, err := os.ReadFile(filepath.Join(cliDir, "helpers.go"))
	require.NoError(t, err)
	assert.Equal(t, src, string(untouched))

	locs := findDeadFuncLocs(cliDir, filepath.Join(dir, "cmd"), filepath.Join(dir, "internal", "mcp"))
	for _, name := range []string{"deadGeneric", "deadSplit", "deadPlain", "deadOuter"} {
		var found bool
		for _, loc := range locs {
			if loc.name == name && filepath.Base(loc.file) == "helpers.go" {
				found = true
			}
		}
		assert.Truef(t, found, "scan did not place %s in helpers.go: %+v", name, locs)
	}

	result, err := RemoveDeadCode(dir, false)
	require.NoError(t, err)
	require.True(t, result.BuildVerified, "build_error=%s", result.BuildError)
	assert.Empty(t, result.Restored)
	assert.ElementsMatch(t, []string{"deadGeneric", "deadInner", "deadOuter", "deadPlain", "deadSplit"}, result.Removed)
	assert.NotContains(t, result.Removed, "liveGeneric")
	assert.NotContains(t, result.Removed, "M")
	assert.NotContains(t, result.Removed, "Unused")

	got, err := os.ReadFile(filepath.Join(cliDir, "helpers.go"))
	require.NoError(t, err)
	text := string(got)
	assert.NotContains(t, text, "func deadGeneric[")
	assert.NotContains(t, text, "func deadSplit[")
	assert.NotContains(t, text, "func deadPlain(")
	assert.NotContains(t, text, "func deadOuter[")
	assert.NotContains(t, text, "func deadInner[")
	assert.Contains(t, text, "func liveGeneric[")
	assert.Contains(t, text, "liveGeneric[int]")
	assert.Contains(t, text, "func (b Box[T]) M()")
	assert.Contains(t, text, "func (b Box[T]) Unused()")
}

func TestRemoveDeadCode_UnlocatedGenericIsNotVerified(t *testing.T) {
	const src = `package cli

func deadGeneric[T any](v T) T { return v }
`
	dir := writePolishModule(t, src, "")
	helpers := filepath.Join(dir, "internal", "cli", "helpers.go")
	original, err := os.ReadFile(helpers)
	require.NoError(t, err)

	result, err := removeDeadCode(dir, false, func(string, ...string) []deadFuncLoc {
		return []deadFuncLoc{{name: "deadGeneric", file: ""}}
	})
	require.NoError(t, err)
	assert.False(t, result.BuildVerified)
	assert.Equal(t, "dead functions not removed: deadGeneric", result.BuildError)
	assert.Empty(t, result.Removed)
	assert.Empty(t, result.Restored)
	assert.Equal(t, []string{"deadGeneric"}, result.DeadFunctions)
	got, err := os.ReadFile(helpers)
	require.NoError(t, err)
	assert.Equal(t, original, got)
}

func TestRemoveDeadCode_RollbackMixRestoresAndDoesNotPanic(t *testing.T) {
	const src = `package cli

func init() { keep() }

func keep() { secondDead() }

func firstDead() { println("first") }

func secondDead() { println("second") }

func deadGeneric[T any](v T) T { return v }

type Box[T any] struct{}

func (b Box[T]) M() {}
`
	t.Run("failed build restores only the function that was removed", func(t *testing.T) {
		dir := writePolishModule(t, src, "package main\n\nfunc main() { _ = undefinedSymbol }\n")
		helpers := filepath.Join(dir, "internal", "cli", "helpers.go")
		original, err := os.ReadFile(helpers)
		require.NoError(t, err)

		var result *PolishResult
		require.NotPanics(t, func() {
			result, err = removeDeadCode(dir, false, func(cliDir string, _ ...string) []deadFuncLoc {
				return []deadFuncLoc{
					{name: "deadGeneric", file: ""},
					{name: "secondDead", file: filepath.Join(cliDir, "helpers.go")},
				}
			})
		})
		require.NoError(t, err)
		require.False(t, result.BuildVerified)
		assert.NotEmpty(t, result.BuildError)
		assert.Empty(t, result.Removed)
		assert.Equal(t, []string{"secondDead"}, result.Restored)
		assert.NotContains(t, result.Restored, "deadGeneric")
		assert.ElementsMatch(t, []string{"deadGeneric", "secondDead"}, result.DeadFunctions)
		got, err := os.ReadFile(helpers)
		require.NoError(t, err)
		assert.Equal(t, original, got)
	})

	t.Run("later failure keeps an earlier removal", func(t *testing.T) {
		dir := writePolishModule(t, src, "")
		helpers := filepath.Join(dir, "internal", "cli", "helpers.go")
		var pass int
		var result *PolishResult
		var err error
		require.NotPanics(t, func() {
			result, err = removeDeadCode(dir, false, func(cliDir string, _ ...string) []deadFuncLoc {
				pass++
				file := filepath.Join(cliDir, "helpers.go")
				if pass == 1 {
					return []deadFuncLoc{{name: "firstDead", file: file}}
				}
				return []deadFuncLoc{
					{name: "deadGeneric", file: ""},
					{name: "secondDead", file: file},
				}
			})
		})
		require.NoError(t, err)
		require.False(t, result.BuildVerified)
		assert.Equal(t, []string{"firstDead"}, result.Removed)
		assert.Equal(t, []string{"secondDead"}, result.Restored)
		assert.NotContains(t, result.Restored, "deadGeneric")
		assert.NotContains(t, result.Removed, "deadGeneric")
		got, err := os.ReadFile(helpers)
		require.NoError(t, err)
		text := string(got)
		assert.NotContains(t, text, "func firstDead(")
		assert.Contains(t, text, "func secondDead(")
		assert.Contains(t, text, "func deadGeneric[")
		assert.Contains(t, text, "func (b Box[T]) M()")
	})
}

func TestRemoveDeadCode_SkippedCandidateDoesNotClaimSuccess(t *testing.T) {
	const src = `package cli

func init() {}

func locatedDead() { println("remove-me") }

func deadGeneric[T any](v T) T { return v }

type T struct{}

func (t T) Method() {}
`
	dir := writePolishModule(t, src, "")
	helpers := filepath.Join(dir, "internal", "cli", "helpers.go")
	missing := filepath.Join(dir, "internal", "cli", "missing.go")

	result, err := removeDeadCode(dir, false, func(cliDir string, _ ...string) []deadFuncLoc {
		file := filepath.Join(cliDir, "helpers.go")
		return []deadFuncLoc{
			{name: "ghost", file: ""},
			{name: "missingDead", file: missing},
			{name: "notHere", file: file},
			{name: "locatedDead", file: file},
		}
	})
	require.NoError(t, err)
	assert.False(t, result.BuildVerified)
	assert.Equal(t, "dead functions not removed: ghost, missingDead, notHere", result.BuildError)
	assert.Equal(t, []string{"locatedDead"}, result.Removed)
	assert.Empty(t, result.Restored)
	got, err := os.ReadFile(helpers)
	require.NoError(t, err)
	text := string(got)
	assert.NotContains(t, text, "func locatedDead(")
	assert.Contains(t, text, "func deadGeneric[")
	assert.Contains(t, text, "func (t T) Method()")
	_, statErr := os.Stat(missing)
	assert.True(t, os.IsNotExist(statErr))
}

func writePolishModule(t *testing.T, cliSrc, mainSrc string) string {
	t.Helper()
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "app"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/acme\n\ngo 1.22\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "helpers.go"), []byte(cliSrc), 0o644))
	if mainSrc == "" {
		mainSrc = "package main\n\nfunc main() {}\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", "app", "main.go"), []byte(mainSrc), 0o644))
	return dir
}
