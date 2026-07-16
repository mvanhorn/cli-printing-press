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
// segment-aware cliutil escaper before substituting it into the URL path. Without
// the escape, values that contain path-reserved characters silently produce
// malformed request URLs; escaping the whole value instead breaks hierarchical
// identifiers such as "allenai/c4".
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
		"replacePathParam must use the shared segment-aware escaper")

	cliutilPath := filepath.Join(outputDir, "internal", "cliutil", "text.go")
	cliutilGo, err := os.ReadFile(cliutilPath)
	require.NoError(t, err)
	cliutilSrc := string(cliutilGo)
	assert.Contains(t, cliutilSrc, "func EscapePathParam(value string) string",
		"cliutil must emit the shared path-param escaper")
	assert.Contains(t, cliutilSrc, "segments := strings.Split(value, \"/\")",
		"path-param escaping must preserve slash separators in hierarchical identifiers")
	assert.Contains(t, cliutilSrc, "segments[i] = url.PathEscape(segment)",
		"each path segment must be percent-encoded independently")

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

func TestReplacePathParamPreservesHierarchicalIdentifiers(t *testing.T) {
	tests := map[string]string{
		"opaque-id": "opaque-id",
		"allenai/c4": "allenai/c4",
		"src/main file.go": "src/main%20file.go",
		"../secret": "%2E%2E/secret",
		"./file": "%2E/file",
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
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestReplacePathParamPreservesHierarchicalIdentifiers")

	cliutilTest := `package cliutil

import "testing"

func TestEscapePathParamPreservesHierarchicalIdentifiers(t *testing.T) {
	tests := map[string]string{
		"opaque-id": "opaque-id",
		"allenai/c4": "allenai/c4",
		"src/main file.go": "src/main%20file.go",
		"../secret": "%2E%2E/secret",
		"./file": "%2E/file",
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
	runGoCommandRequired(t, outputDir, "test", "./internal/cliutil", "-run", "TestEscapePathParamPreservesHierarchicalIdentifiers")

	mcpTest := `package mcp

import "testing"

func TestMCPPathValuePercentEncodesReservedCharacters(t *testing.T) {
	tests := map[string]string{
		"opaque-id": "opaque-id",
		"allenai/c4": "allenai/c4",
		"src/main file.go": "src/main%20file.go",
		"../secret": "%2E%2E/secret",
		"./file": "%2E/file",
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

// TestURLPathEscapeBehaviorPinsContract is a stdlib-behavior pin for each
// segment of a path-param value. The shared helper deliberately preserves
// slash separators while retaining url.PathEscape's reserved-character
// behavior within each segment.
func TestURLPathEscapeBehaviorPinsContract(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{"abc-123-def", "abc-123-def"},
		{"2026-01-15", "2026-01-15"},
		{"src/cli/main.go", "src/cli/main.go"},
		{"../secret", "%2E%2E/secret"},
		{"./file", "%2E/file"},
		{"https://example.com/", "https://example.com/"},
		{"sc-domain:example.com", "sc-domain:example.com"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			t.Parallel()
			parts := strings.Split(c.in, "/")
			for i, part := range parts {
				if part == "." || part == ".." {
					parts[i] = strings.Repeat("%2E", len(part))
					continue
				}
				parts[i] = url.PathEscape(part)
			}
			assert.Equal(t, c.want, strings.Join(parts, "/"))
		})
	}
}
