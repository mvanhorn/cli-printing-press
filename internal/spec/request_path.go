package spec

import "strings"

// EffectiveEndpointPath is the request target generated commands send.
// The endpoint base wins over the resource base.
func EffectiveEndpointPath(resource Resource, endpoint Endpoint) string {
	return EndpointPathWithBase(EffectiveEndpointBaseURL(resource, endpoint), endpoint.Path)
}

// EffectiveSubEndpointPath joins the base that wins for a nested endpoint:
// the endpoint base, then the sub-resource base, then the parent resource base.
func EffectiveSubEndpointPath(parent, sub Resource, endpoint Endpoint) string {
	return EndpointPathWithBase(EffectiveSubEndpointBaseURL(parent, sub, endpoint), endpoint.Path)
}

// EffectiveEndpointBaseURL is the endpoint base when one is set, otherwise the resource base.
func EffectiveEndpointBaseURL(resource Resource, endpoint Endpoint) string {
	baseURL := endpoint.BaseURL
	if baseURL == "" {
		baseURL = resource.BaseURL
	}
	return strings.TrimRight(baseURL, "/")
}

// EffectiveSubEndpointBaseURL walks endpoint, sub-resource, then parent.
func EffectiveSubEndpointBaseURL(parent, sub Resource, endpoint Endpoint) string {
	baseURL := endpoint.BaseURL
	if baseURL == "" {
		baseURL = sub.BaseURL
	}
	if baseURL == "" {
		baseURL = parent.BaseURL
	}
	return strings.TrimRight(baseURL, "/")
}

// EndpointPathWithBase joins a base onto a spec path. An http or https path
// is already a full request target, so the base is left off. The generated
// client skips its own base when the path is absolute.
func EndpointPathWithBase(baseURL, path string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" || isAbsoluteRequestPath(path) {
		return path
	}
	return baseURL + path
}
