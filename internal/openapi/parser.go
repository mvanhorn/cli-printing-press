package openapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mvanhorn/cli-printing-press/internal/spec"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const (
	maxResources            = 50
	maxEndpointsPerResource = 50
)

// stripBrokenRefs attempts to remove broken component references from a spec.
// It extracts the broken key name from the error message, finds and removes it
// from the raw JSON/YAML, then returns the cleaned data.
func stripBrokenRefs(data []byte, errMsg string) []byte {
	// Extract the broken ref path like "#/components/requestBodies/BadKey"
	// from error messages like: bad data in "#/components/requestBodies/BadKey"
	parts := strings.SplitN(errMsg, "\"#/", 2)
	if len(parts) < 2 {
		return data
	}
	refPath := strings.SplitN(parts[1], "\"", 2)[0] // e.g. "components/requestBodies/BadKey"
	segments := strings.Split(refPath, "/")
	if len(segments) < 2 {
		return data
	}
	brokenKey := segments[len(segments)-1]
	section := segments[len(segments)-2] // e.g. "requestBodies"

	refStr := fmt.Sprintf("#/components/%s/%s", section, brokenKey)

	// Try to parse as JSON and remove the broken key + any paths referencing it
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) == nil {
		modified := false

		// Remove from components
		if componentsRaw, ok := raw["components"]; ok {
			var components map[string]json.RawMessage
			if json.Unmarshal(componentsRaw, &components) == nil {
				if sectionRaw, ok := components[section]; ok {
					var sectionMap map[string]json.RawMessage
					if json.Unmarshal(sectionRaw, &sectionMap) == nil {
						if _, exists := sectionMap[brokenKey]; exists {
							delete(sectionMap, brokenKey)
							fmt.Fprintf(os.Stderr, "info: removed broken component %s/%s\n", section, brokenKey)
							sectionBytes, _ := json.Marshal(sectionMap)
							components[section] = sectionBytes
							componentsBytes, _ := json.Marshal(components)
							raw["components"] = componentsBytes
							modified = true
						}
					}
				}
			}
		}

		// Also strip any paths that reference the broken component
		if pathsRaw, ok := raw["paths"]; ok {
			var paths map[string]json.RawMessage
			if json.Unmarshal(pathsRaw, &paths) == nil {
				for pathKey, pathVal := range paths {
					if strings.Contains(string(pathVal), refStr) {
						delete(paths, pathKey)
						fmt.Fprintf(os.Stderr, "info: removed path %s (references broken %s)\n", pathKey, brokenKey)
						modified = true
					}
				}
				pathsBytes, _ := json.Marshal(paths)
				raw["paths"] = pathsBytes
			}
		}

		if modified {
			cleaned, _ := json.Marshal(raw)
			return cleaned
		}
	}

	return data
}

// Parse parses an OpenAPI spec strictly. Use ParseLenient for specs with broken $refs.
func Parse(data []byte) (*spec.APISpec, error) {
	return parse(data, false)
}

// ParseLenient parses an OpenAPI spec, skipping validation errors from broken $refs.
// It logs warnings to stderr for any issues found but continues parsing.
func ParseLenient(data []byte) (*spec.APISpec, error) {
	return parse(data, true)
}

func parse(data []byte, lenient bool) (*spec.APISpec, error) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = lenient
	doc, err := loader.LoadFromData(data)
	if err != nil {
		if !lenient {
			return nil, fmt.Errorf("loading OpenAPI spec: %w", err)
		}
		// In lenient mode, strip broken refs and retry up to 10 times
		for attempt := 0; attempt < 10 && err != nil; attempt++ {
			fmt.Fprintf(os.Stderr, "warning: spec parse error (attempt %d), cleaning: %v\n", attempt+1, err)
			cleaned := stripBrokenRefs(data, err.Error())
			if len(cleaned) == len(data) {
				break // stripBrokenRefs couldn't remove anything
			}
			data = cleaned
			doc, err = loader.LoadFromData(data)
		}
		if err != nil {
			return nil, fmt.Errorf("loading OpenAPI spec (even after cleanup): %w", err)
		}
		fmt.Fprintf(os.Stderr, "info: spec loaded after stripping broken references\n")
	}

	doc.InternalizeRefs(context.Background(), nil)

	name := "api"
	description := ""
	version := ""
	if doc.Info != nil {
		if v := cleanSpecName(doc.Info.Title); v != "" && v != "api" {
			name = v
		}
		if name == "api" && doc.Info.Extensions != nil {
			if raw, ok := doc.Info.Extensions["x-api-name"]; ok {
				if s, ok := raw.(string); ok {
					if v := cleanSpecName(s); v != "" && v != "api" {
						name = v
					}
				}
			}
		}
		description = strings.TrimSpace(doc.Info.Description)
		version = strings.TrimSpace(doc.Info.Version)
	}

	// Extract x-proxy-routes extension for proxy-envelope client pattern
	var proxyRoutes map[string]string
	if doc.Info != nil && doc.Info.Extensions != nil {
		if raw, ok := doc.Info.Extensions["x-proxy-routes"]; ok {
			if m, ok := raw.(map[string]interface{}); ok {
				proxyRoutes = make(map[string]string, len(m))
				for k, v := range m {
					if s, ok := v.(string); ok {
						proxyRoutes[k] = s
					}
				}
			}
		}
	}

	baseURL := ""
	basePath := ""
	if len(doc.Servers) > 0 && doc.Servers[0] != nil {
		serverURL := strings.TrimRight(strings.TrimSpace(doc.Servers[0].URL), "/")
		// Resolve server URL template variables using defaults.
		if strings.Contains(serverURL, "{") && doc.Servers[0].Variables != nil {
			for varName, variable := range doc.Servers[0].Variables {
				if variable != nil && variable.Default != "" {
					serverURL = strings.ReplaceAll(serverURL, "{"+varName+"}", variable.Default)
				}
			}
		}
		// Strip any remaining unresolved template variables.
		for strings.Contains(serverURL, "{") {
			start := strings.Index(serverURL, "{")
			end := strings.Index(serverURL, "}")
			if start == -1 || end == -1 || end < start {
				break
			}
			serverURL = serverURL[:start] + serverURL[end+1:]
		}
		serverURL = strings.ReplaceAll(serverURL, "//", "/")
		// Restore protocol double-slash if normalization collapsed it.
		serverURL = strings.Replace(serverURL, "http:/", "http://", 1)
		serverURL = strings.Replace(serverURL, "https:/", "https://", 1)
		serverURL = strings.TrimRight(serverURL, "/")
		if serverURL != "" {
			lowerURL := strings.ToLower(serverURL)
			if strings.HasPrefix(lowerURL, "http://") || strings.HasPrefix(lowerURL, "https://") {
				baseURL = serverURL
			} else {
				// Relative URL - store the path portion to append when user configures a host
				basePath = serverURL
				warnf("server URL %q is relative; generated CLI will require base_url in config (e.g. https://example.com%s)", serverURL, serverURL)
			}
		}
	}
	if baseURL == "" && basePath == "" {
		warnf("no servers defined in spec; generated CLI will require base_url in config")
		baseURL = "https://api.example.com"
	}

	result := &spec.APISpec{
		Name:        name,
		Description: description,
		Version:     version,
		BaseURL:     baseURL,
		BasePath:    basePath,
		ProxyRoutes: proxyRoutes,
		Auth:        mapAuth(doc, name),
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   fmt.Sprintf("~/.config/%s-pp-cli/config.toml", name),
		},
		Resources: map[string]spec.Resource{},
		Types:     map[string]spec.TypeDef{},
	}

	resourceBasePath := basePath
	if baseURL != "" {
		resourceBasePath = baseURLPath(baseURL)
	}
	mapResources(doc, result, resourceBasePath)
	mapTypes(doc, result)

	if err := result.Validate(); err != nil {
		return nil, fmt.Errorf("validating parsed spec: %w", err)
	}

	return result, nil
}

