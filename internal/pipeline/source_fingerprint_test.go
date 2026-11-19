package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePhase5GateAcceptsUnchangedSourceAndIgnoresREADMEChanges(t *testing.T) {
	cliDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "internal", "cli", "root.go"), []byte("package cli\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "spec.yaml"), []byte("openapi: 3.0.0\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "README.md"), []byte("before\n"), 0o644))

	source, err := CaptureSourceFingerprint(cliDir)
	require.NoError(t, err)
	proofsDir := t.TempDir()
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion:     1,
		APIName:           "test",
		RunID:             "run-1",
		Status:            "pass",
		Level:             "full",
		MatrixSize:        1,
		TestsPassed:       1,
		SourceFingerprint: source.Digest,
		SourceFiles:       source.Files,
		AuthContext:       Phase5AuthContext{Type: "none"},
	})
	manifest := CLIManifest{APIName: "test", RunID: "run-1", AuthType: "none"}

	result := ValidatePhase5Gate(proofsDir, manifest, cliDir)
	require.True(t, result.Passed, result.Detail)

	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "README.md"), []byte("after\n"), 0o644))
	result = ValidatePhase5Gate(proofsDir, manifest, cliDir)
	assert.True(t, result.Passed, result.Detail)
}

func TestValidatePhase5GateRejectsSourceDriftAndNamesChangedFile(t *testing.T) {
	cliDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "cli"), 0o755))
	rootPath := filepath.Join(cliDir, "internal", "cli", "root.go")
	require.NoError(t, os.WriteFile(rootPath, []byte("package cli\n\nvar version = 1\n"), 0o644))

	source, err := CaptureSourceFingerprint(cliDir)
	require.NoError(t, err)
	proofsDir := t.TempDir()
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion:     1,
		APIName:           "test",
		RunID:             "run-1",
		Status:            "pass",
		Level:             "full",
		MatrixSize:        1,
		TestsPassed:       1,
		SourceFingerprint: source.Digest,
		SourceFiles:       source.Files,
		AuthContext:       Phase5AuthContext{Type: "none"},
	})
	manifest := CLIManifest{APIName: "test", RunID: "run-1", AuthType: "none"}

	require.NoError(t, os.WriteFile(rootPath, []byte("package cli\n\nvar version = 2\n"), 0o644))
	result := ValidatePhase5Gate(proofsDir, manifest, cliDir)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "source fingerprint")
	assert.Contains(t, result.Detail, "internal/cli/root.go")

	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "internal", "cli", "added.go"), []byte("package cli\n"), 0o644))
	result = ValidatePhase5Gate(proofsDir, manifest, cliDir)
	assert.Contains(t, result.Detail, "internal/cli/added.go")

	require.NoError(t, os.Remove(rootPath))
	result = ValidatePhase5Gate(proofsDir, manifest, cliDir)
	assert.Contains(t, result.Detail, "internal/cli/root.go")
}

func TestValidatePhase5GateRejectsMissingFingerprint(t *testing.T) {
	cliDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "main.go"), []byte("package main\n"), 0o644))
	proofsDir := t.TempDir()
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "pass",
		Level:         "full",
		MatrixSize:    1,
		TestsPassed:   1,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, CLIManifest{APIName: "test", RunID: "run-1"}, cliDir)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "missing source_fingerprint")
}

func TestValidatePhase5GateDoesNotListFilesForAggregateOnlyProof(t *testing.T) {
	cliDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "main.go"), []byte("package main\n"), 0o644))
	proofsDir := t.TempDir()
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion:     1,
		APIName:           "test",
		RunID:             "run-1",
		Status:            "pass",
		Level:             "full",
		MatrixSize:        1,
		TestsPassed:       1,
		SourceFingerprint: "stale",
		AuthContext:       Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, CLIManifest{APIName: "test", RunID: "run-1"}, cliDir)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "source fingerprint")
	assert.NotContains(t, result.Detail, "changed source files:")
}

func TestCaptureSourceFingerprintIncludesModuleAndSpecInputs(t *testing.T) {
	cliDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "go.mod"), []byte("module example\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "go.sum"), []byte("sum\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "spec.yaml"), []byte("openapi: 3.0.0\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "README.md"), []byte("docs\n"), 0o644))
	for _, dir := range []string{".git", ".manuscripts/archive", ".printing-press/cache"} {
		dirPath := filepath.Join(cliDir, dir)
		require.NoError(t, os.MkdirAll(dirPath, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dirPath, "spec.yaml"), []byte("ignored\n"), 0o644))
	}

	source, err := CaptureSourceFingerprint(cliDir)
	require.NoError(t, err)
	assert.Contains(t, source.Files, "go.mod")
	assert.Contains(t, source.Files, "go.sum")
	assert.Contains(t, source.Files, "spec.yaml")
	assert.NotContains(t, source.Files, "README.md")
	assert.NotContains(t, source.Files, ".git/spec.yaml")
	assert.NotContains(t, source.Files, ".manuscripts/archive/spec.yaml")
	assert.NotContains(t, source.Files, ".printing-press/cache/spec.yaml")
}

