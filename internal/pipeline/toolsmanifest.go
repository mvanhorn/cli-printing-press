package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/mcpdesc"
	"github.com/mvanhorn/cli-printing-press/v4/internal/mcpoverrides"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// ToolsManifestFilename is the name of the tools manifest file written to each
// published CLI directory. Consumed by `printing-press auth doctor` and
// `printing-press mcp-audit` to inspect the published library without parsing
// the original spec.
const ToolsManifestFilename = "tools-manifest.json"

// ToolsManifest describes every MCP tool for an API, along with API-level
// metadata, in a form the diagnostic commands can read directly.
type ToolsManifest struct {
	APIName         string           `json:"api_name"`
	BaseURL         string           `json:"base_url"`
	Description     string           `json:"description"`
	MCPReady        string           `json:"mcp_ready"`
	HTTPTransport   string           `json:"http_transport,omitempty"`
	Auth            ManifestAuth     `json:"auth"`
	TierRouting     *ManifestTiers   `json:"tier_routing,omitempty"`
	RequiredHeaders []ManifestHeader `json:"required_headers"`
	Tools           []ManifestTool   `json:"tools"`
}

// ManifestAuth captures the auth configuration needed to make authenticated
// API requests at runtime.
type ManifestAuth struct {
	Type                           string            `json:"type"`
	Header                         string            `json:"header,omitempty"`
	Format                         string            `json:"format,omitempty"`
	In                             string            `json:"in,omitempty"`
	EnvVars                        []string          `json:"env_vars,omitempty"`
	EnvVarSpecs                    []spec.AuthEnvVar `json:"env_var_specs,omitempty"`
	KeyURL                         string            `json:"key_url,omitempty"`
	CookieDomain                   string            `json:"cookie_domain,omitempty"`
	RequiresBrowserSession         bool              `json:"requires_browser_session,omitempty"`
	BrowserSessionValidationPath   string            `json:"browser_session_validation_path,omitempty"`
	BrowserSessionValidationMethod string            `json:"browser_session_validation_method,omitempty"`
}

// EffectiveEnvVarSpecs returns the rich env-var spec list, preferring EnvVarSpecs
// when present and falling back to legacy EnvVars synthesized as
// per_call+required+sensitive+inferred.
func (a ManifestAuth) EffectiveEnvVarSpecs() []spec.AuthEnvVar {
	if len(a.EnvVarSpecs) > 0 {
		return a.EnvVarSpecs
	}
	if len(a.EnvVars) == 0 {
		return nil
	}
	envVarSpecs := make([]spec.AuthEnvVar, 0, len(a.EnvVars))
	for _, name := range a.EnvVars {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		envVarSpecs = append(envVarSpecs, spec.AuthEnvVar{
			Name:      name,
			Kind:      spec.AuthEnvVarKindPerCall,
			Required:  true,
			Sensitive: true,
			Inferred:  true,
		})
	}
	return envVarSpecs
}

// ManifestTiers records per-tier routing and auth metadata so audit/doctor
// consumers can reason about generated CLIs whose endpoint credentials differ
// from the global auth block.
type ManifestTiers struct {
	DefaultTier string                  `json:"default_tier,omitempty"`
	Tiers       map[string]ManifestTier `json:"tiers,omitempty"`
}

type ManifestTier struct {
	BaseURL            string       `json:"base_url,omitempty"`
	Auth               ManifestAuth `json:"auth"`
	AllowCrossHostAuth bool         `json:"allow_cross_host_auth,omitempty"`
}

// ManifestTool describes a single MCP tool derived from an API endpoint.
type ManifestTool struct {
	Name            string           `json:"name"`
	Description     string           `json:"description"`
	Method          string           `json:"method"`
	Path            string           `json:"path"`
	Tier            string           `json:"tier,omitempty"`
	NoAuth          bool             `json:"no_auth,omitempty"`
	Params          []ManifestParam  `json:"params"`
	HeaderOverrides []ManifestHeader `json:"header_overrides,omitempty"`
}

