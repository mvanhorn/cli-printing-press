package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

func detectNestedDataEnvelopeFixtures(data []byte) map[string]nestedDataEnvelopeFixture {
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

func detectNestedDataEnvelopeFixturesFromRaw(raw map[string]any) map[string]nestedDataEnvelopeFixture {
	paths, ok := raw["paths"].(map[string]any)
	if !ok {
		return nil
	}

	fixtures := map[string]nestedDataEnvelopeFixture{}
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
			if key := nestedDataArrayKey(schema, raw); key != "" {
				fixtures[path] = nestedDataEnvelopeFixture{ArrayKey: key}
				break
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
