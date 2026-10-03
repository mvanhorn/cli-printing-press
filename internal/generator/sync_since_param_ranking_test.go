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

func TestGeneratedSyncRanksTemporalFilterParams(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "temporal-ranking",
		Version: "1.0.0",
		BaseURL: "https://api.example.com",
		Resources: map[string]spec.Resource{
			"sales": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/sales",
						Response: spec.ResponseDef{Type: "array"},
						Params: []spec.Param{
							{Name: "after", In: "query", Type: "string", Format: "date"},
							{Name: "before", In: "query", Type: "string", Format: "date"},
						},
					},
				},
			},
			"events": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/events",
						Response: spec.ResponseDef{Type: "array"},
						Params: []spec.Param{
							{Name: "startDate", In: "query", Type: "string", Format: "date-time"},
							{Name: "endDate", In: "query", Type: "string", Format: "date-time"},
							{Name: "updatedMin", In: "query", Type: "string", Format: "date-time"},
						},
					},
				},
			},
			"users": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:   "GET",
						Path:     "/users",
						Response: spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	sinceParams := generatedFunctionBody(t, string(syncGo), "func syncResourceSinceParam(resource string) string")
	sinceFormats := generatedFunctionBody(t, string(syncGo), "func syncResourceSinceParamFormat(resource string) string")

	assert.Contains(t, sinceParams, "case \"sales\":\n\t\treturn \"after\"")
	assert.Contains(t, sinceFormats, "case \"sales\":\n\t\treturn \"date\"")
	assert.Contains(t, sinceParams, "case \"events\":\n\t\treturn \"updatedMin\"")
	assert.Contains(t, sinceFormats, "case \"events\":\n\t\treturn \"date-time\"")
	assert.NotContains(t, sinceParams, "case \"users\":")

	requireGeneratedCompiles(t, outputDir)
}
