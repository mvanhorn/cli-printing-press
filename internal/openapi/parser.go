package openapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var (
	maxResources            = 500
	maxEndpointsPerResource = 50
	endpointLimitExplicit   = false // true when user set --max-endpoints-per-resource
)

const (
	extensionAuthEnvVars      = "x-auth-env-vars"
	extensionAuthVars         = "x-auth-vars"
	extensionAuthOptional     = "x-auth-optional"
	extensionAuthKeyURL       = "x-auth-key-url"
	extensionAuthTitle        = "x-auth-title"
	extensionAuthDescription  = "x-auth-description"
	extensionSpeakeasyExample = "x-speakeasy-example"
	extensionTierRouting      = "x-tier-routing"
	extensionTier             = "x-tier"
	extensionAPIName          = "x-api-name"
	extensionDisplayName      = "x-display-name"
	extensionWebsite          = "x-website"
	extensionProxyRoutes      = "x-proxy-routes"
)

// SetMaxResources overrides the default resource limit. When not called,
// the parser uses a default of 500 which accommodates all known APIs.
func SetMaxResources(n int) {
	if n > 0 {
		maxResources = n
	}
}

// SetMaxEndpointsPerResource overrides the default limit and disables
// auto-calibration. When not called, the parser auto-calibrates the limit
// from the spec so well-formed specs never have endpoints silently skipped.
func SetMaxEndpointsPerResource(n int) {
	if n > 0 {
		maxEndpointsPerResource = n
		endpointLimitExplicit = true
	}
}

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
				var pathsToDelete []string
				for pathKey, pathVal := range paths {
					if strings.Contains(string(pathVal), refStr) {
						pathsToDelete = append(pathsToDelete, pathKey)
						fmt.Fprintf(os.Stderr, "info: removed path %s (references broken %s)\n", pathKey, brokenKey)
					}
				}
				for _, pk := range pathsToDelete {
					delete(paths, pk)
					modified = true
				}
				if len(pathsToDelete) > 0 {
					pathsBytes, _ := json.Marshal(paths)
					raw["paths"] = pathsBytes
				}
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
	if normalized, err := normalizeSpecData(data); err == nil {
		data = normalized
	}
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
		if name == "api" {
			if raw, ok := lookupOpenAPIInfoExtension(doc, extensionAPIName); ok {
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

	// Extract x-display-name extension. Carries the human-readable
	// brand name when it differs from the slug-derived form ("Cal.com"
	// vs "Cal Com"). When absent, derive a Unicode-preserving form from
	// info.title so accented brands like "Café Bistro" or "PokéAPI"
	// don't get flattened by EffectiveDisplayName's HumanName(slug)
	// fallback (the slug is ASCII-folded for filesystem safety).
	var displayName string
	displayNameDerivedFromTitle := false
	if raw, ok := lookupOpenAPIInfoExtension(doc, extensionDisplayName); ok {
		if s, ok := raw.(string); ok {
			displayName = strings.TrimSpace(s)
		}
	}
	if displayName == "" && doc.Info != nil {
		if derived := cleanSpecNameUnicode(doc.Info.Title); derived != "" && derived != "api" {
			displayName = naming.HumanName(derived)
			displayNameDerivedFromTitle = true
		}
	}

	// Extract website URL from spec metadata (contact URL, externalDocs, or x-website)
	var websiteURL string
	if doc.Info != nil {
		if doc.Info.Contact != nil && doc.Info.Contact.URL != "" {
			websiteURL = doc.Info.Contact.URL
		}
		if websiteURL == "" {
			if raw, ok := lookupOpenAPIInfoExtension(doc, extensionWebsite); ok {
				if s, ok := raw.(string); ok {
					websiteURL = s
				}
			}
		}
	}
	if websiteURL == "" && doc.ExternalDocs != nil && doc.ExternalDocs.URL != "" {
		websiteURL = doc.ExternalDocs.URL
	}

	// Extract x-proxy-routes extension for proxy-envelope client pattern
	var proxyRoutes map[string]string
	if raw, ok := lookupOpenAPIInfoExtension(doc, extensionProxyRoutes); ok {
		if m, ok := raw.(map[string]any); ok {
			proxyRoutes = make(map[string]string, len(m))
			for k, v := range m {
				if s, ok := v.(string); ok {
					proxyRoutes[k] = s
				}
			}
		}
	}

	baseURL := ""
	basePath := ""
	if len(doc.Servers) > 0 && doc.Servers[0] != nil {
		baseURL, basePath = resolveServerURL(doc.Servers[0])
	}
	if baseURL == "" && basePath == "" {
		// No top-level servers — walk per-operation `servers:` blocks. Specs
		// generated by some tools (older Open-Meteo, certain Stripe-derived
		// flows) declare a server per-endpoint instead of globally; without
		// this fallback the parser was emitting `https://api.example.com`
		// and producing a CLI that DNS-fails on every call.
		if perOpURL, perOpPath := mostCommonOperationServer(doc); perOpURL != "" || perOpPath != "" {
			baseURL = perOpURL
			basePath = perOpPath
		}
	}
	if baseURL == "" && basePath == "" {
		warnf("no servers defined in spec; generated CLI will require base_url in config")
		baseURL = "https://api.example.com"
	}

	auth := mapAuth(doc, name)
	if auth.Type != "none" && allOperationsAllowAnonymous(doc) {
		auth = spec.AuthConfig{Type: "none"}
	}

	tierRouting, err := parseTierRoutingExtension(doc)
	if err != nil {
		return nil, err
	}

	result := &spec.APISpec{
		Name:                        name,
		DisplayName:                 displayName,
		DisplayNameDerivedFromTitle: displayNameDerivedFromTitle,
		Description:                 description,
		Version:                     version,
		BaseURL:                     baseURL,
		BasePath:                    basePath,
		WebsiteURL:                  websiteURL,
		ProxyRoutes:                 proxyRoutes,
		Auth:                        auth,
		TierRouting:                 tierRouting,
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

	// Post-parse sweep: if the spec has no authentication at all (not inferred
	// from description keywords), mark every endpoint as NoAuth. The per-operation
	// detection in mapResources handles explicit security:[] overrides; this sweep
	// handles the case where the entire API is public.
	if result.Auth.Type == "none" && !result.Auth.Inferred {
		markAllEndpointsNoAuth(result.Resources)
	}

	var perEndpointHeaders map[string]map[string]string
	result.RequiredHeaders, perEndpointHeaders = detectRequiredHeaders(doc, result.Auth)
	applyHeaderOverrides(result, perEndpointHeaders)

	if err := result.Validate(); err != nil {
		return nil, fmt.Errorf("validating parsed spec: %w", err)
	}

	return result, nil
}

func parseTierRoutingExtension(doc *openapi3.T) (spec.TierRoutingConfig, error) {
	raw, ok := lookupOpenAPIExtension(doc, extensionTierRouting)
	if !ok {
		return spec.TierRoutingConfig{}, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return spec.TierRoutingConfig{}, fmt.Errorf("marshaling %s: %w", extensionTierRouting, err)
	}
	var cfg spec.TierRoutingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return spec.TierRoutingConfig{}, fmt.Errorf("parsing %s: %w", extensionTierRouting, err)
	}
	return cfg, nil
}

func lookupOpenAPIExtension(doc *openapi3.T, key string) (any, bool) {
	if doc != nil && doc.Extensions != nil {
		if raw, ok := doc.Extensions[key]; ok {
			return raw, true
		}
	}
	if doc != nil && doc.Info != nil && doc.Info.Extensions != nil {
		if raw, ok := doc.Info.Extensions[key]; ok {
			return raw, true
		}
	}
	return nil, false
}

func lookupOpenAPIInfoExtension(doc *openapi3.T, key string) (any, bool) {
	if doc != nil && doc.Info != nil && doc.Info.Extensions != nil {
		raw, ok := doc.Info.Extensions[key]
		return raw, ok
	}
	return nil, false
}

func mapAuth(doc *openapi3.T, name string) spec.AuthConfig {
	auth := spec.AuthConfig{Type: "none"}
	schemeName, scheme := selectSecurityScheme(doc)
	if scheme == nil {
		result := inferQueryParamAuth(doc, name, auth)
		if result.Type == "none" {
			result = inferDescriptionAuth(doc, name, result)
		}
		if result.Type == "none" {
			result = inferOperationLevelBearer(doc, name, result)
		}
		return result
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
		// Detect composed cookie auth via x-auth-type extension
		if xType, ok := scheme.Extensions["x-auth-type"]; ok {
			if typeStr, ok := xType.(string); ok && typeStr == "composed" {
				auth.Type = "composed"
				if xFmt, ok := scheme.Extensions["x-auth-format"]; ok {
					if fmtStr, ok := xFmt.(string); ok {
						auth.Format = fmtStr
					}
				}
				if xDomain, ok := scheme.Extensions["x-auth-cookie-domain"]; ok {
					if domainStr, ok := xDomain.(string); ok {
						auth.CookieDomain = domainStr
					}
				}
				if xCookies, ok := scheme.Extensions["x-auth-cookies"]; ok {
					if cookieList, ok := xCookies.([]any); ok {
						for _, c := range cookieList {
							if s, ok := c.(string); ok {
								auth.Cookies = append(auth.Cookies, s)
							}
						}
					}
				}
				break
			}
		}
		if auth.Type == "api_key" && strings.EqualFold(auth.In, "header") {
			if prefix, ok := scheme.Extensions["x-prefix"].(string); ok {
				if prefix = strings.TrimSpace(prefix); prefix != "" {
					auth.Format = prefix + " {token}"
				}
			}
		}
		// Detect bot token pattern from scheme name (e.g. "BotToken")
		if auth.Format == "" && strings.Contains(strings.ToLower(schemeName), "bot") && strings.EqualFold(auth.Header, "Authorization") {
			auth.Format = "Bot {bot_token}"
		}
	case "oauth2":
		auth.Type = "bearer_token"
		auth.Header = "Authorization"
		if scheme.Flows != nil {
			// Prefer client_credentials when both flows are declared.
			// Server-to-server is the more common shape for printed CLIs
			// (which run in CI/scripts, not interactive browsers); the spec
			// author can override post-import by setting OAuth2Grant
			// explicitly. AuthorizationURL stays empty for the cc flow
			// (no user redirect), which is the correct shape.
			if cc := scheme.Flows.ClientCredentials; cc != nil && strings.TrimSpace(cc.TokenURL) != "" {
				auth.OAuth2Grant = spec.OAuth2GrantClientCredentials
				auth.TokenURL = cc.TokenURL
				for scope := range cc.Scopes {
					auth.Scopes = append(auth.Scopes, scope)
				}
				sort.Strings(auth.Scopes)
			} else if ac := scheme.Flows.AuthorizationCode; ac != nil {
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

	envPrefix := naming.EnvPrefix(name)
	switch auth.Type {
	case "api_key":
		// Use scheme name for more specific env var (e.g. BotToken -> DISCORD_BOT_TOKEN)
		schemeEnvSuffix := toSnakeCase(schemeName)
		if schemeEnvSuffix != "" && !isGenericAPIKeySchemeSuffix(schemeEnvSuffix) {
			auth.EnvVars = []string{envPrefix + "_" + strings.ToUpper(schemeEnvSuffix)}
		} else {
			auth.EnvVars = []string{envPrefix + "_API_KEY"}
		}
	case "bearer_token":
		schemeEnvSuffix := toSnakeCase(schemeName)
		switch schemeEnvSuffix {
		case "", "bearer", "bearer_token", "token":
			auth.EnvVars = []string{envPrefix + "_TOKEN"}
		default:
			auth.EnvVars = []string{envPrefix + "_" + strings.ToUpper(schemeEnvSuffix)}
		}
	}
	applyAuthOverrideExtensions(&auth, scheme.Extensions)
	applyAuthEnvVarDefaults(&auth, envPrefix)
	applyAuthVarsRichOverride(&auth, scheme.Extensions, fmt.Sprintf("components.securitySchemes.%s.%s", schemeName, extensionAuthVars))
	return auth
}

func isGenericAPIKeySchemeSuffix(suffix string) bool {
	normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(suffix)), "_", "")
	switch normalized {
	case "", "auth", "authentication", "apikey", "apikeyauth", "default":
		return true
	}
	for _, prefix := range []string{"apikeyv", "apikeyauthv"} {
		if version, ok := strings.CutPrefix(normalized, prefix); ok {
			if version != "" && allDigits(version) {
				return true
			}
		}
	}
	return false
}

func allDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func applyAuthOverrideExtensions(auth *spec.AuthConfig, extensions map[string]any) {
	if auth == nil || len(extensions) == 0 {
		return
	}
	if envVars := stringListExtension(extensions, extensionAuthEnvVars); len(envVars) > 0 {
		applyAuthEnvVars(auth, envVars)
	} else if len(auth.EnvVars) == 1 {
		if envVar := envVarExtension(extensions, extensionSpeakeasyExample); envVar != "" {
			applyAuthEnvVars(auth, []string{envVar})
		}
	}
	if optional, ok := boolExtension(extensions, extensionAuthOptional); ok {
		auth.Optional = optional
	}
	if keyURL := stringExtension(extensions, extensionAuthKeyURL); keyURL != "" {
		auth.KeyURL = keyURL
	}
	if title := stringExtension(extensions, extensionAuthTitle); title != "" {
		auth.Title = title
	}
	if description := stringExtension(extensions, extensionAuthDescription); description != "" {
		auth.Description = description
	}
}

func applyAuthEnvVarDefaults(auth *spec.AuthConfig, envPrefix string) {
	if auth == nil {
		return
	}
	// OAuth2 client_credentials default produces 2 entries (CLIENT_ID, CLIENT_SECRET).
	// Skip the override when the spec already supplied an explicit list (>=2 entries via
	// x-auth-env-vars); fall through to the per-name derivation below in that case.
	if auth.OAuth2Grant == spec.OAuth2GrantClientCredentials && len(auth.EnvVars) <= 1 {
		auth.EnvVarSpecs = []spec.AuthEnvVar{
			{
				Name:      envPrefix + "_CLIENT_ID",
				Kind:      spec.AuthEnvVarKindAuthFlowInput,
				Required:  true,
				Sensitive: false,
				Inferred:  true,
			},
			{
				Name:      envPrefix + "_CLIENT_SECRET",
				Kind:      spec.AuthEnvVarKindAuthFlowInput,
				Required:  true,
				Sensitive: true,
				Inferred:  true,
			},
		}
		auth.EnvVars = []string{auth.EnvVarSpecs[0].Name, auth.EnvVarSpecs[1].Name}
		return
	}
	if len(auth.EnvVars) == 0 {
		return
	}
	auth.EnvVarSpecs = make([]spec.AuthEnvVar, 0, len(auth.EnvVars))
	for _, name := range auth.EnvVars {
		if name = strings.TrimSpace(name); name == "" {
			continue
		}
		envVar := spec.AuthEnvVar{
			Name:      name,
			Kind:      spec.AuthEnvVarKindPerCall,
			Required:  true,
			Sensitive: true,
			Inferred:  true,
		}
		if auth.Type == "cookie" || strings.EqualFold(auth.In, "cookie") {
			envVar.Kind = spec.AuthEnvVarKindHarvested
		}
		auth.EnvVarSpecs = append(auth.EnvVarSpecs, envVar)
	}
}

func applyAuthVarsRichOverride(auth *spec.AuthConfig, extensions map[string]any, path string) {
	if auth == nil || len(extensions) == 0 {
		return
	}
	raw, ok := extensions[extensionAuthVars]
	if !ok {
		return
	}
	envVars, err := authVarsExtension(raw)
	if err != nil {
		warnf("%s is malformed: %v; falling back to generated auth env vars", path, err)
		return
	}
	if len(envVars) == 0 {
		warnf("%s is malformed: expected at least one auth var; falling back to generated auth env vars", path)
		return
	}
	auth.EnvVarSpecs = envVars
	auth.EnvVars = make([]string, 0, len(envVars))
	for _, envVar := range envVars {
		auth.EnvVars = append(auth.EnvVars, envVar.Name)
	}
}

func authVarsExtension(raw any) ([]spec.AuthEnvVar, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("expected a list of objects")
	}
	out := make([]spec.AuthEnvVar, 0, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("item %d must be an object", i)
		}
		name, ok := requiredStringField(m, "name")
		if !ok {
			return nil, fmt.Errorf("item %d missing string field %q", i, "name")
		}
		kindText, ok := requiredStringField(m, "kind")
		if !ok {
			return nil, fmt.Errorf("item %d missing string field %q", i, "kind")
		}
		kind := spec.AuthEnvVarKind(kindText)
		switch kind {
		case spec.AuthEnvVarKindPerCall, spec.AuthEnvVarKindAuthFlowInput, spec.AuthEnvVarKindHarvested:
		default:
			return nil, fmt.Errorf("item %d kind %q is not recognized", i, kindText)
		}
		required, ok := boolExtension(m, "required")
		if !ok {
			return nil, fmt.Errorf("item %d missing boolean field %q", i, "required")
		}
		sensitive, ok := boolExtension(m, "sensitive")
		if !ok {
			return nil, fmt.Errorf("item %d missing boolean field %q", i, "sensitive")
		}
		description := ""
		if rawDescription, ok := m["description"]; ok {
			if description, ok = rawDescription.(string); !ok {
				return nil, fmt.Errorf("item %d field %q must be a string", i, "description")
			}
			description = strings.TrimSpace(description)
		}
		out = append(out, spec.AuthEnvVar{
			Name:        name,
			Kind:        kind,
			Required:    required,
			Sensitive:   sensitive,
			Description: description,
		})
	}
	return out, nil
}

