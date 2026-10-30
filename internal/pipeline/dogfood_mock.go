package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

func detectNestedDataEnvelopeFixtures(data []byte) map[responseFixtureKey]nestedDataEnvelopeFixture {
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

func detectNestedDataEnvelopeFixturesFromRaw(raw map[string]any) map[responseFixtureKey]nestedDataEnvelopeFixture {
	paths, ok := raw["paths"].(map[string]any)
	if !ok {
		return nil
	}

	fixtures := map[responseFixtureKey]nestedDataEnvelopeFixture{}
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
			fixture, ok := responseFixtureForSchema(schema, raw)
			if !ok {
				continue
			}
			fixtures[responseFixtureKey{Method: strings.ToUpper(method), Path: path}] = fixture
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

func responseFixtureForSchema(schema map[string]any, root map[string]any) (nestedDataEnvelopeFixture, bool) {
	if len(schema) == 0 {
		return nestedDataEnvelopeFixture{}, false
	}
	schema = mergedObjectSchema(schema, root, 0)
	if schemaType(schema) != "object" {
		return nestedDataEnvelopeFixture{}, false
	}
	if fixture, ok := singleArrayEnvelopeFixture(schema, root); ok {
		return fixture, true
	}
	if schemaHasArrayProperty(schema, root) {
		return nestedDataEnvelopeFixture{}, false
	}
	if key := nestedDataArrayKey(schema, root); key != "" {
		return nestedDataEnvelopeFixture{Kind: responseFixtureNestedDataObject, ArrayKey: key}, true
	}
	return nestedDataEnvelopeFixture{}, false
}

func singleArrayEnvelopeFixture(schema map[string]any, root map[string]any) (nestedDataEnvelopeFixture, bool) {
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) < 2 {
		return nestedDataEnvelopeFixture{}, false
	}
	var arrayKey string
	scalars := make(map[string]any, len(properties)-1)
	for _, key := range sortedRawKeys(properties) {
		property, ok := properties[key].(map[string]any)
		if !ok {
			return nestedDataEnvelopeFixture{}, false
		}
		switch {
		case isRawObjectArraySchema(property, root):
			if arrayKey != "" {
				return nestedDataEnvelopeFixture{}, false
			}
			arrayKey = key
		case isRawScalarSchema(property, root):
			scalars[key] = plausibleScalarValue(key, property, root)
		default:
			return nestedDataEnvelopeFixture{}, false
		}
	}
	if arrayKey == "" || len(scalars) == 0 {
		return nestedDataEnvelopeFixture{}, false
	}
	kind := responseFixtureTopLevel
	if arrayKey == "data" {
		kind = responseFixtureDataArray
	}
	return nestedDataEnvelopeFixture{Kind: kind, ArrayKey: arrayKey, Scalars: scalars}, true
}

func schemaHasArrayProperty(schema map[string]any, root map[string]any) bool {
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return false
	}
	for _, key := range sortedRawKeys(properties) {
		property, ok := properties[key].(map[string]any)
		if ok && isRawArraySchema(property, root) {
			return true
		}
	}
	return false
}

func nestedDataArrayKey(schema map[string]any, root map[string]any) string {
	schema = resolveRawSchemaRef(schema, root)
	if schemaType(schema) != "object" {
		return ""
	}
	dataSchema := schemaProperty(schema, "data")
	if len(dataSchema) == 0 {
		return ""
	}
	dataSchema = resolveRawSchemaRef(dataSchema, root)
	if schemaType(dataSchema) != "object" {
		return ""
	}
	properties, ok := dataSchema["properties"].(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"items", "results", "records", "nodes", "entries", "values"} {
		if prop := schemaProperty(dataSchema, key); isRawArraySchema(prop, root) {
			return key
		}
	}
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if isRawArraySchema(schemaProperty(dataSchema, key), root) {
			return key
		}
	}
	return ""
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
	if len(items) == 0 {
		return false
	}
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

