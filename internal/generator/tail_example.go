package generator

import (
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/shellargs"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

const tailFollowOnceArg = "--follow=false"

// A syncable name with no parameter-free read path is dropped so the example
// is not rejected as unknown. A GET that still needs query arguments is
// dropped because the poll sends none and the live example would fail.
// Non-JSON bodies are dropped because tail emits NDJSON from a JSON response.
func tailPollResources(data visionRenderData) []string {
	exportable := exportableResources(data.APISpec)
	if len(exportable) == 0 || len(data.SyncableResources) == 0 {
		return nil
	}
	byFold := make(map[string]string, len(exportable))
	for _, name := range exportable {
		fold := strings.ToLower(name)
		if _, ok := byFold[fold]; !ok {
			byFold[fold] = name
		}
	}
	seen := map[string]struct{}{}
	names := make([]string, 0, len(data.SyncableResources))
	for _, resource := range data.SyncableResources {
		if isVestigialSyncResource(resource) {
			continue
		}
		raw := strings.TrimSpace(resource.Name)
		if raw == "" {
			continue
		}
		canonical, ok := byFold[strings.ToLower(raw)]
		if !ok {
			continue
		}
		if data.APISpec != nil {
			if specResource, found := data.Resources[canonical]; found && tailGetNeedsQueryArgs(specResource) {
				continue
			}
		}
		if _, dup := seen[canonical]; dup {
			continue
		}
		seen[canonical] = struct{}{}
		names = append(names, canonical)
	}
	sort.Strings(names)
	return names
}

// Client.Get is called with a nil query map, so a required query argument
// that is not already on the path never reaches the API.
func tailGetNeedsQueryArgs(resource spec.Resource) bool {
	endpoint, ok := resourceEndpointForMethod(resource, "GET")
	if !ok {
		return false
	}
	satisfied := pathQueryNames(endpoint.Path)
	for _, param := range endpoint.Params {
		if unsentRequiredQueryArg(param, satisfied) {
			return true
		}
	}
	return false
}

func unsentRequiredQueryArg(param spec.Param, satisfied map[string]struct{}) bool {
	if !param.Required || param.PathParam {
		return false
	}
	if loc := strings.TrimSpace(param.In); loc != "" && !strings.EqualFold(loc, "query") {
		return false
	}
	wire := param.WireName()
	if wire == "" {
		return false
	}
	_, present := satisfied[strings.ToLower(wire)]
	return !present
}

func pathQueryNames(path string) map[string]struct{} {
	_, query, ok := strings.Cut(path, "?")
	if !ok || query == "" {
		return nil
	}
	names := map[string]struct{}{}
	for part := range strings.SplitSeq(query, "&") {
		name, _, _ := strings.Cut(part, "=")
		if key := strings.ToLower(strings.TrimSpace(name)); key != "" {
			names[key] = struct{}{}
		}
	}
	return names
}

func tailPrimaryResource(data visionRenderData) string {
	names := tailPollResources(data)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func tailHappyArgs(data visionRenderData) string {
	name := tailPrimaryResource(data)
	if name == "" || !happyArgValueEncodable(name) {
		return ""
	}
	return "resource=" + escapeHappyArgValue(name) + ";" + tailFollowOnceArg
}

func tailCommandExample(data visionRenderData) string {
	cli := tailExampleBinary(data)
	name := tailPrimaryResource(data)
	if name == "" {
		return "  # List resources this CLI can tail\n  " + shellargs.Join([]string{cli, "tail", "--json"})
	}
	once := shellargs.Join([]string{cli, "tail", name, tailFollowOnceArg})
	interval := shellargs.Join([]string{cli, "tail", name, "--interval", "30s"})
	return "  # Poll one resource once and exit\n  " + once + "\n\n  # Tail a resource until interrupted\n  " + interval
}

func tailExampleBinary(data visionRenderData) string {
	if data.APISpec == nil {
		return "app-pp-cli"
	}
	name := strings.TrimSpace(data.Name)
	if name == "" {
		return "app-pp-cli"
	}
	return naming.CLI(name)
}
