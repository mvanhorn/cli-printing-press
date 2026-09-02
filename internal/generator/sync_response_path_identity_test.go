// Copyright 2026 Anthropic, PBC. Licensed under Apache-2.0.

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

func TestResponsePathCasesKeyOnResourceIdentity(t *testing.T) {
	t.Parallel()

	cases := responsePathCases(map[string]spec.Resource{
		"zenodo": {
			Description: "Records",
			Endpoints: map[string]spec.Endpoint{
				"search": {
					Method:       "GET",
					Path:         "/api/records",
					Syncable:     true,
					ResponsePath: "hits.hits",
					Response:     spec.ResponseDef{Type: "array"},
				},
				"get": {
					Method:       "GET",
					Path:         "/api/records/{id}",
					ResponsePath: "metadata",
					Response:     spec.ResponseDef{Type: "object"},
				},
			},
		},
	})

	require.Equal(t, []responsePathCase{{
		Key:          "zenodo",
		ResponsePath: "hits.hits",
	}}, cases)
}

func TestGeneratedSyncUnwrapsWhenRequestPathDiffersFromSpec(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("path-identity")
	apiSpec.Resources = map[string]spec.Resource{
		"records": {
			Description: "Records",
			Endpoints: map[string]spec.Endpoint{
				"search": {
					Method:       "GET",
					Path:         "/api/records",
					Description:  "Search records",
					Syncable:     true,
					Response:     spec.ResponseDef{Type: "array", Item: "Record"},
					ResponsePath: "hits.hits",
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Record": {Fields: []spec.TypeField{
			{Name: "id", Type: "string"},
			{Name: "title", Type: "string"},
		}},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	require.Contains(t, syncSrc, `switch resource {`)
	require.Contains(t, syncSrc, `case "records":`)
	require.Contains(t, syncSrc, `return []string{"hits.hits"}`)
	require.NotContains(t, syncSrc, `resource + "\x00" + path`)
	require.NotContains(t, syncSrc, `case "records\x00/api/records":`)

	testPath := filepath.Join(outputDir, "internal", "cli", "sync_response_path_identity_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"`+naming.CLI(apiSpec.Name)+`/internal/store"
)

type fakeIdentityClient struct {
	responses map[string]json.RawMessage
	calls     []string
}

func (f *fakeIdentityClient) Get(_ context.Context, path string, _ map[string]string) (json.RawMessage, error) {
	f.calls = append(f.calls, path)
	if response, ok := f.responses[path]; ok {
		return response, nil
	}
	return json.RawMessage(`+"`"+`null`+"`"+`), nil
}

func (f *fakeIdentityClient) RateLimit() float64 { return 0 }

func openIdentityTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})
	return db
}

func TestResponsePathForResourceIgnoresLiveRequestPath(t *testing.T) {
	for _, path := range []string{
		"https://records.example.test/api/records",
		"/api/records/",
		"/proxy/v2/api/records",
		"/parents/p1/records",
		"",
	} {
		got := responsePathForResource("records", path)
		if len(got) != 1 || got[0] != "hits.hits" {
			t.Fatalf("responsePathForResource(%q) = %#v, want [hits.hits]", path, got)
		}
	}
}

func TestExtractUnwrapsWhenRequestPathIsNotSpecPath(t *testing.T) {
	body := json.RawMessage(`+"`"+`{"hits":{"hits":[{"id":"r1","title":"one"},{"id":"r2","title":"two"}]}}`+"`"+`)
	path := "https://records.example.test/api/records"
	items, _, _ := extractPageItemsWithPagination(body, "", "", responsePathForResource("records", path)...)
	if len(items) != 2 {
		t.Fatalf("absolute-path unwrap got %d items, want 2", len(items))
	}
}

