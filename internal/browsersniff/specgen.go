package browsersniff

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/mvanhorn/cli-printing-press/internal/spec"
	"gopkg.in/yaml.v3"
)

type graphQLOperationGroup struct {
	Method        string
	Path          string
	OperationName string
	Entries       []EnrichedEntry
	SamplePayload map[string]any
}

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

	apiEntries, noiseEntries := ClassifyEntries(capture.Entries)

	resources := make(map[string]spec.Resource)
	graphQLOps, graphQLBFFKeys := detectGraphQLBFFOperations(apiEntries)
	if len(graphQLOps) > 0 {
		addGraphQLBFFResources(resources, graphQLOps)
	}

	htmlEntries := discoverHTMLSurfaceEntries(noiseEntries, capture.TargetURL)

	regularEntries := make([]EnrichedEntry, 0, len(apiEntries)+len(htmlEntries))
	for _, entry := range apiEntries {
		method := strings.ToUpper(strings.TrimSpace(entry.Method))
		key := method + " " + normalizeEntryPath(entry.URL)
		if graphQLBFFKeys[key] {
			continue
		}
		regularEntries = append(regularEntries, entry)
	}
	regularEntries = append(regularEntries, htmlEntries...)

	groups := DeduplicateEndpoints(regularEntries)
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

	baseURL := mostCommonBaseURL(regularEntries)
	if baseURL == "" {
		baseURL = normalizeBaseURL(capture.TargetURL)
	}

	nameSource := capture.TargetURL
	if normalizeBaseURL(nameSource) == "" {
		nameSource = baseURL
	}
	name := deriveNameFromURL(nameSource)

	apiSpec := &spec.APISpec{
		Name:        name,
		Description: fmt.Sprintf("Discovered API spec for %s", name),
		Version:     "0.1.0",
		BaseURL:     baseURL,
		SpecSource:  "sniffed",
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

func detectGraphQLBFFOperations(entries []EnrichedEntry) ([]graphQLOperationGroup, map[string]bool) {
	type bucketKey struct {
		method string
		path   string
	}

	buckets := make(map[bucketKey][]EnrichedEntry)
	for _, entry := range entries {
		if !isGraphQL(entry) {
			continue
		}
		payload := graphqlRequestPayload(entry)
		if graphqlPayloadOperationName(payload, entry.URL) == "" {
			continue
		}
		method := strings.ToUpper(strings.TrimSpace(entry.Method))
		if method == "" {
			method = "GET"
		}
		key := bucketKey{method: method, path: normalizeEntryPath(entry.URL)}
		buckets[key] = append(buckets[key], entry)
	}

	var bestKey bucketKey
	var bestEntries []EnrichedEntry
	for key, bucketEntries := range buckets {
		if len(bucketEntries) > len(bestEntries) {
			bestKey = key
			bestEntries = bucketEntries
		}
	}
	if len(bestEntries) == 0 || len(bestEntries)*2 <= len(entries) {
		return nil, nil
	}

	byOperation := make(map[string][]EnrichedEntry)
	samples := make(map[string]map[string]any)
	for _, entry := range bestEntries {
		payload := graphqlRequestPayload(entry)
		operationName := graphqlPayloadOperationName(payload, entry.URL)
		if operationName == "" {
			continue
		}
		byOperation[operationName] = append(byOperation[operationName], entry)
		if samples[operationName] == nil && len(payload) > 0 {
			samples[operationName] = payload
		}
	}
	if len(byOperation) < 2 {
		return nil, nil
	}

	names := make([]string, 0, len(byOperation))
	for name := range byOperation {
		names = append(names, name)
	}
	sort.Strings(names)

	ops := make([]graphQLOperationGroup, 0, len(names))
	for _, name := range names {
		ops = append(ops, graphQLOperationGroup{
			Method:        bestKey.method,
			Path:          bestKey.path,
			OperationName: name,
			Entries:       byOperation[name],
			SamplePayload: samples[name],
		})
	}
	return ops, map[string]bool{bestKey.method + " " + bestKey.path: true}
}

func addGraphQLBFFResources(resources map[string]spec.Resource, ops []graphQLOperationGroup) {
	for _, op := range ops {
		resourceName, endpointName := graphQLBFFCommandPath(op.OperationName)
		resource := resources[resourceName]
		if resource.Description == "" {
			resource.Description = fmt.Sprintf("GraphQL BFF operations for %s", strings.ReplaceAll(resourceName, "_", " "))
		}
		if resource.Endpoints == nil {
			resource.Endpoints = make(map[string]spec.Endpoint)
		}

		endpoint := buildGraphQLOperationEndpoint(op, resourceName, endpointName)
		name := endpointName
		if name == "" {
			name = safeGraphQLOperationName(op.OperationName)
		}
		if name == "" {
			name = deriveEndpointName(op.Method, op.Path)
		}
		if _, exists := resource.Endpoints[name]; exists {
			name = uniqueEndpointName(resource.Endpoints, name)
		}
		resource.Endpoints[name] = endpoint
		resources[resourceName] = resource
	}
}

func buildGraphQLOperationEndpoint(op graphQLOperationGroup, resourceName string, endpointName string) spec.Endpoint {
	responseBodies := make([]string, 0, len(op.Entries))
	for _, entry := range op.Entries {
		if strings.TrimSpace(entry.ResponseBody) != "" {
			responseBodies = append(responseBodies, entry.ResponseBody)
		}
	}

	payloadParams := graphqlPayloadParams(op)
	endpoint := spec.Endpoint{
		Method:      op.Method,
		Path:        op.Path,
		Description: graphQLBFFCommandDescription(resourceName, endpointName),
		Response: spec.ResponseDef{
			Type: inferResponseType(responseBodies),
			Item: safeGraphQLOperationName(op.OperationName),
		},
	}
	switch strings.ToUpper(op.Method) {
	case "GET", "HEAD":
		endpoint.Params = payloadParams
	default:
		endpoint.Body = payloadParams
	}
	return endpoint
}

func graphqlPayloadParams(op graphQLOperationGroup) []spec.Param {
	params := []spec.Param{
		{
			Name:        "operationName",
			Type:        "string",
			Required:    true,
			Default:     op.OperationName,
			Description: "GraphQL operation name",
		},
	}

	if query, ok := op.SamplePayload["query"].(string); ok && strings.TrimSpace(query) != "" {
		params = append(params, spec.Param{
			Name:        "query",
			Type:        "string",
			Required:    false,
			Default:     query,
			Description: "GraphQL query document",
		})
	}

	variables, _ := op.SamplePayload["variables"].(map[string]any)
	if variables == nil {
		variables = map[string]any{}
	}
	params = append(params, spec.Param{
		Name:        "variables",
		Type:        "object",
		Required:    false,
		Default:     variables,
		Description: "GraphQL variables as JSON",
	})

	if extensions, ok := op.SamplePayload["extensions"].(map[string]any); ok && len(extensions) > 0 {
		params = append(params, spec.Param{
			Name:        "extensions",
			Type:        "object",
			Required:    false,
			Default:     extensions,
			Description: "GraphQL extensions as JSON",
		})
	}
	return params
}

func graphqlRequestPayload(entry EnrichedEntry) map[string]any {
	body := strings.TrimSpace(entry.RequestBody)
	if body != "" {
		var payload map[string]any
		if err := json.Unmarshal([]byte(body), &payload); err == nil {
			return payload
		}
		var batch []map[string]any
		if err := json.Unmarshal([]byte(body), &batch); err == nil && len(batch) > 0 {
			return batch[0]
		}
	}

	parsed, err := url.Parse(entry.URL)
	if err != nil {
		return nil
	}
	query := parsed.Query()
	payload := map[string]any{}
	if operationName := query.Get("operationName"); operationName != "" {
		payload["operationName"] = operationName
	}
	if rawVariables := query.Get("variables"); rawVariables != "" {
		var variables map[string]any
		if err := json.Unmarshal([]byte(rawVariables), &variables); err == nil {
			payload["variables"] = variables
		}
	}
	if rawExtensions := query.Get("extensions"); rawExtensions != "" {
		var extensions map[string]any
		if err := json.Unmarshal([]byte(rawExtensions), &extensions); err == nil {
			payload["extensions"] = extensions
		}
	}
	if len(payload) == 0 {
		return nil
	}
	return payload
}

func graphqlPayloadOperationName(payload map[string]any, rawURL string) string {
	if value, ok := payload["operationName"].(string); ok {
		return strings.TrimSpace(value)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Query().Get("operationName"))
}

func graphqlPayloadPersistedQueryHash(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	extensions, _ := payload["extensions"].(map[string]any)
	if extensions == nil {
		return ""
	}
	persistedQuery, _ := extensions["persistedQuery"].(map[string]any)
	if persistedQuery == nil {
		return ""
	}
	hash, _ := persistedQuery["sha256Hash"].(string)
	return strings.TrimSpace(hash)
}

func safeGraphQLOperationName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	var out []rune
	var prevUnderscore bool
	for i, r := range name {
		if r == '-' || r == ' ' || r == '.' || r == '/' {
			if !prevUnderscore && len(out) > 0 {
				out = append(out, '_')
				prevUnderscore = true
			}
			continue
		}
		if unicode.IsUpper(r) {
			if i > 0 && !prevUnderscore && len(out) > 0 {
				out = append(out, '_')
			}
			out = append(out, unicode.ToLower(r))
			prevUnderscore = false
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			out = append(out, unicode.ToLower(r))
			prevUnderscore = r == '_'
		}
	}

	result := strings.Trim(string(out), "_")
	if result == "" {
		return ""
	}
	if result[0] >= '0' && result[0] <= '9' {
		result = "op_" + result
	}
	for strings.Contains(result, "__") {
		result = strings.ReplaceAll(result, "__", "_")
	}
	return result
}

