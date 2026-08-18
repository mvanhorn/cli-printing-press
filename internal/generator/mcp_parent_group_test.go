package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPParentGroupClassifiesAsCommandGroupAndIsSkippedFromToolsList(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("parentgroup")
	apiSpec.Resources = map[string]spec.Resource{
		"companies": {
			Description: "Manage companies",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/companies", Description: "List companies"},
				"get":  {Method: "GET", Path: "/companies/{id}", Description: "Get a company"},
			},
			SubResources: map[string]spec.Resource{
				"sites": {
					Description: "Manage company sites",
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/companies/{company_id}/sites", Description: "List company sites"},
						"get":  {Method: "GET", Path: "/companies/{company_id}/sites/{id}", Description: "Get a company site"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "parentgroup-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	classifySrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "cobratree", "classify.go"))
	require.NoError(t, err)
	assert.Contains(t, string(classifySrc), `ParentGroupAnnotation = "pp:parent-group"`)
	assert.Contains(t, string(classifySrc), "func isGroupingParent(cmd *cobra.Command) bool")
	assert.Contains(t, string(classifySrc), "annotationIsTrue(cmd, ParentGroupAnnotation)")

	subParentSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "companies_sites.go"))
	require.NoError(t, err)
	assert.Contains(t, string(subParentSrc), `"pp:parent-group": "true"`)
	assert.NotContains(t, string(subParentSrc), `"pp:api-resource"`)
	assert.Regexp(t, `RunE:\s+parentNoSubcommandRunE\(flags\)`, string(subParentSrc))

	topParentSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "companies.go"))
	require.NoError(t, err)
	assert.Contains(t, string(topParentSrc), `"pp:api-resource": "true"`)
	assert.Contains(t, string(topParentSrc), `"pp:parent-group": "true"`)

	const classifyTest = `package cobratree

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestParentGroupAnnotationClassifiesAsCommandGroup(t *testing.T) {
	parent := &cobra.Command{
		Use: "sites",
		Annotations: map[string]string{"pp:parent-group": "true"},
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	}
	leaf := &cobra.Command{
		Use: "list",
		Annotations: map[string]string{"pp:endpoint": "sites.list"},
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	}
	parent.AddCommand(leaf)

	if got := classify(parent); got != commandGroup {
		t.Fatalf("pp:parent-group classify() = %v, want commandGroup", got)
	}
	if got := classify(leaf); got != commandEndpoint {
		t.Fatalf("endpoint leaf classify() = %v, want commandEndpoint", got)
	}

	apiParent := &cobra.Command{
		Use: "companies",
		Annotations: map[string]string{"pp:api-resource": "true"},
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	}
	if got := classify(apiParent); got != commandGroup {
		t.Fatalf("pp:api-resource classify() = %v, want commandGroup", got)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "cobratree", "parent_group_classify_test.go"), []byte(classifyTest), 0o644))

	const toolsListTest = `package mcp

import (
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

func TestParentGroupSkippedFromToolsList(t *testing.T) {
	s := server.NewMCPServer("test", "0.0.0")
	RegisterTools(s)
	tools := s.ListTools()

	if _, ok := tools["companies_sites"]; ok {
		t.Fatalf("sub-resource parent leaked into MCP tools/list: %#v", tools)
	}
	if _, ok := tools["companies"]; ok {
		t.Fatalf("API resource grouping command leaked into MCP tools/list: %#v", tools)
	}
	for _, leaf := range []string{"companies_list", "companies_get", "companies_sites_list", "companies_sites_get"} {
		entry, ok := tools[leaf]
		if !ok {
			t.Fatalf("leaf tool %q missing from MCP tools/list: %#v", leaf, tools)
		}
		if entry.Tool.Annotations.ReadOnlyHint == nil || !*entry.Tool.Annotations.ReadOnlyHint {
			t.Fatalf("leaf tool %q readOnlyHint = %v, want true", leaf, entry.Tool.Annotations.ReadOnlyHint)
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "parent_group_tools_list_test.go"), []byte(toolsListTest), 0o644))

	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp/...", "-run", "TestParentGroup(AnnotationClassifiesAsCommandGroup|SkippedFromToolsList)", "-count", "1")
}
