package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClient_VerifyShortCircuit pins the verify-mode HTTP-verb
// short-circuit contract emitted into every generated client.go.
//
// The contract exists because the AGENTS.md "Side-effect commands" rule
// requires a defense-in-depth gate that catches anything the verifier's
// heuristic classifier misses. Mutating HTTP verbs (DELETE/POST/PUT/
// PATCH) under PRINTING_PRESS_VERIFY=1 must short-circuit with a
// synthetic envelope unless PRINTING_PRESS_VERIFY_LIVE_HTTP=1 opts back
// in. A future template edit that drops the gate, narrows it to fewer
// verbs, or removes either env-var check would silently re-open the
// readiness gap; this test fails on any of those drifts.
func TestClient_VerifyShortCircuit(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("verify-short-circuit")
	outputDir := filepath.Join(t.TempDir(), "verify-short-circuit-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	emitted := string(src)

	// Helper: isMutatingVerb covers exactly the four mutating verbs.
	assert.Contains(t, emitted, "func isMutatingVerb(method string) bool",
		"client.go should define an isMutatingVerb helper at file scope")
	for _, verb := range []string{`"DELETE"`, `"POST"`, `"PUT"`, `"PATCH"`} {
		assert.Contains(t, emitted, verb,
			"isMutatingVerb should enumerate %s as a mutating verb", verb)
	}

	// The short-circuit gate inside doInternal() must consult both helpers,
	// in the order isMutatingVerb -> IsVerifyEnv -> !IsVerifyLiveHTTPEnv.
	// Order matters for short-circuit evaluation: cheapest check first, and
	// the env-var lookups skip when the verb is GET. The leading
	// !readOnlyIntent suppresses the gate for read-only mutating-verb
	// operations (GraphQL queries, JSON-RPC reads, POST-based search).
	gate := "!readOnlyIntent && isMutatingVerb(method) && cliutil.IsVerifyEnv() && !cliutil.IsVerifyLiveHTTPEnv()"
	assert.Contains(t, emitted, gate,
		"doInternal() should gate the short-circuit on !readOnlyIntent && isMutatingVerb && IsVerifyEnv && !IsVerifyLiveHTTPEnv")

	// Synthetic envelope sentinel: downstream consumers (validate-narrative,
	// agent inspections, future verify-mode assertions) key on the namespace
	// -reserved boolean so a real API's "status:noop" cannot be confused
	// for the synthetic short-circuit.
	assert.Contains(t, emitted, `"__pp_verify_synthetic__"`,
		"synthetic envelope should include the namespace-reserved sentinel field")
	assert.Contains(t, emitted, `"verify_short_circuit"`,
		"synthetic envelope's reason field should be the literal verify_short_circuit")

	// Diagnostic prose fields echo back method/path so an operator who
	// inspects the envelope sees exactly which call no-op'd.
	assert.Contains(t, emitted, `"method"`,
		"synthetic envelope should echo the request method")
	assert.Contains(t, emitted, `"path"`,
		"synthetic envelope should echo the request path")

	// Bound the gate inside doInternal(): verify the gate appears between
	// the doInternal() declaration and the next top-level func. A check
	// living outside doInternal() would silently fail to short-circuit
	// real requests.
	doStart := strings.Index(emitted, "func (c *Client) doInternal(")
	require.NotEqual(t, -1, doStart, "client.go must declare Client.doInternal")
	rest := emitted[doStart:]
	nextFunc := strings.Index(rest[1:], "\nfunc ")
	require.NotEqual(t, -1, nextFunc, "client.go should have at least one func after doInternal()")
	doBody := rest[:nextFunc+1]
	assert.Contains(t, doBody, gate,
		"the gate must live INSIDE doInternal(), not in a sibling helper or at file scope")

	// do() and doRead() must both be thin wrappers around doInternal() so
	// the gate-check site stays single. A future edit that inlines the
	// gate into do() would split the source of truth and could drift.
	assert.Contains(t, emitted, "func (c *Client) do(ctx context.Context, method, path string, params map[string]string, body any, headerOverrides map[string]string) (json.RawMessage, int, error) {\n\treturn c.doInternal(ctx, method, path, params, body, headerOverrides, false)\n}",
		"do() must be a thin wrapper passing readOnlyIntent=false to doInternal()")
	assert.Contains(t, emitted, "func (c *Client) doRead(ctx context.Context, method, path string, params map[string]string, body any, headerOverrides map[string]string) (json.RawMessage, int, error) {\n\treturn c.doInternal(ctx, method, path, params, body, headerOverrides, true)\n}",
		"doRead() must be a thin wrapper passing readOnlyIntent=true to doInternal()")

	// Read-only POST surface (GraphQL queries, RPC reads, search-by-POST)
	// must route through doRead so the verify-mode gate does not fire.
	// Without these, any read-only mutating-verb command silently returns
	// a synthetic noop envelope under PRINTING_PRESS_VERIFY=1.
	assert.Contains(t, emitted, "func (c *Client) PostQueryWithParams(",
		"client.go should expose PostQueryWithParams for read-only POST operations")
	assert.Contains(t, emitted, "func (c *Client) PostQueryWithParamsAndHeaders(",
		"client.go should expose PostQueryWithParamsAndHeaders for read-only POST operations")
	assert.Contains(t, emitted, `return c.doRead(ctx, "POST", path, params, body, nil)`,
		"PostQueryWithParams must delegate to doRead so the verify-mode gate is bypassed")
	assert.Contains(t, emitted, `return c.doRead(ctx, "POST", path, params, body, headers)`,
		"PostQueryWithParamsAndHeaders must delegate to doRead so the verify-mode gate is bypassed")
}