func TestCaptureSourceFingerprintStableAcrossPublishModuleRewrite(t *testing.T) {
	cliDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "client"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, ".printing-press.json"), []byte(`{"api_name":"sendfox","cli_name":"sendfox-pp-cli"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "go.mod"), []byte("module sendfox-pp-cli\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "internal", "client", "client.go"), []byte("package client\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "internal", "cli", "root.go"), []byte(`package cli

import (
	"fmt"

	"sendfox-pp-cli/internal/client"
)

func useClient() { fmt.Sprint(client.Client{}) }
`), 0o644))

	before, err := CaptureSourceFingerprint(cliDir)
	require.NoError(t, err)
	require.NoError(t, RewriteModulePath(cliDir, "sendfox-pp-cli", "github.com/mvanhorn/printing-press-library/library/marketing/sendfox"))
	after, err := CaptureSourceFingerprint(cliDir)
	require.NoError(t, err)

	assert.Equal(t, before.Digest, after.Digest)
	assert.Equal(t, before.Files, after.Files)

	rootPath := filepath.Join(cliDir, "internal", "cli", "root.go")
	require.NoError(t, os.WriteFile(rootPath, []byte(`package cli

import (
	"fmt"

	"github.com/mvanhorn/printing-press-library/library/marketing/sendfox/internal/client"
)

func useClient(){ fmt.Sprint(client.Client{}) }
`), 0o644))
	drifted, err := CaptureSourceFingerprint(cliDir)
	require.NoError(t, err)
	assert.NotEqual(t, after.Digest, drifted.Digest)
	assert.NotEqual(t, after.Files["internal/cli/root.go"], drifted.Files["internal/cli/root.go"])
}

func TestNormalizeGoImportFingerprintsKeepsLegacyEncodingWithoutComments(t *testing.T) {
	source := []byte(`package main

import (
	"fmt"
	_ "example.com/source/internal/client"
)

func main() {}
`)
	want := []byte("package main\n\n\n<printing-press-imports>\n\x00fmt\n_\x00printing.press/source/internal/client\n<printing-press-import-comments>\n\n</printing-press-imports>\n\nfunc main() {}\n")

	got, err := normalizeGoImportFingerprints("main.go", source, "example.com/source", "printing.press/source")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestNormalizeGoImportFingerprintsPreservesImportCommentAttachment(t *testing.T) {
	withPreamble := []byte(`package main

import (
	// #cgo CFLAGS: -DPRINTING_PRESS
	"C"
	"fmt"
)
`)
	reorderedWithPreamble := []byte(`package main

import (
	"fmt"
	// #cgo CFLAGS: -DPRINTING_PRESS
	"C"
)
`)
	preambleMovedToFmt := []byte(`package main

import (
	"C"
	// #cgo CFLAGS: -DPRINTING_PRESS
	"fmt"
)
`)

	normalized, err := normalizeGoImportFingerprints("main.go", withPreamble, "example.com/source", "printing.press/source")
	require.NoError(t, err)
	reordered, err := normalizeGoImportFingerprints("main.go", reorderedWithPreamble, "example.com/source", "printing.press/source")
	require.NoError(t, err)
	moved, err := normalizeGoImportFingerprints("main.go", preambleMovedToFmt, "example.com/source", "printing.press/source")
	require.NoError(t, err)

	assert.Equal(t, normalized, reordered)
	assert.NotEqual(t, normalized, moved)
}

func TestCaptureSourceFingerprintRejectsArbitraryModuleRename(t *testing.T) {
	cliDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, ".printing-press.json"), []byte(`{"api_name":"sendfox","cli_name":"sendfox-pp-cli"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "go.mod"), []byte("module sendfox-pp-cli\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "internal", "cli", "root.go"), []byte("package cli\n\nimport _ \"sendfox-pp-cli/internal/client\"\n"), 0o644))

	before, err := CaptureSourceFingerprint(cliDir)
	require.NoError(t, err)
	require.NoError(t, RewriteModulePath(cliDir, "sendfox-pp-cli", "github.com/example/renamed-sendfox"))
	after, err := CaptureSourceFingerprint(cliDir)
	require.NoError(t, err)

	assert.NotEqual(t, before.Digest, after.Digest)
}

