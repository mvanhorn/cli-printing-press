package generator

import (
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

const generatedExampleFieldLimit = 3

// syncResourcesExample returns a small, deterministic selection of resources
// from the profile so generated sync examples remain valid for each API.
func syncResourcesExample(syncable []profiler.SyncableResource, dependent []profiler.DependentResource) string {
	resources := append([]profiler.SyncableResource(nil), syncable...)
	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })

	parents := make(map[string]bool, len(dependent))
	for _, resource := range dependent {
		if name := strings.TrimSpace(resource.ParentResource); name != "" {
			parents[name] = true
		}
	}

	selected := make([]string, 0, 2)
	seen := make(map[string]bool, 2)
	appendResource := func(resource profiler.SyncableResource) {
		name := strings.TrimSpace(resource.Name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		selected = append(selected, name)
	}

	for _, resource := range resources {
		if parents[resource.Name] {
			appendResource(resource)
			break
		}
	}
	for _, resource := range resources {
		if len(selected) >= 2 {
			break
		}
		appendResource(resource)
	}

	return strings.Join(selected, ",")
}

// selectExample returns the first few fields from the first syncable resource
// whose response model is known. It intentionally returns empty when the API
// model cannot support a trustworthy field selection example.
func selectExample(apiSpec *spec.APISpec, syncable []profiler.SyncableResource) string {
	if apiSpec == nil {
		return ""
	}

	resources := append([]profiler.SyncableResource(nil), syncable...)
	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })
	for _, resource := range resources {
		endpoint, ok := endpointForSyncableResource(apiSpec.Resources, resource)
		if !ok {
			continue
		}
		item := strings.TrimSpace(endpoint.Response.Item)
		if item == "" {
			continue
		}
		typeDef, ok := typeDefByName(apiSpec.Types, item)
		if !ok {
			continue
		}
		return generatedExampleFields(typeDef)
	}
	return ""
}

func selectExampleForCommand(apiSpec *spec.APISpec) string {
	if apiSpec == nil {
		return ""
	}
	candidate, ok := firstCommandExampleCandidate(apiSpec.Resources)
	if !ok {
		return ""
	}
	item := strings.TrimSpace(candidate.endpoint.Response.Item)
	if item == "" {
		return ""
	}
	typeDef, ok := typeDefByName(apiSpec.Types, item)
	if !ok {
		return ""
	}
	return generatedExampleFields(typeDef)
}

func generatedExampleFields(typeDef spec.TypeDef) string {
	fields := make([]string, 0, generatedExampleFieldLimit)
	for _, field := range typeDef.Fields {
		name := strings.TrimSpace(field.Name)
		if !safeGeneratedExampleField(name) {
			continue
		}
		fields = append(fields, name)
		if len(fields) == generatedExampleFieldLimit {
			return strings.Join(fields, ",")
		}
	}
	return strings.Join(fields, ",")
}

func safeGeneratedExampleField(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		if strings.ContainsRune("_-", r) {
			continue
		}
		return false
	}
	return true
}

func endpointForSyncableResource(resources map[string]spec.Resource, syncable profiler.SyncableResource) (spec.Endpoint, bool) {
	type endpointCandidate struct {
		resourceName string
		endpoint     spec.Endpoint
	}

	var candidates []endpointCandidate
	var walk func(map[string]spec.Resource)
	walk = func(current map[string]spec.Resource) {
		resourceNames := make([]string, 0, len(current))
		for name := range current {
			resourceNames = append(resourceNames, name)
		}
		sort.Strings(resourceNames)
		for _, resourceName := range resourceNames {
			resource := current[resourceName]
			endpointNames := make([]string, 0, len(resource.Endpoints))
			for name := range resource.Endpoints {
				endpointNames = append(endpointNames, name)
			}
			sort.Strings(endpointNames)
			for _, endpointName := range endpointNames {
				candidates = append(candidates, endpointCandidate{
					resourceName: resourceName,
					endpoint:     resource.Endpoints[endpointName],
				})
			}
			walk(resource.SubResources)
		}
	}
	walk(resources)

	resourceName := strings.TrimSpace(syncable.Name)
	path := strings.TrimSpace(syncable.Path)
	method := strings.ToUpper(strings.TrimSpace(syncable.Method))
	for _, candidate := range candidates {
		if candidate.resourceName == resourceName && endpointMatches(candidate.endpoint, path, method) {
			return candidate.endpoint, true
		}
	}
	for _, candidate := range candidates {
		if endpointMatches(candidate.endpoint, path, method) {
			return candidate.endpoint, true
		}
	}
	for _, candidate := range candidates {
		if candidate.resourceName == resourceName {
			return candidate.endpoint, true
		}
	}
	return spec.Endpoint{}, false
}

func endpointMatches(endpoint spec.Endpoint, path, method string) bool {
	if path != "" && strings.TrimSpace(endpoint.Path) != path {
		return false
	}
	if method != "" && strings.ToUpper(strings.TrimSpace(endpoint.Method)) != method {
		return false
	}
	return true
}

func typeDefByName(types map[string]spec.TypeDef, name string) (spec.TypeDef, bool) {
	if typeDef, ok := types[name]; ok {
		return typeDef, true
	}
	for typeName, typeDef := range types {
		if strings.EqualFold(typeName, name) {
			return typeDef, true
		}
	}
	return spec.TypeDef{}, false
}
