package crowdsniff

import (
	"regexp"
	"strings"
)

// Shared grep patterns for extracting endpoints from SDK source code.
// Used by both the NPM source (Unit 3) and GitHub source (Unit 4).

var (
	// urlPathLiteral matches URL path literals in quoted strings:
	//   "/v1/users", '/api/projects', `/users/${id}`
	urlPathLiteral = regexp.MustCompile(`["'` + "`]" + `(/[a-zA-Z0-9_\-/{}$:.]+)["'` + "`]")

	// httpMethodCall matches common SDK patterns for HTTP method calls:
	//   this.get("/path"), this.post("/path"), client.get("/path"),
	//   fetch("/path"), axios.get("/path"), http.get("/path")
	httpMethodCall = regexp.MustCompile(`(?i)\b(?:this|self|client|api|http|axios|request)\s*\.\s*(get|post|put|patch|delete|head|options)\s*\(`)

	// fetchCall matches fetch("url") or fetch('url') or fetch(`url`)
	fetchCall = regexp.MustCompile(`(?i)\bfetch\s*\(\s*["'` + "`]" + `([^"'` + "`" + `]+)["'` + "`]")

	// requestMethodLiteral matches .request({method: "GET", ...}) patterns
	requestMethodLiteral = regexp.MustCompile(`(?i)method\s*:\s*["']?(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)["']?`)

	// baseURLPattern matches base URL constant assignments:
	//   baseUrl = "https://...", BASE_URL = "https://...",
	//   this.baseUrl = "https://...", apiBase = "https://..."
	baseURLPattern = regexp.MustCompile(`(?i)(?:base_?url|api_?base|base_?uri|api_?url|host|endpoint)\s*[:=]\s*["'` + "`]" + `(https?://[^"'` + "`" + `\s]+)["'` + "`]")

	// validHTTPMethods is the set of methods we recognize.
	validHTTPMethods = map[string]bool{
		"GET": true, "POST": true, "PUT": true, "PATCH": true,
		"DELETE": true, "HEAD": true, "OPTIONS": true,
	}
)

// GrepEndpoints scans source code content for API endpoint patterns.
// It returns discovered endpoints and base URL candidates.
func GrepEndpoints(content, sourceName, sourceTier string) ([]DiscoveredEndpoint, []string) {
	var endpoints []DiscoveredEndpoint
	var baseURLs []string

	lines := strings.Split(content, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
			continue
		}

		// Extract base URL candidates.
		if matches := baseURLPattern.FindStringSubmatch(line); len(matches) > 1 {
			baseURLs = append(baseURLs, matches[1])
		}

		// Try to extract method + path from method call patterns.
		eps := extractMethodCallEndpoints(line, sourceName, sourceTier)
		endpoints = append(endpoints, eps...)

		// Try to extract from fetch() calls.
		eps = extractFetchEndpoints(line, sourceName, sourceTier)
		endpoints = append(endpoints, eps...)

		// Try to extract paths from URL path literals when near an HTTP method.
		eps = extractContextualPathEndpoints(line, sourceName, sourceTier)
		endpoints = append(endpoints, eps...)
	}

	return deduplicateEndpoints(endpoints), deduplicateStrings(baseURLs)
}

// extractMethodCallEndpoints handles patterns like:
//
//	this.get("/v1/users"), client.post("/users")
func extractMethodCallEndpoints(line, sourceName, sourceTier string) []DiscoveredEndpoint {
	methodIndexes := httpMethodCall.FindAllStringSubmatchIndex(line, -1)
	if len(methodIndexes) == 0 {
		return nil
	}

	var endpoints []DiscoveredEndpoint
	for _, methodIdx := range methodIndexes {
		// methodIdx[2]:methodIdx[3] is the capture group (method name).
		method := strings.ToUpper(line[methodIdx[2]:methodIdx[3]])
		if !validHTTPMethods[method] {
			continue
		}

		// Find the first URL path after this method call's opening paren.
		remainder := line[methodIdx[1]:]
		pathMatch := urlPathLiteral.FindStringSubmatch(remainder)
		if pathMatch == nil {
			continue
		}
		path := cleanPath(pathMatch[1])
		if isValidAPIPath(path) {
			endpoints = append(endpoints, DiscoveredEndpoint{
				Method:     method,
				Path:       path,
				SourceTier: sourceTier,
				SourceName: sourceName,
			})
		}
	}
	return endpoints
}