func TestCaptureSourceFingerprintIgnoresStampedVersionLiteral(t *testing.T) {
	dest := t.TempDir()
	base := t.TempDir()
	writeGoFile(t, filepath.Join(dest, "internal", "cli", "version.go"), runtimeVersionGo("0.0.0-dev"))
	writeGoFile(t, filepath.Join(dest, "internal", "cli", "root.go"), "package cli\n\nfunc root() {}\n")
	writeGoFile(t, filepath.Join(dest, "cmd", "widget-pp-mcp", "main.go"), mcpVersionGo("0.0.0-dev"))
	writeGoFile(t, filepath.Join(base, "internal", "cli", "version.go"), runtimeVersionGo("1.2.3"))
	writeGoFile(t, filepath.Join(base, "cmd", "widget-pp-mcp", "main.go"), mcpVersionGo("1.2.3"))

	before, err := CaptureSourceFingerprint(dest)
	require.NoError(t, err)
	require.NotEqual(t, before.Digest, before.legacyDigest, "a real version literal must still have a pre-normalization hash")
	require.NoError(t, PreserveStampedRuntimeVersion(base, dest))
	after, err := CaptureSourceFingerprint(dest)
	require.NoError(t, err)

	assert.Equal(t, before.Digest, after.Digest)
	assert.Equal(t, before.Files, after.Files)
	stamped, err := os.ReadFile(filepath.Join(dest, "internal", "cli", "version.go"))
	require.NoError(t, err)
	assert.Contains(t, string(stamped), `"1.2.3"`)
	assert.NotContains(t, string(stamped), "0.0.0-dev")
	mcp, err := os.ReadFile(filepath.Join(dest, "cmd", "widget-pp-mcp", "main.go"))
	require.NoError(t, err)
	assert.Contains(t, string(mcp), `"1.2.3"`)
}

func TestCaptureSourceFingerprintIgnoresMCPLiteralCopiedFromCLI(t *testing.T) {
	dest := t.TempDir()
	base := t.TempDir()
	writeGoFile(t, filepath.Join(dest, "internal", "cli", "version.go"), runtimeVersionGo("0.0.0-dev"))
	writeGoFile(t, filepath.Join(dest, "cmd", "widget-pp-mcp", "main.go"), mcpVersionGo("0.0.0-dev"))
	writeGoFile(t, filepath.Join(base, "internal", "cli", "version.go"), runtimeVersionGo("1.2.3"))

	before, err := CaptureSourceFingerprint(dest)
	require.NoError(t, err)
	require.NoError(t, PreserveStampedRuntimeVersion(base, dest))
	after, err := CaptureSourceFingerprint(dest)
	require.NoError(t, err)

	assert.Equal(t, before.Digest, after.Digest)
	mcp, err := os.ReadFile(filepath.Join(dest, "cmd", "widget-pp-mcp", "main.go"))
	require.NoError(t, err)
	assert.Contains(t, string(mcp), `"1.2.3"`)
}

func TestCaptureSourceFingerprintChangesWhenStampRewritesMoreThanLiteral(t *testing.T) {
	t.Run("inlines call sites", func(t *testing.T) {
		dest := t.TempDir()
		base := t.TempDir()
		writeGoFile(t, filepath.Join(dest, "internal", "cli", "root.go"), runtimeVersionGo("0.0.0-dev"))
		writeGoFile(t, filepath.Join(dest, "cmd", "widget-pp-mcp", "main.go"), "package main\n\nvar version = \"0.0.0-dev\"\n\nfunc main() { println(version) }\n")
		writeGoFile(t, filepath.Join(base, "internal", "cli", "root.go"), runtimeVersionGo("1.2.3"))
		writeGoFile(t, filepath.Join(base, "cmd", "widget-pp-mcp", "main.go"), "package main\n\nfunc main() {}\n")

		before, err := CaptureSourceFingerprint(dest)
		require.NoError(t, err)
		require.NoError(t, PreserveStampedRuntimeVersion(base, dest))
		after, err := CaptureSourceFingerprint(dest)
		require.NoError(t, err)
		assert.NotEqual(t, before.Digest, after.Digest)
		assert.NotEqual(t, before.Files["cmd/widget-pp-mcp/main.go"], after.Files["cmd/widget-pp-mcp/main.go"])
	})

	t.Run("moves the declaration between files", func(t *testing.T) {
		dest := t.TempDir()
		base := t.TempDir()
		writeGoFile(t, filepath.Join(dest, "internal", "cli", "version.go"), runtimeVersionGo("0.0.0-dev"))
		writeGoFile(t, filepath.Join(dest, "internal", "cli", "root.go"), "package cli\n\nfunc root() {}\n")
		writeGoFile(t, filepath.Join(base, "internal", "cli", "root.go"), "package cli\n\nvar version = \"1.2.3\"\n\nfunc root() {}\n")

		before, err := CaptureSourceFingerprint(dest)
		require.NoError(t, err)
		require.NoError(t, PreserveStampedRuntimeVersion(base, dest))
		after, err := CaptureSourceFingerprint(dest)
		require.NoError(t, err)
		assert.NotEqual(t, before.Digest, after.Digest)
	})
}

