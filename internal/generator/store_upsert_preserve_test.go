package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func idlessAndDetailStoreSpec() *spec.APISpec {
	apiSpec := minimalSpec("idless-richer-store")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Disabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"forecast": {
			Description: "Parameter-shaped forecast",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/v1/forecast",
					Description: "Get forecast",
					Response:    spec.ResponseDef{Type: "object", Item: "Forecast"},
				},
			},
		},
		"mutations": {
			Description: "Mutations with richer detail than list",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/mutations",
					Description: "List mutations",
					Response:    spec.ResponseDef{Type: "array", Item: "Mutation"},
				},
				"get": {
					Method:      "GET",
					Path:        "/mutations/{id}",
					Description: "Get mutation",
					Params:      []spec.Param{{Name: "id", Type: "string", Required: true, PathParam: true}},
					Response:    spec.ResponseDef{Type: "object", Item: "Mutation"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Forecast": {
			Fields: []spec.TypeField{
				{Name: "latitude", Type: "number"},
				{Name: "longitude", Type: "number"},
				{Name: "timezone", Type: "string"},
				{Name: "timezone_abbreviation", Type: "string"},
				{Name: "elevation", Type: "number"},
				{Name: "utc_offset_seconds", Type: "integer"},
				{Name: "hourly", Type: "object"},
				{Name: "current", Type: "object"},
			},
		},
		"Mutation": {
			Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "name", Type: "string"},
				{Name: "description", Type: "string"},
				{Name: "status", Type: "string"},
				{Name: "rows", Type: "array"},
			},
		},
	}
	return apiSpec
}

// TestGeneratedStoreKeepsIDLessAndRicherUpserts proves the emitted store
// contract for parameter-shaped writes and keep-richer list/detail merges.
func TestGeneratedStoreKeepsIDLessAndRicherUpserts(t *testing.T) {
	t.Parallel()

	apiSpec := idlessAndDetailStoreSpec()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	src := string(storeSrc)

	require.Contains(t, src, "func ResolveStorageID(",
		"id-less writers must share ResolveStorageID")
	require.Contains(t, src, "func parameterSnapshotID(",
		"parameter-shaped rows must be keyed by a request-equivalent fingerprint")
	require.Contains(t, src, "func mergeKeepRicherJSON(",
		"conflict writes must apply the documented keep-richer merge")
	require.Contains(t, src, "func (s *Store) mergeIncomingResourceData(",
		"generic and typed upserts must merge against the cached blob")
	require.Contains(t, src, `id := ResolveStorageID("forecast", obj)`,
		"typed id-less Upsert must not require ExtractResourceID")
	require.Contains(t, src, `data = s.mergeIncomingResourceData(tx, "forecast", storageID, data)`,
		"typed upsert must merge before projecting columns")
	require.Contains(t, src, `item = s.mergeIncomingResourceData(tx, resourceType, storageID, item)`,
		"batch upsert must merge before both generic and typed writes")
	require.NotContains(t, src, `id := ExtractResourceID("forecast", obj)`,
		"typed forecast writer must not still key only on ExtractResourceID")

	requireGeneratedCompiles(t, outputDir)

	testPath := filepath.Join(outputDir, "internal", "store", "idless_richer_runtime_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(generatedIDLessRicherRuntimeTest), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/store",
		"-run", "Test(ResolveStorageID_ParameterShapedAndEntityIDs|UpsertBatch_StoresIDLessParameterShapedPayload|Upsert_PreservesRicherCachedDetail|MergeKeepRicherJSON|UpsertForecast_StoresIDLessPayload|UpsertMutations_ListDoesNotShrinkDetail)",
		"-count=1")
}

