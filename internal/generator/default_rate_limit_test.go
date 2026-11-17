package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRootDefaultRateLimitEmission pins the --rate-limit default precedence in
// root.go.tmpl: an explicit spec DefaultRateLimit ("auto" or a number) wins;
// empty (sniffed or documented) uses client.RateLimitAuto so the emitted
// default matches the documented sentinel. "auto" and the empty fallback
// relabel the flag's --help default to "auto".
func TestRootDefaultRateLimitEmission(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		def         string
		specSource  string
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "auto",
			def:         "auto",
			wantContain: []string{"var defaultRateLimit = client.RateLimitAuto", `"rate-limit", defaultRateLimit,`, `f.DefValue = "auto"`},
		},
		{
			name:        "numeric",
			def:         "3",
			wantContain: []string{"var defaultRateLimit float64 = 3", `"rate-limit", defaultRateLimit,`},
			wantAbsent:  []string{"client.RateLimitAuto"},
		},
		{
			name:        "empty_sniffed",
			specSource:  "sniffed",
			wantContain: []string{"var defaultRateLimit = client.RateLimitAuto", `"rate-limit", defaultRateLimit,`, `f.DefValue = "auto"`},
			wantAbsent:  []string{`"rate-limit", 2,`, `"rate-limit", 0,`},
		},
		{
			name:        "empty_default",
			wantContain: []string{"var defaultRateLimit = client.RateLimitAuto", `"rate-limit", defaultRateLimit,`, `f.DefValue = "auto"`},
			wantAbsent:  []string{`"rate-limit", 0,`, `"rate-limit", 2,`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec("ratelimit")
			apiSpec.DefaultRateLimit = tc.def
			apiSpec.SpecSource = tc.specSource

			outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
			require.NoError(t, New(apiSpec, outputDir).Generate())

			rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
			for _, want := range tc.wantContain {
				assert.Contains(t, rootSrc, want)
			}
			for _, absent := range tc.wantAbsent {
				assert.NotContains(t, rootSrc, absent)
			}
		})
	}
}

// TestGeneratedTestsDisableRatePacing checks that the emitted TestMain zeroes
// the --rate-limit default, so command tests against httptest stubs are not
// paced by the auto limiter. The shipped default is unchanged.
func TestGeneratedTestsDisableRatePacing(t *testing.T) {
	t.Parallel()

	for _, def := range []string{"", "3"} {
		t.Run("default_"+def, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec("ratepace")
			apiSpec.DefaultRateLimit = def
			outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
			require.NoError(t, New(apiSpec, outputDir).Generate())

			rootTest := readGeneratedFile(t, outputDir, "internal", "cli", "root_test.go")
			assert.Contains(t, rootTest, "defaultRateLimit = 0")

			probe := `package cli

import "testing"

func TestRatePacingOffUnderTest(t *testing.T) {
	f := RootCmd().PersistentFlags().Lookup("rate-limit")
	if f == nil {
		t.Fatal("no --rate-limit flag")
	}
	if got := f.Value.String(); got != "0" {
		t.Fatalf("--rate-limit under test = %s, want 0", got)
	}
}
`
			probePath := filepath.Join(outputDir, "internal", "cli", "rate_pacing_probe_test.go")
			require.NoError(t, os.WriteFile(probePath, []byte(probe), 0o644))
			runGoCommand(t, outputDir, "mod", "tidy")
			runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestRatePacingOffUnderTest", "-count=1")
		})
	}
}
