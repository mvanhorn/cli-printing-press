package generator

import (
	"slices"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

type resolvedRequestEndpoint struct {
	parent   spec.Resource
	resource spec.Resource
	endpoint spec.Endpoint
	isSub    bool
}

// effectiveRequestPath returns the same host+path the generated list/get
// command would call: endpoint BaseURL, then resource BaseURL, then the
// original path when neither override is set.
func effectiveRequestPath(api *spec.APISpec, resourceName, path, method string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	resolved, ok := resolveRequestEndpoint(api, resourceName, path, method)
	if !ok {
		return path
	}
	got := effectivePathOf(resolved)
	if (strings.TrimSpace(resourceName) == "" || strings.TrimSpace(method) == "") &&
		!requestOverrideIsUnambiguous(api, path, method, got) {
		return path
	}
	return got
}

func effectivePathOf(resolved resolvedRequestEndpoint) string {
	if resolved.isSub {
		return effectiveSubEndpointPath(resolved.parent, resolved.resource, resolved.endpoint)
	}
	return effectiveEndpointPath(resolved.resource, resolved.endpoint)
}

func requestOverrideIsUnambiguous(api *spec.APISpec, path, method, candidate string) bool {
	if api == nil {
		return true
	}
	path = strings.TrimSpace(path)
	method = strings.ToUpper(strings.TrimSpace(method))
	unambiguous := true
	var walk func(parent spec.Resource, resources map[string]spec.Resource, isSub bool)
	walk = func(parent spec.Resource, resources map[string]spec.Resource, isSub bool) {
		if !unambiguous {
			return
		}
		names := make([]string, 0, len(resources))
		for name := range resources {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			resource := resources[name]
			for _, endpoint := range resource.Endpoints {
				if strings.TrimSpace(endpoint.Path) != path {
					continue
				}
				if method != "" && !strings.EqualFold(strings.TrimSpace(endpoint.Method), method) {
					continue
				}
				match := resolvedRequestEndpoint{parent: parent, resource: resource, endpoint: endpoint, isSub: isSub}
				if effectivePathOf(match) != candidate {
					unambiguous = false
					return
				}
			}
			walk(resource, resource.SubResources, true)
		}
	}
	walk(spec.Resource{}, api.Resources, false)
	return unambiguous
}

func resolveRequestEndpoint(api *spec.APISpec, resourceName, path, method string) (resolvedRequestEndpoint, bool) {
	if api == nil {
		return resolvedRequestEndpoint{}, false
	}
	path = strings.TrimSpace(path)
	method = strings.ToUpper(strings.TrimSpace(method))
	resourceName = strings.TrimSpace(resourceName)
	if path == "" {
		return resolvedRequestEndpoint{}, false
	}

	var (
		namePathMethod resolvedRequestEndpoint
		pathMethod     resolvedRequestEndpoint
		namePath       resolvedRequestEndpoint
		haveNamePath   bool
		havePathMethod bool
		haveNameMethod bool
	)

	var walk func(parent spec.Resource, resources map[string]spec.Resource, isSub bool)
	walk = func(parent spec.Resource, resources map[string]spec.Resource, isSub bool) {
		names := make([]string, 0, len(resources))
		for name := range resources {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			resource := resources[name]
			endpointNames := make([]string, 0, len(resource.Endpoints))
			for endpointName := range resource.Endpoints {
				endpointNames = append(endpointNames, endpointName)
			}
			sort.Strings(endpointNames)
			nameOK := resourceName == "" || name == resourceName || spec.ToSnakeCase(name) == resourceName
			for _, endpointName := range endpointNames {
				endpoint := resource.Endpoints[endpointName]
				if strings.TrimSpace(endpoint.Path) != path {
					continue
				}
				match := resolvedRequestEndpoint{
					parent:   parent,
					resource: resource,
					endpoint: endpoint,
					isSub:    isSub,
				}
				methodOK := method == "" || strings.EqualFold(strings.TrimSpace(endpoint.Method), method)
				if nameOK && methodOK && !haveNameMethod {
					namePathMethod = match
					haveNameMethod = true
				}
				if methodOK && !havePathMethod {
					pathMethod = match
					havePathMethod = true
				}
				if nameOK && !haveNamePath {
					namePath = match
					haveNamePath = true
				}
			}
			walk(resource, resource.SubResources, true)
		}
	}
	walk(spec.Resource{}, api.Resources, false)

	switch {
	case haveNameMethod:
		return namePathMethod, true
	case havePathMethod:
		return pathMethod, true
	case haveNamePath:
		return namePath, true
	default:
		return resolvedRequestEndpoint{}, false
	}
}

func withEffectiveSyncableRequestPaths(api *spec.APISpec, resources []profiler.SyncableResource) []profiler.SyncableResource {
	if len(resources) == 0 {
		return resources
	}
	out := slices.Clone(resources)
	for i, resource := range out {
		out[i].Path = effectiveRequestPath(api, resource.Name, resource.Path, resource.Method)
		if resource.HydratePath != "" {
			out[i].HydratePath = effectiveRequestPath(api, resource.Name, resource.HydratePath, "GET")
		}
	}
	return out
}

func withEffectiveDependentRequestPaths(api *spec.APISpec, resources []profiler.DependentResource) []profiler.DependentResource {
	if len(resources) == 0 {
		return resources
	}
	out := slices.Clone(resources)
	for i, resource := range out {
		out[i].Path = effectiveRequestPath(api, resource.Name, resource.Path, resource.Method)
	}
	return out
}
