package appsniff

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
)

var (
	reUUIDSegment  = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	reHexIDSegment = regexp.MustCompile(`(?i)^[0-9a-f]{16,64}$`)
	reNumIDSegment = regexp.MustCompile(`^\d{2,}$`)
)

// ReconcileWithHAR overlays an optional HAR or enriched capture onto a static
// DiscoveryResult. Matching endpoints are matched by origin + canonical path
// (falling back to hostless static entries only when no origin is known) and
// promoted to "confirmed" with real HTTP methods, statuses, query parameters,
// and observed auth header names. Observed gRPC routes are stored in
// GRPCEndpoints so default gRPC exclusion stays intact, and --host / --top
// filters are enforced afterward.
func ReconcileWithHAR(res *DiscoveryResult, harPath string, opts Options) error {
	if res == nil || strings.TrimSpace(harPath) == "" {
		return nil
	}
	capture, err := browsersniff.LoadCapture(harPath)
	if err != nil {
		return fmt.Errorf("loading reconciliation HAR %s: %w", harPath, err)
	}

	// Index static endpoints by (origin + canonical path), plus a separate
	// fallback index ONLY for static endpoints whose origin is genuinely empty.
	byOriginCanon := make(map[string][]int, len(res.Endpoints))
	byHostlessCanon := make(map[string][]int)
	for i := range res.Endpoints {
		res.Endpoints[i].ReconcileState = ReconcileStaticOnly
		canon := canonicalizePathForMatch(res.Endpoints[i].Path)
		origin := normalizeOrigin(res.Endpoints[i].BaseURL)
		if origin != "" {
			byOriginCanon[origin+" "+canon] = append(byOriginCanon[origin+" "+canon], i)
		} else {
			byHostlessCanon[canon] = append(byHostlessCanon[canon], i)
		}
	}

	type obsGroup struct {
		method       string
		host         string
		baseURL      string
		path         string
		canon        string
		isGRPC       bool
		statuses     []int
		sampleCount  int
		params       []EndpointParam
		observedAuth []string
	}
	observed := make(map[string]*obsGroup)

	for _, entry := range capture.Entries {
		if entry.IsNoise {
			continue
		}
		u, err := url.Parse(entry.URL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			continue
		}
		hostname := strings.ToLower(u.Hostname())
		if isIgnoredHost(hostname) || isStaticAssetPath(u.Path) {
			continue
		}
		hostWithPort := strings.ToLower(u.Host)
		if len(opts.HostFilters) > 0 && !matchesHostFilters(hostWithPort, opts.HostFilters) {
			continue
		}
		method := strings.ToUpper(strings.TrimSpace(entry.Method))
		if method == "" {
			method = "GET"
		}
		normPath := normalizeObservedPath(u.Path)
		if normPath == "" {
			continue
		}
		origin := strings.ToLower(u.Scheme) + "://" + hostWithPort
		canon := canonicalizePathForMatch(normPath)
		key := method + " " + origin + " " + canon
		grp, ok := observed[key]
		if !ok {
			ct := strings.ToLower(entry.ResponseContentType)
			isGRPC := classifyProtocol(normPath) == ProtocolGRPC || strings.Contains(ct, "application/grpc")
			grp = &obsGroup{
				method:  method,
				host:    hostWithPort,
				baseURL: origin,
				path:    normPath,
				canon:   canon,
				isGRPC:  isGRPC,
			}
			observed[key] = grp
		}
		grp.sampleCount++
		if entry.ResponseStatus > 0 && !slices.Contains(grp.statuses, entry.ResponseStatus) {
			grp.statuses = append(grp.statuses, entry.ResponseStatus)
		}
		for qk := range u.Query() {
			if qk != "" && !sensitiveQueryKey[strings.ToLower(qk)] {
				grp.params = mergeParams(grp.params, []EndpointParam{{Name: qk, In: ParamInQuery, Type: "string"}})
			}
		}
		// Record auth header names only, never values.
		for hk := range entry.RequestHeaders {
			lh := strings.ToLower(strings.TrimSpace(hk))
			if lh == "authorization" || lh == "cookie" || lh == "x-api-key" || lh == "x-csrf-token" || lh == "x-xsrf-token" {
				if !slices.Contains(grp.observedAuth, lh) {
					grp.observedAuth = append(grp.observedAuth, lh)
				}
			}
		}
	}

	confirmed := 0
	observedOnly := 0

	obsKeys := make([]string, 0, len(observed))
	for k := range observed {
		obsKeys = append(obsKeys, k)
	}
	sort.Strings(obsKeys)

	for _, k := range obsKeys {
		grp := observed[k]
		sort.Ints(grp.statuses)
		sort.Strings(grp.observedAuth)

		// Route observed gRPC calls into GRPCEndpoints so default gRPC
		// exclusion holds even when --har is passed.
		if grp.isGRPC {
			res.GRPCEndpoints = append(res.GRPCEndpoints, Endpoint{
				Method:         grp.method,
				Path:           grp.path,
				Host:           grp.host,
				BaseURL:        grp.baseURL,
				Protocol:       ProtocolGRPC,
				Source:         "har-observed",
				Confidence:     ConfidenceHigh,
				ObservedAuth:   grp.observedAuth,
				ReconcileState: ReconcileObservedOnly,
				Statuses:       grp.statuses,
				SampleCount:    grp.sampleCount,
			})
			observedOnly++
			continue
		}

		// Prefer exact origin + canonical path match; only fall back to
		// hostless static entries when no exact origin match exists.
		indices := byOriginCanon[grp.baseURL+" "+grp.canon]
		if len(indices) == 0 {
			indices = byHostlessCanon[grp.canon]
		}

		matchedIdx := -1
		for _, idx := range indices {
			if res.Endpoints[idx].Method == grp.method {
				matchedIdx = idx
				break
			}
		}
		if matchedIdx < 0 {
			for _, idx := range indices {
				if res.Endpoints[idx].Confidence != ConfidenceHigh && res.Endpoints[idx].Method == "GET" {
					matchedIdx = idx
					res.Endpoints[idx].Method = grp.method
					break
				}
			}
		}

		if matchedIdx >= 0 {
			ep := &res.Endpoints[matchedIdx]
			if ep.ReconcileState != ReconcileConfirmed {
				confirmed++
			}
			ep.ReconcileState = ReconcileConfirmed
			ep.Confidence = ConfidenceHigh
			ep.SampleCount += grp.sampleCount
			if ep.BaseURL == "" {
				ep.Host = grp.host
				ep.BaseURL = grp.baseURL
			}
			for _, st := range grp.statuses {
				if !slices.Contains(ep.Statuses, st) {
					ep.Statuses = append(ep.Statuses, st)
				}
			}
			sort.Ints(ep.Statuses)
			ep.Params = mergeParams(ep.Params, grp.params)
			ep.ObservedAuth = mergeStrings(ep.ObservedAuth, grp.observedAuth)
			continue
		}

		observedOnly++
		res.Endpoints = append(res.Endpoints, Endpoint{
			Method:         grp.method,
			Path:           grp.path,
			Host:           grp.host,
			BaseURL:        grp.baseURL,
			Protocol:       classifyProtocol(grp.path),
			Source:         "har-observed",
			Confidence:     ConfidenceHigh,
			Params:         mergeParams(extractPathParams(grp.path), grp.params),
			ObservedAuth:   grp.observedAuth,
			ReconcileState: ReconcileObservedOnly,
			Statuses:       grp.statuses,
			SampleCount:    grp.sampleCount,
		})
	}

	ApplyResultFilters(res, opts)

	staticOnly := 0
	for _, ep := range res.Endpoints {
		if ep.ReconcileState == ReconcileStaticOnly {
			staticOnly++
		}
	}

	res.Reconciliation = &ReconciliationSummary{
		HARPath:      harPath,
		Confirmed:    confirmed,
		ObservedOnly: observedOnly,
		StaticOnly:   staticOnly,
	}
	return nil
}

func normalizeObservedPath(raw string) string {
	segs := strings.Split(strings.Trim(raw, "/"), "/")
	if len(segs) == 0 || (len(segs) == 1 && segs[0] == "") {
		return ""
	}
	for i, s := range segs {
		if reUUIDSegment.MatchString(s) || reHexIDSegment.MatchString(s) || reNumIDSegment.MatchString(s) {
			segs[i] = "{id}"
		} else if RedactString(s) != s {
			segs[i] = "{token}"
		}
	}
	return "/" + strings.Join(segs, "/")
}

func canonicalizePathForMatch(path string) string {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	for i, s := range segs {
		if (strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")) || reUUIDSegment.MatchString(s) || reHexIDSegment.MatchString(s) || reNumIDSegment.MatchString(s) {
			segs[i] = "{}"
		} else {
			segs[i] = strings.ToLower(s)
		}
	}
	return "/" + strings.Join(segs, "/")
}
