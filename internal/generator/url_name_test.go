package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestParamURLNameOverridesWireKey covers Socrata-style APIs where the URL
// query key needs a literal "$" prefix ($limit, $offset, $where) while the
// user-facing CLI flag stays clean (--limit, --offset, --where). The fix adds
// an optional url_name field on Param that, when set, overrides Name as the
// wire-side URL key without touching the CLI flag derivation.
func TestParamURLNameOverridesWireKey(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("socrata-url-name")
	apiSpec.Resources["records"] = spec.Resource{
		Description: "Records",
		Endpoints: map[string]spec.Endpoint{
			"query": {
				Method:      "GET",
				Path:        "/records",
				Description: "Query records",
				Params: []spec.Param{
					{Name: "limit", URLName: "$limit", Type: "integer", Description: "Max rows"},
					{Name: "offset", URLName: "$offset", Type: "integer", Description: "Offset"},
					{Name: "where", URLName: "$where", Type: "string", Description: "SoQL WHERE"},
					{Name: "borough", Type: "integer", Description: "Plain (no override)"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "socrata-url-name-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	content := readGeneratedHandler(t, outputDir, "records")

	// Wire-side URL keys must be the $-prefixed override
	require.Contains(t, content, `params["$limit"]`, "URLName $limit must appear in the params map")
	require.Contains(t, content, `params["$offset"]`, "URLName $offset must appear in the params map")
	require.Contains(t, content, `params["$where"]`, "URLName $where must appear in the params map")

	// Params without url_name must keep plain Name
	require.Contains(t, content, `params["borough"]`, "Param without URLName must emit plain Name as URL key")

	// CLI flag identifiers must stay plain (no $ in Go identifiers, no $ on cobra flag names)
	require.Contains(t, content, "flagLimit", "Go identifier flagLimit must remain plain")
	require.Contains(t, content, `"limit"`, "cobra flag --limit must remain plain")
	require.NotContains(t, content, "flag$Limit", "no $ should leak into Go identifiers")
	require.NotContains(t, content, "flag\\$Limit", "no escaped $ should leak into Go identifiers either")

	// The Name field must NOT appear as a URL key when URLName is set (regression guard)
	if strings.Contains(content, `params["limit"]`) {
		t.Errorf("when URLName is $limit, params[\"limit\"] must not also be emitted as a URL key")
	}
}

// TestParamWithoutURLNameUnchanged guards against regression: existing specs
// without url_name must continue to emit Name as the URL key.
func TestParamWithoutURLNameUnchanged(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("plain-param")
	apiSpec.Resources["records"] = spec.Resource{
		Description: "Records",
		Endpoints: map[string]spec.Endpoint{
			"query": {
				Method: "GET", Path: "/records", Description: "Query records",
				Params: []spec.Param{
					{Name: "limit", Type: "integer", Description: "Max rows"},
					{Name: "owner", Type: "string", Description: "Owner filter"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "plain-param-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	content := readGeneratedHandler(t, outputDir, "records")

	require.Contains(t, content, `params["limit"]`, "plain Name limit must emit as URL key when URLName unset")
	require.Contains(t, content, `params["owner"]`, "plain Name owner must emit as URL key when URLName unset")
}

// readGeneratedHandler returns the contents of the generated CLI handler for a
// resource. The generator may emit it as either `<resource>.go` (multi-endpoint
// resource) or `promoted_<resource>.go` (single-endpoint promoted pattern), so
// try both.
func readGeneratedHandler(t *testing.T, outputDir, resource string) string {
	t.Helper()
	candidates := []string{
		filepath.Join(outputDir, "internal", "cli", resource+".go"),
		filepath.Join(outputDir, "internal", "cli", "promoted_"+resource+".go"),
	}
	for _, p := range candidates {
		if src, err := os.ReadFile(p); err == nil {
			return string(src)
		}
	}
	t.Fatalf("no generated handler found for resource %q (tried %v)", resource, candidates)
	return ""
}

// TestParamWireNameUnit exercises the spec.Param.WireName() method directly.
func TestParamWireNameUnit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, n, urlName, want string
	}{
		{"name only", "limit", "", "limit"},
		{"url_name overrides", "limit", "$limit", "$limit"},
		{"url_name empty falls back to Name", "where", "", "where"},
		{"url_name with special chars", "complex", "$query.where", "$query.where"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := spec.Param{Name: c.n, URLName: c.urlName}
			require.Equal(t, c.want, p.WireName())
		})
	}
}
