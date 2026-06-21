package generator

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func queryEndpointFixturePath() string {
	return filepath.Join("..", "..", "testdata", "golden", "fixtures", "query-endpoint-api.yaml")
}

// TestGeneratedQueryEndpointSyncEmitsConstructs asserts the SQL-query-endpoint
// sync shape (issue #3011) is emitted by construction from the query_sync hint:
// the per-resource entity map, the query injection keyed on the shared path, the
// STARTPOSITION offset paging, and the response-envelope key — and that the
// generated module compiles. A normal REST spec must emit none of it.
func TestGeneratedQueryEndpointSyncEmitsConstructs(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(queryEndpointFixturePath())
	require.NoError(t, err)
	require.NotNil(t, apiSpec.QuerySync, "fixture must declare a query_sync hint")

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncContent := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")

	// Per-resource entity map, derived from each list endpoint's response item.
	assert.Contains(t, syncContent, `"widgets": "Widget"`)
	assert.Contains(t, syncContent, `"gadgets": "Gadget"`)
	assert.Contains(t, syncContent, "queryPageSize = 2")
	assert.Contains(t, syncContent, `queryPath     = "/query"`)

	// Query injection: SELECT built from the hint template + injected version param.
	assert.Contains(t, syncContent, `if entity, ok := queryEntity[resource]; ok && path == queryPath {`)
	assert.Contains(t, syncContent, `strings.ReplaceAll("select * from {entity} startposition {start} maxresults {limit}", "{entity}", entity)`)
	assert.Contains(t, syncContent, `params["minorversion"] = "75"`)

	// Offset paging driven by page fill, and break-guard exemptions (the
	// qbo-query-paging hand-fix asserts path != "/query" appears at least twice).
	assert.Contains(t, syncContent, "nextCursor = strconv.Itoa(start + queryPageSize)")
	assert.GreaterOrEqual(t, strings.Count(syncContent, "path != queryPath"), 2,
		"break guards must exempt the query path in both the pagination and short-page checks")

	// Response-envelope unwrap: the entity-named envelope key is query-only,
	// not added to the global extractor list used by ordinary resources.
	assert.Contains(t, syncContent, `var dataEnvelopeKeys = []string{"data", "Data", "result", "Result"}`)
	assert.Contains(t, syncContent, `var queryDataEnvelopeKeys = []string{"data", "Data", "result", "Result", "QueryResponse"}`)
	assert.Contains(t, syncContent, `extractPageItems(data, pageSize.cursorParam, dataEnvelopeKeysForResource(resource, path))`)

	requireGeneratedCompiles(t, outputDir)
}

// TestPlainRESTSyncOmitsQueryConstructs is the negative half of #3011: a spec
// with no query_sync hint emits none of the query-endpoint codegen, so normal
// REST CLIs are unaffected. (Byte-identity across the full golden suite is the
// stronger guarantee; this is a fast guard against accidental ungating.)
func TestPlainRESTSyncOmitsQueryConstructs(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("plainrest")
	require.Nil(t, apiSpec.QuerySync)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncContent := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.NotContains(t, syncContent, "queryEntity")
	assert.NotContains(t, syncContent, "queryPath")
	assert.Contains(t, syncContent, `var dataEnvelopeKeys = []string{"data", "Data", "result", "Result"}`)
}

// TestQueryEndpointSyncPagesAndUnwraps is the behavioral acceptance for #3011:
// a printed CLI syncs all rows across multiple STARTPOSITION pages from the
// shared query endpoint and unwraps the entity-named envelope. The mock serves a
// full first page (advancing STARTPOSITION) then a short second page (ending the
// loop); the union must land in the local store.
func TestQueryEndpointSyncPagesAndUnwraps(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a generated binary; runs in the full generated-test CI lane")
	}
	t.Parallel()

	var mu sync.Mutex
	var seenQueries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/query", r.URL.Path)
		q := r.URL.Query().Get("query")
		mu.Lock()
		seenQueries = append(seenQueries, q)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(q, "startposition 1 maxresults 2"):
			// Full page of 2 -> sync must advance to STARTPOSITION 3.
			fmt.Fprint(w, `{"QueryResponse":{"Widget":[{"id":"1","name":"a"},{"id":"2","name":"b"}]}}`)
		case strings.Contains(q, "startposition 3 maxresults 2"):
			// Short page of 1 -> sync must stop after this page.
			fmt.Fprint(w, `{"QueryResponse":{"Widget":[{"id":"3","name":"c"}]}}`)
		default:
			fmt.Fprint(w, `{"QueryResponse":{"Widget":[]}}`)
		}
	}))
	t.Cleanup(server.Close)

	apiSpec, err := spec.Parse(queryEndpointFixturePath())
	require.NoError(t, err)
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.BaseURL = server.URL

	slug := naming.CLI(apiSpec.Name)
	outputDir := filepath.Join(t.TempDir(), slug)
	require.NoError(t, New(apiSpec, outputDir).Generate())

	binaryPath := filepath.Join(outputDir, slug)
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+slug)

	dbPath := filepath.Join(t.TempDir(), "sync.db")
	cmd := exec.Command(binaryPath, "--json", "sync", "--resources", "widgets", "--max-pages", "0", "--db", dbPath)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	// Both pages fetched, with the STARTPOSITION offset advancing by the page size.
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, seenQueries, 2, "expected exactly two STARTPOSITION pages, got %v", seenQueries)
	assert.Contains(t, seenQueries[0], "select * from Widget startposition 1 maxresults 2")
	assert.Contains(t, seenQueries[1], "select * from Widget startposition 3 maxresults 2")

	// All three rows landed (proves the envelope unwrap + no truncation); a
	// failed unwrap or stalled paging would store 0 or 2.
	assert.Contains(t, string(out), `"resource":"widgets"`)
	assert.Contains(t, string(out), `"total":3`)
}