func TestSyncResourceStoresNestedEnvelope(t *testing.T) {
	db := openIdentityTestStore(t)
	body := json.RawMessage(`+"`"+`{"hits":{"hits":[{"id":"r1","title":"one"},{"id":"r2","title":"two"}]}}`+"`"+`)
	client := &fakeIdentityClient{responses: map[string]json.RawMessage{
		"/api/records": body,
	}}
	var events bytes.Buffer

	res := syncResource(context.Background(), client, db, "records", "", true, 0, false, false, nil, &events)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v\nevents: %s", res.Err, events.String())
	}
	if res.Count != 2 {
		t.Fatalf("syncResource count = %d, want 2; events: %s", res.Count, events.String())
	}
	stored, err := db.Count("records")
	if err != nil {
		t.Fatalf("count store: %v", err)
	}
	if stored != 2 {
		t.Fatalf("store count = %d, want 2", stored)
	}
}
`), 0o644))

	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "Test(ResponsePathForResourceIgnoresLiveRequestPath|ExtractUnwrapsWhenRequestPathIsNotSpecPath|SyncResourceStoresNestedEnvelope)", "-count=1")
}

func TestResponsePathCasesPrefersSyncableAcrossDifferentPaths(t *testing.T) {
	t.Parallel()

	cases := responsePathCases(map[string]spec.Resource{
		"photos": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:       "GET",
					Path:         "/photos",
					Syncable:     true,
					ResponsePath: "catalog.items",
				},
				"search": {
					Method:       "GET",
					Path:         "/photos/search",
					ResponsePath: "photos",
				},
			},
		},
	})

	assert.Equal(t, []responsePathCase{{
		Key:          "photos",
		ResponsePath: "catalog.items",
	}}, cases)
}

func TestResponsePathCasesPrefersListOverCreate(t *testing.T) {
	t.Parallel()

	cases := responsePathCases(map[string]spec.Resource{
		"invoices": {
			Endpoints: map[string]spec.Endpoint{
				"create": {
					Method:       "POST",
					Path:         "/invoices",
					ResponsePath: "data",
					Response:     spec.ResponseDef{Type: "object"},
				},
				"list": {
					Method:       "GET",
					Path:         "/invoices",
					ResponsePath: "data.list",
					Response:     spec.ResponseDef{Type: "array"},
				},
			},
		},
	})

	require.Equal(t, []responsePathCase{{
		Key:          "invoices",
		ResponsePath: "data.list",
	}}, cases, "resource-level sync fallback must use list's envelope, not create's")
}

func TestResponsePathCasesUniformPathUnchanged(t *testing.T) {
	t.Parallel()

	cases := responsePathCases(map[string]spec.Resource{
		"notes": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:       "GET",
					Path:         "/notes",
					ResponsePath: "results",
				},
			},
		},
	})

	assert.Equal(t, []responsePathCase{{
		Key:          "notes",
		ResponsePath: "results",
	}}, cases)
}

func TestGeneratedSyncPrefersListResponsePathWhenCreateDiffers(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("invoice-path")
	apiSpec.Resources = map[string]spec.Resource{
		"invoices": {
			Description: "Invoices",
			Endpoints: map[string]spec.Endpoint{
				"create": {
					Method:       "POST",
					Path:         "/invoices",
					Description:  "Create invoice",
					ResponsePath: "data",
					Response:     spec.ResponseDef{Type: "object", Item: "Invoice"},
					Body: []spec.Param{
						{Name: "amount", Type: "number", Required: true},
					},
				},
				"list": {
					Method:       "GET",
					Path:         "/invoices",
					Description:  "List invoices",
					Syncable:     true,
					ResponsePath: "data.list",
					Response:     spec.ResponseDef{Type: "array", Item: "Invoice"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Invoice": {Fields: []spec.TypeField{
			{Name: "id", Type: "string"},
			{Name: "amount", Type: "number"},
		}},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	require.Contains(t, syncSrc, `case "invoices":`)
	require.Contains(t, syncSrc, `return []string{"data.list"}`)
	require.NotContains(t, syncSrc, `return []string{"data"}`,
		"resource-level sync fallback must not collapse to create's envelope")

	listSrc := readGeneratedFile(t, outputDir, "internal", "cli", "invoices_list.go")
	require.Contains(t, listSrc, `"data.list"`)

	createSrc := readGeneratedFile(t, outputDir, "internal", "cli", "invoices_create.go")
	require.Contains(t, createSrc, `"data"`,
		"create must keep its endpoint-level response_path")
	require.NotContains(t, createSrc, `"data.list"`,
		"create must not inherit list's response_path")

	testPath := filepath.Join(outputDir, "internal", "cli", "sync_response_path_list_pref_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"`+naming.CLI(apiSpec.Name)+`/internal/store"
)

type fakeInvoiceClient struct {
	responses map[string]json.RawMessage
}

func (f *fakeInvoiceClient) Get(_ context.Context, path string, _ map[string]string) (json.RawMessage, error) {
	if response, ok := f.responses[path]; ok {
		return response, nil
	}
	return json.RawMessage(`+"`"+`null`+"`"+`), nil
}

func (f *fakeInvoiceClient) RateLimit() float64 { return 0 }

func TestResponsePathForResourceUsesListEnvelope(t *testing.T) {
	got := responsePathForResource("invoices", "/invoices")
	if len(got) != 1 || got[0] != "data.list" {
		t.Fatalf("responsePathForResource(invoices) = %#v, want [data.list]", got)
	}
}

func TestSyncResourceUnwrapsNestedListEnvelope(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})
	body := json.RawMessage(`+"`"+`{"data":{"list":[{"id":"inv_1","amount":10},{"id":"inv_2","amount":20}]}}`+"`"+`)
	client := &fakeInvoiceClient{responses: map[string]json.RawMessage{"/invoices": body}}
	var events bytes.Buffer
	res := syncResource(context.Background(), client, db, "invoices", "", true, 0, false, false, nil, &events)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v\nevents: %s", res.Err, events.String())
	}
	if res.Count != 2 {
		t.Fatalf("syncResource count = %d, want 2; events: %s", res.Count, events.String())
	}
}
`), 0o644))

	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "Test(ResponsePathForResourceUsesListEnvelope|SyncResourceUnwrapsNestedListEnvelope)", "-count=1")
}
