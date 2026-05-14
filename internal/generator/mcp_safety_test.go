package generator

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWarnUnannotatedMutations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		spec           *spec.APISpec
		wantContaining []string
		wantAbsent     []string
	}{
		{
			name: "write POST without explicit destructive annotation warns",
			spec: &spec.APISpec{
				Resources: map[string]spec.Resource{
					"Users": {
						Endpoints: map[string]spec.Endpoint{
							"Create": {
								Method: "POST",
								Path:   "/users",
								Body:   []spec.Param{{Name: "email", Type: "string"}},
							},
						},
					},
				},
			},
			wantContaining: []string{"warning: command users create is an unannotated mutation"},
		},
		{
			name: "read-only POST stays silent",
			spec: &spec.APISpec{
				Resources: map[string]spec.Resource{
					"Search": {
						Endpoints: map[string]spec.Endpoint{
							"Query": {
								Method: "POST",
								Path:   "/search",
								Body:   []spec.Param{{Name: "query", Type: "string"}},
							},
						},
					},
				},
			},
			wantAbsent: []string{"warning:"},
		},
		{
			name: "explicit destructive annotation suppresses warning",
			spec: &spec.APISpec{
				Resources: map[string]spec.Resource{
					"Messages": {
						Endpoints: map[string]spec.Endpoint{
							"Send": {
								Method: "POST",
								Path:   "/messages/send",
								Body:   []spec.Param{{Name: "to", Type: "string"}},
								Meta:   map[string]string{"mcp:destructive": "true"},
							},
						},
					},
				},
			},
			wantAbsent: []string{"warning:"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			warnUnannotatedMutations(tt.spec, &buf)
			got := buf.String()

			for _, want := range tt.wantContaining {
				assert.Contains(t, got, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, got, absent)
			}
		})
	}
}

func TestCommandAnnotationsLiteral(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		endpoint     spec.Endpoint
		isReadOnly   bool
		wantContains []string
		wantAbsent   []string
	}{
		{
			name: "read-only annotation included",
			endpoint: spec.Endpoint{
				Method: "GET",
				Path:   "/items",
			},
			isReadOnly:   true,
			wantContains: []string{`"mcp:read-only": "true"`},
			wantAbsent:   []string{`"mcp:destructive": "true"`},
		},
		{
			name: "destructive and privacy annotations included from meta",
			endpoint: spec.Endpoint{
				Method: "POST",
				Path:   "/messages/send",
				Meta: map[string]string{
					"mcp:destructive":       "true",
					"mcp:privacy-sensitive": "true",
				},
			},
			wantContains: []string{`"mcp:destructive": "true"`, `"mcp:privacy-sensitive": "true"`},
		},
		{
			name: "DELETE method carries destructive annotation",
			endpoint: spec.Endpoint{
				Method: "DELETE",
				Path:   "/messages/{id}",
			},
			wantContains: []string{`"mcp:destructive": "true"`},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := commandAnnotationsLiteral("messages", "send", tt.endpoint.Path, tt.endpoint, tt.isReadOnly)
			for _, want := range tt.wantContains {
				assert.Contains(t, got, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, got, absent)
			}
		})
	}
}

func TestGeneratedMCPSafetySurfaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		resourceName        string
		endpointName        string
		endpoint            spec.Endpoint
		wantCommandContains []string
		wantToolContains    []string
		wantToolAbsent      []string
	}{
		{
			name:         "privacy-sensitive GET carries annotation and metadata",
			resourceName: "Messages",
			endpointName: "Get",
			endpoint: spec.Endpoint{
				Method:      "GET",
				Path:        "/messages/{id}",
				Description: "Get a message body",
				Meta:        map[string]string{"mcp:privacy-sensitive": "true"},
				Params:      []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true}},
			},
			wantCommandContains: []string{`"mcp:read-only": "true"`, `"mcp:privacy-sensitive": "true"`},
			wantToolContains: []string{
				`mcplib.WithReadOnlyHintAnnotation(true)`,
				`Privacy-sensitive: may expose personal, financial, or message content.`,
			},
		},
		{
			name:         "mutation POST stays neutral without explicit annotation",
			resourceName: "Users",
			endpointName: "Create",
			endpoint: spec.Endpoint{
				Method:      "POST",
				Path:        "/users",
				Description: "Create a user",
				Body:        []spec.Param{{Name: "email", Type: "string"}},
			},
			wantToolContains: []string{
				`mcplib.WithOpenWorldHintAnnotation(true)`,
			},
			wantToolAbsent: []string{
				`mcplib.WithDestructiveHintAnnotation(true)`,
				`mcplib.WithDestructiveHintAnnotation(false)`,
			},
		},
		{
			name:         "explicit destructive POST emits destructive hint",
			resourceName: "Messages",
			endpointName: "Send",
			endpoint: spec.Endpoint{
				Method:      "POST",
				Path:        "/messages/send",
				Description: "Send a message",
				Body:        []spec.Param{{Name: "to", Type: "string"}},
				Meta:        map[string]string{"mcp:destructive": "true"},
			},
			wantCommandContains: []string{`"mcp:destructive": "true"`},
			wantToolContains:    []string{`mcplib.WithDestructiveHintAnnotation(true)`},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(strings.ToLower(tt.resourceName) + "-" + strings.ToLower(tt.endpointName))
			apiSpec.Resources = map[string]spec.Resource{
				tt.resourceName: {
					Description: tt.resourceName,
					Endpoints:   map[string]spec.Endpoint{tt.endpointName: tt.endpoint},
				},
			}

			outputDir := filepath.Join(t.TempDir(), strings.ToLower(tt.resourceName)+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			commandSrc := readPromotedCommandFile(t, outputDir)
			for _, want := range tt.wantCommandContains {
				assert.Contains(t, commandSrc, want)
			}

			toolsSrcBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
			require.NoError(t, err)

			toolName := strings.ToLower(tt.resourceName) + "_" + strings.ToLower(tt.endpointName)
			blockRE := regexp.MustCompile(`(?s)mcplib\.NewTool\("` + regexp.QuoteMeta(toolName) + `".*?\n\t\)`)
			block := blockRE.FindString(string(toolsSrcBytes))
			require.NotEmpty(t, block, "expected to find typed MCP tool block for endpoint")

			for _, want := range tt.wantToolContains {
				assert.Contains(t, block, want)
			}
			for _, absent := range tt.wantToolAbsent {
				assert.NotContains(t, block, absent)
			}
		})
	}
}

