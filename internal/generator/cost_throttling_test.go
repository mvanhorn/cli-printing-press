package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateNoCostThrottlingByteCompat guards PR-3's byte-compat promise:
// a spec that doesn't opt into Throttling.Enabled must regenerate without
// throttle.go, without the --throttle-mode flag, and without any
// Throttle/ThrottleMode references in client.go or graphql.go. Linear is
// the canonical existing GraphQL CLI; loops/stytch/clerk cover the REST
// path through TestGenerateProjectsCompile.
func TestGenerateNoCostThrottlingByteCompat(t *testing.T) {
	t.Parallel()

	gqlSpec, err := graphql.ParseSDL(filepath.Join("..", "..", "testdata", "graphql", "test.graphql"))
	require.NoError(t, err)
	require.False(t, gqlSpec.HasCostThrottling(),
		"GraphQL fixture must keep cost throttling off for the byte-compat case")

	outputDir := filepath.Join(t.TempDir(), naming.CLI(gqlSpec.Name))
	gen := New(gqlSpec, outputDir)
	require.NoError(t, gen.Generate())

	_, err = os.Stat(filepath.Join(outputDir, "internal", "client", "throttle.go"))
	assert.True(t, os.IsNotExist(err),
		"throttle.go must NOT be emitted when Throttling.Enabled is unset; got err=%v", err)

	clientGoBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientGo := string(clientGoBytes)
	assert.NotContains(t, clientGo, "Throttle",
		"client.go must not reference Throttle when throttling is off")
	assert.NotContains(t, clientGo, "ThrottleMode",
		"client.go must not reference ThrottleMode when throttling is off")

	graphqlGoBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "graphql.go"))
	require.NoError(t, err)
	graphqlGo := string(graphqlGoBytes)
	assert.NotContains(t, graphqlGo, "queryWithThrottle",
		"graphql.go must keep its byte-identical Query path when throttling is off")
	assert.NotContains(t, graphqlGo, "updateThrottleFromBody",
		"graphql.go must not emit the cost parser when throttling is off")
	// Negative on the bare-Throttle identifier as well; the throttle-aware
	// helpers all start with that prefix, so any reference would be a
	// regression on the byte-compat promise.
	assert.NotContains(t, graphqlGo, "c.Throttle",
		"graphql.go must not reference c.Throttle when throttling is off")

	rootGoBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	rootGo := string(rootGoBytes)
	assert.NotContains(t, rootGo, "throttle-mode",
		"root.go must not register --throttle-mode when throttling is off")
	assert.NotContains(t, rootGo, "throttleMode",
		"rootFlags must not carry throttleMode when throttling is off")
}

