package mcpdesc

import (
	"strings"
	"unicode/utf8"

	"github.com/mvanhorn/cli-printing-press/v3/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
)

const (
	sharedParamMinOccurrences  = 3
	sharedParamMinDescription  = 100
	sharedParamMinSentence     = 24
	sharedParamDescriptionMax  = 96
	sharedParamDescriptionTail = "..."
)

// ParamDescriptionCompactor shortens descriptions only for long, repeated
// parameter text that appears often enough to bloat endpoint-mirror MCP
// schemas. Unique, short, and endpoint-specific descriptions pass through.
type ParamDescriptionCompactor struct {
	compacted map[paramDescriptionKey]string
}

type paramDescriptionKey struct {
	name        string
	paramType   string
	description string
}

func NewParamDescriptionCompactor(api *spec.APISpec) *ParamDescriptionCompactor {
	if api == nil {
		return NewParamDescriptionCompactorForEndpoints(nil)
	}
	var endpoints []spec.Endpoint
	for _, resource := range api.Resources {
		endpoints = appendResourceEndpoints(endpoints, resource)
	}
	return NewParamDescriptionCompactorForEndpoints(endpoints)
}

func NewParamDescriptionCompactorForEndpoints(endpoints []spec.Endpoint) *ParamDescriptionCompactor {
	counts := map[paramDescriptionKey]int{}
	for _, endpoint := range endpoints {
		countParams(counts, endpoint.Params)
		countParams(counts, endpoint.Body)
	}

	compacted := map[paramDescriptionKey]string{}
	for key, count := range counts {
		if count >= sharedParamMinOccurrences && utf8.RuneCountInString(key.description) >= sharedParamMinDescription {
			compacted[key] = compactSharedParamDescription(key.description)
		}
	}
	return &ParamDescriptionCompactor{compacted: compacted}
}

func (c *ParamDescriptionCompactor) Description(p spec.Param) string {
	description := naming.OneLineNormalize(p.Description)
	if c == nil || description == "" {
		return description
	}
	if compacted, ok := c.compacted[keyForParamDescription(p, description)]; ok {
		return compacted
	}
	return description
}

func appendResourceEndpoints(endpoints []spec.Endpoint, resource spec.Resource) []spec.Endpoint {
	for _, endpoint := range resource.Endpoints {
		endpoints = append(endpoints, endpoint)
	}
	for _, subResource := range resource.SubResources {
		endpoints = appendResourceEndpoints(endpoints, subResource)
	}
	return endpoints
}

func countParams(counts map[paramDescriptionKey]int, params []spec.Param) {
	for _, p := range params {
		description := naming.OneLineNormalize(p.Description)
		if description == "" {
			continue
		}
		counts[keyForParamDescription(p, description)]++
	}
}

func keyForParamDescription(p spec.Param, description string) paramDescriptionKey {
	return paramDescriptionKey{
		name:        strings.ToLower(strings.TrimSpace(p.Name)),
		paramType:   normalizeParamType(p.Type),
		description: description,
	}
}

func normalizeParamType(paramType string) string {
	normalized := strings.ToLower(strings.TrimSpace(paramType))
	if normalized == "" {
		return "string"
	}
	return normalized
}

func compactSharedParamDescription(description string) string {
	if first := firstSentence(description); len(first) >= sharedParamMinSentence && len(first) <= sharedParamDescriptionMax {
		return first
	}
	return truncateSharedParamDescription(description)
}

func firstSentence(description string) string {
	for i, r := range description {
		if r == '.' || r == '!' || r == '?' {
			return strings.TrimSpace(description[:i+1])
		}
	}
	return ""
}

func truncateSharedParamDescription(description string) string {
	runes := []rune(description)
	if len(runes) <= sharedParamDescriptionMax {
		return description
	}
	limit := sharedParamDescriptionMax - len(sharedParamDescriptionTail)
	cut := limit
	prefix := string(runes[:limit])
	if idx := strings.LastIndexAny(prefix, " \t"); idx > 0 {
		cut = idx
		return strings.TrimSpace(prefix[:cut]) + sharedParamDescriptionTail
	}
	return strings.TrimSpace(string(runes[:cut])) + sharedParamDescriptionTail
}
