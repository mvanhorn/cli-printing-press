package browsersniff

import (
	"net/url"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/internal/spec"
)

func ApplyReachabilityDefaults(apiSpec *spec.APISpec, analysis *TrafficAnalysis) {
	if apiSpec == nil || analysis == nil || analysis.Reachability == nil {
		return
	}

	if analysis.Reachability.Mode == "browser_http" || analysis.Reachability.Mode == "browser_clearance_http" || analysis.Reachability.Mode == "browser_required" {
		if apiSpec.HTTPTransport == "" {
			switch analysis.Reachability.Mode {
			case "browser_clearance_http":
				apiSpec.HTTPTransport = spec.HTTPTransportBrowserChromeH3
			case "browser_http":
				apiSpec.HTTPTransport = spec.HTTPTransportBrowserChrome
			}
		}
	}

	if analysis.Reachability.Mode != "browser_clearance_http" {
		return
	}

	if apiSpec.Auth.BrowserSessionReason == "" {
		apiSpec.Auth.BrowserSessionReason = "browser clearance is required to replay captured website traffic"
	}
	if apiSpec.Auth.BrowserSessionValidationPath == "" {
		apiSpec.Auth.BrowserSessionValidationPath = firstBrowserSessionValidationPath(apiSpec)
	}
	if apiSpec.Auth.BrowserSessionValidationMethod == "" && apiSpec.Auth.BrowserSessionValidationPath != "" {
		apiSpec.Auth.BrowserSessionValidationMethod = "GET"
	}
	if (apiSpec.Auth.Type == "cookie" || apiSpec.Auth.Type == "composed") && apiSpec.Auth.BrowserSessionValidationPath != "" {
		apiSpec.Auth.RequiresBrowserSession = true
	}

	if hasExplicitAuth(apiSpec.Auth) {
		return
	}

	domain := reachabilityCookieDomain(apiSpec, analysis)
	if domain == "" {
		return
	}

	validationPath := firstBrowserSessionValidationPath(apiSpec)
	apiSpec.Auth = spec.AuthConfig{
		Type:                         "cookie",
		Header:                       "Cookie",
		In:                           "cookie",
		CookieDomain:                 domain,
		EnvVars:                      envVarsOrNil(strings.ToUpper(strings.ReplaceAll(apiSpec.Name, "-", "_")), "COOKIES"),
		RequiresBrowserSession:       validationPath != "",
		BrowserSessionReason:         "browser clearance is required to replay captured website traffic",
		BrowserSessionValidationPath: validationPath,
	}
	if validationPath != "" {
		apiSpec.Auth.BrowserSessionValidationMethod = "GET"
	}
}

func hasExplicitAuth(auth spec.AuthConfig) bool {
	return strings.TrimSpace(auth.Type) != "" && auth.Type != "none"
}

func reachabilityCookieDomain(apiSpec *spec.APISpec, analysis *TrafficAnalysis) string {
	for _, raw := range []string{analysis.Summary.TargetURL, apiSpec.WebsiteURL, apiSpec.BaseURL} {
		host := hostname(raw)
		if host != "" {
			return "." + strings.TrimPrefix(host, ".")
		}
	}
	return ""
}

func hostname(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
}

func firstBrowserSessionValidationPath(apiSpec *spec.APISpec) string {
	if apiSpec == nil {
		return ""
	}
	resourceNames := make([]string, 0, len(apiSpec.Resources))
	for name := range apiSpec.Resources {
		resourceNames = append(resourceNames, name)
	}
	sort.Strings(resourceNames)
	for _, name := range resourceNames {
		if path := firstValidationPathInResource(apiSpec.Resources[name]); path != "" {
			return path
		}
	}
	return ""
}

func firstValidationPathInResource(resource spec.Resource) string {
	endpointNames := make([]string, 0, len(resource.Endpoints))
	for name := range resource.Endpoints {
		endpointNames = append(endpointNames, name)
	}
	sort.Strings(endpointNames)
	for _, name := range endpointNames {
		endpoint := resource.Endpoints[name]
		if !strings.EqualFold(endpoint.Method, "GET") || endpoint.Path == "" || hasRequiredInput(endpoint) {
			continue
		}
		return endpoint.Path
	}

	subNames := make([]string, 0, len(resource.SubResources))
	for name := range resource.SubResources {
		subNames = append(subNames, name)
	}
	sort.Strings(subNames)
	for _, name := range subNames {
		if path := firstValidationPathInResource(resource.SubResources[name]); path != "" {
			return path
		}
	}
	return ""
}

func hasRequiredInput(endpoint spec.Endpoint) bool {
	for _, param := range endpoint.Params {
		if param.Required && param.Default == nil {
			return true
		}
	}
	for _, param := range endpoint.Body {
		if param.Required && param.Default == nil {
			return true
		}
	}
	return false
}
