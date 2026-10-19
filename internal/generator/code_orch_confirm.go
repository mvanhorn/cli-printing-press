package generator

import (
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// codeOrchEndpointNeedsConfirm reports whether a code-orchestration call
// must preview and wait for a top-level boolean confirm. DELETE always
// confirms. Every other method follows the read-command classifier, so a
// codegen read-only POST does not confirm and a mutating GET does.
func codeOrchEndpointNeedsConfirm(endpoint spec.Endpoint, opName string, sharedGETPaths map[string]bool) bool {
	if strings.EqualFold(strings.TrimSpace(endpoint.Method), "DELETE") {
		return true
	}
	return !endpointIsReadCommandShared(endpoint, opName, sharedGETPaths)
}

// codeOrchHasEndpointClass reports whether any endpoint in the spec is a
// confirm-gated call (wantConfirm true) or a read call (wantConfirm false).
func codeOrchHasEndpointClass(resources map[string]spec.Resource, wantConfirm bool) bool {
	if len(resources) == 0 {
		return false
	}
	shared := sharedGETRPCPaths(resources)
	found := false
	walkCodeOrchResources(resources, func(endpoint spec.Endpoint, opName string) bool {
		if codeOrchEndpointNeedsConfirm(endpoint, opName, shared) == wantConfirm {
			found = true
			return true
		}
		return false
	})
	return found
}

func walkCodeOrchResources(resources map[string]spec.Resource, fn func(spec.Endpoint, string) bool) {
	var walk func(spec.Resource) bool
	walk = func(resource spec.Resource) bool {
		for name, endpoint := range resource.Endpoints {
			if fn(endpoint, name) {
				return true
			}
		}
		for _, sub := range resource.SubResources {
			if walk(sub) {
				return true
			}
		}
		return false
	}
	for _, resource := range resources {
		if walk(resource) {
			return
		}
	}
}