func requiredStringField(m map[string]any, name string) (string, bool) {
	raw, ok := m[name]
	if !ok {
		return "", false
	}
	s, ok := raw.(string)
	if !ok {
		return "", false
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	return s, true
}

func applyAuthEnvVars(auth *spec.AuthConfig, envVars []string) {
	oldEnvVars := append([]string(nil), auth.EnvVars...)
	auth.EnvVars = envVars
	remapAuthFormatForEnvOverride(auth, oldEnvVars, envVars)
}

func remapAuthFormatForEnvOverride(auth *spec.AuthConfig, oldEnvVars, newEnvVars []string) {
	if auth.Format == "" || len(oldEnvVars) != 1 || len(newEnvVars) != 1 {
		return
	}
	oldPlaceholder := naming.EnvVarPlaceholder(oldEnvVars[0])
	newPlaceholder := naming.EnvVarPlaceholder(newEnvVars[0])
	if oldPlaceholder == "" || newPlaceholder == "" {
		return
	}
	auth.Format = strings.ReplaceAll(auth.Format, "{"+oldPlaceholder+"}", "{"+newPlaceholder+"}")
	auth.Format = strings.ReplaceAll(auth.Format, "{"+oldEnvVars[0]+"}", "{"+newPlaceholder+"}")
}

func envVarExtension(extensions map[string]any, name string) string {
	value, ok := extensions[name]
	if !ok {
		return ""
	}
	s, ok := value.(string)
	if !ok {
		return ""
	}
	s = strings.TrimSpace(s)
	if !isUpperEnvVarName(s) {
		return ""
	}
	return s
}

func isUpperEnvVarName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || ('A' <= r && r <= 'Z') {
			continue
		}
		if '0' <= r && r <= '9' {
			if i == 0 {
				return false
			}
			continue
		}
		return false
	}
	return true
}

