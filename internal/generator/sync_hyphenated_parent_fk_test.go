package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateDependentSyncHyphenatedParentFKPopulatesTypedTable(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hyphenated-parent-fk")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"chat-attendees": {
			Description: "Chat attendees",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/chat-attendees",
					Description: "List chat attendees",
					Response:    spec.ResponseDef{Type: "array"},
				},
			},
			SubResources: map[string]spec.Resource{
				"chats": {
					Description: "Attendee chats",
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method:      "GET",
							Path:        "/chat-attendees/{id}/chats",
							Description: "List chats for an attendee",
							Response:    spec.ResponseDef{Type: "array"},
							Params:      []spec.Param{{Name: "id", Type: "string", Required: true, Positional: true}},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	gen.profile = &profiler.APIProfile{
		SyncableResources: []profiler.SyncableResource{
			{Name: "chat-attendees", Path: "/chat-attendees", Method: "GET"},
		},
		DependentSyncResources: []profiler.DependentResource{
			{Name: "chats", ParentResource: "chat-attendees", ParentIDParam: "id", Path: "/chat-attendees/{id}/chats", Method: "GET"},
		},
	}
	require.NoError(t, gen.Generate())
	requireGeneratedCompiles(t, outputDir)

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncContent := string(syncGo)
	assert.Contains(t, syncContent, `Name: "chats", ParentTable: "chat-attendees"`,
		"fixture must keep the hyphenated parent resource name at runtime")
	assert.Contains(t, syncContent, `parentFKKey := parentFKColumnName(dep.ParentTable)`,
		"dependent sync must derive the typed parent FK through the shared helper")
	assert.Contains(t, syncContent, `return strings.ReplaceAll(parentTable, "-", "_") + "_id"`,
		"emitted helper must match the schema builder hyphen-to-underscore rule")
	assert.NotContains(t, syncContent, `parentFKKey := dep.ParentTable + "_id"`,
		"raw ParentTable concatenation reintroduces the hyphenated FK mismatch")
	assert.Equal(t, parentFKColumnName("chats"), "chats_id",
		"single-word parents must stay unchanged")

	storeGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	storeContent := string(storeGo)
	assert.Contains(t, storeContent, `lookupFieldValue(obj, "chat_attendees_id")`,
		"typed upsert must read the normalized NOT NULL parent FK column")
	assert.Contains(t, storeContent, `"chat_attendees_id" TEXT NOT NULL`,
		"typed chats table must declare the normalized parent FK")

	module := naming.CLI(apiSpec.Name)
	inlineTest := `package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"` + module + `/internal/store"
)

type hyphenatedParentFKClient struct{}

func (hyphenatedParentFKClient) Get(ctx context.Context, path string, params map[string]string) (json.RawMessage, error) {
	return json.RawMessage(` + "`" + `[
		{"id":"chat-1","subject":"hello"},
		{"id":"chat-2","subject":"follow-up"}
	]` + "`" + `), nil
}

func (hyphenatedParentFKClient) RateLimit() float64 { return 0 }

func TestSyncHyphenatedParentTypedCountMatchesResources(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	if err := db.Upsert("chat-attendees", "att-A", []byte(` + "`" + `{"id":"att-A"}` + "`" + `)); err != nil {
		t.Fatalf("insert parent attendee: %v", err)
	}

	res := syncDependentResource(
		context.Background(),
		hyphenatedParentFKClient{},
		db,
		dependentResourceDef{Name: "chats", ParentTable: "chat-attendees", ParentIDParam: "id", PathTemplate: "/chat-attendees/{id}/chats"},
		"", false, 1, false, false, nil, nil, 1,
	)
	if res.Err != nil {
		t.Fatalf("syncDependentResource error: %v", res.Err)
	}
	if res.Count != 2 {
		t.Fatalf("synced count = %d, want 2", res.Count)
	}

	var typed, generic int
	if err := db.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM chats` + "`" + `).Scan(&typed); err != nil {
		t.Fatalf("count typed chats: %v", err)
	}
	if err := db.DB().QueryRow(` + "`" + `SELECT COUNT(*) FROM resources WHERE resource_type = 'chats'` + "`" + `).Scan(&generic); err != nil {
		t.Fatalf("count generic chats: %v", err)
	}
	if typed != generic {
		t.Fatalf("typed chats = %d, generic resources = %d; typed projection was dropped", typed, generic)
	}
	if typed != 2 {
		t.Fatalf("typed chats = %d, want 2", typed)
	}

	rows, err := db.DB().Query(` + "`" + `SELECT id, chat_attendees_id FROM chats ORDER BY id` + "`" + `)
	if err != nil {
		t.Fatalf("query chats: %v", err)
	}
	defer rows.Close()

	got := 0
	for rows.Next() {
		var id, parentFK string
		if err := rows.Scan(&id, &parentFK); err != nil {
			t.Fatalf("scan chat: %v", err)
		}
		if parentFK != "att-A" {
			t.Fatalf("chat %q chat_attendees_id = %q, want att-A", id, parentFK)
		}
		got++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate chats: %v", err)
	}
	if got != 2 {
		t.Fatalf("scanned %d typed rows, want 2", got)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "hyphenated_parent_fk_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "-run", "TestSyncHyphenatedParentTypedCountMatchesResources", "./internal/cli")
}