func mapAuth(doc *openapi3.T, name string) spec.AuthConfig {
	auth := spec.AuthConfig{Type: "none"}
	schemeName, scheme := selectSecurityScheme(doc)
	if scheme == nil {
		return inferQueryParamAuth(doc, name, auth)
	}

	auth.Scheme = schemeName

	switch strings.ToLower(scheme.Type) {
	case "http":
		switch strings.ToLower(scheme.Scheme) {
		case "bearer":
			auth.Type = "bearer_token"
			auth.Header = "Authorization"
		case "basic":
			auth.Type = "api_key"
			auth.Header = "Authorization"
			auth.Format = "Basic {username}:{password}"
		}
	case "apikey":
		auth.Type = "api_key"
		auth.Header = strings.TrimSpace(scheme.Name)
		if auth.Header == "" {
			auth.Header = "Authorization"
		}
		auth.In = strings.TrimSpace(scheme.In)
		// Detect bot token pattern from scheme name (e.g. "BotToken")
		if strings.Contains(strings.ToLower(schemeName), "bot") && strings.EqualFold(auth.Header, "Authorization") {
			auth.Format = "Bot {bot_token}"
		}
	case "oauth2":
		auth.Type = "bearer_token"
		auth.Header = "Authorization"
		if scheme.Flows != nil {
			if ac := scheme.Flows.AuthorizationCode; ac != nil {
				auth.AuthorizationURL = ac.AuthorizationURL
				auth.TokenURL = ac.TokenURL
				for scope := range ac.Scopes {
					auth.Scopes = append(auth.Scopes, scope)
				}
				sort.Strings(auth.Scopes)
			} else if ic := scheme.Flows.Implicit; ic != nil {
				auth.AuthorizationURL = ic.AuthorizationURL
				for scope := range ic.Scopes {
					auth.Scopes = append(auth.Scopes, scope)
				}
				sort.Strings(auth.Scopes)
			}
		}
	}

	envPrefix := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	switch auth.Type {
	case "api_key":
		// Use scheme name for more specific env var (e.g. BotToken -> DISCORD_BOT_TOKEN)
		schemeEnvSuffix := toSnakeCase(schemeName)
		if schemeEnvSuffix != "" && schemeEnvSuffix != "api_key" {
			auth.EnvVars = []string{envPrefix + "_" + strings.ToUpper(schemeEnvSuffix)}
		} else {
			auth.EnvVars = []string{envPrefix + "_API_KEY"}
		}
	case "bearer_token":
		auth.EnvVars = []string{envPrefix + "_TOKEN"}
	}

	return auth
}

// commonAuthQueryParams are query parameter names that commonly carry API keys.
var commonAuthQueryParams = map[string]bool{
	"key":          true,
	"api_key":      true,
	"apikey":       true,
	"access_token": true,
	"token":        true,
}

// inferQueryParamAuth scans all operations for query parameters that look like
// API keys. If more than 30% of operations carry one, we infer query-param auth.
// This handles specs that omit securitySchemes but pass keys via query string.
func inferQueryParamAuth(doc *openapi3.T, name string, fallback spec.AuthConfig) spec.AuthConfig {
	if doc == nil || doc.Paths == nil {
		return fallback
	}

	paramCounts := map[string]int{}
	totalOps := 0

	for _, pathKey := range doc.Paths.InMatchingOrder() {
		pathItem := doc.Paths.Value(pathKey)
		if pathItem == nil {
			continue
		}
		for _, op := range pathItem.Operations() {
			if op == nil {
				continue
			}
			totalOps++
			seen := false
			// Check path-level parameters first, then operation-level.
			for _, params := range []openapi3.Parameters{pathItem.Parameters, op.Parameters} {
				for _, pRef := range params {
					if pRef == nil || pRef.Value == nil {
						continue
					}
					p := pRef.Value
					if p.In == openapi3.ParameterInQuery && commonAuthQueryParams[strings.ToLower(p.Name)] && !seen {
						paramCounts[p.Name]++
						seen = true
					}
				}
			}
		}
	}

	if totalOps == 0 {
		return fallback
	}

	// Find the most common auth-like param name.
	var best string
	var bestCount int
	for pName, cnt := range paramCounts {
		if cnt > bestCount {
			best = pName
			bestCount = cnt
		}
	}

	if bestCount == 0 || float64(bestCount)/float64(totalOps) <= 0.3 {
		return fallback
	}

	envPrefix := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	return spec.AuthConfig{
		Type:    "api_key",
		In:      "query",
		Header:  best,
		EnvVars: []string{envPrefix + "_API_KEY"},
	}
}

func selectSecurityScheme(doc *openapi3.T) (string, *openapi3.SecurityScheme) {
	if doc == nil || doc.Components == nil || len(doc.Components.SecuritySchemes) == 0 {
		return "", nil
	}

	orderedNames := orderedSecuritySchemeNames(doc)
	for _, name := range orderedNames {
		scheme := securitySchemeValue(doc.Components.SecuritySchemes[name])
		if scheme == nil || !strings.EqualFold(scheme.Type, "oauth2") || scheme.Flows == nil {
			continue
		}
		if ac := scheme.Flows.AuthorizationCode; ac != nil && strings.TrimSpace(ac.AuthorizationURL) != "" && strings.TrimSpace(ac.TokenURL) != "" {
			return name, scheme
		}
	}

	for _, name := range orderedNames {
		scheme := securitySchemeValue(doc.Components.SecuritySchemes[name])
		if scheme == nil {
			continue
		}
		switch strings.ToLower(scheme.Type) {
		case "apikey", "oauth2":
			return name, scheme
		case "http":
			switch strings.ToLower(scheme.Scheme) {
			case "bearer", "basic":
				return name, scheme
			}
		}
	}

	for _, name := range orderedNames {
		scheme := securitySchemeValue(doc.Components.SecuritySchemes[name])
		if scheme != nil {
			return name, scheme
		}
	}

	return "", nil
}

