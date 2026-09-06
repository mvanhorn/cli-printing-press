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
