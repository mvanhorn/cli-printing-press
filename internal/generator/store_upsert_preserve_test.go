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

// Emitted store must compile and honor id-less fingerprint writes plus
// keep-richer list/detail merges, including identity-keyed array merge.
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
	require.Contains(t, src, `"forecast": true`,
		"id-less forecast must be opted into parameter-fingerprint writes")
	require.NotContains(t, src, `"mutations": true`,
		"resources with a real id must keep ExtractResourceID keying")
	require.Contains(t, src, `id := ResolveStorageID("forecast", obj)`,
		"typed id-less Upsert must not require ExtractResourceID")
	require.Contains(t, src, `data = s.mergeIncomingResourceData(tx, "forecast", storageID, data)`,
		"typed upsert must merge before projecting columns")
	require.Contains(t, src, `item = s.mergeIncomingResourceData(tx, resourceType, storageID, item)`,
		"batch upsert must merge before both generic and typed writes")
	require.Contains(t, src, "func arrayItemIdentity(",
		"object arrays must match by a stable id, not numeric index")
	require.Contains(t, src, "func suffixIdentityFromItemKeys(",
		"nested items must pair on suffix identities like currency_code")
	require.Contains(t, src, "func sharedForeignKeyStem(",
		"item identity must skip shared FKs when an own-identity key exists")
	require.Contains(t, src, `canonicalIDFromKey(obj, "sku")`,
		"line items keyed by sku must keep-richer-merge")
	require.Contains(t, src, "func indexObjectArrayByIdentity(",
		"cached array leftovers must be indexed by identity for keep-richer pairing")
	require.NotContains(t, src, `id := ExtractResourceID("forecast", obj)`,
		"typed forecast writer must not still key only on ExtractResourceID")

	requireGeneratedCompiles(t, outputDir)

	testPath := filepath.Join(outputDir, "internal", "store", "idless_richer_runtime_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(generatedIDLessRicherRuntimeTest), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/store",
		"-run", "Test(ResolveStorageID_KeepsEntityIDsAndRefusesUnusable|Upsert_PreservesRicherCachedDetail|MergeKeepRicherJSON|UpsertForecast_StoresIDLessPayload|UpsertForecast_BatchSameLocation|UpsertMutations_ListDoesNotShrinkDetail|UpsertMutations_ArrayReorderDoesNotCorrupt|UpsertMutations_SuffixArrayIdentityKeepsDetail|UpsertMutations_SharedFKDoesNotStealSuffixIdentity)",
		"-count=1")
}

