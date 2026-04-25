package openapi

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mvanhorn/cli-printing-press/internal/generator"
	"github.com/mvanhorn/cli-printing-press/internal/naming"
	"github.com/mvanhorn/cli-printing-press/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePetstore(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "petstore.yaml"))
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)

	assert.Equal(t, "petstore", parsed.Name)
	assert.Equal(t, "", parsed.BaseURL)
	assert.Equal(t, "/api/v3", parsed.BasePath)
	assert.NotEmpty(t, parsed.Resources)

	hasEndpoint := false
	for _, resource := range parsed.Resources {
		if len(resource.Endpoints) > 0 {
			hasEndpoint = true
			break
		}
	}
	assert.True(t, hasEndpoint)

	assert.NotEmpty(t, parsed.Types)
	assert.Contains(t, parsed.Types, "Pet")
}

func TestParseStytchOpenAPI(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "stytch.yaml"))
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)

	assert.Equal(t, "stytch", parsed.Name)
	assert.NotEmpty(t, parsed.BaseURL)
	assert.NotEmpty(t, parsed.Resources)
	assert.NotEmpty(t, parsed.Types)

	totalEndpoints := 0
	for _, resource := range parsed.Resources {
		totalEndpoints += len(resource.Endpoints)
		for _, sub := range resource.SubResources {
			totalEndpoints += len(sub.Endpoints)
		}
	}
	assert.Greater(t, totalEndpoints, 10)
}

func TestParseGmailOAuth2(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "gmail.yaml"))
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)

	assert.Equal(t, "bearer_token", parsed.Auth.Type)
	assert.Equal(t, "Authorization", parsed.Auth.Header)
	assert.Equal(t, "https://accounts.google.com/o/oauth2/auth", parsed.Auth.AuthorizationURL)
	assert.Equal(t, "https://accounts.google.com/o/oauth2/token", parsed.Auth.TokenURL)
	assert.NotEmpty(t, parsed.Auth.Scopes)
}

func TestSkipUnderscoreFields(t *testing.T) {
	spec := []byte(`
openapi: "3.0.0"
info:
  title: Test
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /items:
    get:
      operationId: listItems
      responses:
        "200":
          description: OK
components:
  schemas:
    Item:
      type: object
      properties:
        id:
          type: string
        name:
          type: string
        _errors:
          type: object
        _internal:
          type: string
`)
	parsed, err := Parse(spec)
	require.NoError(t, err)

	item, ok := parsed.Types["Item"]
	require.True(t, ok)

	// Should have id and name but NOT _errors or _internal
	fieldNames := make([]string, 0)
	for _, f := range item.Fields {
		fieldNames = append(fieldNames, f.Name)
	}
	assert.Contains(t, fieldNames, "id")
	assert.Contains(t, fieldNames, "name")
	assert.NotContains(t, fieldNames, "_errors")
	assert.NotContains(t, fieldNames, "_internal")
}

func TestIsOpenAPI(t *testing.T) {
	t.Parallel()

	openAPIYAML := []byte(`
openapi: 3.0.0
info:
  title: Demo
  version: 1.0.0
paths: {}
`)
	openAPIJSON := []byte(`{"openapi":"3.0.1","info":{"title":"Demo","version":"1.0.0"},"paths":{}}`)
	swagger20YAML := []byte(`swagger: "2.0"
info:
  title: Demo
  version: 1.0.0
paths: {}
`)
	swagger20JSON := []byte(`{"swagger":"2.0","info":{"title":"Demo","version":"1.0.0"},"paths":{}}`)
	internalYAML := []byte(`
name: demo
base_url: https://api.example.com
resources:
  users:
    endpoints:
      list:
        method: GET
        path: /users
`)

	assert.True(t, IsOpenAPI(openAPIYAML))
	assert.True(t, IsOpenAPI(openAPIJSON))
	assert.True(t, IsOpenAPI(swagger20YAML))
	assert.True(t, IsOpenAPI(swagger20JSON))
	assert.False(t, IsOpenAPI(internalYAML))
}

func TestGenerateFromOpenAPICompiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		specFile string
	}{
		{name: "petstore", specFile: "petstore.yaml"},
		{name: "stytch", specFile: "stytch.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", tt.specFile))
			require.NoError(t, err)

			parsed, err := Parse(data)
			require.NoError(t, err)

			outputDir := filepath.Join(t.TempDir(), naming.CLI(parsed.Name))
			gen := generator.New(parsed, outputDir)
			require.NoError(t, gen.Generate())

			runGo(t, outputDir, "mod", "tidy")
			runGo(t, outputDir, "build", "./...")

			binaryPath := filepath.Join(outputDir, naming.CLI(parsed.Name))
			runGo(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(parsed.Name))

			info, err := os.Stat(binaryPath)
			require.NoError(t, err)
			require.NotZero(t, info.Size())
		})
	}
}

func runGo(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(dir, ".cache", "go-build"))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
}

func TestSanitizeResourceName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"users", "users"},
		{"user-accounts", "user_accounts"},
		{"../../../etc/passwd", "etc_passwd"},
		{"foo/bar", "foo_bar"},
		{"foo\\bar", "foo_bar"},
		{"..", ""},
		{".", ""},
		{"___", ""},
		{"normal_name", "normal_name"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeResourceName(toSnakeCase(tt.input))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPathSegmentsStripsGenericAPIPrefix(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		basePath  string
		wantFirst string
	}{
		{"strips api prefix", "/v1/api/users", "", "users"},
		{"strips apis prefix", "/v2/apis/teams", "", "teams"},
		{"strips rest prefix", "/rest/orders", "", "orders"},
		{"keeps non-generic prefix", "/v1/billing/invoices", "", "billing"},
		{"keeps api when no sub-segments", "/api", "", "api"},
		{"keeps api when followed by path param", "/api/{id}", "", "api"},
		{"keeps rest when followed by path param", "/rest/{job_id}/runs", "", "rest"},
		{"strips version then api", "/v1/api/networkentity", "", "networkentity"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			segments := pathSegmentsAfterBase(tt.path, tt.basePath)
			if len(segments) > 0 {
				assert.Equal(t, tt.wantFirst, segments[0])
			}
		})
	}
}

