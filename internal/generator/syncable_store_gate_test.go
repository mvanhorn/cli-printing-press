package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGenerateSyncableSmallAPIEmitsLocalDataLayer(t *testing.T) {
	t.Parallel()

	apiSpec := smallReadWriteSyncableOutputSpec("small-syncable")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	require.FileExists(t, filepath.Join(outputDir, "internal", "store", "store.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "search.go"))
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratePostOnlyAPIStillSkipsLocalDataLayer(t *testing.T) {
	t.Parallel()

	apiSpec := postOnlyOutputSpec("post-only-output")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	require.NoFileExists(t, filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "search.go"))

	_, err := os.Stat(filepath.Join(outputDir, "internal", "store"))
	require.True(t, os.IsNotExist(err), "post-only API must not reserve internal/store")
}

func smallReadWriteSyncableOutputSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Resources = map[string]spec.Resource{
		"deliveries": {
			Description: "Manage deliveries",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/deliveries",
					Description: "List deliveries",
					Response:    spec.ResponseDef{Type: "object", Item: "DeliveriesResponse"},
				},
				"add": {
					Method:      "POST",
					Path:        "/add-delivery",
					Description: "Add delivery",
					Body: []spec.Param{
						{Name: "tracking_number", Type: "string", Required: true},
						{Name: "carrier_code", Type: "string", Required: true},
						{Name: "description", Type: "string", Required: true},
					},
					Response: spec.ResponseDef{Type: "object", Item: "SuccessResponse"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Delivery": {
			Fields: []spec.TypeField{
				{Name: "carrier_code", Type: "string"},
				{Name: "description", Type: "string"},
				{Name: "status_code", Type: "integer"},
				{Name: "tracking_number", Type: "string"},
			},
		},
		"DeliveriesResponse": {
			Fields: []spec.TypeField{
				{Name: "success", Type: "boolean"},
				{Name: "error_message", Type: "string"},
				{Name: "deliveries", Type: "array"},
			},
		},
		"SuccessResponse": {
			Fields: []spec.TypeField{
				{Name: "success", Type: "boolean"},
				{Name: "error_message", Type: "string"},
			},
		},
	}
	return apiSpec
}

func postOnlyOutputSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Resources = map[string]spec.Resource{
		"deliveries": {
			Description: "Manage deliveries",
			Endpoints: map[string]spec.Endpoint{
				"add": {
					Method:      "POST",
					Path:        "/add-delivery",
					Description: "Add delivery",
					Body: []spec.Param{
						{Name: "tracking_number", Type: "string", Required: true},
					},
					Response: spec.ResponseDef{Type: "object", Item: "SuccessResponse"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"SuccessResponse": {
			Fields: []spec.TypeField{
				{Name: "success", Type: "boolean"},
				{Name: "error_message", Type: "string"},
			},
		},
	}
	return apiSpec
}
