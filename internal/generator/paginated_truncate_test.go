package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestPaginatedGetEmitsTruncationWarning verifies that generated CLIs include
// the emitTruncationWarning helper and that paginatedGet calls it on the
// single-page path. The warning is the signal agents rely on to detect
// page-1 truncation when --all is not passed (issue #1137).
func TestPaginatedGetEmitsTruncationWarning(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("paginate-warn")
	apiSpec.Resources = map[string]spec.Resource{
		"orders": {
			Description: "Manage orders",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/orders",
					Description: "List orders",
					Pagination: &spec.Pagination{
						Type:           "cursor",
						CursorParam:    "after",
						NextCursorPath: "next_cursor",
						HasMoreField:   "has_more",
					},
					Response: spec.ResponseDef{Type: "array", Item: "Order"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "paginate-warn-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpersSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	require.Contains(t, string(helpersSrc), "func emitTruncationWarning(",
		"generated helpers.go should define emitTruncationWarning")
	require.Contains(t, string(helpersSrc), "emitTruncationWarning(data, nextCursorPath, hasMoreField, paginationType)",
		"paginatedGet should call emitTruncationWarning on the single-page path")

	runGoCommand(t, outputDir, "build", "./internal/cli")
}

func TestPaginatedGetHandlesNumericCursorAndMissingAllSignal(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("paginate-edge")
	apiSpec.Resources = map[string]spec.Resource{
		"orders": {
			Description: "Manage orders",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/orders",
					Description: "List orders",
					Pagination: &spec.Pagination{
						Type:           "page",
						CursorParam:    "page",
						LimitParam:     "limit",
						NextCursorPath: "meta.nextPage",
					},
					Response: spec.ResponseDef{Type: "array", Item: "Order"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "paginate-edge-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	behaviorTest := `package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

type paginatedTestClient struct {
	responses []json.RawMessage
	params    []map[string]string
}

func (c *paginatedTestClient) GetWithHeaders(ctx context.Context, path string, params map[string]string, headers map[string]string) (json.RawMessage, error) {
	_ = ctx
	copied := map[string]string{}
	for k, v := range params {
		copied[k] = v
	}
	c.params = append(c.params, copied)
	if len(c.responses) == 0 {
		return json.RawMessage(` + "`" + `[]` + "`" + `), nil
	}
	next := c.responses[0]
	c.responses = c.responses[1:]
	return next, nil
}

func capturePaginatedStderr(t *testing.T, fn func()) string {
	t.Helper()
	oldErr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = oldErr }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(out)
}

func TestPaginatedGetAcceptsNumericNextCursor(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"nextPage":2}}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"two"}],"meta":{}}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "page", "page", "limit", "meta.nextPage", "")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2; data=%s", len(got), data)
	}
	if len(client.params) != 2 {
		t.Fatalf("got %d requests, want 2", len(client.params))
	}
	if client.params[1]["page"] != "2" {
		t.Fatalf("second request page = %q, want 2", client.params[1]["page"])
	}
}

func TestPaginatedGetStopsAtNumericZeroNextCursor(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"nextPage":0}}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "page", "page", "limit", "meta.nextPage", "")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d items, want 1; data=%s", len(got), data)
	}
	if len(client.params) != 1 {
		t.Fatalf("got %d requests, want 1; params=%v", len(client.params), client.params)
	}
}

func TestPaginatedGetWarnsForSinglePageNumericNextCursor(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"nextPage":2}}` + "`" + `),
	}}
	stderr := capturePaginatedStderr(t, func() {
		_, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, false, "page", "page", "limit", "meta.nextPage", "")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
	})
	if !containsAll(stderr, ` + "`" + `"event":"truncated"` + "`" + `, ` + "`" + `"hint":"pass --all to fetch every page"` + "`" + `) {
		t.Fatalf("stderr missing numeric-cursor truncation warning: %s", stderr)
	}
}

