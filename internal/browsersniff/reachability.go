package browsersniff

import (
	"net/url"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

func ApplyReachabilityDefaults(apiSpec *spec.APISpec, analysis *TrafficAnalysis) {
	if apiSpec == nil || analysis == nil || analysis.Reachability == nil {
		return
	}

	if analysis.Reachability.Mode == "html_scrape" && analysis.Reachability.HTMLExtractSignature != "" {
		applyHTMLScrapeExtractionDefaults(apiSpec, analysis.Reachability.HTMLExtractSignature)
	}

	// browser_required is the "no Go-client replay possible" mode -- the
	// operator must drive a real browser. It intentionally leaves
	// HTTPTransport empty so downstream code does not try to wire a surf
	// client. The HAR-driven transport mapping applies only to modes
	// where the captured traffic is replayable.
	switch analysis.Reachability.Mode {
	case "browser_http", "browser_clearance_http":
		if apiSpec.HTTPTransport == "" {
			apiSpec.HTTPTransport = browserTransportFromHARVersion(analysis.Summary.HTTPVersionDistribution)
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

// scriptSelectorForSignature maps the SSR state-blob signature to the
// runtime extractor's "tag" / "tag#id" / "tag.class" grammar. Empty
// for window.__-style inline state — the runtime falls back to
// DefaultEmbeddedJSONScriptSelector and the operator hand-tunes.
func scriptSelectorForSignature(signature string) string {
	switch signature {
	case SSRSignatureNextData:
		return spec.DefaultEmbeddedJSONScriptSelector
	case SSRSignatureNuxt:
		return "script#__NUXT__"
	case SSRSignatureAppInitialState:
		return "script#__APP_INITIAL_STATE__"
	case SSRSignatureStateView:
		return "script.state-view"
	case SSRSignatureLDJSON:
		return `script[type="application/ld+json"]`
	default:
		return ""
	}
}

// applyHTMLScrapeExtractionDefaults promotes endpoints that already
// declared HTMLExtract to embedded-json mode with a signature-derived
// selector. Endpoints without HTMLExtract are skipped because the
// signature alone is not enough to fabricate extraction config for
// endpoints that never asked for HTML extraction.
func applyHTMLScrapeExtractionDefaults(apiSpec *spec.APISpec, signature string) {
	selector := scriptSelectorForSignature(signature)
	for resourceName, resource := range apiSpec.Resources {
		updated := promoteHTMLExtractInResource(resource, selector)
		apiSpec.Resources[resourceName] = updated
	}
}

func promoteHTMLExtractInResource(resource spec.Resource, selector string) spec.Resource {
	for name, endpoint := range resource.Endpoints {
		if endpoint.HTMLExtract == nil {
			continue
		}
		endpoint.HTMLExtract.Mode = spec.HTMLExtractModeEmbeddedJSON
		if selector != "" {
			endpoint.HTMLExtract.ScriptSelector = selector
		}
		endpoint.HTMLExtract.LinkPrefixes = nil
		resource.Endpoints[name] = endpoint
	}
	for subName, sub := range resource.SubResources {
		resource.SubResources[subName] = promoteHTMLExtractInResource(sub, selector)
	}
	return resource
}
