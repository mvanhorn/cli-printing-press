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

	"github.com/mvanhorn/cli-printing-press/v3/internal/mcpdesc"
	"github.com/mvanhorn/cli-printing-press/v3/internal/mcpoverrides"
	"github.com/mvanhorn/cli-printing-press/v3/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
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
	RequiredHeaders []ManifestHeader `json:"required_headers"`
	Tools           []ManifestTool   `json:"tools"`
}

// ManifestAuth captures the auth configuration needed to make authenticated
// API requests at runtime.
type ManifestAuth struct {
	Type                           string   `json:"type"`
	Header                         string   `json:"header,omitempty"`
	Format                         string   `json:"format,omitempty"`
	In                             string   `json:"in,omitempty"`
	EnvVars                        []string `json:"env_vars,omitempty"`
	KeyURL                         string   `json:"key_url,omitempty"`
	CookieDomain                   string   `json:"cookie_domain,omitempty"`
	RequiresBrowserSession         bool     `json:"requires_browser_session,omitempty"`
	BrowserSessionValidationPath   string   `json:"browser_session_validation_path,omitempty"`
	BrowserSessionValidationMethod string   `json:"browser_session_validation_method,omitempty"`
}

// ManifestTool describes a single MCP tool derived from an API endpoint.
type ManifestTool struct {
	Name            string           `json:"name"`
	Description     string           `json:"description"`
	Method          string           `json:"method"`
	Path            string           `json:"path"`
	NoAuth          bool             `json:"no_auth,omitempty"`
	Params          []ManifestParam  `json:"params"`
	HeaderOverrides []ManifestHeader `json:"header_overrides,omitempty"`
}

// ManifestParam describes a tool parameter with an explicit location
// (path, query, or body).
type ManifestParam struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Location    string `json:"location"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
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

	total, public := parsed.CountMCPTools()
	mcpReady := computeMCPReady(parsed.Auth.Type)

	// For cookie/composed auth, only include NoAuth endpoints.
	cookieOrComposed := parsed.Auth.Type == "cookie" || parsed.Auth.Type == "composed"

	manifest := ToolsManifest{
		APIName:       parsed.Name,
		BaseURL:       parsed.BaseURL,
		Description:   parsed.Description,
		MCPReady:      mcpReady,
		HTTPTransport: parsed.EffectiveHTTPTransport(),
		Auth: ManifestAuth{
			Type:                           parsed.Auth.Type,
			Header:                         parsed.Auth.Header,
			Format:                         normalizeAuthFormat(parsed.Auth.Format, parsed.Auth.EnvVars),
			In:                             parsed.Auth.In,
			EnvVars:                        parsed.Auth.EnvVars,
			KeyURL:                         parsed.Auth.KeyURL,
			CookieDomain:                   parsed.Auth.CookieDomain,
			RequiresBrowserSession:         parsed.Auth.RequiresBrowserSession,
			BrowserSessionValidationPath:   parsed.Auth.BrowserSessionValidationPath,
			BrowserSessionValidationMethod: parsed.Auth.BrowserSessionValidationMethod,
		},
		RequiredHeaders: make([]ManifestHeader, 0, len(parsed.RequiredHeaders)),
		Tools:           make([]ManifestTool, 0),
	}

	for _, rh := range parsed.RequiredHeaders {
		manifest.RequiredHeaders = append(manifest.RequiredHeaders, ManifestHeader{
			Name:  rh.Name,
			Value: rh.Value,
		})
	}

	// Iterate resources in sorted order for deterministic output.
	resourceNames := sortedResourceKeys(parsed.Resources)
	for _, rName := range resourceNames {
		resource := parsed.Resources[rName]

		// Top-level endpoints
		endpointNames := sortedEndpointKeys(resource.Endpoints)
		for _, eName := range endpointNames {
			endpoint := resource.Endpoints[eName]
			if cookieOrComposed && !endpoint.NoAuth {
				continue
			}
			toolName := mcpoverrides.ToolName(rName, "", eName)
			desc := mcpdesc.Compose(mcpdesc.Input{
				Endpoint:    endpoint,
				NoAuth:      endpoint.NoAuth,
				AuthType:    parsed.Auth.Type,
				PublicCount: public,
				TotalCount:  total,
			})
			tool := buildManifestTool(toolName, desc, endpoint)
			manifest.Tools = append(manifest.Tools, tool)
		}

		// Sub-resources
		subNames := sortedResourceKeys(resource.SubResources)
		for _, subName := range subNames {
			subResource := resource.SubResources[subName]
			subEndpointNames := sortedEndpointKeys(subResource.Endpoints)
			for _, eName := range subEndpointNames {
				endpoint := subResource.Endpoints[eName]
				if cookieOrComposed && !endpoint.NoAuth {
					continue
				}
				toolName := mcpoverrides.ToolName(rName, subName, eName)
				desc := mcpdesc.Compose(mcpdesc.Input{
					Endpoint:    endpoint,
					NoAuth:      endpoint.NoAuth,
					AuthType:    parsed.Auth.Type,
					PublicCount: public,
					TotalCount:  total,
				})
				tool := buildManifestTool(toolName, desc, endpoint)
				manifest.Tools = append(manifest.Tools, tool)
			}
		}
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

// buildManifestTool creates a ManifestTool from an endpoint, classifying
// each parameter's location.
func buildManifestTool(name, description string, ep spec.Endpoint) ManifestTool {
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
		tool.Params = append(tool.Params, ManifestParam{
			Name:        p.Name,
			Type:        normalizeParamType(p.Type),
			Location:    loc,
			Description: p.Description,
			Required:    p.Required,
		})
	}

	// Body params → body.
	for _, p := range ep.Body {
		tool.Params = append(tool.Params, ManifestParam{
			Name:        p.Name,
			Type:        normalizeParamType(p.Type),
			Location:    "body",
			Description: p.Description,
			Required:    p.Required,
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