func stringExtension(extensions map[string]any, name string) string {
	value, ok := extensions[name]
	if !ok {
		return ""
	}
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func stringListExtension(extensions map[string]any, name string) []string {
	value, ok := extensions[name]
	if !ok {
		return nil
	}
	switch v := value.(type) {
	case []string:
		var out []string
		for _, item := range v {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		var out []string
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				continue
			}
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if s := strings.TrimSpace(v); s != "" {
			return []string{s}
		}
	}
	return nil
}

func boolExtension(extensions map[string]any, name string) (bool, bool) {
	value, ok := extensions[name]
	if !ok {
		return false, false
	}
	b, ok := value.(bool)
	if !ok {
		return false, false
	}
	return b, true
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

	envPrefix := naming.EnvPrefix(name)
	return spec.AuthConfig{
		Type:    "api_key",
		In:      "query",
		Header:  best,
		EnvVars: []string{envPrefix + "_API_KEY"},
	}
}

// detectRequiredHeaders scans all operations for required header parameters
// and returns those appearing on >80% of operations as global required headers.
// Auth-related and dynamic headers are excluded via case-insensitive matching.
func detectRequiredHeaders(doc *openapi3.T, auth spec.AuthConfig) ([]spec.RequiredHeader, map[string]map[string]string) {
	if doc == nil || doc.Paths == nil {
		return nil, nil
	}

	// Headers to exclude (case-insensitive) — handled by other mechanisms
	excludeHeaders := map[string]bool{
		"authorization": true,
		"content-type":  true,
		"accept":        true,
		"user-agent":    true,
	}
	if auth.Header != "" {
		excludeHeaders[strings.ToLower(auth.Header)] = true
	}

	type headerInfo struct {
		name         string
		defaultValue string
		count        int
		valueCounts  map[string]int    // value → count (for multi-value detection)
		pathValues   map[string]string // apiPath → value (for per-endpoint overrides)
	}

	headers := map[string]*headerInfo{} // keyed by lowercase name
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
			merged := mergeParameters(pathItem, op)
			for _, p := range merged {
				if p == nil {
					continue
				}
				lower := strings.ToLower(p.Name)
				if p.In != openapi3.ParameterInHeader || !p.Required || excludeHeaders[lower] {
					continue
				}
				h, ok := headers[lower]
				if !ok {
					h = &headerInfo{
						name:        p.Name,
						valueCounts: map[string]int{},
						pathValues:  map[string]string{},
					}
					headers[lower] = h
				}
				h.count++

				// Extract this operation's header value
				val := ""
				if p.Schema != nil && p.Schema.Value != nil {
					if p.Schema.Value.Default != nil {
						val = fmt.Sprintf("%v", p.Schema.Value.Default)
					} else if len(p.Schema.Value.Enum) > 0 {
						val = fmt.Sprintf("%v", p.Schema.Value.Enum[0])
					}
				}
				if val != "" {
					h.valueCounts[val]++
					h.pathValues[pathKey] = val
				}
			}
		}
	}

	if totalOps == 0 {
		return nil, nil
	}

	var result []spec.RequiredHeader
	// perEndpointHeaders maps headerName → apiPath → value for headers with
	// multiple distinct values. Endpoints whose value differs from the global
	// default get an override.
	perEndpointHeaders := map[string]map[string]string{}

	threshold := 0.8
	for _, h := range headers {
		if float64(h.count)/float64(totalOps) <= threshold {
			continue
		}

		// Find the majority value (global default). Iterate valueCounts in
		// sorted-key order so ties resolve deterministically; on a count tie,
		// prefer the lexically-greatest value — for ISO-date API-version
		// strings this corresponds to "newest version wins," which matches
		// most APIs' intent. Without this, map iteration order made the
		// choice nondeterministic and the test flaked roughly 1 run in 10.
		vals := make([]string, 0, len(h.valueCounts))
		for v := range h.valueCounts {
			vals = append(vals, v)
		}
		sort.Strings(vals)
		bestVal := ""
		bestCount := 0
		for _, val := range vals {
			cnt := h.valueCounts[val]
			if cnt > bestCount || (cnt == bestCount && val > bestVal) {
				bestVal = val
				bestCount = cnt
			}
		}
		h.defaultValue = bestVal

		result = append(result, spec.RequiredHeader{
			Name:  h.name,
			Value: h.defaultValue,
		})

		// Record per-endpoint overrides for paths with non-majority values
		if len(h.valueCounts) > 1 {
			overrides := map[string]string{}
			for path, val := range h.pathValues {
				if val != bestVal {
					overrides[path] = val
				}
			}
			if len(overrides) > 0 {
				perEndpointHeaders[h.name] = overrides
			}
		}
	}
	return result, perEndpointHeaders
}