func graphQLBFFCommandPath(operationName string) (string, string) {
	normalized := safeGraphQLOperationName(operationName)
	if normalized == "" {
		return "graphql", ""
	}

	rawTokens := strings.Split(normalized, "_")
	if graphQLBFFSiteOperation(rawTokens) {
		return "site", graphQLBFFSiteEndpoint(rawTokens)
	}

	tokens := make([]string, 0, len(rawTokens))
	for _, token := range rawTokens {
		token = strings.TrimSpace(token)
		if token == "" || graphQLBFFCommandStopWord(token) {
			continue
		}
		tokens = append(tokens, token)
	}
	if len(tokens) == 0 {
		return "graphql", normalized
	}
	for len(tokens) > 1 && graphQLBFFCommandActionVerb(tokens[0]) {
		tokens = tokens[1:]
	}

	resource := pluralizeCommandNoun(tokens[0])
	endpoint := "get"
	if len(tokens) > 1 {
		endpoint = strings.Join(tokens[1:], "_")
	}
	return resource, endpoint
}

func graphQLBFFSiteOperation(tokens []string) bool {
	for _, token := range tokens {
		switch token {
		case "header", "footer", "navigation", "nav":
			return true
		}
	}
	return false
}

func graphQLBFFSiteEndpoint(tokens []string) string {
	for _, token := range tokens {
		if token == "navigation" || token == "nav" {
			return "navigation"
		}
	}

	filtered := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token == "" || graphQLBFFCommandActionVerb(token) || graphQLBFFCommandStopWord(token) {
			continue
		}
		filtered = append(filtered, token)
	}
	if len(filtered) == 0 {
		return "get"
	}
	return strings.Join(filtered, "_")
}

