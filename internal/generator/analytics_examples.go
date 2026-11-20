package generator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

const analyticsExampleGroupFieldLimit = 2

// analyticsExample is the resource type and group-by fields help may name.
// Type is empty when no syncable name is a single safe shell token; the
// help text then counts every stored type instead of passing --type.
type analyticsExample struct {
	Type    string
	GroupBy []string
}

type analyticsExampleResource struct {
	name   string
	path   string
	method string
}

func analyticsExampleFor(api *spec.APISpec, syncable []profiler.SyncableResource, dependent []profiler.DependentResource) analyticsExample {
	resource, ok := firstAnalyticsExampleResource(syncable, dependent)
	if !ok {
		return analyticsExample{}
	}
	return analyticsExample{
		Type:    resource.name,
		GroupBy: analyticsExampleGroupFields(api, resource),
	}
}

func firstAnalyticsExampleResource(syncable []profiler.SyncableResource, dependent []profiler.DependentResource) (analyticsExampleResource, bool) {
	if resource, ok := firstAnalyticsSyncable(syncable, false); ok {
		return resource, true
	}
	if resource, ok := firstAnalyticsSyncable(syncable, true); ok {
		return resource, true
	}
	candidates := make([]analyticsExampleResource, 0, len(dependent))
	for _, resource := range dependent {
		if !safeAnalyticsExampleToken(resource.Name) || isVestigialDependentSyncResource(resource) {
			continue
		}
		candidates = append(candidates, analyticsExampleResource{
			name:   resource.Name,
			path:   resource.Path,
			method: resource.Method,
		})
	}
	return firstSortedAnalyticsExample(candidates)
}

// allowSkipDefault is false for the resources a bare sync stores. Those are
// the example when any exist. Otherwise a resource that syncs only with
// --resources is still a real stored type.
func firstAnalyticsSyncable(syncable []profiler.SyncableResource, allowSkipDefault bool) (analyticsExampleResource, bool) {
	candidates := make([]analyticsExampleResource, 0, len(syncable))
	for _, resource := range syncable {
		if resource.SkipDefaultSync != allowSkipDefault {
			continue
		}
		if !safeAnalyticsExampleToken(resource.Name) || isVestigialSyncResource(resource) {
			continue
		}
		candidates = append(candidates, analyticsExampleResource{
			name:   resource.Name,
			path:   resource.Path,
			method: resource.Method,
		})
	}
	return firstSortedAnalyticsExample(candidates)
}

func firstSortedAnalyticsExample(candidates []analyticsExampleResource) (analyticsExampleResource, bool) {
	if len(candidates) == 0 {
		return analyticsExampleResource{}, false
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].name < candidates[j].name })
	return candidates[0], true
}

