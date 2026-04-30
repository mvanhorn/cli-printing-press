package docspec

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v3/internal/llm"
	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
)

var (
	endpointRe = regexp.MustCompile(`(GET|POST|PUT|PATCH|DELETE)\s+(/[a-zA-Z0-9/{}_.\-]+)`)
	baseURLRe  = regexp.MustCompile(`https://api\.[a-zA-Z0-9.\-]+`)
	bearerRe   = regexp.MustCompile(`(?i)(Bearer|Authorization:\s*Bearer)`)
	apiKeyRe   = regexp.MustCompile(`(?i)(API[_ ]key|api_key|X-API-Key)`)
	oauthRe    = regexp.MustCompile(`(?i)OAuth`)
	paramRowRe = regexp.MustCompile(`(?i)<t[dr][^>]*>\s*(\w+)\s*</t[dr]>\s*<t[dr][^>]*>\s*(string|integer|int|boolean|bool|number|float|array|object)\s*</t[dr]>`)
	jsonKeyRe  = regexp.MustCompile(`"(\w+)"\s*:`)
	preBlockRe = regexp.MustCompile(`(?s)<(?:pre|code)[^>]*>(.*?)</(?:pre|code)>`)
)

// GenerateFromDocs fetches an API documentation page and extracts a best-effort
// APISpec by scanning for endpoint patterns, auth hints, and parameters.
func GenerateFromDocs(docsURL, apiName string) (*spec.APISpec, error) {
	body, err := fetchHTML(docsURL)
	if err != nil {
		return nil, fmt.Errorf("fetching docs: %w", err)
	}

	endpoints := extractEndpoints(body)
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no endpoints found in %s", docsURL)
	}

	resources := groupByResource(endpoints)
	auth := detectAuth(body)
	baseURL := detectBaseURL(body)
	params := extractParams(body)

	// Attach extracted params as body params to POST/PUT/PATCH endpoints
	for resName, res := range resources {
		for epName, ep := range res.Endpoints {
			if ep.Method == "POST" || ep.Method == "PUT" || ep.Method == "PATCH" {
				ep.Body = params
				res.Endpoints[epName] = ep
			}
		}
		resources[resName] = res
	}

	apiSpec := &spec.APISpec{
		Name:        apiName,
		Description: fmt.Sprintf("CLI for %s (generated from docs)", apiName),
		Version:     "1.0.0",
		BaseURL:     baseURL,
		Auth:        auth,
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   fmt.Sprintf("~/.config/%s-pp-cli/config.toml", apiName),
		},
		Resources: resources,
	}

	if err := apiSpec.Validate(); err != nil {
		return nil, fmt.Errorf("generated spec failed validation: %w", err)
	}

	return apiSpec, nil
}

