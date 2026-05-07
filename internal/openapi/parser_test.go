package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
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
	// REST specs must leave the GraphQL-only fields unset; the generated
	// graphql_client template is gated on isGraphQLSpec so a stray value here
	// would silently leak into REST clients that never call POST /graphql.
	assert.Empty(t, parsed.GraphQLEndpointPath)
	assert.Empty(t, parsed.EndpointTemplateVars)
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

func TestParsePreservesResponseDiscriminatorAndEnumFields(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Mixed Network
  version: 1.0.0
paths:
  /network-entities:
    get:
      operationId: listNetworkEntities
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: array
                    items:
                      $ref: "#/components/schemas/NetworkEntity"
components:
  schemas:
    NetworkEntity:
      type: object
      discriminator:
        propertyName: type
        mapping:
          workspace: "#/components/schemas/Workspace"
          collection: "#/components/schemas/Collection"
      properties:
        type:
          type: string
          enum: [workspace, collection]
        id:
          type: string
    Workspace:
      type: object
      properties:
        id:
          type: string
    Collection:
      type: object
      properties:
        id:
          type: string
`))
	require.NoError(t, err)

	endpoint := parsed.Resources["network-entities"].Endpoints["list"]
	require.NotNil(t, endpoint.Response.Discriminator)
	assert.Equal(t, "type", endpoint.Response.Discriminator.Field)
	assert.Equal(t, map[string]string{
		"collection": "Collection",
		"workspace":  "Workspace",
	}, endpoint.Response.Discriminator.Mapping)

	var typeField spec.TypeField
	for _, field := range parsed.Types["NetworkEntity"].Fields {
		if field.Name == "type" {
			typeField = field
			break
		}
	}
	assert.Equal(t, []string{"workspace", "collection"}, typeField.Enum)
}

func TestParseMapsAllOfRequestBodyFields(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Banking API
  version: 1.0.0
paths:
  /account/{accountId}/transactions:
    post:
      operationId: createTransaction
      parameters:
        - name: accountId
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [amount, paymentMethod, recipientId, purpose]
              properties:
                amount:
                  allOf:
                    - $ref: "#/components/schemas/PositiveDollar"
                  description: Amount of USD you want to send.
                paymentMethod:
                  allOf:
                    - $ref: "#/components/schemas/PaymentMethod"
                recipientId:
                  allOf:
                    - $ref: "#/components/schemas/RecipientId"
                purpose:
                  allOf:
                    - $ref: "#/components/schemas/PaymentPurpose"
                    - $ref: "#/components/schemas/PurposeMetadata"
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
components:
  schemas:
    PositiveDollar:
      type: number
      format: double
      description: A positive dollar amount with at least 1 cent.
    PaymentMethod:
      type: string
      enum: [ach, check, domesticWire]
    RecipientId:
      type: string
      format: uuid
    PaymentPurpose:
      type: object
      properties:
        simple:
          type: string
    PurposeMetadata:
      type: object
      required: [memo]
      properties:
        memo:
          type: string
`))
	require.NoError(t, err)

	endpoint := findParsedEndpointByPath(t, parsed, "POST", "/account/{accountId}/transactions")
	byName := map[string]spec.Param{}
	for _, param := range endpoint.Body {
		byName[param.Name] = param
	}

	assert.Equal(t, "float", byName["amount"].Type)
	assert.Equal(t, "double", byName["amount"].Format)
	assert.Equal(t, "string", byName["paymentMethod"].Type)
	assert.Equal(t, []string{"ach", "check", "domesticWire"}, byName["paymentMethod"].Enum)
	assert.Equal(t, "string", byName["recipientId"].Type)
	assert.Equal(t, "uuid", byName["recipientId"].Format)
	assert.Equal(t, "object", byName["purpose"].Type)
	assert.Equal(t, []spec.Param{
		{Name: "memo", Type: "string", Required: true, Description: "Memo"},
		{Name: "simple", Type: "string", Description: "Simple"},
	}, byName["purpose"].Fields)
}