func graphQLBFFCommandDescription(resourceName string, endpointName string) string {
	resource := strings.ReplaceAll(strings.TrimSpace(resourceName), "_", " ")
	endpoint := strings.ReplaceAll(strings.TrimSpace(endpointName), "_", " ")
	if resource == "" {
		resource = "GraphQL data"
	}
	if endpoint == "" || endpoint == "get" {
		return fmt.Sprintf("Fetch %s", resource)
	}
	if resource == "site" {
		return fmt.Sprintf("Fetch site %s", endpoint)
	}
	return fmt.Sprintf("Fetch %s %s", resource, endpoint)
}

func graphQLBFFCommandActionVerb(token string) bool {
	switch token {
	case "get", "list", "fetch", "find", "search", "query", "load", "read", "watch", "lookup":
		return true
	default:
		return false
	}
}

func graphQLBFFCommandStopWord(token string) bool {
	switch token {
	case "query", "mutation", "subscription", "page", "screen", "view", "component":
		return true
	case "header", "footer", "desktop", "mobile", "navigation", "nav":
		return true
	case "detail", "details", "detailed":
		return true
	default:
		return false
	}
}

func pluralizeCommandNoun(noun string) string {
	if noun == "" || strings.HasSuffix(noun, "s") {
		return noun
	}
	if strings.HasSuffix(noun, "y") && len(noun) > 1 {
		prev := noun[len(noun)-2]
		if !strings.ContainsRune("aeiou", rune(prev)) {
			return strings.TrimSuffix(noun, "y") + "ies"
		}
	}
	for _, suffix := range []string{"ch", "sh", "x", "z"} {
		if strings.HasSuffix(noun, suffix) {
			return noun + "es"
		}
	}
	return noun + "s"
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

	endpoint := spec.Endpoint{
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
	if groupLooksHTML(group) {
		endpoint.ResponseFormat = spec.ResponseFormatHTML
		endpoint.Response = spec.ResponseDef{
			Type: htmlResponseType(group),
			Item: "html",
		}
		endpoint.HTMLExtract = inferHTMLExtract(group)
		endpoint.Description = htmlEndpointDescription(group)
	}
	return endpoint
}

func discoverHTMLSurfaceEntries(entries []EnrichedEntry, targetURL string) []EnrichedEntry {
	targetHost := extractHost(targetURL)
	if normalizeBaseURL(targetURL) == "" {
		targetHost = mostCommonHTMLSurfaceHost(entries)
	}
	var out []EnrichedEntry
	seen := map[string]bool{}
	for _, entry := range entries {
		if !isUsefulHTMLSurfaceEntry(entry, targetHost) {
			continue
		}
		method := strings.ToUpper(strings.TrimSpace(entry.Method))
		if method == "" {
			method = "GET"
		}
		entry.URL = normalizeHTMLSurfaceURL(entry.URL)
		key := method + " " + normalizeEntryPath(entry.URL)
		if seen[key] {
			continue
		}
		seen[key] = true
		entry.Method = method
		entry.Classification = "api"
		entry.IsNoise = false
		out = append(out, entry)
	}
	return out
}

func mostCommonHTMLSurfaceHost(entries []EnrichedEntry) string {
	counts := map[string]int{}
	order := []string{}
	for _, entry := range entries {
		if !isUsefulHTMLSurfaceEntry(entry, "") {
			continue
		}
		host := extractHost(entry.URL)
		if host != "" {
			if counts[host] == 0 {
				order = append(order, host)
			}
			counts[host]++
		}
	}
	bestHost := ""
	bestCount := 0
	for _, host := range order {
		if counts[host] > bestCount {
			bestHost = host
			bestCount = counts[host]
		}
	}
	return bestHost
}

func isUsefulHTMLSurfaceEntry(entry EnrichedEntry, targetHost string) bool {
	method := strings.ToUpper(strings.TrimSpace(entry.Method))
	if method != "" && method != "GET" && method != "HEAD" {
		return false
	}
	if entry.ResponseStatus < 200 || entry.ResponseStatus >= 400 {
		return false
	}
	if !strings.Contains(strings.ToLower(entry.ResponseContentType), "html") {
		return false
	}
	if targetHost != "" {
		host := extractHost(entry.URL)
		if host != "" && !strings.EqualFold(host, targetHost) {
			return false
		}
	}
	path := strings.ToLower(extractPath(entry.URL))
	for _, suffix := range []string{".js", ".css", ".png", ".jpg", ".jpeg", ".webp", ".svg", ".ico", ".woff", ".woff2"} {
		if strings.HasSuffix(path, suffix) {
			return false
		}
	}
	body := strings.TrimSpace(entry.ResponseBody)
	if len(body) < 64 || htmlChallengeBody(body) {
		return false
	}
	lower := strings.ToLower(body)
	return strings.Contains(lower, "<title") ||
		strings.Contains(lower, "<meta") ||
		strings.Contains(lower, "<a ")
}

func normalizeHTMLSurfaceURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return rawURL
	}
	normalizedPath := normalizeHTMLSurfacePath(parsed.Path)
	if normalizedPath == parsed.Path {
		return rawURL
	}
	result := parsed.Scheme + "://" + parsed.Host + normalizedPath
	if parsed.RawQuery != "" {
		result += "?" + parsed.RawQuery
	}
	return result
}