func fetchHTML(url string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

type rawEndpoint struct {
	Method string
	Path   string
}

func extractEndpoints(body string) []rawEndpoint {
	seen := map[string]bool{}
	var endpoints []rawEndpoint

	matches := endpointRe.FindAllStringSubmatch(body, -1)
	for _, m := range matches {
		key := m[1] + " " + m[2]
		if seen[key] {
			continue
		}
		seen[key] = true
		endpoints = append(endpoints, rawEndpoint{Method: m[1], Path: m[2]})
	}
	return endpoints
}

func groupByResource(endpoints []rawEndpoint) map[string]spec.Resource {
	groups := map[string][]rawEndpoint{}

	for _, ep := range endpoints {
		seg := firstSegment(ep.Path)
		groups[seg] = append(groups[seg], ep)
	}

	resources := map[string]spec.Resource{}
	for seg, eps := range groups {
		res := spec.Resource{
			Description: fmt.Sprintf("Operations on %s", seg),
			Endpoints:   map[string]spec.Endpoint{},
		}
		nameCount := map[string]int{}
		for _, ep := range eps {
			name := endpointName(ep.Method, ep.Path)
			nameCount[name]++
			if nameCount[name] > 1 {
				name = fmt.Sprintf("%s_%d", name, nameCount[name])
			}

			pathParams := extractPathParams(ep.Path)

			res.Endpoints[name] = spec.Endpoint{
				Method:      ep.Method,
				Path:        ep.Path,
				Description: fmt.Sprintf("%s %s", ep.Method, ep.Path),
				Params:      pathParams,
				Response: spec.ResponseDef{
					Type: "object",
				},
			}
		}
		resources[seg] = res
	}

	return resources
}

func firstSegment(path string) string {
	path = strings.TrimPrefix(path, "/")
	parts := strings.SplitN(path, "/", 2)
	seg := parts[0]
	// Clean version prefixes like v1, v2
	if len(seg) >= 2 && seg[0] == 'v' && seg[1] >= '0' && seg[1] <= '9' {
		if len(parts) > 1 {
			sub := strings.SplitN(parts[1], "/", 2)
			return sub[0]
		}
	}
	return seg
}

func endpointName(method, path string) string {
	parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
	last := parts[len(parts)-1]

	// If the last segment is a path param like {id}, use the one before it
	if strings.HasPrefix(last, "{") && len(parts) >= 2 {
		last = parts[len(parts)-2]
	}

	// Clean up the segment
	last = strings.ReplaceAll(last, "{", "")
	last = strings.ReplaceAll(last, "}", "")
	last = strings.ReplaceAll(last, "-", "_")

	prefix := strings.ToLower(method)
	switch prefix {
	case "get":
		prefix = "get"
	case "post":
		prefix = "create"
	case "put":
		prefix = "update"
	case "patch":
		prefix = "update"
	case "delete":
		prefix = "delete"
	}

	return prefix + "_" + last
}

func extractPathParams(path string) []spec.Param {
	re := regexp.MustCompile(`\{(\w+)\}`)
	matches := re.FindAllStringSubmatch(path, -1)
	var params []spec.Param
	for _, m := range matches {
		params = append(params, spec.Param{
			Name:        m[1],
			Type:        "string",
			Required:    true,
			Positional:  true,
			Description: fmt.Sprintf("The %s identifier", m[1]),
		})
	}
	return params
}

func detectAuth(body string) spec.AuthConfig {
	upper := strings.ToUpper(body)
	_ = upper

	if bearerRe.MatchString(body) {
		return spec.AuthConfig{
			Type:   "bearer_token",
			Header: "Authorization",
			Format: "Bearer {token}",
			EnvVars: []string{
				"API_TOKEN",
			},
		}
	}
	if apiKeyRe.MatchString(body) {
		return spec.AuthConfig{
			Type:   "api_key",
			Header: "X-API-Key",
			Format: "{token}",
			EnvVars: []string{
				"API_KEY",
			},
		}
	}
	if oauthRe.MatchString(body) {
		return spec.AuthConfig{
			Type:   "oauth2",
			Header: "Authorization",
			Format: "Bearer {token}",
			EnvVars: []string{
				"API_TOKEN",
			},
		}
	}

	// Default
	return spec.AuthConfig{
		Type:   "bearer_token",
		Header: "Authorization",
		Format: "Bearer {token}",
		EnvVars: []string{
			"API_TOKEN",
		},
	}
}

func detectBaseURL(body string) string {
	matches := baseURLRe.FindAllString(body, -1)
	if len(matches) > 0 {
		// Return the first match, preferring longer ones (more specific)
		best := matches[0]
		for _, m := range matches[1:] {
			if len(m) > len(best) {
				best = m
			}
		}
		return best
	}
	return "https://api.example.com"
}

func extractParams(body string) []spec.Param {
	seen := map[string]bool{}
	var params []spec.Param

	// Strategy 1: HTML table rows with name + type
	rowMatches := paramRowRe.FindAllStringSubmatch(body, -1)
	for _, m := range rowMatches {
		name := m[1]
		typ := normalizeType(m[2])
		if !seen[name] {
			seen[name] = true
			params = append(params, spec.Param{
				Name:        name,
				Type:        typ,
				Description: fmt.Sprintf("The %s parameter", name),
			})
		}
	}

	// Strategy 2: JSON keys from <pre>/<code> blocks
	preMatches := preBlockRe.FindAllStringSubmatch(body, -1)
	for _, pm := range preMatches {
		block := pm[1]
		keyMatches := jsonKeyRe.FindAllStringSubmatch(block, -1)
		for _, km := range keyMatches {
			name := km[1]
			if !seen[name] {
				seen[name] = true
				params = append(params, spec.Param{
					Name:        name,
					Type:        "string",
					Description: fmt.Sprintf("The %s parameter", name),
				})
			}
		}
	}

	return params
}

func normalizeType(t string) string {
	switch strings.ToLower(t) {
	case "integer", "int":
		return "integer"
	case "boolean", "bool":
		return "boolean"
	case "number", "float":
		return "number"
	case "array":
		return "array"
	case "object":
		return "object"
	default:
		return "string"
	}
}

// GenerateFromDocsLLM uses an LLM to understand API documentation and produce
// a structured APISpec. Falls back to regex-based GenerateFromDocs on failure.
func GenerateFromDocsLLM(docsURL, apiName string) (*spec.APISpec, error) {
	html, err := fetchHTML(docsURL)
	if err != nil {
		return nil, fmt.Errorf("fetching docs: %w", err)
	}

	// Truncate to ~40K chars to fit LLM context limits
	if len(html) > 40000 {
		html = html[:40000]
	}

	prompt := BuildDocSpecLLMPrompt(apiName, html)

	response, err := llm.Run(prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM doc-to-spec failed: %w", err)
	}

	yamlContent := ExtractYAML(response)

	return spec.ParseBytes([]byte(yamlContent))
}

// BuildDocSpecLLMPrompt constructs a prompt that asks the LLM to read API docs
// and output a YAML spec in the format expected by spec.ParseBytes.
func BuildDocSpecLLMPrompt(apiName, docsContent string) string {
	return fmt.Sprintf(`You are an API documentation expert. Read the following API documentation and produce a YAML spec.

The YAML must follow this exact format:

name: %s
description: "CLI for %s"
version: "1.0.0"
base_url: "https://api.example.com"
auth:
  type: "bearer_token"   # one of: api_key, oauth2, bearer_token, none
  header: "Authorization"
  format: "Bearer {token}"
  env_vars:
    - "API_TOKEN"
config:
  format: "toml"
  path: "~/.config/%s-pp-cli/config.toml"
resources:
  resource_name:
    description: "Operations on resource_name"
    endpoints:
      list:
        method: GET
        path: "/resource_name"
        description: "List all resource_name"
        params: []
        response:
          type: array
      get:
        method: GET
        path: "/resource_name/{id}"
        description: "Get a resource_name by ID"
        params:
          - name: id
            type: string
            required: true
            positional: true
            description: "The resource identifier"
        response:
          type: object
      create:
        method: POST
        path: "/resource_name"
        description: "Create a new resource_name"
        body:
          - name: field_name
            type: string
            description: "Field description"
        response:
          type: object

Rules:
- Extract ALL endpoints you can find in the docs
- Detect the correct auth type from the documentation
- Extract the real base URL from the docs
- Group endpoints by resource (the first path segment after version prefix)
- Every resource must have at least one endpoint
- Every endpoint must have method and path
- Output ONLY the YAML, no explanation or markdown fences

API Documentation:
%s`, apiName, apiName, apiName, docsContent)
}

// ExtractYAML strips markdown code fences from LLM output to get raw YAML.
func ExtractYAML(response string) string {
	s := strings.TrimSpace(response)

	// Remove ```yaml ... ``` wrapper if present
	if strings.HasPrefix(s, "```yaml") {
		s = s[len("```yaml"):]
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
		return strings.TrimSpace(s)
	}

	// Remove ``` ... ``` wrapper if present
	if strings.HasPrefix(s, "```") {
		s = s[len("```"):]
		// Skip the rest of the first line (could be "yaml\n" or just "\n")
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
		return strings.TrimSpace(s)
	}

	return s
}