// ManifestParam describes a tool parameter with an explicit location
// (path, query, or body). Name is the public CLI/MCP input name; WireName is
// set only when the upstream API key differs from that public name.
type ManifestParam struct {
	Name        string   `json:"name"`
	WireName    string   `json:"wire_name,omitempty"`
	Type        string   `json:"type"`
	Location    string   `json:"location"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
}

// ManifestHeader represents a header name/value pair used for both
// API-level required headers and per-tool header overrides.
type ManifestHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ReadToolsManifest decodes <dir>/tools-manifest.json into a
// ToolsManifest. Returns the wrapped error from os.ReadFile when the
// file is missing — callers that treat absence as "no manifest"
// should check errors.Is(err, fs.ErrNotExist) at the call site.
// Shared between WriteToolsManifest's downstream consumers (audit,
// scorer) so the on-disk schema has a single decode site.
func ReadToolsManifest(dir string) (*ToolsManifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, ToolsManifestFilename))
	if err != nil {
		return nil, err
	}
	var m ToolsManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", ToolsManifestFilename, err)
	}
	return &m, nil
}

// WriteToolsManifest generates a tools-manifest.json from a parsed API spec.
// It iterates Resources/SubResources/Endpoints in sorted key order (matching
// the MCP template's RegisterTools pattern) and writes deterministic JSON.
func WriteToolsManifest(dir string, parsed *spec.APISpec) error {
	if parsed == nil {
		return fmt.Errorf("parsed spec is nil")
	}

	endpoints := manifestEndpointRecords(parsed)
	total, public := manifestToolCounts(endpoints)
	mcpReady := computeMCPReady(parsed.Auth.Type)

	paramDescriptions := mcpdesc.NewParamDescriptionCompactorForEndpoints(manifestEndpoints(endpoints))

	manifest := ToolsManifest{
		APIName:         parsed.Name,
		BaseURL:         parsed.BaseURL,
		Description:     parsed.Description,
		MCPReady:        mcpReady,
		HTTPTransport:   parsed.EffectiveHTTPTransport(),
		Auth:            manifestAuth(parsed.Auth),
		RequiredHeaders: make([]ManifestHeader, 0, len(parsed.RequiredHeaders)),
		Tools:           make([]ManifestTool, 0),
	}
	if parsed.HasTierRouting() {
		manifest.TierRouting = buildManifestTiers(parsed.TierRouting)
	}

	for _, rh := range parsed.RequiredHeaders {
		manifest.RequiredHeaders = append(manifest.RequiredHeaders, ManifestHeader{
			Name:  rh.Name,
			Value: rh.Value,
		})
	}

	for _, endpoint := range endpoints {
		desc := mcpdesc.Compose(mcpdesc.Input{
			Endpoint:    endpoint.Endpoint,
			NoAuth:      endpoint.NoAuth,
			AuthType:    endpoint.AuthType,
			PublicCount: public,
			TotalCount:  total,
		})
		tool := buildManifestTool(endpoint.ToolName, desc, endpoint.Endpoint, paramDescriptions.Description)
		tool.Tier = endpoint.Tier
		tool.NoAuth = endpoint.NoAuth
		manifest.Tools = append(manifest.Tools, tool)
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling tools manifest: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(filepath.Join(dir, ToolsManifestFilename), data, 0o644); err != nil {
		return fmt.Errorf("writing tools manifest: %w", err)
	}
	return nil
}

func buildManifestTiers(tierRouting spec.TierRoutingConfig) *ManifestTiers {
	tiers := make(map[string]ManifestTier, len(tierRouting.Tiers))
	for name, tier := range tierRouting.Tiers {
		tiers[name] = ManifestTier{
			BaseURL:            tier.BaseURL,
			Auth:               manifestAuth(tier.Auth),
			AllowCrossHostAuth: tier.AllowCrossHostAuth,
		}
	}
	return &ManifestTiers{
		DefaultTier: tierRouting.DefaultTier,
		Tiers:       tiers,
	}
}

func manifestAuth(auth spec.AuthConfig) ManifestAuth {
	auth.NormalizeEnvVarSpecs("")
	return ManifestAuth{
		Type:                           auth.Type,
		Header:                         auth.Header,
		Format:                         normalizeAuthFormat(auth.Format, auth.EnvVars),
		In:                             auth.In,
		EnvVars:                        auth.EnvVars,
		EnvVarSpecs:                    auth.EnvVarSpecs,
		KeyURL:                         auth.KeyURL,
		CookieDomain:                   auth.CookieDomain,
		RequiresBrowserSession:         auth.RequiresBrowserSession,
		BrowserSessionValidationPath:   auth.BrowserSessionValidationPath,
		BrowserSessionValidationMethod: auth.BrowserSessionValidationMethod,
	}
}

type manifestEndpointRecord struct {
	ToolName string
	Endpoint spec.Endpoint
	Tier     string
	NoAuth   bool
	AuthType string
}

func manifestEndpointRecords(parsed *spec.APISpec) []manifestEndpointRecord {
	if parsed == nil {
		return nil
	}
	var records []manifestEndpointRecord
	resourceNames := sortedResourceKeys(parsed.Resources)
	for _, rName := range resourceNames {
		resource := parsed.Resources[rName]
		endpointNames := sortedEndpointKeys(resource.Endpoints)
		for _, eName := range endpointNames {
			endpoint := resource.Endpoints[eName]
			noAuth, authType, include := effectiveManifestEndpointAuth(parsed, resource, endpoint)
			if !include {
				continue
			}
			records = append(records, manifestEndpointRecord{
				ToolName: mcpoverrides.ToolName(rName, "", eName),
				Endpoint: endpoint,
				Tier:     parsed.EffectiveTier(resource, endpoint),
				NoAuth:   noAuth,
				AuthType: authType,
			})
		}
		subNames := sortedResourceKeys(resource.SubResources)
		for _, subName := range subNames {
			subResource := resource.SubResources[subName]
			effectiveSub := subResource
			if effectiveSub.Tier == "" {
				effectiveSub.Tier = resource.Tier
			}
			subEndpointNames := sortedEndpointKeys(subResource.Endpoints)
			for _, eName := range subEndpointNames {
				endpoint := subResource.Endpoints[eName]
				noAuth, authType, include := effectiveManifestEndpointAuth(parsed, effectiveSub, endpoint)
				if !include {
					continue
				}
				records = append(records, manifestEndpointRecord{
					ToolName: mcpoverrides.ToolName(rName, subName, eName),
					Endpoint: endpoint,
					Tier:     parsed.EffectiveTier(effectiveSub, endpoint),
					NoAuth:   noAuth,
					AuthType: authType,
				})
			}
		}
	}
	return records
}

func manifestToolCounts(records []manifestEndpointRecord) (total, public int) {
	for _, record := range records {
		total++
		if record.NoAuth {
			public++
		}
	}
	return total, public
}

func effectiveManifestEndpointAuth(parsed *spec.APISpec, resource spec.Resource, endpoint spec.Endpoint) (noAuth bool, authType string, include bool) {
	authType, noAuth = parsed.EffectiveEndpointAuth(resource, endpoint)
	if noAuth {
		return true, authType, true
	}
	if parsed.Auth.Type == "cookie" || parsed.Auth.Type == "composed" {
		return false, authType, authType != parsed.Auth.Type
	}
	return noAuth, authType, true
}

// buildManifestTool creates a ManifestTool from an endpoint, classifying
// each parameter's location.
func buildManifestTool(name, description string, ep spec.Endpoint, describeParam func(spec.Param) string) ManifestTool {
	tool := ManifestTool{
		Name:        name,
		Description: description,
		Method:      strings.ToUpper(ep.Method),
		Path:        ep.Path,
		NoAuth:      ep.NoAuth,
		Params:      make([]ManifestParam, 0, len(ep.Params)+len(ep.Body)),
	}

	// Regular params. A param ends up at "path" when the runtime
	// substitutes it into the URL — that's true for both positional
	// path args (Positional=true) AND path params reclassified into
	// CLI flags by reclassifyPathParamModifiers (PathParam=true, e.g.,
	// enum-typed path params like /v2/calendars/{calendar} that the CLI
	// renders as --calendar). Without this OR, reclassified path params
	// land in the manifest as location: "query", which misleads
	// description-override agents reading the manifest to understand
	// the API contract.
	for _, p := range ep.Params {
		loc := "query"
		if p.Positional || p.PathParam {
			loc = "path"
		}
		name := p.PublicInputName()
		tool.Params = append(tool.Params, ManifestParam{
			Name:        name,
			WireName:    manifestWireName(name, p.Name),
			Type:        normalizeParamType(p.Type),
			Location:    loc,
			Description: describeParam(p),
			Required:    p.Required,
			Aliases:     append([]string(nil), p.Aliases...),
		})
	}

	// Body params → body.
	for _, p := range ep.Body {
		name := p.PublicInputName()
		tool.Params = append(tool.Params, ManifestParam{
			Name:        name,
			WireName:    manifestWireName(name, p.Name),
			Type:        normalizeParamType(p.Type),
			Location:    "body",
			Description: describeParam(p),
			Required:    p.Required,
			Aliases:     append([]string(nil), p.Aliases...),
		})
	}

	// Per-endpoint header overrides.
	if len(ep.HeaderOverrides) > 0 {
		tool.HeaderOverrides = make([]ManifestHeader, 0, len(ep.HeaderOverrides))
		for _, ho := range ep.HeaderOverrides {
			tool.HeaderOverrides = append(tool.HeaderOverrides, ManifestHeader{
				Name:  ho.Name,
				Value: ho.Value,
			})
		}
	}

	return tool
}

func manifestWireName(publicName, wireName string) string {
	if publicName == wireName {
		return ""
	}
	return wireName
}

func manifestEndpoints(records []manifestEndpointRecord) []spec.Endpoint {
	endpoints := make([]spec.Endpoint, 0, len(records))
	for _, record := range records {
		endpoints = append(endpoints, record.Endpoint)
	}
	return endpoints
}

// normalizeAuthFormat rewrites the auth format string so that derived
// placeholders (like {token} from DUB_TOKEN) become the actual env var
// name ({DUB_TOKEN}). This way the mega MCP's runtime expansion only needs
// to handle env var names, not the derived semantic aliases that the
// generated config template uses.
func normalizeAuthFormat(format string, envVars []string) string {
	if format == "" || len(envVars) == 0 {
		return format
	}
	result := format
	for _, envVar := range envVars {
		derived := naming.EnvVarPlaceholder(envVar)
		if derived != strings.ToLower(envVar) {
			// Replace the derived placeholder with the env var name.
			result = strings.ReplaceAll(result, "{"+derived+"}", "{"+envVar+"}")
		}
	}
	// Also replace common semantic aliases with the first env var.
	first := envVars[0]
	for _, alias := range []string{"token", "access_token", "api_key"} {
		// Only replace if it's not already the env var name.
		if alias != strings.ToLower(first) {
			result = strings.ReplaceAll(result, "{"+alias+"}", "{"+first+"}")
		}
	}
	return result
}

// normalizeParamType ensures a consistent type string. Empty types default
// to "string".
func normalizeParamType(t string) string {
	if t == "" {
		return "string"
	}
	return t
}

// sortedResourceKeys returns sorted keys from a map[string]spec.Resource.
func sortedResourceKeys(m map[string]spec.Resource) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedEndpointKeys returns sorted keys from a map[string]spec.Endpoint.
func sortedEndpointKeys(m map[string]spec.Endpoint) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ComputeToolsManifestChecksum returns the SHA-256 checksum of manifest data
// in "sha256:<hex>" format, matching the format used in registry.json.
func ComputeToolsManifestChecksum(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}
