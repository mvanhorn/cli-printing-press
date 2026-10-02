package generator

import (
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

type codeOrchInput struct {
	Name     string
	Location string
	Type     string
	Required bool
	Enum     []string
}

// Discovery must describe the names accepted by the orchestration executor,
// which passes JSON body keys through rather than using the typed-tool aliases.
func codeOrchInputs(ep spec.Endpoint, path string, globalVars ...[]string) []codeOrchInput {
	inputs := make([]codeOrchInput, 0, len(ep.Params)+len(ep.Body))
	for _, p := range ep.Params {
		location := strings.ToLower(strings.TrimSpace(p.In))
		if location == "" {
			location = "query"
		}
		name := p.PublicInputName()
		if isMCPPaginationCursorParam(ep, p) {
			// Cursor bindings are omitted from this executor's query registry.
			name = p.WireName()
		}
		if p.PathParam || (strings.Contains(path, "{"+p.Name+"}") && (p.In == "" || location == "path")) {
			location = "path"
			name = p.Name
		}
		inputs = append(inputs, codeOrchInput{Name: name, Location: location, Type: p.Type,
			Required: p.Required || location == "path", Enum: p.Enum})
	}
	if len(globalVars) > 0 {
		for _, binding := range mcpGlobalTemplateBindings(ep, path, globalVars[0]) {
			present := false
			for _, input := range inputs {
				if input.Name == binding.PublicName {
					present = true
					break
				}
			}
			if present {
				continue
			}
			inputs = append(inputs, codeOrchInput{Name: binding.PublicName, Location: "path", Type: "string"})
		}
	}
	if ep.UsesRawRequestBody() {
		return append(inputs,
			codeOrchInput{Name: "body_base64", Location: "body", Type: "string", Required: ep.BodyRequired},
			codeOrchInput{Name: "content_type", Location: "header", Type: "string"})
	}
	if ep.BodyIsArray {
		return append(inputs, codeOrchInput{Name: "body", Location: "body", Type: "array", Required: ep.BodyRequired})
	}
	for _, p := range ep.Body {
		inputs = append(inputs, codeOrchInput{Name: p.BodyWireName(), Location: "body", Type: p.Type,
			Required: p.Required, Enum: p.Enum})
	}
	return inputs
}

func codeOrchInputKeywords(inputs []codeOrchInput) string {
	var words []string
	for _, input := range inputs {
		words = append(words, input.Name)
		words = append(words, input.Enum...)
	}
	return strings.Join(words, " ")
}
