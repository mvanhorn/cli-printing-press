package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	maxResources                 = 500
	maxEndpointsPerResource      = 50
	endpointLimitExplicit        = false // true when user set --max-endpoints-per-resource
	globalScopeParamNormalizerRE = regexp.MustCompile(`[^a-zA-Z0-9]+`)
	authFormatPlaceholderRE      = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)
)

type additionalHeaderFallbackMode int

const (
	additionalHeaderFallbackNone additionalHeaderFallbackMode = iota
	additionalHeaderFallbackScheme
	additionalHeaderFallbackHeader
)

const (
	extensionAuthEnvVars           = "x-auth-env-vars"
	extensionAuthVars              = "x-auth-vars"
	extensionAuthOptional          = "x-auth-optional"
	extensionAuthKeyURL            = "x-auth-key-url"
	extensionAuthInstructions      = "x-auth-instructions"
	extensionAuthTitle             = "x-auth-title"
	extensionAuthDescription       = "x-auth-description"
	extensionAuthCompanion         = "x-auth-companion"
	extensionAuthSubtype           = "x-auth-subtype"
	extensionAuthBasicUsername     = "x-auth-basic-username"
	extensionAuthBasicPassword     = "x-auth-basic-password"
	extensionOAuthDeviceFlow       = "x-oauth-device-flow"
	extensionOAuthRefreshTokenMech = "x-oauth-refresh-token-mechanism"
	extensionSpeakeasyExample      = "x-speakeasy-example"
	extensionTierRouting           = "x-tier-routing"
	extensionTier                  = "x-tier"
	extensionRoles                 = "x-roles"
	extensionRequiresRole          = "x-requires-role"
	extensionRateClass             = "x-rate-class"
	extensionDefaultRateLimit      = "x-pp-default-rate-limit"
	extensionMCP                   = "x-mcp"
	extensionLegacyMCP             = "mcp"
	extensionDataSourceStrategy    = "x-data-source-strategy"
	extensionCache                 = "x-cache"
	extensionLearn                 = "x-learn"
	extensionStreaming             = "x-streaming"
	extensionPPQuery               = "x-pp-query"
	extensionPPSyncable            = "x-pp-syncable"
	extensionPPPagination          = "x-pp-pagination"
	extensionSyncWalker            = "x-pp-sync-walker"
	extensionHappyArgs             = "x-happy-args"
	extensionPPExample             = "x-pp-example"
	extensionLiveDogfoodTier       = "x-live-dogfood-requires-tier"
	extensionDispatchParam         = "x-pp-dispatch-param"
	extensionPPResource            = "x-pp-resource"
	extensionParamURLName          = "x-url-name"
	extensionParamURLNames         = "x-param-url-names"
	extensionAPIName               = "x-api-name"
	extensionDisplayName           = "x-display-name"
	extensionWebsite               = "x-website"
	extensionProxyRoutes           = "x-proxy-routes"
	extensionOrigin                = "x-origin"
	extensionProviderName          = "x-providerName"
	// extensionTenantEnvVar declares the env-var name that resolves the
	// {tenant} path-positional template in multi-tenant SaaS APIs (every
	// path is /tenant/{tenant}/...). When set, the parser registers
	// "tenant" as an EndpointTemplateVar with this env var as the override,
	// so the profiler treats /tenant/{tenant}/<resource> paths as standalone
	// sync resources rather than parent-context-dependent.
	extensionTenantEnvVar = "x-tenant-env-var"
	// extensionPathTemplateEnvVars is the generic, map-shaped successor to
	// extensionTenantEnvVar. Each entry binds a path placeholder to an
	// object with two optional fields: `env` registers a runtime env-var
	// override that flows into EndpointTemplateVars (same bucket as
	// x-tenant-env-var, suitable for BaseURL placeholders like
	// {workspace} or {org}); `default` bakes a literal into operation
	// paths at generation time and drops the matching path parameter,
	// suitable for canonical always-valid values like Gmail's userId='me'.
	// When both are set, default wins and the env field is ignored — the
	// placeholder is fully resolved before runtime substitution sees it.
	extensionPathTemplateEnvVars = "x-path-template-env-vars"
)

// tenantPlaceholderName is the canonical placeholder that x-tenant-env-var
// maps to. Kept narrow on purpose — when this generalizes beyond ServiceTitan
// (Atlassian {workspace}, GitHub {org}), promote to a list-shaped extension
// rather than overloading this constant.
const tenantPlaceholderName = "tenant"

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

func stubMissingLocalSchemaRefs(data []byte) ([]byte, int, error) {
	root, _, schemas, names, err := findMissingLocalSchemaRefs(data)
	if err != nil {
		return data, 0, err
	}
	if len(names) == 0 {
		return data, 0, nil
	}

	for _, name := range names {
		if _, exists := schemas[name]; exists {
			continue
		}
		schemas[name] = map[string]any{
			"type":                 "object",
			"description":          fmt.Sprintf("Stub for missing local schema ref %s.", name),
			"additionalProperties": true,
		}
		warnf("stubbing missing local schema ref %q as permissive object", name)
	}

	stubbed, err := json.Marshal(root)
	if err != nil {
		return data, 0, err
	}
	return stubbed, len(names), nil
}

func findMissingLocalSchemaRefs(data []byte) (map[string]any, map[string]any, map[string]any, []string, error) {
	if !bytes.Contains(data, []byte(localSchemaRefPrefix)) {
		return nil, nil, nil, nil, nil
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, nil, nil, nil, err
	}

	components := objectValue(root["components"])
	schemas := objectValue(components["schemas"])
	missing := make(map[string]struct{})
	collectMissingLocalSchemaRefs(root, "", schemas, missing)
	if len(missing) == 0 {
		return root, components, schemas, nil, nil
	}

	if components == nil {
		components = make(map[string]any)
		root["components"] = components
	}
	if schemas == nil {
		schemas = make(map[string]any)
		components["schemas"] = schemas
	}

	names := make([]string, 0, len(missing))
	for name := range missing {
		names = append(names, name)
	}
	sort.Strings(names)

	return root, components, schemas, names, nil
}

func collectMissingLocalSchemaRefs(v any, parentKey string, schemas map[string]any, missing map[string]struct{}) {
	if isArbitrarySpecDataKey(parentKey) || strings.HasPrefix(parentKey, "x-") {
		return
	}
	switch typed := v.(type) {
	case map[string]any:
		if rawRef, ok := typed["$ref"].(string); ok {
			if name, ok := localSchemaRefName(rawRef); ok {
				if _, exists := schemas[name]; !exists {
					missing[name] = struct{}{}
				}
			}
		}
		for key, child := range typed {
			collectMissingLocalSchemaRefs(child, key, schemas, missing)
		}
	case []any:
		for _, child := range typed {
			collectMissingLocalSchemaRefs(child, parentKey, schemas, missing)
		}
	}
}

func isArbitrarySpecDataKey(key string) bool {
	switch key {
	case "example", "examples", "default":
		return true
	default:
		return false
	}
}

func localSchemaRefName(ref string) (string, bool) {
	if !strings.HasPrefix(ref, localSchemaRefPrefix) {
		return "", false
	}
	rest := strings.TrimPrefix(ref, localSchemaRefPrefix)
	if rest == "" {
		return "", false
	}
	name, suffix, hasSuffix := strings.Cut(rest, "/")
	if name == "" {
		return "", false
	}
	if hasSuffix && suffix != "" {
		return "", false
	}
	if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}
	name = strings.NewReplacer("~1", "/", "~0", "~").Replace(name)
	return name, true
}

const localSchemaRefPrefix = "#/components/schemas/"

func objectValue(v any) map[string]any {
	obj, _ := v.(map[string]any)
	return obj
}

// Parse parses an OpenAPI spec strictly. Use ParseLenient for specs with broken $refs.
func Parse(data []byte) (*spec.APISpec, error) {
	return ParseWithOptions(data, ParseOptions{})
}

// ParseFile parses an OpenAPI spec from a file and resolves local external
// refs relative to that file.
func ParseFile(path string) (*spec.APISpec, error) {
	return parseFileWithOptions(path, ParseOptions{})
}

// ParseWithPath parses OpenAPI spec bytes and resolves local external refs
// relative to the given file path.
func ParseWithPath(data []byte, path string) (*spec.APISpec, error) {
	return ParseWithOptions(data, ParseOptions{Path: path})
}

// ParseLenient parses an OpenAPI spec, skipping validation errors from broken $refs.
// It logs warnings to stderr for any issues found but continues parsing.
func ParseLenient(data []byte) (*spec.APISpec, error) {
	return ParseWithOptions(data, ParseOptions{Lenient: true})
}

// ParseFileLenient parses an OpenAPI spec from a file and skips validation
// errors from broken $refs after resolving local external refs relative to
// that file.
func ParseFileLenient(path string) (*spec.APISpec, error) {
	return parseFileWithOptions(path, ParseOptions{Lenient: true})
}

// ParseWithPathLenient parses OpenAPI spec bytes, resolving local external refs
// relative to the given file path and skipping validation errors from broken
// refs.
func ParseWithPathLenient(data []byte, path string) (*spec.APISpec, error) {
	return ParseWithOptions(data, ParseOptions{Path: path, Lenient: true})
}

// ParseOptions carries optional hints that influence parsing without changing
// the existing single-purpose Parse* signatures. New behavioral knobs (e.g.
// auth-scheme preference) should be added here rather than as new positional
// parameters across every entry point.
type ParseOptions struct {
	// Path resolves local external refs relative to a file location. Empty
	// means the spec is treated as remote/in-memory only.
	Path string
	// Lenient skips validation errors from broken $refs.
	Lenient bool
	// Set when callers need other lenient cleanup while preserving missing-ref
	// failures.
	StrictRefs bool
	// AuthPreference names a security scheme from components.securitySchemes
	// that should win over the parser's default selection priority. Used when
	// a spec exposes multiple valid schemes (e.g. OAuth2 + basic) and the
	// caller knows which one fits the printed CLI's intended auth model.
	// Unknown names are ignored (default selection runs).
	AuthPreference string
	// SourceURL is the http(s) URL the spec was fetched from, when the spec
	// is remote. Used to derive an absolute BaseURL for specs whose servers:
	// block is relative-only (e.g. {url: /api/v3}). It does NOT affect $ref
	// resolution (that uses Path/location).
	SourceURL string
}

// ParseWithOptions is the canonical parser entry point; the older Parse* and
// ParseFile* helpers delegate to it with default ParseOptions plus their own
// path/lenient settings.
func ParseWithOptions(data []byte, opts ParseOptions) (*spec.APISpec, error) {
	var sourceURL *url.URL
	if opts.SourceURL != "" {
		if u, err := url.Parse(opts.SourceURL); err == nil {
			sourceURL = u
		}
	}
	if opts.Path == "" {
		return parseWithLocation(data, opts.Lenient, opts.StrictRefs, nil, sourceURL, opts.AuthPreference)
	}
	location, err := fileLocation(opts.Path)
	if err != nil {
		return nil, err
	}
	return parseWithLocation(data, opts.Lenient, opts.StrictRefs, location, sourceURL, opts.AuthPreference)
}

func parseFileWithOptions(path string, opts ParseOptions) (*spec.APISpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading OpenAPI spec: %w", err)
	}
	opts.Path = path
	return ParseWithOptions(data, opts)
}