func TestCaptureSourceFingerprintVersionLiteralStaysNarrow(t *testing.T) {
	base := map[string]string{
		"internal/cli/version.go": runtimeVersionGo("1.0.0"),
		"internal/cli/root.go":    "package cli\n\nfunc root() { println(\"ok\") }\n",
	}
	same := fingerprintFixture(t, base)
	literal := fingerprintFixture(t, map[string]string{
		"internal/cli/version.go": runtimeVersionGo("9.9.9"),
		"internal/cli/root.go":    base["internal/cli/root.go"],
	})
	assert.Equal(t, same.Digest, literal.Digest)
	assert.Equal(t, same.Files["internal/cli/version.go"], literal.Files["internal/cli/version.go"])

	crlf := fingerprintFixture(t, map[string]string{
		"internal/cli/version.go": strings.ReplaceAll(runtimeVersionGo("2026.6.1"), "\n", "\r\n"),
		"internal/cli/root.go":    strings.ReplaceAll(base["internal/cli/root.go"], "\n", "\r\n"),
	})
	assert.NotEqual(t, same.Digest, crlf.Digest, "line endings outside the literal stay fingerprinted")
	crlfSameLiteral := fingerprintFixture(t, map[string]string{
		"internal/cli/version.go": strings.ReplaceAll(runtimeVersionGo("1.0.0"), "\n", "\r\n"),
		"internal/cli/root.go":    strings.ReplaceAll(base["internal/cli/root.go"], "\n", "\r\n"),
	})
	assert.Equal(t, crlf.Digest, crlfSameLiteral.Digest)

	placeholder := fingerprintFixture(t, map[string]string{
		"internal/cli/version.go": runtimeVersionGo("<printing-press-version>"),
		"internal/cli/root.go":    base["internal/cli/root.go"],
	})
	assert.Equal(t, same.Digest, placeholder.Digest)

	rootLiteral := fingerprintFixture(t, map[string]string{
		"internal/cli/root.go":    runtimeVersionGo("1.0.0"),
		"internal/cli/version.go": "package cli\n\nfunc extra() {}\n",
	})
	rootOther := fingerprintFixture(t, map[string]string{
		"internal/cli/root.go":    runtimeVersionGo("8.8.8"),
		"internal/cli/version.go": "package cli\n\nfunc extra() {}\n",
	})
	assert.Equal(t, rootLiteral.Digest, rootOther.Digest)

	cases := []struct {
		name string
		path string
		body string
	}{
		{name: "other line in version.go", path: "internal/cli/version.go", body: "package cli\n\n// version is the printed CLI's version, overridable at build time via ldflags.\nvar version = \"1.0.0\"\n\nfunc newVersionCmd() { println(\"edited\") }\n"},
		{name: "comment on the version line", path: "internal/cli/version.go", body: "package cli\n\n// version is the printed CLI's version, overridable at build time via ldflags.\nvar version = \"1.0.0\" // released\n\nfunc newVersionCmd() { println(version) }\n"},
		{name: "spacing on the version line", path: "internal/cli/version.go", body: "package cli\n\n// version is the printed CLI's version, overridable at build time via ldflags.\nvar  version = \"1.0.0\"\n\nfunc newVersionCmd() { println(version) }\n"},
		{name: "trailing statement on the version line", path: "internal/cli/version.go", body: "package cli\n\nvar version = \"1.0.0\"; var sink = \"pwned\"\n"},
		{name: "second version declaration", path: "internal/cli/version.go", body: "package cli\n\nvar version = \"1.0.0\"\nvar version = \"secret\"\n"},
		{name: "function-local version", path: "internal/cli/version.go", body: "package cli\n\nvar version = \"1.0.0\"\n\nfunc f() { var version = \"local\" }\n"},
		{name: "const version", path: "internal/cli/version.go", body: "package cli\n\nconst version = \"1.0.0\"\n"},
		{name: "non-string version", path: "internal/cli/version.go", body: "package cli\n\nvar version = 1\n"},
		{name: "multi-line version", path: "internal/cli/version.go", body: "package cli\n\nvar (\n\tversion = \"1.0.0\"\n)\n"},
		{name: "concatenation", path: "internal/cli/version.go", body: "package cli\n\nvar version = \"1.0.0\" + \"x\"\n"},
		{name: "raw string spans lines", path: "internal/cli/version.go", body: "package cli\n\nvar version = `1.0.0\nmore`\n"},
		{name: "syntax error", path: "internal/cli/version.go", body: "package cli\n\nvar version = \n"},
		{name: "other file", path: "internal/cli/other.go", body: "package cli\n\nfunc other() {}\n"},
		{name: "version.go outside the stamp set", path: "internal/client/version.go", body: runtimeVersionGo("1.0.0")},
		{name: "Version.go case", path: "internal/cli/Version.go", body: runtimeVersionGo("1.0.0")},
		{name: "mcp helper", path: "cmd/widget-pp-mcp/helper.go", body: "package main\n\nvar version = \"1.0.0\"\n"},
		{name: "nested mcp main", path: "cmd/widget-pp-mcp/nested/main.go", body: mcpVersionGo("1.0.0")},
		{name: "non-mcp main", path: "cmd/widget/main.go", body: mcpVersionGo("1.0.0")},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{
				"internal/cli/version.go": base["internal/cli/version.go"],
				"internal/cli/root.go":    base["internal/cli/root.go"],
			}
			files[tt.path] = tt.body
			got := fingerprintFixture(t, files)
			assert.NotEqual(t, same.Digest, got.Digest)
		})
	}

	t.Run("only the second literal is fingerprinted", func(t *testing.T) {
		first := fingerprintFixture(t, map[string]string{
			"internal/cli/version.go": "package cli\n\nvar version = \"1.0.0\"\nvar version = \"secret\"\n",
		})
		second := fingerprintFixture(t, map[string]string{
			"internal/cli/version.go": "package cli\n\nvar version = \"9.9.9\"\nvar version = \"secret\"\n",
		})
		edited := fingerprintFixture(t, map[string]string{
			"internal/cli/version.go": "package cli\n\nvar version = \"1.0.0\"\nvar version = \"other\"\n",
		})
		assert.Equal(t, first.Digest, second.Digest)
		assert.NotEqual(t, first.Digest, edited.Digest)
	})

	t.Run("trailing statement stays fingerprinted", func(t *testing.T) {
		first := fingerprintFixture(t, map[string]string{
			"internal/cli/version.go": "package cli\n\nvar version = \"1.0.0\"; var sink = \"pwned\"\n",
		})
		second := fingerprintFixture(t, map[string]string{
			"internal/cli/version.go": "package cli\n\nvar version = \"9.9.9\"; var sink = \"pwned\"\n",
		})
		edited := fingerprintFixture(t, map[string]string{
			"internal/cli/version.go": "package cli\n\nvar version = \"1.0.0\"; var sink = \"other\"\n",
		})
		assert.Equal(t, first.Digest, second.Digest)
		assert.NotEqual(t, first.Digest, edited.Digest)
	})

	t.Run("bypasses stay fingerprinted", func(t *testing.T) {
		pairs := []struct {
			name   string
			path   string
			before string
			after  string
		}{
			{
				name:   "function-local literal",
				path:   "internal/cli/version.go",
				before: "package cli\n\nvar version = \"1.0.0\"\n\nfunc f() { var version = \"local\" }\n",
				after:  "package cli\n\nvar version = \"1.0.0\"\n\nfunc f() { var version = \"other\" }\n",
			},
			{
				name:   "const version",
				path:   "internal/cli/version.go",
				before: "package cli\n\nconst version = \"1.0.0\"\n",
				after:  "package cli\n\nconst version = \"9.9.9\"\n",
			},
			{
				name:   "non-string version",
				path:   "internal/cli/version.go",
				before: "package cli\n\nvar version = 1\n",
				after:  "package cli\n\nvar version = 2\n",
			},
			{
				name:   "multi-line group",
				path:   "internal/cli/version.go",
				before: "package cli\n\nvar (\n\tversion = \"1.0.0\"\n)\n",
				after:  "package cli\n\nvar (\n\tversion = \"9.9.9\"\n)\n",
			},
			{
				name:   "concatenation",
				path:   "internal/cli/version.go",
				before: "package cli\n\nvar version = \"1.0.0\" + \"x\"\n",
				after:  "package cli\n\nvar version = \"1.0.0\" + \"y\"\n",
			},
			{
				name:   "raw string spans lines",
				path:   "internal/cli/version.go",
				before: "package cli\n\nvar version = `1.0.0\nmore`\n",
				after:  "package cli\n\nvar version = `1.0.0\nless`\n",
			},
			{
				name:   "other version.go",
				path:   "internal/client/version.go",
				before: runtimeVersionGo("1.0.0"),
				after:  runtimeVersionGo("9.9.9"),
			},
			{
				name:   "mcp helper",
				path:   "cmd/widget-pp-mcp/helper.go",
				before: "package main\n\nvar version = \"1.0.0\"\n",
				after:  "package main\n\nvar version = \"9.9.9\"\n",
			},
			{
				name:   "nested mcp main",
				path:   "cmd/widget-pp-mcp/nested/main.go",
				before: mcpVersionGo("1.0.0"),
				after:  mcpVersionGo("9.9.9"),
			},
			{
				name:   "non-mcp main",
				path:   "cmd/widget/main.go",
				before: mcpVersionGo("1.0.0"),
				after:  mcpVersionGo("9.9.9"),
			},
			{
				name:   "lookalike version file",
				path:   "internal/cli/version_windows.go",
				before: runtimeVersionGo("1.0.0"),
				after:  runtimeVersionGo("9.9.9"),
			},
		}
		for _, tt := range pairs {
			t.Run(tt.name, func(t *testing.T) {
				before := fingerprintFixture(t, map[string]string{tt.path: tt.before})
				after := fingerprintFixture(t, map[string]string{tt.path: tt.after})
				assert.NotEqual(t, before.Digest, after.Digest)
			})
		}
	})

	t.Run("unparsed version file still hashes", func(t *testing.T) {
		broken := fingerprintFixture(t, map[string]string{
			"internal/cli/version.go": "package cli\n\nvar version = \n",
		})
		assert.Equal(t, broken.Digest, broken.legacyDigest)
		edited := fingerprintFixture(t, map[string]string{
			"internal/cli/version.go": "package cli\n\nvar version = \nfunc leftover() {}\n",
		})
		assert.NotEqual(t, broken.Digest, edited.Digest)
	})
}

