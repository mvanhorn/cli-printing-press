package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

func detectNestedDataEnvelopeFixtures(data []byte) map[nestedDataEnvelopeFixtureKey]nestedDataEnvelopeFixture {
	raw, err := decodeOpenAPIRaw(data)
	if err != nil {
		return nil
	}
	return detectNestedDataEnvelopeFixturesFromRaw(raw)
}

func decodeOpenAPIRaw(data []byte) (map[string]any, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty spec")
	}
	var raw map[string]any
	if trimmed[0] == '{' {
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		return raw, nil
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func detectNestedDataEnvelopeFixturesFromRaw(raw map[string]any) map[nestedDataEnvelopeFixtureKey]nestedDataEnvelopeFixture {
	paths, ok := raw["paths"].(map[string]any)
	if !ok {
		return nil
	}

	fixtures := map[nestedDataEnvelopeFixtureKey]nestedDataEnvelopeFixture{}
	pathNames := make([]string, 0, len(paths))
	for path := range paths {
		pathNames = append(pathNames, path)
	}
	slices.Sort(pathNames)
	for _, path := range pathNames {
		pathItem, ok := paths[path].(map[string]any)
		if !ok {
			continue
		}
		methods := make([]string, 0, len(pathItem))
		for method := range pathItem {
			if !isDogfoodHTTPMethod(method) {
				continue
			}
			methods = append(methods, method)
		}
		slices.Sort(methods)
		for _, method := range methods {
			operationValue, ok := pathItem[method]
			if !ok {
				continue
			}
			operation, ok := operationValue.(map[string]any)
			if !ok {
				continue
			}
			schema := selectedResponseSchemaFromRaw(operation, raw)
			if fixture, ok := nestedDataEnvelopeFixtureForSchema(schema, raw); ok {
				fixtures[nestedDataEnvelopeFixtureKey{Method: strings.ToUpper(method), Path: path}] = fixture
			}
		}
	}
	if len(fixtures) == 0 {
		return nil
	}
	return fixtures
}

func isDogfoodHTTPMethod(method string) bool {
	switch strings.ToLower(method) {
	case "get", "post", "put", "patch", "delete", "head", "options", "trace":
		return true
	default:
		return false
	}
}

func selectedResponseSchemaFromRaw(operation map[string]any, root map[string]any) map[string]any {
	responses, ok := operation["responses"].(map[string]any)
	if !ok {
		return nil
	}
	for _, status := range sortedSuccessStatuses(responses) {
		response, ok := responses[status].(map[string]any)
		if !ok {
			continue
		}
		response = resolveRawSchemaRef(response, root)
		content, ok := response["content"].(map[string]any)
		if !ok {
			continue
		}
		contentTypes := make([]string, 0, len(content))
		for contentType := range content {
			if isJSONMediaType(contentType) {
				contentTypes = append(contentTypes, contentType)
			}
		}
		slices.Sort(contentTypes)
		for _, contentType := range contentTypes {
			media, ok := content[contentType].(map[string]any)
			if !ok {
				continue
			}
			schema, _ := media["schema"].(map[string]any)
			if len(schema) > 0 {
				return schema
			}
		}
	}
	return nil
}

func isJSONMediaType(mediaType string) bool {
	mediaType = strings.ToLower(strings.TrimSpace(strings.Split(mediaType, ";")[0]))
	return mediaType == "application/json" ||
		mediaType == "application/problem+json" ||
		strings.HasSuffix(mediaType, "+json")
}

func sortedSuccessStatuses(responses map[string]any) []string {
	statuses := make([]string, 0, len(responses))
	for status := range responses {
		if status == "default" || strings.HasPrefix(status, "2") {
			statuses = append(statuses, status)
		}
	}
	slices.Sort(statuses)
	return statuses
}

func nestedDataEnvelopeFixtureForSchema(schema map[string]any, root map[string]any) (nestedDataEnvelopeFixture, bool) {
	schema = resolveRawSchemaRef(schema, root)
	if schemaType(schema) != "object" {
		return nestedDataEnvelopeFixture{}, false
	}
	if fixture, ok := topLevelArrayEnvelopeFixture(schema, root); ok {
		return fixture, true
	}

	// Preserve the previously supported {data: {items: [...]}} shape. It is
	// less strict because its sibling metadata can itself be an object.
	dataSchema := schemaProperty(schema, "data")
	if len(dataSchema) == 0 {
		return nestedDataEnvelopeFixture{}, false
	}
	dataSchema = resolveRawSchemaRef(dataSchema, root)
	if schemaType(dataSchema) != "object" {
		return nestedDataEnvelopeFixture{}, false
	}
	properties, ok := dataSchema["properties"].(map[string]any)
	if !ok {
		return nestedDataEnvelopeFixture{}, false
	}
	for _, key := range []string{"items", "results", "records", "nodes", "entries", "values"} {
		if prop := schemaProperty(dataSchema, key); isRawArraySchema(prop, root) {
			return nestedDataEnvelopeFixture{ArrayKey: key}, true
		}
	}
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if isRawArraySchema(schemaProperty(dataSchema, key), root) {
			return nestedDataEnvelopeFixture{ArrayKey: key}, true
		}
	}
	return nestedDataEnvelopeFixture{}, false
}