func TestOperationIDToName(t *testing.T) {
	tests := []struct {
		operationID  string
		resourceName string
		want         string
	}{
		{operationID: "api_user_v1_create", resourceName: "users", want: "create"},
		{operationID: "api_user_v1_delete_biometric_registration", resourceName: "users", want: "delete-biometric-registration"},
		{operationID: "api_user_v1_connected_apps", resourceName: "users", want: "connected-apps"},
		{operationID: "api_user_v1_get", resourceName: "users", want: "get"},
		{operationID: "api_user_v1_search", resourceName: "users", want: "search"},
		{operationID: "listPets", resourceName: "pet", want: "list"},
		{operationID: "createPet", resourceName: "pet", want: "create"},
		{operationID: "getPetById", resourceName: "pet", want: "get-by-id"},
		{operationID: "addPet", resourceName: "pet", want: "add"},
		{operationID: "deletePet", resourceName: "pet", want: "delete"},
		{operationID: "findPetsByStatus", resourceName: "pet", want: "find-by-status"},
		{operationID: "findPetsByTags", resourceName: "pet", want: "find-by-tags"},
		{operationID: "getInventory", resourceName: "store", want: "get-inventory"},
		{operationID: "placeOrder", resourceName: "store", want: "place-order"},
		{operationID: "createUser", resourceName: "user", want: "create"},
		{operationID: "loginUser", resourceName: "user", want: "login"},
		{operationID: "GetApplicationCommandPermissions", resourceName: "applications", want: "get-command-permissions"},
		{operationID: "", resourceName: "users", want: ""},
		{operationID: "list", resourceName: "users", want: "list"},
		// Cal.com-style: controller class names + embedded version dates
		{operationID: "BookingsController_2024-08-13_getBooking", resourceName: "bookings", want: "get"},
		{operationID: "BookingsController_2024-08-13_createBooking", resourceName: "bookings", want: "create"},
		{operationID: "EventTypesController_2024-06-14_getEventTypes", resourceName: "event-types", want: "get"},
		// Controller suffix without date
		{operationID: "OrganizationsController_getOrg", resourceName: "organizations", want: "get-org"},
		// No controller/version pattern — should be unchanged
		{operationID: "getBookingByUid", resourceName: "bookings", want: "get-by-uid"},
	}

	for _, tt := range tests {
		t.Run(tt.operationID+"_"+tt.resourceName, func(t *testing.T) {
			got := operationIDToName(tt.operationID, tt.resourceName, nil)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReclassifyPathParamModifiers(t *testing.T) {
	tests := []struct {
		name           string
		params         []spec.Param
		wantPositional []string // names that should stay positional
		wantFlags      []string // names that should become flags
	}{
		{
			name: "pagination params become flags",
			params: []spec.Param{
				{Name: "page", Type: "int", Positional: true},
				{Name: "pageSize", Type: "int", Positional: true},
			},
			wantPositional: nil,
			wantFlags:      []string{"page", "pageSize"},
		},
		{
			name: "entity ID stays positional",
			params: []spec.Param{
				{Name: "storeId", Type: "int", Positional: true},
			},
			wantPositional: []string{"storeId"},
			wantFlags:      nil,
		},
		{
			name: "mixed: storeId positional, page/pageSize flags",
			params: []spec.Param{
				{Name: "storeId", Type: "int", Positional: true},
				{Name: "page", Type: "int", Positional: true},
				{Name: "pageSize", Type: "int", Positional: true},
			},
			wantPositional: []string{"storeId"},
			wantFlags:      []string{"page", "pageSize"},
		},
		{
			name: "enum param becomes flag",
			params: []spec.Param{
				{Name: "serviceType", Type: "string", Positional: true, Enum: []string{"PICK", "DEL"}},
			},
			wantPositional: nil,
			wantFlags:      []string{"serviceType"},
		},
		{
			name: "date param becomes flag",
			params: []spec.Param{
				{Name: "storeId", Type: "int", Positional: true},
				{Name: "date", Type: "string", Positional: true, Format: "date"},
			},
			wantPositional: []string{"storeId"},
			wantFlags:      []string{"date"},
		},
		{
			name: "param with default becomes flag",
			params: []spec.Param{
				{Name: "version", Type: "string", Positional: true, Default: "v2"},
			},
			wantPositional: nil,
			wantFlags:      []string{"version"},
		},
		{
			name: "non-positional params unchanged",
			params: []spec.Param{
				{Name: "lang", Type: "string", Positional: false},
			},
			wantPositional: nil,
			wantFlags:      nil, // already a flag, not reclassified
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reclassifyPathParamModifiers(tt.params)

			var gotPositional, gotFlags []string
			for _, p := range tt.params {
				if p.Positional {
					gotPositional = append(gotPositional, p.Name)
				} else if p.PathParam {
					gotFlags = append(gotFlags, p.Name)
				}
			}
			assert.Equal(t, tt.wantPositional, gotPositional, "positional params")
			assert.Equal(t, tt.wantFlags, gotFlags, "reclassified flag params")
		})
	}
}

func TestReclassifyPathParamDefaults(t *testing.T) {
	params := []spec.Param{
		{Name: "page", Type: "int", Positional: true},
		{Name: "pageSize", Type: "int", Positional: true},
		{Name: "serviceType", Type: "string", Positional: true, Enum: []string{"PICK", "DEL"}},
	}
	reclassifyPathParamModifiers(params)

	assert.Equal(t, 1, params[0].Default, "page default should be 1")
	assert.Equal(t, 10, params[1].Default, "pageSize default should be 10")
	assert.Equal(t, "PICK", params[2].Default, "enum default should be first value")
}

func TestCleanSpecName(t *testing.T) {
	tests := []struct {
		title string
		want  string
	}{
		{title: "Swagger Petstore - OpenAPI 3.0", want: "petstore"},
		{title: "Discord HTTP API (Preview)", want: "discord"},
		{title: "Stytch API", want: "stytch"},
		{title: "GitHub REST API", want: "github"},
		{title: "", want: "api"},
		// Apostrophes in brand names should be stripped, not hyphenated
		{title: "Domino's Pizza API", want: "dominos-pizza"},
		{title: "McDonald's API", want: "mcdonalds"},
		{title: "Lowe's Home Improvement", want: "lowes-home-improvement"},
		// Unicode right single quotation mark
		{title: "Domino\u2019s Pizza API", want: "dominos-pizza"},
		// Multiple apostrophes
		{title: "Rock'n'Roll API", want: "rocknroll"},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			assert.Equal(t, tt.want, cleanSpecName(tt.title))
		})
	}
}

func TestHumanizeDescription(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "Connectedapps", want: "Connected apps"},
		{input: "DeleteBiometricRegistration", want: "Delete biometric registration"},
		{input: "Already normal text", want: "Already normal text"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, humanizeDescription(tt.input))
		})
	}
}

