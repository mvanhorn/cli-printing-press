package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCachePolicyStaleAfterEmittedDirectly covers issue #1944. A spec-declared
// stale_after literal must be emitted as a direct Go expression. The old
// template emitted `staleAfter := 6 * time.Hour` then overwrote it via a
// ParseDuration call that cannot fail on a constant literal — dead code that
// Greptile flags on every publish.
func TestCachePolicyStaleAfterEmittedDirectly(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("staledirect")
	apiSpec.Cache = spec.CacheConfig{
		Enabled:    true,
		StaleAfter: "168h",
		Commands: []spec.CacheCommand{
			{Name: "dashboard", Resources: []string{"items"}},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auto_refresh.go"))
	require.NoError(t, err)
	content := string(src)

	assert.Contains(t, content, "staleAfter := 168 * time.Hour",
		"parseable stale_after must initialize staleAfter directly")
	assert.NotContains(t, content, "staleAfter := 6 * time.Hour",
		"the dead 6h default must not be emitted when stale_after is set")
	assert.NotContains(t, content, "staleAfter = d",
		"the dead ParseDuration override of staleAfter must be removed")
}

// TestStaleAfterExpr pins the Go-expression rendering, including the
// type-safety requirement that every result is a time.Duration-typed
// expression (a bare "0" would make staleAfter an int and fail to compile).
func TestStaleAfterExpr(t *testing.T) {
	t.Parallel()

	cases := []struct {
		lit  string
		want string
	}{
		{"", "6 * time.Hour"},
		{"garbage", "6 * time.Hour"},
		{"-1h", "6 * time.Hour"}, // negative would mean a permanently-stale cache
		{"168h", "168 * time.Hour"},
		{"30m", "30 * time.Minute"},
		{"45s", "45 * time.Second"},
		{"1500ms", "time.Duration(1500000000)"},
		{"0s", "0 * time.Hour"}, // must stay time.Duration-typed, never bare "0"
	}
	for _, tc := range cases {
		t.Run(tc.lit, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, staleAfterExpr(tc.lit))
		})
	}
}

// TestCachePolicyStaleAfterDefaultPreserved pins that an absent stale_after
// still falls back to the 6h default (behavior preserved).
func TestCachePolicyStaleAfterDefaultPreserved(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("staledefault")
	apiSpec.Cache = spec.CacheConfig{
		Enabled: true,
		Commands: []spec.CacheCommand{
			{Name: "dashboard", Resources: []string{"items"}},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auto_refresh.go"))
	require.NoError(t, err)
	content := string(src)

	assert.Contains(t, content, "staleAfter := 6 * time.Hour",
		"absent stale_after must default to 6h")
	assert.NotContains(t, content, "staleAfter = d",
		"no ParseDuration override should be emitted for the default case")
}
