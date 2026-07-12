// Package googlediscovery converts Google Discovery format REST descriptions
// (https://googleapis.github.io/discovery/v1/apis) into the internal APISpec
// that the printing-press generator consumes. The converter handles the 80%
// case: resource/method traversal, path+query parameters, request/response
// schema $ref resolution, and OAuth2 bearer auth detection.
//
// Known gaps (documented, not blocking):
//   - Media upload endpoints (resumable + multipart) are skipped; their
//     protocol differs significantly from JSON REST and OpenAPI cannot
//     represent the two-URL upload negotiation cleanly.
//   - Enum schema types with complex "enumDescriptions" get flattened to
//     string with the values preserved in the Enum slice.
//   - Deeply nested anonymous schemas are inlined rather than promoted to
//     named types; the generator handles anonymous objects via Fields.
package googlediscovery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/discovery"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// IsDiscovery reports whether the byte slice looks like a Google Discovery doc.
func IsDiscovery(data []byte) bool {
	return bytes.Contains(data, []byte(`"discovery#restDescription"`)) ||
		bytes.Contains(data, []byte(`"discoveryVersion"`))
}

// discDoc is the minimal shape of a Google Discovery REST description we need
// to convert. Only the fields the converter actually reads are declared.
type discDoc struct {
	Name        string                  `json:"name"`
	Title       string                  `json:"title"`
	Description string                  `json:"description"`
	Version     string                  `json:"version"`
	RootURL     string                  `json:"rootUrl"`
	BasePath    string                  `json:"basePath"`
	BaseURL     string                  `json:"baseUrl"`
	Auth        discAuth                `json:"auth"`
	Parameters  map[string]discParam    `json:"parameters"`
	Schemas     map[string]discSchema   `json:"schemas"`
	Resources   map[string]discResource `json:"resources"`
}

type discAuth struct {
	OAuth2 *discOAuth2 `json:"oauth2"`
}

type discOAuth2 struct {
	Scopes map[string]struct {
		Description string `json:"description"`
	} `json:"scopes"`
}

type discResource struct {
	Methods   map[string]discMethod   `json:"methods"`
	Resources map[string]discResource `json:"resources"`
}

type discMethod struct {
	ID                  string               `json:"id"`
	Path                string               `json:"path"`
	FlatPath            string               `json:"flatPath"`
	HTTPMethod          string               `json:"httpMethod"`
	Description         string               `json:"description"`
	Parameters          map[string]discParam `json:"parameters"`
	ParameterOrder      []string             `json:"parameterOrder"`
	Request             *discRef             `json:"request"`
	Response            *discRef             `json:"response"`
	Scopes              []string             `json:"scopes"`
	SupportsMediaUpload bool                 `json:"supportsMediaUpload"`
}

type discParam struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Location    string   `json:"location"` // "path" or "query"
	Required    bool     `json:"required"`
	Default     string   `json:"default"`
	Enum        []string `json:"enum"`
	Repeated    bool     `json:"repeated"`
	Format      string   `json:"format"`
}

type discRef struct {
	Ref string `json:"$ref"`
}

type discSchema struct {
	Type        string                `json:"type"`
	Description string                `json:"description"`
	Properties  map[string]discSchema `json:"properties"`
	Items       *discSchema           `json:"items"`
	Ref         string                `json:"$ref"`
	Format      string                `json:"format"`
	Enum        []string              `json:"enum"`
}

// Parse converts a Google Discovery JSON document into an APISpec.
func Parse(source string, data []byte) (*spec.APISpec, error) {
	var doc discDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing discovery doc: %w", err)
	}
	return convert(source, &doc)
}

func convert(source string, doc *discDoc) (*spec.APISpec, error) {
	name := doc.Name
	if name == "" {
		name = deriveName(source)
	}

	baseURL := doc.BaseURL
	if baseURL == "" {
		baseURL = strings.TrimRight(doc.RootURL, "/")
		if doc.BasePath != "" {
			baseURL += "/" + strings.Trim(doc.BasePath, "/")
		}
	}
	// Strip trailing slash; generator adds its own separators.
	baseURL = strings.TrimRight(baseURL, "/")

	apiSpec := &spec.APISpec{
		Name:        name,
		DisplayName: doc.Title,
		Description: doc.Description,
		Version:     doc.Version,
		BaseURL:     baseURL,
		SpecSource:  "official",
		Resources:   make(map[string]spec.Resource),
		Types:       make(map[string]spec.TypeDef),
	}

	// Auth: Google Discovery uses OAuth2 exclusively for authorized APIs.
	if doc.Auth.OAuth2 != nil && len(doc.Auth.OAuth2.Scopes) > 0 {
		scopes := make([]string, 0, len(doc.Auth.OAuth2.Scopes))
		for scope := range doc.Auth.OAuth2.Scopes {
			scopes = append(scopes, scope)
		}
		apiSpec.Auth = spec.AuthConfig{
			Type:    "bearer_token",
			EnvVars: []string{strings.ToUpper(name) + "_ACCESS_TOKEN"},
			Header:  "Authorization",
			Format:  "Bearer {token}",
			Scopes:  scopes,
		}
	}

	// Convert root-level resources.
	// The generator only handles one level of SubResources (resource ->
	// sub-resource). Discovery APIs can be deeply nested. We flatten by
	// collecting each top-level resource and its immediate children as
	// sub-resources, then promoting deeper descendants back up to the
	// top-level resource with compound names (e.g. "settings-delegates").
	ctx := &convCtx{
		doc:        doc,
		rootParams: doc.Parameters,
	}
	for resName, res := range doc.Resources {
		converted := ctx.convertTopResource(resName, &res)
		if len(converted.Endpoints) > 0 || len(converted.SubResources) > 0 {
			apiSpec.Resources[resName] = converted
		}
	}

	return apiSpec, nil
}

