package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRootDefaultRateLimitEmission pins the --rate-limit default precedence in
// root.go.tmpl: an explicit spec DefaultRateLimit ("auto" or a number) wins over
// the legacy provenance rule (sniffed → 2, else → 0). "auto" wires the
// header-driven adaptive limiter via client.RateLimitAuto and relabels the
// flag's --help default to "auto".
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
			wantContain: []string{`"rate-limit", client.RateLimitAuto,`, `f.DefValue = "auto"`},
		},
		{
			name:        "numeric",
			def:         "3",
			wantContain: []string{`"rate-limit", 3,`},
			wantAbsent:  []string{"client.RateLimitAuto"},
		},
		{
			name:        "empty_sniffed",
			specSource:  "sniffed",
			wantContain: []string{`"rate-limit", 2,`},
			wantAbsent:  []string{"client.RateLimitAuto"},
		},
		{
			name:        "empty_default",
			wantContain: []string{`"rate-limit", 0,`},
			wantAbsent:  []string{"client.RateLimitAuto"},
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