func TestDetectRequiredHeaders(t *testing.T) {
	t.Parallel()

	t.Run("versioned API with required header on all operations", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "versioned-api.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		require.Len(t, parsed.RequiredHeaders, 1)
		assert.Equal(t, "X-Api-Version", parsed.RequiredHeaders[0].Name)
		assert.Equal(t, "2024-01-01", parsed.RequiredHeaders[0].Value)
	})

	t.Run("petstore has no required headers", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "petstore.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.Empty(t, parsed.RequiredHeaders)
	})

	t.Run("stytch has no required headers (optional session headers)", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "stytch.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.Empty(t, parsed.RequiredHeaders)
	})

	t.Run("multi-version header tracks per-endpoint overrides", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "multi-version-header.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		// Global header should be the majority value (2024-08-13 appears on 3 of 6 ops)
		require.Len(t, parsed.RequiredHeaders, 1)
		assert.Equal(t, "cal-api-version", parsed.RequiredHeaders[0].Name)
		assert.Equal(t, "2024-08-13", parsed.RequiredHeaders[0].Value)

		// Event-types endpoints should have header overrides with 2024-06-14
		eventTypes := parsed.Resources["event-types"]
		require.NotNil(t, eventTypes)
		for eName, ep := range eventTypes.Endpoints {
			require.NotEmpty(t, ep.HeaderOverrides, "event-types endpoint %q should have header overrides", eName)
			assert.Equal(t, "cal-api-version", ep.HeaderOverrides[0].Name)
			assert.Equal(t, "2024-06-14", ep.HeaderOverrides[0].Value)
		}

		// Bookings endpoints should NOT have overrides (they match the global default)
		bookings := parsed.Resources["bookings"]
		require.NotNil(t, bookings)
		for eName, ep := range bookings.Endpoints {
			assert.Empty(t, ep.HeaderOverrides, "bookings endpoint %q should not have overrides (matches global)", eName)
		}
	})

	t.Run("authorization header excluded even if required on all ops", func(t *testing.T) {
		headers, perEndpoint := detectRequiredHeaders(nil, spec.AuthConfig{})
		assert.Empty(t, headers)
		assert.Empty(t, perEndpoint)
	})
}

