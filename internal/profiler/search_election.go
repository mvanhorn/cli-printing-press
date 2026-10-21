package profiler

import (
	"slices"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

const defaultGenericSearchQueryParam = "q"

// Exact names only. A scoped parameter matches on its innermost bracket
// (filter[query]), not on a substring, so queryString does not match.
var genericSearchParamNames = []string{
	"q", "query", "search", "keyword", "term", "querytext", "searchterm", "searchtext", "text",
}

// Endpoint template variables are substituted from the environment. Any other
// {placeholder} would be sent literally; search has no flag to bind it.
func genericSearchPathBindable(path string, s *spec.APISpec) bool {
	if !strings.Contains(path, "{") {
		return true
	}
	return pathParamsAllTemplateVars(path, s)
}

func isGenericSearchParamName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return false
	}
	if slices.Contains(genericSearchParamNames, lower) {
		return true
	}
	inner, ok := bracketedSearchInner(lower)
	return ok && slices.Contains(genericSearchParamNames, inner)
}

func bracketedSearchInner(name string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	if !strings.HasSuffix(lower, "]") || !strings.Contains(lower, "[") {
		return "", false
	}
	parts := strings.Split(lower, "[")
	if parts[0] == "" || strings.Contains(parts[0], "]") {
		return "", false
	}
	inner := ""
	for _, part := range parts[1:] {
		if !strings.HasSuffix(part, "]") || strings.Count(part, "]") != 1 {
			return "", false
		}
		segment := strings.TrimSuffix(part, "]")
		if segment == "" || strings.ContainsAny(segment, "[]") {
			return "", false
		}
		inner = segment
	}
	return inner, inner != ""
}

func matchedGenericSearchWireName(param spec.Param, inBody bool) string {
	wire := param.WireName()
	if inBody {
		wire = param.BodyWireName()
	}
	if isGenericSearchParamName(wire) {
		return wire
	}
	return ""
}

func isDeclaredGenericSearchInput(param spec.Param, inBody bool) bool {
	if inBody {
		return true
	}
	if param.PathParam {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(param.In)) {
	case "path", "header", "cookie":
		return false
	default:
		return true
	}
}

// Parameterless endpoints keep the historical "q" default. Declared query or
// body parameters with no search name are not callable: "q" is not in the spec.
func resolveGenericSearchQuery(endpoint spec.Endpoint) (string, []SearchBodyField, bool) {
	matched := ""
	declared := false
	for _, param := range endpoint.Params {
		if matched == "" {
			if name := matchedGenericSearchWireName(param, false); name != "" {
				matched = name
			}
		}
		if isDeclaredGenericSearchInput(param, false) {
			declared = true
		}
	}

	var fields []SearchBodyField
	if strings.ToUpper(endpoint.Method) == "POST" {
		for _, param := range endpoint.Body {
			declared = true
			if name := matchedGenericSearchWireName(param, true); name != "" {
				matched = name
				continue
			}
			fields = append(fields, searchBodyFieldFromParam(param))
		}
	}
	if matched != "" {
		return matched, fields, true
	}
	if !declared {
		return defaultGenericSearchQueryParam, fields, true
	}
	return "", nil, false
}

func searchBodyFieldFromParam(param spec.Param) SearchBodyField {
	field := SearchBodyField{
		Name:     param.Name,
		Type:     param.Type,
		Required: param.Required,
	}
	if param.Default != nil {
		field.Default = param.Default
	} else if len(param.Enum) > 0 {
		if param.Type == "array" {
			field.Default = param.Enum
		} else {
			field.Default = param.Enum[0]
		}
	} else if param.Type == "array" && len(param.Fields) > 0 && len(param.Fields[0].Enum) > 0 {
		field.Default = param.Fields[0].Enum
	} else {
		switch param.Type {
		case "integer", "number":
			field.Default = 10
		case "boolean":
			field.Default = true
		case "string":
			field.Default = ""
		case "object":
			field.Default = map[string]any{}
		case "array":
			field.Default = []any{}
		}
	}
	return field
}
