package spec

import (
	"bytes"
	"strings"
)

// DetectOData reports whether a parsed spec should use OData v4 request
// semantics. raw may be nil; it lets OpenAPI callers preserve source-level
// clues that are not carried in APISpec after parsing.
func DetectOData(s *APISpec, raw []byte) bool {
	if s == nil {
		return false
	}
	if s.OData {
		return true
	}

	endpointText := strings.ToLower(s.BaseURL + " " + s.BasePath)
	if strings.Contains(endpointText, "/odata") {
		return true
	}

	rawLower := bytes.ToLower(raw)
	if bytes.Contains(rawLower, []byte("/odata")) ||
		bytes.Contains(rawLower, []byte("@odata.context")) ||
		bytes.Contains(rawLower, []byte("@odata.etag")) {
		return true
	}

	return mostlyValueWrappedCollections(s)
}

// ApplyODataConventions annotates OData function/action endpoints and adds the
// standard read query options. It is safe to call more than once.
func ApplyODataConventions(s *APISpec) {
	if s == nil || !s.OData {
		return
	}
	for resourceName, resource := range s.Resources {
		applyODataResourceConventions(&resource)
		s.Resources[resourceName] = resource
	}
}

func applyODataResourceConventions(resource *Resource) {
	if resource == nil {
		return
	}
	for endpointName, endpoint := range resource.Endpoints {
		applyODataEndpointConventions(endpointName, &endpoint)
		resource.Endpoints[endpointName] = endpoint
	}
	for subName, sub := range resource.SubResources {
		applyODataResourceConventions(&sub)
		resource.SubResources[subName] = sub
	}
}

func applyODataEndpointConventions(endpointName string, endpoint *Endpoint) {
	if endpoint == nil {
		return
	}

	if endpoint.ODataOperation == "" {
		endpoint.ODataOperation = inferODataOperation(*endpoint)
	}
	if endpoint.ODataOperation == ODataOperationAction && strings.EqualFold(endpoint.Method, "POST") {
		moveODataActionParamsToBody(endpoint)
	}
	if shouldAddODataQueryParams(endpointName, *endpoint) {
		addODataQueryParams(endpoint)
	}
	if len(endpoint.ResponseContentTypes) == 0 {
		endpoint.ResponseContentTypes = inferODataPrintResponseContentTypes(*endpoint)
	}
}

func inferODataOperation(endpoint Endpoint) string {
	text := strings.ToLower(strings.Join([]string{
		endpoint.Description,
		lastPathSegment(endpoint.Path),
	}, " "))
	switch {
	case strings.Contains(text, "odata action"):
		return ODataOperationAction
	case strings.Contains(text, "odata function"):
		return ODataOperationFunction
	case strings.Contains(text, " action") && strings.Contains(text, "odata"):
		return ODataOperationAction
	case strings.Contains(text, " function") && strings.Contains(text, "odata"):
		return ODataOperationFunction
	default:
		return ""
	}
}

func moveODataActionParamsToBody(endpoint *Endpoint) {
	bodyByName := make(map[string]struct{}, len(endpoint.Body))
	for _, p := range endpoint.Body {
		bodyByName[p.Name] = struct{}{}
	}

	filtered := endpoint.Params[:0]
	for _, p := range endpoint.Params {
		if p.Positional || p.PathParam {
			filtered = append(filtered, p)
			continue
		}
		if _, exists := bodyByName[p.Name]; exists {
			continue
		}
		bodyByName[p.Name] = struct{}{}
		endpoint.Body = append(endpoint.Body, p)
	}
	endpoint.Params = filtered
}

func shouldAddODataQueryParams(endpointName string, endpoint Endpoint) bool {
	if !strings.EqualFold(endpoint.Method, "GET") {
		return false
	}
	if endpoint.ODataOperation == ODataOperationFunction {
		return false
	}
	switch endpointName {
	case "list", "get":
		return true
	default:
		return false
	}
}

