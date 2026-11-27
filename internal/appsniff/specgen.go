package appsniff

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/discovery"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// BuildSpec converts a DiscoveryResult into a validated Printing Press APISpec.
// Reserved Cobra command names (spec.ReservedCobraUseNames) are automatically
// disambiguated so `cli-printing-press generate` succeeds without manual edits.
func BuildSpec(res *DiscoveryResult, opts Options) (*spec.APISpec, error) {
	if res == nil {
		return nil, fmt.Errorf("discovery result is required")
	}

	endpoints := make([]Endpoint, 0, len(res.Endpoints)+len(res.GRPCEndpoints))
	endpoints = append(endpoints, res.Endpoints...)
	if opts.IncludeGRPC {
		endpoints = append(endpoints, res.GRPCEndpoints...)
	}
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no endpoints discovered for %s", res.Target)
	}

	displayName := strings.TrimSpace(opts.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(res.Meta.AppName)
	}
	if displayName == "" && res.Meta.PackageID != "" {
		parts := strings.Split(res.Meta.PackageID, ".")
		displayName = parts[len(parts)-1]
	}
	if displayName == "" {
		displayName = strings.TrimSuffix(filepath.Base(res.Target), filepath.Ext(res.Target))
	}

	apiName := naming.Slug(displayName)
	if apiName == "" {
		return nil, fmt.Errorf("could not derive a valid API slug from %q", displayName)
	}
	if apiName == displayName {
		displayName = ""
	}

	baseURL := strings.TrimSpace(opts.BaseURL)
	if baseURL == "" {
		baseURL = res.PrimaryBaseURL
	}
	if baseURL == "" {
		return nil, fmt.Errorf("could not determine an HTTPS API base URL for %q; pass --base-url https://api.example.com", apiName)
	}
	if parsed, err := url.Parse(baseURL); err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" {
		return nil, fmt.Errorf("base URL must be a valid HTTPS URL: %s", baseURL)
	}

	primaryHost := ""
	if u, err := url.Parse(baseURL); err == nil {
		primaryHost = strings.ToLower(u.Hostname())
	}

	resources := make(map[string]spec.Resource)
	for _, ep := range endpoints {
		resourceKey, resourceLabel := safeResourceKey(ep.Path)
		resource := resources[resourceKey]
		if resource.Description == "" {
			resource.Description = fmt.Sprintf("Operations on %s", resourceLabel)
		}
		if resource.Endpoints == nil {
			resource.Endpoints = make(map[string]spec.Endpoint)
		}

		specEp := buildSpecEndpoint(ep, primaryHost)
		epName := discovery.EndpointName(specEp.Method, specEp.Path)
		if epName == "" {
			epName = strings.ToLower(specEp.Method) + "_endpoint"
		}
		if _, exists := resource.Endpoints[epName]; exists {
			epName = discovery.UniqueEndpointName(resource.Endpoints, epName)
		}
		resource.Endpoints[epName] = specEp
		resources[resourceKey] = resource
	}

	authCfg := buildSpecAuthConfig(apiName, primaryHost, res.Auth)
	desc := fmt.Sprintf("Discovered API spec for %s (app-sniff %s)", apiName, res.TargetKind)
	if res.Meta.Version != "" {
		desc = fmt.Sprintf("Discovered API spec for %s v%s (app-sniff %s)", apiName, res.Meta.Version, res.TargetKind)
	}

	apiSpec := &spec.APISpec{
		Name:        apiName,
		DisplayName: displayName,
		Description: desc,
		Version:     "0.1.0",
		BaseURL:     baseURL,
		SpecSource:  "sniffed",
		Auth:        authCfg,
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   fmt.Sprintf("~/.config/%s-pp-cli/config.toml", apiName),
		},
		Resources: resources,
		Types:     map[string]spec.TypeDef{},
	}

	if err := apiSpec.Validate(); err != nil {
		return nil, fmt.Errorf("validating generated app-sniff spec: %w", err)
	}
	return apiSpec, nil
}

func safeResourceKey(path string) (string, string) {
	key, label := discovery.ResourceKey(path)
	key = naming.Slug(key)
	if key == "" {
		key = "api"
		label = "api"
	}
	if isReservedSpecResource(key) {
		// Disambiguate against Printing Press's own reserved Cobra verbs and
		// generator template resource names (e.g. "client", "search", "auth").
		if cand := spec.ReservedResourceParentPrefixCandidate(key, path); cand != "" {
			candSlug := naming.Slug(cand)
			if candSlug != "" && !isReservedSpecResource(candSlug) {
				return candSlug, label
			}
		}
		key = "api-" + key
	}
	if label == "" {
		label = key
	}
	return key, label
}