func TestParseRecursiveRequestBodyFieldsStopsAtCycle(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Recursive API
  version: 1.0.0
paths:
  /nodes:
    post:
      operationId: createNode
      requestBody:
        content:
          application/json:
            schema:
              type: object
              required: [node]
              properties:
                node:
                  $ref: "#/components/schemas/Node"
      responses:
        "200":
          description: ok
components:
  schemas:
    Node:
      type: object
      required: [name]
      properties:
        name:
          type: string
        child:
          $ref: "#/components/schemas/Node"
`))
	require.NoError(t, err)

	endpoint := findParsedEndpointByPath(t, parsed, "POST", "/nodes")
	require.Len(t, endpoint.Body, 1)
	require.Equal(t, "node", endpoint.Body[0].Name)
	assert.Equal(t, "object", endpoint.Body[0].Type)

	fieldsByName := map[string]spec.Param{}
	for _, field := range endpoint.Body[0].Fields {
		fieldsByName[field.Name] = field
	}
	assert.Equal(t, "string", fieldsByName["name"].Type)
	assert.Equal(t, "object", fieldsByName["child"].Type)
	assert.Empty(t, fieldsByName["child"].Fields)
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
	// gmail uses authorization_code flow; OAuth2Grant stays empty so the
	// EffectiveOAuth2Grant() default of "authorization_code" applies.
	assert.Equal(t, "", parsed.Auth.OAuth2Grant)
}

func TestParseOAuth2ClientCredentialsFlow(t *testing.T) {
	t.Parallel()

	specBytes := []byte(`openapi: "3.0.3"
info:
  title: Auth0Mgmt
  version: "1.0"
servers:
  - url: https://example.auth0.com
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://example.auth0.com/oauth/token
          scopes:
            read:users: Read user profiles
            write:users: Manage users
paths:
  /api/v2/users:
    get:
      operationId: list users
      security:
        - OAuth2: []
      responses: {"200": {description: ok}}
`)

	parsed, err := Parse(specBytes)
	require.NoError(t, err)

	assert.Equal(t, "bearer_token", parsed.Auth.Type, "parser keeps bearer_token shape; grant lives on OAuth2Grant")
	assert.Equal(t, "client_credentials", parsed.Auth.OAuth2Grant)
	assert.Equal(t, "https://example.auth0.com/oauth/token", parsed.Auth.TokenURL)
	assert.Empty(t, parsed.Auth.AuthorizationURL, "client_credentials flow has no user redirect")
	assert.Equal(t, []string{"read:users", "write:users"}, parsed.Auth.Scopes)
}

func TestParseOAuth2BothFlowsPrefersClientCredentials(t *testing.T) {
	t.Parallel()

	// When a single OAuth2 scheme declares both authorizationCode and
	// clientCredentials flows, the parser prefers clientCredentials —
	// server-to-server is the more common shape for printed CLIs (which
	// run in CI/scripts, not interactive browsers). Spec authors override
	// post-import by setting OAuth2Grant explicitly.
	specBytes := []byte(`openapi: "3.0.3"
info:
  title: Hybrid
  version: "1.0"
servers:
  - url: https://example.com
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://example.com/oauth/authorize
          tokenUrl: https://example.com/oauth/token-ac
          scopes:
            user: User access
        clientCredentials:
          tokenUrl: https://example.com/oauth/token-cc
          scopes:
            admin: Admin access
paths:
  /v1/things:
    get:
      operationId: list things
      security:
        - OAuth2: []
      responses: {"200": {description: ok}}
`)

	parsed, err := Parse(specBytes)
	require.NoError(t, err)

	assert.Equal(t, "client_credentials", parsed.Auth.OAuth2Grant)
	assert.Equal(t, "https://example.com/oauth/token-cc", parsed.Auth.TokenURL,
		"clientCredentials tokenUrl wins, not authorizationCode's")
	assert.Empty(t, parsed.Auth.AuthorizationURL, "no user redirect for the cc flow")
	assert.Equal(t, []string{"admin"}, parsed.Auth.Scopes,
		"clientCredentials scopes win, not authorizationCode's")
}

func TestParseOAuth2ClientCredentialsMissingTokenURLSkipsBranch(t *testing.T) {
	t.Parallel()

	// Malformed spec: clientCredentials block exists but has no tokenUrl.
	// Parser should skip the cc branch and fall through to the next flow
	// (or leave fields empty if no other flows exist), not crash.
	specBytes := []byte(`openapi: "3.0.3"
info:
  title: Malformed
  version: "1.0"
servers:
  - url: https://example.com
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: ""
          scopes: {}
        authorizationCode:
          authorizationUrl: https://example.com/auth
          tokenUrl: https://example.com/token
          scopes: {}
paths:
  /v1/foo:
    get:
      operationId: foo
      security:
        - OAuth2: []
      responses: {"200": {description: ok}}
`)

	parsed, err := Parse(specBytes)
	require.NoError(t, err)

	// Falls through to authorizationCode since cc had no tokenUrl.
	assert.Equal(t, "", parsed.Auth.OAuth2Grant)
	assert.Equal(t, "https://example.com/token", parsed.Auth.TokenURL)
	assert.Equal(t, "https://example.com/auth", parsed.Auth.AuthorizationURL)
}

func TestBearerSchemeNameCanSpecializeEnvVar(t *testing.T) {
	t.Parallel()

	spec := []byte(`openapi: "3.0.3"
info:
  title: Sentry
  version: "1.0"
servers:
  - url: https://example.com
components:
  securitySchemes:
    auth_token:
      type: http
      scheme: bearer
paths:
  /api/0/organizations/:
    get:
      operationId: List Your Organizations
      security:
        - auth_token: []
      responses:
        "200":
          description: ok
`)
	parsed, err := Parse(spec)
	require.NoError(t, err)

	assert.Equal(t, "bearer_token", parsed.Auth.Type)
	assert.Equal(t, []string{"SENTRY_AUTH_TOKEN"}, parsed.Auth.EnvVars)
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

func TestParseReadsXDisplayName(t *testing.T) {
	spec := []byte(`
openapi: "3.0.0"
info:
  title: Cal.com API v2
  x-display-name: "Cal.com"
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
`)
	parsed, err := Parse(spec)
	require.NoError(t, err)
	assert.Equal(t, "Cal.com", parsed.DisplayName)
	assert.False(t, parsed.DisplayNameDerivedFromTitle)
}

func TestParseTrimsWhitespaceFromXDisplayName(t *testing.T) {
	spec := []byte(`
openapi: "3.0.0"
info:
  title: Test API
  x-display-name: "  Brand Name  "
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
`)
	parsed, err := Parse(spec)
	require.NoError(t, err)
	assert.Equal(t, "Brand Name", parsed.DisplayName)
	assert.False(t, parsed.DisplayNameDerivedFromTitle)
}

// TestParseDerivesDisplayNameFromTitle locks the dual contract when no
// x-display-name extension is set: slug is ASCII-folded for filesystem and
// shell safety, display_name keeps Unicode for the human-facing surfaces
// (manifest.json, .printing-press.json, MCP server identity).
func TestParseDerivesDisplayNameFromTitle(t *testing.T) {
	cases := []struct {
		name        string
		title       string
		wantSlug    string
		wantDisplay string
	}{
		{name: "ascii", title: "Test API", wantSlug: "test", wantDisplay: "Test"},
		{name: "precomposed_accent", title: "Café Bistro API", wantSlug: "cafe-bistro", wantDisplay: "Café Bistro"},
		{name: "fused_diacritics", title: "Strüdel Service API", wantSlug: "strudel-service", wantDisplay: "Strüdel Service"},
		{name: "non_latin_script", title: "東京 API", wantSlug: "dong-jing", wantDisplay: "東京"},
		{name: "single_token_accent", title: "PokéAPI", wantSlug: "pokeapi", wantDisplay: "Pokéapi"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := fmt.Appendf(nil, `
openapi: "3.0.0"
info:
  title: %s
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
`, tc.title)
			parsed, err := Parse(spec)
			require.NoError(t, err)
			assert.Equal(t, tc.wantSlug, parsed.Name)
			assert.Equal(t, tc.wantDisplay, parsed.DisplayName)
			assert.True(t, parsed.DisplayNameDerivedFromTitle)
		})
	}
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
	if testing.Short() {
		t.Skip("OpenAPI generated CLI compile coverage runs in the generated-test CI lane")
	}

	tests := []struct {
		name     string
		specFile string
	}{
		{name: "petstore", specFile: "petstore.yaml"},
		{name: "stytch", specFile: "stytch.yaml"},
	}

	for _, tt := range tests {
		tt := tt //nolint:modernize // keep the parallel subtest capture explicit
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
		})
	}
}

func runGo(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
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
		{"strips api then version", "/api/v2/pokemon", "", "pokemon"},
		{"strips version then api then version", "/v2/api/v1/pokemon", "", "pokemon"},
		{"strips api then numeric version", "/api/0/organizations", "", "organizations"},
		{"strips beta version", "/v1beta2/{parent}/repositories", "", "{parent}"},
		{"strips alpha version", "/v1alpha1/{parent}/services", "", "{parent}"},
		{"strips p beta version", "/v1p1beta1/{parent}/sessions", "", "{parent}"},
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
		{operationID: "run.projects.locations.services.revisions.delete", resourceName: "revisions", want: "delete"},
		{operationID: "run.projects.locations.services.getIamPolicy", resourceName: "services", want: "get-iam-policy"},
	}

	for _, tt := range tests {
		t.Run(tt.operationID+"_"+tt.resourceName, func(t *testing.T) {
			got := operationIDToName(tt.operationID, tt.resourceName, nil)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResourceAndSubFromGoogleOperationID(t *testing.T) {
	tests := []struct {
		name         string
		operationID  string
		wantPrimary  string
		wantSub      string
		wantName     string
		nameResource string
	}{
		{
			name:         "project location resource",
			operationID:  "run.projects.locations.services.create",
			wantPrimary:  "services",
			nameResource: "services",
			wantName:     "create",
		},
		{
			name:         "nested subresource keeps owning parent",
			operationID:  "run.projects.locations.services.revisions.delete",
			wantPrimary:  "services",
			wantSub:      "revisions",
			nameResource: "revisions",
			wantName:     "delete",
		},
		{
			name:         "deep chain uses immediate parent",
			operationID:  "run.projects.locations.jobs.executions.tasks.list",
			wantPrimary:  "executions",
			wantSub:      "tasks",
			nameResource: "tasks",
			wantName:     "list",
		},
		{
			name:         "organization location scope",
			operationID:  "example.organizations.locations.widgets.get",
			wantPrimary:  "widgets",
			nameResource: "widgets",
			wantName:     "get",
		},
		{
			name:         "billing account scope",
			operationID:  "example.billingAccounts.invoices.list",
			wantPrimary:  "invoices",
			nameResource: "invoices",
			wantName:     "list",
		},
		{
			name:        "non Google scope ignored",
			operationID: "gmail.users.messages.list",
		},
		{
			name:         "unscoped Google Discovery operation",
			operationID:  "bigquery.tables.getIamPolicy",
			wantPrimary:  "tables",
			nameResource: "tables",
			wantName:     "get-iam-policy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPrimary, gotSub := resourceAndSubFromGoogleOperationID(tt.operationID, false)
			if tt.name == "unscoped Google Discovery operation" {
				gotPrimary, gotSub = resourceAndSubFromGoogleOperationID(tt.operationID, true)
			}
			assert.Equal(t, tt.wantPrimary, gotPrimary)
			assert.Equal(t, tt.wantSub, gotSub)
			assert.Equal(t, tt.wantName, googleOperationIDEndpointName(tt.operationID, tt.nameResource))
		})
	}
}

func TestParseGoogleDiscoveryResourceFallback(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "google-discovery-run.yaml"))
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)

	assert.NotContains(t, parsed.Resources, "name-run")
	assert.NotContains(t, parsed.Resources, "name-wait")
	assert.NotContains(t, parsed.Resources, "resource-get-iam-policy")
	assert.NotContains(t, parsed.Resources, "resource-set-iam-policy")
	assert.NotContains(t, parsed.Resources, "resource-test-iam-permissions")

	services := parsed.Resources["services"]
	require.Contains(t, services.Endpoints, "create")
	require.Contains(t, services.Endpoints, "patch")
	require.Contains(t, services.Endpoints, "list")
	require.Contains(t, services.Endpoints, "get-iam-policy")
	require.Contains(t, services.Endpoints, "set-iam-policy")
	require.Contains(t, services.Endpoints, "test-iam-permissions")
	require.Contains(t, services.SubResources, "revisions")
	assert.Contains(t, services.SubResources["revisions"].Endpoints, "delete")
	assert.Contains(t, services.SubResources["revisions"].Endpoints, "get")
	assert.Contains(t, services.SubResources["revisions"].Endpoints, "list")

	assert.NotContains(t, parsed.Resources, "jobs")
	jobs := parsed.Resources["google-cloud-run-jobs"]
	require.Contains(t, jobs.Endpoints, "create")
	require.Contains(t, jobs.Endpoints, "list")
	require.Contains(t, jobs.Endpoints, "run")
	require.Contains(t, jobs.SubResources, "executions")
	assert.Contains(t, jobs.SubResources["executions"].Endpoints, "list")

	executions := parsed.Resources["executions"]
	require.Contains(t, executions.SubResources, "tasks")
	assert.Contains(t, executions.SubResources["tasks"].Endpoints, "list")

	operations := parsed.Resources["operations"]
	assert.Contains(t, operations.Endpoints, "list")
	assert.Contains(t, operations.Endpoints, "wait")

	locations := parsed.Resources["locations"]
	assert.Contains(t, locations.Endpoints, "list")
}

func TestParseGoogleDiscoveryUnscopedOperationIDFallbackRequiresGoogleOrigin(t *testing.T) {
	t.Parallel()

	base := `openapi: 3.0.3
info:
  title: BigQuery API
  version: v2
%s
paths:
  /{resource}:getIamPolicy:
    get:
      operationId: bigquery.tables.getIamPolicy
      parameters:
        - name: resource
          in: path
          required: true
          schema: {type: string}
      responses:
        "200": {description: ok}
`

	parsed, err := Parse(fmt.Appendf(nil, base, "  x-providerName: googleapis.com\n"))
	require.NoError(t, err)
	require.Contains(t, parsed.Resources, "tables")
	assert.Contains(t, parsed.Resources["tables"].Endpoints, "get-iam-policy")
	assert.NotContains(t, parsed.Resources, "resource-get-iam-policy")

	parsed, err = Parse(fmt.Appendf(nil, base, ""))
	require.NoError(t, err)
	assert.NotContains(t, parsed.Resources, "tables")
	assert.Contains(t, parsed.Resources, "resource-get-iam-policy")
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

func TestParsePreservesDefaultedPathParamsDuringGlobalFilter(t *testing.T) {
	data := []byte(`
openapi: 3.0.0
info:
  title: GraphQL Routing API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /graphql/{pathQueryId}/Followers:
    get:
      operationId: getFollowers
      parameters:
        - in: path
          name: pathQueryId
          required: true
          schema:
            type: string
            default: followers123
        - in: query
          name: variables
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
  /graphql/{pathQueryId}/Following:
    get:
      operationId: getFollowing
      parameters:
        - in: path
          name: pathQueryId
          required: true
          schema:
            type: string
            default: following123
        - in: query
          name: variables
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
  /graphql/{pathQueryId}/Likes:
    get:
      operationId: getLikes
      parameters:
        - in: path
          name: pathQueryId
          required: true
          schema:
            type: string
            default: likes123
        - in: query
          name: variables
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err)

	for _, path := range []string{
		"/graphql/{pathQueryId}/Followers",
		"/graphql/{pathQueryId}/Following",
		"/graphql/{pathQueryId}/Likes",
	} {
		endpoint := findEndpoint(t, parsed, path)
		var routingParam *spec.Param
		for i := range endpoint.Params {
			if endpoint.Params[i].Name == "pathQueryId" {
				routingParam = &endpoint.Params[i]
				break
			}
		}
		if assert.NotNil(t, routingParam, "pathQueryId should survive global-param filtering") {
			assert.True(t, routingParam.PathParam, "defaulted path param should remain a URL substitution flag")
			assert.False(t, routingParam.Positional, "defaulted path param should stay flag-shaped")
			assert.NotNil(t, routingParam.Default, "operation-specific query id default should be preserved")
		}
	}
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
		// Precomposed accents:
		{title: "Pok\u00e9mon API", want: "pokemon"},
		{title: "Caf\u00e9 Reservations", want: "cafe-reservations"},
		{title: "Na\u00efve Bayes API", want: "naive-bayes"},
		// Fused-diacritic Latin:
		{title: "Gro\u00dfhandel API", want: "grosshandel"},
		{title: "Encyclop\u00e6dia API", want: "encyclopaedia"},
		{title: "\u00d8rsted Energy", want: "orsted-energy"},
		{title: "\u0141\u00f3d\u017a Transit", want: "lodz-transit"},
		{title: "\u00deingvellir Tours", want: "thingvellir-tours"},
		// Non-Latin scripts:
		{title: "\u6771\u4eac API", want: "dong-jing"},
		{title: "\u0440\u0443\u0441\u0441\u043a\u0438\u0439 API", want: "russkii"},
		{title: "\u0394elta API", want: "delta"},
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

