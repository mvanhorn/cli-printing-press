package generator

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReplacePathParamPercentEncodesValue pins the helpers.go template so the
// emitted replacePathParam routes the user-supplied value through the shared
// path-param escaper before substituting it into the URL path. Without encoding
// the value as one segment, values that contain path-reserved characters
// silently produce malformed request URLs.
//
// We assert at the template-output level (helpers.go calls cliutil and
// cliutil/text.go contains the implementation) so every printed CLI inherits
// the fix.
func TestReplacePathParamPercentEncodesValue(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "encpath",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/encpath-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Items",
				Endpoints: map[string]spec.Endpoint{
					"get": {
						Method:      "GET",
						Path:        "/v1/items/{itemId}",
						Description: "Get an item",
						Params: []spec.Param{
							{Name: "itemId", Type: "string", Required: true, Positional: true},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpersPath := filepath.Join(outputDir, "internal", "cli", "helpers.go")
	helpersGo, err := os.ReadFile(helpersPath)
	require.NoError(t, err)
	src := string(helpersGo)

	// The shared implementation is emitted in cliutil/text.go below.
	assert.Contains(t, src, "/internal/cliutil",
		"helpers.go must import cliutil when replacePathParam is emitted")
	assert.Contains(t, src,
		`return strings.ReplaceAll(path, "{"+name+"}", cliutil.EscapePathParam(value))`,
		"replacePathParam must preserve ordinary path params while using the shared path-param escaper")
	assert.Contains(t, src,
		`return replacePathParam(path, name, pathParamSegmentValue(value))`,
		"replaceURLIDPathParam must normalize URL-backed resource IDs before escaping")

	cliutilPath := filepath.Join(outputDir, "internal", "cliutil", "text.go")
	cliutilGo, err := os.ReadFile(cliutilPath)
	require.NoError(t, err)
	cliutilSrc := string(cliutilGo)
	assert.Contains(t, cliutilSrc, "func EscapePathParam(value string) string",
		"cliutil must emit the shared path-param escaper")
	assert.Contains(t, cliutilSrc, `if value == "." || value == ".."`,
		"path-param escaping must keep dot-only values from becoming path traversal segments")
	assert.Contains(t, cliutilSrc, "return url.PathEscape(value)",
		"path-param values must be percent-encoded as a single segment")

	mcpPath := filepath.Join(outputDir, "internal", "mcp", "tools.go")
	mcpGo, err := os.ReadFile(mcpPath)
	require.NoError(t, err)
	mcpSrc := string(mcpGo)
	assert.Contains(t, mcpSrc, `return cliutil.EscapePathParam(formatMCPParamValue(v))`,
		"MCP path params must use the same generated helper as the CLI")
	assert.Equal(t, 2, strings.Count(mcpSrc, `strings.Replace(path, placeholder, mcpPathValue(v), 1)`),
		"both MCP path-binding loops must percent-encode path values")

	cliTest := `package cli

import "testing"

func TestReplacePathParamEncodesSingleSegment(t *testing.T) {
	tests := map[string]string{
		"opaque-id": "opaque-id",
		"sc-domain:example.com": "sc-domain:example.com",
		"https://example.com/foo?version=2": "https:%2F%2Fexample.com%2Ffoo%3Fversion=2",
		"allenai/c4": "allenai%2Fc4",
		"src/main file.go": "src%2Fmain%20file.go",
		"../secret": "..%2Fsecret",
		"./file": ".%2Ffile",
		"a b?c#d": "a%20b%3Fc%23d",
	}
	for input, want := range tests {
		if got := replacePathParam("/datasets/{id}", "id", input); got != "/datasets/"+want {
			t.Fatalf("replacePathParam(%q) = %q, want %q", input, got, "/datasets/"+want)
		}
	}
}

func TestReplaceURLIDPathParamUsesTrailingSegment(t *testing.T) {
	tests := map[string]string{
		"https://example.com/foo": "foo",
		"https://example.com/foo/bar/": "bar",
		"https://example.com/foo?version=2": "foo",
		"opaque-id": "opaque-id",
		"allenai/c4": "allenai%2Fc4",
	}
	for input, want := range tests {
		if got := replaceURLIDPathParam("/datasets/{id}", "id", input); got != "/datasets/"+want {
			t.Fatalf("replaceURLIDPathParam(%q) = %q, want %q", input, got, "/datasets/"+want)
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "path_param_test.go"), []byte(cliTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestReplacePathParamEncodesSingleSegment")

	cliutilTest := `package cliutil

import "testing"

func TestEscapePathParamEncodesSingleSegment(t *testing.T) {
	tests := map[string]string{
		"opaque-id": "opaque-id",
		"sc-domain:example.com": "sc-domain:example.com",
		"https://example.com/foo": "https:%2F%2Fexample.com%2Ffoo",
		"allenai/c4": "allenai%2Fc4",
		"src/main file.go": "src%2Fmain%20file.go",
		"../secret": "..%2Fsecret",
		"./file": ".%2Ffile",
		"a b?c#d": "a%20b%3Fc%23d",
	}
	for input, want := range tests {
		if got := EscapePathParam(input); got != want {
			t.Fatalf("EscapePathParam(%q) = %q, want %q", input, got, want)
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cliutil", "path_param_test.go"), []byte(cliutilTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cliutil", "-run", "TestEscapePathParamEncodesSingleSegment")

	mcpTest := `package mcp

import "testing"

func TestMCPPathValuePercentEncodesReservedCharacters(t *testing.T) {
	tests := map[string]string{
		"opaque-id": "opaque-id",
		"sc-domain:example.com": "sc-domain:example.com",
		"https://example.com/foo": "https:%2F%2Fexample.com%2Ffoo",
		"allenai/c4": "allenai%2Fc4",
		"src/main file.go": "src%2Fmain%20file.go",
		"../secret": "..%2Fsecret",
		"./file": ".%2Ffile",
		"a b?c#d": "a%20b%3Fc%23d",
	}
	for input, want := range tests {
		if got := mcpPathValue(input); got != want {
			t.Fatalf("mcpPathValue(%q) = %q, want %q", input, got, want)
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "path_value_test.go"), []byte(mcpTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "TestMCPPathValuePercentEncodesReservedCharacters")
	requireGeneratedCompiles(t, outputDir)
}

// TestDependentPathParamStripsCompositeStorageID pins sync.go.tmpl so the
// dependent fan-out substitutes the BARE entity id into a child resource's path
// template, not the NUL-composite storage id that resourceStorageID builds for a
// parent-keyed parent. Without stripping, the composite leaks into
// replacePathParam, whose url.PathEscape renders the NUL as "%00", and nginx
// rejects the request with HTTP 400.
func TestDependentPathParamStripsCompositeStorageID(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("comppath")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"projects": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/projects",
					Response: spec.ResponseDef{Type: "array"},
					IDField:  "id",
				},
				"get": {
					Method:   "GET",
					Path:     "/projects/{projectId}",
					Response: spec.ResponseDef{Type: "object"},
					IDField:  "id",
				},
			},
		},
		"modules": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:     "GET",
					Path:       "/projects/{projectId}/modules",
					Response:   spec.ResponseDef{Type: "array"},
					Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					IDField:    "id",
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	syncPath := filepath.Join(outputDir, "internal", "cli", "sync.go")
	syncGo, err := os.ReadFile(syncPath)
	require.NoError(t, err)
	src := string(syncGo)

	assert.Contains(t, src,
		`path = replaceDependentPathParam(path, pathParam.Param, dep.ParentTable, pathParam.Field, parentRow[pathParam.Field])`,
		"the dependent fan-out must strip the NUL-composite parent storage id via "+
			"store.BareResourceID before substituting it into the path, so a parent-keyed "+
			"parent (composite id) never leaks a %00 into the request URL (nginx 400)")
	assert.Contains(t, src,
		`bareValue := store.BareResourceID(value)`,
		"replaceDependentPathParam must still strip composite storage IDs before path substitution")
}

func TestDependentURLIDPathParamUsesTrailingSegment(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("urlidpath")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"scheduled-events": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/scheduled_events",
					Response: spec.ResponseDef{Type: "array"},
					IDField:  "uri",
				},
			},
		},
		"invitees": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/scheduled_events/{event_uuid}/invitees",
					Response: spec.ResponseDef{Type: "array"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	gen.profile = &profiler.APIProfile{
		SyncableResources: []profiler.SyncableResource{
			{Name: "scheduled-events", Path: "/scheduled_events", Method: "GET", IDField: "uri"},
		},
		DependentSyncResources: []profiler.DependentResource{
			{
				Name:           "invitees",
				ParentResource: "scheduled-events",
				ParentIDParam:  "event_uuid",
				Path:           "/scheduled_events/{event_uuid}/invitees",
				Method:         "GET",
				PathParams:     []profiler.DependentPathParam{{Param: "event_uuid", Field: "uri"}},
			},
		},
	}
	require.NoError(t, gen.Generate())

	inlineTest := `package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

type dependentURLIDClient struct {
	t *testing.T
}

func (c dependentURLIDClient) Get(_ context.Context, path string, _ map[string]string) (json.RawMessage, error) {
	if path != "/scheduled_events/event-a/invitees" {
		c.t.Fatalf("path = %q, want /scheduled_events/event-a/invitees", path)
	}
	return json.RawMessage(` + "`" + `[{"id":"invitee-1"}]` + "`" + `), nil
}

func (dependentURLIDClient) RateLimit() float64 {
	return 0
}

func TestDependentURLIDPathParamUsesTrailingSegment(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	eventURI := "https://api.example.com/scheduled_events/event-a?version=2"
	if err := db.Upsert("scheduled-events", eventURI, []byte(` + "`" + `{"uri":"https://api.example.com/scheduled_events/event-a?version=2","name":"Weekly review"}` + "`" + `)); err != nil {
		t.Fatalf("insert scheduled event: %v", err)
	}

	res := syncDependentResource(
		context.Background(),
		dependentURLIDClient{t: t},
		db,
		dependentResourceDef{
			Name: "invitees",
			ParentTable: "scheduled-events",
			ParentIDParam: "event_uuid",
			PathTemplate: "/scheduled_events/{event_uuid}/invitees",
			PathParams: []dependentPathParamDef{{Param: "event_uuid", Field: "uri"}},
		},
		"", false, 1, false, false, nil, nil, 1,
	)
	if res.Err != nil {
		t.Fatalf("syncDependentResource error: %v", res.Err)
	}
	if res.Count != 1 {
		t.Fatalf("synced count = %d, want 1", res.Count)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "dependent_url_id_path_test.go"), []byte(inlineTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestDependentURLIDPathParamUsesTrailingSegment")
}

// TestPathParamEscapeBehaviorPinsContract is a stdlib-behavior pin for
// path-param values. The shared helper treats the full value as one path segment
// so slashes inside IDs do not change the route shape.
func TestPathParamEscapeBehaviorPinsContract(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{"abc-123-def", "abc-123-def"},
		{"2026-01-15", "2026-01-15"},
		{"src/cli/main.go", "src%2Fcli%2Fmain.go"},
		{"../secret", "..%2Fsecret"},
		{"./file", ".%2Ffile"},
		{"https://example.com/", "https:%2F%2Fexample.com%2F"},
		{"https://example.com/foo", "https:%2F%2Fexample.com%2Ffoo"},
		{"sc-domain:example.com", "sc-domain:example.com"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			t.Parallel()
			got := url.PathEscape(c.in)
			if c.in == "." || c.in == ".." {
				got = strings.Repeat("%2E", len(c.in))
			}
			assert.Equal(t, c.want, got)
		})
	}
}