func orderedSecuritySchemeNames(doc *openapi3.T) []string {
	seen := map[string]struct{}{}
	var names []string

	for _, requirement := range doc.Security {
		var requirementNames []string
		for name := range requirement {
			requirementNames = append(requirementNames, name)
		}
		sort.Strings(requirementNames)
		for _, name := range requirementNames {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}

	var all []string
	for name := range doc.Components.SecuritySchemes {
		all = append(all, name)
	}
	sort.Strings(all)
	for _, name := range all {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	return names
}

func securitySchemeValue(ref *openapi3.SecuritySchemeRef) *openapi3.SecurityScheme {
	if ref == nil {
		return nil
	}
	return ref.Value
}

func mapResources(doc *openapi3.T, out *spec.APISpec, basePath string) {
	if doc == nil || out == nil || doc.Paths == nil {
		return
	}

	tagDescriptions := mapTagDescriptions(doc.Tags)

	pathMap := doc.Paths.Map()
	pathKeys := make([]string, 0, len(pathMap))
	for path := range pathMap {
		pathKeys = append(pathKeys, path)
	}
	sort.Strings(pathKeys)
	commonPrefix := detectCommonPrefix(pathKeys, basePath)

	for _, path := range pathKeys {
		pathItem := doc.Paths.Value(path)
		if pathItem == nil {
			warnf("skipping path %q: path item is nil", path)
			continue
		}

		operations := pathItem.Operations()
		if len(operations) == 0 {
			warnf("skipping path %q: no valid HTTP methods", path)
			continue
		}

		primaryName, subName := resourceAndSubFromPath(path, basePath, commonPrefix)
		if primaryName == "" {
			warnf("skipping path %q: could not derive resource name", path)
			continue
		}

		resource, ok := out.Resources[primaryName]
		if !ok {
			if len(out.Resources) >= maxResources {
				warnf("skipping path %q: resource limit (%d) reached", path, maxResources)
				continue
			}
			resource = spec.Resource{
				Description:  tagDescriptions[primaryName],
				Endpoints:    map[string]spec.Endpoint{},
				SubResources: map[string]spec.Resource{},
			}
		}

		// Determine the target: direct resource endpoints or sub-resource endpoints
		var targetEndpoints map[string]spec.Endpoint
		targetResourceName := primaryName
		if subName != "" {
			if _, ok := resource.SubResources[subName]; !ok {
				resource.SubResources[subName] = spec.Resource{
					Description: tagDescriptions[subName],
					Endpoints:   map[string]spec.Endpoint{},
				}
			}
			targetEndpoints = resource.SubResources[subName].Endpoints
			targetResourceName = subName
		} else {
			targetEndpoints = resource.Endpoints
		}

		methods := make([]string, 0, len(operations))
		for method := range operations {
			methods = append(methods, method)
		}
		sort.Strings(methods)

		for _, method := range methods {
			op := operations[method]
			if op == nil {
				warnf("skipping %s %q: operation is nil", method, path)
				continue
			}

			if len(targetEndpoints) >= maxEndpointsPerResource {
				warnf("skipping %s %q: endpoint limit (%d) reached for resource %q.%s", method, path, maxEndpointsPerResource, primaryName, targetResourceName)
				continue
			}

			endpointName := resolveEndpointName(method, path, op, targetEndpoints, targetResourceName, basePath, commonPrefix)
			summary := strings.TrimSpace(op.Summary)
			desc := strings.TrimSpace(op.Description)
			description := selectDescription(summary, desc)

			if description == "" {
				description = humanizeEndpointName(endpointName)
			}

			params := mapParameters(pathItem, op)
			body := mapRequestBody(op.RequestBody, method, path)

			// Deduplicate body params that collide with query/path params by flag name
			if len(body) > 0 && len(params) > 0 {
				paramFlags := map[string]bool{}
				for _, p := range params {
					paramFlags[toKebabCase(p.Name)] = true
				}
				filtered := make([]spec.Param, 0, len(body))
				for _, b := range body {
					if !paramFlags[toKebabCase(b.Name)] {
						filtered = append(filtered, b)
					}
				}
				body = filtered
			}

			endpoint := spec.Endpoint{
				Method:      strings.ToUpper(method),
				Path:        path,
				Description: description,
				Params:      params,
				Body:        body,
			}

			endpoint.Response, endpoint.ResponsePath = mapResponse(op, endpointName)
			if strings.ToUpper(method) == "GET" {
				endpoint.Pagination = detectPagination(endpoint.Params, op)
			}
			targetEndpoints[endpointName] = endpoint
		}

		// Update descriptions
		if subName != "" {
			sub := resource.SubResources[subName]
			if sub.Description == "" {
				sub.Description = humanizeResourceName(subName)
			}
			resource.SubResources[subName] = sub
		}
		if resource.Description == "" {
			resource.Description = humanizeResourceName(primaryName)
		}
		out.Resources[primaryName] = resource
	}

	assignEndpointAliases(out.Resources)
	filterGlobalParams(out.Resources)
}

func assignEndpointAliases(resources map[string]spec.Resource) {
	resourceNames := make([]string, 0, len(resources))
	for name := range resources {
		resourceNames = append(resourceNames, name)
	}
	sort.Strings(resourceNames)

	for _, name := range resourceNames {
		resource := resources[name]
		assignAliasesInResource(&resource)
		resources[name] = resource
	}
}

func assignAliasesInResource(resource *spec.Resource) {
	if resource == nil {
		return
	}

	assignAliasesForEndpoints(resource.Endpoints)

	subNames := make([]string, 0, len(resource.SubResources))
	for name := range resource.SubResources {
		subNames = append(subNames, name)
	}
	sort.Strings(subNames)

	for _, name := range subNames {
		subResource := resource.SubResources[name]
		assignAliasesInResource(&subResource)
		resource.SubResources[name] = subResource
	}
}

func assignAliasesForEndpoints(endpoints map[string]spec.Endpoint) {
	if len(endpoints) == 0 {
		return
	}

	endpointNames := make([]string, 0, len(endpoints))
	nameSet := make(map[string]struct{}, len(endpoints))
	for name := range endpoints {
		endpointNames = append(endpointNames, name)
		nameSet[name] = struct{}{}
	}
	sort.Strings(endpointNames)

	usedAliases := map[string]struct{}{}
	for _, name := range endpointNames {
		endpoint := endpoints[name]
		alias := computeAlias(endpoint.Method, endpoint.Path, name)
		if alias == "" || alias == name {
			endpoints[name] = endpoint
			continue
		}
		if _, exists := nameSet[alias]; exists {
			endpoints[name] = endpoint
			continue
		}
		if _, used := usedAliases[alias]; used {
			endpoints[name] = endpoint
			continue
		}

		endpoint.Alias = alias
		endpoints[name] = endpoint
		usedAliases[alias] = struct{}{}
	}
}

func computeAlias(method, path, endpointName string) string {
	_ = endpointName

	hasPathParam := strings.Contains(path, "{")
	switch strings.ToUpper(method) {
	case "GET":
		if hasPathParam {
			return "get"
		}
		return "list"
	case "POST":
		return "create"
	case "PUT", "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	default:
		return ""
	}
}

func filterGlobalParams(resources map[string]spec.Resource) {
	totalEndpoints := 0
	paramCounts := map[string]int{}

	walkResourceEndpoints(resources, func(endpoint *spec.Endpoint) {
		totalEndpoints++

		seen := map[string]struct{}{}
		for _, param := range endpoint.Params {
			if param.Positional {
				continue
			}
			if _, ok := seen[param.Name]; ok {
				continue
			}
			seen[param.Name] = struct{}{}
			paramCounts[param.Name]++
		}
	})

	if totalEndpoints == 0 {
		return
	}

	globalParams := map[string]int{}
	threshold := float64(totalEndpoints) * 0.8
	for name, count := range paramCounts {
		if float64(count) > threshold {
			globalParams[name] = count
		}
	}

	if len(globalParams) == 0 {
		return
	}

	walkResourceEndpoints(resources, func(endpoint *spec.Endpoint) {
		filtered := endpoint.Params[:0]
		for _, param := range endpoint.Params {
			if !param.Positional {
				if _, ok := globalParams[param.Name]; ok {
					continue
				}
			}
			filtered = append(filtered, param)
		}
		endpoint.Params = filtered
	})

	names := make([]string, 0, len(globalParams))
	for name := range globalParams {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		warnf("filtered global query param %q from generated commands: present on %d/%d endpoints", name, globalParams[name], totalEndpoints)
	}
}

func walkResourceEndpoints(resources map[string]spec.Resource, fn func(endpoint *spec.Endpoint)) {
	resourceNames := make([]string, 0, len(resources))
	for name := range resources {
		resourceNames = append(resourceNames, name)
	}
	sort.Strings(resourceNames)

	for _, name := range resourceNames {
		resource := resources[name]
		walkResourceEndpointsInResource(&resource, fn)
		resources[name] = resource
	}
}

func walkResourceEndpointsInResource(resource *spec.Resource, fn func(endpoint *spec.Endpoint)) {
	endpointNames := make([]string, 0, len(resource.Endpoints))
	for name := range resource.Endpoints {
		endpointNames = append(endpointNames, name)
	}
	sort.Strings(endpointNames)

	for _, name := range endpointNames {
		endpoint := resource.Endpoints[name]
		fn(&endpoint)
		resource.Endpoints[name] = endpoint
	}

	subNames := make([]string, 0, len(resource.SubResources))
	for name := range resource.SubResources {
		subNames = append(subNames, name)
	}
	sort.Strings(subNames)

	for _, name := range subNames {
		subResource := resource.SubResources[name]
		walkResourceEndpointsInResource(&subResource, fn)
		resource.SubResources[name] = subResource
	}
}

func mapTagDescriptions(tags openapi3.Tags) map[string]string {
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		if tag == nil {
			continue
		}
		if desc := strings.TrimSpace(tag.Description); desc != "" {
			for _, key := range tagDescriptionKeys(tag.Name) {
				out[key] = desc
			}
		}
	}
	return out
}

func tagDescriptionKeys(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}

	seen := map[string]struct{}{}
	keys := make([]string, 0, 6)

	add := func(key string) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}

	snake := toSnakeCase(name)
	kebab := strings.ReplaceAll(snake, "_", "-")
	bases := []string{
		snake,
		kebab,
		strings.ToLower(name),
	}
	for _, base := range bases {
		add(base)
		if strings.HasSuffix(base, "s") && len(base) > 1 {
			add(strings.TrimSuffix(base, "s"))
		} else {
			add(base + "s")
		}
	}

	return keys
}

