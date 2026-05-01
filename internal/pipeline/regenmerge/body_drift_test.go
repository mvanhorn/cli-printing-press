package regenmerge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBodyDriftCatchesCalComShape pins the cal-com regression that motivated
// this probe. Pub's client.go calls a hand-written helper
// (requiredHeadersForPath) inside the templated do() function. Decl-set
// comparison says TEMPLATED-CLEAN because both files have the same set of
// top-level decls. Without the body-drift probe, take-fresh-wholesale would
// silently drop the helper integration and break per-endpoint API
// versioning at runtime.
func TestBodyDriftCatchesCalComShape(t *testing.T) {
	t.Parallel()

	pubCLI := `package client

func do(path string) {
	authHeader := getAuth()
	setHeader("Authorization", authHeader)
	for k, v := range requiredHeadersForPath(path) {
		setHeader(k, v)
	}
}

func getAuth() string  { return "" }
func setHeader(k, v string) {}
`
	freshCLI := `package client

func do(path string) {
	authHeader := getAuth()
	setHeader("Authorization", authHeader)
}

func getAuth() string  { return "" }
func setHeader(k, v string) {}
`
	pubDir, freshDir := buildSyntheticFixture(t,
		map[string]string{"internal/client/client.go": pubCLI},
		map[string]string{"internal/client/client.go": freshCLI})

	report, err := Classify(pubDir, freshDir, Options{Force: true})
	require.NoError(t, err)

	verdicts := verdictMap(report)
	assert.Equal(t, VerdictTemplatedBodyDrift, verdicts["internal/client/client.go"],
		"pub's body calls a helper fresh's body doesn't — must flag drift")

	var fc *FileClassification
	for i := range report.Files {
		if report.Files[i].Path == "internal/client/client.go" {
			fc = &report.Files[i]
			break
		}
	}
	require.NotNil(t, fc.BodyDrift)
	assert.Contains(t, fc.BodyDrift.Functions, "do",
		"do() is the function with drift")
	assert.Contains(t, fc.BodyDrift.Functions["do"], "requiredHeadersForPath",
		"requiredHeadersForPath is the dropped call target")
}

// TestBodyDriftIgnoresAddCommandArgs pins the false-positive guard: the
// lost-registration mechanism handles pub-only AddCommand calls separately,
// so body-drift must not re-flag them. Without this filter, every CLI with
// novel commands would trip body-drift (pub's Execute calls
// newFooCmd/newBarCmd that fresh's Execute doesn't).
func TestBodyDriftIgnoresAddCommandArgs(t *testing.T) {
	t.Parallel()

	pubCLI := `package cli

func Execute() {
	rootCmd := newRoot()
	rootCmd.AddCommand(newFooCmd())
	rootCmd.AddCommand(newBarCmd())
}

func newRoot() *struct{}     { return nil }
func newFooCmd() *struct{}   { return nil }
func newBarCmd() *struct{}   { return nil }
`
	freshCLI := `package cli

func Execute() {
	rootCmd := newRoot()
}

func newRoot() *struct{}     { return nil }
`
	pubDir, freshDir := buildSyntheticFixture(t,
		map[string]string{"internal/cli/root.go": pubCLI},
		map[string]string{"internal/cli/root.go": freshCLI})

	report, err := Classify(pubDir, freshDir, Options{Force: true})
	require.NoError(t, err)

	// Decl-set comparison flags this as TEMPLATED-WITH-ADDITIONS because
	// pub has newFooCmd / newBarCmd that fresh doesn't. The drift probe
	// shouldn't re-flag the AddCommand args separately. (The decl-set
	// mismatch is the load-bearing signal here, not body drift.)
	verdicts := verdictMap(report)
	got := verdicts["internal/cli/root.go"]
	assert.NotEqual(t, VerdictTemplatedBodyDrift, got,
		"AddCommand argument calls must not trigger body drift; lost-registration handles them")
}

// TestBodyDriftCatchesPagliacciShape pins the second regression that
// motivated this probe. Pagliacci-pizza's pub client.go used a Chrome-
// impersonating HTTP client (surf library) inside New() — without it the
// CLI risks Cloudflare blocks. Decl-set comparison says TEMPLATED-CLEAN;
// take-fresh-wholesale dropped the integration silently.
func TestBodyDriftCatchesPagliacciShape(t *testing.T) {
	t.Parallel()

	pubCLI := `package client

func New() *Client {
	builder := surf.Builder().Impersonate().Chrome()
	return &Client{client: builder.Build()}
}

type Client struct {
	client interface{}
}
`
	freshCLI := `package client

func New() *Client {
	return &Client{client: defaultHTTPClient()}
}

func defaultHTTPClient() interface{} { return nil }

type Client struct {
	client interface{}
}
`
	pubDir, freshDir := buildSyntheticFixture(t,
		map[string]string{"internal/client/client.go": pubCLI},
		map[string]string{"internal/client/client.go": freshCLI})

	report, err := Classify(pubDir, freshDir, Options{Force: true})
	require.NoError(t, err)

	verdicts := verdictMap(report)
	assert.Equal(t, VerdictTemplatedBodyDrift, verdicts["internal/client/client.go"],
		"chained .Impersonate().Chrome().Build() in pub's New() must flag drift")

	var fc *FileClassification
	for i := range report.Files {
		if report.Files[i].Path == "internal/client/client.go" {
			fc = &report.Files[i]
			break
		}
	}
	require.NotNil(t, fc.BodyDrift)
	driftCalls := fc.BodyDrift.Functions["New"]
	for _, want := range []string{"Builder", "Impersonate", "Chrome"} {
		assert.Contains(t, driftCalls, want,
			"surf-library call %q must surface in drift report", want)
	}
}

// TestBodyDriftClean verifies the no-drift case: same function bodies in
// both files (modulo comments and whitespace) classify as TEMPLATED-CLEAN.
func TestBodyDriftClean(t *testing.T) {
	t.Parallel()

	src := `package cli

func helper(x int) int {
	return doStuff(x) + 1
}

func doStuff(x int) int { return x * 2 }
`
	pubDir, freshDir := buildSyntheticFixture(t,
		map[string]string{"internal/cli/helpers.go": src},
		map[string]string{"internal/cli/helpers.go": src})

	report, err := Classify(pubDir, freshDir, Options{Force: true})
	require.NoError(t, err)

	verdicts := verdictMap(report)
	assert.Equal(t, VerdictTemplatedClean, verdicts["internal/cli/helpers.go"],
		"identical bodies must stay TEMPLATED-CLEAN")
}