func parseWithLocation(data []byte, lenient bool, strictRefs bool, location *url.URL, sourceURL *url.URL, authPreference string) (*spec.APISpec, error) {
	var metadata specDataMetadata
	if normalized, meta, err := normalizeSpecDataWithMetadata(data); err == nil {
		data = normalized
		metadata = meta
	}
	if lenient && !strictRefs {
		if stubbed, count, err := stubMissingLocalSchemaRefs(data); err == nil && count > 0 {
			data = stubbed
		}
	}
	if lenient && strictRefs {
		if _, _, _, missing, err := findMissingLocalSchemaRefs(data); err == nil && len(missing) > 0 {
			return nil, fmt.Errorf("missing local schema refs: %s", strings.Join(missing, ", "))
		}
	}
	doc, err := loadOpenAPIDoc(data, lenient, location)
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
			doc, err = loadOpenAPIDoc(data, lenient, location)
		}
		if err != nil {
			return nil, fmt.Errorf("loading OpenAPI spec (even after cleanup): %w", err)
		}
		fmt.Fprintf(os.Stderr, "info: spec loaded after stripping broken references\n")
	}

	// Skip InternalizeRefs when the spec has no external $refs (every $ref starts
	// with '#'). The Render Public API spec has 314 cross-referenced schemas;
	// kin-openapi's DefaultRefNameResolver-driven recursion runs > 10 minutes
	// on it. Lazy ref resolution still works for in-document refs during access.
	//
	// Format-agnostic scan: matches both JSON (`"$ref": "..."`) and YAML
	// (`$ref: '...'` or unquoted `$ref: ./schemas/foo.yaml`). A `$ref` whose
	// value's first non-quote character is anything but '#' is external, and
	// we must call InternalizeRefs to resolve it.
	hasExternalRef := false
	for off := 0; off < len(data); {
		idx := bytes.Index(data[off:], []byte("$ref"))
		if idx < 0 {
			break
		}
		off += idx + len("$ref")
		// Skip optional closing quote (JSON `"$ref"`) and whitespace before the colon.
		for off < len(data) && (data[off] == '"' || data[off] == '\'' || data[off] == ' ' || data[off] == '\t') {
			off++
		}
		if off >= len(data) || data[off] != ':' {
			continue
		}
		off++
		for off < len(data) && (data[off] == ' ' || data[off] == '\t') {
			off++
		}
		if off < len(data) && (data[off] == '"' || data[off] == '\'') {
			off++
		}
		if off >= len(data) {
			break
		}
		if data[off] != '#' {
			hasExternalRef = true
			break
		}
	}
	if hasExternalRef {
		doc.InternalizeRefs(context.Background(), nil)
	}

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
	if isPlaceholderURL(websiteURL) {
		websiteURL = ""
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
	// serverTemplatePlaceholders / serverTemplateDefaults carry the placeholder
	// names and their spec-declared defaults when the top-level server URL is
	// a multi-tenant template (e.g. `https://{domain}/api/v2`). Empty for
	// static specs so per-tenant runtime substitution stays opt-in and the
	// existing byte-compat goldens for single-host APIs don't churn.
	var serverTemplatePlaceholders []string
	var serverTemplateDefaults map[string]string
	if server := preferredTopLevelServer(doc.Servers); server != nil {
		baseURL, basePath, serverTemplatePlaceholders, serverTemplateDefaults = resolveServerURLTemplate(server)
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
	// Relative-only servers URL (e.g. `servers: [{url: /api/v3}]`) yields an
	// empty BaseURL but a non-empty BasePath. If the spec was fetched from an
	// http(s) URL, derive the absolute origin from that source so the generated
	// client doesn't construct scheme-less request URLs. Uses the spec's source
	// URL, not the ref-resolution location (which is file:// for local specs).
	if baseURL == "" && basePath != "" && sourceURL != nil &&
		(sourceURL.Scheme == "http" || sourceURL.Scheme == "https") && sourceURL.Host != "" {
		baseURL = sourceURL.Scheme + "://" + sourceURL.Host
	}
	baseURLIsPlaceholder := false
	if baseURL == "" && basePath == "" {
		warnf("no servers defined in spec; generated CLI will require base_url in config")
		baseURL = spec.PlaceholderBaseURL
		baseURLIsPlaceholder = true
	}

	injectGoogleDiscoveryAPIKeyAuth(doc, name)
	auth := mapAuthWithDescriptionInference(doc, name, !metadata.explicitEmptySecuritySchemes, authPreference)
	if auth.Type != "none" && allOperationsAllowAnonymous(doc) {
		auth = spec.AuthConfig{Type: "none"}
	}
	if auth.Type != "none" && auth.KeyURL == "" {
		auth.KeyURL = inferAuthKeyURL(doc, auth.Scheme)
	}

	tierRouting, err := parseTypedExtension[spec.TierRoutingConfig](doc, extensionTierRouting)
	if err != nil {
		return nil, err
	}
	rateClass, err := parseStringOpenAPIExtension(doc, extensionRateClass)
	if err != nil {
		return nil, err
	}
	defaultRateLimit, err := parseDefaultRateLimitOpenAPIExtension(doc, extensionDefaultRateLimit)
	if err != nil {
		return nil, err
	}
	roles, err := parseStringListOpenAPIExtension(doc, extensionRoles)
	if err != nil {
		return nil, err
	}

	mcpConfig, err := parseMCPExtension(doc)
	if err != nil {
		return nil, err
	}
	cacheConfig, err := parseTypedExtension[spec.CacheConfig](doc, extensionCache)
	if err != nil {
		return nil, err
	}
	learnConfig, err := parseTypedExtension[spec.LearnConfig](doc, extensionLearn)
	if err != nil {
		return nil, err
	}
	streamingConfig, err := parseTypedExtension[spec.StreamingConfig](doc, extensionStreaming)
	if err != nil {
		return nil, err
	}
	querySyncConfig, err := parseQuerySyncExtension(doc)
	if err != nil {
		return nil, err
	}

	templateVars, templateEnvOverrides, pathParamDefaults := parseEndpointTemplateExtensions(doc)
	// Merge server-URL template placeholders into the endpoint-template-var
	// bucket. The downstream config.Load template walks EndpointTemplateVars
	// to emit per-variable env-var lookups; without this merge a spec like
	// `https://{domain}/api/v2` would lose `{domain}` between the parser and
	// the generator and the CLI would DNS-fail on every call.
	templateVars, templateDefaults := mergeServerTemplatePlaceholders(templateVars, serverTemplatePlaceholders, serverTemplateDefaults)

	result := &spec.APISpec{
		Name:                         name,
		DisplayName:                  displayName,
		DisplayNameDerivedFromTitle:  displayNameDerivedFromTitle,
		Description:                  description,
		Version:                      version,
		BaseURL:                      baseURL,
		BaseURLIsPlaceholder:         baseURLIsPlaceholder,
		BasePath:                     basePath,
		WebsiteURL:                   websiteURL,
		ProxyRoutes:                  proxyRoutes,
		RateClass:                    rateClass,
		DefaultRateLimit:             defaultRateLimit,
		Auth:                         auth,
		Roles:                        roles,
		TierRouting:                  tierRouting,
		MCP:                          mcpConfig,
		Cache:                        cacheConfig,
		Learn:                        learnConfig,
		Streaming:                    streamingConfig,
		QuerySync:                    querySyncConfig,
		EndpointTemplateVars:         templateVars,
		EndpointTemplateEnvOverrides: templateEnvOverrides,
		EndpointPathParamDefaults:    pathParamDefaults,
		EndpointTemplateVarDefaults:  templateDefaults,
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
	// mapTypes runs before mapResources so Components.Schemas types are
	// registered first. registerInlineSchemaType (called from mapResponse)
	// has an exists-check, so Components-defined types win on name
	// collisions; inline schemas only fill genuine gaps.
	mapTypes(doc, result)
	if err := mapResources(doc, result, resourceBasePath); err != nil {
		return nil, err
	}

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

	applyPathParamDefaults(result)

	// Synthesize Params entries for {placeholder} tokens in the path template
	// that the operation never declared in `parameters` (or via a path-item /
	// $ref shared parameter). Real-world specs frequently omit these — the
	// path template is the source of truth, but without a matching Param
	// entry the generator emits a URL with literal `{name}` segments and the
	// request returns 404. Same pass the YAML loader uses, applied uniformly
	// here so OpenAPI-parsed specs get the same guarantee.
	result.EnrichPathParams()
	result.PromoteGlobalPathTemplateVars()

	if err := result.Validate(); err != nil {
		return nil, fmt.Errorf("validating parsed spec: %w", err)
	}

	return result, nil
}

// parseEndpointTemplateExtensions collects spec-declared endpoint template
// placeholders, env-var name overrides, and build-time path-parameter
// defaults. Returns nil/nil/nil for specs that declare none so existing
// generated outputs are byte-identical.
//
// Two extensions feed this function: x-tenant-env-var (legacy scalar that
// binds {tenant} to a single env-var override) and x-path-template-env-vars
// (generic map of placeholder -> {env, default}). For each entry in the
// generic extension, an `env` field registers the placeholder for runtime
// BaseURL substitution (same bucket as x-tenant-env-var), and a `default`
// field bakes a literal into operation paths at generation time. When both
// are set on the same entry, default wins and env is ignored — the
// placeholder is fully resolved before runtime substitution sees it.
func parseEndpointTemplateExtensions(doc *openapi3.T) ([]string, map[string]string, map[string]string) {
	var vars []string
	envOverrides := map[string]string{}
	defaults := map[string]string{}

	if raw, ok := lookupOpenAPIInfoExtension(doc, extensionTenantEnvVar); ok {
		if envName, ok := raw.(string); ok {
			if envName = strings.TrimSpace(envName); envName != "" {
				vars = append(vars, tenantPlaceholderName)
				envOverrides[tenantPlaceholderName] = envName
			}
		}
	}

	if raw, ok := lookupOpenAPIInfoExtension(doc, extensionPathTemplateEnvVars); ok {
		if entries, ok := raw.(map[string]any); ok {
			keys := make([]string, 0, len(entries))
			for k := range entries {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, name := range keys {
				obj, ok := entries[name].(map[string]any)
				if !ok {
					continue
				}
				var envName, defaultValue string
				if v, ok := obj["env"].(string); ok {
					envName = strings.TrimSpace(v)
				}
				if v, ok := obj["default"].(string); ok {
					defaultValue = strings.TrimSpace(v)
				}
				if defaultValue != "" {
					defaults[name] = defaultValue
					continue
				}
				if envName != "" {
					if !slices.Contains(vars, name) {
						vars = append(vars, name)
					}
					envOverrides[name] = envName
				}
			}
		}
	}

	// default wins across extensions: if x-tenant-env-var registered a key
	// that x-path-template-env-vars then declared a default for, drop the
	// stale runtime entry so the placeholder is fully baked and downstream
	// generators do not emit dead config / URL-substitution code for it.
	if len(defaults) > 0 {
		filtered := vars[:0]
		for _, v := range vars {
			if _, baked := defaults[v]; baked {
				delete(envOverrides, v)
				continue
			}
			filtered = append(filtered, v)
		}
		vars = filtered
	}

	if len(vars) == 0 {
		vars = nil
	}
	if len(envOverrides) == 0 {
		envOverrides = nil
	}
	if len(defaults) == 0 {
		defaults = nil
	}
	return vars, envOverrides, defaults
}

// parseTypedExtension bridges kin-openapi's untyped any-tree to a typed
// config struct via a JSON marshal/unmarshal roundtrip; callers rely on
// T's json tags for field mapping. Reach for it when the extension's
// value is a YAML/JSON object that maps to a Go struct (x-tier-routing,
// x-mcp). Use direct lookupOpenAPIExtension + type assertion for scalar
// extensions (string, bool, []string) where the roundtrip would be waste.
func parseTypedExtension[T any](doc *openapi3.T, key string) (T, error) {
	var zero T
	raw, ok := lookupOpenAPIExtension(doc, key)
	if !ok {
		return zero, nil
	}
	return parseTypedExtensionRaw[T](key, raw)
}

// parseQuerySyncExtension decodes the root-level x-pp-query extension into a
// *spec.QuerySyncConfig, returning nil (not a zero struct) when absent so the
// sync generator's presence gate stays off for every non-query API.
func parseQuerySyncExtension(doc *openapi3.T) (*spec.QuerySyncConfig, error) {
	if _, ok := lookupOpenAPIExtension(doc, extensionPPQuery); !ok {
		return nil, nil
	}
	cfg, err := parseTypedExtension[spec.QuerySyncConfig](doc, extensionPPQuery)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func parseMCPExtension(doc *openapi3.T) (spec.MCPConfig, error) {
	if raw, ok := lookupOpenAPIExtension(doc, extensionMCP); ok {
		return parseTypedExtensionRaw[spec.MCPConfig](extensionMCP, raw)
	}
	var zero spec.MCPConfig
	if doc == nil || doc.Extensions == nil {
		return zero, nil
	}
	raw, ok := doc.Extensions[extensionLegacyMCP]
	if !ok {
		return zero, nil
	}
	warnf("accepted root-level 'mcp:' for backwards compatibility; rename to 'x-mcp:' per OpenAPI extension convention")
	return parseTypedExtensionRaw[spec.MCPConfig](extensionLegacyMCP, raw)
}

func parseTypedExtensionRaw[T any](key string, raw any) (T, error) {
	var zero T
	data, err := json.Marshal(raw)
	if err != nil {
		return zero, fmt.Errorf("marshaling %s: %w", key, err)
	}
	var cfg T
	if err := json.Unmarshal(data, &cfg); err != nil {
		return zero, fmt.Errorf("parsing %s: %w", key, err)
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

func parseStringOpenAPIExtension(doc *openapi3.T, key string) (string, error) {
	if doc != nil && doc.Extensions != nil {
		if _, ok := doc.Extensions[key]; ok {
			return parseStringExtensionValue(doc.Extensions, key)
		}
	}
	if doc != nil && doc.Info != nil && doc.Info.Extensions != nil {
		if _, ok := doc.Info.Extensions[key]; ok {
			return parseStringExtensionValue(doc.Info.Extensions, key)
		}
	}
	return "", nil
}

// parseDefaultRateLimitOpenAPIExtension reads x-pp-default-rate-limit from the
// root or info object. The value may be the string "auto" (case-insensitive) or
// a non-negative number (JSON number or numeric string); it returns the
// canonical string form ("auto" or e.g. "2"). Absent → "".
func parseDefaultRateLimitOpenAPIExtension(doc *openapi3.T, key string) (string, error) {
	var raw any
	var ok bool
	if doc != nil && doc.Extensions != nil {
		raw, ok = doc.Extensions[key]
	}
	if !ok {
		if doc != nil && doc.Info != nil && doc.Info.Extensions != nil {
			raw, ok = doc.Info.Extensions[key]
		}
	}
	if !ok {
		return "", nil
	}
	invalid := fmt.Errorf("%s must be \"auto\" or a non-negative number", key)
	switch v := raw.(type) {
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return "", nil
		}
		if strings.EqualFold(s, "auto") {
			return "auto", nil
		}
		if f, err := parseNonNegativeFloat(s); err == nil {
			return f, nil
		}
		return "", invalid
	case json.Number:
		if f, err := parseNonNegativeFloat(v.String()); err == nil {
			return f, nil
		}
		return "", invalid
	case float64:
		if v < 0 {
			return "", invalid
		}
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	default:
		return "", invalid
	}
}

// parseNonNegativeFloat validates a numeric string is a non-negative number and
// returns it in canonical (no trailing-zero) form.
func parseNonNegativeFloat(s string) (string, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return "", err
	}
	if f < 0 {
		return "", fmt.Errorf("negative")
	}
	return strconv.FormatFloat(f, 'g', -1, 64), nil
}

func parseStringListOpenAPIExtension(doc *openapi3.T, key string) ([]string, error) {
	if doc != nil && doc.Extensions != nil {
		if _, ok := doc.Extensions[key]; ok {
			return parseStringListExtensionValue(doc.Extensions, key)
		}
	}
	if doc != nil && doc.Info != nil && doc.Info.Extensions != nil {
		if _, ok := doc.Info.Extensions[key]; ok {
			return parseStringListExtensionValue(doc.Info.Extensions, key)
		}
	}
	return nil, nil
}

func parseStringExtensionValue(extensions map[string]any, key string) (string, error) {
	if _, ok := extensions[key].(string); !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return stringExtension(extensions, key), nil
}

func parseStringListExtensionValue(extensions map[string]any, key string) ([]string, error) {
	values, ok := strictStringListValue(extensions[key])
	if !ok {
		return nil, fmt.Errorf("%s must be a string array", key)
	}
	return values, nil
}

func mapAuth(doc *openapi3.T, name string) spec.AuthConfig {
	return mapAuthWithDescriptionInference(doc, name, true, "")
}

func injectGoogleDiscoveryAPIKeyAuth(doc *openapi3.T, name string) {
	if doc == nil || !shouldInjectGoogleAPIKeyAuth(doc) {
		return
	}
	if doc.Components == nil {
		doc.Components = &openapi3.Components{}
	}
	if doc.Components.SecuritySchemes == nil {
		doc.Components.SecuritySchemes = openapi3.SecuritySchemes{}
	}

	schemeName := googleAPIKeySecuritySchemeName(doc)
	if schemeName == "" {
		schemeName = uniqueSecuritySchemeName(doc.Components.SecuritySchemes, "ApiKey")
		doc.Components.SecuritySchemes[schemeName] = &openapi3.SecuritySchemeRef{
			Value: &openapi3.SecurityScheme{
				Type: "apiKey",
				In:   "query",
				Name: "key",
				Extensions: map[string]any{
					extensionAuthVars: []any{
						map[string]any{
							"name":      naming.EnvPrefix(name) + "_API_KEY",
							"kind":      string(spec.AuthEnvVarKindPerCall),
							"required":  true,
							"sensitive": true,
						},
					},
				},
			},
		}
	}

	requirement := openapi3.SecurityRequirement{schemeName: []string{}}
	if doc.Security == nil {
		if !hasNonAPIKeySecurityScheme(doc) {
			doc.Security = openapi3.SecurityRequirements{requirement}
		}
	} else if !securityRequirementsReferenceScheme(doc.Security, schemeName) && !securityRequirementsAllowAnonymous(doc.Security) {
		doc.Security = append(doc.Security, requirement)
	}

	if doc.Paths == nil {
		return
	}
	for _, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		for _, op := range pathItem.Operations() {
			if op == nil || op.Security == nil {
				continue
			}
			if securityRequirementsAllowAnonymous(*op.Security) || securityRequirementsReferenceScheme(*op.Security, schemeName) {
				continue
			}
			*op.Security = append(*op.Security, requirement)
		}
	}
}

func shouldInjectGoogleAPIKeyAuth(doc *openapi3.T) bool {
	if !hasNonAnonymousSecurityRequirements(doc) {
		return false
	}
	return isGoogleDiscoverySpec(doc) || hasGoogleAPIsServer(doc)
}

func googleAPIKeySecuritySchemeName(doc *openapi3.T) string {
	if doc == nil || doc.Components == nil {
		return ""
	}
	for _, name := range allSecuritySchemeNames(doc) {
		scheme := securitySchemeValue(doc.Components.SecuritySchemes[name])
		if scheme != nil &&
			strings.EqualFold(scheme.Type, "apiKey") &&
			strings.EqualFold(strings.TrimSpace(scheme.In), "query") &&
			strings.EqualFold(strings.TrimSpace(scheme.Name), "key") {
			return name
		}
	}
	return ""
}

func uniqueSecuritySchemeName(schemes openapi3.SecuritySchemes, base string) string {
	if _, ok := schemes[base]; !ok {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s%d", base, i)
		if _, ok := schemes[candidate]; !ok {
			return candidate
		}
	}
}

func securityRequirementsReferenceScheme(requirements openapi3.SecurityRequirements, schemeName string) bool {
	for _, requirement := range requirements {
		if _, ok := requirement[schemeName]; ok {
			return true
		}
	}
	return false
}

func hasNonAPIKeySecurityScheme(doc *openapi3.T) bool {
	if doc == nil || doc.Components == nil {
		return false
	}
	for _, name := range allSecuritySchemeNames(doc) {
		scheme := securitySchemeValue(doc.Components.SecuritySchemes[name])
		if scheme != nil && !strings.EqualFold(scheme.Type, "apiKey") {
			return true
		}
	}
	return false
}

func hasNonAnonymousSecurityRequirements(doc *openapi3.T) bool {
	if doc == nil {
		return false
	}
	if nonAnonymousSecurityRequirements(doc.Security) {
		return true
	}
	if doc.Paths == nil {
		return false
	}
	for _, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		for _, op := range pathItem.Operations() {
			if op != nil && op.Security != nil && nonAnonymousSecurityRequirements(*op.Security) {
				return true
			}
		}
	}
	return false
}

func nonAnonymousSecurityRequirements(requirements openapi3.SecurityRequirements) bool {
	if len(requirements) == 0 {
		return false
	}
	for _, requirement := range requirements {
		if len(requirement) > 0 {
			return true
		}
	}
	return false
}

func mapAuthWithDescriptionInference(doc *openapi3.T, name string, allowDescriptionInference bool, authPreference string) spec.AuthConfig {
	auth := spec.AuthConfig{Type: "none"}
	schemeName, scheme := selectSecurityScheme(doc, authPreference)
	if scheme == nil {
		result := inferQueryParamAuth(doc, name, auth)
		if result.Type == "none" {
			result = inferHeaderParamAPIKeyAuth(doc, name, result)
		}
		if result.Type == "none" && allowDescriptionInference {
			result = inferDescriptionAuth(doc, name, result)
		}
		if result.Type == "none" {
			result = inferOperationLevelBearer(doc, name, result)
		}
		applyAuthCompanionFromInfo(&result, doc)
		return result
	}

	auth.Scheme = schemeName

	switch strings.ToLower(scheme.Type) {
	case "http":
		switch strings.ToLower(scheme.Scheme) {
		case "bearer":
			auth.Type = "bearer_token"
			auth.Header = "Authorization"
			if xFmt := stringExtension(scheme.Extensions, "x-auth-format"); xFmt != "" {
				auth.Format = xFmt
			}
		case "basic":
			auth.Type = "api_key"
			auth.Header = "Authorization"
			auth.Format = "Basic {username}:{password}"
			if xFmt := stringExtension(scheme.Extensions, "x-auth-format"); xFmt != "" {
				auth.Format = xFmt
			}
			applyBasicAuthConstantHints(&auth, scheme.Extensions, scheme.Description)
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
			if xFmt := stringExtension(scheme.Extensions, "x-auth-format"); xFmt != "" {
				auth.Format = xFmt
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
		auth.EnvVars = defaultAuthEnvVars(auth.Type, auth.Format, schemeName, envPrefix, auth.In)
	case "bearer_token":
		auth.EnvVars = defaultAuthEnvVars(auth.Type, auth.Format, schemeName, envPrefix, "")
	}
	applyAuthOverrideExtensions(&auth, scheme.Extensions)
	applyAuthEnvVarDefaults(&auth, envPrefix)
	applyAuthVarsRichOverride(&auth, scheme.Extensions, fmt.Sprintf("components.securitySchemes.%s.%s", schemeName, extensionAuthVars))
	applyAuthCompanionFromInfo(&auth, doc)
	auth.AdditionalHeaders = collectAdditionalAuthHeaders(doc, schemeName, envPrefix)
	return auth
}

// collectAdditionalAuthHeaders scans AND-group siblings of the winning
// security scheme for per-call credential entries. Composed auth shapes
// (apiKey + OAuth bearer, Stripe-Signature + bearer, ST-App-Key + bearer)
// declare the apiKey scheme in the same security requirement object as the
// bearer; selectSecurityScheme picks the bearer half, and without this sweep
// the apiKey half is silently dropped.
//
// AND vs OR matters: OpenAPI security is a list of requirement objects, where
// each object is an AND-group (all schemes named must be satisfied) and the
// list itself is OR (any one object suffices). A spec offering BearerAuth and
// ApiKeyAuth as alternatives (each in its own requirement object) must NOT
// promote the unused alternative as a sibling — that would emit a spurious
// header on every Bearer-authenticated request. Only schemes co-located with
// the winner in a requirement object are promoted.
//
// Only apiKey-typed siblings with `in: header` or `in: query` are considered.
// Rich x-auth-vars per_call declarations win; legacy x-auth-env-vars supplies
// the same credential name for specs that do not use the rich shape. Missing
// extension metadata falls back to a deterministic header-derived env var for
// header siblings, and to a scheme-derived env var for all-apiKey AND groups,
// so every required supported credential reaches generated config and client
// code.
func collectAdditionalAuthHeaders(doc *openapi3.T, winner, envPrefix string) []spec.AdditionalAuthHeader {
	if doc == nil || doc.Components == nil || len(doc.Components.SecuritySchemes) <= 1 {
		return nil
	}
	requirements := additionalHeaderSecurityRequirements(doc)
	if winner == "" || len(requirements) == 0 {
		return nil
	}
	requirements = additionalHeaderRequirementsForWinner(requirements, winner)
	if len(requirements) == 0 || !additionalHeaderRequirementsShareSiblings(requirements, winner) {
		return nil
	}

	siblingSet := map[string]struct{}{}
	fallbackEligible := map[string]bool{}
	var siblings []string
	for _, req := range requirements {
		if _, ok := req[winner]; !ok {
			continue
		}
		allSupportedAPIKeys := requirementAllSupportedAPIKeys(doc, req)
		for name := range req {
			if name == winner {
				continue
			}
			if _, dup := siblingSet[name]; dup {
				fallbackEligible[name] = fallbackEligible[name] || allSupportedAPIKeys
				continue
			}
			siblingSet[name] = struct{}{}
			fallbackEligible[name] = allSupportedAPIKeys
			siblings = append(siblings, name)
		}
	}
	sort.Strings(siblings)

	var headers []spec.AdditionalAuthHeader
	for _, name := range siblings {
		scheme := securitySchemeValue(doc.Components.SecuritySchemes[name])
		if scheme == nil {
			continue
		}
		if !strings.EqualFold(scheme.Type, "apiKey") {
			continue
		}
		header := strings.TrimSpace(scheme.Name)
		if header == "" {
			continue
		}
		// apiKey schemes must declare `in` per OpenAPI 3.x; an empty value is a
		// spec authoring mistake and would otherwise silently emit a credential.
		placement := strings.ToLower(strings.TrimSpace(scheme.In))
		if placement != "header" && placement != "query" {
			continue
		}
		fallbackMode := additionalHeaderFallbackNone
		if fallbackEligible[name] {
			fallbackMode = additionalHeaderFallbackScheme
		} else if placement == "header" {
			fallbackMode = additionalHeaderFallbackHeader
		}
		envVars := additionalHeaderEnvVars(scheme, name, header, envPrefix, fallbackMode)
		for _, ev := range envVars {
			if ev.EffectiveKind() != spec.AuthEnvVarKindPerCall {
				continue
			}
			if strings.TrimSpace(ev.Name) == "" {
				continue
			}
			headers = append(headers, spec.AdditionalAuthHeader{
				Header: header,
				In:     placement,
				Scheme: name,
				EnvVar: ev,
			})
		}
	}
	return headers
}

func additionalHeaderSecurityRequirements(doc *openapi3.T) openapi3.SecurityRequirements {
	if doc == nil {
		return nil
	}
	var requirements openapi3.SecurityRequirements
	seen := map[string]struct{}{}
	visitedOperation := false
	if doc.Paths == nil {
		appendUniqueSecurityRequirements(&requirements, seen, doc.Security)
		return requirements
	}
	for _, pathKey := range doc.Paths.InMatchingOrder() {
		pathItem := doc.Paths.Value(pathKey)
		if pathItem == nil {
			continue
		}
		for _, op := range pathItem.Operations() {
			if op == nil {
				continue
			}
			visitedOperation = true
			appendUniqueSecurityRequirements(&requirements, seen, effectiveSecurityRequirements(op, doc))
		}
	}
	if visitedOperation {
		return requirements
	}
	appendUniqueSecurityRequirements(&requirements, seen, doc.Security)
	return requirements
}

func appendUniqueSecurityRequirements(requirements *openapi3.SecurityRequirements, seen map[string]struct{}, candidates openapi3.SecurityRequirements) {
	for _, req := range candidates {
		key := securityRequirementKey(req)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		*requirements = append(*requirements, req)
	}
}

func securityRequirementKey(req openapi3.SecurityRequirement) string {
	names := make([]string, 0, len(req))
	for name := range req {
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		scopes := slices.Clone(req[name])
		sort.Strings(scopes)
		parts = append(parts, name+":"+strings.Join(scopes, ","))
	}
	return strings.Join(parts, "|")
}

func additionalHeaderRequirementsForWinner(requirements openapi3.SecurityRequirements, winner string) openapi3.SecurityRequirements {
	matching := make(openapi3.SecurityRequirements, 0, len(requirements))
	for _, req := range requirements {
		if _, ok := req[winner]; ok {
			matching = append(matching, req)
		}
	}
	return matching
}

func additionalHeaderRequirementsShareSiblings(requirements openapi3.SecurityRequirements, winner string) bool {
	var signature string
	for i, req := range requirements {
		current := additionalHeaderSiblingSignature(req, winner)
		if i == 0 {
			signature = current
			continue
		}
		if current != signature {
			return false
		}
	}
	return true
}

func additionalHeaderSiblingSignature(req openapi3.SecurityRequirement, winner string) string {
	siblings := make([]string, 0, len(req))
	for name := range req {
		if name != winner {
			siblings = append(siblings, name)
		}
	}
	sort.Strings(siblings)
	return strings.Join(siblings, "|")
}

func additionalHeaderEnvVars(scheme *openapi3.SecurityScheme, schemeName, headerName, envPrefix string, fallbackMode additionalHeaderFallbackMode) []spec.AuthEnvVar {
	if scheme == nil {
		return nil
	}
	if raw, ok := scheme.Extensions[extensionAuthVars]; ok && raw != nil {
		envVars, err := authVarsExtension(raw)
		if err == nil && len(envVars) > 0 {
			return envVars
		}
	}
	if envVars := stringListExtension(scheme.Extensions, extensionAuthEnvVars); len(envVars) > 0 {
		if len(envVars) > 1 {
			warnf("components.securitySchemes.%s.%s declares multiple names for one sibling header; using %s", schemeName, extensionAuthEnvVars, envVars[0])
		}
		name := strings.TrimSpace(envVars[0])
		if name == "" {
			return nil
		}
		return []spec.AuthEnvVar{{
			Name:      name,
			Kind:      spec.AuthEnvVarKindPerCall,
			Required:  true,
			Sensitive: true,
			Inferred:  true,
		}}
	}
	if fallbackMode == additionalHeaderFallbackNone {
		return nil
	}
	name := derivedAdditionalHeaderEnvVar(schemeName, headerName, envPrefix, fallbackMode)
	if name == "" {
		return nil
	}
	return []spec.AuthEnvVar{{
		Name:      name,
		Kind:      spec.AuthEnvVarKindPerCall,
		Required:  true,
		Sensitive: true,
		Inferred:  true,
	}}
}

func derivedAdditionalHeaderEnvVar(schemeName, headerName, envPrefix string, fallbackMode additionalHeaderFallbackMode) string {
	if fallbackMode == additionalHeaderFallbackHeader {
		headerSuffix := toSnakeCase(headerName)
		if headerSuffix == "" {
			return ""
		}
		return envPrefix + "_" + strings.ToUpper(headerSuffix)
	}
	envVars := defaultAuthEnvVars("api_key", "", schemeName, envPrefix, "")
	if len(envVars) == 0 {
		return ""
	}
	return envVars[0]
}

func defaultAuthEnvVars(authType, format, schemeName, envPrefix, placement string) []string {
	switch authType {
	case "api_key":
		if authFormatIsBasic(format) {
			if envVars := basicAuthEnvVarsFromFormat(format, envPrefix); len(envVars) > 0 {
				return envVars
			}
			return []string{envPrefix + "_USERNAME", envPrefix + "_PASSWORD"}
		}
		// Use scheme name for more specific env var (e.g. BotToken -> DISCORD_BOT_TOKEN).
		schemeEnvSuffix := toSnakeCase(schemeName)
		if !strings.EqualFold(strings.TrimSpace(placement), "cookie") {
			schemeEnvSuffix = stripLeadingEnvPrefix(schemeEnvSuffix, envPrefix)
		}
		if schemeEnvSuffix != "" && !isGenericAPIKeySchemeSuffix(schemeEnvSuffix) {
			return []string{envPrefix + "_" + strings.ToUpper(schemeEnvSuffix)}
		}
		return []string{envPrefix + "_API_KEY"}
	case "bearer_token":
		schemeEnvSuffix := stripLeadingEnvPrefix(toSnakeCase(schemeName), envPrefix)
		switch schemeEnvSuffix {
		case "", "bearer", "bearer_token", "token":
			return []string{envPrefix + "_TOKEN"}
		default:
			return []string{envPrefix + "_" + strings.ToUpper(schemeEnvSuffix)}
		}
	default:
		return nil
	}
}

func stripLeadingEnvPrefix(schemeEnvSuffix, envPrefix string) string {
	prefix := strings.ToLower(strings.TrimSpace(envPrefix))
	if prefix == "" || prefix == "api" {
		return schemeEnvSuffix
	}
	prefixes := []string{prefix}
	if numericPrefix, ok := strings.CutPrefix(prefix, "api_"); ok && numericPrefix != "" && numericPrefix[0] >= '0' && numericPrefix[0] <= '9' {
		prefixes = append(prefixes, numericPrefix)
	}
	for _, candidate := range prefixes {
		if stripped, ok := stripNormalizedEnvPrefix(schemeEnvSuffix, candidate); ok {
			return stripped
		}
	}
	return schemeEnvSuffix
}

func stripNormalizedEnvPrefix(schemeEnvSuffix, prefix string) (string, bool) {
	normalizedPrefix := strings.ReplaceAll(strings.ToLower(prefix), "_", "")
	if normalizedPrefix == "" {
		return "", false
	}
	index := 0
	for prefixIndex := 0; prefixIndex < len(normalizedPrefix); prefixIndex++ {
		for index < len(schemeEnvSuffix) && schemeEnvSuffix[index] == '_' {
			index++
		}
		if index == len(schemeEnvSuffix) || strings.ToLower(schemeEnvSuffix[index:index+1]) != normalizedPrefix[prefixIndex:prefixIndex+1] {
			return "", false
		}
		index++
	}
	if index == len(schemeEnvSuffix) || schemeEnvSuffix[index] != '_' {
		return "", false
	}
	for index < len(schemeEnvSuffix) && schemeEnvSuffix[index] == '_' {
		index++
	}
	return schemeEnvSuffix[index:], true
}

func requirementAllSupportedAPIKeys(doc *openapi3.T, req openapi3.SecurityRequirement) bool {
	if doc == nil || doc.Components == nil || len(req) == 0 {
		return false
	}
	for name := range req {
		scheme := securitySchemeValue(doc.Components.SecuritySchemes[name])
		if scheme == nil {
			return false
		}
		placement := strings.ToLower(strings.TrimSpace(scheme.In))
		if !strings.EqualFold(scheme.Type, "apiKey") || (placement != "header" && placement != "query") {
			return false
		}
	}
	return true
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

// inferAuthKeyURL returns a best-effort HTTPS URL pointing the user at where
// they can obtain a credential when x-auth-key-url is not set. Precedence:
//  1. URL embedded in the selected security scheme's description
//  2. URL embedded in info.description, but only when the surrounding text
//     mentions auth/credential cues (so we don't pick a URL describing an
//     unrelated feature)
//
// Returns "" when no plausible URL is found. The printed CLI surfaces the
// result as "Get a key at: <URL>", so a wrong URL here is worse than no URL.
// We deliberately do NOT fall back to externalDocs.url or info.contact.url —
// those almost always point at the API's docs landing page or the company
// homepage, neither of which is where users actually create a token. When this
// returns "", the printed CLI falls back to a separate "See API docs: <URL>"
// line driven by WebsiteURL, which is honest framing for those URLs.
func inferAuthKeyURL(doc *openapi3.T, schemeName string) string {
	if doc == nil {
		return ""
	}
	if schemeName != "" && doc.Components != nil {
		if ref, ok := doc.Components.SecuritySchemes[schemeName]; ok {
			if scheme := securitySchemeValue(ref); scheme != nil {
				if u := authKeyURLFromText(scheme.Description); u != "" {
					return u
				}
			}
		}
	}
	if doc.Info != nil {
		if u := authRelatedURLFromText(doc.Info.Description); u != "" {
			return u
		}
	}
	return ""
}

var (
	httpsURLPattern = regexp.MustCompile(`https://[^\s)>\]"',]+`)

	genericAuthReferenceHosts = []string{
		"developer.mozilla.org",
		"datatracker.ietf.org",
		"rfc-editor.org",
		"tools.ietf.org",
		"httpwg.org",
		"en.wikipedia.org",
	}
)

// Prefer clear credential-management surfaces because generated auth hints
// label this URL as where users get a key; generic references would misdirect.
func authKeyURLFromText(s string) string {
	if s == "" {
		return ""
	}
	var best string
	bestScore := -1
	for _, match := range httpsURLPattern.FindAllString(s, -1) {
		candidate := strings.TrimRight(match, ".,;:!?)")
		if candidate == "" || strings.ContainsAny(candidate, "<>{}[]") || isPlaceholderURL(candidate) || isGenericAuthReferenceURL(candidate) {
			continue
		}
		score := credentialSurfaceScore(candidate)
		if best == "" || score > bestScore {
			best = candidate
			bestScore = score
		}
	}
	return best
}

func isGenericAuthReferenceURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	for _, genericHost := range genericAuthReferenceHosts {
		if host == genericHost || strings.HasSuffix(host, "."+genericHost) {
			return true
		}
	}
	return false
}

func credentialSurfaceScore(rawURL string) int {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return 0
	}
	tokens := strings.FieldsFunc(strings.ToLower(parsed.Hostname()+parsed.EscapedPath()), func(r rune) bool {
		return r == '.' || r == '/'
	})
	hasSettings := false
	hasIntegrations := false
	for _, token := range tokens {
		switch token {
		case "console", "dashboard", "api-keys", "apikeys":
			return 2
		case "settings":
			hasSettings = true
		case "integrations":
			hasIntegrations = true
		}
	}
	if hasSettings && hasIntegrations {
		return 2
	}
	return 0
}

func isPlaceholderURL(u string) bool {
	normalized := strings.TrimRight(strings.ToLower(strings.TrimSpace(u)), "/")
	switch normalized {
	case "https://en.wikipedia.org/wiki/hateoas":
		return true
	default:
		return false
	}
}

func authRelatedURLFromText(s string) string {
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	cues := []string{
		"token", "api key", "api_key", "apikey",
		"credential", "register", "sign up", "signup",
		"create an app", "create an application", "personal access",
	}
	matched := false
	for _, c := range cues {
		if strings.Contains(lower, c) {
			matched = true
			break
		}
	}
	if !matched {
		return ""
	}
	return authKeyURLFromText(s)
}

func applyAuthOverrideExtensions(auth *spec.AuthConfig, extensions map[string]any) {
	if auth == nil || len(extensions) == 0 {
		return
	}
	applyOAuthDeviceFlowExtension(auth, extensions)
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
	if instructions := stringExtension(extensions, extensionAuthInstructions); instructions != "" {
		auth.Instructions = instructions
	}
	if title := stringExtension(extensions, extensionAuthTitle); title != "" {
		auth.Title = title
	}
	if description := stringExtension(extensions, extensionAuthDescription); description != "" {
		auth.Description = description
	}
	if subtype := stringExtension(extensions, extensionAuthSubtype); subtype != "" {
		// Only known subtype values are accepted. Unknown values would round-trip
		// silently and surface later as a confusing "subtype %q is not
		// recognized" validation error far from the spec source. Filtering here
		// keeps the error close to the typo, matching how unknown auth types are
		// handled elsewhere in this parser.
		switch subtype {
		case spec.AuthSubtypeAuth0SPAInMemory:
			auth.Subtype = subtype
		}
	}
	if mech := stringExtension(extensions, extensionOAuthRefreshTokenMech); mech != "" {
		auth.RefreshTokenMechanism = mech
	}
	applyAuthCompanionExtension(auth, extensions)
}

// applyOAuthDeviceFlowExtension reads x-oauth-device-flow from an OAuth2
// security scheme. OpenAPI 3.0 has no native device-code flow field, so the
// extension carries the device authorization endpoint plus token metadata.
func applyOAuthDeviceFlowExtension(auth *spec.AuthConfig, extensions map[string]any) {
	if auth == nil || len(extensions) == 0 {
		return
	}
	raw, ok := extensions[extensionOAuthDeviceFlow]
	if !ok {
		return
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		warnf("%s is malformed: expected an object", extensionOAuthDeviceFlow)
		return
	}
	auth.Type = "bearer_token"
	auth.Header = "Authorization"
	auth.OAuth2Grant = spec.OAuth2GrantDeviceCode
	auth.EnvVars = nil
	auth.EnvVarSpecs = nil
	if v := stringObjectField(obj, "deviceAuthorizationUrl", "device_authorization_url"); v != "" {
		auth.DeviceAuthorizationURL = v
	}
	if v := stringObjectField(obj, "tokenUrl", "token_url"); v != "" {
		auth.TokenURL = v
	}
	if v := stringObjectField(obj, "defaultClientId", "default_client_id"); v != "" {
		auth.DefaultClientID = v
	}
	if scopes := stringListObjectField(obj, "scopes"); len(scopes) > 0 {
		auth.Scopes = scopes
		sort.Strings(auth.Scopes)
	}
}

func stringObjectField(obj map[string]any, names ...string) string {
	for _, name := range names {
		raw, ok := obj[name]
		if !ok {
			continue
		}
		s, ok := raw.(string)
		if !ok {
			continue
		}
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

func stringListObjectField(obj map[string]any, names ...string) []string {
	for _, name := range names {
		raw, ok := obj[name]
		if !ok {
			continue
		}
		if values := stringListValue(raw); len(values) > 0 {
			return values
		}
	}
	return nil
}

// applyAuthCompanionExtension reads x-auth-companion from a map of OpenAPI
// extensions and copies its fields into auth. The companion object carries
// hints that let the generated CLI's `auth login --chrome --auto-login` hand
// off to `press-auth login` non-interactively. All fields are optional; an
// individual missing field leaves the corresponding AuthConfig field
// untouched.
func applyAuthCompanionExtension(auth *spec.AuthConfig, extensions map[string]any) {
	if auth == nil || len(extensions) == 0 {
		return
	}
	raw, ok := extensions[extensionAuthCompanion]
	if !ok {
		return
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		warnf("%s is malformed: expected an object", extensionAuthCompanion)
		return
	}
	if v, ok := obj["login_url"].(string); ok && strings.TrimSpace(v) != "" {
		auth.LoginURL = strings.TrimSpace(v)
	}
	if v, ok := obj["login_complete_selector"].(string); ok && strings.TrimSpace(v) != "" {
		auth.LoginCompleteSelector = strings.TrimSpace(v)
	}
	if v, ok := obj["jwt_carrier_cookie"].(string); ok && strings.TrimSpace(v) != "" {
		auth.JWTCarrierCookie = strings.TrimSpace(v)
	}
}

// applyAuthCompanionFromInfo reads x-auth-companion at the info-level (or
// document root) as a fallback for specs that don't declare a single named
// security scheme. Scheme-level hints win because the per-scheme placement
// mirrors x-auth-vars / x-auth-instructions, so applyAuthCompanionFromInfo
// is only used to fill fields that scheme-level didn't set.
func applyAuthCompanionFromInfo(auth *spec.AuthConfig, doc *openapi3.T) {
	if auth == nil || doc == nil {
		return
	}
	raw, ok := lookupOpenAPIExtension(doc, extensionAuthCompanion)
	if !ok {
		return
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		warnf("info.%s is malformed: expected an object", extensionAuthCompanion)
		return
	}
	if auth.LoginURL == "" {
		if v, ok := obj["login_url"].(string); ok && strings.TrimSpace(v) != "" {
			auth.LoginURL = strings.TrimSpace(v)
		}
	}
	if auth.LoginCompleteSelector == "" {
		if v, ok := obj["login_complete_selector"].(string); ok && strings.TrimSpace(v) != "" {
			auth.LoginCompleteSelector = strings.TrimSpace(v)
		}
	}
	if auth.JWTCarrierCookie == "" {
		if v, ok := obj["jwt_carrier_cookie"].(string); ok && strings.TrimSpace(v) != "" {
			auth.JWTCarrierCookie = strings.TrimSpace(v)
		}
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
	if auth.OAuth2Grant == spec.OAuth2GrantDeviceCode {
		envVarName := envPrefix + "_CLIENT_ID"
		if len(auth.EnvVars) > 0 && strings.TrimSpace(auth.EnvVars[0]) != "" {
			envVarName = strings.TrimSpace(auth.EnvVars[0])
		}
		auth.EnvVarSpecs = []spec.AuthEnvVar{
			{
				Name:      envVarName,
				Kind:      spec.AuthEnvVarKindAuthFlowInput,
				Required:  strings.TrimSpace(auth.DefaultClientID) == "",
				Sensitive: false,
				Inferred:  true,
			},
		}
		auth.EnvVars = []string{auth.EnvVarSpecs[0].Name}
		return
	}
	if len(auth.EnvVars) == 0 {
		return
	}
	auth.EnvVarSpecs = make([]spec.AuthEnvVar, 0, len(auth.EnvVars))
	for i, name := range auth.EnvVars {
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
		if authFormatIsBasic(auth.Format) && i == 0 && basicAuthFirstPlaceholderIsPublic(auth.Format) {
			envVar.Sensitive = false
		}
		auth.EnvVarSpecs = append(auth.EnvVarSpecs, envVar)
	}
}

func basicAuthFirstPlaceholderIsPublic(format string) bool {
	matches := authFormatPlaceholderRE.FindAllStringSubmatch(format, -1)
	if len(matches) == 0 || len(matches[0]) < 2 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(matches[0][1])) {
	case "username", "user":
		return true
	default:
		return false
	}
}

func basicAuthEnvVarsFromFormat(format, envPrefix string) []string {
	matches := authFormatPlaceholderRE.FindAllStringSubmatch(format, -1)
	if len(matches) == 0 || len(matches) > 2 {
		return nil
	}
	envVars := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		placeholder := strings.TrimSpace(match[1])
		if placeholder == "" {
			return nil
		}
		if _, ok := seen[placeholder]; ok {
			continue
		}
		seen[placeholder] = struct{}{}
		envVars = append(envVars, envPrefix+"_"+strings.ToUpper(toSnakeCase(placeholder)))
	}
	if len(envVars) == 0 || len(envVars) > 2 {
		return nil
	}
	return envVars
}

func authFormatIsBasic(format string) bool {
	return strings.Contains(strings.ToLower(format), "basic ")
}

func applyBasicAuthConstantHints(auth *spec.AuthConfig, extensions map[string]any, description string) {
	if auth == nil || !authFormatIsBasic(auth.Format) {
		return
	}
	username := stringExtension(extensions, extensionAuthBasicUsername)
	password := stringExtension(extensions, extensionAuthBasicPassword)
	if username == "" && password == "" {
		username = describedBasicCredentialConstant(description, "username")
		password = describedBasicCredentialConstant(description, "password")
	}
	switch {
	case username != "" && password == "":
		auth.Format = "Basic " + username + ":{token}"
	case password != "" && username == "":
		auth.Format = "Basic {token}:" + password
	}
}

func describedBasicCredentialConstant(description, field string) string {
	description = strings.TrimSpace(description)
	field = strings.ToLower(strings.TrimSpace(field))
	if description == "" || field == "" {
		return ""
	}
	pattern := regexp.MustCompile(`(?i)` + "`?" + regexp.QuoteMeta(field) + "`?" + `\s*(?:is|=|:)\s*([^,.;\n]+)`)
	match := pattern.FindStringSubmatch(description)
	if len(match) < 2 {
		return ""
	}
	value := strings.TrimSpace(match[1])
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "`") {
		if end := strings.Index(value[1:], "`"); end >= 0 {
			return strings.TrimSpace(value[1 : 1+end])
		}
	}
	if strings.HasPrefix(value, `"`) {
		if end := strings.Index(value[1:], `"`); end >= 0 {
			return strings.TrimSpace(value[1 : 1+end])
		}
	}
	if strings.HasPrefix(value, "'") {
		if end := strings.Index(value[1:], "'"); end >= 0 {
			return strings.TrimSpace(value[1 : 1+end])
		}
	}
	parts := strings.Fields(value)
	if len(parts) != 1 {
		return ""
	}
	candidate := strings.Trim(parts[0], "`'\"")
	if candidate == "" || !looksLikeBasicConstant(candidate) {
		return ""
	}
	return candidate
}

func looksLikeBasicConstant(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	switch lower {
	case "", "a", "an", "the", "your", "you", "username", "password", "key", "secret", "token",
		"always", "required", "provided", "required.", "optional", "empty", "blank":
		return false
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
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

func loadOpenAPIDoc(data []byte, lenient bool, location *url.URL) (*openapi3.T, error) {
	// Swagger 2.0 specs with circular $ref chains (Tripletex, NetSuite, etc.)
	// burn 15-30 minutes of CPU and OOM the process when fed straight to the
	// OpenAPI 3 loader. Detect them at the boundary and route through
	// openapi2conv.ToV3 so the existing OpenAPI 3 code path handles the
	// resolved spec. See issue #1241 and internal/openapi/swagger2.go.
	if isSwagger2SpecJSON(data) {
		return loadSwagger2AsOpenAPI3(data, lenient, location)
	}

	loader := newConfiguredOpenAPI3Loader(lenient, location)
	if location != nil {
		return loader.LoadFromDataWithPath(data, location)
	}
	return loader.LoadFromData(data)
}

// newConfiguredOpenAPI3Loader returns an openapi3.Loader with the project's
// standard ReadFromURIFunc installed: the per-ref file-URI guard for strict
// mode and the YAML->JSON normalization step for referenced files. Shared
// between the OpenAPI 3 load path and the Swagger 2.0 conversion path so both
// honor the same external-ref policy.
func newConfiguredOpenAPI3Loader(lenient bool, location *url.URL) *openapi3.Loader {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = lenient || location != nil
	allowLocalExternalRefs := location != nil
	loader.ReadFromURIFunc = func(loader *openapi3.Loader, refLocation *url.URL) ([]byte, error) {
		if !lenient {
			if !allowLocalExternalRefs || !isFileURI(refLocation) {
				return nil, fmt.Errorf("encountered disallowed external reference: %q", refLocation.String())
			}
		}
		data, err := openapi3.DefaultReadFromURI(loader, refLocation)
		if err != nil {
			return nil, err
		}
		if normalized, err := normalizeSpecData(data); err == nil {
			return normalized, nil
		}
		return data, nil
	}
	return loader
}

func isFileURI(location *url.URL) bool {
	return location != nil && location.Path != "" && location.Host == "" &&
		(location.Scheme == "" || location.Scheme == "file")
}

func fileLocation(path string) (*url.URL, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving OpenAPI spec path: %w", err)
	}
	return &url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}, nil
}

// IsRemoteSpecSource reports whether a spec source should be loaded as a URL.
func IsRemoteSpecSource(source string) bool {
	return strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")
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
	return stringListValue(value)
}

func stringListValue(value any) []string {
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

func strictStringListValue(value any) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out, true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out, true
	default:
		return nil, false
	}
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

func authLikeHeaderName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return false
	}
	switch normalized {
	case "authorization", "proxy-authorization", "cookie", "set-cookie":
		return false
	}
	compact := strings.NewReplacer("-", "", "_", "", " ", "").Replace(normalized)
	if strings.Contains(compact, "apikey") || strings.Contains(compact, "authkey") {
		return true
	}
	return strings.Contains(compact, "token") && (strings.Contains(compact, "auth") || strings.Contains(compact, "access"))
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
	paramNames := make([]string, 0, len(paramCounts))
	for pName := range paramCounts {
		paramNames = append(paramNames, pName)
	}
	sort.Strings(paramNames)
	for _, pName := range paramNames {
		cnt := paramCounts[pName]
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

// inferHeaderParamAPIKeyAuth handles OpenAPI specs that omit securitySchemes
// but repeat an API-key-shaped header parameter across the operation surface.
func inferHeaderParamAPIKeyAuth(doc *openapi3.T, name string, fallback spec.AuthConfig) spec.AuthConfig {
	if doc == nil || doc.Paths == nil || hasTopLevelSecurityDeclaration(doc) {
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
			for _, p := range mergeParameters(pathItem, op) {
				if p == nil || p.In != openapi3.ParameterInHeader || seen {
					continue
				}
				if authLikeHeaderName(p.Name) {
					paramCounts[p.Name]++
					seen = true
				}
			}
		}
	}

	if totalOps == 0 {
		return fallback
	}

	var best string
	var bestCount int
	paramNames := make([]string, 0, len(paramCounts))
	for pName := range paramCounts {
		paramNames = append(paramNames, pName)
	}
	sort.Strings(paramNames)
	for _, pName := range paramNames {
		cnt := paramCounts[pName]
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
		Type:     "api_key",
		In:       "header",
		Header:   best,
		EnvVars:  []string{envPrefix + "_API_KEY"},
		Inferred: true,
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
				for _, o := range overrides {
					e.HeaderOverrides = upsertHeaderOverride(e.HeaderOverrides, o.Name, o.Value)
				}
				r.Endpoints[eName] = e
			}
		}
		for subName, sub := range r.SubResources {
			for eName, e := range sub.Endpoints {
				overrides := headerOverridesForPath(e.Path, perEndpoint)
				if len(overrides) > 0 {
					for _, o := range overrides {
						e.HeaderOverrides = upsertHeaderOverride(e.HeaderOverrides, o.Name, o.Value)
					}
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
	"app installation token",
	"fine-grained pat",
	"oauth app token",
	"personal access token",
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

func selectSecurityScheme(doc *openapi3.T, authPreference string) (string, *openapi3.SecurityScheme) {
	if doc == nil || doc.Components == nil || len(doc.Components.SecuritySchemes) == 0 {
		return "", nil
	}

	if pref := strings.TrimSpace(authPreference); pref != "" {
		for _, name := range allSecuritySchemeNames(doc) {
			if !strings.EqualFold(name, pref) {
				continue
			}
			scheme := securitySchemeValue(doc.Components.SecuritySchemes[name])
			if scheme != nil {
				return name, scheme
			}
		}
	}

	usageCounts := securitySchemeOperationUsageCounts(doc)
	candidates := candidateSecuritySchemeNames(doc, usageCounts)
	effectiveRequirements := allEffectiveSecurityRequirements(doc)

	bestScore := math.MaxInt
	bestUsageCount := 0
	var bestName string
	var bestScheme *openapi3.SecurityScheme

	for _, name := range candidates {
		scheme := securitySchemeValue(doc.Components.SecuritySchemes[name])
		if scheme == nil {
			continue
		}
		usageCount := usageCounts[name]
		score := schemePriorityScoreForDoc(doc, effectiveRequirements, name, scheme)
		if usageCount > bestUsageCount || (usageCount == bestUsageCount && score < bestScore) {
			bestUsageCount = usageCount
			bestScore = score
			bestName = name
			bestScheme = scheme
		}
	}

	return bestName, bestScheme
}

// Effective operation security is authoritative when present: a
// components-only scheme (e.g. an OAuth2 marketplace flow the API doesn't
// actually accept) must not outrank a scheme used by the operation surface.
// The root-security fallback covers specs without paths or security-bearing
// operations. The empty-names guard handles `security: []` and `security: [{}]`
// no-auth declarations, where falling back to components recovers the previous
// behavior for inferred-auth specs.
func candidateSecuritySchemeNames(doc *openapi3.T, usageCounts map[string]int) []string {
	if len(usageCounts) > 0 {
		names := make([]string, 0, len(usageCounts))
		for name := range usageCounts {
			names = append(names, name)
		}
		sort.Strings(names)
		return names
	}

	seen := map[string]struct{}{}
	var names []string

	if doc.Security != nil {
		for _, requirement := range definedSecurityRequirements(doc, doc.Security) {
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
		if len(names) > 0 {
			return names
		}
	}

	return allSecuritySchemeNames(doc)
}

func allSecuritySchemeNames(doc *openapi3.T) []string {
	if doc == nil || doc.Components == nil {
		return nil
	}
	names := make([]string, 0, len(doc.Components.SecuritySchemes))
	for name := range doc.Components.SecuritySchemes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func securitySchemeOperationUsageCounts(doc *openapi3.T) map[string]int {
	counts := map[string]int{}
	if doc == nil || doc.Paths == nil {
		return counts
	}

	for _, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		for _, op := range pathItem.Operations() {
			if op == nil {
				continue
			}
			requirements := effectiveSecurityRequirements(op, doc)
			counted := map[string]struct{}{}
			for _, requirement := range requirements {
				for name := range requirement {
					if _, ok := counted[name]; ok {
						continue
					}
					counted[name] = struct{}{}
					counts[name]++
				}
			}
		}
	}
	return counts
}

func effectiveSecurityRequirements(op *openapi3.Operation, doc *openapi3.T) openapi3.SecurityRequirements {
	if op != nil && op.Security != nil {
		requirements := definedSecurityRequirements(doc, *op.Security)
		if len(requirements) > 0 || securityRequirementsAllowAnonymous(*op.Security) {
			return requirements
		}
		if securityRequirementsReferenceDefinedScheme(doc, *op.Security) {
			return nil
		}
		return definedSecurityRequirements(doc, doc.Security)
	}
	if doc == nil {
		return nil
	}
	return definedSecurityRequirements(doc, doc.Security)
}

func definedSecurityRequirements(doc *openapi3.T, requirements openapi3.SecurityRequirements) openapi3.SecurityRequirements {
	if len(requirements) == 0 {
		return requirements
	}
	if doc == nil || doc.Components == nil || len(doc.Components.SecuritySchemes) == 0 {
		return nil
	}

	filtered := make(openapi3.SecurityRequirements, 0, len(requirements))
	for _, requirement := range requirements {
		if len(requirement) == 0 {
			filtered = append(filtered, requirement)
			continue
		}
		defined := openapi3.SecurityRequirement{}
		hasUndefined := false
		for name, scopes := range requirement {
			if securitySchemeValue(doc.Components.SecuritySchemes[name]) == nil {
				hasUndefined = true
				break
			}
			defined[name] = scopes
		}
		if !hasUndefined && len(defined) > 0 {
			filtered = append(filtered, defined)
		}
	}
	return filtered
}

func securityRequirementsReferenceDefinedScheme(doc *openapi3.T, requirements openapi3.SecurityRequirements) bool {
	if doc == nil || doc.Components == nil || len(doc.Components.SecuritySchemes) == 0 {
		return false
	}
	for _, requirement := range requirements {
		for name := range requirement {
			if securitySchemeValue(doc.Components.SecuritySchemes[name]) != nil {
				return true
			}
		}
	}
	return false
}

// Ordering rationale: Bearer is the simplest already-minted token shape and
// stays first. OAuth2 Device Code and Password (ROPC) are CLI-friendly flows
// that stay above apiKey so specs that intentionally offer them do not silently
// regress. Among OAuth2 alternatives, client_credentials keeps its old edge
// over password for non-interactive runs. A standalone apiKey-in-header beats
// OAuth2 browser/client-credential flows because printed CLIs usually want the
// simplest user-supplied token when the API offers alternatives. apiKey-in-query
// stays lower because query strings leak to logs. HTTP Basic ranks below
// standalone apiKey because compound legacy schemes (email + key) surface as
// basic-ish patterns and shouldn't outrank a modern apiKey alternative when
// both are offered.
const (
	schemePriorityBearer          = 0
	schemePriorityOAuth2Device    = 25
	schemePriorityOAuth2CC        = 30
	schemePriorityOAuth2Password  = 40
	schemePriorityAPIKeyHeader    = 50
	schemePriorityOAuth2CCAlt     = 100
	schemePriorityOAuth2AuthCode  = 200
	schemePriorityOAuth2Implicit  = 300
	schemePriorityAPIKeyANDPeer   = 400
	schemePriorityAPIKeyQuery     = 450
	schemePriorityHTTPBasic       = 500
	schemePriorityAPIKeyCookie    = 600
	schemePriorityAPIKeyOther     = 700
	schemePriorityOAuth2Malformed = 800
	schemePriorityHTTPOther       = 900
	schemePriorityUnknown         = 1000
)

func schemePriorityScore(scheme *openapi3.SecurityScheme) int {
	switch strings.ToLower(scheme.Type) {
	case "http":
		switch strings.ToLower(scheme.Scheme) {
		case "bearer":
			return schemePriorityBearer
		case "basic":
			return schemePriorityHTTPBasic
		}
		return schemePriorityHTTPOther
	case "oauth2":
		if _, ok := scheme.Extensions[extensionOAuthDeviceFlow]; ok {
			return schemePriorityOAuth2Device
		}
		if scheme.Flows != nil {
			if cc := scheme.Flows.ClientCredentials; cc != nil && strings.TrimSpace(cc.TokenURL) != "" {
				return schemePriorityOAuth2CC
			}
			if ac := scheme.Flows.AuthorizationCode; ac != nil && strings.TrimSpace(ac.AuthorizationURL) != "" && strings.TrimSpace(ac.TokenURL) != "" {
				return schemePriorityOAuth2AuthCode
			}
			if pw := scheme.Flows.Password; pw != nil && strings.TrimSpace(pw.TokenURL) != "" {
				return schemePriorityOAuth2Password
			}
			if ic := scheme.Flows.Implicit; ic != nil && strings.TrimSpace(ic.AuthorizationURL) != "" {
				return schemePriorityOAuth2Implicit
			}
		}
		return schemePriorityOAuth2Malformed
	case "apikey":
		switch strings.ToLower(scheme.In) {
		case "header":
			return schemePriorityAPIKeyHeader
		case "query":
			return schemePriorityAPIKeyQuery
		case "cookie":
			return schemePriorityAPIKeyCookie
		}
		return schemePriorityAPIKeyOther
	}
	return schemePriorityUnknown
}

func schemePriorityScoreForDoc(doc *openapi3.T, requirements openapi3.SecurityRequirements, name string, scheme *openapi3.SecurityScheme) int {
	score := schemePriorityScore(scheme)
	if score == schemePriorityAPIKeyHeader && apiKeyOnlyAppearsWithNonAPIKeySibling(doc, requirements, name) {
		return schemePriorityAPIKeyANDPeer
	}
	if score == schemePriorityOAuth2CC && hasStandaloneHeaderAPIKeyRequirement(doc, requirements) {
		return schemePriorityOAuth2CCAlt
	}
	return score
}

func apiKeyOnlyAppearsWithNonAPIKeySibling(doc *openapi3.T, requirements openapi3.SecurityRequirements, schemeName string) bool {
	seen := false
	for _, requirement := range requirements {
		if _, ok := requirement[schemeName]; !ok {
			continue
		}
		seen = true
		hasNonAPIKeySibling := false
		for siblingName := range requirement {
			if siblingName == schemeName {
				continue
			}
			sibling := securitySchemeValue(doc.Components.SecuritySchemes[siblingName])
			if sibling != nil && !strings.EqualFold(sibling.Type, "apiKey") {
				hasNonAPIKeySibling = true
				break
			}
		}
		if !hasNonAPIKeySibling {
			return false
		}
	}
	return seen
}

func hasStandaloneHeaderAPIKeyRequirement(doc *openapi3.T, requirements openapi3.SecurityRequirements) bool {
	for _, requirement := range requirements {
		hasHeaderAPIKey := false
		hasNonAPIKeySibling := false
		for schemeName := range requirement {
			scheme := securitySchemeValue(doc.Components.SecuritySchemes[schemeName])
			if scheme == nil {
				continue
			}
			if strings.EqualFold(scheme.Type, "apiKey") && strings.EqualFold(scheme.In, "header") {
				hasHeaderAPIKey = true
				continue
			}
			if !strings.EqualFold(scheme.Type, "apiKey") {
				hasNonAPIKeySibling = true
			}
		}
		if hasHeaderAPIKey && !hasNonAPIKeySibling {
			return true
		}
	}
	return false
}

func allEffectiveSecurityRequirements(doc *openapi3.T) openapi3.SecurityRequirements {
	if doc == nil {
		return nil
	}
	var requirements openapi3.SecurityRequirements
	rootIncluded := false
	if doc.Paths != nil {
		for _, pathItem := range doc.Paths.Map() {
			if pathItem == nil {
				continue
			}
			for _, op := range pathItem.Operations() {
				if op == nil {
					continue
				}
				if op.Security != nil {
					requirements = append(requirements, effectiveSecurityRequirements(op, doc)...)
					continue
				}
				if !rootIncluded {
					requirements = append(requirements, definedSecurityRequirements(doc, doc.Security)...)
					rootIncluded = true
				}
			}
		}
	}
	if len(requirements) > 0 {
		return requirements
	}
	return definedSecurityRequirements(doc, doc.Security)
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

func mapResources(doc *openapi3.T, out *spec.APISpec, basePath string) error {
	if doc == nil || out == nil || doc.Paths == nil {
		return nil
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
	frameworkRenames := map[string]string{}
	googleDiscovery := isGoogleDiscoverySpec(doc)
	reservedTemplateRenames, err := reservedTemplateCollisionRenames(doc, pathKeys, basePath, commonPrefix, googleDiscovery, out)
	if err != nil {
		return err
	}

	// Auto-calibrate endpoint limit unless the user explicitly set it.
	// Pre-scans the spec to find the largest resource or sub-resource,
	// then bumps the limit so well-formed specs never have endpoints silently
	// skipped. The limit is checked per endpoint map (resource.Endpoints or
	// sub.Endpoints), so we count per (primary, sub) pair to find the true max.
	if !endpointLimitExplicit {
		type resourceKey struct{ primary, sub string }
		endpointCounts := map[resourceKey]int{}
		for _, path := range pathKeys {
			pathItem := doc.Paths.Value(path)
			if pathItem == nil {
				continue
			}
			for _, op := range pathItem.Operations() {
				primaryName, subName := resourceAndSubForOperation(path, basePath, commonPrefix, op, googleDiscovery)
				if primaryName == "" {
					continue
				}
				endpointCounts[resourceKey{primaryName, subName}]++
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
		pathTenantScopeColumn := readPathItemTenantScopeColumn(pathItem, path)
		pathMembershipField := readPathItemMembershipField(pathItem, path)
		pathCritical := readPathItemCritical(pathItem, path)
		pathSyncable, _ := boolExtension(pathItem.Extensions, extensionPPSyncable)
		pathTier := readTierExtension(pathItem.Extensions, fmt.Sprintf("path %q", path))
		pathDataSourceStrategy := readDataSourceStrategyExtension(pathItem.Extensions, fmt.Sprintf("path %q", path))
		pathLiveDogfoodTier := readLiveDogfoodTierExtension(pathItem.Extensions, fmt.Sprintf("path %q", path))

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

			primaryName, subName := resourceAndSubForOperation(path, basePath, commonPrefix, op, googleDiscovery)
			if primaryName == "" {
				warnf("skipping %s %q: could not derive resource name", method, path)
				continue
			}
			if renamed, ok := reservedTemplateRenames[primaryName]; ok {
				primaryName = renamed
			}
			endpointResourceName := primaryName
			if subName != "" {
				endpointResourceName = subName
			}

			// Framework cobra collision check. Sub-resource names do not shadow
			// framework commands, but every primary resource becomes a top-level
			// Cobra command, even when the current endpoint lands under a
			// sub-resource.
			if renamed, ok := frameworkRenames[primaryName]; ok {
				primaryName = renamed
			} else if out.ParseTimeReservedCobraUseName(primaryName) {
				originalName := primaryName
				primaryName = renameForFrameworkCollision(out, primaryName, path)
				frameworkRenames[originalName] = primaryName
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

			if len(targetEndpoints) >= maxEndpointsPerResource {
				warnf("skipping %s %q: endpoint limit (%d) reached for resource %q.%s", method, path, maxEndpointsPerResource, primaryName, targetResourceName)
				continue
			}

			endpointName := resolveEndpointName(method, path, op, targetEndpoints, endpointResourceName, basePath, commonPrefix)
			summary := strings.TrimSpace(op.Summary)
			desc := strings.TrimSpace(op.Description)
			description := selectDescription(summary, desc)
			descriptionSynthesized := false

			if description == "" {
				description = humanizeEndpointName(endpointName)
				descriptionSynthesized = true
			}

			params, err := mapParameters(pathItem, op)
			if err != nil {
				return fmt.Errorf("map parameters for %s %q: %w", strings.ToUpper(method), path, err)
			}
			body, requestContentType, bodyJSONFallback, bodyRequired, bodyIsArray := mapRequestBody(op.RequestBody, method, path)

			endpoint := spec.Endpoint{
				Method:                 strings.ToUpper(method),
				Path:                   path,
				BaseURL:                operationServerBaseURL(out.BaseURL, pathItem, op),
				Description:            description,
				DescriptionSynthesized: descriptionSynthesized,
				Params:                 params,
				Body:                   body,
				BodyJSONFallback:       bodyJSONFallback,
				BodyRequired:           bodyRequired,
				BodyIsArray:            bodyIsArray,
				RequestContentType:     requestContentType,
				Tags:                   append([]string{}, op.Tags...),
			}
			endpoint.Tier = readTierExtension(op.Extensions, fmt.Sprintf("%s %q", strings.ToUpper(method), path))
			if endpoint.Tier == "" {
				endpoint.Tier = pathTier
			}
			requiresRole, err := readRequiresRoleExtension(op.Extensions, fmt.Sprintf("%s %q", strings.ToUpper(method), path))
			if err != nil {
				return err
			}
			endpoint.RequiresRole = requiresRole
			endpoint.DataSourceStrategy = readDataSourceStrategyExtension(op.Extensions, fmt.Sprintf("%s %q", strings.ToUpper(method), path))
			if endpoint.DataSourceStrategy == "" {
				endpoint.DataSourceStrategy = pathDataSourceStrategy
			}
			endpoint.HappyArgs = readHappyArgsExtension(op.Extensions, fmt.Sprintf("%s %q", strings.ToUpper(method), path))
			if ex := readExampleExtension(op.Extensions, fmt.Sprintf("%s %q", strings.ToUpper(method), path)); ex != "" {
				endpoint.Example = ex
			}
			endpoint.LiveDogfoodRequiresTier = readLiveDogfoodTierExtension(op.Extensions, fmt.Sprintf("%s %q", strings.ToUpper(method), path))
			if endpoint.LiveDogfoodRequiresTier == "" {
				endpoint.LiveDogfoodRequiresTier = pathLiveDogfoodTier
			}
			if endpoint.LiveDogfoodRequiresTier == "" {
				endpoint.LiveDogfoodRequiresTier = inferLiveDogfoodTier(method, path, op)
			}

			// Namespace the inline-item synthetic name with the resource so
			// two resources whose default GET endpoints both compute the
			// same endpointName ("list") don't collide on a shared
			// "ListItem" Types entry.
			endpoint.Response, endpoint.ResponsePath = mapResponse(op, targetResourceName+"_"+endpointName, out)
			populateFieldSelectorDefaults(&endpoint, op)
			if strings.ToUpper(method) == "GET" {
				endpoint.Pagination = readPaginationExtension(op.Extensions, fmt.Sprintf("%s %q", strings.ToUpper(method), path))
				if endpoint.Pagination == nil {
					endpoint.Pagination = detectPagination(endpoint.Params, op)
				}
				// Only single-resource fetches (GET /resource/{id}) can carry
				// embedded paged sub-resources; list endpoints ARE the paged
				// endpoint themselves and don't need a companion helper.
				if strings.Contains(path, "{") {
					endpoint.EmbeddedPagedSubresources = detectEmbeddedPagedSubresources(op, path)
				}
			}
			endpoint.NoAuth = operationAllowsAnonymous(op, doc)

			// IDField fallback chain: explicit x-resource-id wins over
			// path-param/resource-shape inference, which wins over the generic
			// response-schema inference. Resolution happens at parse time so
			// the profiler sees a single resolved value per endpoint and
			// templates do not re-walk schemas at generation time.
			if responseUsesBinary(op) {
				endpoint.ResponseFormat = spec.ResponseFormatBinary
			} else if responseUsesXML(op) {
				// XML-only success bodies are normalized to JSON by the
				// generated client (response_format: xml). Pin Accept so the
				// server returns XML instead of 406-ing the default
				// application/json.
				endpoint.ResponseFormat = spec.ResponseFormatXML
				endpoint.HeaderOverrides = upsertHeaderOverride(endpoint.HeaderOverrides, "Accept", "application/xml")
			}
			if pathResourceIDOverride != "" {
				endpoint.IDField = pathResourceIDOverride
			} else if idField := resolveIDFieldFromPathParam(op, path, targetResourceName); idField != "" {
				endpoint.IDField = idField
				endpoint.IDFieldFromPathParam = true
			} else {
				endpoint.IDField = resolveIDFieldFromResponseSchema(op, targetResourceName)
			}
			if strings.ToUpper(method) == "POST" {
				endpoint.Pagination = detectPostQueryIDWalkPagination(endpoint.Body, op, endpoint.IDField)
			}
			endpoint.TenantScopeColumn = pathTenantScopeColumn
			endpoint.MembershipField = pathMembershipField
			endpoint.Critical = pathCritical
			opSyncable, _ := boolExtension(op.Extensions, extensionPPSyncable)
			endpoint.Syncable = pathSyncable || opSyncable
			endpoint.Walker = readWalkerExtension(op.Extensions, fmt.Sprintf("%s %q", strings.ToUpper(method), path))

			// Binary-only success responses (e.g. PDF/octet-stream downloads)
			// would otherwise receive the default Accept: application/json and
			// be rejected with HTTP 406. Pin Accept to what the server
			// produces; rides the existing per-endpoint header-override path.
			if accept := binaryResponseAcceptType(op); accept != "" {
				endpoint.HeaderOverrides = upsertHeaderOverride(endpoint.HeaderOverrides, "Accept", accept)
			}

			targetEndpoints[endpointName] = endpoint

			// Update descriptions
			if subName != "" {
				sub := resource.SubResources[subName]
				if sub.Description == "" {
					sub.Description = humanizeResourceName(subName)
					sub.DescriptionDerived = true
				}
				resource.SubResources[subName] = sub
			}
			if resource.Description == "" {
				resource.Description = humanizeResourceName(primaryName)
				resource.DescriptionDerived = true
			}
			out.Resources[primaryName] = resource
		}
	}

	assignEndpointAliases(out.Resources)
	classifyGlobalParams(out.Resources)
	return nil
}

func operationServerBaseURL(specBaseURL string, pathItem *openapi3.PathItem, op *openapi3.Operation) string {
	var servers openapi3.Servers
	if pathItem != nil {
		servers = pathItem.Servers
	}
	if op != nil && op.Servers != nil {
		servers = *op.Servers
	}
	if len(servers) == 0 || servers[0] == nil {
		return ""
	}
	baseURL, basePath := resolveServerURL(servers[0])
	if baseURL != "" {
		baseURL += basePath
	} else if basePath != "" {
		configuredBase, configuredErr := url.Parse(strings.TrimSpace(specBaseURL))
		relativeBase, relativeErr := url.Parse(basePath)
		if configuredErr == nil && relativeErr == nil && configuredBase.IsAbs() {
			baseURL = configuredBase.ResolveReference(relativeBase).String()
		}
	}
	if baseURL == "" || baseURL == strings.TrimRight(specBaseURL, "/") {
		return ""
	}
	return baseURL
}

// operationAllowsAnonymous checks whether an operation can be called without
// authentication. Returns true when:
//   - The operation has security: [] (explicit opt-out)
//   - The operation has security: [{}] (empty object = anonymous alternative)
//   - The operation inherits global security and the global security is empty
func operationAllowsAnonymous(op *openapi3.Operation, doc *openapi3.T) bool {
	if op != nil && op.Security != nil {
		return securityRequirementsAllowAnonymous(*op.Security)
	}
	if doc != nil && doc.Security != nil {
		return securityRequirementsAllowAnonymous(doc.Security)
	}
	return false
}

func securityRequirementsAllowAnonymous(requirements openapi3.SecurityRequirements) bool {
	if len(requirements) == 0 {
		return true
	}
	for _, req := range requirements {
		if len(req) == 0 {
			return true
		}
	}
	return false
}

// resolveServerURLTemplate is the top-level entry point used for
// `doc.Servers[0]`. It preserves `{var}` placeholders for variables that the
// spec declares an explicit Variables entry for, so the generator can emit a
// runtime substitution path (env var > spec default) in config.Load() rather
// than baking the default into BaseURL at generate time. Returns the same
// (baseURL, basePath) pair as resolveServerURL plus the placeholder names
// that survived substitution and a map of their declared defaults. Variables
// without an explicit Variables entry (e.g. dangling `{foo}` markers in a
// hand-written spec) fall through to the legacy strip-unresolved behavior so
// stale specs don't suddenly require an env var that doesn't exist.
func resolveServerURLTemplate(server *openapi3.Server) (baseURL, basePath string, placeholders []string, defaults map[string]string) {
	if server == nil {
		return "", "", nil, nil
	}
	serverURL := strings.TrimRight(strings.TrimSpace(server.URL), "/")
	if strings.Contains(serverURL, "{") && server.Variables != nil {
		// First pass: identify which `{var}` markers are backed by an explicit
		// variable definition. These get preserved for runtime substitution.
		// Order placeholders by left-to-right appearance in the URL so the
		// generated EndpointTemplateVars slice has a stable, intuitive shape;
		// Go map iteration would otherwise produce nondeterministic ordering.
		matches := templateVarPattern.FindAllStringSubmatch(serverURL, -1)
		seen := map[string]bool{}
		for _, m := range matches {
			name := m[1]
			if seen[name] {
				continue
			}
			variable, ok := server.Variables[name]
			if !ok || variable == nil {
				continue
			}
			seen[name] = true
			placeholders = append(placeholders, name)
			if defaults == nil {
				defaults = map[string]string{}
			}
			defaults[name] = variable.Default
		}
		// Second pass: bake in the default for any Variables entry the first
		// pass did NOT register for runtime substitution. The first pass
		// only registers names that match templateVarPattern's identifier
		// regex (`[a-zA-Z_][a-zA-Z0-9_]*`); a variable with a hyphenated or
		// digit-leading name (`{server-id}`, `{2nd-host}` — OpenAPI 3.0
		// places no character restriction on variable names) falls through
		// here so its default is substituted in place. Without this, the
		// strip pass below would delete the placeholder entirely and the
		// resulting URL would DNS-fail with no actionable hint.
		for varName, variable := range server.Variables {
			if _, runtime := defaults[varName]; runtime {
				continue
			}
			if variable != nil && variable.Default != "" {
				serverURL = strings.ReplaceAll(serverURL, "{"+varName+"}", variable.Default)
			}
		}
	}
	// Strip any remaining unresolved placeholders that don't have a runtime
	// substitution path (matches the legacy resolveServerURL behavior).
	// Runtime placeholders are left in place so the printed CLI's buildURL
	// can substitute them per request.
	serverURL = templateVarPattern.ReplaceAllStringFunc(serverURL, func(match string) string {
		name := match[1 : len(match)-1]
		if _, runtime := defaults[name]; runtime {
			return match
		}
		return ""
	})
	if baseURL, basePath, ok := resolveProtocolRelativeServerURL(serverURL); ok {
		return baseURL, basePath, placeholders, defaults
	}
	serverURL = normalizeURLSlashes(serverURL)
	serverURL = strings.TrimRight(serverURL, "/")
	if serverURL == "" {
		return "", "", placeholders, defaults
	}
	lowerURL := strings.ToLower(serverURL)
	if strings.HasPrefix(lowerURL, "http://") ||
		strings.HasPrefix(lowerURL, "https://") ||
		hasRuntimeTemplateScheme(serverURL, defaults) {
		return serverURL, "", placeholders, defaults
	}
	return "", serverURL, placeholders, defaults
}

// templateVarPattern mirrors the regex used by the generated `buildURL` helper
// so the parser and the runtime substitute the same set of placeholder names.
var templateVarPattern = regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)

func hasRuntimeTemplateScheme(s string, defaults map[string]string) bool {
	scheme, _, ok := strings.Cut(s, "://")
	if !ok {
		return false
	}
	matches := templateVarPattern.FindStringSubmatch(scheme)
	// Only a single bare placeholder is an absolute runtime-template scheme.
	// Compound schemes fall through to the legacy relative-path handling so
	// malformed or intentionally relative inputs are not broadened silently.
	if len(matches) != 2 || matches[0] != scheme {
		return false
	}
	_, ok = defaults[matches[1]]
	return ok
}

func normalizeURLSlashes(s string) string {
	if scheme, rest, ok := strings.Cut(s, "://"); ok {
		if scheme != "" {
			return scheme + "://" + strings.ReplaceAll(rest, "//", "/")
		}
	}
	s = strings.ReplaceAll(s, "//", "/")
	s = strings.Replace(s, "http:/", "http://", 1)
	s = strings.Replace(s, "https:/", "https://", 1)
	return s
}

func preferredTopLevelServer(servers openapi3.Servers) *openapi3.Server {
	if len(servers) == 0 || servers[0] == nil {
		return nil
	}
	protocolRelativeHost, ok := protocolRelativeServerHost(servers[0].URL)
	if !ok {
		return servers[0]
	}
	for _, server := range servers[1:] {
		if server == nil {
			continue
		}
		parsed, err := url.Parse(strings.TrimSpace(server.URL))
		if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") &&
			strings.EqualFold(parsed.Host, protocolRelativeHost) {
			return server
		}
	}
	return servers[0]
}

func protocolRelativeServerHost(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "///") {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", false
	}
	return parsed.Host, true
}

func resolveProtocolRelativeServerURL(raw string) (baseURL, basePath string, ok bool) {
	host, ok := protocolRelativeServerHost(raw)
	if !ok {
		return "", "", false
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", false
	}
	path := parsed.EscapedPath()
	return "https://" + host, strings.TrimRight(path, "/"), true
}

// mergeServerTemplatePlaceholders folds the placeholders the parser pulled
// off `doc.Servers[0].Variables` into the existing EndpointTemplateVars list
// (today populated only by x-tenant-env-var). Order: extension-declared
// placeholders first, then server-URL placeholders in spec order, deduped.
func mergeServerTemplatePlaceholders(existing []string, serverPlaceholders []string, serverDefaults map[string]string) ([]string, map[string]string) {
	if len(serverPlaceholders) == 0 && len(serverDefaults) == 0 {
		return existing, nil
	}
	seen := make(map[string]bool, len(existing)+len(serverPlaceholders))
	out := make([]string, 0, len(existing)+len(serverPlaceholders))
	for _, name := range existing {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, name := range serverPlaceholders {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	var defaults map[string]string
	if len(serverDefaults) > 0 {
		defaults = make(map[string]string, len(serverDefaults))
		for name, val := range serverDefaults {
			if val == "" {
				continue
			}
			defaults[name] = val
		}
		if len(defaults) == 0 {
			defaults = nil
		}
	}
	return out, defaults
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
	if baseURL, basePath, ok := resolveProtocolRelativeServerURL(serverURL); ok {
		return baseURL, basePath
	}
	serverURL = normalizeURLSlashes(serverURL)
	serverURL = strings.TrimRight(serverURL, "/")
	if serverURL == "" {
		return "", ""
	}
	lowerURL := strings.ToLower(serverURL)
	if strings.HasPrefix(lowerURL, "http://") || strings.HasPrefix(lowerURL, "https://") {
		return serverURL, ""
	}
	// Relative URL; caller will need to surface as basePath.
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
	type resolvedServer struct {
		baseURL  string
		basePath string
	}
	urlCounts := map[resolvedServer]int{}
	pathCounts := map[string]int{}
	tally := func(servers openapi3.Servers) {
		for _, srv := range servers {
			u, p := resolveServerURL(srv)
			if u != "" {
				urlCounts[resolvedServer{baseURL: u, basePath: p}]++
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
	var best resolvedServer
	var bestCount int
	for server, count := range urlCounts {
		key := server.baseURL + server.basePath
		bestKey := best.baseURL + best.basePath
		if count > bestCount || (count == bestCount && key < bestKey) {
			best = server
			bestCount = count
		}
	}
	if best.baseURL != "" {
		return best.baseURL, best.basePath
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

// applyPathParamDefaults bakes EndpointPathParamDefaults entries into every
// operation path under result.Resources and drops the matching path
// parameter from each endpoint's Params list. Runs as a post-parse sweep so
// downstream consumers (profiler, generator, manifest emitter) see fully
// resolved paths and never re-emit a flag for the resolved placeholder.
// No-op for specs that declare no defaults, preserving byte-compat with
// existing generated outputs.
func applyPathParamDefaults(result *spec.APISpec) {
	if result == nil || len(result.EndpointPathParamDefaults) == 0 {
		return
	}
	defaults := result.EndpointPathParamDefaults
	var walk func(resources map[string]spec.Resource)
	walk = func(resources map[string]spec.Resource) {
		for key, r := range resources {
			for name, value := range defaults {
				r.Path = strings.ReplaceAll(r.Path, "{"+name+"}", value)
			}
			for eName, e := range r.Endpoints {
				substituted := map[string]bool{}
				for name, value := range defaults {
					placeholder := "{" + name + "}"
					if strings.Contains(e.Path, placeholder) {
						e.Path = strings.ReplaceAll(e.Path, placeholder, value)
						substituted[name] = true
					}
				}
				if len(substituted) > 0 && len(e.Params) > 0 {
					kept := make([]spec.Param, 0, len(e.Params))
					for _, p := range e.Params {
						if substituted[p.Name] && isPathSubstitutionParam(p) {
							continue
						}
						kept = append(kept, p)
					}
					e.Params = kept
				}
				r.Endpoints[eName] = e
			}
			if len(r.SubResources) > 0 {
				walk(r.SubResources)
			}
			resources[key] = r
		}
	}
	walk(result.Resources)
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

func classifyGlobalParams(resources map[string]spec.Resource) {
	totalEndpoints := 0
	paramCounts := map[string]*globalParamCount{}

	walkResourceEndpoints(resources, func(endpoint *spec.Endpoint) {
		totalEndpoints++

		seen := map[string]struct{}{}
		for _, param := range endpoint.Params {
			if isPathSubstitutionParam(param) {
				continue
			}
			key := strings.ToLower(param.Name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			count := paramCounts[key]
			if count == nil {
				count = &globalParamCount{name: param.Name, typ: param.Type}
				paramCounts[key] = count
			}
			count.count++
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

	scopeParams := map[string]*globalParamCount{}
	filteredParams := map[string]*globalParamCount{}
	scopeThreshold := float64(totalEndpoints) * 0.5
	filterThreshold := float64(totalEndpoints) * 0.8
	for key, count := range paramCounts {
		switch {
		case float64(count.count) >= scopeThreshold && isGlobalScopeParamName(count.name) && strings.EqualFold(count.typ, "string"):
			scopeParams[key] = count
		case float64(count.count) > filterThreshold:
			filteredParams[key] = count
		}
	}

	if len(scopeParams) == 0 && len(filteredParams) == 0 {
		return
	}

	// droppedCounts records how many endpoints each global param was actually
	// removed from, so the warning reflects reality: a param retained on every
	// endpoint (because dropping it would empty the surface) must not be
	// reported as filtered.
	droppedCounts := map[string]int{}
	walkResourceEndpoints(resources, func(endpoint *spec.Endpoint) {
		// Keep tRPC-style global `input` params when dropping them would leave
		// the endpoint with no non-path request params at all. Other global
		// query params like `limit` should still be filtered even when that
		// leaves an endpoint with no request surface.
		hasNonFilteredSurface := false
		nonPathCount := 0
		for _, param := range endpoint.Params {
			if isPathSubstitutionParam(param) {
				continue
			}
			nonPathCount++
			if isGlobalFilterCandidate(param) {
				key := strings.ToLower(param.Name)
				if _, ok := filteredParams[key]; ok {
					if isRetainableSoleGlobalInputParam(param) {
						continue
					}
				}
			}
			hasNonFilteredSurface = true
			break
		}
		if nonPathCount > 0 && !hasNonFilteredSurface {
			return
		}

		filtered := endpoint.Params[:0]
		for _, param := range endpoint.Params {
			key := strings.ToLower(param.Name)
			if _, ok := scopeParams[key]; ok {
				param.Required = true
				param.GlobalScope = true
			} else if isGlobalFilterCandidate(param) {
				if _, ok := filteredParams[key]; ok {
					droppedCounts[key]++
					continue
				}
			}
			filtered = append(filtered, param)
		}
		endpoint.Params = filtered
	})

	scopeNames := make([]string, 0, len(scopeParams))
	for _, param := range scopeParams {
		scopeNames = append(scopeNames, param.name)
	}
	sort.Strings(scopeNames)

	for _, name := range scopeNames {
		key := strings.ToLower(name)
		warnf("promoted global scope query param %q to required flag: present on %d/%d endpoints", name, scopeParams[key].count, totalEndpoints)
	}

	filteredNames := make([]string, 0, len(droppedCounts))
	for key := range droppedCounts {
		filteredNames = append(filteredNames, key)
	}
	sort.Strings(filteredNames)

	for _, key := range filteredNames {
		warnf("filtered global query param %q from generated commands: dropped from %d/%d endpoints where present (%d total)", filteredParams[key].name, droppedCounts[key], filteredParams[key].count, totalEndpoints)
	}
}

type globalParamCount struct {
	name  string
	typ   string
	count int
}

func isPathSubstitutionParam(param spec.Param) bool {
	return param.Positional || param.PathParam
}

func isGlobalFilterCandidate(param spec.Param) bool {
	// A param carrying an explicit default expresses deliberate must-send
	// intent: the author wants that value on the wire, not the API's implicit
	// server-side default. Exclude such params from the global-frequency filter
	// so a ubiquitous-but-load-bearing flag (e.g. a supportsAllDrives-style
	// access scope that defaults true) is not silently stripped, while plain
	// high-frequency boilerplate (prettyPrint, quotaUser) with no default is
	// still dropped.
	return !isPathSubstitutionParam(param) && !param.Required && param.Default == nil
}

func isRetainableSoleGlobalInputParam(param spec.Param) bool {
	return strings.EqualFold(param.Name, "input")
}

func isGlobalScopeParamName(name string) bool {
	normalized := strings.ToLower(globalScopeParamNormalizerRE.ReplaceAllString(name, ""))
	switch normalized {
	case "tenant", "tenantid", "tenantfilter",
		"workspace", "workspaceid", "workspacefilter",
		"organization", "organizationid", "organizationfilter",
		"organisation", "organisationid", "organisationfilter",
		"org", "orgid",
		"region", "regionid", "regionfilter",
		"account", "accountid", "accountfilter":
		return true
	default:
		return false
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
	opID := operationID(op)
	opTokens := operationIDTokens(opID)
	var name string
	if fastAPIName, ok := fastAPIOperationIDEndpointName(opTokens, path, resourceName, commonPrefix); ok {
		name = fastAPIName
	} else if !isPathDerivedFrameworkOperationID(opTokens, path) {
		name = operationIDToName(opID, resourceName, commonPrefix)
	}
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

func operationIDTokens(operationID string) []string {
	if operationID == "" {
		return nil
	}
	return strings.Split(toSnakeCase(operationID), "_")
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

func mapParameters(pathItem *openapi3.PathItem, op *openapi3.Operation) ([]spec.Param, error) {
	merged := mergeParameters(pathItem, op)
	var urlNameOverrides map[string]string
	urlNameOverridesRead := false
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
		// Skip phantom names ("" / "[]") that some specs emit for unnamed
		// array query params. They are not usable MCP/CLI arguments and would
		// send "?[]=value" on the wire. Legitimate "foo[]" names are kept.
		if trimmed := strings.TrimSpace(paramName); trimmed == "" || trimmed == "[]" {
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
		param.Example = parameterExample(parameter, schema)
		if parameter.In == openapi3.ParameterInQuery {
			serialization, err := parameter.SerializationMethod()
			if err != nil {
				return nil, fmt.Errorf("query parameter %q has invalid serialization: %w", paramName, err)
			}
			if !supportedQuerySerialization(serialization) {
				return nil, fmt.Errorf("query parameter %q has invalid serialization: style=%q explode=%t", paramName, serialization.Style, serialization.Explode)
			}
			param.QueryStyle = serialization.Style
			param.QueryExplode = &serialization.Explode
			if !urlNameOverridesRead {
				urlNameOverrides = readParamURLNameOverrides(pathItem, op)
				urlNameOverridesRead = true
			}
			param.URLName = paramURLName(paramName, parameter.Extensions, urlNameOverrides)
			if dispatch, ok := boolExtension(parameter.Extensions, extensionDispatchParam); ok {
				param.DispatchParam = dispatch
				param.DispatchParamSet = true
			}
		}
		if parameter.In == openapi3.ParameterInQuery && isFieldSelectorParameter(paramName, description) {
			param.Purpose = spec.ParamPurposeFieldSelector
		}
		if schema != nil && schema.Default != nil {
			param.Default = schema.Default
		}
		setParamMaximum(&param, schema)
		if param.Positional {
			param.Required = true
		}
		params = append(params, param)
	}

	// Reclassify path params that are modifiers (not entity identifiers) as flags.
	// This improves CLI UX: pagination/filter/date params become --flags with defaults
	// instead of required positional args.
	reclassifyPathParamModifiers(params)

	return params, nil
}

func supportedQuerySerialization(method *openapi3.SerializationMethod) bool {
	if method == nil {
		return false
	}
	switch method.Style {
	case openapi3.SerializationForm, openapi3.SerializationSpaceDelimited, openapi3.SerializationPipeDelimited:
		return true
	case openapi3.SerializationDeepObject:
		return method.Explode
	default:
		return false
	}
}

// setParamMaximum records a numeric upper-bound constraint from the schema onto
// the param, normalizing the two OpenAPI encodings of an exclusive bound.
// OpenAPI 3.1 writes `exclusiveMaximum: N` as a number; OpenAPI 3.0 writes
// `maximum: N` plus `exclusiveMaximum: true`. Either way the largest legal value
// is strictly below N, so it lands in ExclusiveMaximum; a plain inclusive
// `maximum` lands in Maximum. Downstream (the sync profiler) turns whichever is
// set into an effective integer page-size cap.
func setParamMaximum(param *spec.Param, schema *openapi3.Schema) {
	if param == nil || schema == nil {
		return
	}
	// OpenAPI 3.0: `exclusiveMaximum: true` turns `maximum` into an exclusive
	// bound — it is not also an inclusive one.
	if schema.ExclusiveMax.IsTrue() {
		if schema.Max != nil {
			param.ExclusiveMaximum = schema.Max
		}
		return
	}
	// OpenAPI 3.1 (or no exclusive modifier): `maximum` is inclusive, and a
	// numeric `exclusiveMaximum` is an independent assertion. Both may be
	// present; capture each so the profiler can take the most restrictive.
	if schema.Max != nil {
		param.Maximum = schema.Max
	}
	if schema.ExclusiveMax.Value != nil {
		param.ExclusiveMaximum = schema.ExclusiveMax.Value
	}
}

func readParamURLNameOverrides(pathItem *openapi3.PathItem, op *openapi3.Operation) map[string]string {
	var out map[string]string
	add := func(extensions map[string]any, context string) {
		if len(extensions) == 0 {
			return
		}
		raw, ok := extensions[extensionParamURLNames]
		if !ok || raw == nil {
			return
		}
		values, ok := raw.(map[string]any)
		if !ok {
			warnf("%s: %s must be an object mapping parameter names to URL names; ignoring", context, extensionParamURLNames)
			return
		}
		for name, value := range values {
			paramName := strings.TrimSpace(name)
			urlName, ok := value.(string)
			if !ok {
				warnf("%s: %s.%s must be a string; ignoring", context, extensionParamURLNames, name)
				continue
			}
			urlName = strings.TrimSpace(urlName)
			if paramName == "" || urlName == "" {
				warnf("%s: %s entries must have non-empty parameter and URL names; ignoring %q", context, extensionParamURLNames, name)
				continue
			}
			if out == nil {
				out = map[string]string{}
			}
			out[paramName] = urlName
		}
	}
	if pathItem != nil {
		add(pathItem.Extensions, "path")
	}
	if op != nil {
		add(op.Extensions, "operation")
	}
	return out
}

func paramURLName(paramName string, extensions map[string]any, overrides map[string]string) string {
	if urlName, ok := overrides[paramName]; ok {
		return urlName
	}
	if len(extensions) == 0 {
		return ""
	}
	raw, ok := extensions[extensionParamURLName]
	if !ok || raw == nil {
		return ""
	}
	urlName, ok := raw.(string)
	if !ok {
		warnf("parameter %q: %s must be a string; ignoring", paramName, extensionParamURLName)
		return ""
	}
	urlName = strings.TrimSpace(urlName)
	if urlName == "" {
		warnf("parameter %q: %s must be non-empty; ignoring", paramName, extensionParamURLName)
		return ""
	}
	return urlName
}

func isFieldSelectorParameter(name, description string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "opt_fields", "fields", "expand", "include", "select":
		return true
	}
	description = strings.ToLower(description)
	return (strings.Contains(description, "field") && strings.Contains(description, "return")) ||
		(strings.Contains(description, "field") && strings.Contains(description, "include")) ||
		(strings.Contains(description, "expand") && strings.Contains(description, "response"))
}

func populateFieldSelectorDefaults(endpoint *spec.Endpoint, op *openapi3.Operation) {
	if endpoint == nil {
		return
	}
	for i := range endpoint.Params {
		if endpoint.Params[i].Purpose == spec.ParamPurposeFieldSelector {
			endpoint.Params[i].FieldSelectorDefault = fieldSelectorDefaultFromOperation(op, endpoint.Params[i].Name)
		}
	}
}

func fieldSelectorDefaultFromOperation(op *openapi3.Operation, paramName string) string {
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
	itemSchema := unwrapItemSchema(schemaRef.Value)
	var fields []string
	switch strings.ToLower(strings.TrimSpace(paramName)) {
	case "expand", "include":
		fields = fieldSelectorRelationshipFields(itemSchema)
	default:
		fields = fieldSelectorFields(itemSchema)
	}
	return strings.Join(fields, ",")
}

func fieldSelectorFields(schema *openapi3.Schema) []string {
	if schema == nil {
		return nil
	}
	properties := map[string]*openapi3.SchemaRef{}
	collectFieldSelectorProperties(schema, properties, map[*openapi3.Schema]struct{}{})
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)

	fields := make([]string, 0, len(names))
	for _, key := range []string{"gid", "id", "sid", "uid", "uuid", "guid"} {
		propSchema := schemaRefValue(properties[key])
		if isScalarSchema(propSchema) {
			fields = append(fields, key)
		}
	}
	for _, name := range names {
		if isIdentifierFieldName(name) {
			continue
		}
		propSchema := schemaRefValue(properties[name])
		if isScalarSchema(propSchema) {
			fields = append(fields, name)
			continue
		}
		if nestedID := nestedObjectIDField(propSchema); nestedID != "" {
			fields = append(fields, name+"."+nestedID)
		}
	}
	return fields
}

func fieldSelectorRelationshipFields(schema *openapi3.Schema) []string {
	if schema == nil {
		return nil
	}
	properties := map[string]*openapi3.SchemaRef{}
	collectFieldSelectorProperties(schema, properties, map[*openapi3.Schema]struct{}{})
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)

	fields := make([]string, 0, len(names))
	for _, name := range names {
		propSchema := schemaRefValue(properties[name])
		if isObjectSchema(propSchema) || isObjectArraySchema(propSchema) {
			fields = append(fields, name)
		}
	}
	return fields
}

func isObjectArraySchema(schema *openapi3.Schema) bool {
	return isArraySchema(schema) && schema.Items != nil && isObjectSchema(schemaRefValue(schema.Items))
}

func isIdentifierFieldName(name string) bool {
	switch name {
	case "gid", "id", "sid", "uid", "uuid", "guid":
		return true
	default:
		return false
	}
}

func collectFieldSelectorProperties(schema *openapi3.Schema, properties map[string]*openapi3.SchemaRef, visited map[*openapi3.Schema]struct{}) {
	if schema == nil {
		return
	}
	if _, ok := visited[schema]; ok {
		return
	}
	visited[schema] = struct{}{}
	for name, prop := range schema.Properties {
		if strings.HasPrefix(name, "_") || prop == nil {
			continue
		}
		properties[name] = prop
	}
	for _, sub := range schema.AllOf {
		collectFieldSelectorProperties(schemaRefValue(sub), properties, visited)
	}
}

func nestedObjectIDField(schema *openapi3.Schema) string {
	if schema == nil {
		return ""
	}
	if isArraySchema(schema) && schema.Items != nil {
		schema = schemaRefValue(schema.Items)
	}
	if !isObjectSchema(schema) {
		return ""
	}
	properties := map[string]*openapi3.SchemaRef{}
	collectFieldSelectorProperties(schema, properties, map[*openapi3.Schema]struct{}{})
	for _, key := range []string{"gid", "id", "sid", "uid", "uuid", "guid"} {
		if prop := schemaRefValue(properties[key]); isScalarSchema(prop) {
			return key
		}
	}
	return ""
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

func mapRequestBody(requestBodyRef *openapi3.RequestBodyRef, method, path string) ([]spec.Param, string, bool, bool, bool) {
	requestBody := requestBodyValue(requestBodyRef)
	if requestBody == nil || requestBody.Content == nil {
		return nil, "", false, false, false
	}

	requestContentType, media := requestBodyMediaType(requestBody.Content)
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		return nil, "", false, false, false
	}

	// Bare top-level array request body: no object properties to flatten to
	// named params. Handled like the oneOf/anyOf fallback (emit the
	// --body-json string flag for the CLI) AND flagged so the MCP
	// orchestration executors send a top-level JSON array instead of the
	// params object. A strict-mapping API rejects an object at the body root
	// with HTTP 422 "Invalid json" (e.g. Tripletex [BETA] PUT
	// /supplierInvoice/voucher/{id}/postings, body [{"posting":{...}}]).
	if media.Schema.Value.Type != nil && media.Schema.Value.Type.Is(openapi3.TypeArray) {
		if !isJSONContentType(requestContentType) {
			warnf("skipping request body for %s %q: array-root body and content type %q is not JSON-shaped", strings.ToUpper(method), path, requestContentType)
			return nil, "", false, false, false
		}
		warnf("request body for %s %q is a bare top-level array; emitting --body-json fallback + array-body marker", strings.ToUpper(method), path)
		return nil, requestContentType, true, requestBody.Required, true
	}

	properties := map[string]*openapi3.SchemaRef{}
	required := map[string]struct{}{}
	if collectAllOfProperties(media.Schema, properties, required, map[*openapi3.Schema]struct{}{}) {
		// oneOf/anyOf at the body root cannot be flattened to named flags.
		// Only enable the --body-json fallback for JSON-shaped content
		// types; the runtime decode path is wired through the JSON branch
		// of the command template and does not understand multipart or
		// form-urlencoded encodings.
		if !isJSONContentType(requestContentType) {
			warnf("skipping request body for %s %q: contains oneOf/anyOf and content type %q is not JSON-shaped", strings.ToUpper(method), path, requestContentType)
			return nil, "", false, false, false
		}
		warnf("request body for %s %q contains oneOf/anyOf; emitting --body-json fallback", strings.ToUpper(method), path)
		return nil, requestContentType, true, requestBody.Required, false
	}

	if len(properties) == 0 {
		return nil, "", false, false, false
	}
	inferCSVArrays := isJSONContentType(requestContentType)

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
			Type:        mapBodyParamType(paramSchema, inferCSVArrays),
			Required:    isRequired(required, name),
			Description: description,
			Fields:      mapBodyFields(paramSchema, inferCSVArrays),
			Enum:        schemaEnum(paramSchema),
			Format:      schemaFormat(paramSchema),
			Example:     schemaExample(paramSchema),
		}
		if schemaHasCompositeUnionAlternative(paramSchema, map[*openapi3.Schema]struct{}{}) {
			param.Type = "string"
			param.Format = "json_or_scalar"
		}
		if inferCSVArrays && isStringArraySchema(paramSchema) {
			param.ItemType = "string"
		}
		if paramSchema != nil && paramSchema.Default != nil {
			param.Default = paramSchema.Default
		}
		setParamMaximum(&param, paramSchema)
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

	return body, requestContentType, false, requestBody.Required, false
}

// isJSONContentType reports whether ct is a JSON-shaped media type:
// application/json, any */*+json variant (e.g. application/vnd.api+json),
// or text/json. Multipart and form-urlencoded encodings are excluded so
// the --body-json fallback only fires when the runtime is wired through
// the JSON branch of the command template.
func isJSONContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if ct == "" {
		return false
	}
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	if ct == "application/json" || ct == "text/json" {
		return true
	}
	return strings.HasPrefix(ct, "application/") && strings.HasSuffix(ct, "+json")
}

func requestBodyMediaType(content openapi3.Content) (string, *openapi3.MediaType) {
	if content == nil {
		return "", nil
	}
	if media := content.Get("application/json"); media != nil {
		return "application/json", media
	}

	contentTypes := sortedContentTypes(content)
	for _, contentType := range contentTypes {
		if strings.Contains(strings.ToLower(contentType), "json") {
			return contentType, content[contentType]
		}
	}
	for _, contentType := range contentTypes {
		if strings.EqualFold(contentType, "multipart/form-data") {
			return contentType, content[contentType]
		}
	}
	for _, contentType := range contentTypes {
		media := content[contentType]
		if media != nil && media.Schema != nil {
			return contentType, media
		}
	}

	return "", nil
}

func bodyParamSchema(schema *openapi3.Schema) *openapi3.Schema {
	if schema == nil || len(schema.AllOf) == 0 {
		return schema
	}

	if scalar, hasObject := firstAllOfNonObjectSchema(schema, map[*openapi3.Schema]struct{}{}); scalar != nil && !hasObject {
		return scalar
	}

	properties := map[string]*openapi3.SchemaRef{}
	required := map[string]struct{}{}
	if collectAllOfProperties(&openapi3.SchemaRef{Value: schema}, properties, required, map[*openapi3.Schema]struct{}{}) || len(properties) == 0 {
		return schema
	}

	merged := &openapi3.Schema{
		Type:        &openapi3.Types{openapi3.TypeObject},
		Properties:  properties,
		Description: schema.Description,
		Default:     schema.Default,
	}
	for name := range required {
		merged.Required = append(merged.Required, name)
	}
	sort.Strings(merged.Required)
	return merged
}

func firstAllOfNonObjectSchema(schema *openapi3.Schema, visited map[*openapi3.Schema]struct{}) (*openapi3.Schema, bool) {
	if schema == nil {
		return nil, false
	}
	if _, ok := visited[schema]; ok {
		return nil, false
	}
	visited[schema] = struct{}{}
	defer delete(visited, schema)

	hasObject := hasDirectObjectShape(schema)
	var firstScalar *openapi3.Schema
	for _, candidate := range schema.AllOf {
		value := schemaRefValue(candidate)
		if value == nil {
			continue
		}
		if hasDirectObjectShape(value) {
			hasObject = true
		} else if firstScalar == nil && (value.Items != nil || (value.Type != nil && !value.Type.Includes(openapi3.TypeObject))) {
			firstScalar = value
		}
		nestedScalar, nestedHasObject := firstAllOfNonObjectSchema(value, visited)
		if firstScalar == nil && nestedScalar != nil {
			firstScalar = nestedScalar
		}
		if nestedHasObject {
			hasObject = true
		}
	}
	return firstScalar, hasObject
}

func hasDirectObjectShape(schema *openapi3.Schema) bool {
	if schema == nil {
		return false
	}
	if schema.Type != nil && schema.Type.Includes(openapi3.TypeObject) {
		return true
	}
	return len(schema.Properties) > 0
}

func mapBodyFields(schema *openapi3.Schema, inferCSVArrays bool) []spec.Param {
	return mapBodyFieldsDepth(schema, inferCSVArrays, map[*openapi3.Schema]struct{}{}, 0)
}

const maxBodyFieldDepth = 8

func mapBodyFieldsDepth(schema *openapi3.Schema, inferCSVArrays bool, visited map[*openapi3.Schema]struct{}, depth int) []spec.Param {
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
			Type:        mapBodyParamType(fieldSchema, inferCSVArrays),
			Required:    isRequired(required, name),
			Description: description,
			Fields:      mapBodyFieldsDepth(fieldSchema, inferCSVArrays, visited, depth+1),
			Enum:        schemaEnum(fieldSchema),
			Format:      schemaFormat(fieldSchema),
		})
		if schemaHasCompositeUnionAlternative(fieldSchema, map[*openapi3.Schema]struct{}{}) {
			fields[len(fields)-1].Type = "string"
			fields[len(fields)-1].Format = "json_or_scalar"
		}
		if inferCSVArrays && isStringArraySchema(fieldSchema) {
			fields[len(fields)-1].ItemType = "string"
		}
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

	for _, sub := range schema.AllOf {
		if collectAllOfProperties(sub, properties, required, visited) {
			return true
		}
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

	return false
}

func mapResponse(op *openapi3.Operation, fallbackName string, out *spec.APISpec) (spec.ResponseDef, string) {
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
			itemFallback := fallbackName + "Item"
			registerInlineSchemaType(out, itemRef, itemFallback)
			return spec.ResponseDef{
				Type:          "array",
				Item:          schemaTypeName(itemRef, itemFallback),
				Discriminator: mapResponseDiscriminator(itemRef),
			}, "data"
		}
		if itemRef, path := singleArrayPropertyRef(schema); itemRef != nil {
			itemFallback := fallbackName + "Item"
			registerInlineSchemaType(out, itemRef, itemFallback)
			return spec.ResponseDef{
				Type:          "array",
				Item:          schemaTypeName(itemRef, itemFallback),
				Discriminator: mapResponseDiscriminator(itemRef),
			}, path
		}
	}

	if isArraySchema(schema) {
		itemFallback := fallbackName + "Item"
		registerInlineSchemaType(out, schema.Items, itemFallback)
		return spec.ResponseDef{
			Type:          "array",
			Item:          schemaTypeName(schema.Items, itemFallback),
			Discriminator: mapResponseDiscriminator(schema.Items),
		}, ""
	}

	if isObjectSchema(schema) {
		objFallback := fallbackName + "Response"
		registerInlineSchemaType(out, schemaRef, objFallback)
		return spec.ResponseDef{
			Type:          "object",
			Item:          schemaTypeName(schemaRef, objFallback),
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

func responseUsesBinary(op *openapi3.Operation) bool {
	if op == nil || op.Responses == nil {
		return false
	}
	success := selectSuccessResponse(op.Responses)
	if success == nil || success.Value == nil || success.Value.Content == nil {
		return false
	}
	for _, contentType := range sortedContentTypes(success.Value.Content) {
		media := success.Value.Content[contentType]
		if media == nil {
			continue
		}
		if schema := schemaRefValue(media.Schema); schema != nil {
			if schema.Type != nil && schema.Type.Includes(openapi3.TypeString) && strings.EqualFold(schemaFormat(schema), "binary") {
				return true
			}
		}
		if binaryContentType(contentType) {
			return true
		}
	}
	return false
}

// responseUsesXML reports whether the operation's success response is
// XML-only. It returns true when at least one declared success media type is
// XML (application/xml, text/xml, or a *+xml suffix other than xhtml) and none
// is JSON. A mixed JSON+XML response keeps the JSON default, so only genuinely
// XML-only endpoints opt into the xml response_format normalization path.
func responseUsesXML(op *openapi3.Operation) bool {
	if op == nil || op.Responses == nil {
		return false
	}
	success := selectSuccessResponse(op.Responses)
	if success == nil || success.Value == nil || len(success.Value.Content) == 0 {
		return false
	}
	sawXML := false
	for ct := range success.Value.Content {
		base := strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
		switch {
		case base == "application/json", base == "text/json", strings.HasSuffix(base, "+json"):
			return false
		case xmlResponseContentType(base):
			sawXML = true
		}
	}
	return sawXML
}

// xmlResponseContentType reports whether a media type is XML for response
// normalization. application/xhtml+xml is intentionally excluded: it is
// HTML-shaped and belongs to the html response path, not XML normalization.
func xmlResponseContentType(base string) bool {
	if base == "" || base == "application/xhtml+xml" {
		return false
	}
	return base == "application/xml" || base == "text/xml" || strings.HasSuffix(base, "+xml")
}

func binaryContentType(contentType string) bool {
	base := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if base == "" {
		return false
	}
	if strings.HasPrefix(base, "audio/") ||
		strings.HasPrefix(base, "video/") ||
		strings.HasPrefix(base, "image/") {
		return true
	}
	switch base {
	case "application/octet-stream", "application/zip", "application/x-zip", "application/x-zip-compressed", "application/pdf", "multipart/mixed":
		return true
	default:
		return false
	}
}

// binaryResponseAcceptType inspects the operation's selected success response.
// When every declared media type is concrete and binary-enveloped by the
// generated client, the server answers the client's default Accept:
// application/json with HTTP 406. It returns the media type the client must
// send instead (application/octet-stream when the response offers it, otherwise
// the lexicographically-first concrete type). Returns "" for JSON, wildcard,
// text, XML, empty, or mixed responses so the existing application/json default
// and non-binary response path stay untouched — the vast majority of endpoints.
func binaryResponseAcceptType(op *openapi3.Operation) string {
	if op == nil || op.Responses == nil {
		return ""
	}
	success := selectSuccessResponse(op.Responses)
	if success == nil || success.Value == nil || len(success.Value.Content) == 0 {
		return ""
	}
	concrete := make([]string, 0, len(success.Value.Content))
	for ct := range success.Value.Content {
		mt := strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
		if !binaryAcceptContentType(mt) {
			return ""
		}
		concrete = append(concrete, mt)
	}
	if len(concrete) == 0 {
		return ""
	}
	sort.Strings(concrete)
	for _, mt := range concrete {
		if mt == "application/octet-stream" {
			return mt
		}
	}
	return concrete[0]
}

func binaryAcceptContentType(mt string) bool {
	if mt == "" {
		return false
	}
	switch {
	case mt == "application/json", mt == "text/json", mt == "*/*":
		return false
	case strings.HasPrefix(mt, "text/"):
		return false
	case strings.HasSuffix(mt, "+json"), strings.HasSuffix(mt, "+xml"):
		return false
	case mt == "application/xml", mt == "application/xhtml+xml":
		return false
	case mt == "application/javascript", mt == "application/ecmascript",
		mt == "application/x-www-form-urlencoded", mt == "application/graphql":
		return false
	}
	return true
}

// upsertHeaderOverride returns headers with name set to value: replacing an
// existing case-insensitive match in place, or appending a new entry. Keeps a
// binary-response Accept override stable when a later pass (applyHeaderOverrides)
// merges per-endpoint configured headers onto the same endpoint.
func upsertHeaderOverride(headers []spec.RequiredHeader, name, value string) []spec.RequiredHeader {
	for i := range headers {
		if strings.EqualFold(headers[i].Name, name) {
			headers[i].Value = value
			return headers
		}
	}
	return append(headers, spec.RequiredHeader{Name: name, Value: value})
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

// readPathItemTenantScopeColumn reads `x-pp-tenant-scope-column` from a path
// item. String-only; other shapes warn and return "". Mirrors
// readPathItemResourceID.
func readPathItemTenantScopeColumn(pathItem *openapi3.PathItem, path string) string {
	if pathItem == nil || pathItem.Extensions == nil {
		return ""
	}
	raw, ok := pathItem.Extensions["x-pp-tenant-scope-column"]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		warnf("path %q: x-pp-tenant-scope-column must be a string, got %T; ignoring", path, raw)
		return ""
	}
}

// readPathItemMembershipField reads `x-pp-membership-field` from a path item.
// String-only; other shapes warn and return "". Mirrors
// readPathItemTenantScopeColumn.
func readPathItemMembershipField(pathItem *openapi3.PathItem, path string) string {
	if pathItem == nil || pathItem.Extensions == nil {
		return ""
	}
	raw, ok := pathItem.Extensions["x-pp-membership-field"]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		warnf("path %q: x-pp-membership-field must be a string, got %T; ignoring", path, raw)
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

func readRequiresRoleExtension(extensions map[string]any, context string) (string, error) {
	if extensions == nil {
		return "", nil
	}
	raw, ok := extensions[extensionRequiresRole]
	if !ok {
		return "", nil
	}
	role, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s: %s must be a string, got %T", context, extensionRequiresRole, raw)
	}
	return strings.TrimSpace(role), nil
}

func readDataSourceStrategyExtension(extensions map[string]any, context string) string {
	if extensions == nil {
		return ""
	}
	raw, ok := extensions[extensionDataSourceStrategy]
	if !ok || raw == nil {
		return ""
	}
	strategy, ok := raw.(string)
	if !ok {
		warnf("%s: %s must be a string, got %T; ignoring", context, extensionDataSourceStrategy, raw)
		return ""
	}
	strategy = strings.ToLower(strings.TrimSpace(strategy))
	switch strategy {
	case spec.DataSourceStrategyAuto, spec.DataSourceStrategyLocal, spec.DataSourceStrategyLive:
		return strategy
	case "":
		return ""
	default:
		warnf("%s: %s must be one of auto, local, live, got %q; ignoring", context, extensionDataSourceStrategy, strategy)
		return ""
	}
}

func readHappyArgsExtension(extensions map[string]any, context string) string {
	if extensions == nil {
		return ""
	}
	raw, ok := extensions[extensionHappyArgs]
	if !ok || raw == nil {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		warnf("%s: %s must be a string, got %T; ignoring", context, extensionHappyArgs, raw)
		return ""
	}
	return strings.TrimSpace(value)
}

// readExampleExtension reads an operation-level x-pp-example string and returns
// it as the verbatim Cobra Example for the generated command, overriding the
// synthesized example. It exists for all-optional "one-of" endpoints (pass one
// of channelId / handle / url): the synthesized example only includes required
// params, so such endpoints would otherwise advertise a bare command the API
// rejects with a 4xx. Marking a param required to force it into the example
// would wrongly make the CLI flag mandatory; x-pp-example fixes only the
// example. Absent (the common case) returns "", preserving synthesis exactly.
func readExampleExtension(extensions map[string]any, context string) string {
	if extensions == nil {
		return ""
	}
	raw, ok := extensions[extensionPPExample]
	if !ok || raw == nil {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		warnf("%s: %s must be a string, got %T; ignoring", context, extensionPPExample, raw)
		return ""
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	// Endpoint.Example is emitted as the verbatim Cobra Example line, and
	// synthesized examples carry a leading two-space indent. Normalize the
	// authored value to the same indent so x-pp-example renders identically
	// whether or not the author included the leading spaces.
	lines := strings.Split(trimmed, "\n")
	for i, line := range lines {
		lines[i] = "  " + strings.TrimLeft(line, " ")
	}
	return strings.Join(lines, "\n")
}

func readLiveDogfoodTierExtension(extensions map[string]any, context string) string {
	if extensions == nil {
		return ""
	}
	raw, ok := extensions[extensionLiveDogfoodTier]
	if !ok || raw == nil {
		return ""
	}
	tier, ok := raw.(string)
	if !ok {
		warnf("%s: %s must be a string, got %T; ignoring", context, extensionLiveDogfoodTier, raw)
		return ""
	}
	return strings.TrimSpace(tier)
}

func inferLiveDogfoodTier(method, path string, op *openapi3.Operation) string {
	if !strings.EqualFold(method, "GET") {
		return ""
	}
	if hasEventStreamResponse(op) || hasTerminalStreamPathSegment(path) {
		return "streaming"
	}
	return ""
}

func hasEventStreamResponse(op *openapi3.Operation) bool {
	if op == nil || op.Responses == nil {
		return false
	}
	success := selectSuccessResponse(op.Responses)
	if success == nil || success.Value == nil {
		return false
	}
	for contentType := range success.Value.Content {
		if strings.EqualFold(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]), "text/event-stream") {
			return true
		}
	}
	return false
}

func hasTerminalStreamPathSegment(path string) bool {
	path = strings.TrimRight(strings.TrimSpace(path), "/")
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash >= 0 {
		path = path[lastSlash+1:]
	}
	if strings.HasPrefix(path, "{") || strings.HasPrefix(path, ":") {
		return false
	}
	return strings.EqualFold(path, "stream")
}

func readPaginationExtension(extensions map[string]any, context string) *spec.Pagination {
	if extensions == nil {
		return nil
	}
	raw, ok := extensions[extensionPPPagination]
	if !ok || raw == nil {
		return nil
	}
	value, ok := raw.(string)
	if !ok {
		warnf("%s: %s must be a string, got %T; ignoring", context, extensionPPPagination, raw)
		return nil
	}
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return nil
	}
	if value == spec.PaginationTypeNone {
		return &spec.Pagination{Type: spec.PaginationTypeNone}
	}
	warnf("%s: %s=%q is not supported; ignoring", context, extensionPPPagination, value)
	return nil
}

// readWalkerExtension reads the `x-pp-sync-walker` extension from an
// operation's Extensions map and returns a parsed WalkerConfig. The raw
// extension value is expected to be a JSON/YAML object with `parent`,
// `key_field`, and `key_param` keys; missing keys default per the
// WalkerConfig field doc. Returns nil when the extension is absent.
// Malformed values warn and return nil rather than failing the whole parse.
func readWalkerExtension(extensions map[string]any, context string) *spec.WalkerConfig {
	if extensions == nil {
		return nil
	}
	raw, ok := extensions[extensionSyncWalker]
	if !ok {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		warnf("%s: %s: marshaling: %v; ignoring", context, extensionSyncWalker, err)
		return nil
	}
	var cfg spec.WalkerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		warnf("%s: %s: parsing: %v; ignoring", context, extensionSyncWalker, err)
		return nil
	}
	if strings.TrimSpace(cfg.Parent) == "" {
		warnf("%s: %s: parent is required; ignoring", context, extensionSyncWalker)
		return nil
	}
	return &cfg
}

// Member paths can provide a stronger primary-key signal than generic response
// fallbacks: /sites/{siteId} points at siteId even when the schema also has a
// display name. Prefer placeholders after the endpoint's target resource
// segment, with a trailing member placeholder fallback for nested resources
// whose generated name includes more context than one path segment.
func resolveIDFieldFromPathParam(op *openapi3.Operation, path string, resourceName string) string {
	itemSchema := responseItemSchema(op)
	if itemSchema == nil || resourceName == "" {
		return ""
	}

	segments := strings.Split(strings.Trim(path, "/"), "/")
	for i := 0; i < len(segments)-1; i++ {
		if !pathSegmentMatchesResource(segments[i], resourceName) {
			continue
		}
		placeholder, ok := pathPlaceholderName(segments[i+1])
		if !ok {
			continue
		}
		if field := responsePropertyForPathParam(itemSchema, placeholder); field != "" {
			return field
		}
	}
	if placeholder, ok := pathPlaceholderName(segments[len(segments)-1]); ok {
		if field := responsePropertyForPathParam(itemSchema, placeholder); field != "" {
			return field
		}
	}
	return ""
}

func pathSegmentMatchesResource(segment string, resourceName string) bool {
	segmentSnake := singularizeIdentifier(toSnakeCase(strings.Trim(segment, "/")))
	resourceSnake := singularizeIdentifier(toSnakeCase(resourceName))
	return segmentSnake != "" && segmentSnake == resourceSnake
}

func pathPlaceholderName(segment string) (string, bool) {
	segment = strings.TrimSpace(segment)
	if !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}"))
	return name, name != ""
}

func responsePropertyForPathParam(schema *openapi3.Schema, paramName string) string {
	if schema == nil || paramName == "" {
		return ""
	}
	if propRef, ok := schema.Properties[paramName]; ok && propRef != nil && isPlausibleIDFieldSchema(schemaRefValue(propRef)) {
		return paramName
	}
	paramSnake := toSnakeCase(paramName)
	propNames := make([]string, 0, len(schema.Properties))
	for propName := range schema.Properties {
		propNames = append(propNames, propName)
	}
	sort.Strings(propNames)
	for _, propName := range propNames {
		if toSnakeCase(propName) != paramSnake {
			continue
		}
		if propRef := schema.Properties[propName]; propRef != nil && isPlausibleIDFieldSchema(schemaRefValue(propRef)) {
			return propName
		}
	}
	return ""
}

func responseItemSchema(op *openapi3.Operation) *openapi3.Schema {
	if op == nil || op.Responses == nil {
		return nil
	}
	success := selectSuccessResponse(op.Responses)
	if success == nil || success.Value == nil {
		return nil
	}
	schemaRef := selectResponseSchema(success.Value)
	if schemaRef == nil || schemaRef.Value == nil {
		return nil
	}
	return unwrapItemSchema(schemaRef.Value)
}

// resolveIDFieldFromResponseSchema implements tiers 2-5 of the IDField fallback
// chain: prefer "id", then a resource-prefixed key (`<singular>_id` /
// `_uuid` / `_guid`), then a vendor identifier (`gid` / `sid` / `uid` /
// `uuid` / `guid`), then "name", then the first scalar field listed in the
// response schema's `required:` array (walking properties in their schema order).
// Returns "" when no field qualifies; templates fall through to runtime list
// scanning. Tier 1 (`x-resource-id` extension) is handled separately by the
// caller — it overrides every tier here.
//
// resourceName is the parser's targetResourceName for this operation; it drives
// the resource-prefixed heuristic and may be empty for synthetic endpoints,
// in which case that tier is skipped.
func resolveIDFieldFromResponseSchema(op *openapi3.Operation, resourceName string) string {
	itemSchema := responseItemSchema(op)
	if itemSchema == nil {
		return ""
	}
	itemFields := collectIDSchemaFields(&openapi3.SchemaRef{Value: itemSchema})
	itemSchema = &openapi3.Schema{
		Properties: itemFields.properties,
		Required:   itemFields.required,
	}

	// Tier 2: explicit `id` (required or optional)
	if _, ok := itemSchema.Properties["id"]; ok {
		return "id"
	}

	// Tier 3: resource-prefixed key. Catches APIs whose item schemas key off
	// `<singular>_id` instead of a bare `id` (e.g. podscan's Category with
	// `category_id`/`category_name`). Both the resource name and each property
	// name are normalized to snake_case, so `categoryId`/`category_id` and
	// `auth-tokens`/`auth_token_id` match through the same comparison.
	if id := resourcePrefixedIDField(itemSchema, resourceName); id != "" {
		return id
	}

	// Tier 3.5: vendor-specific identifier names. Asana keys every resource
	// on `gid`, Twilio on `sid`, others on `uid`/`uuid`/`guid`. These are
	// scalar primary keys by convention; without this tier the heuristic
	// falls through to Tier 4 and picks `name` (a display field), so the
	// generated CLI upserts on names and sync paths like
	// `/workspaces/<workspace>/users` get a name where the API expects a gid.
	for _, key := range []string{"gid", "sid", "uid", "uuid", "guid"} {
		if _, ok := itemSchema.Properties[key]; ok {
			return key
		}
	}

	// Tier 4: explicit `name`
	if _, ok := itemSchema.Properties["name"]; ok {
		return "name"
	}

	// Tier 5: first plausible-PK scalar field appearing in the schema's
	// required[] array, matched against properties in their schema-declared
	// order. kin-openapi preserves YAML/JSON property order in MapKeys/Extensions
	// but not via range over Properties (it's a Go map). Fall back to iterating
	// the required[] slice itself: that order is stable and is what spec authors
	// intend when they care about which field "wins."
	//
	// "Plausible-PK" excludes boolean, enum, and date/date-time fields even
	// though they are scalar — they are structurally low-cardinality or
	// non-identifier-shaped, so committing them as a runtime override
	// collapses unrelated rows onto the same PK during upsert.
	for _, fieldName := range itemSchema.Required {
		propRef, ok := itemSchema.Properties[fieldName]
		if !ok || propRef == nil || propRef.Value == nil {
			continue
		}
		if isPlausibleIDFieldSchema(propRef.Value) {
			return fieldName
		}
	}

	return ""
}

type idSchemaFields struct {
	properties map[string]*openapi3.SchemaRef
	required   []string
}

func collectIDSchemaFields(schemaRef *openapi3.SchemaRef) idSchemaFields {
	fields := idSchemaFields{properties: map[string]*openapi3.SchemaRef{}}
	seenRequired := map[string]struct{}{}
	collectIDSchemaFieldsInto(schemaRef, &fields, seenRequired, map[*openapi3.Schema]struct{}{})
	return fields
}

func collectIDSchemaFieldsInto(schemaRef *openapi3.SchemaRef, fields *idSchemaFields, seenRequired map[string]struct{}, visited map[*openapi3.Schema]struct{}) {
	if schemaRef == nil || fields == nil {
		return
	}
	schema := schemaRefValue(schemaRef)
	if schema == nil {
		return
	}
	if _, ok := visited[schema]; ok {
		return
	}
	visited[schema] = struct{}{}

	for name, prop := range schema.Properties {
		if prop == nil {
			continue
		}
		if _, exists := fields.properties[name]; !exists {
			fields.properties[name] = prop
		}
	}
	for _, name := range schema.Required {
		if _, exists := seenRequired[name]; exists {
			continue
		}
		seenRequired[name] = struct{}{}
		fields.required = append(fields.required, name)
	}
	for _, sub := range schema.AllOf {
		collectIDSchemaFieldsInto(sub, fields, seenRequired, visited)
	}
	for _, sub := range schema.OneOf {
		collectIDSchemaFieldsInto(sub, fields, seenRequired, visited)
	}
	for _, sub := range schema.AnyOf {
		collectIDSchemaFieldsInto(sub, fields, seenRequired, visited)
	}
}

// resourcePrefixedIDField returns the first property whose snake-cased name
// matches `<singular_resource>_id`, then `_uuid`, then `_guid`. Returns "" when
// the resource name is empty or no property matches. Property names are
// returned verbatim so callers preserve the spec's original casing (e.g.
// `categoryId` rather than `category_id`).
func resourcePrefixedIDField(schema *openapi3.Schema, resourceName string) string {
	singular := singularizeIdentifier(toSnakeCase(resourceName))
	if singular == "" {
		return ""
	}
	// Sort property names so behavior is deterministic across Go map
	// iteration order; this only matters when a schema declares multiple
	// keys that snake-case to the same target, which is unusual.
	propNames := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		propNames = append(propNames, name)
	}
	sort.Strings(propNames)
	for _, suffix := range []string{"_id", "_uuid", "_guid"} {
		target := singular + suffix
		for _, propName := range propNames {
			if toSnakeCase(propName) == target {
				return propName
			}
		}
	}
	return ""
}

// singularizeIdentifier returns a simple singular form of a snake-cased
// identifier. Mirrors spec.singularize for parser-local use without exporting
// the spec helper. Only the trailing token is singularized so multi-word
// identifiers like `auth_tokens` become `auth_token`.
func singularizeIdentifier(s string) string {
	if s == "" {
		return ""
	}
	idx := strings.LastIndex(s, "_")
	prefix, last := "", s
	if idx >= 0 {
		prefix, last = s[:idx+1], s[idx+1:]
	}
	irregulars := map[string]string{
		"properties": "property",
		"companies":  "company",
		"categories": "category",
		"entries":    "entry",
		"statuses":   "status",
		"addresses":  "address",
		"analyses":   "analysis",
		// `<stem>+s` plurals on `ie`/`ix`-ending stems that the generic
		// `ies → y` rule would otherwise mangle (`movies → movy`).
		"movies":   "movie",
		"series":   "series",
		"matrices": "matrix",
		"indices":  "index",
		"vertices": "vertex",
	}
	if singular, ok := irregulars[last]; ok {
		return prefix + singular
	}
	switch {
	case strings.HasSuffix(last, "ies") && len(last) > 3:
		return prefix + last[:len(last)-3] + "y"
	case strings.HasSuffix(last, "ses"), strings.HasSuffix(last, "xes"), strings.HasSuffix(last, "zes"):
		return prefix + last[:len(last)-2]
	case strings.HasSuffix(last, "s") && !strings.HasSuffix(last, "ss") && len(last) > 1:
		return prefix + last[:len(last)-1]
	}
	return prefix + last
}

// unwrapItemSchema returns the schema of items inside a list response. Handles
// four shapes: bare array (return Items.Value), {data: [...]} wrapper (fast
// path; mirrors mapResponse's handling), single-named-array envelope where the
// resource is wrapped under its own key alongside scalar/object metadata
// siblings (e.g. Kalshi's {events: [...], cursor: "..."}), and bare object
// (no array, treat as a single-item resource and inspect its fields).
//
// The single-named-array envelope detection is deliberately strict: it
// requires *exactly one* array-typed property at the top level. Multi-array
// envelopes (e.g. /portfolio/positions returning event_positions and
// market_positions) fall through to the bare-object path; they need explicit
// per-resource declaration since picking one would lose data.
func unwrapItemSchema(schema *openapi3.Schema) *openapi3.Schema {
	if schema == nil {
		return nil
	}
	if isArraySchema(schema) && schema.Items != nil {
		return schemaRefValue(schema.Items)
	}
	if !isObjectSchema(schema) {
		return nil
	}
	// {data: [...]} wrapper convention — fast path, preserved verbatim.
	if dataRef, ok := schema.Properties["data"]; ok {
		if data := schemaRefValue(dataRef); isArraySchema(data) && data.Items != nil {
			return schemaRefValue(data.Items)
		}
	}
	// Single-named-array envelope: object whose only array-typed property is
	// the resource list, with the rest being pagination/metadata fields. This
	// catches API patterns like {events: [...], cursor: "..."} where the
	// wrapper key matches the resource name. Without this, the PK profiler
	// would walk the wrapper itself and pick a scalar sibling (cursor,
	// has_more) as the resource ID.
	if itemRef, _ := singleArrayPropertyRef(schema); itemRef != nil {
		return schemaRefValue(itemRef)
	}
	return schema
}

// singleArrayPropertyRef returns the items schema ref and property name of an
// object's sole array-typed property, or nil if zero or multiple array
// properties exist.
// Non-array siblings (scalars, objects) are ignored — they're typically
// pagination metadata.
func singleArrayPropertyRef(schema *openapi3.Schema) (*openapi3.SchemaRef, string) {
	var items *openapi3.SchemaRef
	var name string
	count := 0
	for propName, propRef := range schema.Properties {
		prop := schemaRefValue(propRef)
		if !isArraySchema(prop) || prop.Items == nil {
			continue
		}
		count++
		if count > 1 {
			return nil, ""
		}
		name = propName
		items = prop.Items
	}
	if count == 1 {
		return items, name
	}
	return nil, ""
}

// isScalarSchema reports whether the schema's type is a scalar — string,
// integer, number, or boolean. Excludes objects, arrays, and refs that resolve
// to either.
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

// isPlausibleIDFieldSchema is the tier-5 PK predicate: a scalar that is also
// not boolean, not enum-restricted, and not date/date-time formatted. Booleans
// have cardinality 2 (true/false), enums have hand-picked low cardinality, and
// date/date-time fields are timestamps — none can serve as a primary key
// without collapsing distinct rows during upsert. See profiler.resourceIDFieldOverrides
// and store.UpsertBatch.
func isPlausibleIDFieldSchema(schema *openapi3.Schema) bool {
	if !isScalarSchema(schema) {
		return false
	}
	if schema.Type.Includes(openapi3.TypeBoolean) {
		return false
	}
	if len(schema.Enum) > 0 {
		return false
	}
	format := strings.ToLower(schema.Format)
	if format == "date" || format == "date-time" {
		return false
	}
	return true
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

		out.Types[goName] = spec.TypeDef{Fields: buildTypeFields(schemaRef)}
	}
}

// buildTypeFields walks an object schema's properties (flattening JSON:API
// resource shapes the same way mapTypes/mapResponse do elsewhere) and
// returns the spec.TypeField list used by both Components.Schemas type
// registration and inline-response item registration. Underscore-prefixed
// property names and Go-name collisions are filtered out so the same field
// set drives generated Go structs and SQLite column derivation.
func buildTypeFields(schemaRef *openapi3.SchemaRef) []spec.TypeField {
	schema := schemaRefValue(schemaRef)
	if schema == nil {
		return nil
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
			Name:   fieldName,
			Type:   mapSchemaType(fieldSchema),
			Enum:   schemaEnum(fieldSchema),
			Format: schemaFormat(fieldSchema),
		})
	}
	return fields
}

// registerInlineSchemaType registers an inline response schema (item type
// for list responses or full object type for single-object responses) into
// out.Types under the synthetic name mapResponse uses for
// endpoint.Response.Item. $ref-shaped schemas already land in Types via
// mapTypes; this only fires when the schema is inline and the slot is
// empty. No-op when out is nil (test-only callers).
func registerInlineSchemaType(out *spec.APISpec, itemRef *openapi3.SchemaRef, fallbackName string) {
	if out == nil || itemRef == nil || refComponentName(itemRef.Ref) != "" {
		return
	}
	itemSchema := schemaRefValue(itemRef)
	if itemSchema == nil {
		return
	}
	fields := buildTypeFields(itemRef)
	if len(fields) == 0 {
		return
	}
	typeName := schemaTypeName(itemRef, fallbackName)
	if typeName == "" {
		return
	}
	if out.Types == nil {
		out.Types = map[string]spec.TypeDef{}
	}
	if _, exists := out.Types[typeName]; exists {
		return
	}
	out.Types[typeName] = spec.TypeDef{Fields: fields}
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
	for _, sub := range schema.OneOf {
		collectTypeProperties(sub, properties, visited)
	}
	for _, sub := range schema.AnyOf {
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

func mapBodyParamType(schema *openapi3.Schema, inferCSVArrays bool) string {
	if inferCSVArrays && isStringArraySchema(schema) {
		return "string_csv_array"
	}
	return mapSchemaType(schema)
}

func schemaHasCompositeUnionAlternative(schema *openapi3.Schema, visited map[*openapi3.Schema]struct{}) bool {
	if schema == nil {
		return false
	}
	if _, ok := visited[schema]; ok {
		return false
	}
	visited[schema] = struct{}{}
	defer delete(visited, schema)

	for _, candidate := range append(append([]*openapi3.SchemaRef{}, schema.OneOf...), schema.AnyOf...) {
		value := schemaRefValue(candidate)
		if isObjectSchema(value) || isArraySchema(value) {
			return true
		}
		if schemaHasCompositeUnionAlternative(value, visited) {
			return true
		}
	}
	return false
}

func isStringArraySchema(schema *openapi3.Schema) bool {
	if schema == nil || schema.Type == nil || !schema.Type.Includes(openapi3.TypeArray) {
		return false
	}
	itemSchema := schemaRefValue(schema.Items)
	return itemSchema != nil && itemSchema.Type != nil && itemSchema.Type.Includes(openapi3.TypeString)
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

func schemaExample(schema *openapi3.Schema) any {
	if schema == nil {
		return nil
	}
	if schema.Example != nil {
		return schema.Example
	}
	for _, example := range schema.Examples {
		if example != nil {
			return example
		}
	}
	return nil
}

func parameterExample(parameter *openapi3.Parameter, schema *openapi3.Schema) any {
	if parameter == nil {
		return schemaExample(schema)
	}
	if parameter.Example != nil {
		return parameter.Example
	}
	if len(parameter.Examples) > 0 {
		names := make([]string, 0, len(parameter.Examples))
		for name := range parameter.Examples {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			ref := parameter.Examples[name]
			if ref != nil && ref.Value != nil && ref.Value.Value != nil {
				return ref.Value.Value
			}
		}
	}
	return schemaExample(schema)
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

func sortedContentTypes(content openapi3.Content) []string {
	contentTypes := make([]string, 0, len(content))
	for contentType := range content {
		contentTypes = append(contentTypes, contentType)
	}
	sort.Strings(contentTypes)
	return contentTypes
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

func resourceAndSubForOperation(path, basePath string, commonPrefix []string, op *openapi3.Operation, googleDiscovery bool) (string, string) {
	if override := resourceOverrideForOperation(op, path); override != "" {
		return override, ""
	}
	primary, sub := resourceAndSubFromPath(path, basePath, commonPrefix)
	if primary != "" && !pathStartsWithCustomVerbParam(path, basePath, commonPrefix) {
		return primary, sub
	}
	if googleDiscovery {
		opPrimary, opSub := resourceAndSubFromGoogleOperationID(operationID(op), true)
		if opPrimary != "" {
			return opPrimary, opSub
		}
		if pathPrimary := resourceFromGooglePathAfterLeadingParams(path, basePath, commonPrefix); pathPrimary != "" {
			return pathPrimary, ""
		}
	}
	if opPrimary, opSub := resourceAndSubFromGoogleOperationID(operationID(op), false); opPrimary != "" {
		return opPrimary, opSub
	}
	return primary, sub
}

func resourceOverrideForOperation(op *openapi3.Operation, path string) string {
	if op == nil || op.Extensions == nil {
		return ""
	}
	raw, ok := op.Extensions[extensionPPResource]
	if !ok {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		warnf("path %q: %s must be a string, got %T; ignoring", path, extensionPPResource, raw)
		return ""
	}
	value = sanitizeResourceName(toSnakeCase(value))
	if value == "" {
		warnf("path %q: %s is empty after sanitization; ignoring", path, extensionPPResource)
		return ""
	}
	return value
}

func reservedTemplateCollisionRenames(doc *openapi3.T, pathKeys []string, basePath string, commonPrefix []string, googleDiscovery bool, out *spec.APISpec) (map[string]string, error) {
	type candidateSet map[string]struct{}
	candidates := map[string]candidateSet{}
	firstPath := map[string]string{}
	hardErrors := map[string]reservedTemplateCollision{}
	rawResources := map[string]struct{}{}

	for _, path := range pathKeys {
		pathItem := doc.Paths.Value(path)
		if pathItem == nil {
			continue
		}
		for _, op := range pathItem.Operations() {
			primaryName, _ := resourceAndSubForOperation(path, basePath, commonPrefix, op, googleDiscovery)
			if primaryName == "" {
				continue
			}
			rawResources[primaryName] = struct{}{}
			if primaryName == "auth" && out != nil && !out.ParseTimeReservedCobraUseName(primaryName) {
				continue
			}
			if _, reserved := spec.ReservedCLIResourceNames[primaryName]; !reserved {
				continue
			}
			if resourceOverrideForOperation(op, path) != "" {
				hardErrors[primaryName] = reservedTemplateCollision{Name: primaryName, FromOverride: true}
				continue
			}
			if candidate := spec.ReservedResourceParentPrefixCandidate(primaryName, path); candidate != "" {
				if _, ok := candidates[primaryName]; !ok {
					candidates[primaryName] = candidateSet{}
					firstPath[primaryName] = path
				}
				candidates[primaryName][candidate] = struct{}{}
				continue
			}
			if spec.ReservedResourcePathTerminatesAt(primaryName, path) {
				hardErrors[primaryName] = reservedTemplateCollision{Name: primaryName}
			}
		}
	}

	renames := map[string]string{}
	hardErrorCollisions := make([]reservedTemplateCollision, 0, len(hardErrors))
	for _, collision := range hardErrors {
		hardErrorCollisions = append(hardErrorCollisions, collision)
	}
	slices.SortFunc(hardErrorCollisions, func(a, b reservedTemplateCollision) int {
		return strings.Compare(a.Name, b.Name)
	})
	if len(hardErrorCollisions) > 0 {
		return nil, reservedTemplateCollisionErrors(hardErrorCollisions)
	}

	names := make([]string, 0, len(candidates))
	for name := range candidates {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		set := candidates[name]
		if len(set) != 1 {
			return nil, reservedTemplateCollisionError(name, name+"_resource")
		}
		var candidate string
		for value := range set {
			candidate = value
		}
		if _, reserved := spec.ReservedCLIResourceNames[candidate]; reserved {
			return nil, reservedTemplateCollisionError(name, name+"_resource")
		}
		if _, exists := rawResources[candidate]; exists {
			return nil, reservedTemplateCollisionError(name, name+"_resource")
		}
		renames[name] = candidate
		warnf("resource %q from path %q collides with reserved Printing Press template %q; renamed to %q", name, firstPath[name], name, candidate)
	}
	return renames, nil
}

type reservedTemplateCollision struct {
	Name         string
	Suggestion   string
	FromOverride bool
}

func reservedTemplateCollisionError(name, suggestion string) error {
	return reservedTemplateCollisionErrors([]reservedTemplateCollision{{
		Name:       name,
		Suggestion: suggestion,
	}})
}

func reservedTemplateCollisionErrors(collisions []reservedTemplateCollision) error {
	if len(collisions) == 1 {
		collision := collisions[0]
		suggestion := collision.Suggestion
		if suggestion == "" {
			suggestion = collision.Name + "_resource"
		}
		if collision.FromOverride {
			return fmt.Errorf("%s value %q collides with reserved Printing Press template %q (would overwrite internal/cli/%s.go and produce a duplicate `new%sCmd` function). Rename %s to %q",
				extensionPPResource, collision.Name, collision.Name, collision.Name, spec.SnakeToPascal(collision.Name), extensionPPResource, suggestion)
		}
		return fmt.Errorf("resource name %q collides with reserved Printing Press template %q (would overwrite internal/cli/%s.go and produce a duplicate `new%sCmd` function). Rename to %q in your spec, or set x-pp-resource on the operation",
			collision.Name, collision.Name, collision.Name, spec.SnakeToPascal(collision.Name), suggestion)
	}

	names := make([]string, 0, len(collisions))
	files := make([]string, 0, len(collisions))
	functions := make([]string, 0, len(collisions))
	suggestions := make([]string, 0, len(collisions))
	hasOverride := false
	hasDerived := false
	for _, collision := range collisions {
		suggestion := collision.Suggestion
		if suggestion == "" {
			suggestion = collision.Name + "_resource"
		}
		names = append(names, fmt.Sprintf("%q", collision.Name))
		files = append(files, "internal/cli/"+collision.Name+".go")
		functions = append(functions, "`new"+spec.SnakeToPascal(collision.Name)+"Cmd`")
		suggestions = append(suggestions, fmt.Sprintf("%q", suggestion))
		if collision.FromOverride {
			hasOverride = true
		} else {
			hasDerived = true
		}
	}

	remediation := fmt.Sprintf("Rename them in your spec, e.g. %s", strings.Join(suggestions, ", "))
	if hasOverride && !hasDerived {
		remediation = fmt.Sprintf("Rename each %s value, e.g. %s", extensionPPResource, strings.Join(suggestions, ", "))
	} else if hasOverride {
		remediation = fmt.Sprintf("Rename each resource or %s value, e.g. %s", extensionPPResource, strings.Join(suggestions, ", "))
	} else {
		remediation += ", or set x-pp-resource on each operation"
	}

	return fmt.Errorf("resource names %s collide with reserved Printing Press templates %s (would overwrite %s and produce duplicate %s functions). %s",
		strings.Join(names, ", "), strings.Join(names, ", "), strings.Join(files, ", "), strings.Join(functions, ", "), remediation)
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

func pathStartsWithCustomVerbParam(path, basePath string, commonPrefix []string) bool {
	segments := pathSegmentsAfterBase(path, basePath)
	if len(commonPrefix) > 0 && hasSegmentPrefix(segments, commonPrefix) {
		segments = segments[len(commonPrefix):]
	}
	return len(segments) > 0 && isPathParamCustomVerbSegment(segments[0])
}

func resourceFromGooglePathAfterLeadingParams(path, basePath string, commonPrefix []string) string {
	segments := pathSegmentsAfterBase(path, basePath)
	if len(commonPrefix) > 0 && hasSegmentPrefix(segments, commonPrefix) {
		segments = segments[len(commonPrefix):]
	}
	if len(segments) == 0 || !isPathParamSegment(segments[0]) {
		return ""
	}
	for _, segment := range segments {
		if isPathParamSegment(segment) {
			continue
		}
		if isPathParamCustomVerbSegment(segment) {
			continue
		}
		return sanitizeResourceName(strings.ReplaceAll(toSnakeCase(segment), "_", "-"))
	}
	return ""
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
		return versionSegmentPattern.MatchString(segment) || isVersionToken(segment)
	}
	return isVersionToken(segment)
}

func isPathParamSegment(segment string) bool {
	return strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}")
}

func isPathParamCustomVerbSegment(segment string) bool {
	close := strings.Index(segment, "}:")
	return strings.HasPrefix(segment, "{") && close > 0 && close+2 < len(segment)
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
	if name := googleOperationIDEndpointName(operationID, resourceName); name != "" {
		return name
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

// isFrameworkAutoGeneratedOperationID reports whether operationID was
// auto-generated by a framework (API Platform / Symfony, FastAPI, etc.) that
// names operations from path segments and the HTTP method. Two API Platform
// signals must both fire:
//
//  1. The operationId, snake-cased and tokenized on underscores, ends with
//     a framework verb suffix (_post, _put, _patch, _delete, _get_collection,
//     _get_subresource). Hand-curated ids that ship CRUD verbs as English
//     words (list, create, search) and Google Discovery dotted ids
//     (run.projects.locations.services.create) miss this gate.
//  2. The same token list contains a token equal to the concatenation
//     (no separator) of two consecutive non-routing, non-path-param path
//     segments. Path-param segments are excluded so a hand-curated id
//     that happens to contain a "{paramName}+nextSegment" concat does not
//     trip the detector.
//
// FastAPI's default operationId shape uses a different signal:
// <function_name>_<path_segments>_<method>. The path/method suffix is
// machine-generated, while the function-name prefix may be readable but often
// repeats the resource. When either detector fires, callers skip the raw
// operationIDToName path so name derivation avoids unreadable command names
// like "paymentsubscriptions-get-collection" or "list-api-get".
func isFrameworkAutoGeneratedOperationID(operationID, path string) bool {
	if operationID == "" || path == "" {
		return false
	}
	opTokens := operationIDTokens(operationID)
	if len(opTokens) < 2 {
		return false
	}
	if _, ok := fastAPIOperationIDPrefixTokens(opTokens, path); ok {
		return true
	}
	return isPathDerivedFrameworkOperationID(opTokens, path)
}

func isPathDerivedFrameworkOperationID(opTokens []string, path string) bool {
	if len(opTokens) < 2 || path == "" {
		return false
	}
	if !hasFrameworkVerbSuffix(opTokens) {
		return false
	}

	segs := splitPath(path)
	normSegs := make([]string, 0, len(segs))
	for _, s := range segs {
		if isVersionSegment(s) || isGenericAPIPrefix(s) || isPathParamSegment(s) {
			continue
		}
		n := strings.ReplaceAll(toSnakeCase(s), "_", "")
		if n == "" {
			continue
		}
		normSegs = append(normSegs, n)
	}
	if len(normSegs) < 2 {
		return false
	}

	// frameworkConcatMinLen guards against accidental matches on very short
	// segment pairs. 8 chars is comfortably above any plausible single
	// path segment and below every concatenation we have seen in the wild
	// (paymentsubscriptions, communitycommunities, schoolenrollments).
	const frameworkConcatMinLen = 8

	for i := range len(normSegs) - 1 {
		cand := normSegs[i] + normSegs[i+1]
		if len(cand) < frameworkConcatMinLen {
			continue
		}
		if slices.Contains(opTokens, cand) {
			return true
		}
	}
	return false
}

func fastAPIOperationIDEndpointName(opTokens []string, path, resourceName string, commonPrefix []string) (string, bool) {
	prefixTokens, ok := fastAPIOperationIDPrefixTokens(opTokens, path)
	if !ok {
		return "", false
	}
	if len(prefixTokens) == 0 {
		return "", true
	}
	name := operationIDToName(strings.Join(prefixTokens, "_"), resourceName, commonPrefix)
	methodToken := opTokens[len(opTokens)-1]
	switch prefixTokens[0] {
	case "create":
		if methodToken == "post" {
			return "", true
		}
	case "update":
		if methodToken == "put" || methodToken == "patch" {
			return "", true
		}
	case "delete":
		if methodToken == "delete" {
			return "", true
		}
	case "read":
		if methodToken == "get" {
			return "", true
		}
	}
	return name, true
}

func fastAPIOperationIDPrefixTokens(opTokens []string, path string) ([]string, bool) {
	if len(opTokens) < 3 || !isHTTPMethodToken(opTokens[len(opTokens)-1]) {
		return nil, false
	}

	pathTokens := make([]string, 0)
	for _, segment := range splitPath(path) {
		for token := range strings.SplitSeq(toSnakeCase(segment), "_") {
			if token != "" {
				pathTokens = append(pathTokens, token)
			}
		}
	}
	if len(pathTokens) == 0 {
		return nil, false
	}

	start := len(opTokens) - 1 - len(pathTokens)
	if start < 1 {
		return nil, false
	}
	for i, token := range pathTokens {
		if opTokens[start+i] != token {
			return nil, false
		}
	}
	return opTokens[:start], true
}

func isHTTPMethodToken(token string) bool {
	switch token {
	case "get", "post", "put", "patch", "delete", "head", "options":
		return true
	default:
		return false
	}
}

// hasFrameworkVerbSuffix reports whether opTokens ends with a verb suffix
// that frameworks like API Platform emit as a marker of auto-generation:
// the bare HTTP method (post/put/patch/delete) or one of the well-known
// composite suffixes (get_collection, get_subresource). Hand-curated ids
// ship English CRUD verbs (list, create, update) and do not match.
func hasFrameworkVerbSuffix(opTokens []string) bool {
	if len(opTokens) == 0 {
		return false
	}
	last := opTokens[len(opTokens)-1]
	switch last {
	case "post", "put", "patch", "delete":
		return true
	}
	if len(opTokens) >= 2 && opTokens[len(opTokens)-2] == "get" {
		switch last {
		case "collection", "subresource":
			return true
		}
	}
	return false
}

func googleOperationIDEndpointName(operationID, resourceName string) string {
	chain, verb, ok := googleOperationIDResourceChain(operationID, true, true)
	if !ok {
		return ""
	}
	resource := sanitizeResourceName(strings.ReplaceAll(toSnakeCase(resourceName), "_", "-"))
	if resource == "" {
		return ""
	}
	if slices.Contains(chain, resource) {
		return strings.ReplaceAll(toSnakeCase(verb), "_", "-")
	}
	return ""
}

func resourceAndSubFromGoogleOperationID(operationID string, allowUnscoped bool) (string, string) {
	chain, _, ok := googleOperationIDResourceChain(operationID, allowUnscoped, false)
	if !ok {
		return "", ""
	}
	switch len(chain) {
	case 0:
		return "", ""
	case 1:
		return chain[0], ""
	case 2:
		return chain[0], chain[1]
	default:
		return chain[len(chain)-2], chain[len(chain)-1]
	}
}

func googleOperationIDResourceChain(operationID string, allowUnscoped, fallbackWhenScopedEmpty bool) ([]string, string, bool) {
	parts := strings.Split(strings.TrimSpace(operationID), ".")
	if len(parts) < 3 {
		return nil, "", false
	}
	start, ok := googleOperationIDScopeStart(parts)
	if !ok && allowUnscoped {
		start = 1
		ok = true
	}
	if ok && allowUnscoped && fallbackWhenScopedEmpty && len(parts[start:]) < 2 {
		start = 1
	}
	if !ok || len(parts[start:]) < 2 {
		return nil, "", false
	}

	verb := parts[len(parts)-1]
	rawChain := parts[start : len(parts)-1]
	chain := make([]string, 0, len(rawChain))
	for _, segment := range rawChain {
		resource := sanitizeResourceName(strings.ReplaceAll(toSnakeCase(segment), "_", "-"))
		if resource == "" {
			return nil, "", false
		}
		chain = append(chain, resource)
	}
	return chain, verb, true
}

func googleOperationIDScopeStart(parts []string) (int, bool) {
	for i := range len(parts) {
		switch parts[i] {
		case "projects", "organizations", "folders":
			if i+1 < len(parts) && parts[i+1] == "locations" {
				return i + 2, true
			}
		case "billingAccounts":
			if i+1 < len(parts) && parts[i+1] == "locations" {
				return i + 2, true
			}
			return i + 1, true
		}
	}
	return 0, false
}

func isGoogleDiscoverySpec(doc *openapi3.T) bool {
	if raw, ok := lookupOpenAPIInfoExtension(doc, extensionProviderName); ok {
		if provider, ok := raw.(string); ok && strings.EqualFold(strings.TrimSpace(provider), "googleapis.com") {
			return true
		}
	}
	raw, ok := lookupOpenAPIInfoExtension(doc, extensionOrigin)
	if !ok {
		return false
	}
	origins, ok := raw.([]any)
	if !ok {
		return false
	}
	for _, origin := range origins {
		entry, ok := origin.(map[string]any)
		if !ok {
			continue
		}
		if format, ok := entry["format"].(string); ok && strings.EqualFold(strings.TrimSpace(format), "google") {
			return true
		}
		if originURL, ok := entry["url"].(string); ok && isGoogleDiscoveryOriginURL(originURL) {
			return true
		}
	}
	return false
}

func isGoogleDiscoveryOriginURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host != "googleapis.com" && !strings.HasSuffix(host, ".googleapis.com") {
		return false
	}
	return strings.HasPrefix(strings.TrimLeft(parsed.EscapedPath(), "/"), "$discovery/")
}

func hasGoogleAPIsServer(doc *openapi3.T) bool {
	if doc == nil {
		return false
	}
	for _, server := range doc.Servers {
		if server != nil && isGoogleAPIsServerURL(server.URL) {
			return true
		}
	}
	if doc.Paths == nil {
		return false
	}
	for _, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		for _, server := range pathItem.Servers {
			if server != nil && isGoogleAPIsServerURL(server.URL) {
				return true
			}
		}
		for _, op := range pathItem.Operations() {
			if op == nil || op.Servers == nil {
				continue
			}
			for _, server := range *op.Servers {
				if server != nil && isGoogleAPIsServerURL(server.URL) {
					return true
				}
			}
		}
	}
	return false
}

func isGoogleAPIsServerURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err == nil {
		if host := strings.ToLower(strings.TrimSpace(parsed.Hostname())); host == "googleapis.com" || strings.HasSuffix(host, ".googleapis.com") {
			return true
		}
	}
	return false
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

var versionSegmentPattern = regexp.MustCompile(`^v[0-9]+((alpha|beta|p[0-9]+)[0-9]*)*$`)

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
	// Map lowercase parameter name back to the spec's original casing so
	// the detected LimitParam/CursorParam preserves whatever the API
	// expects (e.g. Google APIs reject `pagesize` but accept `pageSize`).
	originalCase := map[string]string{}
	for _, p := range params {
		originalCase[strings.ToLower(p.Name)] = p.Name
	}

	var pag spec.Pagination

	// Detect limit param
	for _, name := range []string{"limit", "maxresults", "pagesize", "page_size", "max_results", "perpage", "per_page", "page[size]"} {
		if orig, ok := originalCase[name]; ok {
			pag.LimitParam = orig
			break
		}
	}

	// Detect cursor param and pagination type
	for _, name := range []string{"pagetoken", "page_token"} {
		if orig, ok := originalCase[name]; ok {
			pag.CursorParam = orig
			pag.Type = "page_token"
			pag.NextCursorPath = "nextPageToken"
			break
		}
	}
	if pag.Type == "" {
		for _, name := range []string{"after", "cursor", "page[cursor]", "nexttoken", "next_token"} {
			if orig, ok := originalCase[name]; ok {
				pag.CursorParam = orig
				pag.Type = "cursor"
				break
			}
		}
	}
	if pag.Type == "" {
		for _, name := range []string{"offset", "skip"} {
			if orig, ok := originalCase[name]; ok {
				pag.CursorParam = orig
				pag.Type = "offset"
				break
			}
		}
	}
	// Integer page paginator: classify only after the cursor/token/offset
	// branches above so APIs that mix forms (e.g. cursor + page) keep
	// cursor semantics. Runtime sync handles type "page" by incrementing
	// CursorParam as an integer when no body cursor is extracted.
	if pag.Type == "" {
		for _, name := range []string{"page", "pagenumber", "page_number", "page[number]"} {
			if orig, ok := originalCase[name]; ok {
				pag.CursorParam = orig
				pag.Type = "page"
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
				detectPaginationResponseFields(schemaRef.Value, "", &pag)
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

func detectPostQueryIDWalkPagination(body []spec.Param, op *openapi3.Operation, idField string) *spec.Pagination {
	limitParam := ""
	hasFilter := false
	for _, param := range body {
		lower := strings.ToLower(param.Name)
		if isPostQueryIDWalkLimitParam(lower) && limitParam == "" {
			limitParam = param.Name
		}
		if (lower == "filter" || lower == "filters") && param.Type == "array" {
			hasFilter = true
		}
	}
	if limitParam == "" || !hasFilter {
		return nil
	}
	if !responseHasPageDetailsNextPageURL(op) || !responseItemHasNumericID(op, idField) {
		return nil
	}
	return &spec.Pagination{
		Type:       spec.PaginationTypeIDWalk,
		LimitParam: limitParam,
	}
}

func isPostQueryIDWalkLimitParam(lower string) bool {
	switch lower {
	case "maxrecords", "max_records":
		return true
	default:
		return false
	}
}

func responseHasPageDetailsNextPageURL(op *openapi3.Operation) bool {
	schema := successResponseSchema(op)
	if schema == nil || !isObjectSchema(schema) {
		return false
	}
	for name, propRef := range schema.Properties {
		if !strings.EqualFold(name, "pageDetails") {
			continue
		}
		prop := schemaRefValue(propRef)
		if prop == nil || !isObjectSchema(prop) {
			return false
		}
		for nestedName := range prop.Properties {
			if strings.EqualFold(nestedName, "nextPageUrl") || strings.EqualFold(nestedName, "nextPageURL") {
				return true
			}
		}
	}
	return false
}

func responseItemHasNumericID(op *openapi3.Operation, idField string) bool {
	if strings.TrimSpace(idField) == "" {
		return false
	}
	itemSchema := unwrapItemSchema(successResponseSchema(op))
	if itemSchema == nil {
		return false
	}
	var idRef *openapi3.SchemaRef
	for name, propRef := range itemSchema.Properties {
		if strings.EqualFold(name, idField) {
			idRef = propRef
			break
		}
	}
	if idRef == nil {
		return false
	}
	idSchema := schemaRefValue(idRef)
	return idSchema != nil && idSchema.Type != nil && (idSchema.Type.Is(openapi3.TypeInteger) || idSchema.Type.Is(openapi3.TypeNumber))
}

func successResponseSchema(op *openapi3.Operation) *openapi3.Schema {
	if op == nil || op.Responses == nil {
		return nil
	}
	success := selectSuccessResponse(op.Responses)
	if success == nil || success.Value == nil {
		return nil
	}
	schemaRef := selectResponseSchema(success.Value)
	if schemaRef == nil {
		return nil
	}
	return schemaRef.Value
}

func detectPaginationResponseFields(schema *openapi3.Schema, prefix string, pag *spec.Pagination) {
	if schema == nil {
		return
	}
	propNames := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		propNames = append(propNames, name)
	}
	sort.Strings(propNames)

	for _, propName := range propNames {
		propRef := schema.Properties[propName]
		if pag.NextCursorPath != "" && pag.HasMoreField != "" {
			return
		}
		path := propName
		if prefix != "" {
			path = prefix + "." + propName
		}
		lower := strings.ToLower(propName)
		switch {
		case stringInSlice(lower, nextFieldPageTokenNames):
			if pag.NextCursorPath == "" && pag.CursorParam != "" {
				pag.NextCursorPath = path
			}
			if pag.Type == "" {
				pag.Type = "page_token"
			}
		case stringInSlice(lower, nextFieldPageNumberNames):
			if pag.NextCursorPath == "" && pag.CursorParam != "" {
				pag.NextCursorPath = path
			}
			if pag.Type == "" {
				pag.Type = "page"
			}
		case stringInSlice(lower, nextFieldCursorNames):
			if pag.NextCursorPath == "" && pag.CursorParam != "" {
				pag.NextCursorPath = path
			}
			if pag.Type == "" {
				pag.Type = "cursor"
			}
		case stringInSlice(lower, nextFieldBoolNames):
			if pag.HasMoreField == "" {
				pag.HasMoreField = path
			}
			if pag.Type == "" {
				pag.Type = "cursor"
			}
		}

		if prefix != "" || !isPaginationWrapperField(lower) || propRef == nil || propRef.Value == nil {
			continue
		}
		detectPaginationResponseFields(propRef.Value, path, pag)
	}
}

func isPaginationWrapperField(lower string) bool {
	switch lower {
	case "meta", "metadata", "pagination", "paging", "response_metadata":
		return true
	default:
		return false
	}
}

// itemsFieldNames lists JSON property names that, when typed as an array,
// signal "this object is a paged envelope" — matches the runtime
// extractPaginatedItems helper in templates/helpers.go.tmpl so detect-time
// and runtime walks agree on what counts as a page of items.
var itemsFieldNames = []string{"data", "items", "results", "messages", "members", "values"}

// nextFieldURLNames lists string properties that carry a full URL to
// the next page (resolvable directly with GET). The runtime helper
// follows these without API-specific cursor arithmetic.
var nextFieldURLNames = []string{"next", "next_url", "nexturl"}

// nextFieldPageNumberNames lists numeric fields that carry the next page
// number. Unlike opaque cursors, these can be fed back to page-style
// pagination params as strings.
var nextFieldPageNumberNames = []string{"next_page", "nextpage"}

// nextFieldPageTokenNames lists cursor fields that conventionally pair with
// page-token request params.
var nextFieldPageTokenNames = []string{"next_page_token", "nextpagetoken"}

// nextFieldCursorNames lists opaque cursor fields. Unlike page-token
// names (handled by nextFieldPageTokenNames), these carry unstructured
// strings that cannot advance pagination without a known query parameter.
// The runtime treats their presence as a truncation signal when no cursor
// param is configured.
var nextFieldCursorNames = []string{"next_cursor", "nextcursor", "next_token", "nexttoken", "cursor"}

// nextFieldBoolNames lists boolean-typed "more pages" flags.
var nextFieldBoolNames = []string{"has_more", "hasmore"}

// detectEmbeddedPagedSubresources walks the success-response schema of a
// GET operation looking for top-level properties whose nested shape is a
// paged envelope. Each match yields a companion-helper candidate; the
// caller stores the result on Endpoint.EmbeddedPagedSubresources and the
// generator emits fetchFull<Endpoint><Property> helpers from there.
// Returns nil when nothing matches so callers can compare against nil.
//
// Detection is a two-part heuristic: the property schema must be
// object-shaped AND carry both an array items field (items/data/
// results/...) and a next-page signal (cursor/URL string or has_more-
// style bool).
func detectEmbeddedPagedSubresources(op *openapi3.Operation, parentPath string) []spec.EmbeddedPagedSubresource {
	if op == nil || op.Responses == nil {
		return nil
	}
	success := selectSuccessResponse(op.Responses)
	if success == nil || success.Value == nil {
		return nil
	}
	schemaRef := selectResponseSchema(success.Value)
	if schemaRef == nil || schemaRef.Value == nil {
		return nil
	}

	var out []spec.EmbeddedPagedSubresource
	propNames := make([]string, 0, len(schemaRef.Value.Properties))
	for name := range schemaRef.Value.Properties {
		propNames = append(propNames, name)
	}
	sort.Strings(propNames)

	for _, propName := range propNames {
		propRef := schemaRef.Value.Properties[propName]
		if propRef == nil || propRef.Value == nil {
			continue
		}
		propSchema := propRef.Value
		if !schemaHasObjectShape(propSchema) {
			continue
		}
		lowered := loweredPropertyMap(propSchema)
		itemsField, ok := findItemsField(propSchema, lowered)
		if !ok {
			continue
		}
		nextField, kind := findNextField(lowered)
		if kind == nextKindNone {
			continue
		}
		out = append(out, spec.EmbeddedPagedSubresource{
			Property:      propName,
			ChildPath:     joinChildPath(parentPath, propName),
			ItemsField:    itemsField,
			NextField:     nextField,
			NextIsURL:     kind == nextKindURL,
			NextIsBoolean: kind == nextKindBoolean,
		})
	}
	return out
}

func schemaHasObjectShape(s *openapi3.Schema) bool {
	if s == nil {
		return false
	}
	if s.Type != nil && s.Type.Is("object") {
		return true
	}
	// Some specs omit `type: object` but declare properties anyway —
	// treat that as object-shaped for detection purposes.
	return len(s.Properties) > 0
}

// loweredPropertyMap returns a case-insensitive lookup from canonical
// lowercase property name to the wire-side name actually declared on the
// schema, so detection survives vendors that capitalize fields ("Items",
// "NextPageToken") while preserving the original casing for emission.
func loweredPropertyMap(s *openapi3.Schema) map[string]string {
	out := make(map[string]string, len(s.Properties))
	for name := range s.Properties {
		out[strings.ToLower(name)] = name
	}
	return out
}

func findItemsField(s *openapi3.Schema, lowered map[string]string) (string, bool) {
	if s == nil {
		return "", false
	}
	for _, candidate := range itemsFieldNames {
		actual, ok := lowered[candidate]
		if !ok {
			continue
		}
		ref := s.Properties[actual]
		if ref == nil || ref.Value == nil {
			continue
		}
		if ref.Value.Type != nil && ref.Value.Type.Is("array") {
			return actual, true
		}
	}
	return "", false
}

// nextFieldKind classifies the detected next-page signal so the
// runtime can decide whether to follow it (URL) or stop and warn
// (cursor / has_more bool).
type nextFieldKind int

const (
	nextKindNone nextFieldKind = iota
	nextKindURL
	nextKindCursor
	nextKindBoolean
)

func findNextField(lowered map[string]string) (string, nextFieldKind) {
	for _, candidate := range nextFieldURLNames {
		if actual, ok := lowered[candidate]; ok {
			return actual, nextKindURL
		}
	}
	for _, candidate := range nextFieldPageTokenNames {
		if actual, ok := lowered[candidate]; ok {
			return actual, nextKindCursor
		}
	}
	for _, candidate := range nextFieldPageNumberNames {
		if actual, ok := lowered[candidate]; ok {
			return actual, nextKindCursor
		}
	}
	for _, candidate := range nextFieldCursorNames {
		if actual, ok := lowered[candidate]; ok {
			return actual, nextKindCursor
		}
	}
	for _, candidate := range nextFieldBoolNames {
		if actual, ok := lowered[candidate]; ok {
			return actual, nextKindBoolean
		}
	}
	return "", nextKindNone
}

func stringInSlice(s string, values []string) bool {
	return slices.Contains(values, s)
}

// joinChildPath returns parentPath + "/" + property — the conventional
// URL shape for the dedicated child endpoint of an embedded paged
// sub-resource (e.g. <api>/resource/{id}/<subresource>). APIs that
// publish the sub-resource at a non-conventional URL must override via
// the spec field after parsing.
func joinChildPath(parentPath, property string) string {
	if parentPath == "" || property == "" {
		return ""
	}
	if strings.HasSuffix(parentPath, "/") {
		return parentPath + property
	}
	return parentPath + "/" + property
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
	return spec.DefaultResourceDescription(name)
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
	candidate := out.UniqueFrameworkCollisionResourceName(original)
	warnf("resource %q from path %q would shadow framework cobra command %q; renamed to %q", original, path, original, candidate)
	return candidate
}
