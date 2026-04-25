package graphql

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/mvanhorn/cli-printing-press/internal/spec"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var (
	docBlockRE = regexp.MustCompile(`(?s)""".*?"""`)
	commentRE  = regexp.MustCompile(`(?m)#.*$`)
	blockRE    = regexp.MustCompile(`(?s)\b(type|input|enum)\s+([A-Za-z_][A-Za-z0-9_]*)[^{=]*\{(.*?)\}`)
	fieldRE    = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*(?:\((.*)\))?\s*:\s*([A-Za-z0-9_\[\]!]+)`)
	scalarRE   = regexp.MustCompile(`(?m)^\s*scalar\s+([A-Za-z_][A-Za-z0-9_]*)\s*$`)
)

type gqlType struct {
	Kind       string
	Name       string
	Fields     []gqlField
	EnumValues []string
}

type gqlField struct {
	Name string
	Type string
	Args []gqlArg
}

type gqlArg struct {
	Name string
	Type string
}

// ParseSDL reads a GraphQL SDL file and converts it to an APISpec.
func ParseSDL(path string) (*spec.APISpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading SDL: %w", err)
	}
	return ParseSDLBytes(path, data)
}

// ParseSDLBytes parses SDL bytes from a file path or URL-like source identifier.
func ParseSDLBytes(source string, data []byte) (*spec.APISpec, error) {
	return parseSDLContent(source, string(data))
}

// IsGraphQLSDL checks if the data looks like a GraphQL schema.
func IsGraphQLSDL(data []byte) bool {
	s := string(data)
	return strings.Contains(s, "type Query") || strings.Contains(s, "type Mutation") ||
		(strings.Contains(s, "type ") && strings.Contains(s, "scalar "))
}

func parseSDLContent(source, raw string) (*spec.APISpec, error) {
	cleaned := stripSDLComments(raw)
	types, scalars, err := parseSDLTypes(cleaned)
	if err != nil {
		return nil, err
	}

	name := deriveAPIName(source)
	baseURL, auth := knownGraphQLDefaults(name, source)
	if baseURL == "" {
		baseURL = "https://api.example.com/graphql"
	}
	if auth.Type == "" {
		auth = spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			EnvVars: []string{strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_API_KEY"},
		}
	}

	apiSpec := &spec.APISpec{
		Name:        name,
		Description: "Generated from GraphQL schema",
		BaseURL:     baseURL,
		Auth:        auth,
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   fmt.Sprintf("~/.config/%s-pp-cli/config.toml", name),
		},
		Resources: map[string]spec.Resource{},
		Types:     map[string]spec.TypeDef{},
	}

	enumMap := map[string][]string{}
	for name, typ := range types {
		if typ.Kind == "enum" {
			enumMap[name] = append([]string(nil), typ.EnumValues...)
		}
	}

	connectionEntities := detectConnections(types)
	queryType := types["Query"]
	mutationType := types["Mutation"]
	resourceEntities := map[string]struct{}{}

	for _, field := range queryType.Fields {
		baseType := unwrapType(field.Type)
		entityName, isConnection := connectionEntities[baseType]

		switch {
		case isConnection:
			resourceName := resourceNameForQuery(field.Name, entityName)
			resource := apiSpec.Resources[resourceName]
			if resource.Description == "" {
				resource.Description = "Manage " + resourceName
			}
			resource.Endpoints = ensureEndpoints(resource.Endpoints)
			resource.Endpoints["list"] = buildListEndpoint(field, entityName, enumMap)
			apiSpec.Resources[resourceName] = resource
			resourceEntities[entityName] = struct{}{}
		case isEntityType(baseType, types):
			resourceName := resourceNameFromType(baseType)
			resource := apiSpec.Resources[resourceName]
			if resource.Description == "" {
				resource.Description = "Manage " + resourceName
			}
			resource.Endpoints = ensureEndpoints(resource.Endpoints)
			resource.Endpoints["get"] = buildGetEndpoint(field, baseType, enumMap)
			apiSpec.Resources[resourceName] = resource
			resourceEntities[baseType] = struct{}{}
		}
	}

	for _, field := range mutationType.Fields {
		action, entityName := classifyMutation(field, types)
		if action == "" || entityName == "" {
			continue
		}

		resourceName := resourceNameFromType(entityName)
		resource := apiSpec.Resources[resourceName]
		if resource.Description == "" {
			resource.Description = "Manage " + resourceName
		}
		resource.Endpoints = ensureEndpoints(resource.Endpoints)
		resource.Endpoints[action] = buildMutationEndpoint(field, action, entityName, types, enumMap)
		apiSpec.Resources[resourceName] = resource
		resourceEntities[entityName] = struct{}{}
	}

	for _, typ := range types {
		if !shouldExposeType(typ.Name, typ.Kind) {
			continue
		}
		apiSpec.Types[typ.Name] = buildTypeDef(typ, enumMap)
	}

	addSupportTypes(types, resourceEntities, apiSpec.Types)
	addScalarTypes(scalars, apiSpec.Types)

	if err := apiSpec.Validate(); err != nil {
		return nil, fmt.Errorf("validating parsed SDL: %w", err)
	}

	return apiSpec, nil
}

func stripSDLComments(s string) string {
	s = docBlockRE.ReplaceAllString(s, "")
	s = commentRE.ReplaceAllString(s, "")
	return s
}

func parseSDLTypes(s string) (map[string]gqlType, map[string]struct{}, error) {
	types := map[string]gqlType{}
	scalars := map[string]struct{}{}

	for _, match := range scalarRE.FindAllStringSubmatch(s, -1) {
		if len(match) > 1 {
			scalars[match[1]] = struct{}{}
		}
	}

	matches := blockRE.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return nil, nil, fmt.Errorf("no GraphQL type definitions found")
	}

	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		kind := match[1]
		name := match[2]
		body := match[3]

		switch kind {
		case "enum":
			enumValues := parseEnumValues(body)
			types[name] = gqlType{Kind: kind, Name: name, EnumValues: enumValues}
		case "type", "input":
			fields := parseFields(body)
			types[name] = gqlType{Kind: kind, Name: name, Fields: fields}
		}
	}

	return types, scalars, nil
}

func parseEnumValues(body string) []string {
	var values []string
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSuffix(line, ",")
		if line == "" {
			continue
		}
		values = append(values, line)
	}
	sort.Strings(values)
	return values
}

func parseFields(body string) []gqlField {
	var fields []gqlField
	for rawLine := range strings.SplitSeq(body, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "@") {
			continue
		}
		match := fieldRE.FindStringSubmatch(line)
		if len(match) < 4 {
			continue
		}
		field := gqlField{
			Name: match[1],
			Type: strings.TrimSpace(match[3]),
		}
		if len(match) > 2 && strings.TrimSpace(match[2]) != "" {
			field.Args = parseArgs(match[2])
		}
		fields = append(fields, field)
	}
	return fields
}

func parseArgs(raw string) []gqlArg {
	var args []gqlArg
	for _, part := range splitArgs(raw) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, typeRef, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		typeRef = strings.TrimSpace(typeRef)
		if idx := strings.Index(typeRef, "="); idx >= 0 {
			typeRef = strings.TrimSpace(typeRef[:idx])
		}
		args = append(args, gqlArg{
			Name: strings.TrimSpace(name),
			Type: typeRef,
		})
	}
	return args
}

func splitArgs(raw string) []string {
	var parts []string
	var current strings.Builder
	depth := 0

	for _, r := range raw {
		switch r {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, current.String())
				current.Reset()
				continue
			}
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func detectConnections(types map[string]gqlType) map[string]string {
	connections := map[string]string{}
	for name, typ := range types {
		if !strings.HasSuffix(name, "Connection") || typ.Kind != "type" {
			continue
		}
		hasNodes := false
		hasPageInfo := false
		entityName := ""

		for _, field := range typ.Fields {
			switch field.Name {
			case "nodes":
				hasNodes = true
				entityName = unwrapType(field.Type)
			case "pageInfo":
				hasPageInfo = unwrapType(field.Type) == "PageInfo"
			}
		}

		if hasNodes && hasPageInfo && entityName != "" {
			connections[name] = entityName
		}
	}
	return connections
}

func buildListEndpoint(field gqlField, entityName string, enumMap map[string][]string) spec.Endpoint {
	params := mapArgs(field.Args, enumMap, false)
	pagination := &spec.Pagination{
		Type:           "cursor",
		LimitParam:     "first",
		CursorParam:    "after",
		NextCursorPath: "data." + field.Name + ".pageInfo.endCursor",
		HasMoreField:   "data." + field.Name + ".pageInfo.hasNextPage",
	}
	ensurePaginationParams(&params)

	return spec.Endpoint{
		Method:       "GET",
		Path:         "/graphql",
		Description:  "List " + resourceNameFromType(entityName),
		Params:       params,
		Response:     spec.ResponseDef{Type: "array", Item: entityName},
		Pagination:   pagination,
		ResponsePath: "data." + field.Name + ".nodes",
	}
}

func buildGetEndpoint(field gqlField, entityName string, enumMap map[string][]string) spec.Endpoint {
	params := mapArgs(field.Args, enumMap, true)
	return spec.Endpoint{
		Method:       "GET",
		Path:         "/graphql",
		Description:  "Get a single " + strings.ToLower(entityName),
		Params:       params,
		Response:     spec.ResponseDef{Type: "object", Item: entityName},
		ResponsePath: "data." + field.Name,
	}
}

func buildMutationEndpoint(field gqlField, action, entityName string, types map[string]gqlType, enumMap map[string][]string) spec.Endpoint {
	method := mutationMethod(action)
	params := mapMutationParams(field.Args, enumMap)
	body := mapMutationBody(field.Args, types, enumMap)

	return spec.Endpoint{
		Method:       method,
		Path:         "/graphql",
		Description:  mutationDescription(action, entityName),
		Params:       params,
		Body:         body,
		Response:     spec.ResponseDef{Type: "object", Item: entityName},
		ResponsePath: "data." + field.Name,
	}
}

func mutationMethod(action string) string {
	switch action {
	case "create":
		return "POST"
	case "update":
		return "PATCH"
	case "delete":
		return "DELETE"
	default:
		return "POST"
	}
}

func mutationDescription(action, entityName string) string {
	switch action {
	case "create":
		return "Create a " + strings.ToLower(entityName)
	case "update":
		return "Update a " + strings.ToLower(entityName)
	case "delete":
		return "Delete a " + strings.ToLower(entityName)
	default:
		return cases.Title(language.English).String(action) + " " + strings.ToLower(entityName)
	}
}

func mapArgs(args []gqlArg, enumMap map[string][]string, positionalID bool) []spec.Param {
	params := make([]spec.Param, 0, len(args))
	for _, arg := range args {
		param := mapArg(arg, enumMap)
		if positionalID && arg.Name == "id" {
			param.Positional = true
		}
		params = append(params, param)
	}
	return params
}

func mapMutationParams(args []gqlArg, enumMap map[string][]string) []spec.Param {
	var params []spec.Param
	for _, arg := range args {
		if strings.HasSuffix(unwrapType(arg.Type), "Input") {
			continue
		}
		param := mapArg(arg, enumMap)
		if arg.Name == "id" {
			param.Positional = true
		}
		params = append(params, param)
	}
	return params
}

func mapMutationBody(args []gqlArg, types map[string]gqlType, enumMap map[string][]string) []spec.Param {
	var body []spec.Param
	for _, arg := range args {
		inputName := unwrapType(arg.Type)
		inputType, ok := types[inputName]
		if !ok || inputType.Kind != "input" {
			continue
		}
		for _, field := range inputType.Fields {
			body = append(body, mapFieldToParam(field, enumMap))
		}
	}
	return body
}

func mapArg(arg gqlArg, enumMap map[string][]string) spec.Param {
	paramType, required, format := mapGraphQLType(arg.Type)
	param := spec.Param{
		Name:     arg.Name,
		Type:     paramType,
		Required: required,
		Format:   format,
	}
	if enumValues := enumMap[unwrapType(arg.Type)]; len(enumValues) > 0 {
		param.Enum = append([]string(nil), enumValues...)
	}
	return param
}

func mapFieldToParam(field gqlField, enumMap map[string][]string) spec.Param {
	paramType, required, format := mapGraphQLType(field.Type)
	param := spec.Param{
		Name:     field.Name,
		Type:     paramType,
		Required: required,
		Format:   format,
	}
	if enumValues := enumMap[unwrapType(field.Type)]; len(enumValues) > 0 {
		param.Enum = append([]string(nil), enumValues...)
	}
	return param
}

func mapGraphQLType(typeRef string) (paramType string, required bool, format string) {
	required = strings.HasSuffix(strings.TrimSpace(typeRef), "!")
	base := unwrapType(typeRef)
	if strings.Contains(typeRef, "[") {
		return "array", required, ""
	}

	switch base {
	case "String", "ID":
		return "string", required, ""
	case "Int":
		return "integer", required, ""
	case "Float":
		return "number", required, ""
	case "Boolean":
		return "boolean", required, ""
	case "DateTime":
		return "string", required, "date-time"
	default:
		return "string", required, ""
	}
}

func classifyMutation(field gqlField, types map[string]gqlType) (string, string) {
	nameLower := strings.ToLower(field.Name)
	action := ""
	switch {
	case strings.Contains(nameLower, "create"):
		action = "create"
	case strings.Contains(nameLower, "update"):
		action = "update"
	case strings.Contains(nameLower, "delete"), strings.Contains(nameLower, "archive"):
		action = "delete"
	default:
		return "", ""
	}

	entityName := entityFromMutationInput(field, types)
	if entityName == "" {
		entityName = entityFromReturnType(field.Type, types)
	}
	return action, entityName
}

func entityFromMutationInput(field gqlField, types map[string]gqlType) string {
	for _, arg := range field.Args {
		inputName := unwrapType(arg.Type)
		if !strings.HasSuffix(inputName, "Input") {
			continue
		}
		trimmed := strings.TrimSuffix(inputName, "Input")
		trimmed = strings.TrimSuffix(trimmed, "Create")
		trimmed = strings.TrimSuffix(trimmed, "Update")
		if _, ok := types[trimmed]; ok {
			return trimmed
		}
	}
	return ""
}

func entityFromReturnType(typeRef string, types map[string]gqlType) string {
	name := unwrapType(typeRef)
	if typ, ok := types[name]; ok {
		for _, field := range typ.Fields {
			fieldType := unwrapType(field.Type)
			if isEntityType(fieldType, types) {
				return fieldType
			}
		}
	}
	if isEntityType(name, types) {
		return name
	}
	return ""
}

func isEntityType(name string, types map[string]gqlType) bool {
	typ, ok := types[name]
	if !ok || typ.Kind != "type" {
		return false
	}
	return shouldExposeType(name, typ.Kind)
}

func shouldExposeType(name, kind string) bool {
	if kind != "type" {
		return false
	}
	switch name {
	case "Query", "Mutation", "Subscription", "PageInfo":
		return false
	}
	for _, suffix := range []string{"Connection", "Edge", "Payload", "Input"} {
		if strings.HasSuffix(name, suffix) {
			return false
		}
	}
	return true
}

func buildTypeDef(typ gqlType, enumMap map[string][]string) spec.TypeDef {
	fields := make([]spec.TypeField, 0, len(typ.Fields))
	seen := make(map[string]bool, len(typ.Fields))
	for _, field := range typ.Fields {
		if seen[field.Name] {
			continue
		}
		seen[field.Name] = true
		fieldType, _, _ := mapGraphQLType(field.Type)
		if enumValues := enumMap[unwrapType(field.Type)]; len(enumValues) > 0 && fieldType == "string" {
			fieldType = "string"
		}
		fields = append(fields, spec.TypeField{
			Name: field.Name,
			Type: fieldType,
		})
	}
	return spec.TypeDef{Fields: fields}
}

func addSupportTypes(types map[string]gqlType, resourceEntities map[string]struct{}, dest map[string]spec.TypeDef) {
	visited := map[string]struct{}{}
	var walk func(string)
	walk = func(typeName string) {
		if _, ok := visited[typeName]; ok {
			return
		}
		visited[typeName] = struct{}{}

		typ, ok := types[typeName]
		if !ok || !shouldExposeType(typeName, typ.Kind) {
			return
		}

		if _, exists := dest[typeName]; !exists {
			dest[typeName] = buildTypeDef(typ, nil)
		}

		for _, field := range typ.Fields {
			ref := unwrapType(field.Type)
			if shouldExposeType(ref, types[ref].Kind) {
				walk(ref)
			}
		}
	}

	for typeName := range resourceEntities {
		walk(typeName)
	}
}

func addScalarTypes(scalars map[string]struct{}, dest map[string]spec.TypeDef) {
	if len(scalars) == 0 {
		return
	}
}

func ensureEndpoints(endpoints map[string]spec.Endpoint) map[string]spec.Endpoint {
	if endpoints == nil {
		return map[string]spec.Endpoint{}
	}
	return endpoints
}

func ensurePaginationParams(params *[]spec.Param) {
	if !hasParam(*params, "first") {
		*params = append(*params, spec.Param{
			Name:        "first",
			Type:        "integer",
			Default:     50,
			Description: "Number of results per page",
		})
	}
	if !hasParam(*params, "after") {
		*params = append(*params, spec.Param{
			Name:        "after",
			Type:        "string",
			Description: "Pagination cursor",
		})
	}
}

func hasParam(params []spec.Param, name string) bool {
	for _, param := range params {
		if param.Name == name {
			return true
		}
	}
	return false
}

func unwrapType(typeRef string) string {
	typeRef = strings.TrimSpace(typeRef)
	typeRef = strings.TrimSuffix(typeRef, "!")
	typeRef = strings.TrimPrefix(typeRef, "[")
	typeRef = strings.TrimSuffix(typeRef, "]")
	typeRef = strings.TrimSuffix(typeRef, "!")
	return strings.TrimSpace(typeRef)
}

func resourceNameForQuery(queryName, entityName string) string {
	queryName = toKebabCase(queryName)
	if queryName != "" && queryName != toKebabCase(entityName) {
		return queryName
	}
	return resourceNameFromType(entityName)
}

func resourceNameFromType(typeName string) string {
	words := splitWords(typeName)
	if len(words) == 0 {
		return strings.ToLower(typeName)
	}

	last := pluralize(strings.ToLower(words[len(words)-1]))
	words[len(words)-1] = last
	for i := range words[:len(words)-1] {
		words[i] = strings.ToLower(words[i])
	}
	return strings.Join(words, "-")
}

func pluralize(s string) string {
	switch {
	case strings.HasSuffix(s, "y") && !strings.HasSuffix(s, "ay") && !strings.HasSuffix(s, "ey") && !strings.HasSuffix(s, "iy") && !strings.HasSuffix(s, "oy") && !strings.HasSuffix(s, "uy"):
		return s[:len(s)-1] + "ies"
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "z"), strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es"
	default:
		return s + "s"
	}
}

func splitWords(s string) []string {
	var words []string
	var current strings.Builder

	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) && current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
		if r == '-' || r == '_' || unicode.IsSpace(r) {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}

func toKebabCase(s string) string {
	words := splitWords(s)
	for i := range words {
		words[i] = strings.ToLower(words[i])
	}
	return strings.Join(words, "-")
}

func deriveAPIName(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "graphql-api"
	}

	lower := strings.ToLower(source)
	for _, known := range []string{"linear", "github", "shopify"} {
		if strings.Contains(lower, known) {
			return known
		}
	}

	base := filepath.Base(source)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "" || base == "schema" || base == "graphql" || base == "spec" {
		parent := filepath.Base(filepath.Dir(source))
		if parent != "." && parent != "/" && parent != "" {
			base = parent
		}
	}

	base = strings.TrimSpace(base)
	if base == "" {
		return "graphql-api"
	}
	base = toKebabCase(base)
	base = strings.Trim(base, "-")
	if base == "" {
		return "graphql-api"
	}
	return base
}

func knownGraphQLDefaults(name, source string) (string, spec.AuthConfig) {
	match := strings.ToLower(name + " " + source)
	switch {
	case strings.Contains(match, "linear"):
		return "https://api.linear.app/graphql", spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			EnvVars: []string{"LINEAR_API_KEY"},
		}
	case strings.Contains(match, "github"):
		return "https://api.github.com/graphql", spec.AuthConfig{
			Type:    "bearer_token",
			Header:  "Authorization",
			EnvVars: []string{"GITHUB_TOKEN"},
		}
	case strings.Contains(match, "shopify"):
		return "https://shopify.dev/graphql", spec.AuthConfig{
			Type:    "api_key",
			Header:  "X-Shopify-Access-Token",
			EnvVars: []string{"SHOPIFY_ACCESS_TOKEN"},
		}
	default:
		return "", spec.AuthConfig{}
	}
}
