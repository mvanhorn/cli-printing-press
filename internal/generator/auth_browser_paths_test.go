// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedPersistedQueryOutputPathRejectsRelativeBeforeMkdir(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("pq-output-path")
	apiSpec.BaseURL = "https://api.example.com"
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	outputDir := filepath.Join(t.TempDir(), "pq-output-path-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.TrafficAnalysis = &browsersniff.TrafficAnalysis{GenerationHints: []string{"graphql_persisted_query"}}
	require.NoError(t, gen.Generate())

	const runtimeTest = `package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistedQueryRegistryOutputPathRejectsRelativeBeforeMkdir(t *testing.T) {
	cwd := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	defer os.Chdir(old)

	if _, err := persistedQueryRegistryPath("../../../tmp/exfil.json"); err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("persistedQueryRegistryPath(relative) error = %v, want must be absolute", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "relative-sentinel")); !os.IsNotExist(err) {
		t.Fatalf("relative rejection should not create directories, stat err=%v", err)
	}
	if _, err := persistedQueryRegistryPath("relative-sentinel/exfil.json"); err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("persistedQueryRegistryPath(relative sentinel) error = %v, want must be absolute", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "relative-sentinel")); !os.IsNotExist(err) {
		t.Fatalf("relative rejection created a directory before returning, stat err=%v", err)
	}

	abs := filepath.Join(t.TempDir(), "out.json")
	got, err := persistedQueryRegistryPath(abs)
	if err != nil {
		t.Fatalf("persistedQueryRegistryPath(abs) error = %v", err)
	}
	if got != abs {
		t.Fatalf("persistedQueryRegistryPath(abs) = %q, want %q", got, abs)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "persisted_query_path_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestPersistedQueryRegistryOutputPath")

	authSrc := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	callIdx := strings.Index(authSrc, "persistedQueryRegistryPath(outputPath)")
	require.NotEqual(t, -1, callIdx)
	body := authSrc[callIdx:]
	mkdirIdx := strings.Index(body, "os.MkdirAll(filepath.Dir(path)")
	require.NotEqual(t, -1, mkdirIdx)
}
