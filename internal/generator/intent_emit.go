package generator

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

func intentStepOpName(ref string) string {
	if i := strings.LastIndex(ref, "."); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

func intentIsReadOnly(api *spec.APISpec, intent spec.Intent) bool {
	if len(intent.Steps) == 0 {
		return false
	}
	shared := sharedGETRPCPaths(api.Resources)
	for _, step := range intent.Steps {
		ep, ok := lookupEndpointForTemplate(api, step.Endpoint)
		if !ok {
			return false
		}
		if !endpointIsReadCommandShared(ep.Endpoint, intentStepOpName(step.Endpoint), shared) {
			return false
		}
	}
	return true
}

func intentIsDestructive(api *spec.APISpec, intent spec.Intent) bool {
	for _, step := range intent.Steps {
		ep, ok := lookupEndpointForTemplate(api, step.Endpoint)
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(ep.Method), "DELETE") {
			return true
		}
	}
	return false
}

// mcpHint is the destructiveHint an MCP tool should advertise.
// Set is false when the hint must stay unset so hosts treat the tool
// as possibly destructive. Value is meaningful only when Set is true.
type mcpHint struct {
	Set   bool
	Value bool
}

// mcpWriteDestructiveHint is the destructiveHint for a non-read endpoint.
// Reads are decided by the caller; a read keeps readOnlyHint and
// destructiveHint false even when Destructive is set.
func mcpWriteDestructiveHint(endpoint spec.Endpoint) mcpHint {
	if endpoint.Destructive != nil {
		return mcpHint{Set: true, Value: *endpoint.Destructive}
	}
	if strings.EqualFold(strings.TrimSpace(endpoint.Method), "DELETE") {
		return mcpHint{Set: true, Value: true}
	}
	return mcpHint{}
}

// intentDestructiveHint is the write hint for an intent that is not
// read-only. One destructive step wins. Every write step must be
// explicitly additive before the intent advertises destructiveHint false.
// An unresolved step is not additive.
func intentDestructiveHint(api *spec.APISpec, intent spec.Intent) mcpHint {
	if api == nil {
		return mcpHint{}
	}
	shared := sharedGETRPCPaths(api.Resources)
	sawWrite := false
	allExplicitAdditive := true
	for _, step := range intent.Steps {
		ep, ok := lookupEndpointForTemplate(api, step.Endpoint)
		if !ok {
			sawWrite = true
			allExplicitAdditive = false
			continue
		}
		if endpointIsReadCommandShared(ep.Endpoint, intentStepOpName(step.Endpoint), shared) {
			continue
		}
		sawWrite = true
		hint := mcpWriteDestructiveHint(ep.Endpoint)
		if hint.Set && hint.Value {
			return mcpHint{Set: true, Value: true}
		}
		if !hint.Set || hint.Value {
			allExplicitAdditive = false
		}
	}
	if sawWrite && allExplicitAdditive {
		return mcpHint{Set: true, Value: false}
	}
	return mcpHint{}
}

func composeIntentToolDescription(api *spec.APISpec, intent spec.Intent) string {
	summary := honestIntentSummary(intent)
	var steps []string
	for _, step := range intent.Steps {
		ep, ok := lookupEndpointForTemplate(api, step.Endpoint)
		label := ""
		if ok {
			label = strings.TrimSpace(ep.Description)
		}
		if label == "" {
			label = "Calls " + step.Endpoint
		}
		steps = append(steps, strings.TrimSuffix(label, "."))
	}
	stepText := strings.Join(steps, ". Then ")
	switch {
	case summary == "" && stepText == "":
		return intent.Name
	case summary == "":
		return stepText + "."
	case stepText == "":
		return summary
	default:
		return strings.TrimSuffix(summary, ".") + ". " + stepText + "."
	}
}

func honestIntentSummary(intent spec.Intent) string {
	desc := strings.TrimSpace(intent.Description)
	if i := documentedDefaultIndex(desc); i >= 0 {
		desc = strings.TrimSpace(strings.TrimRight(desc[:i], " .,"))
	}
	lower := strings.ToLower(desc)
	for _, sep := range []string{", then ", "; then ", ". then ", " and then "} {
		if i := strings.Index(lower, sep); i >= 0 {
			return strings.TrimSpace(desc[:i])
		}
	}
	return desc
}

func intentParamsForEmit(intent spec.Intent) []spec.IntentParam {
	params := append([]spec.IntentParam(nil), intent.Params...)
	for i := range params {
		if strings.TrimSpace(params[i].Default) == "" {
			params[i].Default = documentedDefault(params[i].Description)
		}
	}
	if fallback := documentedDefault(intent.Description); fallback != "" {
		var candidates []int
		for i, p := range params {
			if p.Required || strings.TrimSpace(p.Default) != "" {
				continue
			}
			if p.Type == "string" || p.Type == "" {
				candidates = append(candidates, i)
			}
		}
		if len(candidates) == 1 {
			params[candidates[0]].Default = fallback
		}
	}
	for i := range params {
		params[i].Default = canonicalIntentParamDefault(params[i])
	}
	return params
}

func canonicalIntentParamDefault(p spec.IntentParam) string {
	d := strings.TrimSpace(p.Default)
	if d == "" {
		return ""
	}
	switch p.Type {
	case "integer":
		n, err := strconv.ParseInt(d, 10, 64)
		if err != nil {
			return ""
		}
		return strconv.FormatInt(n, 10)
	case "boolean":
		switch strings.ToLower(d) {
		case "true", "1", "yes":
			return "true"
		case "false", "0", "no":
			return "false"
		default:
			return ""
		}
	default:
		return d
	}
}

func documentedDefault(text string) string {
	i := documentedDefaultIndex(text)
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(text[i:])
	lower := strings.ToLower(rest)
	for _, marker := range []string{"defaults to ", "default: "} {
		if strings.HasPrefix(lower, marker) {
			rest = strings.TrimSpace(rest[len(marker):])
			break
		}
	}
	return strings.TrimRight(firstShellSafeDescriptionToken(rest), ".,;)")
}

func documentedDefaultIndex(text string) int {
	lower := strings.ToLower(text)
	best := -1
	for _, marker := range []string{"defaults to ", "default: "} {
		if i := markerIndexAtWord(lower, marker); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	return best
}

func markerIndexAtWord(lower, marker string) int {
	searchFrom := 0
	for {
		idx := strings.Index(lower[searchFrom:], marker)
		if idx < 0 {
			return -1
		}
		idx += searchFrom
		if idx == 0 {
			return idx
		}
		prev := rune(lower[idx-1])
		if !unicode.IsLetter(prev) && !unicode.IsDigit(prev) {
			return idx
		}
		searchFrom = idx + len(marker)
	}
}

func intentParamDefaultGo(p spec.IntentParam) string {
	if d := canonicalIntentParamDefault(p); d != "" {
		switch p.Type {
		case "integer", "boolean":
			return d
		default:
			return strconv.Quote(d)
		}
	}
	raw := strings.TrimSpace(p.Default)
	if raw == "" {
		switch p.Type {
		case "integer":
			return "0"
		case "boolean":
			return "false"
		default:
			return `""`
		}
	}
	return strconv.Quote(raw)
}
