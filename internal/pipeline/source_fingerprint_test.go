package pipeline

import (
	"os"
	"path/filepath"
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

func TestCaptureSourceFingerprintRejectsSymlinkedRoot(t *testing.T) {
	target := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(target, "main.go"), []byte("package main\n"), 0o644))
	link := filepath.Join(t.TempDir(), "cli")
	require.NoError(t, os.Symlink(target, link))

	_, err := CaptureSourceFingerprint(link)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be a symlink")
}
