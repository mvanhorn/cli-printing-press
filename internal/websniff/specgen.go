package websniff

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/internal/spec"
	"gopkg.in/yaml.v3"
)

func Analyze(capturePath string) (*spec.APISpec, error) {
	capture, err := LoadCapture(capturePath)
	if err != nil {
		return nil, err
	}

	return AnalyzeCapture(capture)
}

func AnalyzeCapture(capture *EnrichedCapture) (*spec.APISpec, error) {
	if capture == nil {
		return nil, fmt.Errorf("capture is required")
	}

	apiEntries, _ := ClassifyEntries(capture.Entries)
	groups := DeduplicateEndpoints(apiEntries)

	resources := make(map[string]spec.Resource)
	for _, group := range groups {
		endpoint := buildEndpoint(group)
		resourceKey, resourceName := deriveResourceKey(group.NormalizedPath)
		if resourceKey == "" {
			resourceKey = "default"
			resourceName = "default"
		}

		resource := resources[resourceKey]
		if resource.Description == "" {
			resource.Description = fmt.Sprintf("Operations on %s", resourceName)
		}
		if resource.Endpoints == nil {
			resource.Endpoints = make(map[string]spec.Endpoint)
		}

		name := deriveEndpointName(group.Method, group.NormalizedPath)
		if _, exists := resource.Endpoints[name]; exists {
			name = uniqueEndpointName(resource.Endpoints, name)
		}
		resource.Endpoints[name] = endpoint
		resources[resourceKey] = resource
	}

	baseURL := mostCommonBaseURL(apiEntries)
	if baseURL == "" {
		baseURL = normalizeBaseURL(capture.TargetURL)
	}

	nameSource := capture.TargetURL
	if nameSource == "" {
		nameSource = baseURL
	}
	name := deriveNameFromURL(nameSource)

	apiSpec := &spec.APISpec{
		Name:        name,
		Description: fmt.Sprintf("Discovered API spec for %s", name),
		Version:     "0.1.0",
		BaseURL:     baseURL,
		Auth:        detectAuth(capture, apiEntries, name),
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   fmt.Sprintf("~/.config/%s-pp-cli/config.toml", name),
		},
		Resources: resources,
		Types:     map[string]spec.TypeDef{},
	}

	if err := apiSpec.Validate(); err != nil {
		if len(apiSpec.Resources) == 0 && len(groups) > 0 {
			apiSpec.Resources["default"] = spec.Resource{
				Description: "Discovered operations",
				Endpoints:   map[string]spec.Endpoint{},
			}
		}
		if apiSpec.Auth.Type == "" {
			apiSpec.Auth = spec.AuthConfig{Type: "none"}
		}
		if validateErr := apiSpec.Validate(); validateErr != nil {
			return nil, fmt.Errorf("validating generated spec: %w", validateErr)
		}
	}

	return apiSpec, nil
}

func WriteSpec(apiSpec *spec.APISpec, outputPath string) error {
	if apiSpec == nil {
		return fmt.Errorf("api spec is required")
	}

	data, err := yaml.Marshal(apiSpec)
	if err != nil {
		return fmt.Errorf("marshaling spec yaml: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return fmt.Errorf("writing spec yaml: %w", err)
	}

	return nil
}

func DefaultCachePath(name string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".cache", "printing-press", "sniff", name+"-spec.yaml")
	}

	return filepath.Join(home, ".cache", "printing-press", "sniff", name+"-spec.yaml")
}

func buildEndpoint(group EndpointGroup) spec.Endpoint {
	responseBodies := make([]string, 0, len(group.Entries))
	for _, entry := range group.Entries {
		if strings.TrimSpace(entry.ResponseBody) != "" {
			responseBodies = append(responseBodies, entry.ResponseBody)
		}
	}

	body := inferRequestBody(group.Entries)
	params := inferURLParams(group.Entries, group.NormalizedPath)
	auth := detectAuth(nil, group.Entries, "")
	if auth.Type == "api_key" && strings.EqualFold(auth.In, "query") && auth.Header != "" {
		params = filterAuthQueryParam(params, auth.Header)
	}

	responseType := inferResponseType(responseBodies)
	responseFields := InferResponseSchema(responseBodies)
	if len(params) == 0 && len(responseFields) > 0 {
		params = responseFields
	}

	return spec.Endpoint{
		Method:      group.Method,
		Path:        group.NormalizedPath,
		Description: fmt.Sprintf("%s %s", group.Method, group.NormalizedPath),
		Params:      params,
		Body:        body,
		Response: spec.ResponseDef{
			Type: responseType,
			Item: deriveResponseItemName(group.NormalizedPath),
		},
	}
}

