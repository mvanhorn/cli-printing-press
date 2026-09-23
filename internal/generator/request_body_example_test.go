package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/shellargs"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExampleLineSynthesizesRequestBodyMediaExample(t *testing.T) {
	t.Parallel()

	g := New(minimalSpec("body-ex"), t.TempDir())
	ep := spec.Endpoint{
		Method: "POST",
		Path:   "/accounts",
		Body: []spec.Param{
			{Name: "holder", Type: "string", Required: true, Example: "Acme"},
			{Name: "currency", Type: "string", Example: "USD"},
		},
		RequestBodyExample: map[string]any{"currency": "GBP", "holder": "Acme Ltd"},
	}

	got := g.exampleLine("accounts", "open", ep)
	assert.Equal(t, "  body-ex-pp-cli accounts open --holder 'Acme Ltd' --currency GBP", got)
	// Property examples stay on the happy-path surface. The media example
	// must not replace them.
	assert.Equal(t, "--holder=Acme", endpointHappyArgs(ep))
}

func TestExampleLineComposesRequiredBodyPropertyExamples(t *testing.T) {
	t.Parallel()

	g := New(minimalSpec("body-ex"), t.TempDir())
	ep := spec.Endpoint{
		Method: "POST",
		Path:   "/transfers",
		Body: []spec.Param{
			{Name: "amount", Type: "integer", Required: true, Example: float64(2500)},
			{Name: "memo", Type: "string", Required: true, Example: "invoice 41"},
			{Name: "note", Type: "string", Example: "optional note"},
		},
	}

	got := g.exampleLine("transfers", "create", ep)
	assert.Equal(t, "  body-ex-pp-cli transfers create --amount 2500 --memo 'invoice 41'", got)
	assert.NotContains(t, got, "optional note")
	assert.Empty(t, endpointHappyArgs(ep))
}

func TestExampleLineComposesNestedRequiredBodyExamples(t *testing.T) {
	t.Parallel()

	g := New(minimalSpec("body-ex"), t.TempDir())
	ep := spec.Endpoint{
		Method: "POST",
		Path:   "/places",
		Body: []spec.Param{{
			Name:     "address",
			Type:     "object",
			Required: true,
			Fields: []spec.Param{
				{Name: "city", Type: "string", Required: true, Example: "London"},
			},
		}},
		RequestBodyExample: map[string]any{"address": map[string]any{"city": "Paris"}},
	}

	got := g.exampleLine("places", "create", ep)
	assert.Equal(t, "  body-ex-pp-cli places create --address-city Paris", got)
}

