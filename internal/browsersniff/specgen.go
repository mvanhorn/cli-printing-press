package browsersniff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/mvanhorn/cli-printing-press/v4/internal/discovery"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
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
		resourceKey, resourceName := discovery.ResourceKey(group.NormalizedPath)
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

		name := discovery.EndpointName(group.Method, group.NormalizedPath)
		if _, exists := resource.Endpoints[name]; exists {
			name = discovery.UniqueEndpointName(resource.Endpoints, name)
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
			name = discovery.EndpointName(op.Method, op.Path)
		}
		if _, exists := resource.Endpoints[name]; exists {
			name = discovery.UniqueEndpointName(resource.Endpoints, name)
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
		Method:       op.Method,
		Path:         op.Path,
		Description:  graphQLBFFCommandDescription(resourceName, endpointName),
		ObservedAuth: observedAuthHeaders(op.Entries),
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
		Method:       group.Method,
		Path:         group.NormalizedPath,
		Description:  fmt.Sprintf("%s %s", group.Method, group.NormalizedPath),
		Params:       params,
		Body:         body,
		ObservedAuth: observedAuthHeaders(group.Entries),
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
	limit := min(len(values), 3)
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

	for segment := range strings.SplitSeq(normalizedPath, "/") {
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
	auth0SPA := detectAuth0SPAInMemory(entries)
	if capture != nil && capture.Auth != nil {
		auth := detectCapturedAuth(capture.Auth, envPrefix)
		if auth.Type != "" {
			// Capture-derived bearer auth with an Auth0-SPA-in-memory signature
			// gets the subtype annotation so the generator routes to the CDP
			// extractor; other auth shapes (cookie, composed, api_key) are
			// reached by their own extractor paths and don't need the hint.
			if auth0SPA && auth.Type == "bearer_token" {
				auth.Subtype = spec.AuthSubtypeAuth0SPAInMemory
			}
			return auth
		}
	}

	for _, entry := range entries {
		for headerName, value := range entry.RequestHeaders {
			lowerHeader := strings.ToLower(headerName)
			switch {
			case strings.EqualFold(headerName, "Authorization") && strings.HasPrefix(strings.TrimSpace(value), "Bearer "):
				auth := spec.AuthConfig{
					Type:    "bearer_token",
					Header:  "Authorization",
					EnvVars: envVarsOrNil(envPrefix, "TOKEN"),
				}
				if auth0SPA {
					auth.Subtype = spec.AuthSubtypeAuth0SPAInMemory
				}
				return auth
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

// observedAuthHeaders returns the sorted set of lowercased request header
// names observed across entries that match common auth surfaces
// (Authorization, Cookie, X-CSRF-Token, X-API-Key, etc., plus contains-style
// matches on token / secret / signature / api-key). Values are never
// inspected or returned; only the header NAME travels. Returns nil when
// nothing matched so consumers using `omitempty` can drop the field cleanly.
func observedAuthHeaders(entries []EnrichedEntry) []string {
	seen := map[string]bool{}
	for _, entry := range entries {
		for name := range entry.RequestHeaders {
			lower := strings.ToLower(strings.TrimSpace(name))
			if lower == "" {
				continue
			}
			if isObservedAuthHeaderName(lower) {
				seen[lower] = true
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	return sortedBoolKeys(seen)
}

func isObservedAuthHeaderName(lowerName string) bool {
	switch lowerName {
	case "authorization", "cookie", "set-cookie", "x-csrf-token", "x-xsrf-token", "x-api-key", "proxy-authorization":
		return true
	}
	if strings.Contains(lowerName, "api-key") || strings.Contains(lowerName, "api_key") {
		return true
	}
	if strings.Contains(lowerName, "token") || strings.Contains(lowerName, "secret") || strings.Contains(lowerName, "signature") {
		return true
	}
	return false
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
	segments := discovery.SignificantSegments(path)
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

// FilterEndpointsByMinSamples drops endpoints from apiSpec.Resources whose
// underlying capture cluster carries fewer than minSamples paired entries.
// Resources left with no endpoints are dropped too. Returns the number of
// endpoints removed. A minSamples value <= 1 is a no-op (default behavior).
//
// Filtering happens against re-derived endpoint groups from the capture
// rather than the spec itself, so the qualifying set matches exactly what
// AnalyzeCapture would have seen. GraphQL operations that share one
// underlying cluster (e.g., many distinct operationName values all hitting
// /frontend/graphql) survive together or drop together with that cluster.
// Per-operation thresholding is a future refinement.
//
// The TrafficAnalysis sidecar is intentionally untouched: dropped endpoints
// remain visible there with their `low` confidence and `single-sample`
// flag (or whichever applies) so an operator can audit what filtered out.
func FilterEndpointsByMinSamples(apiSpec *spec.APISpec, capture *EnrichedCapture, minSamples int) int {
	if apiSpec == nil || capture == nil || minSamples <= 1 {
		return 0
	}
	apiEntries, _ := ClassifyEntries(capture.Entries)
	groups := DeduplicateEndpoints(apiEntries)

	qualifying := map[string]bool{}
	for _, g := range groups {
		if len(g.Entries) >= minSamples {
			qualifying[endpointFilterKey(g.Method, g.NormalizedPath)] = true
		}
	}

	dropped := 0
	for resourceName, resource := range apiSpec.Resources {
		for endpointName, endpoint := range resource.Endpoints {
			key := endpointFilterKey(endpoint.Method, endpoint.Path)
			if !qualifying[key] {
				delete(resource.Endpoints, endpointName)
				dropped++
			}
		}
		if len(resource.Endpoints) == 0 {
			delete(apiSpec.Resources, resourceName)
		} else {
			apiSpec.Resources[resourceName] = resource
		}
	}
	return dropped
}

func endpointFilterKey(method string, path string) string {
	return strings.ToUpper(strings.TrimSpace(method)) + " " + path
}

// SampleFile is the on-disk shape of one redacted endpoint sample written
// to <spec-stem>-samples/<method>__<path-slug>__<hash>.json. Designed so a
// reviewer can read a single file and see exactly what evidence backed the
// emitted spec entry, with credentials stripped at write time.
type SampleFile struct {
	Endpoint              string            `json:"endpoint"`
	Method                string            `json:"method"`
	RawURL                string            `json:"raw_url"`
	Status                int               `json:"status"`
	RequestHeaders        map[string]string `json:"request_headers,omitempty"`
	RequestBody           any               `json:"request_body"`
	ResponseHeaders       map[string]string `json:"response_headers,omitempty"`
	ResponseBody          any               `json:"response_body"`
	ResponseBodyKnown     bool              `json:"response_body_known"`
	ResponseBodyTruncated bool              `json:"response_body_truncated,omitempty"`
	Redactions            []string          `json:"redactions,omitempty"`
}

const sampleBodyMaxBytes = 16 * 1024

// DefaultSamplesPath returns the canonical samples directory for a spec at
// specPath: a sibling directory named <stem>-samples/. Mirrors
// DefaultTrafficAnalysisPath's naming convention so artifacts cluster.
func DefaultSamplesPath(specPath string) string {
	dir := filepath.Dir(specPath)
	base := filepath.Base(specPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" || stem == "." {
		stem = "spec"
	}
	return filepath.Join(dir, stem+"-samples")
}

// WriteSamples writes one redacted SampleFile per endpoint group to
// outputDir. outputDir must already exist. Returns the number of files
// written. Files are named method__path-slug__hash.json so the same
// endpoint group produces the same filename across reruns.
//
// The capture's entries are re-classified and re-deduplicated locally
// using the same ClassifyEntries + DeduplicateEndpoints path that
// AnalyzeCapture uses, so the file set is a 1:1 reflection of what the
// emitted spec would contain.
func WriteSamples(capture *EnrichedCapture, outputDir string) (int, error) {
	if capture == nil {
		return 0, fmt.Errorf("capture is required")
	}
	if strings.TrimSpace(outputDir) == "" {
		return 0, fmt.Errorf("output directory is required")
	}

	apiEntries, _ := ClassifyEntries(capture.Entries)
	groups := DeduplicateEndpoints(apiEntries)

	written := 0
	for _, group := range groups {
		sample := buildSampleFile(group)
		filename := sampleFilename(group)
		data, err := encodeSampleJSON(sample)
		if err != nil {
			return written, fmt.Errorf("marshaling sample %s: %w", filename, err)
		}
		path := filepath.Join(outputDir, filename)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return written, fmt.Errorf("writing sample %s: %w", filename, err)
		}
		written++
	}
	return written, nil
}

// encodeSampleJSON marshals a SampleFile with HTML-escaping disabled so the
// `<redacted>` sentinel and other URL-safe characters travel verbatim
// instead of becoming `<redacted>`. Reviewers read these files
// directly; unicode-escaped content fights that.
func encodeSampleJSON(sample SampleFile) ([]byte, error) {
	var buf strings.Builder
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(sample); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// sampleFilename returns method__path-slug__hash.json. The hash is the
// first 8 hex chars of sha256(METHOD + " " + normalizedPath) so the same
// endpoint group always lands at the same filename across reruns.
func sampleFilename(group EndpointGroup) string {
	method := strings.ToLower(strings.TrimSpace(group.Method))
	if method == "" {
		method = "get"
	}

	slug := group.NormalizedPath
	slug = strings.ReplaceAll(slug, "{", "")
	slug = strings.ReplaceAll(slug, "}", "")
	slug = strings.ReplaceAll(slug, "/", "_")
	slug = strings.TrimLeft(slug, "_")
	if slug == "" {
		slug = "root"
	}

	h := sha256.Sum256([]byte(strings.ToUpper(method) + " " + group.NormalizedPath))
	return fmt.Sprintf("%s__%s__%s.json", method, slug, hex.EncodeToString(h[:4]))
}

// buildSampleFile selects a representative entry from the group, redacts
// it, and assembles the on-disk SampleFile struct. Redaction paths are
// path-prefixed so a reviewer can locate exactly what was stripped.
func buildSampleFile(group EndpointGroup) SampleFile {
	entry := selectSampleEntry(group.Entries)

	redactedReqHeaders, reqHeaderRedactions := RedactHeaders(entry.RequestHeaders)
	redactedRespHeaders, respHeaderRedactions := RedactHeaders(entry.ResponseHeaders)

	reqBody, _, reqBodyRedactions := preparedSampleBody(entry.RequestBody, "request_body")
	respBody, respTruncated, respBodyRedactions := preparedSampleBody(entry.ResponseBody, "response_body")

	redactions := make([]string, 0, len(reqHeaderRedactions)+len(respHeaderRedactions)+len(reqBodyRedactions)+len(respBodyRedactions))
	for _, name := range reqHeaderRedactions {
		redactions = append(redactions, "request_headers."+name)
	}
	for _, name := range respHeaderRedactions {
		redactions = append(redactions, "response_headers."+name)
	}
	redactions = append(redactions, reqBodyRedactions...)
	redactions = append(redactions, respBodyRedactions...)
	sort.Strings(redactions)
	if len(redactions) == 0 {
		redactions = nil
	}

	return SampleFile{
		Endpoint:              fmt.Sprintf("%s %s", group.Method, group.NormalizedPath),
		Method:                group.Method,
		RawURL:                entry.URL,
		Status:                entry.ResponseStatus,
		RequestHeaders:        redactedReqHeaders,
		RequestBody:           reqBody,
		ResponseHeaders:       redactedRespHeaders,
		ResponseBody:          respBody,
		ResponseBodyKnown:     strings.TrimSpace(entry.ResponseBody) != "",
		ResponseBodyTruncated: respTruncated,
		Redactions:            redactions,
	}
}

// selectSampleEntry picks the most-recent successful (2xx) entry; falls
// back to most-recent non-error (<500); falls back to the most-recent
// entry. Capture order is preserved by ClassifyEntries, so iterating and
// overwriting on each match yields "most-recent of category".
func selectSampleEntry(entries []EnrichedEntry) EnrichedEntry {
	if len(entries) == 0 {
		return EnrichedEntry{}
	}

	var success, nonError EnrichedEntry
	var hasSuccess, hasNonError bool
	for _, entry := range entries {
		if entry.ResponseStatus >= 200 && entry.ResponseStatus < 300 {
			success = entry
			hasSuccess = true
		}
		if entry.ResponseStatus > 0 && entry.ResponseStatus < 500 {
			nonError = entry
			hasNonError = true
		}
	}
	if hasSuccess {
		return success
	}
	if hasNonError {
		return nonError
	}
	return entries[len(entries)-1]
}

// preparedSampleBody returns the body value (JSON-typed when parseable,
// otherwise a string), a truncation flag, and the path-prefixed redaction
// labels. pathPrefix is the dotted root applied to redaction labels.
func preparedSampleBody(body string, pathPrefix string) (any, bool, []string) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return nil, false, nil
	}

	truncated := false
	if len(trimmed) > sampleBodyMaxBytes {
		trimmed = trimmed[:sampleBodyMaxBytes]
		truncated = true
	}

	redactedBody, redactedPaths := RedactJSONBody(trimmed)

	prefixed := make([]string, 0, len(redactedPaths))
	for _, p := range redactedPaths {
		prefixed = append(prefixed, pathPrefix+"."+p)
	}

	var parsed any
	if err := json.Unmarshal([]byte(redactedBody), &parsed); err == nil {
		return parsed, truncated, prefixed
	}
	return redactedBody, truncated, prefixed
}