type convCtx struct {
	doc        *discDoc
	rootParams map[string]discParam
}

// convertTopResource converts a Discovery top-level resource into a spec.Resource.
// The generator supports exactly one level of SubResources (resource -> sub-resource).
// Discovery APIs can nest deeper, so we flatten: the top resource's direct children
// become SubResources, and deeper descendants are promoted to SubResources of the
// top resource with compound names (e.g. "settings-delegates").
func (c *convCtx) convertTopResource(resName string, res *discResource) spec.Resource {
	out := spec.Resource{
		Endpoints:    make(map[string]spec.Endpoint),
		SubResources: make(map[string]spec.Resource),
	}

	// Top resource's own methods.
	endpointNames := make(map[string]spec.Endpoint)
	for methodName, method := range res.Methods {
		m := method
		if m.SupportsMediaUpload {
			continue
		}
		ep := c.convertMethod(&m)
		epName := endpointName(methodName, m.HTTPMethod, ep.Path)
		if _, exists := endpointNames[epName]; exists {
			epName = discovery.UniqueEndpointName(endpointNames, epName)
		}
		endpointNames[epName] = ep
		out.Endpoints[epName] = ep
	}

	// Walk children and flatten into SubResources (max one level).
	c.flattenInto(&out, res, "")

	return out
}

// flattenInto walks a Discovery resource tree and adds each child as a
// sub-resource of out. Children of children are promoted with compound names
// so the generator's single-level SubResource handling works correctly.
func (c *convCtx) flattenInto(out *spec.Resource, res *discResource, prefix string) {
	for subName, subRes := range res.Resources {
		sub := subRes
		key := subName
		if prefix != "" {
			key = prefix + "-" + subName
		}

		converted := c.convertShallowResource(&sub)
		if len(converted.Endpoints) > 0 || len(converted.SubResources) > 0 {
			out.SubResources[key] = converted
		}

		// Recurse: sub's own children become sibling sub-resources of out.
		c.flattenInto(out, &sub, key)
	}
}

// convertShallowResource converts a Discovery resource's direct methods only,
// without recursing into sub-resources (the caller handles recursion via flattenInto).
func (c *convCtx) convertShallowResource(res *discResource) spec.Resource {
	out := spec.Resource{
		Endpoints: make(map[string]spec.Endpoint),
	}
	endpointNames := make(map[string]spec.Endpoint)
	for methodName, method := range res.Methods {
		m := method
		if m.SupportsMediaUpload {
			continue
		}
		ep := c.convertMethod(&m)
		epName := endpointName(methodName, m.HTTPMethod, ep.Path)
		if _, exists := endpointNames[epName]; exists {
			epName = discovery.UniqueEndpointName(endpointNames, epName)
		}
		endpointNames[epName] = ep
		out.Endpoints[epName] = ep
	}
	return out
}