func isReservedSpecResource(name string) bool {
	if _, reserved := spec.ReservedCobraUseNames[name]; reserved {
		return true
	}
	var dummy spec.APISpec
	if dummy.ConflictsWithReservedCLIResourceName(name) {
		return true
	}
	return dummy.ParseTimeReservedCobraUseName(spec.NormalizeCobraCommandName(name))
}

func buildSpecEndpoint(ep Endpoint, primaryHost string) spec.Endpoint {
	meta := map[string]string{
		"source":     ep.Source,
		"protocol":   string(ep.Protocol),
		"confidence": string(ep.Confidence),
	}
	if ep.ReconcileState != ReconcileNone {
		meta["reconcile_state"] = string(ep.ReconcileState)
	}

	var params []spec.Param
	var body []spec.Param
	seenParam := make(map[string]bool)
	seenBody := make(map[string]bool)

	for _, p := range ep.Params {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		typ := p.Type
		if typ == "" {
			typ = "string"
		}
		switch p.In {
		case ParamInBody:
			if !seenBody[name] {
				seenBody[name] = true
				body = append(body, spec.Param{Name: name, Type: typ, Required: p.Required})
			}
		case ParamInPath:
			if !seenParam["path:"+name] {
				seenParam["path:"+name] = true
				params = append(params, spec.Param{
					Name:       name,
					In:         "path",
					Type:       typ,
					Required:   true,
					Positional: false,
				})
			}
		case ParamInHeader:
			// Never include credential headers as regular endpoint flags.
			lh := strings.ToLower(name)
			if lh == "authorization" || lh == "cookie" || lh == "x-api-key" {
				continue
			}
			if !seenParam["header:"+name] {
				seenParam["header:"+name] = true
				params = append(params, spec.Param{
					Name: name,
					In:   "header",
					Type: typ,
				})
			}
		default:
			if !seenParam["query:"+name] {
				seenParam["query:"+name] = true
				params = append(params, spec.Param{
					Name: name,
					In:   "query",
					Type: typ,
				})
			}
		}
	}

	for _, bk := range ep.BodyKeys {
		bk = strings.TrimSpace(bk)
		if bk != "" && !seenBody[bk] {
			seenBody[bk] = true
			body = append(body, spec.Param{Name: bk, Type: "string"})
		}
	}

	// Preserve shard/secondary host on per-endpoint BaseURL when different from primary.
	epBaseURL := ""
	if ep.BaseURL != "" && ep.Host != "" && primaryHost != "" && !strings.EqualFold(ep.Host, primaryHost) {
		epBaseURL = ep.BaseURL
	}

	return spec.Endpoint{
		Method:       ep.Method,
		Path:         ep.Path,
		BaseURL:      epBaseURL,
		Description:  fmt.Sprintf("%s %s", ep.Method, ep.Path),
		Params:       params,
		Body:         body,
		ObservedAuth: ep.ObservedAuth,
		Meta:         meta,
	}
}

func buildSpecAuthConfig(apiName, primaryHost string, sig AuthSignal) spec.AuthConfig {
	prefix := envPrefix(apiName)
	switch sig.Scheme {
	case AuthOAuth:
		cfg := spec.AuthConfig{
			Type:     spec.TierAuthTypeBearerToken,
			Header:   "Authorization",
			Prefix:   "Bearer",
			In:       spec.TierAuthPlacementHeader,
			EnvVars:  []string{prefix + "_TOKEN"},
			Inferred: true,
		}
		if len(sig.TokenURLs) > 0 {
			cfg.TokenURL = sig.TokenURLs[0]
		}
		return cfg
	case AuthBearer:
		return spec.AuthConfig{
			Type:     spec.TierAuthTypeBearerToken,
			Header:   "Authorization",
			Prefix:   "Bearer",
			In:       spec.TierAuthPlacementHeader,
			EnvVars:  []string{prefix + "_TOKEN"},
			Inferred: true,
		}
	case AuthAPIKey:
		hdr := "X-API-Key"
		for _, h := range sig.HeaderNames {
			if strings.Contains(h, "key") {
				hdr = h
				break
			}
		}
		return spec.AuthConfig{
			Type:     spec.TierAuthTypeAPIKey,
			Header:   hdr,
			In:       spec.TierAuthPlacementHeader,
			EnvVars:  []string{prefix + "_API_KEY"},
			Inferred: true,
		}
	case AuthCookie:
		return spec.AuthConfig{
			Type:         "cookie",
			CookieDomain: primaryHost,
			Cookies:      sig.CookieNames,
			EnvVars:      []string{prefix + "_COOKIE"},
			Inferred:     true,
		}
	default:
		return spec.AuthConfig{Type: spec.TierAuthTypeNone}
	}
}

