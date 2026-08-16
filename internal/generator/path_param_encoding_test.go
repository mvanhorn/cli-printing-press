package generator

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
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
		`return strings.ReplaceAll(path, "{"+name+"}", cliutil.EscapePathParam(pathParamSegmentValue(value)))`,
		"replacePathParam must normalize URL IDs before using the shared path-param escaper")

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
		"https://example.com/foo": "foo",
		"https://example.com/foo/bar/": "bar",
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
		`path = replacePathParam(path, pathParam.Param, store.BareResourceID(parentRow[pathParam.Field]))`,
		"the dependent fan-out must strip the NUL-composite parent storage id via "+
			"store.BareResourceID before substituting it into the path, so a parent-keyed "+
			"parent (composite id) never leaks a %00 into the request URL (nginx 400)")
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