func resolveEndpointName(method, path string, op *openapi3.Operation, existing map[string]spec.Endpoint, resourceName, basePath string, commonPrefix []string) string {
	name := operationIDToName(operationID(op), resourceName, commonPrefix)
	if name == "" {
		name = defaultEndpointName(method, path)
	}
	if name == "" {
		name = strings.ToLower(method)
	}

	if _, ok := existing[name]; !ok {
		return name
	}

	suffix := endpointCollisionSuffix(path, resourceName, basePath)
	if suffix == "" {
		suffix = "endpoint"
	}

	candidate := name + "-" + suffix
	if _, ok := existing[candidate]; !ok {
		return candidate
	}

	for i := 2; ; i++ {
		alt := fmt.Sprintf("%s-%s-%d", name, suffix, i)
		if _, ok := existing[alt]; !ok {
			return alt
		}
	}
}

func operationID(op *openapi3.Operation) string {
	if op == nil {
		return ""
	}
	return strings.TrimSpace(op.OperationID)
}

func defaultEndpointName(method, path string) string {
	switch strings.ToUpper(method) {
	case "GET":
		if hasPathParams(path) {
			return "get"
		}
		return "list"
	case "POST":
		return "create"
	case "PUT", "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	default:
		return strings.ToLower(method)
	}
}

func hasPathParams(path string) bool {
	return strings.Contains(path, "{") && strings.Contains(path, "}")
}

func mapParameters(pathItem *openapi3.PathItem, op *openapi3.Operation) []spec.Param {
	merged := mergeParameters(pathItem, op)
	params := make([]spec.Param, 0, len(merged))
	for _, parameter := range merged {
		if parameter == nil {
			continue
		}
		if parameter.In != openapi3.ParameterInPath && parameter.In != openapi3.ParameterInQuery {
			continue
		}

		schema := schemaRefValue(parameter.Schema)
		// Skip parameters with names that can't be Go identifiers
		paramName := parameter.Name
		if strings.HasPrefix(paramName, "$") || strings.HasPrefix(paramName, ".") {
			continue
		}
		description := strings.TrimSpace(parameter.Description)
		if description == "" {
			description = humanizeFieldName(paramName)
		}
		param := spec.Param{
			Name:        paramName,
			Type:        mapSchemaType(schema),
			Required:    parameter.Required,
			Positional:  parameter.In == openapi3.ParameterInPath,
			Description: description,
			Enum:        schemaEnum(schema),
			Format:      schemaFormat(schema),
		}
		if schema != nil && schema.Default != nil {
			param.Default = schema.Default
		}
		if param.Positional {
			param.Required = true
		}
		params = append(params, param)
	}
	return params
}

func mergeParameters(pathItem *openapi3.PathItem, op *openapi3.Operation) []*openapi3.Parameter {
	var merged []*openapi3.Parameter
	index := map[string]int{}

	add := func(parameters openapi3.Parameters, override bool) {
		for _, parameterRef := range parameters {
			if parameterRef == nil || parameterRef.Value == nil {
				continue
			}
			parameter := parameterRef.Value
			key := strings.ToLower(parameter.In) + ":" + parameter.Name
			if i, ok := index[key]; ok {
				if override {
					merged[i] = parameter
				}
				continue
			}
			index[key] = len(merged)
			merged = append(merged, parameter)
		}
	}

	if pathItem != nil {
		add(pathItem.Parameters, false)
	}
	if op != nil {
		add(op.Parameters, true)
	}

	return merged
}

func mapRequestBody(requestBodyRef *openapi3.RequestBodyRef, method, path string) []spec.Param {
	requestBody := requestBodyValue(requestBodyRef)
	if requestBody == nil || requestBody.Content == nil {
		return nil
	}

	media := requestBody.Content.Get("application/json")
	if media == nil {
		media = firstJSONMediaType(requestBody.Content)
	}
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		return nil
	}

	properties := map[string]*openapi3.SchemaRef{}
	required := map[string]struct{}{}
	if collectAllOfProperties(media.Schema, properties, required, map[*openapi3.Schema]struct{}{}) {
		warnf("skipping request body for %s %q: contains oneOf/anyOf", strings.ToUpper(method), path)
		return nil
	}

	if len(properties) == 0 {
		return nil
	}

	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)

	body := make([]spec.Param, 0, len(names))
	seenCamelNames := map[string]bool{}
	for _, name := range names {
		camelName := toCamelCase(name)
		if seenCamelNames[camelName] {
			continue
		}
		seenCamelNames[camelName] = true
		schema := schemaRefValue(properties[name])
		if isComplexBodyFieldSchema(schema) {
			warnf("skipping body field %q: complex type not supported as CLI flag", name)
			continue
		}
		description := schemaDescription(schema)
		if description == "" {
			description = humanizeFieldName(name)
		}
		param := spec.Param{
			Name:        name,
			Type:        mapSchemaType(schema),
			Required:    isRequired(required, name),
			Description: description,
			Enum:        schemaEnum(schema),
			Format:      schemaFormat(schema),
		}
		if schema != nil && schema.Default != nil {
			param.Default = schema.Default
		}
		body = append(body, param)
	}

	return body
}