func TestGeneratedCobratreeSafetyHints(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cobratree-safety")
	apiSpec.Resources = map[string]spec.Resource{
		"Items": {
			Description: "Items",
			Endpoints: map[string]spec.Endpoint{
				"List": {
					Method:      "GET",
					Path:        "/items",
					Description: "List items",
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "cobratree-safety-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	testSrc := `package cobratree

import (
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/cobra"
)

func TestToolOptionsForCommandSafetyHints(t *testing.T) {
	cases := []struct {
		name                 string
		cmd                  *cobra.Command
		wantReadOnly         *bool
		wantDestructive      *bool
		wantOpenWorld        *bool
		wantTitle            string
		wantDescriptionStart string
	}{
		{
			name: "GET defaults to read-only",
			cmd: &cobra.Command{
				Use:   "list",
				Short: "List items",
				Annotations: map[string]string{
					MethodAnnotation: "GET",
				},
			},
			wantReadOnly: boolPtr(true),
			wantDestructive: boolPtr(false),
			wantOpenWorld: boolPtr(true),
		},
		{
			name: "DELETE defaults to destructive",
			cmd: &cobra.Command{
				Use:   "delete",
				Short: "Delete item",
				Annotations: map[string]string{
					MethodAnnotation: "DELETE",
				},
			},
			wantDestructive: boolPtr(true),
			wantOpenWorld: boolPtr(true),
		},
		{
			name: "POST stays neutral without annotation",
			cmd: &cobra.Command{
				Use:   "create",
				Short: "Create item",
				Annotations: map[string]string{
					MethodAnnotation: "POST",
				},
			},
		},
		{
			name: "explicit destructive POST sets destructive hint",
			cmd: &cobra.Command{
				Use:   "send",
				Short: "Send message",
				Annotations: map[string]string{
					MethodAnnotation:      "POST",
					DestructiveAnnotation: "true",
				},
			},
			wantDestructive: boolPtr(true),
			wantOpenWorld: boolPtr(true),
		},
		{
			name: "privacy-sensitive GET adds title and warning",
			cmd: &cobra.Command{
				Use:   "mail",
				Short: "Privacy-sensitive: Get mail",
				Annotations: map[string]string{
					MethodAnnotation:           "GET",
					PrivacySensitiveAnnotation: "true",
				},
			},
			wantReadOnly: boolPtr(true),
			wantDestructive: boolPtr(false),
			wantOpenWorld: boolPtr(true),
			wantDescriptionStart: "Privacy-sensitive: Get mail",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tool := mcplib.NewTool("x", toolOptionsForCommand(tc.cmd)...)
			if tc.wantReadOnly == nil {
				if tool.Annotations.ReadOnlyHint != nil {
					t.Fatalf("readOnlyHint = %v, want nil", *tool.Annotations.ReadOnlyHint)
				}
			} else if tool.Annotations.ReadOnlyHint == nil || *tool.Annotations.ReadOnlyHint != *tc.wantReadOnly {
				t.Fatalf("readOnlyHint = %v, want %v", tool.Annotations.ReadOnlyHint, *tc.wantReadOnly)
			}
			if tc.wantDestructive == nil {
				if tool.Annotations.DestructiveHint != nil {
					t.Fatalf("destructiveHint = %v, want nil", *tool.Annotations.DestructiveHint)
				}
			} else if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint != *tc.wantDestructive {
				t.Fatalf("destructiveHint = %v, want %v", tool.Annotations.DestructiveHint, *tc.wantDestructive)
			}
			if tc.wantOpenWorld == nil {
				if tool.Annotations.OpenWorldHint != nil {
					t.Fatalf("openWorldHint = %v, want nil", *tool.Annotations.OpenWorldHint)
				}
			} else if tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint != *tc.wantOpenWorld {
				t.Fatalf("openWorldHint = %v, want %v", tool.Annotations.OpenWorldHint, *tc.wantOpenWorld)
			}
			if tc.wantTitle != "" && tool.Annotations.Title != tc.wantTitle {
				t.Fatalf("title = %q, want %q", tool.Annotations.Title, tc.wantTitle)
			}
			if tc.wantDescriptionStart != "" && !strings.HasPrefix(tool.Description, tc.wantDescriptionStart) {
				t.Fatalf("description = %q, want prefix %q", tool.Description, tc.wantDescriptionStart)
			}
		})
	}
}

func boolPtr(v bool) *bool { return &v }
`

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "cobratree", "safety_extra_test.go"), []byte(testSrc), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp/cobratree")
}