func TestIsStampedRuntimeVersionFile(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
	}{
		{rel: "internal/cli/version.go", want: true},
		{rel: "internal/cli/root.go", want: true},
		{rel: `internal\cli\version.go`, want: true},
		{rel: "internal/cli/version_test.go", want: false},
		{rel: "internal/cli/Version.go", want: false},
		{rel: "internal/client/version.go", want: false},
		{rel: "pkg/version.go", want: false},
		{rel: "cmd/widget-pp-mcp/main.go", want: true},
		{rel: "cmd/widget-pp-mcp/helper.go", want: false},
		{rel: "cmd/widget-pp-mcp/nested/main.go", want: false},
		{rel: "cmd/widget/main.go", want: false},
		{rel: "cmd/widget-pp-mcp/main.go/extra", want: false},
		{rel: "not-cmd/widget-pp-mcp/main.go", want: false},
		{rel: "cmd//main.go", want: false},
	}
	for _, tt := range cases {
		t.Run(tt.rel, func(t *testing.T) {
			assert.Equal(t, tt.want, isStampedRuntimeVersionFile(tt.rel))
		})
	}
}

func TestReplaceStampedVersionLiteralRejectsBadSpan(t *testing.T) {
	data := []byte("var version = \"1.0.0\"\n")
	decl := versionDecl{present: true, literal: `"1.0.0"`}
	cases := []versionDecl{
		{present: true, literal: `"1.0.0"`, litStart: -1, litEnd: 4},
		{present: true, literal: `"1.0.0"`, litStart: 0, litEnd: 0},
		{present: true, literal: `"1.0.0"`, litStart: 4, litEnd: 2},
		{present: true, literal: `"1.0.0"`, litStart: 0, litEnd: len(data) + 1},
		{present: true, literal: `"nope"`, litStart: 0, litEnd: 3},
		decl,
	}
	for _, tt := range cases {
		got := replaceStampedVersionLiteral(data, tt)
		assert.Equal(t, data, got)
	}

	parsed, err := parseVersionDeclSource("version.go", append([]byte("package cli\n\n"), data...))
	require.NoError(t, err)
	require.True(t, parsed.present)
	replaced := replaceStampedVersionLiteral(append([]byte("package cli\n\n"), data...), parsed)
	assert.Contains(t, string(replaced), stampedVersionFingerprintLiteral)
	assert.NotContains(t, string(replaced), `"1.0.0"`)
	again := replaceStampedVersionLiteral(replaced, versionDecl{
		present:  true,
		literal:  stampedVersionFingerprintLiteral,
		litStart: parsed.litStart,
		litEnd:   parsed.litStart + len(stampedVersionFingerprintLiteral),
	})
	assert.Equal(t, replaced, again)
}

