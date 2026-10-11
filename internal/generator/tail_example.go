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
// is not rejected as unknown. A GET that still needs a query argument the
// client does not add is dropped because the poll sends a nil query map and
// the live example would fail. Non-JSON bodies are dropped because tail
// emits NDJSON from a JSON response.
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
			if specResource, found := data.Resources[canonical]; found && tailGetNeedsQueryArgs(data.APISpec, specResource) {
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

// Client.Get is called with a nil query map. Path query keys and query
// credentials the client attaches on its own are already on the request.
func tailGetNeedsQueryArgs(api *spec.APISpec, resource spec.Resource) bool {
	endpoint, ok := resourceEndpointForMethod(resource, "GET")
	if !ok {
		return false
	}
	satisfied := pathQueryNames(endpoint.Path)
	for name := range clientSuppliedQueryNames(api, endpoint) {
		if satisfied == nil {
			satisfied = map[string]struct{}{}
		}
		satisfied[name] = struct{}{}
	}
	for _, param := range endpoint.Params {
		if unsentRequiredQueryArg(param, satisfied) {
			return true
		}
	}
	return false
}

func clientSuppliedQueryNames(api *spec.APISpec, endpoint spec.Endpoint) map[string]struct{} {
	// The envelope path is built from the caller's query map. Auth query
	// keys are attached to the outer proxy URL after that, so they never
	// satisfy a required parameter of the inner request.
	if api == nil || api.ClientPattern == "proxy-envelope" || !hasAuth(api.Auth) {
		return nil
	}
	names := map[string]struct{}{}
	add := func(name string) {
		key := strings.TrimSpace(name)
		if key == "" {
			return
		}
		names[key] = struct{}{}
	}
	if name, ok := primaryAuthQueryName(api, endpoint); ok {
		add(name)
	}
	for _, extra := range api.Auth.AdditionalHeaders {
		if additionalAuthQuerySent(api, endpoint, extra) {
			add(extra.Header)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func primaryAuthQueryName(api *spec.APISpec, endpoint spec.Endpoint) (string, bool) {
	if !sendsPrimaryCredential(api, endpoint) {
		return "", false
	}
	auth := api.Auth
	// Session handshake attaches TokenParamName and does not reach the
	// Auth.In query branch. Tier routing resolves the empty tier through
	// the primary placement instead, so that branch is absent there.
	if auth.Type == "session_handshake" && !api.HasTierRouting() {
		if strings.TrimSpace(auth.TokenParamIn) == "header" {
			return "", false
		}
		name := strings.TrimSpace(auth.TokenParamName)
		return name, name != ""
	}
	if authPlacement(auth) != "query" {
		return "", false
	}
	return authParameterName(auth), true
}

func sendsPrimaryCredential(api *spec.APISpec, endpoint spec.Endpoint) bool {
	if !api.HasPerOperationAuth() {
		return true
	}
	scheme := strings.TrimSpace(endpoint.AuthScheme)
	return scheme == "" || scheme == strings.TrimSpace(api.Auth.Scheme)
}

func additionalAuthQuerySent(api *spec.APISpec, endpoint spec.Endpoint, extra spec.AdditionalAuthHeader) bool {
	if strings.TrimSpace(extra.In) != "query" || strings.TrimSpace(extra.Header) == "" {
		return false
	}
	if !api.HasPerOperationAuth() {
		return true
	}
	scheme := strings.TrimSpace(endpoint.AuthScheme)
	extraScheme := strings.TrimSpace(extra.Scheme)
	if extra.PerOperation {
		return scheme != "" && scheme == extraScheme
	}
	return scheme == "" || scheme == extraScheme
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
	_, present := satisfied[wire]
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
		if key := strings.TrimSpace(name); key != "" {
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