func TestInferDescriptionAuth(t *testing.T) {
	t.Parallel()

	t.Run("bearer in description, no securitySchemes", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "bearer-in-description.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.Equal(t, "bearer_token", parsed.Auth.Type)
		assert.Equal(t, "Authorization", parsed.Auth.Header)
		assert.Equal(t, "header", parsed.Auth.In)
		assert.True(t, parsed.Auth.Inferred)
		assert.NotEmpty(t, parsed.Auth.EnvVars)
		assert.Contains(t, parsed.Auth.EnvVars[0], "_TOKEN")
	})

	t.Run("petstore has explicit auth, not inferred", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "petstore.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.False(t, parsed.Auth.Inferred)
		assert.NotEqual(t, "none", parsed.Auth.Type)
	})

	t.Run("stytch has explicit auth, not inferred", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "stytch.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.False(t, parsed.Auth.Inferred)
	})

	t.Run("no auth keywords in description stays none", func(t *testing.T) {
		doc := &openapi3.T{
			Info: &openapi3.Info{
				Description: "A simple API for managing widgets and gadgets.",
			},
		}
		result := inferDescriptionAuth(doc, "widgets", spec.AuthConfig{Type: "none"})
		assert.Equal(t, "none", result.Type)
		assert.False(t, result.Inferred)
	})

	t.Run("negation suppresses inference", func(t *testing.T) {
		result := inferDescriptionAuth(nil, "test", spec.AuthConfig{Type: "none"})
		assert.Equal(t, "none", result.Type)

		doc := &openapi3.T{
			Info: &openapi3.Info{
				Description: "This API does not require Bearer authentication",
			},
		}
		result = inferDescriptionAuth(doc, "test", spec.AuthConfig{Type: "none"})
		assert.Equal(t, "none", result.Type, "negated 'Bearer' should not trigger inference")
		assert.False(t, result.Inferred)
	})

	t.Run("api_key keyword produces api_key type", func(t *testing.T) {
		doc := &openapi3.T{
			Info: &openapi3.Info{
				Description: "Authenticate with your API key in the Authorization header",
			},
		}
		result := inferDescriptionAuth(doc, "example", spec.AuthConfig{Type: "none"})
		assert.Equal(t, "api_key", result.Type)
		assert.Equal(t, "EXAMPLE_API_KEY", result.EnvVars[0])
		assert.True(t, result.Inferred)
	})

	t.Run("scans past negated match to find positive mention", func(t *testing.T) {
		doc := &openapi3.T{
			Info: &openapi3.Info{
				Description: "Sandbox requests do not require a bearer token, but production requests use a bearer token for authentication.",
			},
		}
		result := inferDescriptionAuth(doc, "example", spec.AuthConfig{Type: "none"})
		assert.Equal(t, "bearer_token", result.Type, "should find the second non-negated 'bearer' mention")
		assert.True(t, result.Inferred)
	})

	t.Run("Notion bearer token not falsely negated", func(t *testing.T) {
		doc := &openapi3.T{
			Info: &openapi3.Info{
				Description: "Use your Notion bearer token to authenticate",
			},
		}
		result := inferDescriptionAuth(doc, "notion", spec.AuthConfig{Type: "none"})
		assert.Equal(t, "bearer_token", result.Type, "'Notion' contains 'no' but should not trigger negation")
		assert.True(t, result.Inferred)
	})

	t.Run("custom header X-Api-Key extracted from description", func(t *testing.T) {
		doc := &openapi3.T{
			Info: &openapi3.Info{
				Description: "Send your API key in the X-Api-Key header",
			},
		}
		result := inferDescriptionAuth(doc, "example", spec.AuthConfig{Type: "none"})
		assert.Equal(t, "api_key", result.Type)
		assert.Equal(t, "X-Api-Key", result.Header, "should extract X-Api-Key, not default to Authorization")
		assert.True(t, result.Inferred)
	})

	t.Run("nil doc returns fallback", func(t *testing.T) {
		fb := spec.AuthConfig{Type: "none"}
		assert.Equal(t, fb, inferDescriptionAuth(nil, "test", fb))
	})
}

