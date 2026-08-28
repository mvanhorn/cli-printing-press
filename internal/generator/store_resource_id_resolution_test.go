package generator

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
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

func TestGeneratedStoreResolvesSoleCamelCaseStemUID(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(`openapi: "3.0.3"
info:
  title: Stem UID Alerts
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /account-alerts-open:
    get:
      operationId: listAccountAlertsOpen
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    alertUid: {type: string}
                    alertContext: {type: string}
                    severity: {type: string}
  /account-alerts-resolved:
    get:
      operationId: listAccountAlertsResolved
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    alert_uid: {type: string}
                    alert_context: {type: string}
                    severity: {type: string}
`))
	require.NoError(t, err)
	profile := profiler.Profile(apiSpec)
	byName := make(map[string]string, len(profile.SyncableResources))
	for _, resource := range profile.SyncableResources {
		byName[resource.Name] = resource.IDField
	}
	require.Equal(t, "alertUid", byName["account-alerts-open"])
	require.Equal(t, "alert_uid", byName["account-alerts-resolved"])

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	storeGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	storeContent := string(storeGo)
	syncContent := string(syncGo)
	openOverride := regexp.MustCompile(`"account-alerts-open":\s+"alertUid"`)
	resolvedOverride := regexp.MustCompile(`"account-alerts-resolved":\s+"alert_uid"`)
	assert.Regexp(t, openOverride, storeContent)
	assert.Regexp(t, resolvedOverride, storeContent)
	assert.Regexp(t, openOverride, syncContent)
	assert.Regexp(t, resolvedOverride, syncContent)

	requireGeneratedCompiles(t, outputDir)
}
