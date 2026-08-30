package regenmerge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverlayDoesNotCopyUnusedPublishedImports(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		return path
	}
	pub := write("pub.go", `package client

import (
	"fmt"
	"os"
)

func HandPatched() string { return fmt.Sprint("kept") }
func Moved() { os.Exit(1) }
`)
	fresh := write("fresh.go", `package client

func HandPatched() string { return "fresh" }
func Rewritten() bool { return true }
`)
	base := write("base.go", `package client

func HandPatched() string { return "orig" }
func Moved() { os.Exit(1) }
`)
	dest := filepath.Join(dir, "dest.go")
	require.NoError(t, overlayHandEditedDecls(pub, fresh, base, dest))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Contains(t, string(got), `fmt.Sprint("kept")`)
	assert.Contains(t, string(got), `"fmt"`)
	assert.NotContains(t, string(got), `"os"`)
	assert.NotContains(t, string(got), "os.Exit")
	assert.Contains(t, string(got), "func Rewritten")
}

func TestOverlayDropsUnusedFreshImports(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		return path
	}
	pub := write("pub.go", `package client

func HandPatched() string { return "kept" }
func Other() {}
`)
	fresh := write("fresh.go", `package client

import "fmt"

func HandPatched() string { return fmt.Sprint("fresh") }
func Other() {}
`)
	base := write("base.go", `package client

func HandPatched() string { return "orig" }
func Other() {}
`)
	dest := filepath.Join(dir, "dest.go")
	require.NoError(t, overlayHandEditedDecls(pub, fresh, base, dest))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Contains(t, string(got), `"kept"`)
	assert.NotContains(t, string(got), `"fmt"`)
	assert.NotContains(t, string(got), "fmt.Sprint")
	assert.Contains(t, string(got), "func Other")
}

func TestOverlayRewritesPublishedAliasToDestAlias(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		return path
	}
	pub := write("pub.go", `package client

import p "fmt"

func HandPatched() string { return p.Sprint("kept") }
func Other() {}
`)
	fresh := write("fresh.go", `package client

import f "fmt"

func HandPatched() string { return f.Sprint("fresh") }
func Other() { _ = f.Sprint("other") }
`)
	base := write("base.go", `package client

func HandPatched() string { return "orig" }
func Other() {}
`)
	dest := filepath.Join(dir, "dest.go")
	require.NoError(t, overlayHandEditedDecls(pub, fresh, base, dest))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Contains(t, string(got), `f.Sprint("kept")`)
	assert.Contains(t, string(got), `"fmt"`)
	assert.NotContains(t, string(got), `p.Sprint`)
	assert.Contains(t, string(got), `f.Sprint("other")`)
}

func TestOverlayRewritesCollidingAliasToDestPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		return path
	}
	pub := write("pub.go", `package client

import f "os"

func HandPatched() string { return f.Getenv("kept") }
func Other() {}
`)
	fresh := write("fresh.go", `package client

import (
	f "fmt"
	"os"
)

func HandPatched() string { return f.Sprint("fresh") }
func Other() string { return f.Sprint("other") + os.Getenv("x") }
`)
	base := write("base.go", `package client

func HandPatched() string { return "orig" }
func Other() {}
`)
	dest := filepath.Join(dir, "dest.go")
	require.NoError(t, overlayHandEditedDecls(pub, fresh, base, dest))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Contains(t, string(got), `os.Getenv("kept")`)
	assert.Contains(t, string(got), `f.Sprint("other")`)
	assert.NotContains(t, string(got), `f.Getenv`)
}

func TestOverlayAddsImportWhenAliasCollides(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		return path
	}
	pub := write("pub.go", `package client

import f "os"

func HandPatched() { f.Exit(0) }
func Other() {}
`)
	fresh := write("fresh.go", `package client

import f "fmt"

func HandPatched() string { return f.Sprint("fresh") }
func Other() string { return f.Sprint("other") }
`)
	base := write("base.go", `package client

func HandPatched() string { return "orig" }
func Other() {}
`)
	dest := filepath.Join(dir, "dest.go")
	require.NoError(t, overlayHandEditedDecls(pub, fresh, base, dest))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Contains(t, string(got), `os.Exit(0)`)
	assert.Contains(t, string(got), `"os"`)
	assert.Contains(t, string(got), `f.Sprint("other")`)
	assert.NotContains(t, string(got), `f.Exit`)
}