// applyHeaderOverrides sets HeaderOverrides on each Endpoint whose API path
// has a per-endpoint header value differing from the global default.
func applyHeaderOverrides(s *spec.APISpec, perEndpoint map[string]map[string]string) {
	if len(perEndpoint) == 0 || s == nil {
		return
	}
	for rName, r := range s.Resources {
		for eName, e := range r.Endpoints {
			overrides := headerOverridesForPath(e.Path, perEndpoint)
			if len(overrides) > 0 {
				e.HeaderOverrides = overrides
				r.Endpoints[eName] = e
			}
		}
		for subName, sub := range r.SubResources {
			for eName, e := range sub.Endpoints {
				overrides := headerOverridesForPath(e.Path, perEndpoint)
				if len(overrides) > 0 {
					e.HeaderOverrides = overrides
					sub.Endpoints[eName] = e
				}
			}
			r.SubResources[subName] = sub
		}
		s.Resources[rName] = r
	}
}

// headerOverridesForPath checks if the given endpoint path has per-endpoint header
// overrides. Returns the matching overrides as RequiredHeader entries.
func headerOverridesForPath(endpointPath string, perEndpoint map[string]map[string]string) []spec.RequiredHeader {
	var result []spec.RequiredHeader
	for headerName, pathValues := range perEndpoint {
		if val, ok := pathValues[endpointPath]; ok {
			result = append(result, spec.RequiredHeader{Name: headerName, Value: val})
		}
	}
	return result
}

// bearerKeywords indicate Bearer/token auth when found in info.description.
// Produces Type="bearer_token" with EnvVars suffix "_TOKEN".
var bearerKeywords = []string{
	"bearer",
	"access token",
	"auth token",
}

// apiKeyKeywords indicate API-key auth when found in info.description.
// Produces Type="api_key" with EnvVars suffix "_API_KEY".
// Only secret-key vendor prefixes (sk_*, cal_*), not publishable (pk_*).
var apiKeyKeywords = []string{
	"api key",
	"api_key",
	"authorization header",
	"sk_live_",
	"sk_test_",
	"cal_live_",
}

// negationWords suppress a keyword match when they appear within 5 words
// before the keyword, catching "does not require Bearer" patterns.
var negationWords = []string{"not", "no", "without", "unnecessary", "optional"}

// inferDescriptionAuth scans info.description for auth keywords when both
// selectSecurityScheme and inferQueryParamAuth produce nothing. This is the
// third and final tier of the auth detection pipeline.
func inferDescriptionAuth(doc *openapi3.T, name string, fallback spec.AuthConfig) spec.AuthConfig {
	if doc == nil || doc.Info == nil {
		return fallback
	}
	desc := strings.ToLower(doc.Info.Description)
	if desc == "" {
		return fallback
	}

	envPrefix := naming.EnvPrefix(name)

	// Check bearer keywords first (stronger signal for Bearer-prefix auth).
	// Scan all occurrences — a negated first mention ("does not require bearer")
	// should not prevent finding a later positive mention ("use a bearer token").
	for _, kw := range bearerKeywords {
		if findUnnegated(desc, kw) {
			return spec.AuthConfig{
				Type:     "bearer_token",
				In:       "header",
				Header:   "Authorization",
				EnvVars:  []string{envPrefix + "_TOKEN"},
				Inferred: true,
			}
		}
	}

	// Check API key keywords
	for _, kw := range apiKeyKeywords {
		if findUnnegated(desc, kw) {
			return spec.AuthConfig{
				Type:     "api_key",
				In:       "header",
				Header:   detectHeaderName(desc),
				EnvVars:  []string{envPrefix + "_API_KEY"},
				Inferred: true,
			}
		}
	}

	return fallback
}

// inferOperationLevelBearer scans all operations for required Authorization
// header parameters that identify themselves as Bearer tokens. This is the
// fourth-tier auth fallback — it fires only when securitySchemes, query-param
// inference, and description inference all fail.
func inferOperationLevelBearer(doc *openapi3.T, name string, fallback spec.AuthConfig) spec.AuthConfig {
	if doc == nil || doc.Paths == nil {
		return fallback
	}
	if hasTopLevelSecurityDeclaration(doc) {
		return fallback
	}

	authParamCount := 0
	hasBearerSignal := false
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
			if authParam, ok := requiredAuthorizationParam(pathItem, op); ok {
				authParamCount++
				if authorizationParamMentionsBearer(authParam) {
					hasBearerSignal = true
				}
			}
		}
	}

	if totalOps == 0 || !hasBearerSignal || float64(authParamCount)/float64(totalOps) < 0.8 {
		return fallback
	}

	envPrefix := naming.EnvPrefix(name)
	return spec.AuthConfig{
		Type:     "bearer_token",
		Header:   "Authorization",
		In:       "header",
		EnvVars:  []string{envPrefix + "_TOKEN"},
		Inferred: true,
	}
}

func hasTopLevelSecurityDeclaration(doc *openapi3.T) bool {
	return (doc.Components != nil && len(doc.Components.SecuritySchemes) > 0) || doc.Security != nil
}

func requiredAuthorizationParam(pathItem *openapi3.PathItem, op *openapi3.Operation) (*openapi3.Parameter, bool) {
	for _, p := range mergeParameters(pathItem, op) {
		if p.In == openapi3.ParameterInHeader && strings.EqualFold(p.Name, "Authorization") && p.Required {
			return p, true
		}
	}
	return nil, false
}

func authorizationParamMentionsBearer(p *openapi3.Parameter) bool {
	if p == nil {
		return false
	}
	if findUnnegated(strings.ToLower(p.Description), "bearer") {
		return true
	}
	if p.Schema != nil && p.Schema.Value != nil {
		return findUnnegated(strings.ToLower(p.Schema.Value.Description), "bearer")
	}
	return false
}

// commonCustomHeaders are header names that APIs use instead of Authorization.
// Checked case-insensitively against the description text.
var commonCustomHeaders = []string{
	"X-Api-Key",
	"X-API-Key",
	"X-Auth-Token",
	"X-Access-Token",
}

// detectHeaderName scans description text for a known custom auth header name.
// Returns the canonical casing if found, "Authorization" otherwise.
func detectHeaderName(desc string) string {
	lower := strings.ToLower(desc)
	for _, h := range commonCustomHeaders {
		if strings.Contains(lower, strings.ToLower(h)) {
			return h
		}
	}
	return "Authorization"
}

// findUnnegated scans all occurrences of keyword in text and returns true if
// at least one is not negated. Handles "sandbox does not require bearer, but
// production uses a bearer token" by scanning past the first negated match.
func findUnnegated(text, keyword string) bool {
	offset := 0
	for {
		idx := strings.Index(text[offset:], keyword)
		if idx < 0 {
			return false
		}
		absIdx := offset + idx
		if !isNegated(text, absIdx) {
			return true
		}
		offset = absIdx + len(keyword)
	}
}