const generatedIDLessRicherRuntimeTest = "package store\n\n" +
	"import (\n" +
	"\t\"encoding/json\"\n" +
	"\t\"path/filepath\"\n" +
	"\t\"strings\"\n" +
	"\t\"testing\"\n" +
	")\n\n" +
	"func TestUpsertForecast_StoresIDLessPayload(t *testing.T) {\n" +
	"\ts, err := Open(filepath.Join(t.TempDir(), \"data.db\"))\n" +
	"\tif err != nil {\n" +
	"\t\tt.Fatalf(\"open: %v\", err)\n" +
	"\t}\n" +
	"\tdefer s.Close()\n\n" +
	"\tpayload := json.RawMessage(`{\"latitude\":52.52,\"longitude\":13.41,\"timezone\":\"Europe/Berlin\",\"hourly\":{\"temperature_2m\":[12.1]}}`)\n" +
	"\tif err := s.UpsertForecast(payload); err != nil {\n" +
	"\t\tt.Fatalf(\"UpsertForecast: %v\", err)\n" +
	"\t}\n" +
	"\trows, err := s.List(\"forecast\", 10)\n" +
	"\tif err != nil {\n" +
	"\t\tt.Fatalf(\"list: %v\", err)\n" +
	"\t}\n" +
	"\tif len(rows) != 1 {\n" +
	"\t\tt.Fatalf(\"forecast rows = %d, want 1\", len(rows))\n" +
	"\t}\n" +
	"\tif !strings.Contains(string(rows[0]), \"Europe/Berlin\") {\n" +
	"\t\tt.Fatalf(\"stored forecast missing payload, got %s\", rows[0])\n" +
	"\t}\n\n" +
	"\tvar typed int\n" +
	"\tif err := s.DB().QueryRow(`SELECT COUNT(*) FROM \"forecast\"`).Scan(&typed); err != nil {\n" +
	"\t\tt.Fatalf(\"typed count: %v\", err)\n" +
	"\t}\n" +
	"\tif typed != 1 {\n" +
	"\t\tt.Fatalf(\"typed forecast rows = %d, want 1\", typed)\n" +
	"\t}\n" +
	"}\n\n" +
	"func TestUpsertMutations_ListDoesNotShrinkDetail(t *testing.T) {\n" +
	"\ts, err := Open(filepath.Join(t.TempDir(), \"data.db\"))\n" +
	"\tif err != nil {\n" +
	"\t\tt.Fatalf(\"open: %v\", err)\n" +
	"\t}\n" +
	"\tdefer s.Close()\n\n" +
	"\tdetail := json.RawMessage(`{\"id\":\"mut-1\",\"name\":\"Invoice\",\"rows\":[{\"vat\":21,\"amount\":100}]}`)\n" +
	"\tif err := s.UpsertMutations(detail); err != nil {\n" +
	"\t\tt.Fatalf(\"detail UpsertMutations: %v\", err)\n" +
	"\t}\n" +
	"\tlist := json.RawMessage(`{\"id\":\"mut-1\",\"name\":\"Invoice list\"}`)\n" +
	"\tif err := s.UpsertMutations(list); err != nil {\n" +
	"\t\tt.Fatalf(\"list UpsertMutations: %v\", err)\n" +
	"\t}\n" +
	"\tgot, err := s.Get(\"mutations\", \"mut-1\")\n" +
	"\tif err != nil {\n" +
	"\t\tt.Fatalf(\"get: %v\", err)\n" +
	"\t}\n" +
	"\tif !strings.Contains(string(got), \"rows\") || !strings.Contains(string(got), \"vat\") {\n" +
	"\t\tt.Fatalf(\"list upsert destroyed detail, got %s\", got)\n" +
	"\t}\n" +
	"\tif !strings.Contains(string(got), \"Invoice list\") {\n" +
	"\t\tt.Fatalf(\"incoming list scalar should win, got %s\", got)\n" +
	"\t}\n\n" +
	"\tvar typedData string\n" +
	"\tif err := s.DB().QueryRow(`SELECT data FROM \"mutations\" WHERE id = ?`, \"mut-1\").Scan(&typedData); err != nil {\n" +
	"\t\tt.Fatalf(\"typed get: %v\", err)\n" +
	"\t}\n" +
	"\tif !strings.Contains(typedData, \"rows\") {\n" +
	"\t\tt.Fatalf(\"typed table lost rows, got %s\", typedData)\n" +
	"\t}\n" +
	"}\n"
