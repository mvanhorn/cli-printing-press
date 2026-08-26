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

func TestGeneratedStoreResolvesNestedAndUnusableRecordIDs(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "nestedidstore",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/nestedidstore-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"entities": {
				Description: "Entities keyed on a nested identifier",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/entities",
						Description: "List entities",
						Response:    spec.ResponseDef{Type: "array"},
						IDField:     "entityInfo.entityId",
					},
				},
			},
			"devices": {
				Description: "Devices using generic ID fallbacks",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/devices",
						Description: "List devices",
						Response:    spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	storeGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	storeContent := string(storeGo)

	assert.Contains(t, storeContent, `"entities": "entityInfo.entityId"`,
		"dotted x-resource-id / id_field must land in resourceIDFieldOverrides")
	assert.Contains(t, storeContent, "func lookupRawDottedFieldValue(",
		"generated store must walk dotted identifier paths")
	assert.Contains(t, storeContent, "func CanonicalResourceID(",
		"generated store must expose CanonicalResourceID")
	assert.Contains(t, storeContent, "func unusableResourceID(",
		"generated store must refuse unusable identifier values")
	assert.Contains(t, storeContent,
		`var genericIDFieldFallbacks = []string{"id", "ID", "_id", "id_", "gid", "sid", "uid", "uuid", "guid", "api_id"}`,
		"generated store must probe id_ as a generic identifier spelling")
	assert.Contains(t, storeContent, "func fieldKeySpellings(",
		"LookupFieldValue must widen identifier keys including trailing-underscore spellings")

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	assert.Contains(t, string(syncGo), `"entities": "entityInfo.entityId"`,
		"sync override map must carry the dotted identifier path")

	requireGeneratedCompiles(t, outputDir)

	runGoCommandRequired(t, outputDir, "test", "./internal/store",
		"-run", "Test(LookupFieldValue_DottedPathAndTrailingUnderscore|ExtractResourceID_NestedOverrideAndIDUnderscore|ExtractResourceID_RefusesUnusableValues|UpsertBatch_NestedOverrideStoresRows|UpsertBatch_RefusesUnusableIDsInsteadOfWritingThem|UpsertBatch_GenericFallbackList|UpsertBatch_TemplatedIDFieldOverrideWins)",
		"-count=1")
}