func addODataQueryParams(endpoint *Endpoint) {
	for _, p := range odataQueryParams() {
		if endpointHasPublicParam(*endpoint, p.Name, p.FlagName) {
			continue
		}
		endpoint.Params = append(endpoint.Params, p)
	}
}

func inferODataPrintResponseContentTypes(endpoint Endpoint) []string {
	text := strings.ToLower(endpoint.Path + " " + endpoint.Description)
	if !strings.Contains(text, "/print") && !strings.Contains(text, " print") {
		return nil
	}

	var formatParam *Param
	for i := range endpoint.Params {
		if strings.EqualFold(endpoint.Params[i].Name, "format") {
			formatParam = &endpoint.Params[i]
			break
		}
	}
	if formatParam == nil {
		for i := range endpoint.Body {
			if strings.EqualFold(endpoint.Body[i].Name, "format") {
				formatParam = &endpoint.Body[i]
				break
			}
		}
	}
	if formatParam == nil || len(formatParam.Enum) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	var out []string
	add := func(contentType string) {
		if contentType == "" {
			return
		}
		if _, ok := seen[contentType]; ok {
			return
		}
		seen[contentType] = struct{}{}
		out = append(out, contentType)
	}
	for _, value := range formatParam.Enum {
		lower := strings.ToLower(value)
		switch {
		case strings.Contains(lower, "pdf"):
			add("application/pdf")
		case strings.Contains(lower, "xlsx") || strings.Contains(lower, "excel"):
			add("application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		case strings.Contains(lower, "csv") || strings.Contains(lower, "delimited"):
			add("text/csv")
		}
	}
	return out
}

func endpointHasPublicParam(endpoint Endpoint, wireName, flagName string) bool {
	matches := func(p Param) bool {
		return p.Name == wireName || p.FlagName == flagName || p.PublicInputName() == flagName
	}
	for _, p := range endpoint.Params {
		if matches(p) {
			return true
		}
	}
	for _, p := range endpoint.Body {
		if matches(p) {
			return true
		}
	}
	return false
}

func odataQueryParams() []Param {
	return []Param{
		{Name: "$top", IdentName: "ODataTop", FlagName: "top", Type: "integer", Description: "OData $top query option"},
		{Name: "$skip", IdentName: "ODataSkip", FlagName: "skip", Type: "integer", Description: "OData $skip query option"},
		{Name: "$filter", IdentName: "ODataFilter", FlagName: "filter", Type: "string", Description: "OData $filter query option"},
		{Name: "$orderby", IdentName: "ODataOrderby", FlagName: "orderby", Type: "string", Description: "OData $orderby query option"},
		{Name: "$expand", IdentName: "ODataExpand", FlagName: "expand", Type: "string", Description: "OData $expand query option"},
		{Name: "$count", IdentName: "ODataCount", FlagName: "count", Type: "boolean", Description: "OData $count query option"},
		{Name: "$search", IdentName: "ODataSearch", FlagName: "search", Type: "string", Description: "OData $search query option"},
	}
}

func mostlyValueWrappedCollections(s *APISpec) bool {
	total := 0
	valueWrapped := 0
	walkSpecEndpoints(s, func(_ string, endpoint *Endpoint) {
		if endpoint == nil || !strings.EqualFold(endpoint.Method, "GET") {
			return
		}
		if endpoint.Response.Type != "array" {
			return
		}
		total++
		if endpoint.ResponsePath == "value" {
			valueWrapped++
		}
	})
	return total >= 3 && valueWrapped == total
}

func walkSpecEndpoints(s *APISpec, visit func(name string, endpoint *Endpoint)) {
	if s == nil {
		return
	}
	for resourceName, resource := range s.Resources {
		walkEndpointResource(resourceName, &resource, visit)
	}
}

func walkEndpointResource(prefix string, resource *Resource, visit func(name string, endpoint *Endpoint)) {
	if resource == nil {
		return
	}
	for endpointName, endpoint := range resource.Endpoints {
		visit(endpointName, &endpoint)
	}
	for subName, sub := range resource.SubResources {
		walkEndpointResource(prefix+"."+subName, &sub, visit)
	}
}

func lastPathSegment(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}
