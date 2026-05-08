package openapi

import (
	"encoding/json"
	"fmt"
	"strings"

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
	var root any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("normalize spec: yaml unmarshal: %w", err)
	}
	root = convertToStringKeyed(root)
	flattenObjectDescriptions(root, "")
	normalizeSwagger2Shape(root)
	out, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("normalize spec: json marshal: %w", err)
	}
	return out, nil
}

func normalizeSwagger2Shape(root any) {
	doc, ok := root.(map[string]any)
	if !ok {
		return
	}
	if fmt.Sprint(doc["swagger"]) != "2.0" {
		return
	}
	normalizeSwagger2Servers(doc)
	docProduces := swagger2Produces(doc)
	docConsumes := swagger2Consumes(doc)
	paths, _ := doc["paths"].(map[string]any)
	for _, pathItemRaw := range paths {
		pathItem, ok := pathItemRaw.(map[string]any)
		if !ok {
			continue
		}
		normalizeSwagger2ParameterList(pathItem["parameters"])
		for _, method := range []string{"get", "put", "post", "delete", "patch", "head", "options"} {
			op, ok := pathItem[method].(map[string]any)
			if !ok {
				continue
			}
			normalizeSwagger2ParameterList(op["parameters"])
			consumes := swagger2Consumes(op)
			if len(consumes) == 0 {
				consumes = docConsumes
			}
			normalizeSwagger2RequestBody(op, consumes)
			produces := swagger2Produces(op)
			if len(produces) == 0 {
				produces = docProduces
			}
			if len(produces) > 0 {
				op[extensionPPProduces] = produces
			}
			normalizeSwagger2Responses(op["responses"], produces)
		}
	}
}

func normalizeSwagger2Servers(doc map[string]any) {
	if _, ok := doc["servers"]; ok {
		return
	}
	host, _ := doc["host"].(string)
	if host == "" {
		return
	}
	scheme := "https"
	if schemes, ok := doc["schemes"].([]any); ok {
		for _, candidate := range schemes {
			if fmt.Sprint(candidate) == "https" {
				scheme = "https"
				break
			}
			if fmt.Sprint(candidate) == "http" {
				scheme = "http"
			}
		}
	}
	basePath, _ := doc["basePath"].(string)
	doc["servers"] = []any{map[string]any{"url": scheme + "://" + host + basePath}}
}

func normalizeSwagger2ParameterList(raw any) {
	params, ok := raw.([]any)
	if !ok {
		return
	}
	for _, paramRaw := range params {
		param, ok := paramRaw.(map[string]any)
		if !ok {
			continue
		}
		if _, hasSchema := param["schema"]; hasSchema {
			continue
		}
		typ, _ := param["type"].(string)
		if typ == "" {
			continue
		}
		schema := map[string]any{"type": typ}
		for _, key := range []string{"format", "items", "enum", "default"} {
			if value, ok := param[key]; ok {
				schema[key] = value
			}
		}
		param["schema"] = schema
	}
}

func normalizeSwagger2RequestBody(op map[string]any, consumes []string) {
	if _, ok := op["requestBody"]; ok {
		return
	}
	params, ok := op["parameters"].([]any)
	if !ok {
		return
	}

	filtered := make([]any, 0, len(params))
	var bodyParam map[string]any
	for _, paramRaw := range params {
		param, ok := paramRaw.(map[string]any)
		if !ok || fmt.Sprint(param["in"]) != "body" {
			filtered = append(filtered, paramRaw)
			continue
		}
		if bodyParam == nil {
			bodyParam = param
		}
	}
	if bodyParam == nil {
		return
	}
	if len(filtered) == 0 {
		delete(op, "parameters")
	} else {
		op["parameters"] = filtered
	}

	schema, ok := bodyParam["schema"]
	if !ok {
		return
	}
	contentTypes := firstJSONConsumes(consumes)
	if len(contentTypes) == 0 {
		contentTypes = []string{"application/json"}
	}
	content := make(map[string]any, len(contentTypes))
	for _, contentType := range contentTypes {
		content[contentType] = map[string]any{"schema": schema}
	}
	requestBody := map[string]any{"content": content}
	if required, ok := bodyParam["required"].(bool); ok {
		requestBody["required"] = required
	}
	if description := fmt.Sprint(bodyParam["description"]); description != "" && description != "<nil>" {
		requestBody["description"] = description
	}
	op["requestBody"] = requestBody
}

func normalizeSwagger2Responses(raw any, produces []string) {
	responses, ok := raw.(map[string]any)
	if !ok {
		return
	}
	for _, responseRaw := range responses {
		response, ok := responseRaw.(map[string]any)
		if !ok {
			continue
		}
		if _, hasContent := response["content"]; hasContent {
			continue
		}
		schema, hasSchema := response["schema"]
		contentTypes := produces
		if len(contentTypes) == 0 && hasSchema {
			contentTypes = []string{"application/json"}
		}
		if len(contentTypes) == 0 {
			continue
		}
		content := make(map[string]any, len(contentTypes))
		for _, contentType := range contentTypes {
			contentType = fmt.Sprint(contentType)
			if contentType == "" {
				continue
			}
			mediaType := map[string]any{}
			if hasSchema {
				mediaType["schema"] = normalizeSwagger2ResponseSchema(schema)
			}
			content[contentType] = mediaType
		}
		if len(content) > 0 {
			response["content"] = content
		}
	}
}

func swagger2Produces(obj map[string]any) []string {
	raw, ok := obj["produces"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s := fmt.Sprint(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func swagger2Consumes(obj map[string]any) []string {
	raw, ok := obj["consumes"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s := fmt.Sprint(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstJSONConsumes(consumes []string) []string {
	if len(consumes) == 0 {
		return nil
	}
	for _, consumesType := range consumes {
		if isJSONMediaType(consumesType) {
			return []string{consumesType}
		}
	}
	return nil
}

func isJSONMediaType(mediaType string) bool {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if i := strings.Index(mediaType, ";"); i >= 0 {
		mediaType = strings.TrimSpace(mediaType[:i])
	}
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func normalizeSwagger2ResponseSchema(schema any) any {
	m, ok := schema.(map[string]any)
	if !ok || fmt.Sprint(m["type"]) != "file" {
		return schema
	}
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	out["type"] = "string"
	if _, ok := out["format"]; !ok {
		out["format"] = "binary"
	}
	return out
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

// flattenObjectDescriptions walks the decoded spec tree and replaces any
// `description` key whose value is a map or slice with an empty string. Scalar
// descriptions (the common case) pass through untouched.
//
// Skips the flatten when the immediate parent key is one whose children are
// user-named entries (Schema property names, response codes, pattern regexes,
// component-section names). In those positions a "description" key is the
// caller's chosen name for an entry, not the structural OpenAPI documentation
// field. Stytch has a schema with `properties: { description: { type: string,
// ... } }` that hits this case; flattening there would replace the entry's
// schema with an empty string and produce
// `cannot unmarshal string into field Schema.properties of type openapi3.Schema`.
func flattenObjectDescriptions(node any, parentKey string) {
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
		for key, value := range v {
			flattenObjectDescriptions(value, key)
		}
	case []any:
		for _, item := range v {
			flattenObjectDescriptions(item, "")
		}
	}
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
