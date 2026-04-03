package openapi

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

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