func TestValidatePhase5GateAcceptsMarkerRecordedBeforeVersionNormalization(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "internal", "cli", "version.go"), runtimeVersionGo("1.0.0"))
	writeGoFile(t, filepath.Join(dir, "internal", "cli", "root.go"), "package cli\n\nfunc root() {}\n")
	current, err := CaptureSourceFingerprint(dir)
	require.NoError(t, err)
	require.NotEqual(t, current.Digest, current.legacyDigest)

	proofs := t.TempDir()
	manifest := CLIManifest{APIName: "test", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofs, Phase5AcceptanceFilename, phase5PassMarker(manifest, current.legacyDigest, current.legacyFiles))

	result := ValidatePhase5Gate(proofs, manifest, dir)
	require.True(t, result.Passed, result.Detail)

	writeGoFile(t, filepath.Join(dir, "internal", "cli", "version.go"), runtimeVersionGo("9.9.9"))
	result = ValidatePhase5Gate(proofs, manifest, dir)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "internal/cli/version.go")

	writeGoFile(t, filepath.Join(dir, "internal", "cli", "version.go"), runtimeVersionGo("1.0.0"))
	writeGoFile(t, filepath.Join(dir, "internal", "cli", "root.go"), "package cli\n\nfunc root() { println(1) }\n")
	result = ValidatePhase5Gate(proofs, manifest, dir)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "internal/cli/root.go")
	assert.NotContains(t, result.Detail, "internal/cli/version.go")
}

