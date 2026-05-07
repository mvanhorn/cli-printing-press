package pipeline

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"

	apispec "github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// isInternalYAMLSpec returns true if data looks like an internal YAML spec
// (starts with "name:" and contains a "resources:" section) rather than OpenAPI.
func isInternalYAMLSpec(data []byte) bool {
	// Internal YAML specs start with "name:" (possibly after comments/blank lines).
	// OpenAPI specs start with "openapi:" or have a top-level "paths:" key.
	lines := bytes.SplitSeq(data, []byte("\n"))
	for line := range lines {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] == '#' {
			continue
		}
		if bytes.HasPrefix(trimmed, []byte("name:")) {
			return bytes.Contains(data, []byte("\nresources:"))
		}
		// If the first non-comment line is openapi: or swagger:, it's OpenAPI
		if bytes.HasPrefix(trimmed, []byte("openapi:")) || bytes.HasPrefix(trimmed, []byte("swagger:")) {
			return false
		}
		// If it starts with { it's JSON (OpenAPI)
		if trimmed[0] == '{' {
			return false
		}
		break
	}
	return false
}

// internalSpecToDogfoodSpec converts a parsed internal YAML APISpec into the
// openAPISpec struct used by dogfood/verify. Sets IsInternalYAML so
// downstream checks (notably path-validity) can skip rules that only
// make sense for OpenAPI-derived path patterns.
func internalSpecToDogfoodSpec(s *apispec.APISpec) *openAPISpec {
	return &openAPISpec{
		Paths:          collectInternalSpecPaths(s),
		Auth:           s.Auth,
		Kind:           s.Kind,
		HTTPTransport:  s.EffectiveHTTPTransport(),
		ParamDefaults:  collectInternalSpecParamDefaults(s),
		IsInternalYAML: true,
	}
}

// collectInternalSpecParamDefaults walks every endpoint param in the spec
// and records its declared Default (when non-empty) keyed on the lowercase
// param name. Later wins on duplicate names — that's fine because the
// verifier matches placeholders by name regardless of which endpoint they
// appear under, and a spec author defining the same name with different
// defaults across endpoints is already an inconsistency they'll surface
// elsewhere. Only positional params contribute (placeholder lookup is the
// caller).
func collectInternalSpecParamDefaults(s *apispec.APISpec) map[string]string {
	if s == nil {
		return nil
	}
	out := map[string]string{}
	for _, resource := range s.Resources {
		collectResourceParamDefaults(resource, out)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func collectResourceParamDefaults(r apispec.Resource, out map[string]string) {
	for _, endpoint := range r.Endpoints {
		for _, param := range endpoint.Params {
			if !param.Positional {
				continue
			}
			if param.Default == nil {
				continue
			}
			str := stringifyParamDefault(param.Default)
			if str == "" {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(param.Name))
			if key == "" {
				continue
			}
			out[key] = str
		}
	}
	for _, sub := range r.SubResources {
		collectResourceParamDefaults(sub, out)
	}
}

// stringifyParamDefault converts a Param.Default (typed as `any` per the
// spec schema, since YAML can deliver int/bool/string) into the wire-side
// string the verifier passes on the command line. Returns "" for nil or
// empty-string defaults; numbers and bools render via fmt.Sprintf("%v",...).
func stringifyParamDefault(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}

// internalSpecToOpenAPISpecInfo converts a parsed internal YAML APISpec into
// the openAPISpecInfo struct used by scorecard.
func internalSpecToOpenAPISpecInfo(s *apispec.APISpec) *openAPISpecInfo {
	info := &openAPISpecInfo{
		Paths:           collectInternalSpecPaths(s),
		SecuritySchemes: make(map[string]openAPISecurityScheme),
		Kind:            s.Kind,
	}

	// Map auth config to a synthetic security scheme so scorecard auth
	// evaluation works the same as with OpenAPI specs.
	if s.Auth.Type != "" && s.Auth.Type != "none" {
		schemeName := s.Auth.Scheme
		if schemeName == "" {
			schemeName = s.Auth.Type
		}
		scheme := openAPISecurityScheme{Key: schemeName}
		switch s.Auth.Type {
		case "bearer_token":
			scheme.Type = "http"
			scheme.Scheme = "bearer"
		case "api_key":
			scheme.Type = "apikey"
			scheme.In = s.Auth.In
			if scheme.In == "" {
				scheme.In = "header"
			}
			scheme.HeaderName = s.Auth.Header
		case "oauth2":
			scheme.Type = "oauth2"
		case "cookie", "composed":
			scheme.Type = "apikey"
			scheme.In = "cookie"
		default:
			scheme.Type = s.Auth.Type
		}
		info.SecuritySchemes[schemeName] = scheme
		info.SecurityRequirements = []securityRequirementSet{
			{Alternatives: [][]string{{schemeName}}},
		}
	}

	return info
}

// collectInternalSpecPaths extracts all endpoint paths from an internal YAML spec.
func collectInternalSpecPaths(s *apispec.APISpec) []string {
	var paths []string
	for _, resource := range s.Resources {
		collectInternalResourcePaths(resource, &paths)
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

func collectInternalResourcePaths(r apispec.Resource, paths *[]string) {
	for _, endpoint := range r.Endpoints {
		if endpoint.Path != "" {
			*paths = append(*paths, endpoint.Path)
		}
	}
	for _, sub := range r.SubResources {
		collectInternalResourcePaths(sub, paths)
	}
}

// tryLoadInternalYAMLSpec reads specPath and, if it's an internal YAML spec,
// parses it and returns the APISpec. Returns nil, nil if not internal YAML.
func tryLoadInternalYAMLSpec(specPath string) (*apispec.APISpec, error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("reading spec: %w", err)
	}

	if !isInternalYAMLSpec(data) {
		return nil, nil
	}

	parsed, err := apispec.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parsing internal YAML spec: %w", err)
	}
	return parsed, nil
}