func TestInferAuthHeaderParam(t *testing.T) {
	t.Parallel()

	t.Run("detects auth from required Authorization header params", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "auth-header-param.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.Equal(t, "bearer_token", parsed.Auth.Type)
		assert.Equal(t, "Authorization", parsed.Auth.Header)
		assert.Equal(t, "header", parsed.Auth.In)
		assert.True(t, parsed.Auth.Inferred)
		assert.NotEmpty(t, parsed.Auth.EnvVars, "EnvVars must be populated for verify")
	})

	t.Run("does not trigger when Authorization params below threshold", func(t *testing.T) {
		// 1 out of 5 operations = 20% < 30% threshold
		doc := &openapi3.T{
			Info:  &openapi3.Info{Title: "test", Description: "no auth keywords"},
			Paths: &openapi3.Paths{},
		}
		for i, path := range []string{"/a", "/b", "/c", "/d", "/e"} {
			pathItem := &openapi3.PathItem{
				Get: &openapi3.Operation{Responses: openapi3.NewResponses()},
			}
			if i == 0 { // only first has Authorization param
				pathItem.Get.Parameters = openapi3.Parameters{
					&openapi3.ParameterRef{Value: &openapi3.Parameter{
						Name: "Authorization", In: "header", Required: true,
					}},
				}
			}
			doc.Paths.Set(path, pathItem)
		}
		result := mapAuth(doc, "test-api")
		assert.Equal(t, "none", result.Type)
	})

	t.Run("optional Authorization param not counted", func(t *testing.T) {
		doc := &openapi3.T{
			Info:  &openapi3.Info{Title: "test", Description: "no auth keywords"},
			Paths: &openapi3.Paths{},
		}
		for _, path := range []string{"/a", "/b", "/c"} {
			pathItem := &openapi3.PathItem{
				Get: &openapi3.Operation{
					Responses: openapi3.NewResponses(),
					Parameters: openapi3.Parameters{
						&openapi3.ParameterRef{Value: &openapi3.Parameter{
							Name: "Authorization", In: "header", Required: false,
						}},
					},
				},
			}
			doc.Paths.Set(path, pathItem)
		}
		result := mapAuth(doc, "test-api")
		assert.Equal(t, "none", result.Type)
	})

	t.Run("explicit securitySchemes still wins over header param", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "gmail.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.Equal(t, "bearer_token", parsed.Auth.Type)
		assert.False(t, parsed.Auth.Inferred, "explicit auth should not be marked as inferred")
	})
}

func TestAuthTierPrecedence(t *testing.T) {
	t.Parallel()

	t.Run("explicit securitySchemes wins over description keywords", func(t *testing.T) {
		// Gmail has both securitySchemes AND description that could mention auth
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "gmail.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.Equal(t, "bearer_token", parsed.Auth.Type)
		assert.False(t, parsed.Auth.Inferred, "explicit auth from securitySchemes should not be marked as inferred")
	})

	t.Run("query-param auth tier 2 wins over description tier 3", func(t *testing.T) {
		// Build a minimal spec with auth-like query params on >30% of ops
		// AND bearer keyword in description. Tier 2 should win.
		doc := &openapi3.T{
			Info: &openapi3.Info{
				Description: "This API uses Bearer token authentication.",
			},
			Paths: &openapi3.Paths{},
		}
		// Add 5 operations, 3 with api_key query param (60% > 30% threshold)
		for i, path := range []string{"/a", "/b", "/c", "/d", "/e"} {
			pathItem := &openapi3.PathItem{
				Get: &openapi3.Operation{
					Responses: openapi3.NewResponses(),
				},
			}
			if i < 3 { // first 3 have api_key param
				pathItem.Get.Parameters = openapi3.Parameters{
					&openapi3.ParameterRef{
						Value: &openapi3.Parameter{
							Name:     "api_key",
							In:       "query",
							Required: false,
						},
					},
				}
			}
			doc.Paths.Set(path, pathItem)
		}

		// Run mapAuth directly — it should pick up query-param auth (tier 2)
		result := mapAuth(doc, "test-api")
		assert.Equal(t, "api_key", result.Type)
		assert.Equal(t, "query", result.In, "tier 2 query-param auth should win over tier 3 description")
		assert.False(t, result.Inferred, "query-param auth is not 'inferred from description'")
	})
}