func envPrefix(apiName string) string {
	var b strings.Builder
	for _, r := range apiName {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(strings.ToUpper(b.String()), "_")
	if out == "" {
		return "API"
	}
	return out
}

// BuildTrafficAnalysis emits a schema-valid browsersniff.TrafficAnalysis sidecar
// for an app-sniff DiscoveryResult so downstream Printing Press phases and
// `cli-printing-press schema traffic-analysis` consume it natively.
func BuildTrafficAnalysis(res *DiscoveryResult, apiSpec *spec.APISpec) *browsersniff.TrafficAnalysis {
	hostDist := make(map[string]int)
	protoCounts := make(map[ProtocolKind]int)
	clusters := make([]browsersniff.EndpointCluster, 0, len(res.Endpoints))

	for _, ep := range res.Endpoints {
		h := ep.Host
		if h == "" && res.PrimaryBaseURL != "" {
			if u, err := url.Parse(res.PrimaryBaseURL); err == nil {
				h = strings.ToLower(u.Hostname())
			}
		}
		if h != "" {
			hostDist[h]++
		}
		protoCounts[ep.Protocol]++

		count := ep.SampleCount
		if count <= 0 {
			count = 1
		}
		var reqFields []browsersniff.ShapeField
		for _, bk := range ep.BodyKeys {
			reqFields = append(reqFields, browsersniff.ShapeField{Name: bk, Type: "string"})
		}
		clusters = append(clusters, browsersniff.EndpointCluster{
			Host:         h,
			Method:       ep.Method,
			Path:         ep.Path,
			Count:        count,
			Statuses:     ep.Statuses,
			ObservedAuth: ep.ObservedAuth,
			RequestShape: browsersniff.ShapeSummary{
				Kind:   "object",
				Fields: reqFields,
			},
			ResponseShape: browsersniff.ShapeSummary{Kind: "object"},
		})
	}

	// Build protocol observations.
	protocols := make([]browsersniff.ProtocolObservation, 0, len(protoCounts)+1)
	protoKeys := make([]ProtocolKind, 0, len(protoCounts))
	for k := range protoCounts {
		protoKeys = append(protoKeys, k)
	}
	slices.Sort(protoKeys)
	for _, pk := range protoKeys {
		protocols = append(protocols, browsersniff.ProtocolObservation{
			Label:      string(pk),
			Confidence: 0.85,
			Evidence: []browsersniff.EvidenceRef{{
				EntryIndex: browsersniff.EvidenceRefStringSentinel,
				Reason:     fmt.Sprintf("app-sniff (%s) discovered %d %s endpoint(s)", res.TargetKind, protoCounts[pk], pk),
			}},
		})
	}
	if len(res.GRPCEndpoints) > 0 && protoCounts[ProtocolGRPC] == 0 {
		protocols = append(protocols, browsersniff.ProtocolObservation{
			Label:      string(ProtocolGRPC),
			Confidence: 0.8,
			Evidence: []browsersniff.EvidenceRef{{
				EntryIndex: browsersniff.EvidenceRefStringSentinel,
				Reason:     fmt.Sprintf("app-sniff discovered %d gRPC/Protobuf service path(s) (excluded from default REST spec; pass --include-grpc to include)", len(res.GRPCEndpoints)),
			}},
		})
	}

	// Auth candidates.
	var authCandidates []browsersniff.AuthCandidate
	if res.Auth.Scheme != AuthNone {
		authType := string(res.Auth.Scheme)
		if res.Auth.Scheme == AuthOAuth {
			authType = spec.TierAuthTypeBearerToken
		}
		var domainHints []string
		if res.PrimaryBaseURL != "" {
			if u, err := url.Parse(res.PrimaryBaseURL); err == nil && u.Hostname() != "" {
				domainHints = []string{strings.ToLower(u.Hostname())}
			}
		}
		authCandidates = append(authCandidates, browsersniff.AuthCandidate{
			Type:        authType,
			Confidence:  0.75,
			HeaderNames: res.Auth.HeaderNames,
			CookieNames: res.Auth.CookieNames,
			DomainHints: domainHints,
		})
	}

	// Protections (TLS pinning, FairPlay DRM).
	var protections []browsersniff.ProtectionObservation
	for _, p := range res.Pinning {
		notes := []string{p.Detail}
		if len(p.Domains) > 0 {
			notes = append(notes, "pinned domains: "+strings.Join(p.Domains, ", "))
		}
		protections = append(protections, browsersniff.ProtectionObservation{
			Label:      p.Kind,
			Confidence: 0.9,
			Notes:      notes,
		})
	}
	if res.Meta.Encrypted {
		protections = append(protections, browsersniff.ProtectionObservation{
			Label:      "fairplay-drm",
			Confidence: 1.0,
			Notes:      []string{res.Meta.EncryptionNote},
		})
	}

	// Secondary hosts.
	primaryHost := ""
	if res.PrimaryBaseURL != "" {
		if u, err := url.Parse(res.PrimaryBaseURL); err == nil {
			primaryHost = strings.ToLower(u.Hostname())
		}
	}
	var secondaryHosts []browsersniff.SecondaryHost
	for h, c := range hostDist {
		if h != "" && h != primaryHost {
			secondaryHosts = append(secondaryHosts, browsersniff.SecondaryHost{
				Host:   h,
				Count:  c,
				Reason: browsersniff.SecondaryHostReasonNonPrimary,
			})
		}
	}
	sort.Slice(secondaryHosts, func(i, j int) bool { return secondaryHosts[i].Host < secondaryHosts[j].Host })

	// Warnings.
	var warnings []browsersniff.AnalysisWarning
	for _, w := range res.Warnings {
		warnings = append(warnings, browsersniff.AnalysisWarning{
			Type:       "app_sniff_note",
			Message:    RedactString(w),
			Confidence: 0.9,
		})
	}

	// Generation hints.
	var hints []string
	hints = append(hints, "discovered_via_app_sniff")
	if res.Meta.Framework != "" && res.Meta.Framework != FrameworkUnknown {
		hints = append(hints, "framework:"+string(res.Meta.Framework))
	}
	if len(res.Pinning) > 0 {
		hints = append(hints, "tls_pinning_detected")
	}
	if len(res.GRPCEndpoints) > 0 {
		hints = append(hints, "grpc_endpoints_available")
	}
	slices.Sort(hints)

	analysis := &browsersniff.TrafficAnalysis{
		Version: "1",
		Summary: browsersniff.TrafficAnalysisSummary{
			TargetURL:        res.PrimaryBaseURL,
			CapturedAt:       time.Now().UTC().Format(time.RFC3339),
			EntryCount:       len(res.Endpoints) + len(res.GRPCEndpoints),
			APIEntryCount:    len(res.Endpoints),
			NoiseEntryCount:  0,
			HostDistribution: hostDist,
		},
		SecondaryHosts:   secondaryHosts,
		Protocols:        protocols,
		Auth:             browsersniff.AuthAnalysis{Candidates: authCandidates},
		Protections:      protections,
		EndpointClusters: clusters,
		GenerationHints:  hints,
		Warnings:         warnings,
	}

	if apiSpec != nil {
		browsersniff.AddReservedResourceNameWarnings(apiSpec, analysis)
	}
	return analysis
}

// WriteManifest writes the full redacted DiscoveryResult JSON manifest to path.
func WriteManifest(res *DiscoveryResult, outputPath string) error {
	if res == nil {
		return fmt.Errorf("discovery result is required")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("creating manifest directory: %w", err)
	}
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling app-sniff manifest: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return fmt.Errorf("writing app-sniff manifest: %w", err)
	}
	return nil
}

// DefaultCachePath returns the default spec output path for an app-sniff run.
func DefaultCachePath(name string) string {
	slug := naming.Slug(name)
	if slug == "" {
		slug = "app"
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".cache", "printing-press", "app-sniff", slug+"-spec.yaml")
	}
	return filepath.Join(home, ".cache", "printing-press", "app-sniff", slug+"-spec.yaml")
}