// extractFetchEndpoints handles fetch("/path") patterns.
func extractFetchEndpoints(line, sourceName, sourceTier string) []DiscoveredEndpoint {
	matches := fetchCall.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		return nil
	}

	// Check if there's an explicit method in the same line or nearby.
	method := "GET" // default for fetch
	if methodMatch := requestMethodLiteral.FindStringSubmatch(line); len(methodMatch) > 1 {
		method = strings.ToUpper(methodMatch[1])
	}

	var endpoints []DiscoveredEndpoint
	for _, match := range matches {
		path := cleanPath(match[1])
		if isValidAPIPath(path) {
			endpoints = append(endpoints, DiscoveredEndpoint{
				Method:     method,
				Path:       path,
				SourceTier: sourceTier,
				SourceName: sourceName,
			})
		}
	}
	return endpoints
}

// extractContextualPathEndpoints handles lines that have both an HTTP method
// reference and a path literal, but not in a standard call pattern.
// Example: .request({method: "POST", url: "/v1/users"})
func extractContextualPathEndpoints(line, sourceName, sourceTier string) []DiscoveredEndpoint {
	methodMatch := requestMethodLiteral.FindStringSubmatch(line)
	if len(methodMatch) < 2 {
		return nil
	}
	method := strings.ToUpper(methodMatch[1])

	paths := urlPathLiteral.FindAllStringSubmatch(line, -1)
	if len(paths) == 0 {
		return nil
	}

	var endpoints []DiscoveredEndpoint
	for _, pathMatch := range paths {
		path := cleanPath(pathMatch[1])
		if isValidAPIPath(path) {
			endpoints = append(endpoints, DiscoveredEndpoint{
				Method:     method,
				Path:       path,
				SourceTier: sourceTier,
				SourceName: sourceName,
			})
		}
	}
	return endpoints
}

// cleanPath normalizes a path extracted from source code.
// It removes template literal syntax like ${...} -> {id} and trims trailing slashes.
func cleanPath(path string) string {
	// Replace template literal interpolation ${varName} with {id}.
	templateVar := regexp.MustCompile(`\$\{[^}]+\}`)
	path = templateVar.ReplaceAllString(path, "{id}")

	// Trim trailing slash (but keep leading).
	path = strings.TrimRight(path, "/")
	if path == "" {
		return "/"
	}
	return path
}

// isValidAPIPath checks if an extracted path looks like an API path
// rather than a file path, import path, or other false positive.
func isValidAPIPath(path string) bool {
	if !strings.HasPrefix(path, "/") {
		return false
	}
	// Reject file extensions that are clearly not API paths.
	lower := strings.ToLower(path)
	for _, ext := range []string{".js", ".ts", ".mjs", ".cjs", ".json", ".css", ".html", ".md", ".yaml", ".yml", ".png", ".jpg", ".svg", ".ico"} {
		if strings.HasSuffix(lower, ext) {
			return false
		}
	}
	// Reject paths that look like node_modules imports.
	if strings.Contains(path, "node_modules") {
		return false
	}
	// Must have at least one meaningful segment.
	segments := strings.Split(path, "/")
	meaningfulCount := 0
	for _, s := range segments {
		if s != "" {
			meaningfulCount++
		}
	}
	return meaningfulCount >= 1
}

// deduplicateEndpoints removes duplicate endpoints (same method+path+source).
func deduplicateEndpoints(endpoints []DiscoveredEndpoint) []DiscoveredEndpoint {
	type key struct {
		method     string
		path       string
		sourceName string
	}
	seen := make(map[key]struct{})
	var result []DiscoveredEndpoint
	for _, ep := range endpoints {
		k := key{method: ep.Method, path: ep.Path, sourceName: ep.SourceName}
		if _, exists := seen[k]; exists {
			continue
		}
		seen[k] = struct{}{}
		result = append(result, ep)
	}
	return result
}
