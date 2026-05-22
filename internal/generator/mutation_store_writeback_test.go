package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestMutationCommandsWriteBackEntityResponses(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mutation-writeback")
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Items",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/items",
					Description: "List items",
					Response:    spec.ResponseDef{Type: "array"},
				},
				"create": {
					Method:      "POST",
					Path:        "/items",
					Description: "Create an item",
					Body:        []spec.Param{{Name: "name", Type: "string"}},
				},
				"delete": {
					Method:      "DELETE",
					Path:        "/items/{id}",
					Description: "Delete an item",
					Params:      []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true, PathParam: true}},
				},
			},
		},
		"widgets": {
			Description: "Widgets",
			Endpoints: map[string]spec.Endpoint{
				"create": {
					Method:      "POST",
					Path:        "/widgets",
					Description: "Create a widget",
					Body:        []spec.Param{{Name: "name", Type: "string"}},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "items_create.go")
	require.Contains(t, endpointSrc, `writeMutationResponseToStore(cmd.Context(), "items", data, "")`,
		"non-promoted mutation commands must write entity responses through to the local store")

	deleteSrc := readGeneratedFile(t, outputDir, "internal", "cli", "items_delete.go")
	require.NotContains(t, deleteSrc, "writeMutationResponseToStore",
		"delete commands need delete-aware store handling and must not use the upsert-only write-back path")

	promotedSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_widgets.go")
	require.Contains(t, promotedSrc, `writeMutationResponseToStore(cmd.Context(), "widgets", data, "")`,
		"promoted mutation commands must write entity responses through to the local store")

	behaviorTest := `package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func runMutationWritebackCommand(t *testing.T, args ...string) error {
	t.Helper()
	var flags rootFlags
	cmd := newRootCmd(&flags)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	return cmd.Execute()
}

func withMutationWritebackServer(t *testing.T, status int, body string) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func setMutationWritebackEnv(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MYAPI_TOKEN", "test-token")
	t.Setenv("MUTATION_WRITEBACK_BASE_URL", baseURL)
}

func TestMutationCommandWritesSuccessfulResponseToStore(t *testing.T) {
	server, calls := withMutationWritebackServer(t, http.StatusOK, ` + "`" + `{"id":"cmd-1","name":"From command"}` + "`" + `)
	setMutationWritebackEnv(t, server.URL)

	if err := runMutationWritebackCommand(t, "items", "create", "--name", "From command", "--json"); err != nil {
		t.Fatalf("mutation command: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("mutation command made %d calls, want 1", *calls)
	}

	data, _, err := resolveLocal(context.Background(), nil, nil, "items", true, "/items", nil, "test")
	if err != nil {
		t.Fatalf("resolveLocal after command write-back: %v", err)
	}
	if !strings.Contains(string(data), "cmd-1") {
		t.Fatalf("local list missing command response row: %s", data)
	}
}

func TestPromotedMutationCommandWritesSuccessfulResponseToStore(t *testing.T) {
	server, calls := withMutationWritebackServer(t, http.StatusOK, ` + "`" + `{"id":"widget-1","name":"Promoted"}` + "`" + `)
	setMutationWritebackEnv(t, server.URL)

	if err := runMutationWritebackCommand(t, "widgets", "--name", "Promoted", "--json"); err != nil {
		t.Fatalf("promoted mutation command: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("promoted mutation command made %d calls, want 1", *calls)
	}

	data, _, err := resolveLocal(context.Background(), nil, nil, "widgets", true, "/widgets", nil, "test")
	if err != nil {
		t.Fatalf("resolveLocal after promoted command write-back: %v", err)
	}
	if !strings.Contains(string(data), "widget-1") {
		t.Fatalf("local list missing promoted command response row: %s", data)
	}
}

func TestPromotedMutationCommandSkipsPartialFailureWriteBack(t *testing.T) {
	server, calls := withMutationWritebackServer(t, http.StatusOK, ` + "`" + `{"id":"widget-partial","partialFailureError":{"message":"partial failure","code":3}}` + "`" + `)
	setMutationWritebackEnv(t, server.URL)

	if err := runMutationWritebackCommand(t, "widgets", "--name", "Partial", "--json"); err != nil {
		t.Fatalf("promoted partial-failure mutation command: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("promoted partial-failure mutation command made %d calls, want 1", *calls)
	}
	if _, _, err := resolveLocal(context.Background(), nil, nil, "widgets", true, "/widgets", nil, "test"); err == nil {
		t.Fatalf("promoted partial-failure mutation command must not create local rows")
	}
}

func TestMutationCommandDryRunDoesNotWriteToStore(t *testing.T) {
	server, calls := withMutationWritebackServer(t, http.StatusOK, ` + "`" + `{"id":"dry-run-1","name":"Dry run"}` + "`" + `)
	setMutationWritebackEnv(t, server.URL)

	if err := runMutationWritebackCommand(t, "items", "create", "--name", "Dry run", "--dry-run", "--json"); err != nil {
		t.Fatalf("dry-run mutation command: %v", err)
	}
	if *calls != 0 {
		t.Fatalf("dry-run mutation command made %d calls, want 0", *calls)
	}
	if _, _, err := resolveLocal(context.Background(), nil, nil, "items", true, "/items", nil, "test"); err == nil {
		t.Fatalf("dry-run mutation command must not create local rows")
	}
}

func TestDeleteCommandDoesNotUseUpsertWriteBack(t *testing.T) {
	server, calls := withMutationWritebackServer(t, http.StatusOK, ` + "`" + `{"id":"deleted-1","name":"Deleted"}` + "`" + `)
	setMutationWritebackEnv(t, server.URL)

	if err := runMutationWritebackCommand(t, "items", "delete", "deleted-1", "--json"); err != nil {
		t.Fatalf("delete mutation command: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("delete mutation command made %d calls, want 1", *calls)
	}
	if _, _, err := resolveLocal(context.Background(), nil, nil, "items", true, "/items", nil, "test"); err == nil {
		t.Fatalf("delete mutation command must not upsert a deleted row")
	}
}

func TestWriteMutationResponseToStoreWritesEntity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	writeMutationResponseToStore(context.Background(), "items", json.RawMessage(` + "`" + `{"id":"item-1","name":"Alpha"}` + "`" + `), "")

	data, _, err := resolveLocal(context.Background(), nil, nil, "items", true, "/items", nil, "test")
	if err != nil {
		t.Fatalf("resolveLocal after mutation write-back: %v", err)
	}
	if !strings.Contains(string(data), "item-1") {
		t.Fatalf("local list missing mutation response row: %s", data)
	}
}

func TestWriteMutationResponseToStoreUnwrapsStatusDataEnvelope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	writeMutationResponseToStore(context.Background(), "items", json.RawMessage(` + "`" + `{"status":"success","data":{"id":"item-2","name":"Beta"}}` + "`" + `), "")

	data, _, err := resolveLocal(context.Background(), nil, nil, "items", true, "/items", nil, "test")
	if err != nil {
		t.Fatalf("resolveLocal after mutation envelope write-back: %v", err)
	}
	if !strings.Contains(string(data), "item-2") {
		t.Fatalf("local list missing unwrapped mutation response row: %s", data)
	}
}

func TestWriteMutationResponseToStoreUnwrapsDataEntityEnvelope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	writeMutationResponseToStore(context.Background(), "items", json.RawMessage(` + "`" + `{"data":{"id":"item-5","name":"Epsilon"}}` + "`" + `), "")

	data, _, err := resolveLocal(context.Background(), nil, nil, "items", true, "/items", nil, "test")
	if err != nil {
		t.Fatalf("resolveLocal after data envelope write-back: %v", err)
	}
	if !strings.Contains(string(data), "item-5") {
		t.Fatalf("local list missing data-envelope mutation row: %s", data)
	}
}

func TestWriteMutationResponseToStorePreservesMixedStatusDataEntity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	writeMutationResponseToStore(context.Background(), "items", json.RawMessage(` + "`" + `{"id":"item-6","status":"success","data":{"extra":"details"}}` + "`" + `), "")

	data, _, err := resolveLocal(context.Background(), nil, nil, "items", true, "/items", nil, "test")
	if err != nil {
		t.Fatalf("resolveLocal after mixed envelope write-back: %v", err)
	}
	if !strings.Contains(string(data), "item-6") {
		t.Fatalf("local list missing mixed-envelope mutation row: %s", data)
	}
}

func TestWriteMutationResponseToStoreUsesResponsePath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	writeMutationResponseToStore(context.Background(), "items", json.RawMessage(` + "`" + `{"data":{"createItem":{"item":{"id":"item-3","name":"Gamma"}}}}` + "`" + `), "data.createItem.item")

	data, _, err := resolveLocal(context.Background(), nil, nil, "items", true, "/items", nil, "test")
	if err != nil {
		t.Fatalf("resolveLocal after response-path write-back: %v", err)
	}
	if !strings.Contains(string(data), "item-3") {
		t.Fatalf("local list missing response-path mutation row: %s", data)
	}
}

func TestWriteMutationResponseToStoreSkipsNoEntityResponses(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	writeMutationResponseToStore(context.Background(), "items", nil, "")
	writeMutationResponseToStore(context.Background(), "items", json.RawMessage(` + "`" + `{"success":true}` + "`" + `), "")
	writeMutationResponseToStore(context.Background(), "items", json.RawMessage(` + "`" + `{"status":"noop","reason":"verify_short_circuit"}` + "`" + `), "")
	writeMutationResponseToStore(context.Background(), "items", json.RawMessage(` + "`" + `{"status":"noop","data":{"id":"noop-data-1","name":"Noop"}}` + "`" + `), "")
	writeMutationResponseToStore(context.Background(), "items", json.RawMessage(` + "`" + `{"__pp_verify_synthetic__":true,"id":"synthetic-1","status":"noop","reason":"verify_short_circuit"}` + "`" + `), "")

	if _, _, err := resolveLocal(context.Background(), nil, nil, "items", true, "/items", nil, "test"); err == nil {
		t.Fatalf("success-only mutation responses must not create local rows")
	}
}

func TestWriteMutationResponseToStoreFiltersArrayItemsWithoutIDs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	writeMutationResponseToStore(context.Background(), "items", json.RawMessage(` + "`" + `[{"id":"item-4","name":"Delta"},{"description":"missing-id"}]` + "`" + `), "")

	data, _, err := resolveLocal(context.Background(), nil, nil, "items", true, "/items", nil, "test")
	if err != nil {
		t.Fatalf("resolveLocal after array write-back: %v", err)
	}
	if !strings.Contains(string(data), "item-4") {
		t.Fatalf("local list missing array mutation row: %s", data)
	}
	if strings.Contains(string(data), "missing-id") {
		t.Fatalf("local list stored array item without an ID: %s", data)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "mutation_writeback_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestWriteMutationResponseToStore")

	dataSourceSrc := readGeneratedFile(t, outputDir, "internal", "cli", "data_source.go")
	require.Contains(t, dataSourceSrc, "func writeMutationResponseToStore(")
	require.NotContains(t, dataSourceSrc, "writeMutationResponseToStore(ctx context.Context, resourceType string, data json.RawMessage)")
	require.Contains(t, dataSourceSrc, "mutationResponseEntityItems(resourceType, data, responsePath)")
	require.False(t, strings.Contains(dataSourceSrc, "TODO"), "mutation write-back must not ship as a TODO stub")
}
