// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

var nonLiteralExecCommand = regexp.MustCompile(`exec\.Command\([^"\n]`)

func TestGeneratedCookieAuthHardening(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "cookiehardening-pp-cli")
	require.NoError(t, New(chromeChannelSpec("cookiehardening"), outputDir).Generate())

	authGo := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	doctorGo := readGeneratedFile(t, outputDir, "internal", "cli", "doctor.go")
	deliverGo := readGeneratedFile(t, outputDir, "internal", "cli", "deliver.go")

	require.NotRegexp(t, nonLiteralExecCommand, authGo)
	require.NotContains(t, doctorGo, "exec.Command(")
	require.NotContains(t, authGo, `os.CreateTemp("", "cookies-probe-*.db")`)
	require.NotContains(t, authGo, "os.Create(dst)")
	require.Contains(t, authGo, `os.MkdirTemp("", "pp-cookie-probe-")`)
	require.Contains(t, authGo, "os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600")
	require.Contains(t, authGo, "func runPythonFile(")
	require.Contains(t, authGo, "func execNamed(")
	require.NotContains(t, authGo, `"-c", script`)
	require.NotContains(t, authGo, `"-c", "import pycookiecheat"`)

	require.Contains(t, doctorGo, "detectCookieTool()")
	require.Contains(t, doctorGo, "func doctorIsInfoKey(")
	require.Contains(t, doctorGo, "strings.ToLower(strings.TrimSpace(failOn))")
	require.Contains(t, doctorGo, `"auth_hint":`)

	require.Contains(t, deliverGo, "func safeJoinUnder(")
	require.Contains(t, deliverGo, "func writeDownloadUnder(")
	require.Contains(t, deliverGo, "os.WriteFile(tmp, body, 0o600)")
	require.Contains(t, deliverGo, "os.MkdirAll(dir, 0o700)")

	if exportGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "export.go")); err == nil {
		require.NotContains(t, string(exportGo), "os.Create(outputFile)")
		require.Contains(t, string(exportGo), "func openExportOutput(")
		require.Contains(t, string(exportGo), "f.Chmod(0o600)")
		require.Contains(t, string(exportGo), "os.MkdirAll(dir, 0o700)")
	}
	require.Contains(t, deliverGo, "return writeDownloadUnder(dir, filepath.Base(path), body)")

	requireGeneratedCompiles(t, outputDir)

	const runtime = `package cli

import "testing"

func TestDoctorFailOnCaseAndInfoKeys(t *testing.T) {
	if err := doctorExitForFailOn("ERROR", map[string]any{"auth": "ERROR token expired at now"}); err == nil {
		t.Fatal("uppercase ERROR gate missed an ERROR verdict")
	}
	if err := doctorExitForFailOn("error", map[string]any{"auth_hint": "client id is missing; set it"}); err != nil {
		t.Fatalf("info hint tripped --fail-on=error: %v", err)
	}
	if err := doctorExitForFailOn("Error", map[string]any{"credentials": "rejected — session expired"}); err == nil {
		t.Fatal("rejected credential did not trip --fail-on=error")
	}
	if err := doctorExitForFailOn("WARN", map[string]any{"paths_warning": "WARN paths: relative override skipped"}); err == nil {
		t.Fatal("WARN gate missed a WARN section")
	}
	if err := doctorExitForFailOn("error", map[string]any{"paths_warning": "WARN paths: relative override skipped"}); err != nil {
		t.Fatalf("WARN text tripped --fail-on=error: %v", err)
	}
	graphqlWarn := "WARN not verified (HTTP 404 from GraphQL probe) — auth was neither accepted nor rejected; set auth.verify_query to a known-good viewer query"
	if err := doctorExitForFailOn("error", map[string]any{"credentials": graphqlWarn}); err != nil {
		t.Fatalf("graphql non-auth WARN tripped --fail-on=error: %v", err)
	}
	if err := doctorExitForFailOn("stale", map[string]any{"credentials": graphqlWarn}); err != nil {
		t.Fatalf("graphql non-auth WARN tripped --fail-on=stale: %v", err)
	}
	if err := doctorExitForFailOn("warn", map[string]any{"credentials": graphqlWarn}); err == nil {
		t.Fatal("graphql non-auth WARN did not trip --fail-on=warn")
	}
	if err := doctorExitForFailOn("error", map[string]any{"credentials": "rejected — GraphQL response contained top-level errors"}); err == nil {
		t.Fatal("graphql rejected payload did not trip --fail-on=error")
	}
	if err := doctorExitForFailOn("stale", map[string]any{"credentials": "rejected — GraphQL response contained top-level errors"}); err == nil {
		t.Fatal("graphql rejected payload did not trip --fail-on=stale")
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "cookie_hardening_runtime_test.go"), []byte(runtime), 0o600))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-count=1", "-run", "TestDoctorFailOnCaseAndInfoKeys|TestWriteDownloadUnderRejectsEscapeAndUsesPrivateMode")
}