func TestRequestBodyExampleSynthesisEdges(t *testing.T) {
	t.Parallel()

	g := New(minimalSpec("body-ex"), t.TempDir())
	deepLeaf := spec.Param{Name: "leaf", Type: "string", Required: true, Example: "shown"}
	deepObject := spec.Param{
		Name: "object", Type: "object", Required: true,
		Example: map[string]any{"deep": "hidden"},
		Fields: []spec.Param{
			{Name: "deep", Type: "string", Required: true, Example: "hidden"},
		},
	}
	deepBody := []spec.Param{{
		Name: "parent", Type: "object", Required: true,
		Fields: []spec.Param{{
			Name: "child", Type: "object", Required: true,
			Fields: []spec.Param{deepLeaf, deepObject},
		}},
	}}
	truncatedOnly := []spec.Param{{
		Name: "parent", Type: "object", Required: true,
		Fields: []spec.Param{{
			Name: "child", Type: "object", Required: true,
			Fields: []spec.Param{deepObject},
		}},
	}}

	tests := []struct {
		name     string
		command  string
		endpoint string
		ep       spec.Endpoint
		want     string
	}{
		{
			name:     "partial media example falls back to required properties",
			command:  "places",
			endpoint: "create",
			ep: spec.Endpoint{
				Method: "POST",
				Path:   "/places",
				Body: []spec.Param{
					{Name: "holder", Type: "string", Example: "Acme"},
					{Name: "address", Type: "object", Required: true, Fields: []spec.Param{
						{Name: "city", Type: "string", Required: true, Example: "London"},
					}},
				},
				RequestBodyExample: map[string]any{"holder": "Beta"},
			},
			want: "  body-ex-pp-cli places create --address-city London",
		},
		{
			name:     "partial media example does not replace complete property examples",
			command:  "accounts",
			endpoint: "open",
			ep: spec.Endpoint{
				Method: "POST",
				Path:   "/accounts",
				Body: []spec.Param{
					{Name: "holder", Type: "string", Required: true, Example: "Acme"},
					{Name: "currency", Type: "string", Required: true, Example: "USD"},
				},
				RequestBodyExample: map[string]any{"holder": "Beta Inc"},
			},
			want: "  body-ex-pp-cli accounts open --holder Acme --currency USD",
		},
		{
			name:     "partial media example without property examples is omitted",
			command:  "places",
			endpoint: "create",
			ep: spec.Endpoint{
				Method: "POST",
				Path:   "/places",
				Body: []spec.Param{
					{Name: "holder", Type: "string"},
					{Name: "address", Type: "object", Required: true, Fields: []spec.Param{
						{Name: "city", Type: "string", Required: true},
					}},
				},
				RequestBodyExample: map[string]any{"holder": "Beta"},
			},
		},
		{
			name:     "partial optional object falls back to required properties",
			command:  "places",
			endpoint: "create",
			ep: spec.Endpoint{
				Method: "POST",
				Path:   "/places",
				Body: []spec.Param{
					{Name: "holder", Type: "string", Required: true, Example: "Acme"},
					{Name: "address", Type: "object", Fields: []spec.Param{
						{Name: "city", Type: "string", Required: true, Example: "London"},
						{Name: "zip", Type: "string", Example: "E1"},
					}},
				},
				RequestBodyExample: map[string]any{
					"holder":  "Beta",
					"address": map[string]any{"zip": "SW1"},
				},
			},
			want: "  body-ex-pp-cli places create --holder Acme",
		},
		{
			name:     "complete optional object keeps the media example",
			command:  "places",
			endpoint: "create",
			ep: spec.Endpoint{
				Method: "POST",
				Path:   "/places",
				Body: []spec.Param{
					{Name: "holder", Type: "string", Required: true, Example: "Acme"},
					{Name: "address", Type: "object", Fields: []spec.Param{
						{Name: "city", Type: "string", Required: true, Example: "London"},
						{Name: "zip", Type: "string", Example: "E1"},
					}},
				},
				RequestBodyExample: map[string]any{
					"holder":  "Beta",
					"address": map[string]any{"city": "Paris", "zip": "SW1"},
				},
			},
			want: "  body-ex-pp-cli places create --holder Beta --address-city Paris --address-zip SW1",
		},
		{
			name:     "required field with a default does not block the media example",
			command:  "accounts",
			endpoint: "open",
			ep: spec.Endpoint{
				Method: "POST",
				Path:   "/accounts",
				Body: []spec.Param{
					{Name: "holder", Type: "string", Required: true, Example: "Acme"},
					{Name: "currency", Type: "string", Required: true, Default: "USD", Example: "GBP"},
				},
				RequestBodyExample: map[string]any{"holder": "Beta"},
			},
			want: "  body-ex-pp-cli accounts open --holder Beta",
		},
		{
			name:     "media example omits object past flag depth",
			command:  "nodes",
			endpoint: "create",
			ep: spec.Endpoint{
				Method: "POST",
				Path:   "/nodes",
				Body:   deepBody,
				RequestBodyExample: map[string]any{
					"parent": map[string]any{
						"child": map[string]any{
							"leaf":   "shown",
							"object": map[string]any{"deep": "hidden"},
						},
					},
				},
			},
			want: "  body-ex-pp-cli nodes create --parent-child-leaf shown",
		},
		{
			name:     "property examples omit object past flag depth",
			command:  "nodes",
			endpoint: "create",
			ep: spec.Endpoint{
				Method: "POST",
				Path:   "/nodes",
				Body:   deepBody,
			},
			want: "  body-ex-pp-cli nodes create --parent-child-leaf shown",
		},
		{
			name:     "truncated object alone synthesizes no example",
			command:  "nodes",
			endpoint: "create",
			ep: spec.Endpoint{
				Method: "POST",
				Path:   "/nodes",
				Body:   truncatedOnly,
				RequestBodyExample: map[string]any{
					"parent": map[string]any{
						"child": map[string]any{
							"object": map[string]any{"deep": "hidden"},
						},
					},
				},
			},
		},
		{
			name:     "partial object property example falls back to child examples",
			command:  "places",
			endpoint: "create",
			ep: spec.Endpoint{
				Method: "POST",
				Path:   "/places",
				Body: []spec.Param{{
					Name: "address", Type: "object", Required: true,
					Example: map[string]any{"zip": "SW1"},
					Fields: []spec.Param{
						{Name: "city", Type: "string", Required: true, Example: "London"},
						{Name: "zip", Type: "string", Example: "E1"},
					},
				}},
			},
			want: "  body-ex-pp-cli places create --address-city London",
		},
		{
			name:     "complete object property example is used",
			command:  "places",
			endpoint: "create",
			ep: spec.Endpoint{
				Method: "POST",
				Path:   "/places",
				Body: []spec.Param{{
					Name: "address", Type: "object", Required: true,
					Example: map[string]any{"city": "Paris", "zip": "SW1"},
					Fields: []spec.Param{
						{Name: "city", Type: "string", Required: true, Example: "London"},
						{Name: "zip", Type: "string", Example: "E1"},
					},
				}},
			},
			want: "  body-ex-pp-cli places create --address-city Paris --address-zip SW1",
		},
		{
			name:     "flat partial media example falls back to the object flag",
			command:  "uploads",
			endpoint: "create",
			ep: spec.Endpoint{
				Method:             "POST",
				Path:               "/uploads",
				RequestContentType: "multipart/form-data",
				Body: []spec.Param{
					{
						Name: "meta", Type: "object", Required: true,
						Example: map[string]any{"city": "London"},
						Fields: []spec.Param{
							{Name: "city", Type: "string", Required: true, Example: "London"},
						},
					},
					{Name: "note", Type: "string", Example: "hi"},
				},
				RequestBodyExample: map[string]any{"note": "skip-me"},
			},
			want: `  body-ex-pp-cli uploads create --meta '{"city":"London"}'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := g.exampleLine(tt.command, tt.endpoint, tt.ep)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExampleLineKeepsParameterExamplesOverRequestBody(t *testing.T) {
	t.Parallel()

	g := New(minimalSpec("body-ex"), t.TempDir())
	base := spec.Endpoint{
		Method: "POST",
		Path:   "/accounts/{account_id}/entries",
		Params: []spec.Param{{
			Name: "account_id", Type: "string", Required: true, Positional: true, Example: "acct_7f3",
		}},
		Body: []spec.Param{
			{Name: "amount", Type: "integer", Required: true, Example: 2500},
			{Name: "memo", Type: "string", Example: "invoice 41"},
		},
	}
	withMedia := base
	withMedia.RequestBodyExample = map[string]any{"amount": 1, "memo": "nope"}

	got := g.exampleLine("accounts entries", "post-entry", base)
	assert.Equal(t, got, g.exampleLine("accounts entries", "post-entry", withMedia))
	assert.Contains(t, got, "acct_7f3")
	assert.NotContains(t, got, "--amount")
	assert.NotContains(t, got, "invoice")
	assert.NotEmpty(t, endpointHappyArgs(base))
	assert.Equal(t, endpointHappyArgs(base), endpointHappyArgs(withMedia))
}

func TestExampleLineKeepsExplicitExampleOverRequestBody(t *testing.T) {
	t.Parallel()

	g := New(minimalSpec("body-ex"), t.TempDir())
	ep := spec.Endpoint{
		Method:  "POST",
		Path:    "/holds",
		Example: "  body-ex-pp-cli holds create --amount 1",
		Body: []spec.Param{
			{Name: "amount", Type: "integer", Required: true, Example: 9},
		},
		RequestBodyExample: map[string]any{"amount": 99},
	}

	assert.Equal(t, "  body-ex-pp-cli holds create --amount 1", g.exampleLine("holds", "create", ep))
}

func TestPromotedExampleLineSynthesizesRequestBodyExample(t *testing.T) {
	t.Parallel()

	g := New(minimalSpec("body-ex"), t.TempDir())
	ep := spec.Endpoint{
		Method: "POST",
		Path:   "/pings",
		Body: []spec.Param{
			{Name: "message", Type: "string", Required: true, Example: "hello"},
		},
		RequestBodyExample: map[string]any{"message": "pong now"},
	}

	got := g.promotedExampleLine("pings", "send", ep)
	assert.Equal(t, "  body-ex-pp-cli pings --message 'pong now'", got)
	assert.Equal(t, "--message=hello", endpointHappyArgs(ep))
}

func TestExampleLineOmitsBodyWithoutExamples(t *testing.T) {
	t.Parallel()

	g := New(minimalSpec("body-ex"), t.TempDir())
	ep := spec.Endpoint{
		Method: "POST",
		Path:   "/accounts",
		Body:   []spec.Param{{Name: "holder", Type: "string", Required: true}},
	}

	assert.Empty(t, g.exampleLine("accounts", "open", ep))
	assert.Empty(t, endpointHappyArgs(ep))
}

func TestExampleLineBodyJSONFallbackUsesMediaExample(t *testing.T) {
	t.Parallel()

	g := New(minimalSpec("body-ex"), t.TempDir())
	ep := spec.Endpoint{
		Method:             "POST",
		Path:               "/items",
		BodyJSONFallback:   true,
		RequestBodyExample: map[string]any{"holder": "Acme Ltd"},
	}

	got := g.exampleLine("items", "create", ep)
	assert.Contains(t, got, "--body-json")
	assert.Contains(t, got, "Acme Ltd")
	assert.Empty(t, endpointHappyArgs(ep))
}

func TestGeneratedRequestBodyExamples(t *testing.T) {
	t.Parallel()

	t.Run("ledgerops", func(t *testing.T) {
		t.Parallel()
		apiSpec := parseOpenAPISpec(t, ledgerOpsExampleSpec)
		dir := filepath.Join(t.TempDir(), "ledger-ops-pp-cli")
		require.NoError(t, New(apiSpec, dir).Generate())
		sources := cliSources(t, dir)

		_, openSrc := mustCLISource(t, sources, "accounts open --currency GBP")
		openExample, ok := cobraExampleFromSource(t, openSrc)
		require.True(t, ok)
		assert.Equal(t, "  ledger-ops-pp-cli accounts open --currency GBP --holder 'Acme Ltd'", openExample)
		assert.NotContains(t, openSrc, "pp:happy-args")

		_, entrySrc := mustCLISource(t, sources, "acct_7f3")
		entryExample, ok := cobraExampleFromSource(t, entrySrc)
		require.True(t, ok)
		assert.Contains(t, entryExample, "acct_7f3")
		assert.NotContains(t, entryExample, "--")
		assert.Contains(t, entrySrc, "pp:happy-args")
		assert.Contains(t, entrySrc, "amount")

		_, transferSrc := mustCLISource(t, sources, "invoice 41")
		transferExample, ok := cobraExampleFromSource(t, transferSrc)
		require.True(t, ok)
		assert.Equal(t, "  ledger-ops-pp-cli transfers create --amount 2500 --memo 'invoice 41'", transferExample)
		assert.NotContains(t, transferExample, "optional note")
		assert.NotContains(t, transferSrc, "pp:happy-args")

		_, holdSrc := mustCLISource(t, sources, "holds create --amount 1")
		holdExample, ok := cobraExampleFromSource(t, holdSrc)
		require.True(t, ok)
		assert.Equal(t, "  ledger-ops-pp-cli holds create --amount 1", holdExample)
		assert.NotContains(t, holdExample, "99")

		aliasExample := exampleContaining(t, sources, "aliases")
		assert.Contains(t, aliasExample, "Acme Ltd")
		assert.Contains(t, aliasExample, "GBP")
		assert.NotContains(t, aliasExample, "Beta Inc")
		assert.NotContains(t, aliasExample, "USD")

		_, pingSrc := mustCLISource(t, sources, "pong now")
		pingExample, ok := cobraExampleFromSource(t, pingSrc)
		require.True(t, ok)
		assert.Contains(t, pingExample, "pong now")
		assert.NotContains(t, pingExample, "hello")
		assert.Contains(t, pingSrc, `"pp:happy-args": "--message=hello"`)

		if testing.Short() {
			t.Skip("generated CLI help check runs in the full test lane")
		}
		requireGeneratedCompiles(t, dir)
		runGoCommand(t, dir, "build", "-o", "ledger-ops-pp-cli", "./cmd/ledger-ops-pp-cli")
		binary := filepath.Join(dir, "ledger-ops-pp-cli")
		for _, example := range []string{openExample, transferExample, pingExample} {
			help := runSandboxedBinary(t, binary, exampleHelpArgs(t, example)...)
			require.Contains(t, help, "Examples:")
			require.Contains(t, help, strings.TrimSpace(example))
		}
	})

	t.Run("widgetdepot", func(t *testing.T) {
		t.Parallel()
		apiSpec := parseOpenAPISpec(t, widgetDepotExampleSpec)
		dir := filepath.Join(t.TempDir(), "widget-depot-pp-cli")
		require.NoError(t, New(apiSpec, dir).Generate())
		sources := cliSources(t, dir)

		name, src := mustCLISource(t, sources, "Blue widget")
		assert.Equal(t, "widgets_create.go", name)
		example, ok := cobraExampleFromSource(t, src)
		require.True(t, ok)
		assert.Equal(t, "  widget-depot-pp-cli widgets create --colour blue --name 'Blue widget'", example)
		assert.NotContains(t, src, "pp:happy-args")
	})
}

func parseOpenAPISpec(t *testing.T, raw string) *spec.APISpec {
	t.Helper()
	parsed, err := openapi.Parse([]byte(raw))
	require.NoError(t, err)
	return parsed
}

func cliSources(t *testing.T, dir string) map[string]string {
	t.Helper()
	root := filepath.Join(dir, "internal", "cli")
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, entry.Name()))
		require.NoError(t, err)
		out[entry.Name()] = string(body)
	}
	return out
}

func mustCLISource(t *testing.T, sources map[string]string, needle string) (string, string) {
	t.Helper()
	var names []string
	var src string
	for name, body := range sources {
		if strings.Contains(body, needle) {
			names = append(names, name)
			src = body
		}
	}
	require.Len(t, names, 1, "files containing %q: %v", needle, names)
	return names[0], src
}

func exampleContaining(t *testing.T, sources map[string]string, needle string) string {
	t.Helper()
	var found []string
	for _, src := range sources {
		example, ok := cobraExampleFromSource(t, src)
		if !ok || !strings.Contains(example, needle) {
			continue
		}
		found = append(found, example)
	}
	require.Len(t, found, 1, "examples containing %q: %v", needle, found)
	return found[0]
}

func cobraExampleFromSource(t *testing.T, src string) (string, bool) {
	t.Helper()
	for line := range strings.SplitSeq(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "Example:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(trimmed, "Example:"))
		raw = strings.TrimSuffix(raw, ",")
		example, err := strconv.Unquote(raw)
		if err != nil {
			return "", false
		}
		return example, true
	}
	return "", false
}

func exampleHelpArgs(t *testing.T, example string) []string {
	t.Helper()
	tokens, err := shellargs.Split(strings.TrimSpace(example))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(tokens), 2)
	var args []string
	for _, tok := range tokens[1:] {
		if strings.HasPrefix(tok, "-") {
			break
		}
		args = append(args, tok)
	}
	return append(args, "--help")
}

func runSandboxedBinary(t *testing.T, binary string, args ...string) string {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}

const ledgerOpsExampleSpec = `{
  "openapi": "3.0.3",
  "info": {"title": "Ledger Ops", "version": "2.0.0"},
  "servers": [{"url": "https://ledger.example.org/api"}],
  "paths": {
    "/accounts": {
      "get": {
        "operationId": "list_accounts",
        "summary": "List accounts",
        "responses": {"200": {"description": "ok"}}
      },
      "post": {
        "operationId": "open_account",
        "summary": "Open an account",
        "requestBody": {
          "required": true,
          "content": {"application/json": {
            "schema": {"$ref": "#/components/schemas/AccountIn"},
            "example": {"holder": "Acme Ltd", "currency": "GBP"}
          }}
        },
        "responses": {"201": {"description": "created"}}
      }
    },
    "/accounts/{account_id}/entries": {
      "post": {
        "operationId": "post_entry",
        "summary": "Post a ledger entry",
        "parameters": [{
          "name": "account_id",
          "in": "path",
          "required": true,
          "schema": {"type": "string"},
          "example": "acct_7f3"
        }],
        "requestBody": {
          "required": true,
          "content": {"application/json": {
            "schema": {
              "type": "object",
              "required": ["amount"],
              "properties": {
                "amount": {"type": "integer", "example": 2500},
                "memo": {"type": "string", "example": "invoice 41"}
              }
            }
          }}
        },
        "responses": {"201": {"description": "created"}}
      }
    },
    "/transfers": {
      "get": {
        "operationId": "list_transfers",
        "summary": "List transfers",
        "responses": {"200": {"description": "ok"}}
      },
      "post": {
        "operationId": "create_transfer",
        "summary": "Create a transfer",
        "requestBody": {
          "required": true,
          "content": {"application/json": {
            "schema": {
              "type": "object",
              "required": ["amount", "memo"],
              "properties": {
                "amount": {"type": "integer", "example": 2500},
                "memo": {"type": "string", "example": "invoice 41"},
                "note": {"type": "string", "example": "optional note"}
              }
            }
          }}
        },
        "responses": {"201": {"description": "created"}}
      }
    },
    "/holds": {
      "get": {
        "operationId": "list_holds",
        "summary": "List holds",
        "responses": {"200": {"description": "ok"}}
      },
      "post": {
        "operationId": "create_hold",
        "summary": "Create a hold",
        "x-pp-example": "  ledger-ops-pp-cli holds create --amount 1",
        "requestBody": {
          "required": true,
          "content": {"application/json": {
            "schema": {
              "type": "object",
              "required": ["amount"],
              "properties": {"amount": {"type": "integer", "example": 9}}
            },
            "example": {"amount": 99}
          }}
        },
        "responses": {"201": {"description": "created"}}
      }
    },
    "/aliases": {
      "post": {
        "operationId": "create_alias",
        "summary": "Create an alias",
        "requestBody": {
          "required": true,
          "content": {"application/json": {
            "schema": {"$ref": "#/components/schemas/AccountIn"},
            "examples": {
              "usd": {"value": {"holder": "Beta Inc", "currency": "USD"}},
              "gbp": {"value": {"holder": "Acme Ltd", "currency": "GBP"}}
            }
          }}
        },
        "responses": {"201": {"description": "created"}}
      }
    },
    "/pings": {
      "post": {
        "operationId": "send_ping",
        "summary": "Send a ping",
        "requestBody": {
          "required": true,
          "content": {"application/json": {
            "schema": {
              "type": "object",
              "required": ["message"],
              "properties": {"message": {"type": "string", "example": "hello"}}
            },
            "example": {"message": "pong now"}
          }}
        },
        "responses": {"201": {"description": "created"}}
      }
    }
  },
  "components": {
    "schemas": {
      "AccountIn": {
        "type": "object",
        "required": ["holder"],
        "properties": {
          "holder": {"type": "string", "example": "Acme Ltd"},
          "currency": {"type": "string", "example": "GBP"}
        }
      }
    },
    "securitySchemes": {
      "api_key": {"type": "apiKey", "in": "header", "name": "X-Ledger-Key"}
    }
  }
}`

const widgetDepotExampleSpec = `{
  "openapi": "3.0.3",
  "info": {"title": "Widget Depot", "version": "1.0.0"},
  "servers": [{"url": "https://api.widgetdepot.example.com/v1"}],
  "paths": {
    "/widgets": {
      "get": {
        "operationId": "list_widgets",
        "summary": "List widgets",
        "responses": {"200": {"description": "ok"}}
      },
      "post": {
        "operationId": "create_widget",
        "summary": "Create a widget",
        "requestBody": {
          "required": true,
          "content": {"application/json": {
            "schema": {"$ref": "#/components/schemas/WidgetIn"},
            "example": {"name": "Blue widget", "colour": "blue"}
          }}
        },
        "responses": {"201": {"description": "created"}}
      }
    }
  },
  "components": {
    "schemas": {
      "WidgetIn": {
        "type": "object",
        "required": ["name"],
        "properties": {
          "name": {"type": "string", "example": "Blue widget"},
          "colour": {"type": "string", "example": "blue"}
        }
      }
    },
    "securitySchemes": {
      "bearer_auth": {"type": "http", "scheme": "bearer"}
    }
  }
}`