// isNegated checks if any negation word appears as a whole word within ~50 chars
// before the keyword position, catching "does not require Bearer" while avoiding
// false negation on words like "Notion" that contain "no" as a substring.
func isNegated(text string, keywordIdx int) bool {
	start := max(keywordIdx-50, 0)
	preceding := text[start:keywordIdx]
	for _, neg := range negationWords {
		idx := strings.Index(preceding, neg)
		if idx < 0 {
			continue
		}
		// Check word boundaries: char before must be space/start, char after must be space/end
		beforeOk := idx == 0 || preceding[idx-1] == ' ' || preceding[idx-1] == ',' || preceding[idx-1] == '.'
		afterIdx := idx + len(neg)
		afterOk := afterIdx >= len(preceding) || preceding[afterIdx] == ' ' || preceding[afterIdx] == ',' || preceding[afterIdx] == '.'
		if beforeOk && afterOk {
			return true
		}
	}
	return false
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

// pathPriorityScore assigns a priority score to an API path so that
// user-facing endpoints sort before admin/internal ones. Higher is better.
// Scoring rules:
//   - Base score: 100
//   - Subtract 10 per path segment (depth penalty)
//   - Subtract 30 if any segment starts with admin, internal, system, or management
//   - Add 10 for short paths (2 or fewer segments)
func pathPriorityScore(path string) int {
	score := 100

	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return score + 10 // root path, short bonus
	}
	segments := strings.Split(trimmed, "/")
	score -= 10 * len(segments)

	if len(segments) <= 2 {
		score += 10
	}

	lowPath := strings.ToLower(path)
	for _, prefix := range []string{"admin", "internal", "system", "management"} {
		for seg := range strings.SplitSeq(strings.TrimPrefix(lowPath, "/"), "/") {
			// Match segments that start with the prefix (catches admin, admin.users, etc.)
			if strings.HasPrefix(seg, prefix) {
				score -= 30
				break
			}
		}
	}

	return score
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
	sort.SliceStable(pathKeys, func(i, j int) bool {
		si, sj := pathPriorityScore(pathKeys[i]), pathPriorityScore(pathKeys[j])
		if si != sj {
			return si > sj
		}
		return pathKeys[i] < pathKeys[j]
	})
	commonPrefix := detectCommonPrefix(pathKeys, basePath)

	// Auto-calibrate endpoint limit unless the user explicitly set it.
	// Pre-scans the spec to find the largest resource or sub-resource,
	// then bumps the limit so well-formed specs never have endpoints silently
	// skipped. The limit is checked per endpoint map (resource.Endpoints or
	// sub.Endpoints), so we count per (primary, sub) pair to find the true max.
	if !endpointLimitExplicit {
		type resourceKey struct{ primary, sub string }
		endpointCounts := map[resourceKey]int{}
		for _, path := range pathKeys {
			primaryName, subName := resourceAndSubFromPath(path, basePath, commonPrefix)
			if primaryName == "" {
				continue
			}
			pathItem := doc.Paths.Value(path)
			if pathItem != nil {
				endpointCounts[resourceKey{primaryName, subName}] += len(pathItem.Operations())
			}
		}
		for _, count := range endpointCounts {
			if count > maxEndpointsPerResource {
				maxEndpointsPerResource = count
			}
		}
	}

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

		// Read path-item-level extensions once per path. They apply to every
		// operation under this path item — sync resources are resource-scoped,
		// not method-scoped, so per-operation reads would either duplicate or
		// disagree on the same identity.
		pathResourceIDOverride := readPathItemResourceID(pathItem, path)
		pathCritical := readPathItemCritical(pathItem, path)
		pathTier := readTierExtension(pathItem.Extensions, fmt.Sprintf("path %q", path))

		primaryName, subName := resourceAndSubFromPath(path, basePath, commonPrefix)
		if primaryName == "" {
			warnf("skipping path %q: could not derive resource name", path)
			continue
		}

		// Framework cobra collision check — only for top-level resources
		// (subName == ""). Sub-resources register under a parent prefix and
		// don't shadow framework commands. If primaryName matches a reserved
		// cobra Use literal, rename to <api-slug>-<resource> and warn so
		// the operator sees it. Self-collision (the renamed name already
		// exists in out.Resources) bumps a numeric suffix until unique.
		if subName == "" {
			if _, reserved := spec.ReservedCobraUseNames[primaryName]; reserved {
				primaryName = renameForFrameworkCollision(out, primaryName, path)
			}
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
			endpoint.Tier = readTierExtension(op.Extensions, fmt.Sprintf("%s %q", strings.ToUpper(method), path))
			if endpoint.Tier == "" {
				endpoint.Tier = pathTier
			}

			endpoint.Response, endpoint.ResponsePath = mapResponse(op, endpointName)
			if strings.ToUpper(method) == "GET" {
				endpoint.Pagination = detectPagination(endpoint.Params, op)
			}
			endpoint.NoAuth = operationAllowsAnonymous(op, doc)

			// IDField fallback chain: explicit x-resource-id wins over
			// response-schema inference. Resolution happens at parse time so
			// the profiler sees a single resolved value per endpoint and
			// templates do not re-walk schemas at generation time.
			if pathResourceIDOverride != "" {
				endpoint.IDField = pathResourceIDOverride
			} else {
				endpoint.IDField = resolveIDFieldFromResponseSchema(op)
			}
			endpoint.Critical = pathCritical

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

// operationAllowsAnonymous checks whether an operation can be called without
// authentication. Returns true when:
//   - The operation has security: [] (explicit opt-out)
//   - The operation has security: [{}] (empty object = anonymous alternative)
//   - The operation inherits global security and the global security is empty
func operationAllowsAnonymous(op *openapi3.Operation, doc *openapi3.T) bool {
	if op.Security != nil {
		// Per-operation security declared
		if len(*op.Security) == 0 {
			return true // security: []
		}
		for _, req := range *op.Security {
			if len(req) == 0 {
				return true // security: [{}]
			}
		}
		return false
	}
	// op.Security is nil — inherits global security
	if doc.Security != nil && len(doc.Security) == 0 {
		return true // global security: []
	}
	for _, req := range doc.Security {
		if len(req) == 0 {
			return true // global security: [{}]
		}
	}
	return false
}

// resolveServerURL applies template-variable substitution and protocol
// normalization to an OpenAPI Server, returning either an absolute http(s)
// base URL or a relative base path. Empty strings indicate the server entry
// produced no usable URL (e.g., empty after trimming).
func resolveServerURL(server *openapi3.Server) (baseURL, basePath string) {
	if server == nil {
		return "", ""
	}
	serverURL := strings.TrimRight(strings.TrimSpace(server.URL), "/")
	// Resolve server URL template variables using defaults.
	if strings.Contains(serverURL, "{") && server.Variables != nil {
		for varName, variable := range server.Variables {
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
	if serverURL == "" {
		return "", ""
	}
	lowerURL := strings.ToLower(serverURL)
	if strings.HasPrefix(lowerURL, "http://") || strings.HasPrefix(lowerURL, "https://") {
		return serverURL, ""
	}
	// Relative URL — caller will need to surface as basePath.
	return "", serverURL
}

// mostCommonOperationServer scans every operation (and each operation's
// parent path-item) for `servers:` blocks, ranks resolved URLs by occurrence
// count, and returns the most common one. Used as a fallback when the spec
// has no top-level `servers:` block. Ties are broken deterministically by
// lexicographic URL order so the output doesn't churn across runs.
func mostCommonOperationServer(doc *openapi3.T) (baseURL, basePath string) {
	if doc == nil || doc.Paths == nil {
		return "", ""
	}
	urlCounts := map[string]int{}
	pathCounts := map[string]int{}
	tally := func(servers openapi3.Servers) {
		for _, srv := range servers {
			u, p := resolveServerURL(srv)
			if u != "" {
				urlCounts[u]++
			} else if p != "" {
				pathCounts[p]++
			}
		}
	}
	for _, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		tally(pathItem.Servers)
		for _, op := range pathItem.Operations() {
			if op != nil && op.Servers != nil {
				tally(*op.Servers)
			}
		}
	}
	// Prefer absolute URLs over relative paths when both exist — gives the
	// generated CLI a working default rather than a config-required relative.
	pickTopKey := func(m map[string]int) string {
		var best string
		var bestCount int
		for k, v := range m {
			if v > bestCount || (v == bestCount && k < best) {
				best = k
				bestCount = v
			}
		}
		return best
	}
	if u := pickTopKey(urlCounts); u != "" {
		return u, ""
	}
	if p := pickTopKey(pathCounts); p != "" {
		warnf("no top-level servers; using most common per-operation relative path %q (generated CLI will need base_url in config)", p)
		return "", p
	}
	return "", ""
}

func allOperationsAllowAnonymous(doc *openapi3.T) bool {
	if doc == nil || doc.Paths == nil {
		return false
	}
	seenOperation := false
	for _, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		for _, op := range pathItem.Operations() {
			if op == nil {
				continue
			}
			seenOperation = true
			if !operationAllowsAnonymous(op, doc) {
				return false
			}
		}
	}
	return seenOperation
}

// markAllEndpointsNoAuth sets NoAuth=true on every endpoint across all
// resources and sub-resources. Used when the spec has no authentication.
func markAllEndpointsNoAuth(resources map[string]spec.Resource) {
	for name, r := range resources {
		for eName, e := range r.Endpoints {
			e.NoAuth = true
			r.Endpoints[eName] = e
		}
		for subName, sub := range r.SubResources {
			for eName, e := range sub.Endpoints {
				e.NoAuth = true
				sub.Endpoints[eName] = e
			}
			r.SubResources[subName] = sub
		}
		resources[name] = r
	}
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
			if isPathSubstitutionParam(param) {
				continue
			}
			if _, ok := seen[param.Name]; ok {
				continue
			}
			seen[param.Name] = struct{}{}
			paramCounts[param.Name]++
		}
	})

	// "Global" only makes sense when there are several endpoints to be
	// global across. With 1 or 2 endpoints, every param is trivially on
	// 100% of them, which would strip the entire flag set from
	// single-endpoint specs (e.g., open-meteo: 70+ query params all
	// dropped, leaving `forecast` as a no-arg command). Require at least
	// 3 endpoints before applying the filter.
	const minEndpointsForGlobalFilter = 3
	if totalEndpoints < minEndpointsForGlobalFilter {
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
			if !isPathSubstitutionParam(param) {
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

func isPathSubstitutionParam(param spec.Param) bool {
	return param.Positional || param.PathParam
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

	// Reclassify path params that are modifiers (not entity identifiers) as flags.
	// This improves CLI UX: pagination/filter/date params become --flags with defaults
	// instead of required positional args.
	reclassifyPathParamModifiers(params)

	return params
}

// reclassifyPathParamModifiers converts path params that are modifiers (pagination,
// filters, dates) from positional args to flags with sensible defaults. This improves
// CLI UX — users type `order-list --page 2` instead of `order-list 2 10`.
//
// Classification priority (first match wins):
//  1. Has an enum → flag (user picks from a set)
//  2. Has a spec-declared default → flag with that default
//  3. Known pagination name → flag with sensible default
//  4. Date/time format → flag (defaults to empty, meaning "latest" or "today")
//  5. Anything left → stays positional (likely an entity identifier)
func reclassifyPathParamModifiers(params []spec.Param) {
	paginationDefaults := map[string]int{
		"page": 1, "pagenumber": 1, "page_number": 1,
		"pagesize": 10, "page_size": 10, "per_page": 10,
		"perpage": 10, "limit": 10, "count": 10,
		"maxresults": 10, "max_results": 10,
		"offset": 0, "skip": 0,
	}

	for i := range params {
		p := &params[i]
		if !p.Positional {
			continue // only reclassify path params
		}
		lowerName := strings.ToLower(p.Name)

		// Decide whether this path param should be a flag instead of positional.
		// Classification priority (first match wins):
		reclassify := false

		// 1. Has an enum → flag (user picks from a set)
		if len(p.Enum) > 0 {
			reclassify = true
			if p.Default == nil {
				p.Default = p.Enum[0]
			}
		}

		// 2. Has a spec-declared default → flag
		if !reclassify && p.Default != nil {
			reclassify = true
		}

		// 3. Known pagination name → flag with default
		if !reclassify {
			if def, ok := paginationDefaults[lowerName]; ok {
				reclassify = true
				p.Default = def
			}
		}

		// 4. Date/time format or name → flag
		if !reclassify {
			if p.Format == "date" || p.Format == "date-time" ||
				strings.Contains(lowerName, "date") ||
				strings.Contains(lowerName, "year") ||
				strings.Contains(lowerName, "month") {
				reclassify = true
			}
		}

		if reclassify {
			p.Positional = false
			p.PathParam = true
			// A path param is a URL segment — it can only be optional if a default
			// value can fill its slot. No default → the user must provide a value.
			p.Required = p.Default == nil
		}
	}
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
		paramSchema := bodyParamSchema(schema)
		description := schemaDescription(schema)
		if description == "" {
			description = schemaDescription(paramSchema)
		}
		if description == "" {
			description = humanizeFieldName(name)
		}
		param := spec.Param{
			Name:        name,
			Type:        mapSchemaType(paramSchema),
			Required:    isRequired(required, name),
			Description: description,
			Fields:      mapBodyFields(paramSchema),
			Enum:        schemaEnum(paramSchema),
			Format:      schemaFormat(paramSchema),
		}
		if paramSchema != nil && paramSchema.Default != nil {
			param.Default = paramSchema.Default
		}
		// For array types, propagate item-level enum as a Fields entry
		// so downstream consumers (profiler) can access it.
		if paramSchema != nil && paramSchema.Type != nil && paramSchema.Type.Is(openapi3.TypeArray) &&
			paramSchema.Items != nil && paramSchema.Items.Value != nil && len(paramSchema.Items.Value.Enum) > 0 {
			param.Fields = []spec.Param{{
				Name: "items",
				Type: "string",
				Enum: schemaEnum(paramSchema.Items.Value),
			}}
		}
		body = append(body, param)
	}

	return body
}

func bodyParamSchema(schema *openapi3.Schema) *openapi3.Schema {
	if schema == nil || len(schema.AllOf) == 0 {
		return schema
	}
	if schema.Type != nil || len(schema.Properties) > 0 || schema.Items != nil {
		return schema
	}

	var mergedObject *openapi3.Schema
	required := map[string]struct{}{}
	for _, candidate := range schema.AllOf {
		value := schemaRefValue(candidate)
		if value == nil {
			continue
		}
		if isObjectSchema(value) {
			if mergedObject == nil {
				mergedObject = &openapi3.Schema{
					Type:       &openapi3.Types{openapi3.TypeObject},
					Properties: map[string]*openapi3.SchemaRef{},
				}
			}
			if mergedObject.Description == "" {
				mergedObject.Description = value.Description
			}
			maps.Copy(mergedObject.Properties, value.Properties)
			for _, name := range value.Required {
				required[name] = struct{}{}
			}
			continue
		}
		if mergedObject == nil && (value.Type != nil || value.Items != nil) {
			return value
		}
	}
	if mergedObject != nil {
		for name := range required {
			mergedObject.Required = append(mergedObject.Required, name)
		}
		sort.Strings(mergedObject.Required)
		return mergedObject
	}
	return schema
}

func mapBodyFields(schema *openapi3.Schema) []spec.Param {
	return mapBodyFieldsDepth(schema, map[*openapi3.Schema]struct{}{}, 0)
}

const maxBodyFieldDepth = 8

func mapBodyFieldsDepth(schema *openapi3.Schema, visited map[*openapi3.Schema]struct{}, depth int) []spec.Param {
	if !isObjectSchema(schema) || len(schema.Properties) == 0 {
		return nil
	}
	if depth >= maxBodyFieldDepth {
		return nil
	}
	if _, ok := visited[schema]; ok {
		return nil
	}
	visited[schema] = struct{}{}
	defer delete(visited, schema)

	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	sort.Strings(names)

	required := map[string]struct{}{}
	for _, name := range schema.Required {
		required[name] = struct{}{}
	}

	fields := make([]spec.Param, 0, len(names))
	for _, name := range names {
		fieldSchema := bodyParamSchema(schemaRefValue(schema.Properties[name]))
		description := schemaDescription(schemaRefValue(schema.Properties[name]))
		if description == "" {
			description = schemaDescription(fieldSchema)
		}
		if description == "" {
			description = humanizeFieldName(name)
		}
		fields = append(fields, spec.Param{
			Name:        name,
			Type:        mapSchemaType(fieldSchema),
			Required:    isRequired(required, name),
			Description: description,
			Fields:      mapBodyFieldsDepth(fieldSchema, visited, depth+1),
			Enum:        schemaEnum(fieldSchema),
			Format:      schemaFormat(fieldSchema),
		})
	}
	return fields
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
		properties[naming.ASCIIFold(name)] = prop
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
			itemRef := schemaRefValue(dataRef).Items
			return spec.ResponseDef{
				Type:          "array",
				Item:          schemaTypeName(itemRef, fallbackName+"Item"),
				Discriminator: mapResponseDiscriminator(itemRef),
			}, "data"
		}
	}

	if isArraySchema(schema) {
		return spec.ResponseDef{
			Type:          "array",
			Item:          schemaTypeName(schema.Items, fallbackName+"Item"),
			Discriminator: mapResponseDiscriminator(schema.Items),
		}, ""
	}

	if isObjectSchema(schema) {
		return spec.ResponseDef{
			Type:          "object",
			Item:          schemaTypeName(schemaRef, fallbackName+"Response"),
			Discriminator: mapResponseDiscriminator(schemaRef),
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

// readPathItemResourceID reads the `x-resource-id` extension from a path item
// and returns the resolved field name. Accepts only string values; non-string
// values (numbers, booleans, malformed YAML) emit a warning and return "".
// Empty/missing extensions return "" without warning.
func readPathItemResourceID(pathItem *openapi3.PathItem, path string) string {
	if pathItem == nil || pathItem.Extensions == nil {
		return ""
	}
	raw, ok := pathItem.Extensions["x-resource-id"]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		warnf("path %q: x-resource-id must be a string, got %T; ignoring", path, raw)
		return ""
	}
}

// readPathItemCritical reads the `x-critical` extension from a path item.
// Accepts native booleans and the truthy strings "true"/"1" (case-insensitive).
// Other shapes emit a warning and return false.
func readPathItemCritical(pathItem *openapi3.PathItem, path string) bool {
	if pathItem == nil || pathItem.Extensions == nil {
		return false
	}
	raw, ok := pathItem.Extensions["x-critical"]
	if !ok {
		return false
	}
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1":
			return true
		case "false", "0", "":
			return false
		default:
			warnf("path %q: x-critical string %q is not truthy; treating as false", path, v)
			return false
		}
	default:
		warnf("path %q: x-critical must be bool or truthy string, got %T; treating as false", path, raw)
		return false
	}
}