// TestGenerateCostThrottlingPrimitives covers PR-3's contract end-to-end:
// when a GraphQL spec opts into Throttling.Enabled, the generator emits
// throttle.go, the --throttle-mode flag, and the Client throttle field.
// The test compiles the generated tree and runs an injected behavioral
// test that exercises every required path — !seeded fast path, project-
// forward bucket math, 429 + THROTTLED detection, max-retry cap, and
// concurrent update under -race. Mirrors the inject-and-go-test pattern
// PR-2 used in TestGenerateEndpointTemplateVarsRuntimeSubstitution.
func TestGenerateCostThrottlingPrimitives(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:                "shoppy",
		Description:         "Cost-throttled GraphQL fixture",
		Version:             "1.0.0",
		BaseURL:             "https://api.shoppy.example",
		GraphQLEndpointPath: "/admin/api/graphql.json",
		Throttling:          spec.ThrottlingConfig{Enabled: true, Shape: spec.ThrottleShapeShopify},
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "X-API-Token",
			EnvVars: []string{"SHOPPY_API_TOKEN"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/shoppy-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"orders": {
				Description: "Orders",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:       "GET",
						Path:         "/graphql",
						Description:  "List orders",
						ResponsePath: "data.orders.nodes",
						Pagination: &spec.Pagination{
							Type:           "cursor",
							LimitParam:     "first",
							CursorParam:    "after",
							NextCursorPath: "data.orders.pageInfo.endCursor",
							HasMoreField:   "data.orders.pageInfo.hasNextPage",
						},
						Response: spec.ResponseDef{Type: "array", Item: "Order"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Order": {Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "name", Type: "string"},
			}},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	// throttle.go must exist; this is where ThrottleState lives.
	throttleGoPath := filepath.Join(outputDir, "internal", "client", "throttle.go")
	throttleBytes, err := os.ReadFile(throttleGoPath)
	require.NoError(t, err, "throttle.go should be generated when Throttling.Enabled is set")
	throttle := string(throttleBytes)
	assert.Contains(t, throttle, "type ThrottleState struct",
		"throttle.go must declare ThrottleState")
	assert.Contains(t, throttle, "WaitForBudget",
		"throttle.go must declare WaitForBudget")
	assert.Contains(t, throttle, "HandleThrottleError",
		"throttle.go must declare HandleThrottleError")

	// client.go must carry the Throttle field and instantiate it.
	clientGoBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientGo := string(clientGoBytes)
	assert.Contains(t, clientGo, "Throttle *ThrottleState",
		"client.go must declare the Throttle field")
	assert.Contains(t, clientGo, "ThrottleMode: ThrottleModeStrict",
		"client.go must default ThrottleMode to strict in New()")

	// graphql.go must wire updateThrottleFromBody and queryWithThrottle.
	graphqlGoBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "graphql.go"))
	require.NoError(t, err)
	graphqlGo := string(graphqlGoBytes)
	assert.Contains(t, graphqlGo, "queryWithThrottle",
		"graphql.go must use queryWithThrottle when throttling is enabled")
	assert.Contains(t, graphqlGo, "updateThrottleFromBody",
		"graphql.go must parse extensions.cost via updateThrottleFromBody")
	assert.Contains(t, graphqlGo, "throttleStatus",
		"graphql.go cost parser must read throttleStatus")

	// root.go must register --throttle-mode and validate its value.
	rootGoBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	rootGo := string(rootGoBytes)
	assert.Contains(t, rootGo, "--throttle-mode",
		"root.go must register the --throttle-mode flag (mentioned in --help)")
	assert.Contains(t, rootGo, `"strict", "lenient", "off"`,
		"root.go must validate --throttle-mode against the enum")
	assert.Contains(t, rootGo, "c.ThrottleMode = f.throttleMode",
		"root.go must propagate --throttle-mode to the client")

	// Compile before injecting tests so a syntax error in throttle.go.tmpl
	// surfaces here, not as a confusing test runner failure.
	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")

	// Behavioral tests injected into the generated client package. Verify
	// every acceptance path from PR-3 against real generated code:
	//   (a) WaitForBudget returns immediately when !seeded
	//   (b) computeWait projects forward (status 100/1000, restoreRate 50,
	//       requested 800 after 5s elapsed → projected 350, deficit 450,
	//       sleep 9s)
	//   (c) HandleThrottleError detects 429 with Retry-After
	//   (d) HandleThrottleError detects GraphQL extensions code "THROTTLED"
	//   (e) HandleThrottleError caps at MaxThrottleRetries
	//   (f) Concurrent Update calls are race-free (run under -race below)
	const behaviorTest = `package client

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestThrottleWaitForBudgetUnseededReturnsImmediately(t *testing.T) {
	state := NewThrottleState()
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := state.WaitForBudget(ctx, 100); err != nil {
		t.Fatalf("unexpected error from unseeded WaitForBudget: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("unseeded WaitForBudget should return immediately; took %s", elapsed)
	}
}

func TestThrottleComputeWaitProjectsForward(t *testing.T) {
	state := NewThrottleState()
	state.Update(ThrottleStatus{
		MaximumAvailable:   1000,
		CurrentlyAvailable: 100,
		RestoreRate:        50,
	})
	// Force the LastUpdated stamp 5s into the past so the projection has
	// observable elapsed time without sleeping the test.
	state.mu.Lock()
	state.status.LastUpdated = time.Now().Add(-5 * time.Second)
	state.mu.Unlock()

	got := state.computeWait(time.Now(), 800)
	const want = 9 * time.Second
	const tolerance = 100 * time.Millisecond
	if got < want-tolerance || got > want+tolerance {
		t.Fatalf("computeWait: got %s, want ~%s (projected 100 + 5*50 = 350; deficit 450; 450/50 = 9s)", got, want)
	}
}

func TestThrottleComputeWaitZeroWhenProjectedExceedsRequested(t *testing.T) {
	state := NewThrottleState()
	state.Update(ThrottleStatus{
		MaximumAvailable:   1000,
		CurrentlyAvailable: 900,
		RestoreRate:        50,
	})
	if got := state.computeWait(time.Now(), 100); got != 0 {
		t.Fatalf("expected 0 wait when projected balance covers cost; got %s", got)
	}
}

func TestThrottleHandleErrorDetects429WithRetryAfter(t *testing.T) {
	state := NewThrottleState()
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
	}
	resp.Header.Set("Retry-After", "3")

	retry, wait := state.HandleThrottleError(resp, nil, 0)
	if !retry {
		t.Fatal("HandleThrottleError must request retry on 429")
	}
	if wait < 3*time.Second {
		t.Fatalf("HandleThrottleError must honor Retry-After (3s); got %s", wait)
	}
}

func TestThrottleHandleErrorDetectsGraphQLThrottledExtension(t *testing.T) {
	state := NewThrottleState()
	body := []byte(` + "`" + `{"errors":[{"message":"Throttled","extensions":{"code":"THROTTLED"}}]}` + "`" + `)
	retry, wait := state.HandleThrottleError(nil, body, 0)
	if !retry {
		t.Fatal("HandleThrottleError must request retry on THROTTLED extension code")
	}
	if wait <= 0 {
		t.Fatalf("HandleThrottleError must return a positive wait for THROTTLED; got %s", wait)
	}
}

func TestThrottleHandleErrorCapsAtMaxRetries(t *testing.T) {
	state := NewThrottleState()
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}
	if retry, _ := state.HandleThrottleError(resp, nil, MaxThrottleRetries); retry {
		t.Fatalf("HandleThrottleError must stop retrying at attempt=MaxThrottleRetries (%d)", MaxThrottleRetries)
	}
}

func TestThrottleReserveBudgetDebitsConcurrentCallers(t *testing.T) {
	// Codex P2 from PR-3 review: WaitForBudget without reservation lets
	// every concurrent caller read the same projected balance and race
	// through. reserveBudget is the fix — under one mutex, project AND
	// debit. Two callers asking for 50 against a 100/1000 bucket
	// (restoreRate 50) must each be admitted with no wait, but the
	// third caller arriving immediately after must wait because the
	// local bucket is now drained.
	state := NewThrottleState()
	state.Update(ThrottleStatus{
		MaximumAvailable:   1000,
		CurrentlyAvailable: 100,
		RestoreRate:        50,
	})
	now := time.Now()
	if got := state.reserveBudget(now, 50); got != 0 {
		t.Fatalf("first reserveBudget(50) on 100-bucket must not wait; got %s", got)
	}
	if got := state.reserveBudget(now, 50); got != 0 {
		t.Fatalf("second reserveBudget(50) on the now-50 bucket must not wait; got %s", got)
	}
	// The third concurrent call lands on a debited-to-zero bucket. With
	// negligible elapsed time, projected ≈ 0, deficit = 50, sleep = 1s.
	got := state.reserveBudget(now, 50)
	const want = time.Second
	if got < want-50*time.Millisecond || got > want+50*time.Millisecond {
		t.Fatalf("third reserveBudget after two debits must wait ~1s for the bucket to refill; got %s", got)
	}
}

func TestThrottleConcurrentUpdateNoRace(t *testing.T) {
	// This test is meaningful when run under -race; the bare exec also
	// verifies the mutex shape is correct (no deadlock).
	state := NewThrottleState()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			state.Update(ThrottleStatus{
				MaximumAvailable:   1000,
				CurrentlyAvailable: float64(i),
				RestoreRate:        50,
			})
			_ = state.computeWait(time.Now(), 100)
			_, _ = state.Status()
		}(i)
	}
	wg.Wait()
}

func TestThrottleModeOffSkipsAwaitBudget(t *testing.T) {
	// awaitBudget is the integration glue used by queryWithThrottle.
	// Off-mode must short-circuit even when the bucket is exhausted, so
	// the call returns immediately regardless of computeWait's verdict.
	c := &Client{Throttle: NewThrottleState(), ThrottleMode: ThrottleModeOff}
	c.Throttle.Update(ThrottleStatus{
		MaximumAvailable:   1000,
		CurrentlyAvailable: 0,
		RestoreRate:        1,
	})
	c.Throttle.mu.Lock()
	c.Throttle.status.LastUpdated = time.Now()
	c.Throttle.mu.Unlock()

	start := time.Now()
	if err := c.awaitBudget(context.Background(), 999); err != nil {
		t.Fatalf("off-mode awaitBudget should never error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("off-mode awaitBudget should not block; took %s", elapsed)
	}
}

func TestUpdateThrottleFromBodyParsesShopifyShape(t *testing.T) {
	c := &Client{Throttle: NewThrottleState()}
	body := []byte(` + "`" + `{"data":{},"extensions":{"cost":{"requestedQueryCost":10,"actualQueryCost":7,"throttleStatus":{"maximumAvailable":1000,"currentlyAvailable":993,"restoreRate":50}}}}` + "`" + `)
	if !c.updateThrottleFromBody(body) {
		t.Fatal("updateThrottleFromBody must consume Shopify's extensions.cost.throttleStatus shape")
	}
	status, seeded := c.Throttle.Status()
	if !seeded {
		t.Fatal("Throttle must be seeded after updateThrottleFromBody")
	}
	if status.MaximumAvailable != 1000 || status.CurrentlyAvailable != 993 || status.RestoreRate != 50 {
		t.Fatalf("unexpected status %+v", status)
	}
}

func TestUpdateThrottleFromBodyNoOpsWithoutCost(t *testing.T) {
	// APIs that don't return extensions.cost (Linear, GitHub via the
	// non-cost rateLimit node, vanilla GraphQL servers) must be a silent
	// no-op. This is the defensive guard that lets ANY GraphQL spec opt
	// in without breaking when extensions.cost isn't in the response.
	c := &Client{Throttle: NewThrottleState()}
	if c.updateThrottleFromBody([]byte(` + "`" + `{"data":{"viewer":{"id":"x"}}}` + "`" + `)) {
		t.Fatal("updateThrottleFromBody must no-op when extensions.cost is missing")
	}
	if _, seeded := c.Throttle.Status(); seeded {
		t.Fatal("Throttle must remain unseeded when extensions.cost is missing")
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "client", "throttle_behavior_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(behaviorTest), 0o644))

	// First plain test pass to verify all paths.
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestThrottle|TestUpdateThrottle")
	// Then a -race pass focused on the concurrent test to prove the mutex
	// is correct under the race detector. -race is opt-in elsewhere in
	// this suite (PR-2 didn't run it); the mutex is the explicit V3 fix
	// from V2's sync.Map shape, so we exercise it directly.
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestThrottleConcurrentUpdateNoRace", "-race")
}
