package openapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStructuralGuardNoFalsePositivesOnRealSpecs is the safety net for the
// parse-time GuardStructuralStrings prose/structural taxonomy: it parses every
// real spec the repo ships and asserts the guard never rejects one. A failure
// means a prose field is mis-tagged as structural (a `pp:"prose"` tag is
// missing) — fix the tag, do not weaken the guard. Intentionally-malformed
// fixtures may still fail Parse for other reasons; only a "disallowed
// character" error is treated as a guard false positive.
func TestStructuralGuardNoFalsePositivesOnRealSpecs(t *testing.T) {
	t.Parallel()

	dirs := []string{"../../catalog/specs", "../../testdata/openapi", "../../testdata/golden/fixtures"}

	checked := 0
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // corpus optional in some checkouts
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext != ".yaml" && ext != ".yml" && ext != ".json" {
				continue
			}
			path := filepath.Join(dir, e.Name())
			data, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			checked++

			// Route by format: OpenAPI/Swagger via Parse, internal Printing
			// Press YAML via spec.ParseBytes. We only assert the guard never
			// false-positives; other parse errors (refs, intentionally-bad
			// fixtures) are out of scope here.
			head := data
			if len(head) > 400 {
				head = head[:400]
			}
			var parseErr error
			if strings.Contains(string(head), "openapi:") || strings.Contains(string(head), "swagger:") {
				_, parseErr = Parse(data)
			} else {
				_, parseErr = spec.ParseBytes(data)
			}
			if parseErr != nil {
				assert.NotContains(t, parseErr.Error(), "disallowed character",
					"guard false-positive on shipped spec %s — a prose field is likely missing its pp:\"prose\" tag", path)
			}
		}
	}
	require.Positive(t, checked, "expected to parse at least one corpus spec")
}
