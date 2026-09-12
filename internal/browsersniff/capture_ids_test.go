package browsersniff

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/piiplaceholders"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	capturedCLIID       = "cli_a1b2c3d4e5f6g7h8i9j0"
	capturedCLIExample  = "cli_example0000000000000"
	capturedPropID      = "prop_k9m2n3p4q5r6s7t8u9v0"
	capturedPropExample = "prop_example0000000000000"
	capturedUUID        = "7c9e6679-7425-40de-944b-e07fc1f90ae7"
)

func TestSanitizeCapturedResourceIDValue(t *testing.T) {
	t.Parallel()

	got := sanitizeCapturedResourceIDValue(map[string]any{
		"id":         capturedCLIID,
		"clientId":   capturedCLIID,
		"propertyId": capturedPropID,
		"uuid":       capturedUUID,
		"date":       "2026-04-22",
		"slug":       "sample-product",
		"hash":       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"nested": map[string]any{
			"id": capturedPropID,
		},
	})
	object, ok := got.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, capturedCLIExample, object["id"])
	assert.Equal(t, capturedCLIExample, object["clientId"])
	assert.Equal(t, capturedPropExample, object["propertyId"])
	assert.Equal(t, piiplaceholders.SyntheticUUID, object["uuid"])
	assert.Equal(t, "2026-04-22", object["date"])
	assert.Equal(t, "sample-product", object["slug"])
	assert.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", object["hash"])
	nested, ok := object["nested"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, capturedPropExample, nested["id"])
	assert.NotContains(t, object["id"], capturedCLIID)
}

func TestSanitizeCapturedResourceIDValue_Idempotent(t *testing.T) {
	t.Parallel()

	first := sanitizeCapturedResourceIDValue(map[string]any{"id": capturedCLIID})
	second := sanitizeCapturedResourceIDValue(first)
	assert.Equal(t, first, second)
}

func TestSanitizeSpecCapturedResourceIDs_OmitsScalarIDDefaults(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"clients": {
				Endpoints: map[string]spec.Endpoint{
					"get": {
						Params: []spec.Param{
							{Name: "id", Type: "string", Required: true, Default: capturedCLIID},
							{Name: "q", Type: "string", Default: "hello"},
						},
						Body: []spec.Param{
							{
								Name:    "variables",
								Type:    "object",
								Default: map[string]any{"id": capturedCLIID},
							},
							{Name: "operationName", Type: "string", Default: "GetClient"},
						},
						Example: "clients get --variables {\"id\":\"" + capturedCLIID + "\"}",
					},
				},
			},
		},
	}

	SanitizeSpecCapturedResourceIDs(apiSpec)

	get := apiSpec.Resources["clients"].Endpoints["get"]
	assert.Nil(t, get.Params[0].Default)
	assert.Equal(t, "hello", get.Params[1].Default)
	vars, ok := get.Body[0].Default.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, capturedCLIExample, vars["id"])
	assert.Equal(t, "GetClient", get.Body[1].Default)
	assert.NotContains(t, get.Example, capturedCLIID)
	assert.Contains(t, get.Example, capturedCLIExample)
}

func TestSanitizeSpecCapturedResourceIDs_KeepsDispatchAndSecrets(t *testing.T) {
	t.Parallel()

	githubToken := "ghp_" + "abcdefghijklmnopqrstuvwxyz0123456789"
	apiSpec := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"search": {
				Endpoints: map[string]spec.Endpoint{
					"run": {
						Params: []spec.Param{
							{Name: "type", Type: "string", Default: "domain_rank", DispatchParam: true},
						},
						Body: []spec.Param{
							{Name: "token", Type: "string", Default: githubToken},
							{
								Name: "extensions",
								Type: "object",
								Default: map[string]any{
									"persistedQuery": map[string]any{
										"version":    1,
										"sha256Hash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	SanitizeSpecCapturedResourceIDs(apiSpec)

	run := apiSpec.Resources["search"].Endpoints["run"]
	assert.Equal(t, "domain_rank", run.Params[0].Default)
	assert.Equal(t, githubToken, run.Body[0].Default)
	extensions, ok := run.Body[1].Default.(map[string]any)
	require.True(t, ok)
	persisted, ok := extensions["persistedQuery"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", persisted["sha256Hash"])
}

func TestSanitizeSpecCapturedResourceIDs_KeepsSnakeResourceNames(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"issue_categories": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:  "GET",
						Path:    "/issue_categories",
						Example: "  snake-example-pp-cli issue_categories list --from-spec",
						Params: []spec.Param{
							{Name: "slug", Type: "string", Default: "issue_categories"},
						},
					},
				},
			},
		},
	}

	SanitizeSpecCapturedResourceIDs(apiSpec)

	list := apiSpec.Resources["issue_categories"].Endpoints["list"]
	assert.Equal(t, "  snake-example-pp-cli issue_categories list --from-spec", list.Example)
	assert.Equal(t, "issue_categories", list.Params[0].Default)
}