func TestInferredAuthEnvVarsAreASCIISafe(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: PokéAPI
  version: "1.0.0"
  description: Authenticate with your API key in the Authorization header.
servers:
  - url: https://api.example.com
paths:
  /pokemon:
    get:
      summary: List pokemon
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	require.NotEmpty(t, parsed.Auth.EnvVars)
	assert.Equal(t, "POKEAPI_API_KEY", parsed.Auth.EnvVars[0])
}

func TestGenericAPIKeySchemeNamesUseAPIKeyEnvVar(t *testing.T) {
	t.Parallel()

	tests := []string{
		"ApiKeyAuth",
		"APIKey",
		"ApiKeyAuth_v2",
		"auth",
	}

	for _, schemeName := range tests {
		t.Run(schemeName, func(t *testing.T) {
			t.Parallel()

			yamlSpec := fmt.Appendf(nil, `openapi: "3.0.3"
info:
  title: FlightGoat
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    %s:
      type: apiKey
      in: header
      name: x-apikey
paths:
  /flights:
    get:
      responses:
        "200":
          description: OK
`, schemeName)
			parsed, err := Parse(yamlSpec)
			require.NoError(t, err)

			assert.Equal(t, "api_key", parsed.Auth.Type)
			assert.Equal(t, []string{"FLIGHTGOAT_API_KEY"}, parsed.Auth.EnvVars)
		})
	}
}