const generatedIDLessRicherRuntimeTest = "package store\n\n" +
	"import (\n" +
	"\t\"encoding/json\"\n" +
	"\t\"fmt\"\n" +
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
	"func TestUpsertForecast_BatchSameLocation(t *testing.T) {\n" +
	"\ts, err := Open(filepath.Join(t.TempDir(), \"data.db\"))\n" +
	"\tif err != nil {\n" +
	"\t\tt.Fatalf(\"open: %v\", err)\n" +
	"\t}\n" +
	"\tdefer s.Close()\n\n" +
	"\tfirst := json.RawMessage(`{\"latitude\":52.52,\"longitude\":13.41,\"generationtime_ms\":0.1,\"hourly\":{\"temperature_2m\":[1,2]}}`)\n" +
	"\tstored, extractFailures, err := s.UpsertBatch(\"forecast\", []json.RawMessage{first})\n" +
	"\tif err != nil {\n" +
	"\t\tt.Fatalf(\"UpsertBatch: %v\", err)\n" +
	"\t}\n" +
	"\tif stored != 1 || extractFailures != 0 {\n" +
	"\t\tt.Fatalf(\"stored/extractFailures = %d/%d, want 1/0\", stored, extractFailures)\n" +
	"\t}\n" +
	"\tupdated := json.RawMessage(`{\"latitude\":52.52,\"longitude\":13.41,\"generationtime_ms\":9.9,\"hourly\":{\"temperature_2m\":[3,4]}}`)\n" +
	"\tif stored, extractFailures, err = s.UpsertBatch(\"forecast\", []json.RawMessage{updated}); err != nil {\n" +
	"\t\tt.Fatalf(\"update: %v\", err)\n" +
	"\t}\n" +
	"\tif stored != 1 || extractFailures != 0 {\n" +
	"\t\tt.Fatalf(\"update stored/extractFailures = %d/%d, want 1/0\", stored, extractFailures)\n" +
	"\t}\n" +
	"\trows, err := s.List(\"forecast\", 10)\n" +
	"\tif err != nil {\n" +
	"\t\tt.Fatalf(\"list: %v\", err)\n" +
	"\t}\n" +
	"\tif len(rows) != 1 {\n" +
	"\t\tt.Fatalf(\"same-location rows = %d, want 1\", len(rows))\n" +
	"\t}\n" +
	"\tother := json.RawMessage(`{\"latitude\":40.7,\"longitude\":-74.0,\"hourly\":{\"temperature_2m\":[10]}}`)\n" +
	"\tif stored, extractFailures, err = s.UpsertBatch(\"forecast\", []json.RawMessage{other}); err != nil {\n" +
	"\t\tt.Fatalf(\"other location: %v\", err)\n" +
	"\t}\n" +
	"\tif stored != 1 || extractFailures != 0 {\n" +
	"\t\tt.Fatalf(\"other stored/extractFailures = %d/%d, want 1/0\", stored, extractFailures)\n" +
	"\t}\n" +
	"\trows, err = s.List(\"forecast\", 10)\n" +
	"\tif err != nil {\n" +
	"\t\tt.Fatalf(\"list after other: %v\", err)\n" +
	"\t}\n" +
	"\tif len(rows) != 2 {\n" +
	"\t\tt.Fatalf(\"distinct-location rows = %d, want 2\", len(rows))\n" +
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
	"}\n\n" +
	"func TestUpsertMutations_ArrayReorderDoesNotCorrupt(t *testing.T) {\n" +
	"\ts, err := Open(filepath.Join(t.TempDir(), \"data.db\"))\n" +
	"\tif err != nil {\n" +
	"\t\tt.Fatalf(\"open: %v\", err)\n" +
	"\t}\n" +
	"\tdefer s.Close()\n\n" +
	"\tdetail := json.RawMessage(`{\"id\":\"mut-2\",\"name\":\"Invoice\",\"rows\":[{\"id\":\"a\",\"vat\":21,\"notes\":\"keep\"},{\"id\":\"b\",\"vat\":9},{\"id\":\"c\",\"vat\":0}]}`)\n" +
	"\tif err := s.UpsertMutations(detail); err != nil {\n" +
	"\t\tt.Fatalf(\"detail UpsertMutations: %v\", err)\n" +
	"\t}\n" +
	"\treordered := json.RawMessage(`{\"id\":\"mut-2\",\"rows\":[{\"id\":\"c\",\"vat\":0},{\"id\":\"a\",\"vat\":22}]}`)\n" +
	"\tif err := s.UpsertMutations(reordered); err != nil {\n" +
	"\t\tt.Fatalf(\"reordered UpsertMutations: %v\", err)\n" +
	"\t}\n" +
	"\tgot, err := s.Get(\"mutations\", \"mut-2\")\n" +
	"\tif err != nil {\n" +
	"\t\tt.Fatalf(\"get: %v\", err)\n" +
	"\t}\n" +
	"\tvar obj map[string]any\n" +
	"\tif err := json.Unmarshal(got, &obj); err != nil {\n" +
	"\t\tt.Fatalf(\"unmarshal: %v\", err)\n" +
	"\t}\n" +
	"\trows, _ := obj[\"rows\"].([]any)\n" +
	"\tif len(rows) != 2 {\n" +
	"\t\tt.Fatalf(\"array length must follow incoming, got %d stored %s\", len(rows), got)\n" +
	"\t}\n" +
	"\tfirst, _ := rows[0].(map[string]any)\n" +
	"\tsecond, _ := rows[1].(map[string]any)\n" +
	"\tif fmt.Sprint(first[\"id\"]) != \"c\" || fmt.Sprint(second[\"id\"]) != \"a\" {\n" +
	"\t\tt.Fatalf(\"array order must follow incoming, stored %s\", got)\n" +
	"\t}\n" +
	"\tif fmt.Sprint(second[\"vat\"]) != \"22\" {\n" +
	"\t\tt.Fatalf(\"incoming scalar on matched object should win, stored %s\", got)\n" +
	"\t}\n" +
	"\tif fmt.Sprint(second[\"notes\"]) != \"keep\" {\n" +
	"\t\tt.Fatalf(\"keep-richer lost notes on id match, stored %s\", got)\n" +
	"\t}\n" +
	"\tfor _, row := range rows {\n" +
	"\t\tobj, _ := row.(map[string]any)\n" +
	"\t\tif fmt.Sprint(obj[\"id\"]) == \"b\" {\n" +
	"\t\t\tt.Fatalf(\"dropped middle entry must not remain, stored %s\", got)\n" +
	"\t\t}\n" +
	"\t}\n" +
	"}\n\n" +
	"func TestUpsertMutations_SuffixArrayIdentityKeepsDetail(t *testing.T) {\n" +
	"\ts, err := Open(filepath.Join(t.TempDir(), \"data.db\"))\n" +
	"\tif err != nil {\n" +
	"\t\tt.Fatalf(\"open: %v\", err)\n" +
	"\t}\n" +
	"\tdefer s.Close()\n\n" +
	"\tdetail := json.RawMessage(`{\"id\":\"mut-3\",\"rows\":[{\"currency_code\":\"USD\",\"notes\":\"keep\"},{\"currency_code\":\"EUR\"},{\"currency_code\":\"GBP\"}]}`)\n" +
	"\tif err := s.UpsertMutations(detail); err != nil {\n" +
	"\t\tt.Fatalf(\"detail: %v\", err)\n" +
	"\t}\n" +
	"\tlist := json.RawMessage(`{\"id\":\"mut-3\",\"rows\":[{\"currency_code\":\"EUR\"},{\"currency_code\":\"USD\"}]}`)\n" +
	"\tif err := s.UpsertMutations(list); err != nil {\n" +
	"\t\tt.Fatalf(\"list: %v\", err)\n" +
	"\t}\n" +
	"\tgot, err := s.Get(\"mutations\", \"mut-3\")\n" +
	"\tif err != nil {\n" +
	"\t\tt.Fatalf(\"get: %v\", err)\n" +
	"\t}\n" +
	"\tvar obj map[string]any\n" +
	"\tif err := json.Unmarshal(got, &obj); err != nil {\n" +
	"\t\tt.Fatalf(\"unmarshal: %v\", err)\n" +
	"\t}\n" +
	"\trows, _ := obj[\"rows\"].([]any)\n" +
	"\tif len(rows) != 2 {\n" +
	"\t\tt.Fatalf(\"length must follow incoming, got %d stored %s\", len(rows), got)\n" +
	"\t}\n" +
	"\tsecond, _ := rows[1].(map[string]any)\n" +
	"\tif fmt.Sprint(second[\"currency_code\"]) != \"USD\" {\n" +
	"\t\tt.Fatalf(\"order must follow incoming, stored %s\", got)\n" +
	"\t}\n" +
	"\tif fmt.Sprint(second[\"notes\"]) != \"keep\" {\n" +
	"\t\tt.Fatalf(\"suffix identity lost detail, stored %s\", got)\n" +
	"\t}\n" +
	"}\n\n" +
	"func TestUpsertMutations_SharedFKDoesNotStealSuffixIdentity(t *testing.T) {\n" +
	"\ts, err := Open(filepath.Join(t.TempDir(), \"data.db\"))\n" +
	"\tif err != nil {\n" +
	"\t\tt.Fatalf(\"open: %v\", err)\n" +
	"\t}\n" +
	"\tdefer s.Close()\n\n" +
	"\tdetail := json.RawMessage(`{\"id\":\"mut-4\",\"rows\":[{\"account_id\":\"acct\",\"currency_code\":\"USD\",\"notes\":\"usd\"},{\"account_id\":\"acct\",\"currency_code\":\"EUR\",\"notes\":\"eur\"}]}`)\n" +
	"\tif err := s.UpsertMutations(detail); err != nil {\n" +
	"\t\tt.Fatalf(\"detail: %v\", err)\n" +
	"\t}\n" +
	"\tlist := json.RawMessage(`{\"id\":\"mut-4\",\"rows\":[{\"account_id\":\"acct\",\"currency_code\":\"EUR\"},{\"account_id\":\"acct\",\"currency_code\":\"USD\"}]}`)\n" +
	"\tif err := s.UpsertMutations(list); err != nil {\n" +
	"\t\tt.Fatalf(\"list: %v\", err)\n" +
	"\t}\n" +
	"\tgot, err := s.Get(\"mutations\", \"mut-4\")\n" +
	"\tif err != nil {\n" +
	"\t\tt.Fatalf(\"get: %v\", err)\n" +
	"\t}\n" +
	"\tvar obj map[string]any\n" +
	"\tif err := json.Unmarshal(got, &obj); err != nil {\n" +
	"\t\tt.Fatalf(\"unmarshal: %v\", err)\n" +
	"\t}\n" +
	"\trows, _ := obj[\"rows\"].([]any)\n" +
	"\tif len(rows) != 2 {\n" +
	"\t\tt.Fatalf(\"length must follow incoming, got %d stored %s\", len(rows), got)\n" +
	"\t}\n" +
	"\tfirst, _ := rows[0].(map[string]any)\n" +
	"\tsecond, _ := rows[1].(map[string]any)\n" +
	"\tif fmt.Sprint(first[\"currency_code\"]) != \"EUR\" || fmt.Sprint(second[\"currency_code\"]) != \"USD\" {\n" +
	"\t\tt.Fatalf(\"must pair on currency_code, stored %s\", got)\n" +
	"\t}\n" +
	"\tif fmt.Sprint(first[\"notes\"]) != \"eur\" {\n" +
	"\t\tt.Fatalf(\"EUR row lost its notes, stored %s\", got)\n" +
	"\t}\n" +
	"\tif fmt.Sprint(second[\"notes\"]) != \"usd\" {\n" +
	"\t\tt.Fatalf(\"USD row lost its notes, stored %s\", got)\n" +
	"\t}\n" +
	"}\n"