func topLevelArrayEnvelopeFixture(schema map[string]any, root map[string]any) (nestedDataEnvelopeFixture, bool) {
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) < 2 {
		return nestedDataEnvelopeFixture{}, false
	}

	fixture := nestedDataEnvelopeFixture{Scalars: make(map[string]any, len(properties)-1)}
	for key, value := range properties {
		property, ok := value.(map[string]any)
		if !ok {
			return nestedDataEnvelopeFixture{}, false
		}
		if isRawObjectArraySchema(property, root) {
			if fixture.ArrayKey != "" {
				return nestedDataEnvelopeFixture{}, false
			}
			fixture.ArrayKey = key
			continue
		}
		if !isRawScalarSchema(property, root) {
			return nestedDataEnvelopeFixture{}, false
		}
		fixture.Scalars[key] = mockScalarValue(key, property, root)
	}
	if fixture.ArrayKey == "" || len(fixture.Scalars) == 0 {
		return nestedDataEnvelopeFixture{}, false
	}
	return fixture, true
}

func schemaProperty(schema map[string]any, key string) map[string]any {
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil
	}
	prop, _ := properties[key].(map[string]any)
	return prop
}

func isRawArraySchema(schema map[string]any, root map[string]any) bool {
	schema = resolveRawSchemaRef(schema, root)
	return schemaType(schema) == "array"
}

func isRawObjectArraySchema(schema map[string]any, root map[string]any) bool {
	schema = resolveRawSchemaRef(schema, root)
	if schemaType(schema) != "array" {
		return false
	}
	items, _ := schema["items"].(map[string]any)
	items = resolveRawSchemaRef(items, root)
	return schemaType(items) == "object"
}

func isRawScalarSchema(schema map[string]any, root map[string]any) bool {
	schema = resolveRawSchemaRef(schema, root)
	switch schemaType(schema) {
	case "boolean", "integer", "number", "string":
		return true
	default:
		return false
	}
}

func mockScalarValue(key string, schema map[string]any, root map[string]any) any {
	schema = resolveRawSchemaRef(schema, root)
	if isContinuationScalarKey(key) {
		// A mock must terminate pagination even when the schema's default would
		// advertise another page. The verifier's sync probe follows these values.
		switch schemaType(schema) {
		case "boolean":
			return false
		case "integer", "number":
			return nil
		case "string":
			return ""
		}
	}
	if value, ok := schema["default"]; ok {
		return value
	}
	switch schemaType(schema) {
	case "boolean":
		return true
	case "integer", "number":
		return 2
	case "string":
		return "mock"
	default:
		return nil
	}
}

func isContinuationScalarKey(key string) bool {
	key = snakeCaseKey(key)
	if strings.Contains(key, "cursor") || strings.Contains(key, "token") || strings.Contains(key, "continuation") {
		return true
	}
	switch key {
	case "more", "has_more", "has_next", "has_next_page", "next", "next_page", "after", "starting_after", "truncated", "is_truncated":
		return true
	default:
		return strings.HasPrefix(key, "next_")
	}
}

// snakeCaseKey folds camelCase, PascalCase, kebab-case, and dotted keys to
// lower snake_case so hasMore, HasMore, and has-more compare equal.
func snakeCaseKey(key string) string {
	var b strings.Builder
	runes := []rune(key)
	for i, r := range runes {
		switch {
		case r == '-' || r == ' ' || r == '.':
			b.WriteByte('_')
		case unicode.IsUpper(r):
			prevLower := i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]))
			nextLower := i > 0 && i+1 < len(runes) && unicode.IsUpper(runes[i-1]) && unicode.IsLower(runes[i+1])
			if prevLower || nextLower {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "_")
}

func schemaType(schema map[string]any) string {
	if schema == nil {
		return ""
	}
	switch value := schema["type"].(type) {
	case string:
		return value
	case []any:
		for _, item := range value {
			if str, ok := item.(string); ok && str != "null" {
				return str
			}
		}
	}
	if _, ok := schema["properties"].(map[string]any); ok {
		return "object"
	}
	if _, ok := schema["items"].(map[string]any); ok {
		return "array"
	}
	return ""
}

func resolveRawSchemaRef(schema map[string]any, root map[string]any) map[string]any {
	for range 8 {
		ref, _ := schema["$ref"].(string)
		if ref != "" {
			resolved := resolveRawPointer(ref, root)
			if resolved == nil {
				return schema
			}
			schema = resolved
			continue
		}
		if nested := singleCompositionSchema(schema); nested != nil {
			schema = nested
			continue
		}
		return schema
	}
	return schema
}

func resolveRawPointer(ref string, root map[string]any) map[string]any {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	cur := any(root)
	for part := range strings.SplitSeq(strings.TrimPrefix(ref, "#/"), "/") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		next, ok := obj[part]
		if !ok {
			return nil
		}
		cur = next
	}
	resolved, ok := cur.(map[string]any)
	if !ok {
		return nil
	}
	return resolved
}

func singleCompositionSchema(schema map[string]any) map[string]any {
	for _, key := range []string{"allOf", "oneOf", "anyOf"} {
		items, ok := schema[key].([]any)
		if !ok || len(items) != 1 {
			continue
		}
		nested, _ := items[0].(map[string]any)
		return nested
	}
	return nil
}