func TestOverlayDoesNotRewriteShadowedImportAlias(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		return path
	}
	pub := write("pub.go", `package client

import f "os"

func HandPatched(f interface{ Close() error }) string {
	f.Close()
	return "kept"
}
`)
	fresh := write("fresh.go", `package client

import pkgos "os"

func HandPatched() string { return pkgos.Getenv("fresh") }
func Other() string { return pkgos.Getenv("other") }
`)
	base := write("base.go", `package client

func HandPatched() string { return "orig" }
func Other() {}
`)
	dest := filepath.Join(dir, "dest.go")
	require.NoError(t, overlayHandEditedDecls(pub, fresh, base, dest))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Contains(t, string(got), "f.Close()")
	assert.Contains(t, string(got), `"kept"`)
	assert.NotContains(t, string(got), "pkgos.Close")
}

func TestOverlayKeepsSignatureOnlyImport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		return path
	}
	pub := write("pub.go", `package client

import "os"

func HandPatched(f *os.File) string { return "kept" }
func Other() {}
`)
	fresh := write("fresh.go", `package client

func HandPatched() string { return "fresh" }
func Other() {}
`)
	base := write("base.go", `package client

func HandPatched() string { return "orig" }
func Other() {}
`)
	dest := filepath.Join(dir, "dest.go")
	require.NoError(t, overlayHandEditedDecls(pub, fresh, base, dest))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Contains(t, string(got), `*os.File`)
	assert.Contains(t, string(got), `"os"`)
	assert.Contains(t, string(got), `"kept"`)
}

func TestOverlayRewritesAliasInSignature(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		return path
	}
	pub := write("pub.go", `package client

import f "os"

func HandPatched(x *f.File) string { return "kept" }
func Other() {}
`)
	fresh := write("fresh.go", `package client

import pkgos "os"

func HandPatched() string { return pkgos.Getenv("fresh") }
func Other() string { return pkgos.Getenv("other") }
`)
	base := write("base.go", `package client

func HandPatched() string { return "orig" }
func Other() {}
`)
	dest := filepath.Join(dir, "dest.go")
	require.NoError(t, overlayHandEditedDecls(pub, fresh, base, dest))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Contains(t, string(got), `*pkgos.File`)
	assert.Contains(t, string(got), `"kept"`)
	assert.NotContains(t, string(got), `*f.File`)
}

func TestOverlayKeepsFreshMemberOfGroupedDecl(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		return path
	}
	header := "// Generated by CLI Printing Press. DO NOT EDIT.\npackage client\n\n"
	pub := write("pub.go", header+`const (
	Hand = "kept"
	Rewritten = "old"
)
`)
	fresh := write("fresh.go", header+`const (
	Hand = "fresh"
	Rewritten = "new"
)
`)
	base := write("base.go", header+`const (
	Hand = "orig"
	Rewritten = "old"
)
`)
	dest := filepath.Join(dir, "dest.go")
	require.NoError(t, overlayHandEditedDecls(pub, fresh, base, dest))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Contains(t, string(got), `"kept"`)
	assert.Contains(t, string(got), `"new"`)
	assert.NotContains(t, string(got), `"old"`)
	assert.Contains(t, string(got), "Hand")
	assert.Contains(t, string(got), "Rewritten")
}

func TestOverlayKeepsFreshMemberOfMultiNameSpec(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		return path
	}
	header := "// Generated by CLI Printing Press. DO NOT EDIT.\npackage client\n\n"
	pub := write("pub.go", header+`const (
	Hand, Rewritten = "kept", "old"
)
`)
	fresh := write("fresh.go", header+`const (
	Hand, Rewritten = "fresh", "new"
)
`)
	base := write("base.go", header+`const (
	Hand, Rewritten = "orig", "old"
)
`)
	dest := filepath.Join(dir, "dest.go")
	require.NoError(t, overlayHandEditedDecls(pub, fresh, base, dest))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Contains(t, string(got), `"kept"`)
	assert.Contains(t, string(got), `"new"`)
	assert.NotContains(t, string(got), `"old"`)
	assert.NotContains(t, string(got), `"fresh"`)
}