func readTierExtension(extensions map[string]any, context string) string {
	if extensions == nil {
		return ""
	}
	raw, ok := extensions[extensionTier]
	if !ok {
		return ""
	}
	tier, ok := raw.(string)
	if !ok {
		warnf("%s: %s must be a string, got %T; ignoring", context, extensionTier, raw)
		return ""
	}
	return strings.TrimSpace(tier)
}

// resolveIDFieldFromResponseSchema implements tiers 2-4 of the IDField fallback
// chain: prefer "id", then "name", then the first scalar field listed in the
// response schema's `required:` array (walking properties in their schema order).
// Returns "" when no field qualifies; templates fall through to runtime list
// scanning. Tier 1 (`x-resource-id` extension) is handled separately by the
// caller — it overrides every tier here.
func resolveIDFieldFromResponseSchema(op *openapi3.Operation) string {
	if op == nil || op.Responses == nil {
		return ""
	}
	success := selectSuccessResponse(op.Responses)
	if success == nil || success.Value == nil {
		return ""
	}
	schemaRef := selectResponseSchema(success.Value)
	if schemaRef == nil || schemaRef.Value == nil {
		return ""
	}

	// Walk to the item schema: arrays, {data: [...]} wrappers, or the object itself.
	itemSchema := unwrapItemSchema(schemaRef.Value)
	if itemSchema == nil {
		return ""
	}

	// Tier 2: explicit `id` (required or optional)
	if _, ok := itemSchema.Properties["id"]; ok {
		return "id"
	}
	// Tier 3: explicit `name`
	if _, ok := itemSchema.Properties["name"]; ok {
		return "name"
	}

	// Tier 4: first scalar field appearing in the schema's required[] array,
	// matched against properties in their schema-declared order. kin-openapi
	// preserves YAML/JSON property order in MapKeys/Extensions but not via
	// range over Properties (it's a Go map). Fall back to iterating the
	// required[] slice itself: that order is stable and is what spec authors
	// intend when they care about which field "wins."
	for _, fieldName := range itemSchema.Required {
		propRef, ok := itemSchema.Properties[fieldName]
		if !ok || propRef == nil || propRef.Value == nil {
			continue
		}
		if isScalarSchema(propRef.Value) {
			return fieldName
		}
	}

	return ""
}

