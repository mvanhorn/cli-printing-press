package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedPaginatedCommandsEmitResponsePath(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("paginated-response-path")
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Manage items",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:       "GET",
					Path:         "/items",
					Description:  "List items",
					Response:     spec.ResponseDef{Type: "array", Item: "Item"},
					ResponsePath: "results",
					Pagination: &spec.Pagination{
						Type:           "cursor",
						CursorParam:    "after",
						LimitParam:     "limit",
						NextCursorPath: "paging.next.after",
						HasMoreField:   "paging.has_more",
					},
					Params: []spec.Param{
						{Name: "after", Type: "string", Description: "Cursor"},
						{Name: "limit", Type: "integer", Description: "Page size"},
					},
				},
			},
		},
		"catalog": {
			Description: "Manage catalog",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/catalog",
					Description: "List catalog",
					Response:    spec.ResponseDef{Type: "array", Item: "Item"},
				},
				"lookup": {
					Method:       "GET",
					Path:         "/catalog/lookup",
					Description:  "Look up items",
					Response:     spec.ResponseDef{Type: "array", Item: "Item"},
					ResponsePath: "payload.records",
					Pagination: &spec.Pagination{
						Type:         "offset",
						CursorParam:  "offset",
						LimitParam:   "limit",
						HasMoreField: "payload.has_more",
					},
					Params: []spec.Param{
						{Name: "offset", Type: "integer", Description: "Offset"},
						{Name: "limit", Type: "integer", Description: "Page size"},
					},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Item": {Fields: []spec.TypeField{{Name: "id", Type: "string"}}},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true, Store: true}
	require.NoError(t, gen.Generate())

	promotedSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_items.go")
	assert.Contains(t, promotedSrc, `"paging.next.after", "paging.has_more", "results", cmd.ErrOrStderr())`)

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "catalog_lookup.go")
	assert.Contains(t, endpointSrc, `"", "payload.has_more", "payload.records", cmd.ErrOrStderr())`)
	requireGeneratedCompiles(t, outputDir)

	behaviorTest := `package cli

import (
    "context"
    "encoding/json"
    "testing"
)

type responsePathPaginationClient struct {
    responses []json.RawMessage
    params []map[string]string
}

func (c *responsePathPaginationClient) IsDryRun() bool { return false }

func (c *responsePathPaginationClient) GetWithHeaders(_ context.Context, _ string, params map[string]string, _ map[string]string) (json.RawMessage, error) {
    copied := map[string]string{}
    for key, value := range params {
        copied[key] = value
    }
    c.params = append(c.params, copied)
    response := c.responses[0]
    c.responses = c.responses[1:]
    return response, nil
}

func TestPaginatedResponsePathAggregatesPages(t *testing.T) {
    client := &responsePathPaginationClient{responses: []json.RawMessage{
        json.RawMessage("{\"results\":[{\"id\":\"item-1\"}],\"paging\":{\"next\":{\"after\":\"token-2\"}}}"),
        json.RawMessage("{\"results\":[{\"id\":\"item-2\"}],\"paging\":{\"next\":{\"after\":\"\"}}}"),
    }}
    data, err := paginatedGetWithResponsePath(context.Background(), client, "/items", map[string]string{"limit":"1"}, nil, true, "after", "cursor", "limit", 100, "paging.next.after", "", "results")
    if err != nil {
        t.Fatalf("paginatedGetWithResponsePath: %v", err)
    }
    var items []map[string]string
    if err := json.Unmarshal(data, &items); err != nil {
        t.Fatalf("decode aggregated items: %v; data=%s", err, data)
    }
    if len(items) != 2 || items[0]["id"] != "item-1" || items[1]["id"] != "item-2" {
        t.Fatalf("items = %#v, want both projected pages", items)
    }
    if len(client.params) != 2 || client.params[1]["after"] != "token-2" {
        t.Fatalf("params = %#v, want the declared nested cursor on page two", client.params)
    }
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "pagination_declarations_runtime_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestPaginatedResponsePathAggregatesPages", "-count=1")
}
