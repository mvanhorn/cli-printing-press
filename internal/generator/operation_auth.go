package generator

import (
	"sort"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

type operationAuthRoute struct {
	Method string
	Path   string
	Scheme string
}

type perOperationDoctorScheme struct {
	Name        string
	ConfigField string
	Primary     bool
}

func operationAuthRoutes(api *spec.APISpec) []operationAuthRoute {
	if api == nil {
		return nil
	}
	var routes []operationAuthRoute
	for _, resName := range sortedKeys(api.Resources) {
		collectOperationAuthRoutes(&routes, api.Resources[resName])
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Method != routes[j].Method {
			return routes[i].Method < routes[j].Method
		}
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}
		return routes[i].Scheme < routes[j].Scheme
	})
	return routes
}

func collectOperationAuthRoutes(routes *[]operationAuthRoute, root spec.Resource) {
	for _, epName := range sortedKeys(root.Endpoints) {
		endpoint := root.Endpoints[epName]
		if endpoint.AuthScheme == "" {
			continue
		}
		*routes = append(*routes, operationAuthRoute{
			Method: endpoint.Method,
			Path:   effectiveEndpointPath(root, endpoint),
			Scheme: endpoint.AuthScheme,
		})
	}
	collectSubOperationAuthRoutes(routes, root, root.SubResources)
}

func collectSubOperationAuthRoutes(routes *[]operationAuthRoute, root spec.Resource, subs map[string]spec.Resource) {
	for _, subName := range sortedKeys(subs) {
		sub := subs[subName]
		for _, epName := range sortedKeys(sub.Endpoints) {
			endpoint := sub.Endpoints[epName]
			if endpoint.AuthScheme == "" {
				continue
			}
			*routes = append(*routes, operationAuthRoute{
				Method: endpoint.Method,
				Path:   effectiveSubEndpointPath(root, sub, endpoint),
				Scheme: endpoint.AuthScheme,
			})
		}
		collectSubOperationAuthRoutes(routes, root, sub.SubResources)
	}
}

func perOperationDoctorSchemes(api *spec.APISpec) []perOperationDoctorScheme {
	if api == nil || !api.HasPerOperationAuth() {
		return nil
	}
	var schemes []perOperationDoctorScheme
	if api.Auth.Scheme != "" {
		schemes = append(schemes, perOperationDoctorScheme{
			Name:    api.Auth.Scheme,
			Primary: true,
		})
	}
	for _, header := range api.Auth.AdditionalHeaders {
		if !header.PerOperation || header.Scheme == "" {
			continue
		}
		schemes = append(schemes, perOperationDoctorScheme{
			Name:        header.Scheme,
			ConfigField: resolveEnvVarField(header.EnvVar.Name),
		})
	}
	sort.Slice(schemes, func(i, j int) bool {
		return schemes[i].Name < schemes[j].Name
	})
	return schemes
}

func hasNonPerOperationAdditionalHeaders(auth spec.AuthConfig) bool {
	for _, header := range auth.AdditionalHeaders {
		if !header.PerOperation {
			return true
		}
	}
	return false
}

func additionalHeaderEnvCount(auth spec.AuthConfig) int {
	count := 0
	for _, header := range auth.AdditionalHeaders {
		if !header.PerOperation {
			count++
		}
	}
	return count
}