func TestSpeakeasyAuthExampleOverridesDerivedEnvVar(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Dub API
  version: "1.0.0"
servers:
  - url: https://api.dub.co
components:
  securitySchemes:
    token:
      type: http
      scheme: bearer
      x-speakeasy-example: DUB_API_KEY
paths:
  /links:
    get:
      security:
        - token: []
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, "bearer_token", parsed.Auth.Type)
	assert.Equal(t, []string{"DUB_API_KEY"}, parsed.Auth.EnvVars)
}

func TestSpeakeasyAuthExampleRemapsInferredFormatPlaceholder(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Discord
  version: "1.0.0"
servers:
  - url: https://discord.com/api
components:
  securitySchemes:
    BotToken:
      type: apiKey
      in: header
      name: Authorization
      x-speakeasy-example: DISCORD_TOKEN
paths:
  /users/@me:
    get:
      security:
        - BotToken: []
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, []string{"DISCORD_TOKEN"}, parsed.Auth.EnvVars)
	assert.Equal(t, "Bot {token}", parsed.Auth.Format)
}

func TestSpeakeasyAuthExampleDoesNotOverrideExplicitEnvVars(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: OAuth Client
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    ClientCredentials:
      type: oauth2
      x-auth-env-vars:
        - OAUTH_CLIENT_ID
        - OAUTH_CLIENT_SECRET
      x-speakeasy-example: OAUTH_TOKEN
      flows:
        clientCredentials:
          tokenUrl: https://api.example.com/oauth/token
          scopes: {}
paths:
  /widgets:
    get:
      security:
        - ClientCredentials: []
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, []string{"OAUTH_CLIENT_ID", "OAUTH_CLIENT_SECRET"}, parsed.Auth.EnvVars)
}

func TestOpenAPIAuthOverrideExtensions(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: FlightGoat
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: x-apikey
      x-auth-env-vars:
        - FLIGHTAWARE_API_KEY
      x-auth-optional: true
      x-auth-key-url: https://flightaware.com/commercial/aeroapi/
      x-auth-title: FlightAware AeroAPI Key
      x-auth-description: Optional FlightAware AeroAPI credential for enriched flight data.
paths:
  /flights:
    get:
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, "api_key", parsed.Auth.Type)
	assert.Equal(t, []string{"FLIGHTAWARE_API_KEY"}, parsed.Auth.EnvVars)
	assert.True(t, parsed.Auth.Optional)
	assert.Equal(t, "https://flightaware.com/commercial/aeroapi/", parsed.Auth.KeyURL)
	assert.Equal(t, "FlightAware AeroAPI Key", parsed.Auth.Title)
	assert.Equal(t, "Optional FlightAware AeroAPI credential for enriched flight data.", parsed.Auth.Description)
}

func TestOpenAPIAuthEnvVarsPopulateRichDefaults(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Todoist
  version: "1.0.0"
servers:
  - url: https://api.todoist.com
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: Authorization
      x-auth-env-vars:
        - TODOIST_API_KEY
paths:
  /tasks:
    get:
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, []string{"TODOIST_API_KEY"}, parsed.Auth.EnvVars)
	require.Len(t, parsed.Auth.EnvVarSpecs, 1)
	assert.Equal(t, spec.AuthEnvVar{
		Name:      "TODOIST_API_KEY",
		Kind:      spec.AuthEnvVarKindPerCall,
		Required:  true,
		Sensitive: true,
		Inferred:  true,
	}, parsed.Auth.EnvVarSpecs[0])
}

func TestOpenAPIAuthVarsRichOverride(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Todoist
  version: "1.0.0"
servers:
  - url: https://api.todoist.com
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: Authorization
      x-auth-vars:
        - name: TODOIST_API_KEY
          kind: per_call
          required: true
          sensitive: true
          description: Todoist API key.
paths:
  /tasks:
    get:
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, []string{"TODOIST_API_KEY"}, parsed.Auth.EnvVars)
	require.Len(t, parsed.Auth.EnvVarSpecs, 1)
	assert.Equal(t, spec.AuthEnvVar{
		Name:        "TODOIST_API_KEY",
		Kind:        spec.AuthEnvVarKindPerCall,
		Required:    true,
		Sensitive:   true,
		Description: "Todoist API key.",
	}, parsed.Auth.EnvVarSpecs[0])
}

func TestOpenAPIAuthVarsPreservesExplicitSensitiveFalse(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Public Slug API
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    AccountSlug:
      type: apiKey
      in: header
      name: X-Account-Slug
      x-auth-vars:
        - name: PUBLIC_ACCOUNT_SLUG
          kind: per_call
          required: true
          sensitive: false
paths:
  /items:
    get:
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	require.Len(t, parsed.Auth.EnvVarSpecs, 1)
	assert.False(t, parsed.Auth.EnvVarSpecs[0].Sensitive)
}