func mergedObjectSchema(schema map[string]any, root map[string]any, depth int) map[string]any {
	schema = resolveRawSchemaRef(schema, root)
	parts, ok := schema["allOf"].([]any)
	if !ok || len(parts) < 2 || depth > 8 {
		return schema
	}
	properties := map[string]any{}
	if own, ok := schema["properties"].(map[string]any); ok {
		maps.Copy(properties, own)
	}
	merged := false
	for _, part := range parts {
		sub, ok := part.(map[string]any)
		if !ok {
			continue
		}
		sub = mergedObjectSchema(sub, root, depth+1)
		subProperties, ok := sub["properties"].(map[string]any)
		if !ok {
			continue
		}
		merged = true
		maps.Copy(properties, subProperties)
	}
	if !merged {
		return schema
	}
	out := map[string]any{"type": "object", "properties": properties}
	if add, ok := schema["additionalProperties"]; ok {
		out["additionalProperties"] = add
	}
	return out
}

func sortedRawKeys(properties map[string]any) []string {
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// A mock page is the whole result. Continuation scalars that would ask for
// another page are terminal so verify and sync do not walk the same page
// forever, and a success flag stays true because the mock answered HTTP 200.
func plausibleScalarValue(key string, schema map[string]any, root map[string]any) any {
	schema = resolveRawSchemaRef(schema, root)
	typ := schemaType(schema)
	if typ == "boolean" && isSuccessScalarKey(key) {
		return true
	}
	if isContinuationScalarKey(key) {
		switch typ {
		case "boolean":
			return false
		case "integer", "number":
			return nil
		case "string":
			return ""
		}
	}
	if value, ok := schema["default"]; ok && !isFailureStatusValue(value) {
		return value
	}
	if enums, ok := schema["enum"].([]any); ok {
		for _, value := range enums {
			if !isFailureStatusValue(value) {
				return value
			}
		}
	}
	switch typ {
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

func isSuccessScalarKey(key string) bool {
	switch envelopePropertyKey(key) {
	case "success", "ok", "succeeded", "is_success":
		return true
	default:
		return false
	}
}

func isContinuationScalarKey(key string) bool {
	key = envelopePropertyKey(key)
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

func isFailureStatusValue(value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	switch strings.ToLower(text) {
	case "error", "fail", "failed":
		return true
	default:
		return false
	}
}

func envelopePropertyKey(key string) string {
	var b strings.Builder
	var prev rune
	lastUnderscore := true
	for i, r := range key {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if unicode.IsUpper(r) && i > 0 && (unicode.IsLower(prev) || unicode.IsDigit(prev)) && !lastUnderscore {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			lastUnderscore = false
		} else if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
		prev = r
	}
	return strings.Trim(b.String(), "_")
}

func renderResponseFixture(fixture nestedDataEnvelopeFixture) string {
	switch fixture.Kind {
	case responseFixtureTopLevel, responseFixtureDataArray:
		return renderScalarSiblingEnvelope(fixture)
	default:
		return renderNestedDataObjectEnvelope(fixture)
	}
}

func renderScalarSiblingEnvelope(fixture nestedDataEnvelopeFixture) string {
	body := make(map[string]any, len(fixture.Scalars)+1)
	for key, value := range fixture.Scalars {
		body[key] = value
	}
	key := fixture.ArrayKey
	if key == "" {
		key = "items"
	}
	body[key] = mockListItems()
	return marshalMockBody(body, `{"items":[{"id":1,"name":"mock-item-1"},{"id":2,"name":"mock-item-2"}]}`)
}

func renderNestedDataObjectEnvelope(fixture nestedDataEnvelopeFixture) string {
	arrayKey := fixture.ArrayKey
	if arrayKey == "" {
		arrayKey = "items"
	}
	body := map[string]any{
		"success": true,
		"data": map[string]any{
			arrayKey:     mockListItems(),
			"pagination": map[string]any{"total": 2},
		},
	}
	return marshalMockBody(body, `{"success":true,"data":{"items":[{"id":1,"name":"mock-item-1"},{"id":2,"name":"mock-item-2"}],"pagination":{"total":2}}}`)
}

func marshalMockBody(body any, fallback string) string {
	data, err := json.Marshal(body)
	if err != nil {
		return fallback
	}
	return string(data)
}

func mockListItems() []map[string]any {
	return []map[string]any{
		{"id": 1, "name": "mock-item-1", "state": "open", "title": "Mock Item", "created_at": "2026-03-27T00:00:00Z", "updated_at": "2026-03-27T00:00:00Z"},
		{"id": 2, "name": "mock-item-2", "state": "open", "title": "Mock Item 2", "created_at": "2026-03-27T00:00:00Z", "updated_at": "2026-03-27T00:00:00Z"},
	}
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