func inferRequestBody(entries []EnrichedEntry) []spec.Param {
	for _, entry := range entries {
		body := strings.TrimSpace(entry.RequestBody)
		if body == "" {
			continue
		}

		contentType := getHeaderValue(entry.RequestHeaders, "Content-Type")
		params := InferRequestSchema(body, contentType)
		if len(params) > 0 {
			return params
		}
	}

	return nil
}

func inferURLParams(entries []EnrichedEntry, normalizedPath string) []spec.Param {
	paramsByName := make(map[string]spec.Param)

	for _, segment := range strings.Split(normalizedPath, "/") {
		if !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") {
			continue
		}

		name := strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}")
		paramsByName[name] = spec.Param{
			Name:        name,
			Type:        "string",
			Required:    true,
			Positional:  true,
			Description: fmt.Sprintf("The %s path segment", name),
		}
	}

	for _, entry := range entries {
		parsed, err := url.Parse(entry.URL)
		if err != nil {
			continue
		}

		for key, values := range parsed.Query() {
			if _, exists := paramsByName[key]; exists {
				continue
			}

			value := ""
			if len(values) > 0 {
				value = values[0]
			}

			paramsByName[key] = spec.Param{
				Name:        key,
				Type:        inferScalarStringType(value),
				Required:    false,
				Description: "",
			}
		}
	}

	if len(paramsByName) == 0 {
		return nil
	}

	names := make([]string, 0, len(paramsByName))
	for name := range paramsByName {
		names = append(names, name)
	}
	sort.Strings(names)

	params := make([]spec.Param, 0, len(names))
	for _, name := range names {
		params = append(params, paramsByName[name])
	}

	return params
}

func detectAuth(capture *EnrichedCapture, entries []EnrichedEntry, name string) spec.AuthConfig {
	envPrefix := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	if capture != nil && capture.Auth != nil {
		auth := detectCapturedAuth(capture.Auth, envPrefix)
		if auth.Type != "" {
			return auth
		}
	}

	for _, entry := range entries {
		for headerName, value := range entry.RequestHeaders {
			lowerHeader := strings.ToLower(headerName)
			switch {
			case strings.EqualFold(headerName, "Authorization") && strings.HasPrefix(strings.TrimSpace(value), "Bearer "):
				return spec.AuthConfig{
					Type:    "bearer_token",
					Header:  "Authorization",
					EnvVars: envVarsOrNil(envPrefix, "TOKEN"),
				}
			case strings.Contains(lowerHeader, "api-key") || strings.Contains(lowerHeader, "api_key"):
				return spec.AuthConfig{
					Type:    "api_key",
					Header:  headerName,
					In:      "header",
					EnvVars: envVarsOrNil(envPrefix, "API_KEY"),
				}
			}
		}

		parsed, err := url.Parse(entry.URL)
		if err != nil {
			continue
		}
		for key := range parsed.Query() {
			lowerKey := strings.ToLower(key)
			if strings.Contains(lowerKey, "key") || strings.Contains(lowerKey, "token") {
				return spec.AuthConfig{
					Type:    "api_key",
					Header:  key,
					In:      "query",
					EnvVars: envVarsOrNil(envPrefix, "API_KEY"),
				}
			}
		}
	}

	return spec.AuthConfig{Type: "none"}
}

func detectCapturedAuth(capture *AuthCapture, envPrefix string) spec.AuthConfig {
	if capture == nil {
		return spec.AuthConfig{}
	}

	captureType := strings.ToLower(strings.TrimSpace(capture.Type))
	switch {
	case len(capture.Headers) > 0:
		switch captureType {
		case "bearer":
			return spec.AuthConfig{
				Type:    "bearer_token",
				Header:  "Authorization",
				EnvVars: envVarsOrNil(envPrefix, "TOKEN"),
			}
		case "api_key":
			headerName := firstAuthHeader(capture.Headers)
			if headerName == "" {
				headerName = "X-API-Key"
			}
			return spec.AuthConfig{
				Type:    "api_key",
				Header:  headerName,
				In:      "header",
				EnvVars: envVarsOrNil(envPrefix, "API_KEY"),
			}
		case "cookie":
			return spec.AuthConfig{
				Type:   "cookie",
				Header: "Cookie",
				In:     "cookie",
				Format: "informational only; no template support",
			}
		}
	case captureType == "cookie" && len(capture.Cookies) > 0:
		return spec.AuthConfig{
			Type:   "cookie",
			Header: "Cookie",
			In:     "cookie",
			Format: "informational only; no template support",
		}
	}

	return spec.AuthConfig{}
}