func TestOpenAPIAuthVarsMalformedFallsBackToDefaults(t *testing.T) {
	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Todoist
  version: "1.0.0"
servers:
  - url: https://api.todoist.com
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: Authorization
      x-auth-vars:
        - name: TODOIST_API_KEY
          kind: per_call
paths:
  /tasks:
    get:
      responses:
        "200":
          description: OK
`)
	var parsed *spec.APISpec
	var err error
	warnings := captureWarnings(t, func() {
		parsed, err = Parse(yamlSpec)
	})
	require.NoError(t, err)

	assert.Contains(t, warnings, "components.securitySchemes.ApiKeyAuth.x-auth-vars is malformed")
	assert.Equal(t, []string{"TODOIST_API_KEY"}, parsed.Auth.EnvVars)
	require.Len(t, parsed.Auth.EnvVarSpecs, 1)
	assert.Equal(t, spec.AuthEnvVarKindPerCall, parsed.Auth.EnvVarSpecs[0].Kind)
	assert.True(t, parsed.Auth.EnvVarSpecs[0].Required)
	assert.True(t, parsed.Auth.EnvVarSpecs[0].Sensitive)
}

func TestOpenAPIAuthClassifiesCookieAndOAuth2ClientCredentialsEnvVars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []spec.AuthEnvVar
	}{
		{
			name: "cookie auth is harvested",
			body: `openapi: "3.0.3"
info:
  title: Cookie API
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    cookieAuth:
      type: apiKey
      in: cookie
      name: sessionid
paths:
  /me:
    get:
      responses:
        "200":
          description: OK
`,
			want: []spec.AuthEnvVar{{
				Name:      "COOKIE_COOKIE_AUTH",
				Kind:      spec.AuthEnvVarKindHarvested,
				Required:  true,
				Sensitive: true,
				Inferred:  true,
			}},
		},
		{
			name: "oauth2 client credentials are auth flow inputs",
			body: `openapi: "3.0.3"
info:
  title: Auth0Mgmt
  version: "1.0"
servers:
  - url: https://example.auth0.com
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://example.auth0.com/oauth/token
          scopes:
            read:users: Read user profiles
paths:
  /api/v2/users:
    get:
      security:
        - OAuth2: []
      responses:
        "200":
          description: OK
`,
			want: []spec.AuthEnvVar{
				{Name: "AUTH0MGMT_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: false, Inferred: true},
				{Name: "AUTH0MGMT_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: true, Inferred: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parsed, err := Parse([]byte(tt.body))
			require.NoError(t, err)
			assert.Equal(t, tt.want, parsed.Auth.EnvVarSpecs)
			if parsed.Auth.OAuth2Grant == spec.OAuth2GrantClientCredentials {
				require.Len(t, parsed.Auth.EnvVars, len(parsed.Auth.EnvVarSpecs))
				for i, envVar := range parsed.Auth.EnvVarSpecs {
					assert.Equal(t, envVar.Name, parsed.Auth.EnvVars[i])
				}
			}
		})
	}
}

func TestOpenAPINoSecurityHasNoAuthEnvVars(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Public API
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths:
  /items:
    get:
      responses:
        "200":
          description: OK
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	assert.Equal(t, "none", parsed.Auth.Type)
	assert.Empty(t, parsed.Auth.EnvVars)
	assert.Empty(t, parsed.Auth.EnvVarSpecs)
}

func TestOpenAPIAuthVarsCanConsolidateLegacyMultipleEnvVars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		extension string
		want      []spec.AuthEnvVar
	}{
		{
			name: "legacy override preserves multiple generated entries",
			extension: `      x-auth-env-vars:
        - TRIGGER_SECRET_KEY
        - TRIGGER_DEV_API_KEY`,
			want: []spec.AuthEnvVar{
				{Name: "TRIGGER_SECRET_KEY", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true, Inferred: true},
				{Name: "TRIGGER_DEV_API_KEY", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true, Inferred: true},
			},
		},
		{
			name: "rich override consolidates to declared credential",
			extension: `      x-auth-env-vars:
        - TRIGGER_SECRET_KEY
        - TRIGGER_DEV_API_KEY
      x-auth-vars:
        - name: TRIGGER_SECRET_KEY
          kind: per_call
          required: true
          sensitive: true`,
			want: []spec.AuthEnvVar{
				{Name: "TRIGGER_SECRET_KEY", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			yamlSpec := fmt.Appendf(nil, `openapi: "3.0.3"
info:
  title: Trigger Dev
  version: "1.0.0"
servers:
  - url: https://api.trigger.dev
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: Authorization
%s
paths:
  /runs:
    get:
      responses:
        "200":
          description: OK
`, tt.extension)
			parsed, err := Parse(yamlSpec)
			require.NoError(t, err)
			assert.Equal(t, tt.want, parsed.Auth.EnvVarSpecs)
		})
	}
}

func TestInferOperationLevelBearer(t *testing.T) {
	t.Parallel()

	t.Run("detects bearer auth from required Authorization header params", func(t *testing.T) {
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
		// 1 out of 5 operations = 20% < 80% threshold
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
						Name: "Authorization", In: "header", Required: true, Description: "Bearer token credential",
					}},
				}
			}
			doc.Paths.Set(path, pathItem)
		}
		result := mapAuth(doc, "test-api")
		assert.Equal(t, "none", result.Type)
	})

	t.Run("detects auth at exact eighty percent threshold", func(t *testing.T) {
		doc := &openapi3.T{
			Info:  &openapi3.Info{Title: "test", Description: "no auth keywords"},
			Paths: &openapi3.Paths{},
		}
		for i, path := range []string{"/a", "/b", "/c", "/d", "/e"} {
			pathItem := &openapi3.PathItem{
				Get: &openapi3.Operation{Responses: openapi3.NewResponses()},
			}
			if i < 4 {
				pathItem.Get.Parameters = openapi3.Parameters{
					&openapi3.ParameterRef{Value: &openapi3.Parameter{
						Name: "Authorization", In: "header", Required: true, Description: "Bearer token credential",
					}},
				}
			}
			doc.Paths.Set(path, pathItem)
		}
		result := mapAuth(doc, "test-api")
		assert.Equal(t, "bearer_token", result.Type)
		assert.Equal(t, []string{"TEST_API_TOKEN"}, result.EnvVars)
		assert.True(t, result.Inferred)
	})

	t.Run("does not infer bearer without bearer signal", func(t *testing.T) {
		doc := &openapi3.T{
			Info:  &openapi3.Info{Title: "test", Description: "no auth keywords"},
			Paths: &openapi3.Paths{},
		}
		for _, path := range []string{"/a", "/b", "/c", "/d", "/e"} {
			pathItem := &openapi3.PathItem{
				Get: &openapi3.Operation{
					Responses: openapi3.NewResponses(),
					Parameters: openapi3.Parameters{
						&openapi3.ParameterRef{Value: &openapi3.Parameter{
							Name: "Authorization", In: "header", Required: true,
						}},
					},
				},
			}
			doc.Paths.Set(path, pathItem)
		}
		result := mapAuth(doc, "test-api")
		assert.Equal(t, "none", result.Type)
	})

	t.Run("does not infer bearer from negated bearer signal", func(t *testing.T) {
		doc := &openapi3.T{
			Info:  &openapi3.Info{Title: "test", Description: "no auth keywords"},
			Paths: &openapi3.Paths{},
		}
		for _, path := range []string{"/a", "/b", "/c", "/d", "/e"} {
			pathItem := &openapi3.PathItem{
				Get: &openapi3.Operation{
					Responses: openapi3.NewResponses(),
					Parameters: openapi3.Parameters{
						&openapi3.ParameterRef{Value: &openapi3.Parameter{
							Name: "Authorization", In: "header", Required: true, Description: "Do not use Bearer prefix.",
						}},
					},
				},
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
							Name: "Authorization", In: "header", Required: false, Description: "Bearer token credential",
						}},
					},
				},
			}
			doc.Paths.Set(path, pathItem)
		}
		result := mapAuth(doc, "test-api")
		assert.Equal(t, "none", result.Type)
	})

	t.Run("top-level security declaration disables inline inference", func(t *testing.T) {
		doc := &openapi3.T{
			Info:     &openapi3.Info{Title: "test", Description: "no auth keywords"},
			Paths:    &openapi3.Paths{},
			Security: openapi3.SecurityRequirements{},
		}
		for _, path := range []string{"/a", "/b", "/c", "/d", "/e"} {
			pathItem := &openapi3.PathItem{
				Get: &openapi3.Operation{
					Responses: openapi3.NewResponses(),
					Parameters: openapi3.Parameters{
						&openapi3.ParameterRef{Value: &openapi3.Parameter{
							Name: "Authorization", In: "header", Required: true, Description: "Bearer token credential",
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

	t.Run("anonymous security alternative on every operation makes whole API no-auth", func(t *testing.T) {
		t.Parallel()
		yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Optional Auth API
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    basicAuth:
      type: http
      scheme: basic
    cookieAuth:
      type: apiKey
      in: cookie
      name: sessionid
paths:
  /pokemon:
    get:
      summary: List pokemon
      security:
        - cookieAuth: []
        - basicAuth: []
        - {}
      responses:
        "200":
          description: OK
  /pokemon/{id}:
    get:
      summary: Get pokemon
      security:
        - cookieAuth: []
        - basicAuth: []
        - {}
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: OK
`)
		parsed, err := Parse(yamlSpec)
		require.NoError(t, err)

		assert.Equal(t, "none", parsed.Auth.Type)
		for _, r := range parsed.Resources {
			for _, e := range r.Endpoints {
				assert.True(t, e.NoAuth, "%s %s should be public", e.Method, e.Path)
			}
		}
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