func normalizeHTMLSurfacePath(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if i > 0 && htmlCollectionSlugSegment(segments[i-1], segment) {
			segments[i] = "{slug}"
		}
	}
	normalized := strings.Join(segments, "/")
	if normalized == "" {
		return "/"
	}
	return normalized
}

func htmlCollectionSlugSegment(previous string, segment string) bool {
	previous = strings.TrimSpace(strings.ToLower(previous))
	segment = strings.TrimSpace(strings.ToLower(segment))
	if previous == "" || segment == "" {
		return false
	}
	if strings.HasPrefix(segment, "{") || strings.Contains(segment, ".") || !htmlSlugSegment(segment) {
		return false
	}
	switch segment {
	case "new", "edit", "search", "settings", "login", "logout", "signin", "signup", "current", "me", "self", "profile", "daily", "weekly", "monthly", "yearly":
		return false
	}
	switch previous {
	case "products", "posts", "topics", "categories", "makers", "users":
		return true
	default:
		return false
	}
}

func htmlSlugSegment(segment string) bool {
	if len(segment) < 3 {
		return false
	}
	for _, r := range segment {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func groupLooksHTML(group EndpointGroup) bool {
	for _, entry := range group.Entries {
		if strings.Contains(strings.ToLower(entry.ResponseContentType), "html") {
			return true
		}
	}
	return false
}

func htmlResponseType(group EndpointGroup) string {
	if inferHTMLExtract(group).EffectiveMode() == spec.HTMLExtractModeLinks {
		return "array"
	}
	return "object"
}

func inferHTMLExtract(group EndpointGroup) *spec.HTMLExtract {
	prefixes := inferHTMLLinkPrefixes(group.Entries)
	mode := spec.HTMLExtractModePage
	if len(prefixes) > 0 && !strings.Contains(group.NormalizedPath, "{") {
		mode = spec.HTMLExtractModeLinks
	}
	return &spec.HTMLExtract{
		Mode:         mode,
		LinkPrefixes: prefixes,
		Limit:        50,
	}
}

func inferHTMLLinkPrefixes(entries []EnrichedEntry) []string {
	counts := map[string]int{}
	for _, entry := range entries {
		for _, prefix := range htmlLinkPrefixesInBody(entry.ResponseBody) {
			counts[prefix]++
		}
	}
	if len(counts) == 0 {
		return nil
	}
	type prefixCount struct {
		prefix string
		count  int
	}
	values := make([]prefixCount, 0, len(counts))
	for prefix, count := range counts {
		values = append(values, prefixCount{prefix: prefix, count: count})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].count == values[j].count {
			return values[i].prefix < values[j].prefix
		}
		return values[i].count > values[j].count
	})
	limit := len(values)
	if limit > 3 {
		limit = 3
	}
	prefixes := make([]string, 0, limit)
	for _, value := range values[:limit] {
		prefixes = append(prefixes, value.prefix)
	}
	return prefixes
}