func (c *convCtx) convertMethod(method *discMethod) spec.Endpoint {
	path := method.Path
	if method.FlatPath != "" {
		path = method.FlatPath
	}
	// Ensure path starts with /
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	ep := spec.Endpoint{
		Method:      strings.ToUpper(method.HTTPMethod),
		Path:        path,
		Description: method.Description,
	}

	// Path parameters first (they become positional args), then query params.
	// Discovery's parameterOrder is authoritative for positional binding.
	var pathParams, queryParams []spec.Param
	pathParamsByName := make(map[string]spec.Param)
	for paramName, param := range method.Parameters {
		p := convertParam(paramName, &param)
		if param.Location == "path" {
			pathParamsByName[paramName] = p
		} else {
			queryParams = append(queryParams, p)
		}
	}
	for _, name := range method.ParameterOrder {
		if p, ok := pathParamsByName[name]; ok {
			pathParams = append(pathParams, p)
			delete(pathParamsByName, name)
		}
	}
	remainingPathNames := make([]string, 0, len(pathParamsByName))
	for name := range pathParamsByName {
		remainingPathNames = append(remainingPathNames, name)
	}
	sort.Strings(remainingPathNames)
	for _, name := range remainingPathNames {
		pathParams = append(pathParams, pathParamsByName[name])
	}
	sort.Slice(queryParams, func(i, j int) bool { return queryParams[i].Name < queryParams[j].Name })
	ep.Params = append(pathParams, queryParams...)

	// Also include root-level parameters that appear on every method
	// (e.g. prettyPrint, quotaUser) as optional query params — but only if
	// the method doesn't already declare them.
	declared := make(map[string]bool)
	for _, p := range ep.Params {
		declared[p.Name] = true
	}
	for paramName, param := range c.rootParams {
		if declared[paramName] {
			continue
		}
		// Skip noisy system params users rarely need.
		if isSystemParam(paramName) {
			continue
		}
		p := convertParam(paramName, &param)
		ep.Params = append(ep.Params, p)
	}

	// Request body.
	if method.Request != nil && method.Request.Ref != "" {
		bodyParams := c.schemaToParams(method.Request.Ref)
		ep.Body = bodyParams
	}

	// Response schema — derive IDField if possible.
	if method.Response != nil && method.Response.Ref != "" {
		ep.IDField = c.resolveIDField(method.Response.Ref)
	}

	return ep
}

// convertParam maps a Discovery parameter to a spec.Param.
func convertParam(name string, p *discParam) spec.Param {
	paramType := mapType(p.Type, p.Format)
	sp := spec.Param{
		Name:        name,
		Type:        paramType,
		Required:    p.Required,
		Description: p.Description,
		Enum:        p.Enum,
		Format:      p.Format,
	}
	if p.Location == "path" {
		sp.PathParam = true
		sp.Positional = p.Required
	}
	if p.Default != "" {
		sp.Default = p.Default
	}
	return sp
}

// schemaToParams converts a named schema's top-level properties into body
// params. Nested objects become Param.Fields entries.
func (c *convCtx) schemaToParams(schemaName string) []spec.Param {
	schema, ok := c.doc.Schemas[schemaName]
	if !ok {
		return nil
	}
	var params []spec.Param
	for propName, prop := range schema.Properties {
		p := spec.Param{
			Name:        propName,
			Type:        mapSchemaType(&prop),
			Description: prop.Description,
		}
		// Recurse one level for object properties.
		if prop.Type == "object" && len(prop.Properties) > 0 {
			for subName, subProp := range prop.Properties {
				sub := subProp
				p.Fields = append(p.Fields, spec.Param{
					Name:        subName,
					Type:        mapSchemaType(&sub),
					Description: sub.Description,
				})
			}
		}
		params = append(params, p)
	}
	return params
}

// resolveIDField looks at the response schema and returns a likely primary key.
func (c *convCtx) resolveIDField(schemaName string) string {
	schema, ok := c.doc.Schemas[schemaName]
	if !ok {
		return ""
	}
	for _, candidate := range []string{"id", "name", "messageId", "threadId"} {
		if _, has := schema.Properties[candidate]; has {
			return candidate
		}
	}
	return ""
}

// mapType converts a Discovery primitive type + format to a spec type string.
func mapType(typ, format string) string {
	switch typ {
	case "integer":
		if format == "int64" || format == "uint64" {
			return "integer"
		}
		return "integer"
	case "boolean":
		return "boolean"
	case "number":
		return "number"
	case "array":
		return "array"
	case "object":
		return "object"
	default:
		return "string"
	}
}

// mapSchemaType maps a Discovery schema node to a spec type string.
func mapSchemaType(s *discSchema) string {
	if s.Ref != "" {
		return "object"
	}
	return mapType(s.Type, s.Format)
}

// endpointName derives a stable name from the method name and HTTP verb+path.
func endpointName(methodName, httpMethod, path string) string {
	// Discovery method names are like "list", "get", "insert", "delete",
	// "modify", "batchModify" etc. Use them directly since they're already
	// well-named.
	if methodName != "" {
		return strings.ReplaceAll(methodName, "-", "_")
	}
	return discovery.EndpointName(httpMethod, path)
}

// isSystemParam returns true for Discovery system parameters that add noise
// but have no user-facing value in a CLI tool.
func isSystemParam(name string) bool {
	switch name {
	case "alt", "callback", "fields", "key", "oauth_token",
		"prettyPrint", "quotaUser", "uploadType", "upload_protocol",
		"$.xgafv", "xgafv":
		return true
	}
	return false
}

// deriveName extracts an API name from a URL or file path.
func deriveName(source string) string {
	parts := strings.Split(strings.TrimRight(source, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		p := parts[i]
		if p != "" && p != "rest" && !strings.HasPrefix(p, "v") {
			return strings.ToLower(p)
		}
	}
	return "google-api"
}