func TestParseTierRoutingExtensions(t *testing.T) {
	t.Parallel()
	data := []byte(`
openapi: 3.0.3
info:
  title: Tiered API
  version: 1.0.0
servers:
  - url: https://api.example.com
x-tier-routing:
  default_tier: free
  tiers:
    free:
      auth:
        type: none
    paid:
      base_url: https://paid.api.example.com
      auth:
        type: api_key
        in: query
        header: api_key
        env_vars: [TIERED_PAID_KEY]
security:
  - ApiKeyAuth: []
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: X-API-Key
paths:
  /items:
    x-tier: free
    get:
      summary: List items
      responses:
        "200":
          description: ok
  /items/{id}:
    get:
      summary: Get item
      x-tier: paid
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err)
	require.True(t, parsed.HasTierRouting())
	assert.Equal(t, "free", parsed.TierRouting.DefaultTier)
	assert.Equal(t, "none", parsed.TierRouting.Tiers["free"].Auth.Type)
	assert.Equal(t, "https://paid.api.example.com", parsed.TierRouting.Tiers["paid"].BaseURL)
	items := parsed.Resources["items"]
	require.NotNil(t, items.Endpoints)
	assert.Equal(t, "free", findEndpointByPath(items, "/items").Tier)
	assert.Equal(t, "paid", findEndpointByPath(items, "/items/{id}").Tier)
}

func TestParseTierRoutingExtensionFromInfo(t *testing.T) {
	t.Parallel()
	data := []byte(`
openapi: 3.0.3
info:
  title: Tiered API
  version: 1.0.0
  x-tier-routing:
    default_tier: free
    tiers:
      free:
        auth:
          type: none
servers:
  - url: https://api.example.com
paths:
  /items:
    get:
      summary: List items
      responses:
        "200":
          description: ok
`)

	parsed, err := Parse(data)
	require.NoError(t, err)
	require.True(t, parsed.HasTierRouting())
	assert.Equal(t, "free", parsed.TierRouting.DefaultTier)
	assert.Equal(t, "none", parsed.TierRouting.Tiers["free"].Auth.Type)
}

func TestParseTierRoutingRejectsAnonymousSecurityOnCredentialTier(t *testing.T) {
	t.Parallel()
	data := []byte(`
openapi: 3.0.3
info:
  title: Contradictory Tier API
  version: 1.0.0
servers:
  - url: https://api.example.com
x-tier-routing:
  default_tier: free
  tiers:
    free:
      auth:
        type: none
    paid:
      auth:
        type: bearer_token
        env_vars: [TIERED_PAID_TOKEN]
security:
  - ApiKeyAuth: []
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: X-API-Key
paths:
  /items:
    get:
      summary: List paid items
      x-tier: paid
      security: []
      responses:
        "200":
          description: ok
`)

	_, err := Parse(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no_auth")
	assert.Contains(t, err.Error(), "paid")
}

func findEndpointByPath(resource spec.Resource, path string) spec.Endpoint {
	for _, endpoint := range resource.Endpoints {
		if endpoint.Path == path {
			return endpoint
		}
	}
	for _, sub := range resource.SubResources {
		for _, endpoint := range sub.Endpoints {
			if endpoint.Path == path {
				return endpoint
			}
		}
	}
	return spec.Endpoint{}
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

// TestSelectDescription locks in that the OpenAPI parser prefers
// the long-form `description` over the short `summary` when both are
// present. The earlier rule ("if summary has spaces, use it")
// inverted the priority for the common case where a multi-word
// summary sits alongside a rich description, and was the root cause
// behind 47 thin-mcp-description findings on the scrape-creators
// CLI even though every endpoint had rich source description text.
func TestSelectDescription(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		summary     string
		description string
		want        string
	}{
		{
			name:        "rich description wins over multi-word summary",
			summary:     "Get credit balance",
			description: "Returns the number of API credits remaining on your Scrape Creators account.",
			want:        "Returns the number of API credits remaining on your Scrape Creators account.",
		},
		{
			name:        "rich description wins over single-word summary",
			summary:     "Profile",
			description: "Fetches public profile data for a TikTok user by their handle.",
			want:        "Fetches public profile data for a TikTok user by their handle.",
		},
		{
			name:        "summary used when description empty",
			summary:     "Get the user",
			description: "",
			want:        "Get the user",
		},
		{
			name:        "shorter description (placeholder) falls back to summary",
			summary:     "Returns the order with full line items and shipping address",
			description: "TODO",
			want:        "Returns the order with full line items and shipping address",
		},
		{
			name:        "mangled operationID summary is humanized when alone",
			summary:     "GetUserById",
			description: "",
			want:        "Get user by id",
		},
		{
			name:        "both empty returns empty",
			summary:     "",
			description: "",
			want:        "",
		},
		{
			name:        "description-only case",
			summary:     "",
			description: "Returns recent orders.",
			want:        "Returns recent orders.",
		},
		{
			name:        "description equal length to summary still prefers description",
			summary:     "abc",
			description: "xyz",
			want:        "xyz",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := selectDescription(tt.summary, tt.description)
			assert.Equal(t, tt.want, got)
		})
	}
}

// findEndpoint walks resource endpoints (top-level and sub-resource) returning
// the first endpoint whose path matches. Test helper.
func findEndpoint(t *testing.T, parsed *spec.APISpec, path string) spec.Endpoint {
	t.Helper()
	for _, r := range parsed.Resources {
		for _, e := range r.Endpoints {
			if e.Path == path {
				return e
			}
		}
		for _, sub := range r.SubResources {
			for _, e := range sub.Endpoints {
				if e.Path == path {
					return e
				}
			}
		}
	}
	t.Fatalf("no endpoint found at path %q", path)
	return spec.Endpoint{}
}

func TestParseReadsXResourceIDAndXCritical(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string // OpenAPI path key — kept stable across cases
		extraExt     string // extra path-item extensions injected raw
		wantIDField  string
		wantCritical bool
	}{
		{
			name: "x-resource-id explicit string wins over schema fallbacks",
			extraExt: `    x-resource-id: ticker
    x-critical: true
`,
			wantIDField:  "ticker",
			wantCritical: true,
		},
		{
			name: "x-critical accepts string \"true\"",
			extraExt: `    x-resource-id: ticker
    x-critical: "true"
`,
			wantIDField:  "ticker",
			wantCritical: true,
		},
		{
			name: "x-critical accepts string \"1\"",
			extraExt: `    x-resource-id: ticker
    x-critical: "1"
`,
			wantIDField:  "ticker",
			wantCritical: true,
		},
		{
			name: "x-critical false (bool) leaves resource non-critical",
			extraExt: `    x-resource-id: ticker
    x-critical: false
`,
			wantIDField:  "ticker",
			wantCritical: false,
		},
		{
			name: "x-critical non-truthy string treated as false",
			extraExt: `    x-resource-id: ticker
    x-critical: "maybe"
`,
			wantIDField:  "ticker",
			wantCritical: false,
		},
		{
			name: "malformed x-resource-id integer ignored, falls back to id",
			extraExt: `    x-resource-id: 123
`,
			wantIDField:  "id", // fallback tier 2: response schema declares id
			wantCritical: false,
		},
		{
			name:         "no extensions: response-schema fallback picks id",
			extraExt:     ``,
			wantIDField:  "id",
			wantCritical: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Test
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /widgets:
` + tt.extraExt + `    get:
      operationId: listWidgets
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
                    id:
                      type: string
                    label:
                      type: string
`)
			parsed, err := Parse(yamlSpec)
			require.NoError(t, err)

			ep := findEndpoint(t, parsed, "/widgets")
			assert.Equal(t, tt.wantIDField, ep.IDField, "IDField")
			assert.Equal(t, tt.wantCritical, ep.Critical, "Critical")
		})
	}
}