func collectAllOfProperties(
	schemaRef *openapi3.SchemaRef,
	properties map[string]*openapi3.SchemaRef,
	required map[string]struct{},
	visited map[*openapi3.Schema]struct{},
) bool {
	if schemaRef == nil || schemaRef.Value == nil {
		return false
	}

	schema := schemaRef.Value
	if _, ok := visited[schema]; ok {
		return false
	}
	visited[schema] = struct{}{}

	if len(schema.OneOf) > 0 || len(schema.AnyOf) > 0 {
		return true
	}

	for _, field := range schema.Required {
		required[field] = struct{}{}
	}
	for name, prop := range schema.Properties {
		if prop == nil {
			continue
		}
		properties[name] = prop
	}
	for _, sub := range schema.AllOf {
		if collectAllOfProperties(sub, properties, required, visited) {
			return true
		}
	}

	return false
}

func mapResponse(op *openapi3.Operation, fallbackName string) (spec.ResponseDef, string) {
	if op == nil || op.Responses == nil {
		return spec.ResponseDef{}, ""
	}

	success := selectSuccessResponse(op.Responses)
	if success == nil || success.Value == nil {
		return spec.ResponseDef{}, ""
	}

	schemaRef := selectResponseSchema(success.Value)
	if schemaRef == nil || schemaRef.Value == nil {
		return spec.ResponseDef{}, ""
	}

	schema := schemaRef.Value
	if isObjectSchema(schema) {
		if dataRef := schema.Properties["data"]; dataRef != nil && isArraySchema(schemaRefValue(dataRef)) {
			return spec.ResponseDef{
				Type: "array",
				Item: schemaTypeName(schemaRefValue(dataRef).Items, fallbackName+"Item"),
			}, "data"
		}
	}

	if isArraySchema(schema) {
		return spec.ResponseDef{
			Type: "array",
			Item: schemaTypeName(schema.Items, fallbackName+"Item"),
		}, ""
	}

	if isObjectSchema(schema) {
		return spec.ResponseDef{
			Type: "object",
			Item: schemaTypeName(schemaRef, fallbackName+"Response"),
		}, ""
	}

	return spec.ResponseDef{}, ""
}

func selectSuccessResponse(responses *openapi3.Responses) *openapi3.ResponseRef {
	if responses == nil {
		return nil
	}
	if v := responses.Value("200"); v != nil {
		return v
	}
	if v := responses.Value("201"); v != nil {
		return v
	}

	responseMap := responses.Map()
	keys := make([]string, 0, len(responseMap))
	for key := range responseMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if len(key) == 3 && key[0] == '2' {
			return responses.Value(key)
		}
		if strings.EqualFold(key, "2XX") {
			return responses.Value(key)
		}
	}

	return nil
}

func selectResponseSchema(response *openapi3.Response) *openapi3.SchemaRef {
	if response == nil || response.Content == nil {
		return nil
	}
	if media := response.Content.Get("application/json"); media != nil && media.Schema != nil {
		return media.Schema
	}

	contentTypes := make([]string, 0, len(response.Content))
	for contentType := range response.Content {
		contentTypes = append(contentTypes, contentType)
	}
	sort.Strings(contentTypes)
	for _, contentType := range contentTypes {
		media := response.Content[contentType]
		if media != nil && media.Schema != nil {
			return media.Schema
		}
	}

	return nil
}

func mapTypes(doc *openapi3.T, out *spec.APISpec) {
	if doc == nil || doc.Components == nil {
		return
	}

	schemaMap := doc.Components.Schemas
	if len(schemaMap) == 0 {
		return
	}

	names := make([]string, 0, len(schemaMap))
	for name := range schemaMap {
		names = append(names, name)
	}
	sort.Strings(names)

	usedTypeNames := map[string]int{}
	for _, name := range names {
		goName := sanitizeTypeName(name)
		if goName == "" {
			continue
		}
		if count, exists := usedTypeNames[goName]; exists {
			usedTypeNames[goName] = count + 1
			goName = fmt.Sprintf("%s%d", goName, count+1)
		} else {
			usedTypeNames[goName] = 1
		}

		schemaRef := schemaMap[name]
		schema := schemaRefValue(schemaRef)
		if schema == nil {
			warnf("skipping schema %q: schema is nil", name)
			continue
		}
		if !isObjectSchema(schema) {
			continue
		}

		properties := map[string]*openapi3.SchemaRef{}
		collectTypeProperties(schemaRef, properties, map[*openapi3.Schema]struct{}{})

		fieldNames := make([]string, 0, len(properties))
		for fieldName := range properties {
			fieldNames = append(fieldNames, fieldName)
		}
		sort.Strings(fieldNames)

		fields := make([]spec.TypeField, 0, len(fieldNames))
		seenGoNames := map[string]bool{}
		for _, fieldName := range fieldNames {
			if strings.HasPrefix(fieldName, "_") {
				continue
			}
			goFieldName := toCamelCase(fieldName)
			if seenGoNames[goFieldName] {
				continue
			}
			seenGoNames[goFieldName] = true
			fields = append(fields, spec.TypeField{
				Name: fieldName,
				Type: mapSchemaType(schemaRefValue(properties[fieldName])),
			})
		}

		out.Types[goName] = spec.TypeDef{Fields: fields}
	}
}

func collectTypeProperties(schemaRef *openapi3.SchemaRef, properties map[string]*openapi3.SchemaRef, visited map[*openapi3.Schema]struct{}) {
	if schemaRef == nil || schemaRef.Value == nil {
		return
	}

	schema := schemaRef.Value
	if _, ok := visited[schema]; ok {
		return
	}
	visited[schema] = struct{}{}

	for name, prop := range schema.Properties {
		if prop == nil {
			continue
		}
		if strings.HasPrefix(name, "_") {
			continue
		}
		properties[sanitizeTypeName(name)] = prop
	}
	for _, sub := range schema.AllOf {
		collectTypeProperties(sub, properties, visited)
	}
}

func requestBodyValue(ref *openapi3.RequestBodyRef) *openapi3.RequestBody {
	if ref == nil {
		return nil
	}
	return ref.Value
}

func schemaRefValue(ref *openapi3.SchemaRef) *openapi3.Schema {
	if ref == nil {
		return nil
	}
	return ref.Value
}

func mapSchemaType(schema *openapi3.Schema) string {
	if schema == nil || schema.Type == nil {
		return "string"
	}
	// Use Includes instead of Is to handle nullable types like ["boolean", "null"]
	switch {
	case schema.Type.Includes(openapi3.TypeBoolean):
		return "bool"
	case schema.Type.Includes(openapi3.TypeInteger):
		return "int"
	case schema.Type.Includes(openapi3.TypeNumber):
		return "float"
	case schema.Type.Includes(openapi3.TypeString):
		return "string"
	default:
		return "string"
	}
}