func firstAuthHeader(headers map[string]string) string {
	for _, preferred := range []string{"Authorization", "X-API-Key", "Api-Key", "X-Auth-Token"} {
		for name := range headers {
			if strings.EqualFold(name, preferred) {
				return name
			}
		}
	}

	for name := range headers {
		return name
	}

	return ""
}

func envVarsOrNil(prefix string, suffix string) []string {
	if prefix == "" {
		return nil
	}

	return []string{prefix + "_" + suffix}
}

func mostCommonBaseURL(entries []EnrichedEntry) string {
	counts := make(map[string]int)
	best := ""
	bestCount := 0

	for _, entry := range entries {
		base := normalizeBaseURL(entry.URL)
		if base == "" {
			continue
		}

		counts[base]++
		if counts[base] > bestCount {
			best = base
			bestCount = counts[base]
		}
	}

	return best
}

func normalizeBaseURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}

	return parsed.Scheme + "://" + parsed.Host
}

func deriveResourceKey(path string) (string, string) {
	segments := significantSegments(path)
	if len(segments) == 0 {
		return "", ""
	}

	// Use only the first significant segment as the resource key.
	// This prevents slashes in resource names which break the generator's
	// filepath.Join and Cobra Use field.
	return segments[0], segments[len(segments)-1]
}

func significantSegments(path string) []string {
	parts := strings.Split(path, "/")
	segments := make([]string, 0, len(parts))
	for _, segment := range parts {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			continue
		}
		if segment == "api" || isVersionSegment(segment) {
			continue
		}
		segments = append(segments, segment)
	}

	return segments
}

func isVersionSegment(segment string) bool {
	if len(segment) < 2 || segment[0] != 'v' {
		return false
	}
	for _, r := range segment[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func deriveEndpointName(method string, normalizedPath string) string {
	resource := "endpoint"
	segments := significantSegments(normalizedPath)
	if len(segments) > 0 {
		resource = strings.ReplaceAll(segments[len(segments)-1], "-", "_")
	}

	switch strings.ToUpper(method) {
	case "GET":
		if strings.Contains(normalizedPath, "{") {
			return "get_" + resource
		}
		return "list_" + resource
	case "POST":
		return "create_" + resource
	case "PUT", "PATCH":
		return "update_" + resource
	case "DELETE":
		return "delete_" + resource
	default:
		return strings.ToLower(method) + "_" + resource
	}
}

func uniqueEndpointName(endpoints map[string]spec.Endpoint, base string) string {
	for i := 2; ; i++ {
		name := fmt.Sprintf("%s_%d", base, i)
		if _, exists := endpoints[name]; !exists {
			return name
		}
	}
}

func inferResponseType(bodies []string) string {
	for _, body := range bodies {
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}

		var value any
		if err := json.Unmarshal([]byte(body), &value); err != nil {
			continue
		}

		switch value.(type) {
		case []any:
			return "array"
		case map[string]any:
			return "object"
		}
	}

	return "object"
}

func deriveResponseItemName(path string) string {
	segments := significantSegments(path)
	if len(segments) == 0 {
		return "response"
	}

	return strings.ReplaceAll(segments[len(segments)-1], "-", "_")
}

func filterAuthQueryParam(params []spec.Param, authParam string) []spec.Param {
	filtered := make([]spec.Param, 0, len(params))
	for _, param := range params {
		if strings.EqualFold(param.Name, authParam) {
			continue
		}
		filtered = append(filtered, param)
	}
	return filtered
}

func deriveNameFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return "api"
	}

	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	labels := strings.Split(host, ".")
	if len(labels) == 0 {
		return "api"
	}
	if len(labels) > 2 {
		switch labels[0] {
		case "api", "app", "developer", "developers":
			labels = labels[1:]
		}
	}
	if len(labels) > 1 {
		labels = labels[:len(labels)-1]
	}
	if len(labels) == 0 {
		return "api"
	}

	return strings.Join(labels, "-")
}
