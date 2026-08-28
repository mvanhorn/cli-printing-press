package generator

import (
	"strconv"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncOwnedParamDefaultsSpec declares one list endpoint whose spec puts a
// `default:` on both kinds of param at once: keys the syncer assigns itself
// (the since filter, its companion ascending sort, the date range, and paging)
// and a genuinely load-bearing tenant scope. Only the scope may be seeded. Sync
// sends the others conditionally, so a seeded default would survive exactly the
// branch where sync chose to withhold it and quietly turn a full sync into a
// filtered, re-ordered one.
func syncOwnedParamDefaultsSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"tickets": {
			Description: "Tickets",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/tickets",
					Response: spec.ResponseDef{Type: "array"},
					Params: []spec.Param{
						{Name: "workspace-id", In: "query", Type: "string", Default: "ws-42"},
						{Name: "updated_since", In: "query", Type: "string", Default: "2020-01-01"},
						{Name: "sort", In: "query", Type: "string", Default: "updated_at:asc"},
						{Name: "dates", In: "query", Type: "string", Default: "last_30_days"},
						{Name: "limit", In: "query", Type: "integer", Default: 25},
					},
				},
			},
		},
	}
	return apiSpec
}

// TestGeneratedSyncSeedsScopeDefaultsButNotSyncOwnedParams pins the boundary the
// seeding must respect. A tenant scope default is the reported symptom: the
// endpoint command puts it on the wire, so sync must too. The since, sort,
// date-range, and paging keys are the inverse case -- sync owns them and
// decides per run whether to send them at all, so seeding them would override a
// deliberate decision rather than fill a gap.
func TestGeneratedSyncSeedsScopeDefaultsButNotSyncOwnedParams(t *testing.T) {
	outputDir := t.TempDir()
	apiSpec := syncOwnedParamDefaultsSpec("sync-owned-defaults")

	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	defaults := extractSyncParamDefaultsBlock(t, syncSrc, "tickets")

	assert.Contains(t, defaults, `"workspace-id": "ws-42"`,
		"tenant scope default must be seeded so sync and list address the same slice of the API")

	for _, owned := range []string{"updated_since", "sort", "dates", "limit"} {
		assert.NotContains(t, defaults, `"`+owned+`"`,
			"%q is assigned by the syncer itself and must not be seeded from a spec default", owned)
	}

	requireGeneratedCompiles(t, outputDir)
}

// extractSyncParamDefaultsBlock returns the body of the syncResourceParamDefaults
// case for one resource, so a NotContains assertion cannot be satisfied or
// defeated by a key that appears elsewhere in the generated file.
func extractSyncParamDefaultsBlock(t *testing.T, src, resource string) string {
	t.Helper()

	fnIdx := strings.Index(src, "func syncResourceParamDefaults(")
	require.GreaterOrEqual(t, fnIdx, 0, "generated sync.go must declare syncResourceParamDefaults")

	caseIdx := strings.Index(src[fnIdx:], "case "+strconv.Quote(resource)+":")
	require.GreaterOrEqual(t, caseIdx, 0, "syncResourceParamDefaults must have a case for %q", resource)
	start := fnIdx + caseIdx

	end := strings.Index(src[start:], "\n\t\t}")
	require.GreaterOrEqual(t, end, 0, "unterminated case body for %q", resource)
	return src[start : start+end]
}