func schemaEnum(schema *openapi3.Schema) []string {
	if schema == nil || len(schema.Enum) == 0 {
		return nil
	}
	enum := make([]string, 0, len(schema.Enum))
	for _, value := range schema.Enum {
		switch v := value.(type) {
		case string:
			enum = append(enum, v)
		default:
			enum = append(enum, fmt.Sprint(v))
		}
	}
	return enum
}

func schemaFormat(schema *openapi3.Schema) string {
	if schema == nil {
		return ""
	}
	return strings.TrimSpace(schema.Format)
}

func schemaDescription(schema *openapi3.Schema) string {
	if schema == nil {
		return ""
	}
	return strings.TrimSpace(schema.Description)
}

func isArraySchema(schema *openapi3.Schema) bool {
	if schema == nil {
		return false
	}
	if schema.Type != nil && schema.Type.Includes(openapi3.TypeArray) {
		return true
	}
	return schema.Items != nil
}

func isObjectSchema(schema *openapi3.Schema) bool {
	if schema == nil {
		return false
	}
	if schema.Type != nil && schema.Type.Includes(openapi3.TypeObject) {
		return true
	}
	return len(schema.Properties) > 0 || len(schema.AllOf) > 0
}

func isComplexBodyFieldSchema(schema *openapi3.Schema) bool {
	return isObjectSchema(schema) || isArraySchema(schema)
}

func schemaTypeName(schemaRef *openapi3.SchemaRef, fallback string) string {
	if schemaRef == nil {
		return toTypeName(fallback)
	}

	if refName := refComponentName(schemaRef.Ref); refName != "" {
		return toTypeName(refName)
	}

	schema := schemaRef.Value
	if schema == nil {
		return toTypeName(fallback)
	}
	if schema.Title != "" {
		return toTypeName(schema.Title)
	}

	if schema.Type != nil {
		switch {
		case schema.Type.Is(openapi3.TypeString):
			return "string"
		case schema.Type.Is(openapi3.TypeInteger):
			return "int"
		case schema.Type.Is(openapi3.TypeBoolean):
			return "bool"
		case schema.Type.Is(openapi3.TypeNumber):
			return "float"
		}
	}

	return toTypeName(fallback)
}

func refComponentName(ref string) string {
	if ref == "" {
		return ""
	}
	i := strings.LastIndex(ref, "/")
	if i == -1 || i+1 >= len(ref) {
		return ""
	}
	return ref[i+1:]
}

func firstJSONMediaType(content openapi3.Content) *openapi3.MediaType {
	if content == nil {
		return nil
	}

	contentTypes := make([]string, 0, len(content))
	for contentType := range content {
		contentTypes = append(contentTypes, contentType)
	}
	sort.Strings(contentTypes)

	for _, contentType := range contentTypes {
		if strings.Contains(strings.ToLower(contentType), "json") {
			return content[contentType]
		}
	}

	for _, contentType := range contentTypes {
		media := content[contentType]
		if media != nil && media.Schema != nil {
			return media
		}
	}

	return nil
}

func resourceAndSubFromPath(path, basePath string, commonPrefix []string) (string, string) {
	segments := pathSegmentsAfterBase(path, basePath)
	if len(commonPrefix) > 0 && hasSegmentPrefix(segments, commonPrefix) {
		if primary, sub := resourceAndSubFromSegments(segments[len(commonPrefix):]); primary != "" {
			return primary, sub
		}
	}
	return resourceAndSubFromSegments(segments)
}

func resourceAndSubFromSegments(segments []string) (string, string) {
	if len(segments) == 0 {
		return "", ""
	}
	if isPathParamSegment(segments[0]) {
		return "", ""
	}
	primary := sanitizeResourceName(strings.ReplaceAll(toSnakeCase(segments[0]), "_", "-"))
	if primary == "" {
		return "", ""
	}

	// Look for sub-resource: requires a path param between primary and sub-resource
	// e.g. /guilds/{guild_id}/members -> sub-resource "members"
	// but /store/inventory -> NOT a sub-resource (no param between store and inventory)
	rest := segments[1:]
	hasParam := false
	for len(rest) > 0 && isPathParamSegment(rest[0]) {
		hasParam = true
		rest = rest[1:]
	}
	if !hasParam || len(rest) == 0 {
		return primary, ""
	}
	// The first non-param segment after the path param is the sub-resource
	sub := sanitizeResourceName(strings.ReplaceAll(toSnakeCase(rest[0]), "_", "-"))
	return primary, sub
}

func detectCommonPrefix(paths []string, basePath string) []string {
	segmentLists := make([][]string, 0, len(paths))
	for _, path := range paths {
		segments := pathSegmentsAfterBase(path, basePath)
		if len(segments) > 0 {
			segmentLists = append(segmentLists, segments)
		}
	}
	if len(segmentLists) == 0 {
		return nil
	}

	total := len(segmentLists)
	prefix := make([]string, 0)

	for idx := 0; ; idx++ {
		counts := map[string]int{}
		examples := map[string]string{}

		for _, segments := range segmentLists {
			if len(segments) <= idx || !hasSegmentPrefix(segments, prefix) {
				continue
			}

			key := canonicalPrefixSegment(segments[idx])
			counts[key]++
			if _, ok := examples[key]; !ok {
				examples[key] = segments[idx]
			}
		}

		bestKey := ""
		bestCount := 0
		for key, count := range counts {
			if count > bestCount {
				bestKey = key
				bestCount = count
			}
		}

		if bestCount*10 <= total*9 {
			break
		}

		prefix = append(prefix, examples[bestKey])
	}

	lastParam := -1
	for i, segment := range prefix {
		if isPathParamSegment(segment) {
			lastParam = i
		}
	}
	if lastParam == -1 {
		return nil
	}

	return prefix[:lastParam+1]
}

func hasSegmentPrefix(segments, prefix []string) bool {
	if len(prefix) > len(segments) {
		return false
	}

	for i, prefixSegment := range prefix {
		if canonicalPrefixSegment(segments[i]) != canonicalPrefixSegment(prefixSegment) {
			return false
		}
	}

	return true
}

func canonicalPrefixSegment(segment string) string {
	if isPathParamSegment(segment) {
		return "{}"
	}
	return segment
}

func endpointCollisionSuffix(path, resourceName, basePath string) string {
	segments := pathSegmentsAfterBase(path, basePath)
	if len(segments) == 0 {
		return ""
	}
	if toSnakeCase(segments[0]) == resourceName {
		segments = segments[1:]
	}

	for _, segment := range segments {
		if isPathParamSegment(segment) {
			continue
		}
		if suffix := toKebabCase(segment); suffix != "" {
			return suffix
		}
	}
	for _, segment := range segments {
		segment = strings.Trim(segment, "{}")
		if suffix := toKebabCase(segment); suffix != "" {
			return suffix
		}
	}

	return ""
}