// unwrapItemSchema returns the schema of items inside a list response. Handles
// three shapes: bare object (no array, treat as a single-item resource and
// inspect its fields), bare array (return Items.Value), or {data: [...]}
// wrapper (mirroring mapResponse's handling).
func unwrapItemSchema(schema *openapi3.Schema) *openapi3.Schema {
	if schema == nil {
		return nil
	}
	if isArraySchema(schema) && schema.Items != nil {
		return schemaRefValue(schema.Items)
	}
	if isObjectSchema(schema) {
		// {data: [...]} wrapper convention
		if dataRef, ok := schema.Properties["data"]; ok {
			if data := schemaRefValue(dataRef); isArraySchema(data) && data.Items != nil {
				return schemaRefValue(data.Items)
			}
		}
		return schema
	}
	return nil
}

// isScalarSchema reports whether the schema's type is a scalar — string,
// integer, number, or boolean. Excludes objects, arrays, and refs that resolve
// to either. Used by tier 4 of the IDField fallback chain to skip non-scalar
// fields when picking a primary key.
func isScalarSchema(schema *openapi3.Schema) bool {
	if schema == nil || schema.Type == nil {
		return false
	}
	switch {
	case schema.Type.Includes(openapi3.TypeString),
		schema.Type.Includes(openapi3.TypeInteger),
		schema.Type.Includes(openapi3.TypeNumber),
		schema.Type.Includes(openapi3.TypeBoolean):
		return true
	}
	return false
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
		if isJSONAPIResourceSchema(schema) {
			jsonAPIFlattenInto(schema, properties)
		} else {
			collectTypeProperties(schemaRef, properties, map[*openapi3.Schema]struct{}{})
		}

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
			fieldSchema := schemaRefValue(properties[fieldName])
			fields = append(fields, spec.TypeField{
				Name: fieldName,
				Type: mapSchemaType(fieldSchema),
				Enum: schemaEnum(fieldSchema),
			})
		}

		out.Types[goName] = spec.TypeDef{Fields: fields}
	}
}