func TestNoAuthDetection(t *testing.T) {
	t.Parallel()

	t.Run("mixed-auth fixture: per-operation security overrides", func(t *testing.T) {
		t.Parallel()
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "mixed-auth.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		// stores.listStores has security: [] — should be NoAuth
		stores := parsed.Resources["stores"]
		require.NotNil(t, stores)
		for _, e := range stores.Endpoints {
			if e.Path == "/stores" && e.Method == "GET" {
				assert.True(t, e.NoAuth, "stores GET with security:[] should be NoAuth")
			}
		}

		// menus.getMenu has security: [{}] — should be NoAuth
		menus := parsed.Resources["menus"]
		require.NotNil(t, menus)
		for _, e := range menus.Endpoints {
			if e.Path == "/menus" && e.Method == "GET" {
				assert.True(t, e.NoAuth, "menus GET with security:[{}] should be NoAuth")
			}
		}

		// orders.listOrders inherits global ApiKeyAuth — should NOT be NoAuth
		orders := parsed.Resources["orders"]
		require.NotNil(t, orders)
		for _, e := range orders.Endpoints {
			if e.Path == "/orders" && e.Method == "GET" {
				assert.False(t, e.NoAuth, "orders GET inheriting global auth should not be NoAuth")
			}
			if e.Path == "/orders" && e.Method == "POST" {
				assert.False(t, e.NoAuth, "orders POST with explicit ApiKeyAuth should not be NoAuth")
			}
		}

		// account.getAccount inherits global ApiKeyAuth — should NOT be NoAuth
		account := parsed.Resources["account"]
		require.NotNil(t, account)
		for _, e := range account.Endpoints {
			if e.Path == "/account" && e.Method == "GET" {
				assert.False(t, e.NoAuth, "account GET inheriting global auth should not be NoAuth")
			}
		}
	})

	t.Run("spec with no auth at all marks all endpoints NoAuth", func(t *testing.T) {
		t.Parallel()
		// Build a spec with no securitySchemes, no global security
		doc := &openapi3.T{
			OpenAPI: "3.0.3",
			Info:    &openapi3.Info{Title: "No Auth API", Version: "1.0.0"},
			Paths:   &openapi3.Paths{},
			Servers: openapi3.Servers{{URL: "https://api.example.com"}},
		}
		doc.Paths.Set("/items", &openapi3.PathItem{
			Get: &openapi3.Operation{
				Summary:   "List items",
				Responses: openapi3.NewResponses(),
			},
		})
		doc.Paths.Set("/items/{id}", &openapi3.PathItem{
			Get: &openapi3.Operation{
				Summary:   "Get item",
				Responses: openapi3.NewResponses(),
				Parameters: openapi3.Parameters{
					&openapi3.ParameterRef{Value: &openapi3.Parameter{
						Name: "id", In: "path", Required: true,
						Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
					}},
				},
			},
		})

		parsed, err := Parse(mustMarshalJSON(t, doc))
		require.NoError(t, err)

		assert.Equal(t, "none", parsed.Auth.Type)
		// All endpoints should be NoAuth via post-parse sweep
		for _, r := range parsed.Resources {
			for eName, e := range r.Endpoints {
				assert.True(t, e.NoAuth, "endpoint %s should be NoAuth in no-auth spec", eName)
			}
		}
	})

	t.Run("global security empty array marks inherited endpoints NoAuth", func(t *testing.T) {
		t.Parallel()
		// Use raw YAML to preserve the security: [] distinction
		yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Global Empty Security
  version: "1.0.0"
servers:
  - url: https://api.example.com
security: []
components:
  securitySchemes:
    ApiKey:
      type: apiKey
      name: X-Api-Key
      in: header
paths:
  /public:
    get:
      summary: Public endpoint
      responses:
        "200":
          description: OK
  /private:
    get:
      summary: Private endpoint
      security:
        - ApiKey: []
      responses:
        "200":
          description: OK
`)
		parsed, err := Parse(yamlSpec)
		require.NoError(t, err)

		// /public inherits global security:[] — should be NoAuth
		foundPublic := false
		foundPrivate := false
		for _, r := range parsed.Resources {
			for _, e := range r.Endpoints {
				if e.Path == "/public" {
					assert.True(t, e.NoAuth, "/public should be NoAuth from global security:[]")
					foundPublic = true
				}
				if e.Path == "/private" {
					assert.False(t, e.NoAuth, "/private has explicit ApiKey requirement")
					foundPrivate = true
				}
			}
		}
		assert.True(t, foundPublic, "should have found /public endpoint")
		assert.True(t, foundPrivate, "should have found /private endpoint")
	})

	t.Run("petstore still parses without regression", func(t *testing.T) {
		t.Parallel()
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "petstore.yaml"))
		require.NoError(t, err)

		parsed, err := Parse(data)
		require.NoError(t, err)

		assert.Equal(t, "petstore", parsed.Name)
		assert.True(t, len(parsed.Resources) > 0, "petstore should have resources")
	})
}

func TestPathPriorityScore(t *testing.T) {
	t.Parallel()

	t.Run("admin paths score lower than user paths", func(t *testing.T) {
		assert.Greater(t, pathPriorityScore("/users"), pathPriorityScore("/admin/users"))
		assert.Greater(t, pathPriorityScore("/channels"), pathPriorityScore("/admin.conversations"))
		assert.Greater(t, pathPriorityScore("/messages"), pathPriorityScore("/internal/metrics"))
		assert.Greater(t, pathPriorityScore("/items"), pathPriorityScore("/system/health"))
		assert.Greater(t, pathPriorityScore("/teams"), pathPriorityScore("/management/roles"))
	})

	t.Run("shallow paths score higher than deep paths", func(t *testing.T) {
		assert.Greater(t, pathPriorityScore("/users"), pathPriorityScore("/users/{id}/posts/{postId}/comments"))
	})

	t.Run("short paths get bonus", func(t *testing.T) {
		short := pathPriorityScore("/users")
		long := pathPriorityScore("/a/b/c/d")
		assert.Greater(t, short, long)
	})
}

func TestPathPriorityScoreSortOrder(t *testing.T) {
	t.Parallel()

	// Build 600 paths: 100 admin.* paths and 500 user-facing paths.
	var paths []string
	for i := range 100 {
		paths = append(paths, fmt.Sprintf("/admin.resource%d/action", i))
	}
	for i := range 500 {
		paths = append(paths, fmt.Sprintf("/resource%d", i))
	}

	// Sort by priority score descending, alphabetical tiebreaker.
	sort.SliceStable(paths, func(i, j int) bool {
		si, sj := pathPriorityScore(paths[i]), pathPriorityScore(paths[j])
		if si != sj {
			return si > sj
		}
		return paths[i] < paths[j]
	})

	// With a 500-path cap, all admin paths should be in the tail (indices 500+).
	const maxResources = 500
	kept := paths[:maxResources]
	dropped := paths[maxResources:]

	for _, p := range dropped {
		assert.Contains(t, p, "admin", "expected only admin paths to be dropped, but got: %s", p)
	}
	for _, p := range kept {
		assert.NotContains(t, p, "admin", "expected no admin paths in kept set, but got: %s", p)
	}
}

func mustMarshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}