func TestValidatePhase5GateAcceptsStampedLiteralForNormalizedMarker(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "internal", "cli", "version.go"), runtimeVersionGo("0.0.0-dev"))
	current, err := CaptureSourceFingerprint(dir)
	require.NoError(t, err)
	proofs := t.TempDir()
	manifest := CLIManifest{APIName: "test", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofs, Phase5AcceptanceFilename, phase5PassMarker(manifest, current.Digest, current.Files))

	writeGoFile(t, filepath.Join(dir, "internal", "cli", "version.go"), runtimeVersionGo("1.0.0"))
	result := ValidatePhase5Gate(proofs, manifest, dir)
	require.True(t, result.Passed, result.Detail)

	writeGoFile(t, filepath.Join(dir, "internal", "cli", "added.go"), "package cli\n")
	result = ValidatePhase5Gate(proofs, manifest, dir)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "internal/cli/added.go")
}

func TestCaptureSourceFingerprintIgnoresVersionLiteralBesideImports(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "sendfox-pp-mcp"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(`{"api_name":"sendfox","cli_name":"sendfox-pp-cli"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module sendfox-pp-cli\n\ngo 1.26\n"), 0o644))
	writeGoFile(t, filepath.Join(dir, "internal", "cli", "version.go"), printedVersionGo("0.0.0-dev"))
	writeGoFile(t, filepath.Join(dir, "cmd", "sendfox-pp-mcp", "main.go"), printedMCPVersionGo("0.0.0-dev"))

	before, err := CaptureSourceFingerprint(dir)
	require.NoError(t, err)
	require.NotEqual(t, before.Digest, before.legacyDigest, "import canonicalization must not keep the version literal in the hash")

	writeGoFile(t, filepath.Join(dir, "internal", "cli", "version.go"), printedVersionGo("1.2.3"))
	writeGoFile(t, filepath.Join(dir, "cmd", "sendfox-pp-mcp", "main.go"), printedMCPVersionGo("1.2.3"))
	after, err := CaptureSourceFingerprint(dir)
	require.NoError(t, err)
	assert.Equal(t, before.Digest, after.Digest)
	assert.NotEqual(t, before.legacyDigest, after.legacyDigest)

	proofs := t.TempDir()
	manifest := CLIManifest{APIName: "sendfox", CLIName: "sendfox-pp-cli", RunID: "run-imports", AuthType: "none"}
	writePhase5GateMarker(t, proofs, Phase5AcceptanceFilename, phase5PassMarker(manifest, before.legacyDigest, before.legacyFiles))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(`{"api_name":"sendfox","cli_name":"sendfox-pp-cli","run_id":"run-imports"}`), 0o644))
	writeGoFile(t, filepath.Join(dir, "internal", "cli", "version.go"), printedVersionGo("0.0.0-dev"))
	writeGoFile(t, filepath.Join(dir, "cmd", "sendfox-pp-mcp", "main.go"), printedMCPVersionGo("0.0.0-dev"))
	result := ValidatePhase5Gate(proofs, manifest, dir)
	require.True(t, result.Passed, result.Detail)

	writeGoFile(t, filepath.Join(dir, "internal", "cli", "version.go"), strings.Replace(printedVersionGo("0.0.0-dev"), "fmt.Println(version)", "fmt.Println(version, 1)", 1))
	drifted, err := CaptureSourceFingerprint(dir)
	require.NoError(t, err)
	assert.NotEqual(t, before.Digest, drifted.Digest)
}

func TestCaptureSourceFingerprintStableAcrossModuleRewriteAndVersionStamp(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(`{"api_name":"sendfox","cli_name":"sendfox-pp-cli"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module sendfox-pp-cli\n\ngo 1.26\n"), 0o644))
	writeGoFile(t, filepath.Join(dir, "internal", "cli", "version.go"), runtimeVersionGo("0.0.0-dev"))
	writeGoFile(t, filepath.Join(dir, "internal", "cli", "root.go"), "package cli\n\nimport _ \"sendfox-pp-cli/internal/client\"\n\nfunc root() {}\n")

	before, err := CaptureSourceFingerprint(dir)
	require.NoError(t, err)
	require.NoError(t, RewriteModulePath(dir, "sendfox-pp-cli", "github.com/mvanhorn/printing-press-library/library/marketing/sendfox"))
	writeGoFile(t, filepath.Join(dir, "internal", "cli", "version.go"), runtimeVersionGo("1.0.0"))
	after, err := CaptureSourceFingerprint(dir)
	require.NoError(t, err)
	assert.Equal(t, before.Digest, after.Digest)

	writeGoFile(t, filepath.Join(dir, "internal", "cli", "root.go"), "package cli\n\nimport _ \"github.com/mvanhorn/printing-press-library/library/marketing/sendfox/internal/client\"\n\nfunc root() { println(1) }\n")
	drifted, err := CaptureSourceFingerprint(dir)
	require.NoError(t, err)
	assert.NotEqual(t, after.Digest, drifted.Digest)
}

func fingerprintFixture(t *testing.T, files map[string]string) SourceFingerprint {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		writeGoFile(t, filepath.Join(dir, filepath.FromSlash(rel)), body)
	}
	fp, err := CaptureSourceFingerprint(dir)
	require.NoError(t, err)
	return fp
}

func runtimeVersionGo(lit string) string {
	return "package cli\n\n// version is the printed CLI's version, overridable at build time via ldflags.\nvar version = \"" + lit + "\"\n\nfunc newVersionCmd() { println(version) }\n"
}

func mcpVersionGo(lit string) string {
	return "package main\n\nvar version = \"" + lit + "\"\n\nfunc main() { println(version) }\n"
}

func printedVersionGo(lit string) string {
	return "package cli\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/spf13/cobra\"\n)\n\n// version is the printed CLI's version, overridable at build time via ldflags.\nvar version = \"" + lit + "\"\n\nfunc newVersionCmd() {\n\tfmt.Println(version)\n\t_ = cobra.Command{}\n}\n"
}

func printedMCPVersionGo(lit string) string {
	return "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\n// version is the printed MCP server's version, overridable at build time via ldflags.\nvar version = \"" + lit + "\"\n\nfunc main() { fmt.Fprintln(os.Stderr, version) }\n"
}

func phase5PassMarker(manifest CLIManifest, digest string, files map[string]string) Phase5GateMarker {
	return Phase5GateMarker{
		SchemaVersion:     1,
		APIName:           manifest.APIName,
		RunID:             manifest.RunID,
		Status:            "pass",
		Level:             "full",
		MatrixSize:        1,
		TestsPassed:       1,
		SourceFingerprint: digest,
		SourceFiles:       files,
		AuthContext:       Phase5AuthContext{Type: manifest.AuthType},
	}
}

func TestCaptureSourceFingerprintRejectsSymlinkedRoot(t *testing.T) {
	target := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(target, "main.go"), []byte("package main\n"), 0o644))
	link := filepath.Join(t.TempDir(), "cli")
	require.NoError(t, os.Symlink(target, link))

	_, err := CaptureSourceFingerprint(link)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be a symlink")
}