func htmlLinkPrefixesInBody(body string) []string {
	lower := strings.ToLower(body)
	candidates := []string{"/products/", "/posts/", "/topics/", "/categories/", "/users/", "/@"}
	var prefixes []string
	for _, candidate := range candidates {
		if strings.Contains(lower, `href="`+candidate) || strings.Contains(lower, `href='`+candidate) {
			prefixes = append(prefixes, strings.TrimSuffix(candidate, "/"))
		}
	}
	return prefixes
}

func htmlEndpointDescription(group EndpointGroup) string {
	path := group.NormalizedPath
	if path == "/" || path == "" {
		return "Fetch structured links from the website homepage"
	}
	if strings.Contains(path, "{") {
		return fmt.Sprintf("Fetch structured metadata from %s", path)
	}
	return fmt.Sprintf("Fetch structured links from %s", path)
}

func htmlChallengeBody(body string) bool {
	lower := strings.ToLower(body)
	markers := []string{
		"<title>just a moment",
		"cf-browser-verification",
		"cf-challenge",
		"cf-mitigated",
		"_cf_chl_",
		"challenge-platform",
		"verify you are human",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
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
				Type:         "cookie",
				Header:       "Cookie",
				In:           "cookie",
				CookieDomain: capture.BoundDomain,
				EnvVars:      envVarsOrNil(envPrefix, "COOKIES"),
			}
		case "composed":
			headerName := firstAuthHeader(capture.Headers)
			if headerName == "" {
				headerName = "Authorization"
			}
			return spec.AuthConfig{
				Type:         "composed",
				Header:       headerName,
				Format:       capture.Format,
				CookieDomain: capture.BoundDomain,
				Cookies:      capture.Cookies,
			}
		}
	case captureType == "cookie" && len(capture.Cookies) > 0:
		return spec.AuthConfig{
			Type:         "cookie",
			Header:       "Cookie",
			In:           "cookie",
			CookieDomain: capture.BoundDomain,
			EnvVars:      envVarsOrNil(envPrefix, "COOKIES"),
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