func analyticsExampleGroupFields(api *spec.APISpec, resource analyticsExampleResource) []string {
	endpoint, ok := analyticsExampleEndpoint(api, resource)
	if !ok {
		return nil
	}
	item := strings.TrimSpace(endpoint.Response.Item)
	if item == "" {
		return nil
	}
	typeDef, ok := typeDefByName(api.Types, item)
	if !ok {
		return nil
	}
	fields := make([]string, 0, analyticsExampleGroupFieldLimit)
	seen := make(map[string]struct{}, analyticsExampleGroupFieldLimit)
	for _, field := range typeDef.Fields {
		name := strings.TrimSpace(field.Name)
		if !safeAnalyticsExampleToken(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		fields = append(fields, name)
		if len(fields) == analyticsExampleGroupFieldLimit {
			break
		}
	}
	return fields
}

// analyticsCompiledResourceTypes is every name sync can write, including
// names that are not safe to drop unquoted into a help line.
func analyticsCompiledResourceTypes(syncable []profiler.SyncableResource, dependent []profiler.DependentResource) []string {
	seen := make(map[string]struct{}, len(syncable)+len(dependent))
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		seen[name] = struct{}{}
	}
	for _, resource := range syncable {
		add(resource.Name)
	}
	for _, resource := range dependent {
		add(resource.Name)
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func analyticsHelpExample(cliName string, example analyticsExample) string {
	cliName = strings.TrimSpace(cliName)
	if cliName == "" {
		cliName = "cli"
	}
	bin := cliName + "-pp-cli"
	if example.Type == "" {
		return fmt.Sprintf("  # Summarize locally synced resources\n  %s analytics\n\n  # Machine-readable summary\n  %s analytics --json", bin, bin)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  # Count records by type\n  %s analytics --type %s", bin, example.Type)
	if len(example.GroupBy) == 0 {
		fmt.Fprintf(&b, "\n\n  # Count as JSON\n  %s analytics --type %s --json", bin, example.Type)
		return b.String()
	}
	fmt.Fprintf(&b, "\n\n  # Group by a field\n  %s analytics --type %s --group-by %s", bin, example.Type, example.GroupBy[0])
	limitField := example.GroupBy[0]
	if len(example.GroupBy) > 1 {
		limitField = example.GroupBy[1]
	}
	fmt.Fprintf(&b, "\n\n  # Top 10 most frequent values\n  %s analytics --type %s --group-by %s --limit 10 --json", bin, example.Type, limitField)
	return b.String()
}

func safeAnalyticsExampleToken(name string) bool {
	return safeGeneratedExampleField(name) && !strings.HasPrefix(name, "-")
}

// analyticsExampleEndpoint returns the list (or other sync) endpoint for a
// stored type. The path may be the spec path or the same path with a resource
// BaseURL prefix. A different method on the same resource is not a match:
// its response fields are not what sync stores.
func analyticsExampleEndpoint(api *spec.APISpec, want analyticsExampleResource) (spec.Endpoint, bool) {
	if api == nil {
		return spec.Endpoint{}, false
	}
	wantName := strings.TrimSpace(want.name)
	wantPath := strings.TrimSpace(want.path)
	wantMethod := strings.ToUpper(strings.TrimSpace(want.method))
	if wantName == "" {
		return spec.Endpoint{}, false
	}
	var found spec.Endpoint
	ok := false
	var walk func(parent spec.Resource, resources map[string]spec.Resource, isSub bool)
	walk = func(parent spec.Resource, resources map[string]spec.Resource, isSub bool) {
		if ok || len(resources) == 0 {
			return
		}
		names := make([]string, 0, len(resources))
		for name := range resources {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			resource := resources[name]
			if analyticsResourceNameMatches(name, wantName) {
				endpointNames := make([]string, 0, len(resource.Endpoints))
				for endpointName := range resource.Endpoints {
					endpointNames = append(endpointNames, endpointName)
				}
				sort.Strings(endpointNames)
				for _, endpointName := range endpointNames {
					endpoint := resource.Endpoints[endpointName]
					if wantMethod != "" && strings.ToUpper(strings.TrimSpace(endpoint.Method)) != wantMethod {
						continue
					}
					raw := strings.TrimSpace(endpoint.Path)
					effective := analyticsEndpointPath(parent, resource, endpoint, isSub)
					if wantPath != "" && wantPath != raw && wantPath != effective {
						continue
					}
					found = endpoint
					ok = true
					return
				}
			}
			walk(resource, resource.SubResources, true)
			if ok {
				return
			}
		}
	}
	walk(spec.Resource{}, api.Resources, false)
	return found, ok
}

func analyticsResourceNameMatches(specName, want string) bool {
	return specName == want || strings.EqualFold(specName, want) || spec.ToSnakeCase(specName) == want
}

func analyticsEndpointPath(parent, resource spec.Resource, endpoint spec.Endpoint, isSub bool) string {
	if isSub {
		return strings.TrimSpace(spec.EffectiveSubEndpointPath(parent, resource, endpoint))
	}
	return strings.TrimSpace(spec.EffectiveEndpointPath(resource, endpoint))
}
