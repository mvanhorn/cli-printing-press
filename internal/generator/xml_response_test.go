package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGenerateXMLResponseParseHelper(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("xml-response")
	apiSpec.Resources["things"] = spec.Resource{
		Description: "Things",
		Endpoints: map[string]spec.Endpoint{
			"get": {
				Method:         "GET",
				Path:           "/thing/{id}",
				Description:    "Get a thing",
				ResponseFormat: spec.ResponseFormatXML,
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "xml-response-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	helper, err := os.ReadFile(filepath.Join(outputDir, "internal", "cliutil", "xml_parse.go"))
	require.NoError(t, err)
	require.Contains(t, string(helper), `func XMLToJSON(raw json.RawMessage) json.RawMessage`)

	testSrc := []byte(`package cliutil

import (
	"encoding/json"
	"reflect"
	"testing"
)

func decode(t *testing.T, in string) map[string]any {
	t.Helper()
	out := XMLToJSON(json.RawMessage(in))
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("result is not valid JSON: %v (%s)", err, string(out))
	}
	return got
}

func TestXMLToJSONAttributesAndNesting(t *testing.T) {
	got := decode(t, ` + "`" + `<items total="1"><item id="13"><name value="Catan"/></item></items>` + "`" + `)
	want := map[string]any{"items": map[string]any{
		"@total": "1",
		"item": map[string]any{
			"@id":  "13",
			"name": map[string]any{"@value": "Catan"},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("attrs/nesting mismatch:\n got=%#v\nwant=%#v", got, want)
	}
}

func TestXMLToJSONRepeatedSiblingsBecomeArray(t *testing.T) {
	got := decode(t, ` + "`" + `<items><item id="1"/><item id="2"/></items>` + "`" + `)
	items, ok := got["items"].(map[string]any)
	if !ok {
		t.Fatalf("items not a map: %#v", got["items"])
	}
	arr, ok := items["item"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("item should be a 2-element array: %#v", items["item"])
	}
}

func TestXMLToJSONTextAndMixedContent(t *testing.T) {
	got := decode(t, ` + "`" + `<root><message>hello</message><name type="primary">Catan</name></root>` + "`" + `)
	root := got["root"].(map[string]any)
	if root["message"] != "hello" {
		t.Fatalf("text element should be a bare string: %#v", root["message"])
	}
	name := root["name"].(map[string]any)
	if name["@type"] != "primary" || name["#text"] != "Catan" {
		t.Fatalf("mixed attr+text mismatch: %#v", name)
	}
}

func TestXMLToJSONMalformedPassesThrough(t *testing.T) {
	in := "not xml at all"
	out := XMLToJSON(json.RawMessage(in))
	if string(out) != in {
		t.Fatalf("malformed input should pass through unchanged, got %q", string(out))
	}
}
`)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cliutil", "xml_parse_extra_test.go"), testSrc, 0o600))
	runGoCommand(t, outputDir, "test", "./internal/cliutil/...")
}

func TestGenerateJSONOnlyOmitsXMLResponseParseHelper(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("json-only-xml")
	outputDir := filepath.Join(t.TempDir(), "json-only-xml-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	_, err := os.Stat(filepath.Join(outputDir, "internal", "cliutil", "xml_parse.go"))
	require.True(t, os.IsNotExist(err), "JSON-only CLIs should not emit xml_parse.go")
}

func TestGenerateMixedXMLJSONScopesNormalizationToXMLRequests(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mixed-xml-json")
	apiSpec.Resources["things"] = spec.Resource{
		Description: "Things",
		Endpoints: map[string]spec.Endpoint{
			"get": {
				Method:         "GET",
				Path:           "/thing/{id}",
				Description:    "Get a thing",
				ResponseFormat: spec.ResponseFormatXML,
			},
		},
	}
	require.True(t, apiSpec.HasXMLResponse(), "fixture should have an XML endpoint")

	outputDir := filepath.Join(t.TempDir(), "mixed-xml-json-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	_, err := os.Stat(filepath.Join(outputDir, "internal", "cliutil", "xml_parse.go"))
	require.NoError(t, err, "mixed XML/JSON specs need the XML normalizer")

	client, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	require.Contains(t, string(client), `isXMLRequest(req.Header.Get("Accept")) && isXMLResponseContentType`,
		"mixed specs should scope normalization to each XML request")
	require.NotContains(t, string(client), `req.Header.Set("Accept", "application/xml")`,
		"mixed specs should keep the application/json Accept default")
}
