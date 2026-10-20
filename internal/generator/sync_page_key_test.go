package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/stretchr/testify/require"
)

func TestGeneratedSyncPageKeyFetchesSecondPage(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(pageKeySalesFixture))
	require.NoError(t, err)
	profile := profiler.Profile(apiSpec)

	var sales profiler.SyncableResource
	found := false
	for _, resource := range profile.SyncableResources {
		if resource.Path == "/sales" {
			sales = resource
			found = true
			break
		}
	}
	require.True(t, found, "sales list must be syncable")
	require.Equal(t, "page_key", sales.PaginationCursorParam)
	require.Equal(t, "cursor", sales.PaginationCursorType)
	require.True(t, sales.SupportsPagination, "page_key must mark the list paginated so sync does not stop after page one")
	require.Equal(t, "after", sales.SinceParam)

	outputDir := filepath.Join(t.TempDir(), "page-key-sales")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	modulePath := generatedModulePath(t, outputDir)
	testSrc := `package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"__MODULE__/internal/store"
)

type pageKeyClient struct {
	got []map[string]string
}

func (c *pageKeyClient) Get(_ context.Context, path string, params map[string]string) (json.RawMessage, error) {
	if path != "/sales" {
		return nil, errUnexpectedPath(path)
	}
	copied := map[string]string{}
	for key, value := range params {
		copied[key] = value
	}
	c.got = append(c.got, copied)
	if len(c.got) == 1 {
		return json.RawMessage(` + "`" + `{"data":[{"id":"sale-1"}],"next_cursor":"k2"}` + "`" + `), nil
	}
	return json.RawMessage(` + "`" + `{"data":[{"id":"sale-2"}]}` + "`" + `), nil
}

func (*pageKeyClient) RateLimit() float64 { return 0 }

type unexpectedPathError string

func errUnexpectedPath(path string) error { return unexpectedPathError(path) }

func (e unexpectedPathError) Error() string { return "unexpected path " + string(e) }

func TestSyncSalesFollowsPageKey(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	client := &pageKeyClient{}
	res := syncResource(context.Background(), client, db, "__RESOURCE__", "", false, 0, false, false, nil, nil)
	if res.Err != nil || res.Warn != nil {
		t.Fatalf("sync err=%v warn=%v calls=%v", res.Err, res.Warn, client.got)
	}
	if len(client.got) != 2 {
		t.Fatalf("calls=%d params=%v, want a second page", len(client.got), client.got)
	}
	if _, ok := client.got[0]["page_key"]; ok {
		t.Fatalf("first page sent page_key=%q", client.got[0]["page_key"])
	}
	if client.got[1]["page_key"] != "k2" {
		t.Fatalf("second page page_key=%q, want k2 (params %#v)", client.got[1]["page_key"], client.got[1])
	}
	if res.Count != 2 {
		t.Fatalf("stored=%d, want both pages", res.Count)
	}
}
`
	testSrc = strings.ReplaceAll(testSrc, "__MODULE__", modulePath)
	testSrc = strings.ReplaceAll(testSrc, "__RESOURCE__", sales.Name)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_page_key_test.go"), []byte(testSrc), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli", "-count=1", "-run", "^TestSyncSalesFollowsPageKey$")
}

const pageKeySalesFixture = `
openapi: 3.0.3
info:
  title: Page Key Sales
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /sales:
    get:
      operationId: listSales
      parameters:
        - name: after
          in: query
          description: Return records after this date
          schema: {type: string, format: date}
        - name: before
          in: query
          description: Return records before this date
          schema: {type: string, format: date}
        - name: page_key
          in: query
          schema: {type: string}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: array
                    items:
                      type: object
                      properties:
                        id: {type: string}
                  next_cursor: {type: string}
`