func TestParseIDFieldFallbackChain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		schemaYAML string
		wantID     string
	}{
		{
			name: "tier 2: id present (required)",
			schemaYAML: `                  type: object
                  required: [id]
                  properties:
                    id: {type: string}
                    label: {type: string}
`,
			wantID: "id",
		},
		{
			name: "tier 2: id present (optional) still wins",
			schemaYAML: `                  type: object
                  properties:
                    id: {type: string}
                    label: {type: string}
`,
			wantID: "id",
		},
		{
			name: "tier 3: name when id absent",
			schemaYAML: `                  type: object
                  properties:
                    name: {type: string}
                    description: {type: string}
`,
			wantID: "name",
		},
		{
			name: "tier 4: first required scalar when id and name absent",
			schemaYAML: `                  type: object
                  required: [ticker, market]
                  properties:
                    market: {type: string}
                    ticker: {type: string}
                    description: {type: string}
`,
			wantID: "ticker",
		},
		{
			name: "tier 4: object-typed required field is skipped, next scalar wins",
			schemaYAML: `                  type: object
                  required: [meta, code]
                  properties:
                    meta:
                      type: object
                      properties:
                        version: {type: string}
                    code: {type: integer}
`,
			wantID: "code",
		},
		{
			name: "tier 5: bottoms out when no required scalar exists",
			schemaYAML: `                  type: object
                  properties:
                    payload:
                      type: object
                      properties:
                        x: {type: string}
`,
			wantID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Test
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /things:
    get:
      operationId: listThings
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: array
                items:
` + tt.schemaYAML)
			parsed, err := Parse(yamlSpec)
			require.NoError(t, err)

			ep := findEndpoint(t, parsed, "/things")
			assert.Equal(t, tt.wantID, ep.IDField)
			assert.False(t, ep.Critical)
		})
	}
}

// TestParseXResourceIDAppliesToEveryOperationOnPath exercises the "extensions
// live on the path item" rule — both GET and POST operations under /widgets
// inherit the x-resource-id and x-critical values, even though x-critical is
// only meaningful for the syncable list endpoint.
func TestParseXResourceIDAppliesToEveryOperationOnPath(t *testing.T) {
	t.Parallel()

	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: Test
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /widgets:
    x-resource-id: widget_uid
    x-critical: true
    get:
      operationId: listWidgets
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
                    id: {type: string}
    post:
      operationId: createWidget
      responses:
        "201":
          description: Created
`)
	parsed, err := Parse(yamlSpec)
	require.NoError(t, err)

	var seen int
	for _, r := range parsed.Resources {
		for _, e := range r.Endpoints {
			if e.Path == "/widgets" {
				assert.Equal(t, "widget_uid", e.IDField, "method=%s", e.Method)
				assert.True(t, e.Critical, "method=%s", e.Method)
				seen++
			}
		}
	}
	assert.Equal(t, 2, seen, "expected GET + POST on /widgets to inherit extensions")
}

// TestParsePetstoreXExtensionsBaseline ensures the existing OpenAPI fixture
// (no x-resource-id, no x-critical) is unaffected — IDField falls through to
// the schema-fallback path, Critical stays false.
func TestParsePetstoreXExtensionsBaseline(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "petstore.yaml"))
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)

	for _, r := range parsed.Resources {
		for _, e := range r.Endpoints {
			assert.False(t, e.Critical, "%s %s: Critical must default to false", e.Method, e.Path)
		}
	}
}

// captureWarnings swaps the package-level warnWriter for an in-memory
// buffer, runs fn, and returns whatever fn wrote via warnf. Restores
// warnWriter on exit so other tests aren't affected. Tests are NOT
// parallel-safe with this helper because warnWriter is package-global —
// callers must not call t.Parallel().
func captureWarnings(t *testing.T, fn func()) string {
	t.Helper()
	prev := warnWriter
	var buf bytes.Buffer
	warnWriter = &buf
	defer func() { warnWriter = prev }()
	fn()
	return buf.String()
}

// TestParseFrameworkCollisionRenamesAndWarns asserts that an OpenAPI
// path producing a top-level resource name in ReservedCobraUseNames is
// renamed to <api-slug>-<original> and a warning is emitted naming both
// forms.
func TestParseFrameworkCollisionRenamesAndWarns(t *testing.T) {
	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: TestAPI
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /version:
    get:
      operationId: listVersions
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id:
                      type: string
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id:
                      type: string
`)

	var parsed *spec.APISpec
	output := captureWarnings(t, func() {
		var err error
		parsed, err = Parse(yamlSpec)
		require.NoError(t, err)
	})

	resourceNames := make([]string, 0, len(parsed.Resources))
	for name := range parsed.Resources {
		resourceNames = append(resourceNames, name)
	}
	sort.Strings(resourceNames)

	assert.NotContains(t, resourceNames, "version", "version resource must be renamed; raw `version` would shadow framework cobra command")
	assert.Contains(t, resourceNames, "testapi-version", "renamed resource must use <api-slug>-<original> form")
	assert.Contains(t, resourceNames, "widgets", "non-colliding resources are unchanged")

	assert.Contains(t, output, `"version"`, "warning must name the original resource")
	assert.Contains(t, output, `"testapi-version"`, "warning must name the renamed resource")
	assert.Contains(t, output, "shadow framework cobra command", "warning must explain the failure mode")
}

// TestParseFrameworkCollisionLeavesNonCollidingSpecsAlone asserts specs
// without a colliding resource produce byte-identical resource maps —
// no spurious renames, no warnings emitted.
func TestParseFrameworkCollisionLeavesNonCollidingSpecsAlone(t *testing.T) {
	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: TestAPI
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id:
                      type: string
`)

	var parsed *spec.APISpec
	output := captureWarnings(t, func() {
		var err error
		parsed, err = Parse(yamlSpec)
		require.NoError(t, err)
	})

	require.Contains(t, parsed.Resources, "widgets")
	assert.NotContains(t, output, "shadow framework cobra command", "non-colliding spec must not emit a collision warning")
}

// TestParseFrameworkCollisionExemptsSubresources verifies sub-resources
// don't trigger the collision check — paths like /games/{id}/version
// produce a `version` sub-resource under `games`, which registers as a
// subcommand of `games` rather than at the root, so it can't shadow the
// framework's top-level `version` command.
func TestParseFrameworkCollisionExemptsSubresources(t *testing.T) {
	yamlSpec := []byte(`openapi: "3.0.3"
info:
  title: TestAPI
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /games:
    get:
      operationId: listGames
      responses:
        "200":
          description: ok
  /games/{id}/version:
    get:
      operationId: getGameVersion
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
`)

	var parsed *spec.APISpec
	output := captureWarnings(t, func() {
		var err error
		parsed, err = Parse(yamlSpec)
		require.NoError(t, err)
	})

	// The path /games/{id}/version creates a `games` resource with a
	// `version` sub-resource — neither needs renaming. Top-level `games`
	// stays as-is; sub-resource `version` lives under it.
	require.Contains(t, parsed.Resources, "games")
	games := parsed.Resources["games"]
	assert.Contains(t, games.SubResources, "version", "version path under games should land as a sub-resource")
	assert.NotContains(t, parsed.Resources, "version", "top-level version resource must not exist — sub-resources are exempt from rename")
	assert.NotContains(t, parsed.Resources, "testapi-version", "no rename should fire for sub-resource paths")
	assert.NotContains(t, output, "shadow framework cobra command", "sub-resources must not trigger the collision warning")
}

