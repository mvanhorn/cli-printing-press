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

func TestParamURLNameOverridesWriteMethodQueryKeys(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("write-url-name")
	apiSpec.Resources["records"] = spec.Resource{
		Description: "Records",
		Endpoints: map[string]spec.Endpoint{
			"create": {
				Method:      "POST",
				Path:        "/records",
				Description: "Create record",
				Params:      []spec.Param{{Name: "dry_run", URLName: "$dry_run", Type: "boolean", Description: "Preview"}},
				Body:        []spec.Param{{Name: "name", Type: "string", Description: "Name"}},
			},
			"delete": {
				Method:      "DELETE",
				Path:        "/records",
				Description: "Delete records",
				Params:      []spec.Param{{Name: "dry_run", URLName: "$dry_run", Type: "boolean", Description: "Preview"}},
			},
			"update": {
				Method:      "PUT",
				Path:        "/records",
				Description: "Update records",
				Params:      []spec.Param{{Name: "dry_run", URLName: "$dry_run", Type: "boolean", Description: "Preview"}},
				Body:        []spec.Param{{Name: "name", Type: "string", Description: "Name"}},
			},
			"patch": {
				Method:      "PATCH",
				Path:        "/records",
				Description: "Patch records",
				Params:      []spec.Param{{Name: "dry_run", URLName: "$dry_run", Type: "boolean", Description: "Preview"}},
				Body:        []spec.Param{{Name: "name", Type: "string", Description: "Name"}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "write-url-name-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	content := readGeneratedCLIHandlers(t, outputDir)
	require.Equal(t, 4, strings.Count(content, `params["$dry_run"]`), "all write-method query maps must use URLName")
	require.NotContains(t, content, `params["dry_run"]`, "plain Name must not be used as the URL key when URLName is set")
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

func readGeneratedCLIHandlers(t *testing.T, outputDir string) string {
	t.Helper()
	var out strings.Builder
	cliDir := filepath.Join(outputDir, "internal", "cli")
	require.NoError(t, filepath.WalkDir(cliDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".go" {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out.Write(src)
		out.WriteByte('\n')
		return nil
	}))
	return out.String()
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

func TestMCPParamBindingsUseParamWireName(t *testing.T) {
	t.Parallel()

	bindings := mcpParamBindings(spec.Endpoint{
		Params: []spec.Param{
			{Name: "locationId", URLName: "location_id", Type: "string"},
		},
	}, "/opportunities/search")

	require.Len(t, bindings, 1)
	require.Equal(t, "locationId", bindings[0].PublicName)
	require.Equal(t, "location_id", bindings[0].WireName)
	require.Equal(t, "query", bindings[0].Location)
}

func TestCodeOrchQueryParamsUseParamWireName(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("code-orch-url-name")
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	apiSpec.Resources["opportunities"] = spec.Resource{
		Description: "Opportunities",
		Endpoints: map[string]spec.Endpoint{
			"search": {
				Method:      "GET",
				Path:        "/opportunities/search",
				Description: "Search opportunities",
				Params: []spec.Param{
					{Name: "locationId", URLName: "location_id", Type: "string", Description: "Location id"},
				},
			},
			"create": {
				Method:      "POST",
				Path:        "/opportunities",
				Description: "Create opportunity",
				Params: []spec.Param{
					{Name: "locationId", URLName: "location_id", Type: "string", Description: "Location id"},
				},
				Body: []spec.Param{
					{Name: "name", Type: "string", Description: "Opportunity name"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "code-orch-url-name-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	content := readGeneratedFile(t, outputDir, "internal", "mcp", "code_orch.go")
	require.Contains(t, content, `QueryParams []codeOrchParamBinding`,
		"code-orch endpoints must retain public-to-wire query bindings")
	require.Equal(t, 2, strings.Count(content, `{PublicName: "locationId", WireName: "location_id"}`),
		"GET and POST code-orch endpoints must preserve both public and wire query names")
	require.Contains(t, content, `query[codeOrchWireQueryName(ep.QueryParams, k)]`,
		"GET/DELETE code-orch routing must translate public names to wire names")
	require.Contains(t, content, `uv.Set(q.WireName, fmt.Sprintf("%v", v))`,
		"write-method code-orch routing must append wire query names")
}