func TestPaginatedGetWarnsForSinglePageHasMoreNumericPagination(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"has_more":true}}` + "`" + `),
	}}
	stderr := capturePaginatedStderr(t, func() {
		_, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, false, "page", "page", "limit", "", "meta.has_more")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
	})
	if !containsAll(stderr, ` + "`" + `"event":"truncated"` + "`" + `, ` + "`" + `"hint":"pass --all to fetch every page"` + "`" + `) {
		t.Fatalf("stderr missing has-more numeric pagination truncation hint: %s", stderr)
	}
}

func TestPaginatedGetWarnsWhenAllHasNoAdvanceSignal(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `[{"id":"one"}]` + "`" + `),
	}}
	stderr := capturePaginatedStderr(t, func() {
		data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "page", "page", "limit", "", "")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
		var got []map[string]string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d items, want 1", len(got))
		}
	})
	if len(client.params) != 1 {
		t.Fatalf("got %d requests, want 1", len(client.params))
	}
	if !containsAll(stderr, ` + "`" + `"event":"truncated"` + "`" + `, ` + "`" + `"reason":"pagination_signal_missing"` + "`" + `) {
		t.Fatalf("stderr missing structured truncation warning: %s", stderr)
	}
}

func TestPaginatedGetIncrementsNumericCursorWhenHasMoreHasNoBodyCursor(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"has_more":true}}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"two"}],"meta":{"has_more":true}}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"three"}],"meta":{"has_more":true}}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"four"}],"meta":{"has_more":false}}` + "`" + `),
	}}
	stderr := capturePaginatedStderr(t, func() {
		data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "page", "page", "limit", "", "meta.has_more")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
		var got []map[string]string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if len(got) != 4 {
			t.Fatalf("got %d items, want 4; data=%s", len(got), data)
		}
	})
	if len(client.params) != 4 {
		t.Fatalf("got %d requests, want 4", len(client.params))
	}
	for i, want := range []string{"2", "3", "4"} {
		if client.params[i+1]["page"] != want {
			t.Fatalf("request %d page = %q, want %s", i+2, client.params[i+1]["page"], want)
		}
	}
	if !containsAll(stderr, ` + "`" + `"event":"complete"` + "`" + `, ` + "`" + `"pages":4` + "`" + `) {
		t.Fatalf("stderr missing complete event for numeric has-more pagination: %s", stderr)
	}
	if strings.Contains(stderr, ` + "`" + `"reason":"pagination_cursor_missing"` + "`" + `) {
		t.Fatalf("stderr should not warn when numeric has-more pagination advances: %s", stderr)
	}
}

func TestPaginatedGetIncrementsNumericCursorWhenDeclaredBodyCursorIsMissing(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"has_more":true}}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"two"}],"meta":{"has_more":false}}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "page", "page", "limit", "meta.nextPage", "meta.has_more")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2; data=%s", len(got), data)
	}
	if len(client.params) != 2 {
		t.Fatalf("got %d requests, want 2", len(client.params))
	}
	if client.params[1]["page"] != "2" {
		t.Fatalf("second request page = %q, want 2", client.params[1]["page"])
	}
}

func TestPaginatedGetAdvancesOffsetCursorByLimitWhenHasMoreHasNoBodyCursor(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"has_more":true}}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"two"}],"meta":{"has_more":false}}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"100", "offset":"0"}, nil, true, "offset", "offset", "limit", "", "meta.has_more")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2; data=%s", len(got), data)
	}
	if len(client.params) != 2 {
		t.Fatalf("got %d requests, want 2", len(client.params))
	}
	if client.params[1]["offset"] != "100" {
		t.Fatalf("second request offset = %q, want 100", client.params[1]["offset"])
	}
}

func TestPaginatedGetAdvancesOffsetAfterFullPageWithoutHasMore(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"},{"id":"two"}]}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"three"}]}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"2", "offset":"0"}, nil, true, "offset", "offset", "limit", "", "")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d items, want 3; data=%s", len(got), data)
	}
	if len(client.params) != 2 {
		t.Fatalf("got %d requests, want 2", len(client.params))
	}
	if client.params[1]["offset"] != "2" {
		t.Fatalf("second request offset = %q, want 2", client.params[1]["offset"])
	}
}

func TestPaginatedGetStopsOffsetAtExplicitHasMoreFalseAfterFullPage(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"},{"id":"two"}],"meta":{"has_more":false}}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"three"}]}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"2", "offset":"0"}, nil, true, "offset", "offset", "limit", "", "meta.has_more")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2; data=%s", len(got), data)
	}
	if len(client.params) != 1 {
		t.Fatalf("got %d requests, want 1; params=%v", len(client.params), client.params)
	}
}

func TestPaginatedGetStopsOffsetAfterShortPageWithoutHasMore(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}]}` + "`" + `),
		json.RawMessage(` + "`" + `{"items":[{"id":"two"}]}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"2", "offset":"0"}, nil, true, "offset", "offset", "limit", "", "")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d items, want 1; data=%s", len(got), data)
	}
	if len(client.params) != 1 {
		t.Fatalf("got %d requests, want 1", len(client.params))
	}
}

func TestPaginatedGetWarnsWhenHasMorePageParamIsNonNumeric(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"has_more":true}}` + "`" + `),
	}}
	stderr := capturePaginatedStderr(t, func() {
		data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1", "page":"not-a-number"}, nil, true, "page", "page", "limit", "", "meta.has_more")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
		var got []map[string]string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d items, want 1", len(got))
		}
	})
	if len(client.params) != 1 {
		t.Fatalf("got %d requests, want 1", len(client.params))
	}
	if !containsAll(stderr, ` + "`" + `"event":"truncated"` + "`" + `, ` + "`" + `"reason":"pagination_cursor_missing"` + "`" + `) {
		t.Fatalf("stderr missing has-more truncation warning: %s", stderr)
	}
	if strings.Contains(stderr, ` + "`" + `"next_cursor_path":""` + "`" + `) {
		t.Fatalf("stderr should omit an empty next_cursor_path: %s", stderr)
	}
}

func TestPaginatedGetStopsAtMaxPageSafetyLimit(t *testing.T) {
	responses := make([]json.RawMessage, paginatedGetMaxPages+1)
	for i := range responses {
		responses[i] = json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"has_more":true}}` + "`" + `)
	}
	client := &paginatedTestClient{responses: responses}
	stderr := capturePaginatedStderr(t, func() {
		data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "page", "page", "limit", "", "meta.has_more")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
		var got []map[string]string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if len(got) != paginatedGetMaxPages {
			t.Fatalf("got %d items, want %d", len(got), paginatedGetMaxPages)
		}
	})
	if len(client.params) != paginatedGetMaxPages {
		t.Fatalf("got %d requests, want %d", len(client.params), paginatedGetMaxPages)
	}
	if !containsAll(stderr, ` + "`" + `"event":"truncated"` + "`" + `, ` + "`" + `"reason":"max_pages_cap_hit"` + "`" + `) {
		t.Fatalf("stderr missing max-pages truncation warning: %s", stderr)
	}
}

func TestPaginatedGetStopsAtMaxPageSafetyLimitForBodyCursor(t *testing.T) {
	responses := make([]json.RawMessage, paginatedGetMaxPages+1)
	for i := range responses {
		responses[i] = json.RawMessage(` + "`" + `{"items":[{"id":"one"}],"meta":{"next":"next-token"}}` + "`" + `)
	}
	client := &paginatedTestClient{responses: responses}
	stderr := capturePaginatedStderr(t, func() {
		data, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"limit":"1"}, nil, true, "cursor", "cursor", "limit", "meta.next", "")
		if err != nil {
			t.Fatalf("paginatedGet returned error: %v", err)
		}
		var got []map[string]string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if len(got) != paginatedGetMaxPages {
			t.Fatalf("got %d items, want %d", len(got), paginatedGetMaxPages)
		}
	})
	if len(client.params) != paginatedGetMaxPages {
		t.Fatalf("got %d requests, want %d", len(client.params), paginatedGetMaxPages)
	}
	if client.params[1]["cursor"] != "next-token" {
		t.Fatalf("second request cursor = %q, want next-token", client.params[1]["cursor"])
	}
	if !containsAll(stderr, ` + "`" + `"event":"truncated"` + "`" + `, ` + "`" + `"reason":"max_pages_cap_hit"` + "`" + `) {
		t.Fatalf("stderr missing max-pages body-cursor truncation warning: %s", stderr)
	}
}

func TestPaginatedGetMergesDomainSpecificWrappedArrayWithMetadataArrays(t *testing.T) {
	client := &paginatedTestClient{responses: []json.RawMessage{
		json.RawMessage(` + "`" + `{
			"charges": [{"id":"ch_1","amount":10}],
			"warnings": [{"code":"slow"}],
			"cursor": "next-token"
		}` + "`" + `),
		json.RawMessage(` + "`" + `{
			"charges": [{"id":"ch_2","amount":20}],
			"warnings": [],
			"cursor": null
		}` + "`" + `),
	}}
	data, err := paginatedGet(context.Background(), client, "/charges", map[string]string{"limit":"1"}, nil, true, "cursor", "cursor", "limit", "cursor", "")
	if err != nil {
		t.Fatalf("paginatedGet returned error: %v", err)
	}
	if string(data) == "null" {
		t.Fatalf("paginatedGet returned null for populated wrapped pages")
	}
	var got []map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v\n%s", err, data)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2; data=%s", len(got), data)
	}
	if got[0]["id"] != "ch_1" || got[1]["id"] != "ch_2" {
		t.Fatalf("merged wrong collection: %#v", got)
	}
	if len(client.params) != 2 {
		t.Fatalf("got %d requests, want 2", len(client.params))
	}
	if client.params[1]["cursor"] != "next-token" {
		t.Fatalf("second request cursor = %q, want next-token", client.params[1]["cursor"])
	}
}

func containsAll(s string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(s, needle) {
			return false
		}
	}
	return true
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "paginated_get_issue1688_test.go"), []byte(behaviorTest), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestPaginatedGet")
}

func TestOpenAPINestedNextPageGeneratesPaginatedCommandSignal(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(`
openapi: 3.0.3
info:
  title: Nested Page API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /opportunities/search:
    get:
      operationId: searchOpportunities
      parameters:
        - name: page
          in: query
          schema: {type: integer}
        - name: limit
          in: query
          schema: {type: integer}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  items:
                    type: array
                    items:
                      type: object
                  meta:
                    type: object
                    properties:
                      nextPage:
                        type: integer
`))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "nested-page-api-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cliDir := filepath.Join(outputDir, "internal", "cli")
	entries, err := os.ReadDir(cliDir)
	require.NoError(t, err)
	var commandSrc strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(cliDir, entry.Name()))
		require.NoError(t, err)
		commandSrc.Write(src)
		commandSrc.WriteByte('\n')
	}
	require.Contains(t, commandSrc.String(), `flagAll, "page", "page", "limit", "meta.nextPage", ""`,
		"generated command must pass parser-detected nested nextPage to resolvePaginatedRead")
}

func TestOpenAPIHasMoreOnlyPageGeneratesPaginatedCommandSignal(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(`
openapi: 3.0.3
info:
  title: Has More Page API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /folders:
    get:
      operationId: listFolders
      parameters:
        - name: page
          in: query
          schema: {type: integer}
        - name: limit
          in: query
          schema: {type: integer}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  items:
                    type: array
                    items:
                      type: object
                  has_more:
                    type: boolean
`))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "has-more-page-api-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cliDir := filepath.Join(outputDir, "internal", "cli")
	entries, err := os.ReadDir(cliDir)
	require.NoError(t, err)
	var commandSrc strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(cliDir, entry.Name()))
		require.NoError(t, err)
		commandSrc.Write(src)
		commandSrc.WriteByte('\n')
	}
	require.Contains(t, commandSrc.String(), `flagAll, "page", "page", "limit", "", "has_more"`,
		"generated command must pass has-more-only page pagination metadata to resolvePaginatedRead")
}
