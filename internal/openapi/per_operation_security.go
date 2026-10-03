package openapi

import (
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// applyPerOperationSecurity records a credential per operation when the spec
// splits auth by endpoint family. selectSecurityScheme still picks one primary
// scheme, and AND-group siblings stay global. A second scheme that appears
// only as its own requirement would otherwise be dropped, or, if forced into
// an AND group, sent on operations that reject it.
func applyPerOperationSecurity(doc *openapi3.T, out *spec.APISpec) {
	if doc == nil || out == nil || doc.Components == nil || len(doc.Components.SecuritySchemes) < 2 {
		return
	}
	if out.Auth.Type == "" || out.Auth.Type == "none" || out.Auth.Scheme == "" {
		return
	}
	sole := exclusiveOperationSchemes(doc)
	distinct := map[string]struct{}{}
	for _, scheme := range sole {
		distinct[scheme] = struct{}{}
	}
	if len(distinct) < 2 {
		return
	}

	representable := map[string]struct{}{}
	for name := range distinct {
		if perOperationSchemeRepresentable(doc, out.Auth.Scheme, name) {
			representable[name] = struct{}{}
		}
	}
	if len(representable) < 2 {
		return
	}

	existingByScheme := map[string]struct{}{}
	usedEnv := map[string]struct{}{}
	for _, envVar := range out.Auth.EnvVarSpecs {
		if name := strings.TrimSpace(envVar.Name); name != "" {
			usedEnv[name] = struct{}{}
		}
	}
	for _, name := range out.Auth.EnvVars {
		if name = strings.TrimSpace(name); name != "" {
			usedEnv[name] = struct{}{}
		}
	}
	for _, header := range out.Auth.AdditionalHeaders {
		if header.Scheme != "" {
			existingByScheme[header.Scheme] = struct{}{}
		}
		if name := strings.TrimSpace(header.EnvVar.Name); name != "" {
			usedEnv[name] = struct{}{}
		}
	}

	names := make([]string, 0, len(representable))
	for name := range representable {
		names = append(names, name)
	}
	sort.Strings(names)

	envPrefix := naming.EnvPrefix(out.Name)
	kept := map[string]struct{}{}
	var extras []spec.AdditionalAuthHeader
	for _, name := range names {
		if name == out.Auth.Scheme {
			kept[name] = struct{}{}
			continue
		}
		if _, exists := existingByScheme[name]; exists {
			kept[name] = struct{}{}
			continue
		}
		header, ok := perOperationAdditionalHeader(doc, name, envPrefix, usedEnv)
		if !ok {
			continue
		}
		extras = append(extras, header)
		usedEnv[header.EnvVar.Name] = struct{}{}
		kept[name] = struct{}{}
	}
	if len(kept) < 2 {
		return
	}
	out.Auth.AdditionalHeaders = append(out.Auth.AdditionalHeaders, extras...)
	assignPerOperationAuthSchemes(out, sole, kept)
}

func exclusiveOperationSchemes(doc *openapi3.T) map[string]string {
	out := map[string]string{}
	if doc == nil || doc.Paths == nil {
		return out
	}
	for path, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		for method, op := range pathItem.Operations() {
			if op == nil {
				continue
			}
			scheme := soleSecuritySchemeName(effectiveSecurityRequirements(op, doc))
			if scheme == "" {
				continue
			}
			out[strings.ToUpper(method)+" "+path] = scheme
		}
	}
	return out
}

func soleSecuritySchemeName(requirements openapi3.SecurityRequirements) string {
	if len(requirements) != 1 {
		return ""
	}
	requirement := requirements[0]
	if len(requirement) != 1 {
		return ""
	}
	for name := range requirement {
		return name
	}
	return ""
}

func perOperationSchemeRepresentable(doc *openapi3.T, winner, name string) bool {
	if name == winner {
		return true
	}
	if doc == nil || doc.Components == nil {
		return false
	}
	scheme := securitySchemeValue(doc.Components.SecuritySchemes[name])
	if scheme == nil || !strings.EqualFold(scheme.Type, "apiKey") {
		return false
	}
	if strings.TrimSpace(scheme.Name) == "" {
		return false
	}
	placement := strings.ToLower(strings.TrimSpace(scheme.In))
	return placement == "header" || placement == "query"
}

func perOperationAdditionalHeader(doc *openapi3.T, name, envPrefix string, usedEnv map[string]struct{}) (spec.AdditionalAuthHeader, bool) {
	if doc == nil || doc.Components == nil {
		return spec.AdditionalAuthHeader{}, false
	}
	scheme := securitySchemeValue(doc.Components.SecuritySchemes[name])
	if scheme == nil {
		return spec.AdditionalAuthHeader{}, false
	}
	headerName := strings.TrimSpace(scheme.Name)
	placement := strings.ToLower(strings.TrimSpace(scheme.In))
	envVars := additionalHeaderEnvVars(scheme, name, headerName, envPrefix, additionalHeaderFallbackScheme)
	for _, envVar := range envVars {
		if envVar.EffectiveKind() != spec.AuthEnvVarKindPerCall {
			continue
		}
		envName := strings.TrimSpace(envVar.Name)
		if envName == "" {
			continue
		}
		if _, collision := usedEnv[envName]; collision {
			continue
		}
		envVar.Name = envName
		envVar.Required = false
		if envVar.Kind == "" {
			envVar.Kind = spec.AuthEnvVarKindPerCall
		}
		return spec.AdditionalAuthHeader{
			Header:       headerName,
			In:           placement,
			Scheme:       name,
			PerOperation: true,
			EnvVar:       envVar,
		}, true
	}
	return spec.AdditionalAuthHeader{}, false
}

func assignPerOperationAuthSchemes(out *spec.APISpec, sole map[string]string, kept map[string]struct{}) {
	for resName, resource := range out.Resources {
		changed := assignResourceAuthSchemes(&resource, sole, kept)
		if changed {
			out.Resources[resName] = resource
		}
	}
}

func assignResourceAuthSchemes(resource *spec.Resource, sole map[string]string, kept map[string]struct{}) bool {
	if resource == nil {
		return false
	}
	changed := false
	for epName, endpoint := range resource.Endpoints {
		if !setEndpointAuthScheme(&endpoint, sole, kept) {
			continue
		}
		resource.Endpoints[epName] = endpoint
		changed = true
	}
	for subName, sub := range resource.SubResources {
		if !assignResourceAuthSchemes(&sub, sole, kept) {
			continue
		}
		resource.SubResources[subName] = sub
		changed = true
	}
	return changed
}

func setEndpointAuthScheme(endpoint *spec.Endpoint, sole map[string]string, kept map[string]struct{}) bool {
	scheme, ok := sole[endpoint.Method+" "+endpoint.Path]
	if !ok {
		return false
	}
	if _, ok := kept[scheme]; !ok {
		return false
	}
	if endpoint.AuthScheme == scheme {
		return false
	}
	endpoint.AuthScheme = scheme
	return true
}