// TestParseFrameworkCollisionFallsBackToApiSlugWhenSpecNameEmpty pins
// the empty-slug fallback: when out.Name is empty, the rename uses "api"
// as the slug so the result never has a leading hyphen.
func TestParseFrameworkCollisionFallsBackToApiSlugWhenSpecNameEmpty(t *testing.T) {
	// info.title omitted forces cleanSpecName to return its default ("api"),
	// which the parser then refuses (line 167 in parser.go), so we simulate
	// the empty-slug path by directly invoking renameForFrameworkCollision
	// against a spec.APISpec with Name == "".
	out := &spec.APISpec{Name: "", Resources: map[string]spec.Resource{}}
	output := captureWarnings(t, func() {
		renamed := renameForFrameworkCollision(out, "version", "/version")
		assert.Equal(t, "api-version", renamed, "empty Name must fall back to `api` so the result never starts with a hyphen")
	})
	assert.Contains(t, output, `"api-version"`)
}

// TestParseFrameworkCollisionSelfCollisionBumpsSuffix covers the rare
// case where <api-slug>-<original> itself collides with another resource
// already in out.Resources. The implementation falls through to a
// numeric suffix (-2, -3, ...) until unique.
func TestParseFrameworkCollisionSelfCollisionBumpsSuffix(t *testing.T) {
	out := &spec.APISpec{
		Name: "testapi",
		Resources: map[string]spec.Resource{
			"testapi-version": {}, // pre-existing — forces suffix bump
		},
	}
	output := captureWarnings(t, func() {
		renamed := renameForFrameworkCollision(out, "version", "/version")
		assert.Equal(t, "testapi-version-2", renamed, "first-fallback should suffix -2 when the primary rename target already exists")
	})
	assert.Contains(t, output, `"testapi-version-2"`)
}

// TestFilterGlobalParamsRequiresMinEndpoints pins the open-meteo regression:
// a single-endpoint spec with many query parameters used to have all its
// params stripped because every param trivially appeared on 100% of
// endpoints (1/1) and the >80% global-filter threshold matched. The filter
// now requires at least 3 endpoints before it considers any pattern
// "global" — fewer endpoints means there's nothing meaningful to compare.
func TestFilterGlobalParamsRequiresMinEndpoints(t *testing.T) {
	t.Parallel()

	specYAML := `openapi: 3.0.0
info:
  title: TestAPI
  version: "1.0"
paths:
  /v1/forecast:
    get:
      operationId: list
      tags: [forecast]
      parameters:
        - name: latitude
          in: query
          required: true
          schema: {type: number}
        - name: longitude
          in: query
          required: true
          schema: {type: number}
        - name: hourly
          in: query
          schema: {type: string}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: object}
`
	spec, err := Parse([]byte(specYAML))
	require.NoError(t, err)

	resource, ok := spec.Resources["forecast"]
	require.True(t, ok, "forecast resource should exist")
	endpoint, ok := resource.Endpoints["list"]
	require.True(t, ok, "list endpoint should exist")
	assert.Len(t, endpoint.Params, 3, "single-endpoint spec must keep its params; the global-filter must not strip them")

	names := map[string]bool{}
	for _, p := range endpoint.Params {
		names[p.Name] = true
	}
	for _, want := range []string{"latitude", "longitude", "hourly"} {
		assert.True(t, names[want], "param %q must be preserved", want)
	}
}

// TestParsePerOperationServersFallback covers the case where a spec has no
// top-level `servers:` block but each operation declares its own. The parser
// must walk per-operation servers and pick the most common one as base URL.
// Pre-fix this hit https://api.example.com and produced a CLI that DNS-failed
// every call — see cli-printing-press#510 for the open-meteo report.
func TestParsePerOperationServersFallback(t *testing.T) {
	t.Parallel()

	specYAML := `openapi: "3.0.3"
info:
  title: Per-Op Servers Test
  version: "1.0"
paths:
  /forecast:
    get:
      operationId: forecast
      servers:
        - url: https://api.example.com
      responses:
        '200':
          description: OK
  /historical:
    get:
      operationId: historical
      servers:
        - url: https://archive.example.com
      responses:
        '200':
          description: OK
  /weather:
    get:
      operationId: weather
      servers:
        - url: https://api.example.com
      responses:
        '200':
          description: OK
`
	parsed, err := Parse([]byte(specYAML))
	require.NoError(t, err)
	// api.example.com appears 2x, archive.example.com appears 1x — most-common wins.
	assert.Equal(t, "https://api.example.com", parsed.BaseURL)
}

// TestParsePerOperationServersFallbackTieBreak verifies deterministic
// tie-breaking: when two server URLs appear with equal frequency, the
// lexicographically smaller one wins so the output doesn't churn run-to-run.
func TestParsePerOperationServersFallbackTieBreak(t *testing.T) {
	t.Parallel()

	specYAML := `openapi: "3.0.3"
info:
  title: Tie Break Test
  version: "1.0"
paths:
  /alpha:
    get:
      operationId: alpha
      servers:
        - url: https://b.example.com
      responses:
        '200': {description: OK}
  /beta:
    get:
      operationId: beta
      servers:
        - url: https://a.example.com
      responses:
        '200': {description: OK}
`
	parsed, err := Parse([]byte(specYAML))
	require.NoError(t, err)
	// Both URLs appear once; lexicographically smallest wins.
	assert.Equal(t, "https://a.example.com", parsed.BaseURL)
}

// TestParseTopLevelServersStillPreferred verifies the per-operation walk is
// only used as a fallback. When top-level `servers:` is set, the parser must
// continue to use it even if operations also declare their own.
func TestParseTopLevelServersStillPreferred(t *testing.T) {
	t.Parallel()

	specYAML := `openapi: "3.0.3"
info:
  title: Top-Level Wins Test
  version: "1.0"
servers:
  - url: https://global.example.com
paths:
  /thing:
    get:
      operationId: thing
      servers:
        - url: https://override.example.com
      responses:
        '200': {description: OK}
`
	parsed, err := Parse([]byte(specYAML))
	require.NoError(t, err)
	assert.Equal(t, "https://global.example.com", parsed.BaseURL)
}

func findParsedEndpointByPath(t *testing.T, parsed *spec.APISpec, method, path string) spec.Endpoint {
	t.Helper()
	for _, resource := range parsed.Resources {
		for _, endpoint := range resource.Endpoints {
			if endpoint.Method == method && endpoint.Path == path {
				return endpoint
			}
		}
		for _, sub := range resource.SubResources {
			for _, endpoint := range sub.Endpoints {
				if endpoint.Method == method && endpoint.Path == path {
					return endpoint
				}
			}
		}
	}
	t.Fatalf("endpoint %s %s not found", method, path)
	return spec.Endpoint{}
}
