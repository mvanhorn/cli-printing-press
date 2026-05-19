package openapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bigSwagger2Spec returns a Swagger 2.0 JSON document with a large
// `definitions/` block padded so that the `"swagger":"2.0"` marker only
// appears well past the 4KB mark in the serialized output. Models the real
// shape produced by normalizeSpecData when fed a vendor Swagger 2.0 spec:
// encoding/json sorts keys alphabetically, so "definitions" (d) and "paths"
// (p) precede "swagger" (s) in the wire form.
func bigSwagger2Spec() []byte {
	const pad = 8192 // generously past any plausible head-buffer cap
	definitions := map[string]any{
		"Padding": map[string]any{
			"type":        "string",
			"description": strings.Repeat("x", pad),
		},
	}
	doc := map[string]any{
		"swagger":     "2.0",
		"info":        map[string]any{"title": "Big", "version": "1.0.0"},
		"definitions": definitions,
		"paths":       map[string]any{},
	}
	out, err := json.Marshal(doc)
	if err != nil {
		panic(err)
	}
	return out
}

func TestIsSwagger2SpecJSON(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		data []byte
		want bool
	}{
		{
			name: "swagger 2.0 minimal",
			data: []byte(`{"swagger":"2.0","info":{"title":"Demo","version":"1.0.0"},"paths":{}}`),
			want: true,
		},
		{
			name: "swagger 2.0 with space",
			data: []byte(`{"swagger": "2.0","info":{"title":"Demo","version":"1.0.0"},"paths":{}}`),
			want: true,
		},
		{
			name: "openapi 3.0",
			data: []byte(`{"openapi":"3.0.3","info":{"title":"Demo","version":"1.0.0"},"paths":{}}`),
			want: false,
		},
		{
			name: "openapi 3 with swagger mention in description",
			// A spec that mentions "swagger" in a description should not be misdetected.
			data: []byte(`{"openapi":"3.0.3","info":{"title":"Demo","description":"swagger inspired","version":"1.0.0"},"paths":{}}`),
			want: false,
		},
		{
			name: "swagger 2 without minor version",
			// Bare "2" is not a Swagger version string and must not match.
			data: []byte(`{"swagger":"2","info":{"title":"Demo","version":"1.0.0"},"paths":{}}`),
			want: false,
		},
		{
			name: "swagger future minor version",
			// "2.5" is not Swagger 2.0; prefix-match temptation must be avoided.
			data: []byte(`{"swagger":"2.5","info":{"title":"Demo","version":"1.0.0"},"paths":{}}`),
			want: false,
		},
		{
			name: "swagger key after large alphabetized definitions block",
			// normalizeSpecData round-trips through encoding/json, which sorts
			// keys alphabetically; "swagger" (s) lands after "definitions" (d)
			// and "paths" (p). A real Swagger 2.0 spec's "swagger" marker is
			// often far past the start of the serialized JSON. Build a fixture
			// where definitions[]'s padding pushes "swagger" well past 4KB to
			// assert the detector still finds it.
			data: bigSwagger2Spec(),
			want: true,
		},
		{
			name: "empty",
			data: []byte{},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isSwagger2SpecJSON(tc.data))
		})
	}
}

func TestParseSwagger2WithCircularRefsConverts(t *testing.T) {
	t.Parallel()

	// Build a small Swagger 2.0 spec with a bi-directional cycle in
	// definitions/. Pre-fix, this shape combined with kin-openapi's OpenAPI 3
	// loader caused unbounded resolution time. Post-fix, the conversion to
	// OpenAPI 3 should let the parser complete quickly.
	swagger2 := []byte(`{
  "swagger": "2.0",
  "info": {"title": "Cycle Demo", "version": "1.0.0"},
  "host": "api.example.com",
  "basePath": "/v1",
  "schemes": ["https"],
  "paths": {
    "/employees/{id}": {
      "get": {
        "operationId": "getEmployee",
        "parameters": [
          {"name": "id", "in": "path", "required": true, "type": "string"}
        ],
        "responses": {
          "200": {
            "description": "ok",
            "schema": {"$ref": "#/definitions/Employee"}
          }
        }
      }
    }
  },
  "definitions": {
    "Employee": {
      "type": "object",
      "properties": {
        "id": {"type": "string"},
        "name": {"type": "string"},
        "department": {"$ref": "#/definitions/Department"},
        "manager": {"$ref": "#/definitions/Employee"}
      }
    },
    "Department": {
      "type": "object",
      "properties": {
        "id": {"type": "string"},
        "name": {"type": "string"},
        "head": {"$ref": "#/definitions/Employee"}
      }
    }
  }
}`)

	done := make(chan struct{})
	var parsed any
	var parseErr error
	go func() {
		defer close(done)
		parsed, parseErr = Parse(swagger2)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		// Note: this intentionally abandons the Parse goroutine when the
		// regression returns. Parse exposes no cancellation hook, so the
		// alternative is letting the test run for ~25 minutes and OOM the
		// CI runner before failing. Leaking one goroutine for the rest of
		// the test binary's lifetime is the lesser evil; the failure will
		// surface immediately and the run will end shortly after.
		t.Fatal("parsing cyclic Swagger 2.0 spec did not complete within 30s; regression of issue #1241")
	}

	require.NoError(t, parseErr)
	require.NotNil(t, parsed)
}

func TestParseSwagger2BasicEndpointShape(t *testing.T) {
	t.Parallel()

	// Make sure the conversion preserves enough endpoint metadata for the
	// downstream generator. Asserts only the load-bearing shape (resource +
	// endpoint by method+path); fine-grained field-by-field parity with the
	// equivalent OpenAPI 3 spec is enforced by the conversion library's own
	// test suite.
	swagger2 := []byte(`{
  "swagger": "2.0",
  "info": {"title": "Shape Demo", "version": "1.0.0"},
  "host": "api.example.com",
  "basePath": "/v1",
  "schemes": ["https"],
  "paths": {
    "/users": {
      "get": {
        "operationId": "listUsers",
        "responses": {"200": {"description": "ok"}}
      }
    }
  }
}`)

	parsed, err := Parse(swagger2)
	require.NoError(t, err)
	require.NotNil(t, parsed)
	assert.Equal(t, "https://api.example.com/v1", parsed.BaseURL)

	endpoint := findParsedEndpointByPath(t, parsed, "GET", "/users")
	require.NotNil(t, endpoint)
}
