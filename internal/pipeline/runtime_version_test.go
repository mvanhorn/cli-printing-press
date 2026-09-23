package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreserveStampedRuntimeVersionRelocatesRootDeclaration(t *testing.T) {
	base := t.TempDir()
	dest := t.TempDir()
	const stamped = "var version = \"2026.8.1\""
	writeGoFile(t, filepath.Join(base, "internal", "cli", "root.go"), "package cli\n\n"+stamped+"\n\nfunc newRootCmd() {}\n")
	writeGoFile(t, filepath.Join(dest, "internal", "cli", "root.go"), "package cli\n\nfunc newRootCmd() {}\n")
	versionSrc := "package cli\n\nimport \"fmt\"\n\n// version is the printed CLI's version, overridable at build time via ldflags.\nvar version = \"0.0.0-dev\"\n\nfunc newVersionCmd() {\n\tfmt.Println(version)\n}\n"
	writeGoFile(t, filepath.Join(dest, "internal", "cli", "version.go"), versionSrc)

	require.NoError(t, PreserveStampedRuntimeVersion(base, dest))

	root, err := os.ReadFile(filepath.Join(dest, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(root), stamped)
	version, err := os.ReadFile(filepath.Join(dest, "internal", "cli", "version.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(version), "var version")
	assert.Contains(t, string(version), "func newVersionCmd()")
	assert.Contains(t, string(version), "fmt.Println(version)")
}

func TestPreserveStampedRuntimeVersionKeepsVersionGoDeclaration(t *testing.T) {
	base := t.TempDir()
	dest := t.TempDir()
	const stamped = "var version = \"2026.8.1\""
	writeGoFile(t, filepath.Join(base, "internal", "cli", "version.go"), "package cli\n\n"+stamped+"\n\nfunc newVersionCmd() {}\n")
	destVersion := filepath.Join(dest, "internal", "cli", "version.go")
	writeGoFile(t, destVersion, "package cli\n\nvar version = \"0.0.0-dev\"\n\nfunc newVersionCmd() {}\n")

	require.NoError(t, PreserveStampedRuntimeVersion(base, dest))

	got, err := os.ReadFile(destVersion)
	require.NoError(t, err)
	assert.Contains(t, string(got), stamped)
	assert.NotContains(t, string(got), "0.0.0-dev")
	assert.Contains(t, string(got), "func newVersionCmd()")
}

func TestPreserveStampedRuntimeVersionNoopsWhenAlreadyStamped(t *testing.T) {
	base := t.TempDir()
	dest := t.TempDir()
	src := "package cli\n\nvar version = \"2026.8.1\"\n\nfunc newVersionCmd() {}\n"
	writeGoFile(t, filepath.Join(base, "internal", "cli", "version.go"), src)
	destVersion := filepath.Join(dest, "internal", "cli", "version.go")
	writeGoFile(t, destVersion, src)
	before, err := os.ReadFile(destVersion)
	require.NoError(t, err)

	require.NoError(t, PreserveStampedRuntimeVersion(base, dest))

	after, err := os.ReadFile(destVersion)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestPreserveStampedRuntimeVersionLeavesDestAloneWithoutBaseDeclaration(t *testing.T) {
	base := t.TempDir()
	dest := t.TempDir()
	writeGoFile(t, filepath.Join(base, "internal", "cli", "root.go"), "package cli\n\nfunc newRootCmd() {}\n")
	destVersion := filepath.Join(dest, "internal", "cli", "version.go")
	src := "package cli\n\nvar version = \"0.0.0-dev\"\n"
	writeGoFile(t, destVersion, src)
	before, err := os.ReadFile(destVersion)
	require.NoError(t, err)

	require.NoError(t, PreserveStampedRuntimeVersion(base, dest))

	after, err := os.ReadFile(destVersion)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestPreserveStampedRuntimeVersionUpdatesMCPMainFromBaseLine(t *testing.T) {
	base := t.TempDir()
	dest := t.TempDir()
	const stamped = "var version = \"2026.8.1\""
	writeGoFile(t, filepath.Join(base, "cmd", "demo-pp-mcp", "main.go"), "package main\n\n"+stamped+"\n\nfunc main() { _ = version }\n")
	destMain := filepath.Join(dest, "cmd", "demo-pp-mcp", "main.go")
	writeGoFile(t, destMain, "package main\n\nvar version = \"0.0.0-dev\"\n\nfunc main() { _ = version }\n")

	require.NoError(t, PreserveStampedRuntimeVersion(base, dest))

	got, err := os.ReadFile(destMain)
	require.NoError(t, err)
	assert.Contains(t, string(got), stamped)
	assert.NotContains(t, string(got), "0.0.0-dev")
}

func TestPreserveStampedRuntimeVersionStampsNewMCPMain(t *testing.T) {
	base := t.TempDir()
	dest := t.TempDir()
	writeGoFile(t, filepath.Join(base, "internal", "cli", "root.go"), "package cli\n\nvar version = \"2026.8.1\"\n")
	writeGoFile(t, filepath.Join(dest, "internal", "cli", "root.go"), "package cli\n\nfunc newRootCmd() {}\n")
	destMain := filepath.Join(dest, "cmd", "demo-pp-mcp", "main.go")
	writeGoFile(t, destMain, "package main\n\nvar version = \"0.0.0-dev\"\n\nfunc main() { _ = version }\n")

	require.NoError(t, PreserveStampedRuntimeVersion(base, dest))

	got, err := os.ReadFile(destMain)
	require.NoError(t, err)
	assert.Contains(t, string(got), "var version = \"2026.8.1\"")
	assert.NotContains(t, string(got), "0.0.0-dev")
}

func TestPreserveStampedRuntimeVersionInlinesMCPMainWithoutBaseDeclaration(t *testing.T) {
	base := t.TempDir()
	dest := t.TempDir()
	writeGoFile(t, filepath.Join(base, "internal", "cli", "root.go"), "package cli\n\nvar version = \"2026.8.1\"\n")
	writeGoFile(t, filepath.Join(dest, "internal", "cli", "root.go"), "package cli\n\nfunc newRootCmd() {}\n")
	writeGoFile(t, filepath.Join(base, "cmd", "demo-pp-mcp", "main.go"), "package main\n\nfunc main() {}\n")
	destMain := filepath.Join(dest, "cmd", "demo-pp-mcp", "main.go")
	writeGoFile(t, destMain, "package main\n\nvar version = \"0.0.0-dev\"\n\nfunc main() {\n\tprintln(version)\n}\n\nfunc local(version string) {\n\tprintln(version)\n}\n")

	require.NoError(t, PreserveStampedRuntimeVersion(base, dest))

	got, err := os.ReadFile(destMain)
	require.NoError(t, err)
	assert.NotContains(t, string(got), "var version")
	assert.Contains(t, string(got), "println(\"2026.8.1\")")
	assert.Contains(t, string(got), "func local(version string)")
	assert.Contains(t, string(got), "\tprintln(version)")
}

func TestPreserveStampedRuntimeVersionRejectsNonLiteral(t *testing.T) {
	base := t.TempDir()
	dest := t.TempDir()
	writeGoFile(t, filepath.Join(base, "internal", "cli", "root.go"), "package cli\n\nvar version = \"2026.8.1\" + \"\"\n")
	writeGoFile(t, filepath.Join(dest, "internal", "cli", "root.go"), "package cli\n\nvar version = \"0.0.0-dev\"\n")

	err := PreserveStampedRuntimeVersion(base, dest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a string literal")
}

func TestPreserveStampedRuntimeVersionRejectsFileBase(t *testing.T) {
	base := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(base, []byte("x"), 0o644))
	err := PreserveStampedRuntimeVersion(base, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
	require.NoError(t, PreserveStampedRuntimeVersion("", t.TempDir()))
}

func writeGoFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}
