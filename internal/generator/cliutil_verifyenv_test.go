package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCliutilVerifyEnvTemplateEmitsHarnessHelpers asserts the rendered
// cliutil package exposes the verify helpers plus the harness helpers with
// their canonical env-var names. This pins the generator's contract so a future
// template edit cannot silently drop or rename helpers that the transport
// short-circuit and visible side-effect refusals depend on.
func TestCliutilVerifyEnvTemplateEmitsHarnessHelpers(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("verifyenv-helpers")
	outputDir := filepath.Join(t.TempDir(), "verifyenv-helpers-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cliutil", "verifyenv.go"))
	require.NoError(t, err)
	emitted := string(src)

	assert.Contains(t, emitted, `const VerifyEnvVar = "PRINTING_PRESS_VERIFY"`,
		"existing VerifyEnvVar constant should still be emitted")
	assert.Contains(t, emitted, `const VerifyLiveHTTPEnvVar = "PRINTING_PRESS_VERIFY_LIVE_HTTP"`,
		"new VerifyLiveHTTPEnvVar constant should be emitted with its canonical name")
	assert.Contains(t, emitted, `const DogfoodEnvVar = "PRINTING_PRESS_DOGFOOD"`,
		"DogfoodEnvVar constant should be emitted with its canonical name")
	assert.Contains(t, emitted, `type Harness string`,
		"typed harness names should be emitted")
	assert.Contains(t, emitted, `HarnessVerify  Harness = "verify"`,
		"verify harness name should stay stable for suppression output")
	assert.Contains(t, emitted, `HarnessDogfood Harness = "dogfood"`,
		"dogfood harness name should stay stable for suppression output")
	assert.Contains(t, emitted, "func IsVerifyEnv() bool",
		"existing IsVerifyEnv function should still be emitted")
	assert.Contains(t, emitted, "func IsVerifyLiveHTTPEnv() bool",
		"new IsVerifyLiveHTTPEnv function should be emitted")
	assert.Contains(t, emitted, "func CurrentHarness() Harness",
		"CurrentHarness function should be emitted")
	assert.Contains(t, emitted, "func HarnessName() string",
		"HarnessName function should be emitted for user-facing suppression output")
	assert.Contains(t, emitted, "func IsAnyHarness() bool",
		"IsAnyHarness function should be emitted for visible side-effect refusal gates")
	assert.Contains(t, emitted, `os.Getenv(VerifyLiveHTTPEnvVar) == "1"`,
		"helper should treat only the literal string \"1\" as truthy, matching IsVerifyEnv's contract")
	assert.Contains(t, emitted, `return CurrentHarness() != HarnessNone`,
		"IsAnyHarness should cover both verify and dogfood via CurrentHarness")

	// Docstring widening: the file-level comment block should mention the
	// transport-layer use case so an external reader who hits this file
	// understands both gates without having to read client.go.
	assert.True(t,
		strings.Contains(emitted, "DELETE/POST/PUT/PATCH") ||
			strings.Contains(emitted, "mutating HTTP verbs"),
		"docstring should document the new transport-layer scope (DELETE/POST/PUT/PATCH or 'mutating HTTP verbs')")

	requireGeneratedCompiles(t, outputDir)
}

// TestIsVerifyLiveHTTPEnv_OnlyOneIsTruthy mirrors the IsVerifyEnv
// truthiness contract: the helper returns true ONLY for the literal
// string "1". Common alternative truthy values (true, yes, 2, empty)
// must return false so the gate behavior is unambiguous across shells
// and CI runners that interpret env-var truthiness differently.
func TestIsVerifyLiveHTTPEnv_OnlyOneIsTruthy(t *testing.T) {
	apiSpec := minimalSpec("verifyenv-truthiness")
	outputDir := filepath.Join(t.TempDir(), "verifyenv-truthiness-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cliutil", "verifyenv.go"))
	require.NoError(t, err)
	emitted := string(src)

	// The helper body is a one-liner. Asserting on its exact shape is
	// the simplest way to pin the truthiness contract without compiling
	// and executing the emitted package from this generator-side test.
	assert.Contains(t, emitted,
		`func IsVerifyLiveHTTPEnv() bool {
	return os.Getenv(VerifyLiveHTTPEnvVar) == "1"
}`,
		"IsVerifyLiveHTTPEnv body should be the canonical one-liner that treats only \"1\" as truthy")
}

func TestGeneratedHarnessHelpersAndRefusalOutput(t *testing.T) {
	apiSpec := minimalSpec("harness-refusal")
	outputDir := filepath.Join(t.TempDir(), "harness-refusal-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const cliutilRuntimeTest = `package cliutil

import "testing"

func TestCurrentHarnessPrecedenceAndTruthiness(t *testing.T) {
	if got := CurrentHarness(); got != HarnessNone {
		t.Fatalf("CurrentHarness() = %q, want none", got)
	}
	t.Setenv(VerifyEnvVar, "true")
	if got := IsAnyHarness(); got {
		t.Fatalf("IsAnyHarness() = true for non-canonical verify value")
	}
	t.Setenv(VerifyEnvVar, "1")
	t.Setenv(DogfoodEnvVar, "1")
	if got := CurrentHarness(); got != HarnessVerify {
		t.Fatalf("CurrentHarness() = %q, want verify precedence", got)
	}
	t.Setenv(VerifyEnvVar, "")
	if got := CurrentHarness(); got != HarnessDogfood {
		t.Fatalf("CurrentHarness() = %q, want dogfood", got)
	}
	if got := HarnessName(); got != "dogfood" {
		t.Fatalf("HarnessName() = %q, want dogfood", got)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cliutil", "harness_runtime_test.go"), []byte(cliutilRuntimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cliutil", "-run", "TestCurrentHarness")

	const cliRuntimeTest = `package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"harness-refusal-pp-cli/internal/cliutil"
)

func TestWriteHarnessRefusalHonorsJSONOutput(t *testing.T) {
	t.Setenv(cliutil.DogfoodEnvVar, "1")
	var out bytes.Buffer
	if err := writeHarnessRefusal(&out, &rootFlags{asJSON: true}, "broadcast audio"); err != nil {
		t.Fatalf("writeHarnessRefusal() error = %v", err)
	}
	var payload struct {
		Refused bool   ` + "`json:\"refused\"`" + `
		Harness string ` + "`json:\"harness\"`" + `
		Action  string ` + "`json:\"action\"`" + `
		Reason  string ` + "`json:\"reason\"`" + `
		Would   string ` + "`json:\"would\"`" + `
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("refusal output is not JSON: %v; output=%q", err, out.String())
	}
	if !payload.Refused || payload.Harness != "dogfood" || payload.Action != "broadcast audio" {
		t.Fatalf("refusal payload = %+v", payload)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "harness_refusal_runtime_test.go"), []byte(cliRuntimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestWriteHarnessRefusal")
	requireGeneratedCompiles(t, outputDir)
}
