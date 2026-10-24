package pipeline

import (
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	apispec "github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// reasonExampleOmittedUnderivableInputs is the non-blocking help skip when
// the generator left a command without an Example because required inputs
// are not derivable. Failing that row blocks promotion of untouched output.
const reasonExampleOmittedUnderivableInputs = "example omitted: required inputs are not derivable"

// liveDogfoodSpecEndpoint is one generated endpoint command, keyed the same
// way command_endpoint.go.tmpl stamps pp:endpoint.
type liveDogfoodSpecEndpoint struct {
	annotation string
	method     string
	path       string
	endpoint   apispec.Endpoint
}

func liveDogfoodSpecEndpoints(parsed *apispec.APISpec) []liveDogfoodSpecEndpoint {
	if parsed == nil {
		return nil
	}
	// The archive is the pre-generation input. Generate drops captured
	// resource-id defaults before it decides an example is derivable; seeing
	// the original default here fails help for an example that was omitted.
	generator.SanitizeCapturedResourceIDsForGenerate(parsed)
	var out []liveDogfoodSpecEndpoint
	for name, resource := range parsed.Resources {
		out = appendLiveDogfoodSpecEndpoints(out, "", resource, name)
	}
	return out
}

func appendLiveDogfoodSpecEndpoints(out []liveDogfoodSpecEndpoint, parentBase string, resource apispec.Resource, annotationResource string) []liveDogfoodSpecEndpoint {
	for endpointName, endpoint := range resource.Endpoints {
		out = append(out, liveDogfoodSpecEndpoint{
			annotation: annotationResource + "." + endpointName,
			method:     strings.ToUpper(strings.TrimSpace(endpoint.Method)),
			path:       liveDogfoodEffectiveAnnotatedPath([]string{endpoint.BaseURL, resource.BaseURL, parentBase}, endpoint.Path),
			endpoint:   endpoint,
		})
	}
	for subName, sub := range resource.SubResources {
		// pp:endpoint is the subresource map key plus the endpoint, not a
		// parent-qualified name. The path base passed down is this resource
		// only, matching effectiveSubEndpointPath.
		out = appendLiveDogfoodSpecEndpoints(out, strings.TrimSpace(resource.BaseURL), sub, subName)
	}
	return out
}

func liveDogfoodEffectiveAnnotatedPath(bases []string, path string) string {
	base := ""
	for _, candidate := range bases {
		if strings.TrimSpace(candidate) != "" {
			base = candidate
			break
		}
	}
	return normalizeLiveDogfoodPathWithBase(base, path)
}

func liveDogfoodEndpointForCommand(command liveDogfoodCommand, endpoints []liveDogfoodSpecEndpoint) (apispec.Endpoint, bool) {
	name := strings.TrimSpace(command.Annotations[endpointAnnotation])
	if name == "" || len(endpoints) == 0 {
		return apispec.Endpoint{}, false
	}
	var matches []liveDogfoodSpecEndpoint
	for _, endpoint := range endpoints {
		if strings.EqualFold(endpoint.annotation, name) {
			matches = append(matches, endpoint)
		}
	}
	switch len(matches) {
	case 0:
		return apispec.Endpoint{}, false
	case 1:
		return matches[0].endpoint, true
	}
	method := strings.ToUpper(strings.TrimSpace(command.Annotations[endpointMethodAnnotation]))
	path := normalizeLiveDogfoodPath(command.Annotations[endpointPathAnnotation])
	if method == "" || path == "" {
		return apispec.Endpoint{}, false
	}
	var narrowed []liveDogfoodSpecEndpoint
	for _, match := range matches {
		if match.method == method && match.path == path {
			narrowed = append(narrowed, match)
		}
	}
	if len(narrowed) != 1 {
		return apispec.Endpoint{}, false
	}
	return narrowed[0].endpoint, true
}

func liveDogfoodOmittedExampleSkipReason(command liveDogfoodCommand, endpoints []liveDogfoodSpecEndpoint) string {
	endpoint, ok := liveDogfoodEndpointForCommand(command, endpoints)
	if !ok || !generator.ExampleOmittedForUnderivableInputs(endpoint) {
		return ""
	}
	return reasonExampleOmittedUnderivableInputs
}

func liveDogfoodCommandHasDeclaredFixture(command liveDogfoodCommand) bool {
	return strings.TrimSpace(command.Annotations[happyArgsAnnotation]) != "" ||
		strings.TrimSpace(command.Annotations[happyStdinAnnotation]) != ""
}
