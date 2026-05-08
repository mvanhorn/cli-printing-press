package openapi

import (
	"encoding/json"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// normalizeSpecData parses raw OpenAPI spec bytes (JSON or YAML), rewrites any
// non-scalar `description` field to an empty string, and re-emits the result as
// JSON. Returning JSON consistently lets kin-openapi's JSON-first unmarshal
// path succeed without falling through to its YAML fallback, which collapses
// the `failed to unmarshal data: json error: ..., yaml error: ...` combined
// error messages into single-line errors when something further downstream
// fails.
//
// The flatten step exists because some vendors (DigitalOcean's public spec is
// the canonical example) emit `description: { $ref: "description.yml#/foo" }`
// pointing at an external markdown bundle instead of the inline string the
// OpenAPI 3.0 specification mandates. kin-openapi's `Tag.Description`,
// `Info.Description`, and similar fields are typed as `string`, so the
// non-string value triggers `cannot unmarshal object into field X of type
// string` and the spec refuses to load.
//
// Descriptions are documentation, not load-bearing for code generation;
// dropping the text is the right trade-off for keeping the spec parseable.
// If the input data is not parseable as YAML or JSON, the function returns an
// error and callers fall back to the original bytes — which preserves
// kin-openapi's existing error reporting for genuinely malformed input.
func normalizeSpecData(data []byte) ([]byte, error) {
	normalized, _, err := normalizeSpecDataWithMetadata(data)
	return normalized, err
}

type specDataMetadata struct {
	explicitEmptySecuritySchemes bool
}

func normalizeSpecDataWithMetadata(data []byte) ([]byte, specDataMetadata, error) {
	root, err := decodeSpecTree(data)
	if err != nil {
		return nil, specDataMetadata{}, err
	}
	metadata := specDataMetadata{
		explicitEmptySecuritySchemes: treeHasExplicitEmptySecuritySchemes(root),
	}
	normalizeSpecTree(root, "", false)
	out, err := json.Marshal(root)
	if err != nil {
		return nil, specDataMetadata{}, fmt.Errorf("normalize spec: json marshal: %w", err)
	}
	return out, metadata, nil
}

func decodeSpecTree(data []byte) (any, error) {
	var root any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("normalize spec: yaml unmarshal: %w", err)
	}
	return convertToStringKeyed(root), nil
}

func treeHasExplicitEmptySecuritySchemes(root any) bool {
	rootMap, ok := root.(map[string]any)
	if !ok {
		return false
	}
	components, ok := rootMap["components"].(map[string]any)
	if !ok {
		return false
	}
	raw, ok := components["securitySchemes"]
	if !ok {
		return false
	}
	schemes, ok := raw.(map[string]any)
	return ok && len(schemes) == 0
}

// convertToStringKeyed walks a value decoded by gopkg.in/yaml.v3 and rewrites
// any map[any]any nodes (which YAML produces when keys are non-strings) into
// map[string]any. JSON marshaling rejects map[any]any, so this conversion
// is a prerequisite for round-tripping through encoding/json. Non-string keys
// are coerced via fmt.Sprint, which is acceptable for OpenAPI specs where
// non-string keys are extremely rare and treated as authoring mistakes.
func convertToStringKeyed(node any) any {
	switch v := node.(type) {
	case map[any]any:
		out := make(map[string]any, len(v))
		for key, value := range v {
			out[fmt.Sprint(key)] = convertToStringKeyed(value)
		}
		return out
	case map[string]any:
		for key, value := range v {
			v[key] = convertToStringKeyed(value)
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = convertToStringKeyed(item)
		}
		return v
	default:
		return node
	}
}

// normalizeSpecTree walks the decoded spec tree and applies tolerant rewrites
// kin-openapi needs before loading real-world specs. It replaces non-scalar
// `description` fields with an empty string and normalizes schema `examples`
// values to the array shape OpenAPI expects.
//
// Skips the flatten when the immediate parent key is one whose children are
// user-named entries (Schema property names, response codes, pattern regexes,
// component-section names). In those positions a "description" key is the
// caller's chosen name for an entry, not the structural OpenAPI documentation
// field. Stytch has a schema with `properties: { description: { type: string,
// ... } }` that hits this case; flattening there would replace the entry's
// schema with an empty string and produce
// `cannot unmarshal string into field Schema.properties of type openapi3.Schema`.
func normalizeSpecTree(node any, parentKey string, inSchema bool) {
	switch v := node.(type) {
	case map[string]any:
		if !isNameKeyedParent(parentKey) {
			if desc, ok := v["description"]; ok {
				switch desc.(type) {
				case map[string]any, []any:
					v["description"] = ""
				}
			}
		}
		if inSchema {
			if examples, ok := v["examples"]; ok {
				v["examples"] = normalizeExamplesValue(examples)
			}
		}
		for key, value := range v {
			normalizeSpecTree(value, key, childIsSchema(parentKey, key))
		}
	case []any:
		for _, item := range v {
			normalizeSpecTree(item, "", schemaArrayItems(parentKey))
		}
	}
}

func normalizeExamplesValue(examples any) any {
	switch v := examples.(type) {
	case []any:
		return v
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make([]any, 0, len(keys))
		for _, key := range keys {
			out = append(out, v[key])
		}
		return out
	default:
		return []any{v}
	}
}

func childIsSchema(parentKey, key string) bool {
	switch parentKey {
	case "schemas", "definitions", "properties", "patternProperties":
		return true
	}
	switch key {
	case "schema", "items", "additionalProperties", "not":
		return true
	}
	return false
}

func schemaArrayItems(parentKey string) bool {
	switch parentKey {
	case "allOf", "oneOf", "anyOf":
		return true
	}
	return false
}

// isNameKeyedParent reports whether children of the given parent key are
// user-defined names rather than OpenAPI structural fields. Inside these
// containers a "description" key is the caller's chosen name and must not be
// rewritten as if it were the structural documentation field. Drawn from the
// OpenAPI 3.0 spec's section names plus JSON Schema's `properties` family.
func isNameKeyedParent(parentKey string) bool {
	switch parentKey {
	case "properties", "patternProperties", "definitions",
		"schemas", "parameters", "requestBodies", "responses",
		"headers", "securitySchemes", "links", "callbacks",
		"examples", "pathItems", "encoding", "content", "paths":
		return true
	}
	return false
}
