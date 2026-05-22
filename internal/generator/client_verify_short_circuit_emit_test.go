package generator

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmitted_ClientVerifyShortCircuitTest pins the contract that every
// regenerated CLI ships with a client_verify_short_circuit_test.go in
// its internal/client package, exercising the transport-layer verify
// short-circuit at the printed-CLI level. Without this pin, a future
// template edit that silently drops the emitted file (or weakens its
// assertions) would let each downstream CLI's go-test pass even when
// verify-mode behavior regresses.
func TestEmitted_ClientVerifyShortCircuitTest(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("emit-verify-test")
	outputDir := filepath.Join(t.TempDir(), "emit-verify-test-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client_verify_short_circuit_test.go"))
	require.NoError(t, err, "client_verify_short_circuit_test.go must be emitted into every printed CLI")
	emitted := string(src)

	// Package declaration must match the host package so the test can
	// call the unexported do() method.
	assert.Contains(t, emitted, "package client",
		"emitted test file must be in package client")

	// Four canonical test functions cover the gate's four states:
	// mutating-verb short-circuit, LIVE_HTTP opt-in, no-env operator path,
	// and GET control case.
	for _, fn := range []string{
		"func TestClient_VerifyShortCircuit_MutatingVerbs",
		"func TestClient_VerifyShortCircuit_LiveHTTPOptIn",
		"func TestClient_VerifyShortCircuit_NoEnv",
		"func TestClient_VerifyShortCircuit_GETControl",
	} {
		assert.Contains(t, emitted, fn,
			"emitted test must define %s to cover its gate state", fn)
	}

	// Recording-mock infrastructure must be present so the gate's
	// "no-network-call" assertion is observable.
	assert.Contains(t, emitted, "type recordingRoundTripper struct",
		"emitted test must define the recording RoundTripper helper")
	assert.Contains(t, emitted, "func (r *recordingRoundTripper) RoundTrip(req *http.Request)",
		"recordingRoundTripper must implement http.RoundTripper")

	// All four mutating verbs are enumerated in the verbs-loop test.
	for _, verb := range []string{`"DELETE"`, `"POST"`, `"PUT"`, `"PATCH"`} {
		assert.Contains(t, emitted, verb,
			"mutating-verbs subtest must enumerate %s", verb)
	}

	// Envelope-shape assertions are present: the synthetic sentinel field
	// plus the reason literal. Drift on either is a contract regression.
	assert.Contains(t, emitted, `"__pp_verify_synthetic__"`,
		"emitted test must assert on the __pp_verify_synthetic__ sentinel")
	assert.Contains(t, emitted, `"verify_short_circuit"`,
		"emitted test must assert on the verify_short_circuit reason literal")

	// Final guard: the emitted file must be syntactically valid Go.
	// Strings-only assertions above can pass against malformed source;
	// parsing the file catches template-syntax errors that survive the
	// content checks.
	_, parseErr := parser.ParseFile(token.NewFileSet(), "client_verify_short_circuit_test.go", emitted, parser.AllErrors)
	require.NoError(t, parseErr, "emitted test file must be syntactically valid Go")
}