func pathSegmentsAfterBase(path, basePath string) []string {
	segments := splitPath(path)
	if len(segments) == 0 {
		return nil
	}

	baseSegments := splitPath(basePath)
	if len(baseSegments) > 0 && len(segments) >= len(baseSegments) {
		match := true
		for i := range baseSegments {
			if segments[i] != baseSegments[i] {
				match = false
				break
			}
		}
		if match {
			segments = segments[len(baseSegments):]
		}
	}

	if len(segments) > 0 && isVersionSegment(segments[0]) {
		segments = segments[1:]
	}

	return segments
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	raw := strings.Split(path, "/")
	segments := make([]string, 0, len(raw))
	for _, segment := range raw {
		segment = strings.TrimSpace(segment)
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	return segments
}

func isVersionSegment(segment string) bool {
	if len(segment) < 2 || segment[0] != 'v' {
		return false
	}
	_, err := strconv.Atoi(segment[1:])
	return err == nil
}

func isPathParamSegment(segment string) bool {
	return strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}")
}

func baseURLPath(baseURL string) string {
	if baseURL == "" {
		return ""
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	return parsed.Path
}

func operationIDToName(operationID, resourceName string, commonPrefix []string) string {
	if strings.TrimSpace(operationID) == "" {
		return ""
	}
	original := toSnakeCase(operationID)
	if original == "" {
		return ""
	}

	name := strings.TrimPrefix(original, "api_")
	resourceVariants := operationIDResourceVariants(resourceName)

	// Also strip common prefix segments (e.g., "gmail", "users" from "gmail_users_messages_list")
	for _, seg := range commonPrefix {
		if isPathParamSegment(seg) {
			continue
		}
		prefixVariants := operationIDResourceVariants(seg)
		name = stripOperationIDResourcePrefix(name, prefixVariants)
	}

	name = stripOperationIDVersionPrefix(name)
	name = stripOperationIDResourcePrefix(name, resourceVariants)
	name = stripOperationIDVersionPrefix(name)
	name = stripOperationIDResourceSegments(name, resourceVariants)
	name = strings.Trim(name, "_")

	if name == "" {
		return original
	}

	return strings.ReplaceAll(name, "_", "-")
}

func operationIDResourceVariants(resourceName string) []string {
	resource := toSnakeCase(strings.TrimSpace(resourceName))
	if resource == "" {
		return nil
	}

	seen := map[string]struct{}{}
	variants := make([]string, 0, 3)
	addVariant := func(candidate string) {
		if candidate == "" {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		variants = append(variants, candidate)
	}

	addVariant(resource)
	if strings.HasSuffix(resource, "s") && len(resource) > 1 {
		addVariant(strings.TrimSuffix(resource, "s"))
	} else {
		addVariant(resource + "s")
	}
	// Add collapsed variant (no underscores) for operationIDs like "connectedapps"
	collapsed := strings.ReplaceAll(resource, "_", "")
	addVariant(collapsed)
	if strings.HasSuffix(collapsed, "s") && len(collapsed) > 1 {
		addVariant(strings.TrimSuffix(collapsed, "s"))
	}

	return variants
}

func stripOperationIDVersionPrefix(name string) string {
	for {
		switch {
		case strings.HasPrefix(name, "v1_"):
			name = strings.TrimPrefix(name, "v1_")
		case strings.HasPrefix(name, "v2_"):
			name = strings.TrimPrefix(name, "v2_")
		case strings.HasPrefix(name, "v3_"):
			name = strings.TrimPrefix(name, "v3_")
		default:
			return name
		}
	}
}

func stripOperationIDResourcePrefix(name string, variants []string) string {
	if name == "" || len(variants) == 0 {
		return name
	}

	for {
		stripped := false
		for _, variant := range variants {
			prefix := variant + "_"
			if strings.HasPrefix(name, prefix) {
				name = strings.TrimPrefix(name, prefix)
				stripped = true
				break
			}
		}
		if !stripped {
			return name
		}
	}
}

func stripOperationIDResourceSegments(name string, variants []string) string {
	if name == "" || len(variants) == 0 {
		return name
	}

	tokens := strings.Split(name, "_")
	if len(tokens) == 0 {
		return name
	}

	sequences := make([][]string, 0, len(variants))
	for _, variant := range variants {
		parts := strings.Split(variant, "_")
		if len(parts) == 0 {
			continue
		}
		sequences = append(sequences, parts)
	}

	if len(sequences) == 0 {
		return name
	}

	filtered := make([]string, 0, len(tokens))
	for i := 0; i < len(tokens); {
		matched := false
		for _, sequence := range sequences {
			if len(sequence) == 0 || i+len(sequence) > len(tokens) {
				continue
			}

			sequenceMatches := true
			for j, part := range sequence {
				if tokens[i+j] != part {
					sequenceMatches = false
					break
				}
			}
			if !sequenceMatches {
				continue
			}

			i += len(sequence)
			matched = true
			break
		}
		if matched {
			continue
		}

		filtered = append(filtered, tokens[i])
		i++
	}

	return strings.Join(filtered, "_")
}

func toSnakeCase(input string) string {
	var b strings.Builder
	var prev rune
	lastUnderscore := true

	for i, r := range input {
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

func sanitizeResourceName(name string) string {
	name = strings.ReplaceAll(name, ".", "")
	name = strings.ReplaceAll(name, "/", "")
	name = strings.ReplaceAll(name, "\\", "")
	name = strings.Trim(name, "_")
	if name == "" {
		return ""
	}
	return name
}

func sanitizeTypeName(name string) string {
	name = strings.TrimLeft(name, "$")
	name = strings.NewReplacer(".", "_", "/", "_", "\\", "_", "-", "_", " ", "_").Replace(name)
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
		}
	}
	result := b.String()
	if len(result) > 0 && !unicode.IsLetter(rune(result[0])) {
		result = "T" + result
	}
	return result
}

func toCamelCase(s string) string {
	s = strings.TrimLeft(s, "$")
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' ' || r == '.' || r == '/' || r == '\\' || r == '$' || r == '#' || r == '@'
	})
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	result := strings.Join(parts, "")
	if len(result) > 0 && !unicode.IsLetter(rune(result[0])) {
		result = "V" + result
	}
	return result
}

func toKebabCase(input string) string {
	var b strings.Builder
	lastHyphen := true

	for _, r := range input {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			lastHyphen = false
		case unicode.IsSpace(r):
			if !lastHyphen && b.Len() > 0 {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}

	return strings.Trim(b.String(), "-")
}

func cleanSpecName(title string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	if title == "" {
		return "api"
	}

	title = strings.ReplaceAll(title, "open api", " ")

	var normalized strings.Builder
	lastSpace := true
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			normalized.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			normalized.WriteByte(' ')
			lastSpace = true
		}
	}

	tokens := strings.Fields(normalized.String())
	if len(tokens) == 0 {
		return "api"
	}

	noiseWords := map[string]struct{}{
		"swagger":       {},
		"openapi":       {},
		"rest":          {},
		"api":           {},
		"spec":          {},
		"specification": {},
		"preview":       {},
		"http":          {},
		"with":          {},
		"and":           {},
		"from":          {},
		"for":           {},
		"the":           {},
		"by":            {},
		"of":            {},
		"in":            {},
		"on":            {},
		"to":            {},
		"fixes":         {},
		"improvements":  {},
	}

	filtered := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if _, ok := noiseWords[token]; ok {
			continue
		}
		if isVersionToken(token) {
			continue
		}
		filtered = append(filtered, token)
	}
	if len(filtered) > 3 {
		filtered = filtered[:3]
	}

	name := toKebabCase(strings.Join(filtered, " "))
	if name == "" {
		return "api"
	}
	return name
}

