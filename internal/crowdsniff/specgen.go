package crowdsniff

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mvanhorn/cli-printing-press/internal/spec"
)

// BuildSpec assembles a valid spec.APISpec from aggregated endpoints.
// If auth is non-nil and the spec would otherwise default to "none", the
// detected auth is applied.
func BuildSpec(name, baseURL string, endpoints []AggregatedEndpoint, auth *DiscoveredAuth) (*spec.APISpec, error) {
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no endpoints to build spec from")
	}
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("base_url is required")
	}

	resources := make(map[string]spec.Resource)

	for _, ep := range endpoints {
		endpoint := spec.Endpoint{
			Method:      ep.Method,
			Path:        ep.Path,
			Description: fmt.Sprintf("%s %s", ep.Method, ep.Path),
			Meta: map[string]string{
				"source_tier":  ep.SourceTier,
				"source_count": strconv.Itoa(ep.SourceCount),
			},
		}

		// Map DiscoveredParam → spec.Param for each aggregated endpoint's params.
		if ep.Params != nil {
			specParams := make([]spec.Param, len(ep.Params))
			for i, p := range ep.Params {
				var defaultVal any
				if p.Default != "" {
					defaultVal = p.Default
				}
				specParams[i] = spec.Param{
					Name:       p.Name,
					Type:       p.Type,
					Required:   p.Required,
					Positional: false,
					Default:    defaultVal,
				}
			}
			endpoint.Params = specParams
		}

		resourceKey, resourceName := deriveResourceKey(ep.Path)
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

		endpointName := deriveEndpointName(ep.Method, ep.Path)
		if _, exists := resource.Endpoints[endpointName]; exists {
			endpointName = uniqueEndpointName(resource.Endpoints, endpointName)
		}
		resource.Endpoints[endpointName] = endpoint
		resources[resourceKey] = resource
	}

	authConfig := spec.AuthConfig{Type: "none"}
	if auth != nil {
		authConfig = buildAuthConfig(name, auth)
	}

	apiSpec := &spec.APISpec{
		Name:        name,
		Description: fmt.Sprintf("Discovered API spec for %s (crowd-sniff)", name),
		Version:     "0.1.0",
		BaseURL:     baseURL,
		Auth:        authConfig,
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   fmt.Sprintf("~/.config/%s-pp-cli/config.toml", name),
		},
		Resources: resources,
		Types:     map[string]spec.TypeDef{},
	}

	if err := apiSpec.Validate(); err != nil {
		return nil, fmt.Errorf("validating generated spec: %w", err)
	}

	return apiSpec, nil
}

// ResolveBaseURL picks the first non-empty URL from the cascade:
// explicit flag > source candidates (in order).
func ResolveBaseURL(explicit string, candidates []string) string {
	if explicit != "" {
		return explicit
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return ""
}

// --- Helpers adapted from websniff/specgen.go ---

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

// buildAuthConfig converts a DiscoveredAuth into a spec.AuthConfig.
// It also derives an env var name from the API name if no hint was detected.
func buildAuthConfig(apiName string, auth *DiscoveredAuth) spec.AuthConfig {
	cfg := spec.AuthConfig{
		Type:   auth.Type,
		Header: auth.Header,
		In:     auth.In,
		Format: auth.Format,
	}

	envVar := auth.EnvVarHint
	if envVar == "" {
		envVar = deriveEnvVar(apiName, auth.Type)
	}
	if envVar != "" {
		cfg.EnvVars = []string{envVar}
	}

	return cfg
}

// deriveEnvVar generates an environment variable name from the API name and auth type.
// Example: apiName="steam", authType="api_key" → "STEAM_API_KEY"
func deriveEnvVar(apiName, authType string) string {
	// Normalize: replace non-alphanumeric with underscore, uppercase.
	var b strings.Builder
	for _, r := range apiName {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	prefix := strings.ToUpper(b.String())
	prefix = strings.Trim(prefix, "_")

	switch authType {
	case "bearer_token":
		return prefix + "_TOKEN"
	case "api_key":
		return prefix + "_API_KEY"
	case "basic":
		return prefix + "_API_KEY"
	default:
		return prefix + "_API_KEY"
	}
}
