package generator

import (
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// endpointHappyArgs returns spec-declared live-dogfood fixtures, or a
// generate-time synthesis from derivable parameter values. Required inputs
// that would need invented IDs, placeholder literals, or other underivable
// values stay unset so the first live matrix does not send known-broken args.
func endpointHappyArgs(ep spec.Endpoint) string {
	if declared := strings.TrimSpace(ep.HappyArgs); declared != "" {
		return declared
	}
	if ep.BodyJSONFallback && ep.BodyRequired && strings.TrimSpace(ep.HappyStdin) == "" {
		return ""
	}
	tokens, ok := synthesizeHappyArgTokens(ep)
	if !ok {
		return ""
	}
	return strings.Join(tokens, ";")
}

func requiredInputsAreDerivable(ep spec.Endpoint) bool {
	_, ok := synthesizeHappyArgTokens(ep)
	return ok
}

func synthesizeHappyArgTokens(ep spec.Endpoint) ([]string, bool) {
	positionals := orderedPositionalParams(ep)
	var tokens []string
	for i, p := range positionals {
		value, ok := derivableHappyArgValue(ep, p)
		if !ok {
			if p.Required || !remainingPositionalsAreOmittable(ep, positionals[i+1:]) {
				return nil, false
			}
			break
		}
		tokens = append(tokens, encodeHappyArgPositional(p, value))
	}
	for _, p := range ep.Params {
		if p.Positional || !p.Required {
			continue
		}
		value, ok := derivableHappyArgValue(ep, p)
		if !ok {
			return nil, false
		}
		tokens = append(tokens, encodeHappyArgFlag(p, value))
	}
	for _, p := range ep.Body {
		if !p.Required || p.Positional {
			continue
		}
		value, ok := derivableHappyArgValue(ep, p)
		if !ok {
			return nil, false
		}
		tokens = append(tokens, encodeHappyArgFlag(p, value))
	}
	return tokens, true
}

func derivableHappyArgValue(ep spec.Endpoint, p spec.Param) (string, bool) {
	value, ok := lookupDerivableHappyArgValue(ep, p)
	if !ok || !happyArgValueEncodable(value) {
		return "", false
	}
	return value, true
}

func lookupDerivableHappyArgValue(ep spec.Endpoint, p spec.Param) (string, bool) {
	if p.Example != nil {
		if s := stringifyDefault(p.Example); shellSafeSchemaExampleValue(s) {
			return s, true
		}
	}
	for _, v := range p.Enum {
		if strings.TrimSpace(v) != "" {
			return v, true
		}
	}
	if val, ok := dispatchParamDefaultValue(ep, p); ok {
		return val, true
	}
	if s, ok := schemaDefaultExampleValue(p); ok {
		return s, true
	}
	if value, ok := descriptionExampleValue(p.Description); ok {
		return value, true
	}
	return formatHappyArgValue(p)
}

func formatHappyArgValue(p spec.Param) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(p.Format)) {
	case "date":
		return "2026-01-15", true
	case "date-time":
		return "2026-01-15T09:00:00Z", true
	case "email":
		return "user@example.com", true
	case "uri", "url", "uri-reference":
		return "https://example.com/resource", true
	default:
		return "", false
	}
}

func remainingPositionalsAreOmittable(ep spec.Endpoint, rest []spec.Param) bool {
	// parseHappyArgsAnnotation discards labels and mergeHappyPositionals overlays
	// by index, so omitting a non-trailing slot would rebind later values.
	for _, p := range rest {
		if _, ok := derivableHappyArgValue(ep, p); ok {
			return false
		}
		if p.Required {
			return false
		}
	}
	return true
}

func encodeHappyArgFlag(p spec.Param, value string) string {
	return "--" + publicFlagName(p) + "=" + escapeHappyArgValue(value)
}

func encodeHappyArgPositional(p spec.Param, value string) string {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = "arg"
	}
	return name + "=" + escapeHappyArgValue(value)
}

func escapeHappyArgValue(value string) string {
	return strings.ReplaceAll(value, ";", `\;`)
}

func happyArgValueEncodable(value string) bool {
	// splitHappyArgs treats \; as an escaped semicolon and does not unescape \\,
	// so a literal backslash cannot round-trip through the annotation.
	return !strings.Contains(value, `\`)
}