func isVersionToken(token string) bool {
	token = strings.TrimSpace(strings.ToLower(token))
	if token == "" {
		return false
	}

	if strings.HasPrefix(token, "v") {
		token = strings.TrimPrefix(token, "v")
		if token == "" {
			return false
		}
	}

	hasDigit := false
	for _, r := range token {
		if unicode.IsDigit(r) {
			hasDigit = true
			continue
		}
		if r == '.' {
			continue
		}
		return false
	}

	return hasDigit
}

func shouldHumanizeDescription(description string) bool {
	description = strings.TrimSpace(description)
	if description == "" || strings.ContainsAny(description, " \t\r\n") {
		return false
	}
	for i, r := range description {
		if i > 0 && unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func looksLikeMangledOperationID(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	// Single word, no spaces - likely an operationID
	// Check for CamelCase
	if shouldHumanizeDescription(s) {
		return true
	}
	// Check for lowercase concatenated words (e.g. "deleteexternalid")
	// Heuristic: single token > 12 chars with no separators
	if len(s) > 12 && !strings.ContainsAny(s, " _-\t") {
		return true
	}
	return false
}

func humanizeDescription(description string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return ""
	}

	var b strings.Builder
	var prev rune
	for i, r := range description {
		if i > 0 && unicode.IsUpper(r) && unicode.IsLower(prev) {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
		prev = r
	}

	words := strings.Fields(b.String())
	if len(words) == 1 {
		word := strings.ToLower(words[0])
		if strings.HasSuffix(word, "apps") && len(word) > len("apps") {
			words = []string{word[:len(word)-len("apps")], "apps"}
		}
	}

	if len(words) == 0 {
		return ""
	}

	sentence := strings.ToLower(strings.Join(words, " "))
	runes := []rune(sentence)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func toTypeName(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return "Item"
	}

	var parts []string
	var current strings.Builder

	flush := func() {
		if current.Len() == 0 {
			return
		}
		parts = append(parts, current.String())
		current.Reset()
	}

	for i, r := range input {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if i > 0 && unicode.IsUpper(r) {
				prev := rune(input[i-1])
				if unicode.IsLower(prev) || unicode.IsDigit(prev) {
					flush()
				}
			}
			current.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()

	if len(parts) == 0 {
		return "Item"
	}

	var b strings.Builder
	for _, part := range parts {
		part = strings.ToLower(part)
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}

	result := b.String()
	if result == "" {
		return "Item"
	}
	if unicode.IsDigit(rune(result[0])) {
		return "Type" + result
	}
	return result
}

func isRequired(required map[string]struct{}, name string) bool {
	_, ok := required[name]
	return ok
}

func selectDescription(summary, description string) string {
	summaryHasSpaces := summary != "" && strings.ContainsAny(summary, " \t")

	// If summary is a real sentence (has spaces), prefer it
	if summaryHasSpaces {
		return summary
	}

	// Summary is a single word or empty - prefer the description if available
	if description != "" {
		return description
	}

	// No description - use summary, humanizing if it looks like a mangled operationID
	if summary != "" {
		if shouldHumanizeDescription(summary) {
			return humanizeDescription(summary)
		}
		if looksLikeMangledOperationID(summary) {
			return humanizeConcatenated(summary)
		}
		return summary
	}

	return ""
}

func detectPagination(params []spec.Param, op *openapi3.Operation) *spec.Pagination {
	paramNames := map[string]struct{}{}
	for _, p := range params {
		paramNames[strings.ToLower(p.Name)] = struct{}{}
	}

	var pag spec.Pagination

	// Detect limit param
	for _, name := range []string{"limit", "maxresults", "pagesize", "page_size", "max_results", "per_page"} {
		if _, ok := paramNames[name]; ok {
			pag.LimitParam = name
			break
		}
	}

	// Detect cursor param and pagination type
	for _, name := range []string{"pagetoken", "page_token"} {
		if _, ok := paramNames[name]; ok {
			pag.CursorParam = name
			pag.Type = "page_token"
			pag.NextCursorPath = "nextPageToken"
			break
		}
	}
	if pag.Type == "" {
		for _, name := range []string{"after", "cursor"} {
			if _, ok := paramNames[name]; ok {
				pag.CursorParam = name
				pag.Type = "cursor"
				break
			}
		}
	}
	if pag.Type == "" {
		for _, name := range []string{"offset"} {
			if _, ok := paramNames[name]; ok {
				pag.CursorParam = name
				pag.Type = "offset"
				break
			}
		}
	}

	// Also check for has_more in response schemas
	if op != nil && op.Responses != nil {
		success := selectSuccessResponse(op.Responses)
		if success != nil && success.Value != nil {
			schemaRef := selectResponseSchema(success.Value)
			if schemaRef != nil && schemaRef.Value != nil {
				for propName := range schemaRef.Value.Properties {
					lower := strings.ToLower(propName)
					if lower == "nextpagetoken" || lower == "next_page_token" {
						pag.NextCursorPath = propName
						if pag.Type == "" {
							pag.Type = "page_token"
						}
					}
					if lower == "has_more" || lower == "hasmore" {
						pag.HasMoreField = propName
						if pag.Type == "" {
							pag.Type = "cursor"
						}
					}
				}
			}
		}
	}

	// Only return pagination if we detected at least a limit or cursor param
	if pag.LimitParam == "" && pag.CursorParam == "" {
		return nil
	}
	if pag.Type == "" {
		pag.Type = "offset" // default if we found limit but no cursor
	}

	return &pag
}

func humanizeEndpointName(name string) string {
	words := strings.Split(strings.ReplaceAll(name, "_", "-"), "-")
	if len(words) == 0 {
		return ""
	}
	sentence := strings.Join(words, " ")
	runes := []rune(sentence)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func humanizeResourceName(name string) string {
	words := strings.Split(strings.ReplaceAll(name, "_", "-"), "-")
	if len(words) == 0 {
		return ""
	}
	sentence := "Manage " + strings.Join(words, " ")
	return sentence
}

func humanizeConcatenated(s string) string {
	lower := strings.ToLower(s)
	// Try to split on known verb prefixes
	prefixes := []string{"delete", "create", "update", "get", "list", "search", "revoke", "exchange", "rotate", "authenticate", "migrate"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) && len(lower) > len(prefix) {
			rest := lower[len(prefix):]
			words := strings.Fields(strings.ReplaceAll(strings.ReplaceAll(toSnakeCase(rest), "_", " "), "-", " "))
			if len(words) > 0 {
				sentence := cases.Title(language.English).String(prefix) + " " + strings.Join(words, " ")
				return sentence
			}
		}
	}
	return s
}

func humanizeFieldName(name string) string {
	words := strings.Fields(strings.ReplaceAll(strings.ReplaceAll(toSnakeCase(name), "_", " "), "-", " "))
	if len(words) == 0 {
		return ""
	}
	sentence := strings.Join(words, " ")
	runes := []rune(sentence)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "warning: "+format+"\n", args...)
}