func mapResponseDiscriminator(schemaRef *openapi3.SchemaRef) *spec.ResponseDiscriminator {
	schema := schemaRefValue(schemaRef)
	if schema == nil || schema.Discriminator == nil || strings.TrimSpace(schema.Discriminator.PropertyName) == "" {
		return nil
	}

	out := &spec.ResponseDiscriminator{
		Field:   strings.TrimSpace(schema.Discriminator.PropertyName),
		Mapping: map[string]string{},
	}
	if len(schema.Discriminator.Mapping) == 0 {
		return out
	}

	values := make([]string, 0, len(schema.Discriminator.Mapping))
	for value := range schema.Discriminator.Mapping {
		values = append(values, value)
	}
	sort.Strings(values)
	for _, value := range values {
		ref := schema.Discriminator.Mapping[value].Ref
		target := refComponentName(ref)
		if target == "" {
			target = ref
		}
		if target == "" {
			target = value
		}
		out.Mapping[value] = toTypeName(target)
	}
	return out
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
		properties[naming.ASCIIFold(name)] = prop
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
	case schema.Type.Includes(openapi3.TypeArray):
		return "array"
	case schema.Type.Includes(openapi3.TypeObject):
		return "object"
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

// isJSONAPIResourceSchema reports whether the schema is a JSON:API
// (jsonapi.org/format) Resource Object: a string `type` discriminator,
// a string `id`, and an `attributes` object. The trio is tight enough
// that vanilla REST specs (Stripe, HubSpot, Twilio) never accidentally
// match — they don't co-locate type+id+attributes.
func isJSONAPIResourceSchema(schema *openapi3.Schema) bool {
	if !isObjectSchema(schema) {
		return false
	}
	typeRef, hasType := schema.Properties["type"]
	if !hasType {
		return false
	}
	typeVal := schemaRefValue(typeRef)
	if typeVal == nil || typeVal.Type == nil || !typeVal.Type.Includes(openapi3.TypeString) {
		return false
	}
	idRef, hasID := schema.Properties["id"]
	if !hasID {
		return false
	}
	idVal := schemaRefValue(idRef)
	if idVal == nil || idVal.Type == nil || !idVal.Type.Includes(openapi3.TypeString) {
		return false
	}
	attrsRef, hasAttrs := schema.Properties["attributes"]
	if !hasAttrs {
		return false
	}
	if !isObjectSchema(schemaRefValue(attrsRef)) {
		return false
	}
	return true
}

// jsonAPIFlattenInto populates properties with the canonical projection
// of a JSON:API Resource Object: id (preserved), attributes.* (hoisted
// to top-level), discriminator `type` dropped (constant per resource).
// Relationships are intentionally omitted in this pass; foreign-key
// extraction is a follow-up.
func jsonAPIFlattenInto(schema *openapi3.Schema, properties map[string]*openapi3.SchemaRef) {
	if schema == nil {
		return
	}
	if idRef, ok := schema.Properties["id"]; ok && idRef != nil {
		properties["id"] = idRef
	}
	attrsRef, ok := schema.Properties["attributes"]
	if !ok || attrsRef == nil || attrsRef.Value == nil {
		return
	}
	for name, prop := range attrsRef.Value.Properties {
		if prop == nil {
			continue
		}
		if strings.HasPrefix(name, "_") {
			continue
		}
		properties[name] = prop
	}
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

	for len(segments) > 0 {
		if isVersionSegment(segments[0]) {
			segments = segments[1:]
			continue
		}

		// Strip generic API routing prefixes when followed by a concrete
		// resource or another routing segment. "api", "apis", "rest" are
		// infrastructure, not resource names. /v1/api/users and
		// /api/v2/users both normalize to "users".
		// Do NOT strip when the next segment is a path param ({id}), because
		// /api/{id} means "api" IS the resource.
		if len(segments) >= 2 && isGenericAPIPrefix(segments[0]) && !isPathParamSegment(segments[1]) {
			segments = segments[1:]
			continue
		}

		break
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

// isGenericAPIPrefix returns true if a path segment is a routing-only prefix
// that should be stripped when extracting resource names. These segments are
// API infrastructure ("api", "rest") rather than domain resources.
func isGenericAPIPrefix(segment string) bool {
	switch strings.ToLower(segment) {
	case "api", "apis", "rest":
		return true
	default:
		return false
	}
}

func isVersionSegment(segment string) bool {
	if segment == "" {
		return false
	}
	if segment[0] == 'v' && len(segment) >= 2 {
		_, err := strconv.Atoi(segment[1:])
		return err == nil
	}
	_, err := strconv.Atoi(segment)
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
	name = stripOperationIDControllerAndDate(name)
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

// operationIDDatePattern matches embedded version dates (YYYY_MM_DD) in snake_case operationIDs.
// These appear in specs like Cal.com: BookingsController_2024-08-13_getBooking → 2024_08_13
var operationIDDatePattern = regexp.MustCompile(`_\d{4}_\d{2}_\d{2}_?`)

// stripOperationIDControllerAndDate removes controller class names and embedded
// version dates from operationIDs. APIs auto-generated from NestJS, FastAPI, or
// similar frameworks include these patterns (e.g., BookingsController_2024-08-13_getBooking).
func stripOperationIDControllerAndDate(name string) string {
	// Strip controller segments: "_controller_" mid-name or "_controller" suffix
	name = strings.ReplaceAll(name, "_controller_", "_")
	name = strings.TrimSuffix(name, "_controller")

	// Strip embedded version dates (YYYY_MM_DD)
	name = operationIDDatePattern.ReplaceAllString(name, "_")

	// Clean up doubled underscores from removals
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}
	return strings.Trim(name, "_")
}

func stripOperationIDResourcePrefix(name string, variants []string) string {
	if name == "" || len(variants) == 0 {
		return name
	}

	for {
		stripped := false
		for _, variant := range variants {
			prefix := variant + "_"
			if after, ok := strings.CutPrefix(name, prefix); ok {
				name = after
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
	input = naming.ASCIIFold(input)
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
	name = naming.ASCIIFold(name)
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
	if isGoKeyword(result) {
		result = "T" + result
	}
	return result
}

// goKeywords is the set of reserved words from the Go language spec
// (https://go.dev/ref/spec#Keywords). When a sanitized type name matches one
// of these, the generated `type X struct { ... }` will fail to parse;
// sanitize prepends "T" to dodge the collision. Predeclared identifiers
// (bool, int, string, error, etc.) shadow rather than fail and are
// intentionally excluded.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// isGoKeyword reports whether s is a reserved word in the Go language spec.
func isGoKeyword(s string) bool {
	return goKeywords[s]
}

func toCamelCase(s string) string {
	s = naming.ASCIIFold(s)
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
	return toKebabCaseInternal(input, true)
}

func toKebabCaseInternal(input string, foldASCII bool) string {
	if foldASCII {
		input = naming.ASCIIFold(input)
	}
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

// specNameNoiseWords are tokens stripped during slug derivation. Shared
// across both fold variants so adding a word affects slug and display_name.
var specNameNoiseWords = map[string]struct{}{
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

func cleanSpecName(title string) string {
	return cleanSpecNameInternal(title, true)
}

// cleanSpecNameUnicode is cleanSpecName without the ASCII fold, so display_name
// keeps accents the slug must drop for filesystem and shell safety.
func cleanSpecNameUnicode(title string) string {
	return cleanSpecNameInternal(title, false)
}

func cleanSpecNameInternal(title string, foldASCII bool) string {
	if foldASCII {
		title = naming.ASCIIFold(title)
	}
	title = strings.ToLower(strings.TrimSpace(title))
	if title == "" {
		return "api"
	}

	title = strings.ReplaceAll(title, "open api", " ")

	// Strip apostrophes so brand names like "Domino's" become "dominos" not
	// "domino-s". When foldASCII is false, smart-quote U+2019 hasn't been
	// folded to ASCII '\'' yet, so strip both forms.
	title = strings.ReplaceAll(title, "'", "")
	title = strings.ReplaceAll(title, "’", "")

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

	filtered := make([]string, 0, len(tokens))
	for _, token := range tokens {
		// The noise list is ASCII; fold for the membership check so accented
		// variants still match, but keep the original token in the output.
		compare := token
		if !foldASCII {
			compare = naming.ASCIIFold(token)
		}
		if _, ok := specNameNoiseWords[compare]; ok {
			continue
		}
		if isVersionToken(compare) {
			continue
		}
		filtered = append(filtered, token)
	}
	if len(filtered) > 3 {
		filtered = filtered[:3]
	}

	name := toKebabCaseInternal(strings.Join(filtered, " "), foldASCII)
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

	if after, ok := strings.CutPrefix(token, "v"); ok {
		token = after
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

// selectDescription chooses between an OpenAPI operation's summary and
// description. OpenAPI convention has summary as a one-line title and
// description as long-form prose; when both exist, description carries
// the richer agent-useful detail and wins. Fall back to summary when
// description is empty, or when description is shorter than summary
// (rare: placeholder strings like "TODO" or a truncated migration
// artifact). Single-word/mangled summaries get humanized in the
// fallback path.
func selectDescription(summary, description string) string {
	if description != "" && len(description) >= len(summary) {
		return description
	}
	if summary != "" {
		if shouldHumanizeDescription(summary) {
			return humanizeDescription(summary)
		}
		if looksLikeMangledOperationID(summary) {
			return humanizeConcatenated(summary)
		}
		return summary
	}
	return description
}

func detectPagination(params []spec.Param, op *openapi3.Operation) *spec.Pagination {
	paramNames := map[string]struct{}{}
	for _, p := range params {
		paramNames[strings.ToLower(p.Name)] = struct{}{}
	}

	var pag spec.Pagination

	// Detect limit param
	for _, name := range []string{"limit", "maxresults", "pagesize", "page_size", "max_results", "per_page", "page[size]"} {
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
		for _, name := range []string{"after", "cursor", "page[cursor]"} {
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

// warnWriter is the destination for warnf. Tests swap it for an
// in-memory buffer; production code writes to os.Stderr.
var warnWriter io.Writer = os.Stderr

func warnf(format string, args ...any) {
	fmt.Fprintf(warnWriter, "warning: "+format+"\n", args...)
}

// renameForFrameworkCollision returns a kebab-case resource name that
// won't shadow a framework cobra command. The default form is
// `<api-slug>-<original>`; if that itself collides with another resource
// already in out.Resources, a numeric suffix (`-2`, `-3`, ...) is added
// until the name is unique. A warning is emitted on every rename so the
// operator sees what happened.
//
// Falls back to "api" when out.Name is empty so the rename never
// produces a leading-hyphen name like "-version".
func renameForFrameworkCollision(out *spec.APISpec, original, path string) string {
	slug := out.Name
	if slug == "" {
		slug = "api"
	}
	candidate := slug + "-" + original
	if _, exists := out.Resources[candidate]; exists {
		// Suffix-bump on self-collision. Bounded by maxResources, which
		// the outer loop already enforces, so the loop terminates.
		for i := 2; ; i++ {
			next := fmt.Sprintf("%s-%d", candidate, i)
			if _, exists := out.Resources[next]; !exists {
				candidate = next
				break
			}
		}
	}
	warnf("resource %q from path %q would shadow framework cobra command %q; renamed to %q", original, path, original, candidate)
	return candidate
}
