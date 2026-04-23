package browsersniff

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/internal/spec"
)

const trafficAnalysisVersion = "1"

type TrafficAnalysis struct {
	Version           string                  `json:"version"`
	Summary           TrafficAnalysisSummary  `json:"summary"`
	Reachability      *ReachabilityAnalysis   `json:"reachability,omitempty"`
	Protocols         []ProtocolObservation   `json:"protocols"`
	Auth              AuthAnalysis            `json:"auth"`
	Protections       []ProtectionObservation `json:"protections,omitempty"`
	EndpointClusters  []EndpointCluster       `json:"endpoint_clusters"`
	RequestSequences  []RequestSequence       `json:"request_sequences,omitempty"`
	Pagination        []PaginationSignal      `json:"pagination,omitempty"`
	CandidateCommands []CandidateCommand      `json:"candidate_commands,omitempty"`
	GenerationHints   []string                `json:"generation_hints,omitempty"`
	Warnings          []AnalysisWarning       `json:"warnings,omitempty"`
}

type TrafficAnalysisSummary struct {
	TargetURL        string         `json:"target_url,omitempty"`
	CapturedAt       string         `json:"captured_at,omitempty"`
	EntryCount       int            `json:"entry_count"`
	APIEntryCount    int            `json:"api_entry_count"`
	NoiseEntryCount  int            `json:"noise_entry_count"`
	HostDistribution map[string]int `json:"host_distribution,omitempty"`
	TimeStart        string         `json:"time_start,omitempty"`
	TimeEnd          string         `json:"time_end,omitempty"`
}

type EvidenceRef struct {
	EntryIndex  int    `json:"entry_index"`
	Method      string `json:"method,omitempty"`
	Host        string `json:"host,omitempty"`
	Path        string `json:"path,omitempty"`
	Status      int    `json:"status,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type ProtocolObservation struct {
	Label      string            `json:"label"`
	Confidence float64           `json:"confidence"`
	Evidence   []EvidenceRef     `json:"evidence,omitempty"`
	Details    map[string]string `json:"details,omitempty"`
}

type AuthAnalysis struct {
	Candidates []AuthCandidate `json:"candidates,omitempty"`
}

type AuthCandidate struct {
	Type        string        `json:"type"`
	Confidence  float64       `json:"confidence"`
	HeaderNames []string      `json:"header_names,omitempty"`
	QueryNames  []string      `json:"query_names,omitempty"`
	CookieNames []string      `json:"cookie_names,omitempty"`
	DomainHints []string      `json:"domain_hints,omitempty"`
	Evidence    []EvidenceRef `json:"evidence,omitempty"`
}

type ReachabilityAnalysis struct {
	Mode       string        `json:"mode"`
	Confidence float64       `json:"confidence"`
	Reasons    []string      `json:"reasons,omitempty"`
	Evidence   []EvidenceRef `json:"evidence,omitempty"`
}

type ProtectionObservation struct {
	Label      string        `json:"label"`
	Confidence float64       `json:"confidence"`
	Evidence   []EvidenceRef `json:"evidence,omitempty"`
	Notes      []string      `json:"notes,omitempty"`
}

type EndpointCluster struct {
	Host          string        `json:"host,omitempty"`
	Method        string        `json:"method"`
	Path          string        `json:"path"`
	Count         int           `json:"count"`
	Statuses      []int         `json:"statuses,omitempty"`
	ContentTypes  []string      `json:"content_types,omitempty"`
	SizeClass     string        `json:"size_class,omitempty"`
	RequestShape  ShapeSummary  `json:"request_shape,omitempty"`
	ResponseShape ShapeSummary  `json:"response_shape,omitempty"`
	Evidence      []EvidenceRef `json:"evidence,omitempty"`
}

type ShapeSummary struct {
	Kind   string       `json:"kind,omitempty"`
	Fields []ShapeField `json:"fields,omitempty"`
}

type ShapeField struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	Required bool   `json:"required,omitempty"`
	Format   string `json:"format,omitempty"`
}

type RequestSequence struct {
	Label      string        `json:"label"`
	Confidence float64       `json:"confidence"`
	Evidence   []EvidenceRef `json:"evidence,omitempty"`
	Notes      []string      `json:"notes,omitempty"`
}

type PaginationSignal struct {
	Location   string        `json:"location"`
	Name       string        `json:"name"`
	Confidence float64       `json:"confidence"`
	Evidence   []EvidenceRef `json:"evidence,omitempty"`
}

type CandidateCommand struct {
	Name       string        `json:"name"`
	Resource   string        `json:"resource,omitempty"`
	Confidence float64       `json:"confidence"`
	Rationale  string        `json:"rationale,omitempty"`
	Evidence   []EvidenceRef `json:"evidence,omitempty"`
}

type AnalysisWarning struct {
	Type       string        `json:"type"`
	Message    string        `json:"message"`
	Confidence float64       `json:"confidence"`
	Evidence   []EvidenceRef `json:"evidence,omitempty"`
}

func AnalyzeTraffic(capture *EnrichedCapture) (*TrafficAnalysis, error) {
	if capture == nil {
		return nil, fmt.Errorf("capture is required")
	}

	apiEntries, noiseEntries := ClassifyEntries(capture.Entries)
	classifiedEntries := classifyInCaptureOrder(capture.Entries, apiEntries, noiseEntries)
	groups := DeduplicateTrafficEndpoints(apiEntries)

	analysis := &TrafficAnalysis{
		Version:          trafficAnalysisVersion,
		Summary:          buildTrafficSummary(capture, apiEntries, noiseEntries),
		Protocols:        detectProtocols(classifiedEntries),
		Auth:             detectTrafficAuth(capture, classifiedEntries),
		Protections:      detectProtections(classifiedEntries),
		EndpointClusters: buildEndpointClusters(groups, classifiedEntries),
		RequestSequences: detectRequestSequences(classifiedEntries),
		Pagination:       detectPagination(classifiedEntries),
	}
	analysis.Warnings = detectAnalysisWarnings(classifiedEntries, analysis.EndpointClusters)
	if len(capture.Entries) == 0 {
		analysis.Warnings = append(analysis.Warnings, AnalysisWarning{
			Type:       "empty_capture",
			Message:    "Capture contains no entries; no traffic evidence is available.",
			Confidence: 1,
		})
	}
	analysis.Reachability = classifyReachability(analysis, classifiedEntries)
	analysis.CandidateCommands = suggestCandidateCommands(analysis.EndpointClusters)
	analysis.GenerationHints = deriveGenerationHints(analysis)
	sortTrafficAnalysis(analysis)

	return analysis, nil
}

func WriteTrafficAnalysis(analysis *TrafficAnalysis, outputPath string) error {
	if analysis == nil {
		return fmt.Errorf("traffic analysis is required")
	}
	if strings.TrimSpace(outputPath) == "" {
		return fmt.Errorf("output path is required")
	}

	data, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling traffic analysis json: %w", err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening traffic analysis json: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("writing traffic analysis json: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing traffic analysis json: %w", err)
	}

	return nil
}

func ReadTrafficAnalysis(inputPath string) (*TrafficAnalysis, error) {
	if strings.TrimSpace(inputPath) == "" {
		return nil, fmt.Errorf("input path is required")
	}

	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("reading traffic analysis json: %w", err)
	}

	var analysis TrafficAnalysis
	if err := json.Unmarshal(data, &analysis); err != nil {
		return nil, fmt.Errorf("parsing traffic analysis json: %w", err)
	}
	if analysis.Version == "" {
		return nil, fmt.Errorf("traffic analysis missing version")
	}
	if analysis.Version != trafficAnalysisVersion {
		return nil, fmt.Errorf("unsupported traffic analysis version %q", analysis.Version)
	}

	return &analysis, nil
}

func DefaultTrafficAnalysisPath(specPath string) string {
	dir := filepath.Dir(specPath)
	base := filepath.Base(specPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" || stem == "." {
		stem = "traffic"
	}
	return filepath.Join(dir, stem+"-traffic-analysis.json")
}

func DeduplicateTrafficEndpoints(entries []EnrichedEntry) []EndpointGroup {
	groups := make([]EndpointGroup, 0)
	indexByKey := make(map[string]int)

	for _, entry := range entries {
		method := strings.ToUpper(strings.TrimSpace(entry.Method))
		host := strings.ToLower(extractHost(entry.URL))
		normalizedPath := normalizeEntryPath(entry.URL)
		key := host + " " + method + " " + normalizedPath

		if idx, ok := indexByKey[key]; ok {
			groups[idx].Entries = append(groups[idx].Entries, entry)
			continue
		}

		indexByKey[key] = len(groups)
		groups = append(groups, EndpointGroup{
			Method:         method,
			NormalizedPath: normalizedPath,
			Entries:        []EnrichedEntry{entry},
		})
	}

	return groups
}

func classifyInCaptureOrder(entries []EnrichedEntry, apiEntries []EnrichedEntry, noiseEntries []EnrichedEntry) []EnrichedEntry {
	apiKeys := entryClassificationKeys(apiEntries)
	noiseKeys := entryClassificationKeys(noiseEntries)

	classified := make([]EnrichedEntry, 0, len(entries))
	for _, entry := range entries {
		key := entryClassificationKey(entry)
		switch {
		case apiKeys[key] > 0:
			apiKeys[key]--
			entry.Classification = "api"
			entry.IsNoise = false
		case noiseKeys[key] > 0:
			noiseKeys[key]--
			entry.Classification = "noise"
			entry.IsNoise = true
		}
		classified = append(classified, entry)
	}
	return classified
}

func entryClassificationKeys(entries []EnrichedEntry) map[string]int {
	keys := make(map[string]int, len(entries))
	for _, entry := range entries {
		keys[entryClassificationKey(entry)]++
	}
	return keys
}

func entryClassificationKey(entry EnrichedEntry) string {
	return strings.Join([]string{entry.Method, entry.URL, entry.RequestBody, entry.ResponseBody}, "\x00")
}

func buildTrafficSummary(capture *EnrichedCapture, apiEntries []EnrichedEntry, noiseEntries []EnrichedEntry) TrafficAnalysisSummary {
	summary := TrafficAnalysisSummary{
		TargetURL:        capture.TargetURL,
		CapturedAt:       capture.CapturedAt,
		EntryCount:       len(capture.Entries),
		APIEntryCount:    len(apiEntries),
		NoiseEntryCount:  len(noiseEntries),
		HostDistribution: map[string]int{},
	}
	var start *time.Time
	var end *time.Time
	for _, entry := range capture.Entries {
		host := extractHost(entry.URL)
		if host != "" {
			summary.HostDistribution[host]++
		}
		parsed, ok := parseEntryTime(entry.StartedDateTime)
		if !ok {
			continue
		}
		if start == nil || parsed.Before(*start) {
			copy := parsed
			start = &copy
		}
		if end == nil || parsed.After(*end) {
			copy := parsed
			end = &copy
		}
	}
	if len(summary.HostDistribution) == 0 {
		summary.HostDistribution = nil
	}
	if start != nil {
		summary.TimeStart = start.Format(time.RFC3339Nano)
	}
	if end != nil {
		summary.TimeEnd = end.Format(time.RFC3339Nano)
	}
	return summary
}

func detectProtocols(entries []EnrichedEntry) []ProtocolObservation {
	observations := map[string]*ProtocolObservation{}
	addProtocol := func(label string, confidence float64, entry EnrichedEntry, index int, reason string, details map[string]string) {
		observation := observations[label]
		if observation == nil {
			observation = &ProtocolObservation{Label: label, Confidence: confidence, Details: map[string]string{}}
			observations[label] = observation
		}
		if confidence > observation.Confidence {
			observation.Confidence = confidence
		}
		observation.Evidence = appendEvidence(observation.Evidence, evidenceForEntry(entry, index, reason))
		for key, value := range details {
			if value != "" {
				observation.Details[key] = value
			}
		}
	}

	for index, entry := range entries {
		path := strings.ToLower(extractPath(entry.URL))
		host := strings.ToLower(extractHost(entry.URL))
		reqType := strings.ToLower(getHeaderValue(entry.RequestHeaders, "Content-Type"))
		respType := strings.ToLower(entry.ResponseContentType)
		body := strings.TrimSpace(entry.RequestBody)
		respBody := strings.TrimSpace(entry.ResponseBody)

		if isGraphQL(entry) {
			payload := graphqlRequestPayload(entry)
			operationName := graphqlPayloadOperationName(payload, entry.URL)
			if operationName == "" {
				operationName = graphqlOperationName(body)
			}
			addProtocol("graphql", 0.92, entry, index, "graphql path or operation body", map[string]string{"operation_name": operationName})
			if hash := graphqlPayloadPersistedQueryHash(payload); hash != "" {
				addProtocol("graphql_persisted_query", 0.9, entry, index, "GraphQL persisted-query hash", map[string]string{"operation_name": operationName, "hash": hash})
			}
		}
		if isGoogleBatchExecute(entry) {
			addProtocol("google_batchexecute", 0.95, entry, index, "google batchexecute endpoint or f.req payload", map[string]string{"rpcids": queryValue(entry.URL, "rpcids")})
			addProtocol("rpc_envelope", 0.9, entry, index, "batchexecute is an RPC envelope", nil)
		} else if isRPCEnvelope(entry) {
			addProtocol("rpc_envelope", 0.8, entry, index, "RPC envelope markers", nil)
		}
		if containsJSONRPC(body) || containsJSONRPC(respBody) {
			addProtocol("json_rpc", 0.9, entry, index, "jsonrpc field", nil)
		}
		if strings.Contains(path, "/trpc") {
			addProtocol("trpc", 0.85, entry, index, "tRPC path", nil)
		}
		if strings.Contains(reqType, "grpc-web") || strings.Contains(respType, "grpc-web") || strings.EqualFold(getHeaderValue(entry.RequestHeaders, "X-Grpc-Web"), "1") {
			addProtocol("grpc_web", 0.9, entry, index, "gRPC-Web headers or content type", nil)
		}
		if strings.EqualFold(getHeaderValue(entry.RequestHeaders, "Upgrade"), "websocket") || strings.HasPrefix(strings.ToLower(entry.URL), "ws://") || strings.HasPrefix(strings.ToLower(entry.URL), "wss://") {
			addProtocol("websocket", 0.95, entry, index, "websocket upgrade", nil)
		}
		if strings.Contains(respType, "text/event-stream") {
			addProtocol("sse", 0.95, entry, index, "event-stream response", nil)
		}
		if strings.Contains(host, "firebase") || strings.Contains(path, "firestore") || strings.Contains(path, "google.firestore") {
			addProtocol("firebase", 0.75, entry, index, "firebase/firestore host or path", nil)
		}
		if isSSREmbeddedData(entry) {
			addProtocol("ssr_embedded_data", 0.85, entry, index, "HTML contains embedded structured data", nil)
		} else if strings.Contains(respType, "text/html") && strings.TrimSpace(entry.ResponseBody) != "" {
			addProtocol("html_scrape", 0.55, entry, index, "HTML response observed", nil)
		}
		if looksBrowserRendered(entry) {
			addProtocol("browser_rendered", 0.7, entry, index, "browser-rendered page marker", nil)
		}
		if isRESTJSON(entry) {
			addProtocol("rest_json", 0.75, entry, index, "JSON HTTP request/response without specialized protocol markers", nil)
		}
	}

	out := make([]ProtocolObservation, 0, len(observations))
	for _, observation := range observations {
		if len(observation.Details) == 0 {
			observation.Details = nil
		}
		out = append(out, *observation)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence == out[j].Confidence {
			return out[i].Label < out[j].Label
		}
		return out[i].Confidence > out[j].Confidence
	})
	return out
}

func detectTrafficAuth(capture *EnrichedCapture, entries []EnrichedEntry) AuthAnalysis {
	type accumulator struct {
		candidate AuthCandidate
	}
	candidates := map[string]*accumulator{}
	add := func(key string, candidate AuthCandidate) {
		existing := candidates[key]
		if existing == nil {
			candidates[key] = &accumulator{candidate: candidate}
			return
		}
		if candidate.Confidence > existing.candidate.Confidence {
			existing.candidate.Confidence = candidate.Confidence
		}
		existing.candidate.HeaderNames = uniqueStrings(append(existing.candidate.HeaderNames, candidate.HeaderNames...))
		existing.candidate.QueryNames = uniqueStrings(append(existing.candidate.QueryNames, candidate.QueryNames...))
		existing.candidate.CookieNames = uniqueStrings(append(existing.candidate.CookieNames, candidate.CookieNames...))
		existing.candidate.DomainHints = uniqueStrings(append(existing.candidate.DomainHints, candidate.DomainHints...))
		existing.candidate.Evidence = appendEvidence(existing.candidate.Evidence, candidate.Evidence...)
	}

	if capture.Auth != nil {
		candidate := AuthCandidate{
			Type:        normalizeCapturedAuthType(capture.Auth.Type),
			Confidence:  0.95,
			HeaderNames: sortedMapKeys(capture.Auth.Headers),
			CookieNames: cookieNames(capture.Auth.Cookies),
			DomainHints: uniqueStrings([]string{capture.Auth.BoundDomain}),
		}
		add("captured:"+candidate.Type, candidate)
	}

	for index, entry := range entries {
		for name, value := range entry.RequestHeaders {
			lowerName := strings.ToLower(name)
			switch {
			case strings.EqualFold(name, "Authorization") && strings.HasPrefix(strings.TrimSpace(value), "Bearer "):
				add("bearer_token:header", AuthCandidate{Type: "bearer_token", Confidence: 0.9, HeaderNames: []string{name}, Evidence: []EvidenceRef{evidenceForEntry(entry, index, "bearer authorization header")}})
			case strings.Contains(lowerName, "api-key") || strings.Contains(lowerName, "api_key") || strings.Contains(lowerName, "x-auth-token"):
				add("api_key:header", AuthCandidate{Type: "api_key", Confidence: 0.85, HeaderNames: []string{name}, Evidence: []EvidenceRef{evidenceForEntry(entry, index, "API key-like header")}})
			case strings.EqualFold(name, "Cookie"):
				add("cookie:header", AuthCandidate{Type: "cookie", Confidence: 0.8, CookieNames: cookieNamesFromHeader(value), Evidence: []EvidenceRef{evidenceForEntry(entry, index, "cookie header")}})
			}
		}

		parsed, err := url.Parse(entry.URL)
		if err == nil {
			for name := range parsed.Query() {
				lowerName := strings.ToLower(name)
				if isAuthQueryName(lowerName) {
					add("api_key:query", AuthCandidate{Type: "api_key", Confidence: 0.7, QueryNames: []string{name}, Evidence: []EvidenceRef{evidenceForEntry(entry, index, "auth-like query parameter")}})
				}
			}
		}
	}

	out := make([]AuthCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		candidate.candidate.HeaderNames = uniqueStrings(candidate.candidate.HeaderNames)
		candidate.candidate.QueryNames = uniqueStrings(candidate.candidate.QueryNames)
		candidate.candidate.CookieNames = uniqueStrings(candidate.candidate.CookieNames)
		candidate.candidate.DomainHints = uniqueStrings(candidate.candidate.DomainHints)
		out = append(out, candidate.candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence == out[j].Confidence {
			return out[i].Type < out[j].Type
		}
		return out[i].Confidence > out[j].Confidence
	})
	return AuthAnalysis{Candidates: out}
}

func detectProtections(entries []EnrichedEntry) []ProtectionObservation {
	observations := map[string]*ProtectionObservation{}
	add := func(label string, confidence float64, entry EnrichedEntry, index int, reason string, notes ...string) {
		observation := observations[label]
		if observation == nil {
			observation = &ProtectionObservation{Label: label, Confidence: confidence}
			observations[label] = observation
		}
		if confidence > observation.Confidence {
			observation.Confidence = confidence
		}
		observation.Evidence = appendEvidence(observation.Evidence, evidenceForEntry(entry, index, reason))
		observation.Notes = uniqueStrings(append(observation.Notes, notes...))
	}

	for index, entry := range entries {
		body := strings.ToLower(entry.ResponseBody)
		headers := lowerHeaderMap(entry.ResponseHeaders)
		server := headers["server"]
		if headers["cf-mitigated"] == "challenge" {
			add("bot_challenge", 0.97, entry, index, "Cloudflare managed challenge header", "requires browser clearance")
		}
		if headers["x-vercel-mitigated"] == "challenge" || headers["x-vercel-challenge-token"] != "" {
			add("bot_challenge", 0.95, entry, index, "Vercel challenge header", "requires browser clearance")
			add("vercel_challenge", 0.9, entry, index, "Vercel challenge header")
		}
		if headers["aws-waf-token"] != "" || anyHeaderPrefix(headers, "x-amzn-waf") || strings.Contains(body, "awswaf") || strings.Contains(body, "aws-waf") {
			add("aws_waf", 0.9, entry, index, "AWS WAF challenge marker", "requires browser clearance")
		}
		switch {
		case strings.Contains(server, "cloudflare") || headers["cf-ray"] != "" || strings.Contains(body, "cf-chl") || strings.Contains(body, "cloudflare"):
			add("cloudflare", 0.9, entry, index, "Cloudflare header or challenge marker")
		case headers["x-akamai-transformed"] != "" || strings.Contains(body, "akamai"):
			add("akamai", 0.75, entry, index, "Akamai header or body marker")
		case headers["x-datadome"] != "" || strings.Contains(body, "datadome"):
			add("datadome", 0.85, entry, index, "DataDome marker")
		case strings.Contains(body, "perimeterx") || strings.Contains(body, "_px"):
			add("perimeterx", 0.8, entry, index, "PerimeterX marker")
		}

		if strings.Contains(body, "recaptcha") || strings.Contains(body, "hcaptcha") || strings.Contains(body, "captcha") {
			add("captcha", 0.85, entry, index, "CAPTCHA marker")
		}
		if entry.ResponseStatus == 403 || entry.ResponseStatus == 429 {
			if strings.Contains(entry.ResponseContentType, "html") || strings.Contains(body, "access denied") || strings.Contains(body, "too many requests") {
				add("protected_web", 0.75, entry, index, "403/429 HTML or access-denied response", "requires protected client handling")
			}
		}
		if entry.ResponseStatus >= 300 && entry.ResponseStatus < 400 {
			location := getHeaderValue(entry.ResponseHeaders, "Location")
			if strings.Contains(strings.ToLower(location), "login") || strings.Contains(strings.ToLower(location), "signin") {
				add("login_redirect", 0.8, entry, index, "redirect to login")
			}
		}
	}

	out := make([]ProtectionObservation, 0, len(observations))
	for _, observation := range observations {
		out = append(out, *observation)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence == out[j].Confidence {
			return out[i].Label < out[j].Label
		}
		return out[i].Confidence > out[j].Confidence
	})
	return out
}

func classifyReachability(analysis *TrafficAnalysis, entries []EnrichedEntry) *ReachabilityAnalysis {
	if analysis == nil {
		return nil
	}
	if analysis.Summary.EntryCount == 0 {
		return &ReachabilityAnalysis{
			Mode:       "unknown",
			Confidence: 0.2,
			Reasons:    []string{"no traffic evidence"},
		}
	}

	mode := "standard_http"
	confidence := 0.65
	reasons := []string{"no browser-only reachability signals observed"}
	evidence := make([]EvidenceRef, 0)

	hasProtocol := func(label string) bool {
		for _, protocol := range analysis.Protocols {
			if protocol.Label == label {
				evidence = appendEvidence(evidence, protocol.Evidence...)
				return true
			}
		}
		return false
	}
	hasProtection := func(labels ...string) bool {
		want := map[string]bool{}
		for _, label := range labels {
			want[label] = true
		}
		found := false
		for _, protection := range analysis.Protections {
			if want[protection.Label] {
				evidence = appendEvidence(evidence, protection.Evidence...)
				found = true
			}
		}
		return found
	}
	hasAuth := func(types ...string) bool {
		want := map[string]bool{}
		for _, typ := range types {
			want[typ] = true
		}
		for _, candidate := range analysis.Auth.Candidates {
			if want[candidate.Type] {
				evidence = appendEvidence(evidence, candidate.Evidence...)
				return true
			}
		}
		return false
	}

	if hasProtocol("browser_rendered") && hasAPIBrowserRenderedEntry(entries) {
		mode = "browser_required"
		confidence = 0.75
		reasons = []string{"captured API response appears browser-rendered"}
	}
	if hasProtection("captcha") {
		mode = "browser_required"
		confidence = 0.9
		reasons = []string{"CAPTCHA challenge observed"}
	}
	if hasProtection("bot_challenge", "aws_waf", "vercel_challenge") && mode != "browser_required" {
		mode = "browser_clearance_http"
		confidence = 0.9
		reasons = []string{"managed bot challenge observed; replay likely needs browser-derived clearance cookies"}
	}
	if hasProtection("cloudflare", "akamai", "datadome", "perimeterx", "protected_web") && mode == "standard_http" {
		mode = "browser_http"
		confidence = 0.78
		reasons = []string{"bot-protection signals observed; use browser-like HTTP transport"}
	}
	if hasProtection("login_redirect") || hasAuth("cookie", "composed") {
		if mode == "standard_http" || mode == "browser_http" {
			mode = "browser_clearance_http"
			confidence = 0.82
			reasons = []string{"browser session cookies or login redirect observed"}
		}
	}

	return &ReachabilityAnalysis{
		Mode:       mode,
		Confidence: confidence,
		Reasons:    reasons,
		Evidence:   evidence,
	}
}

func hasAPIBrowserRenderedEntry(entries []EnrichedEntry) bool {
	for _, entry := range entries {
		if entry.Classification == "api" && looksBrowserRendered(entry) {
			return true
		}
	}
	return false
}

func buildEndpointClusters(groups []EndpointGroup, entries []EnrichedEntry) []EndpointCluster {
	entryIndexes := originalEntryIndexes(entries)
	clusters := make([]EndpointCluster, 0, len(groups))
	for _, group := range groups {
		cluster := EndpointCluster{
			Method: group.Method,
			Path:   group.NormalizedPath,
			Count:  len(group.Entries),
		}
		if len(group.Entries) > 0 {
			cluster.Host = extractHost(group.Entries[0].URL)
		}
		statuses := map[int]bool{}
		contentTypes := map[string]bool{}
		totalSize := 0
		requestBodies := make([]string, 0, len(group.Entries))
		responseBodies := make([]string, 0, len(group.Entries))
		for _, entry := range group.Entries {
			statuses[entry.ResponseStatus] = true
			if entry.ResponseContentType != "" {
				contentTypes[entry.ResponseContentType] = true
			}
			totalSize += len(entry.ResponseBody)
			if strings.TrimSpace(entry.RequestBody) != "" {
				requestBodies = append(requestBodies, entry.RequestBody)
			}
			if strings.TrimSpace(entry.ResponseBody) != "" {
				responseBodies = append(responseBodies, entry.ResponseBody)
			}
			cluster.Evidence = appendEvidence(cluster.Evidence, evidenceForEntry(entry, popEntryIndex(entryIndexes, entry), "endpoint cluster member"))
		}
		cluster.Statuses = sortedInts(statuses)
		cluster.ContentTypes = sortedStringSet(contentTypes)
		cluster.SizeClass = classifyBodySize(totalSize, len(group.Entries))
		cluster.RequestShape = summarizeRequestShape(group.Entries, requestBodies)
		cluster.ResponseShape = summarizeResponseShape(responseBodies)
		clusters = append(clusters, cluster)
	}
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].Host == clusters[j].Host {
			if clusters[i].Path == clusters[j].Path {
				return clusters[i].Method < clusters[j].Method
			}
			return clusters[i].Path < clusters[j].Path
		}
		return clusters[i].Host < clusters[j].Host
	})
	return clusters
}

func originalEntryIndexes(entries []EnrichedEntry) map[string][]int {
	indexes := make(map[string][]int, len(entries))
	for index, entry := range entries {
		indexes[entryClassificationKey(entry)] = append(indexes[entryClassificationKey(entry)], index)
	}
	return indexes
}

func popEntryIndex(indexes map[string][]int, entry EnrichedEntry) int {
	key := entryClassificationKey(entry)
	values := indexes[key]
	if len(values) == 0 {
		return 0
	}
	index := values[0]
	indexes[key] = values[1:]
	return index
}

func detectRequestSequences(entries []EnrichedEntry) []RequestSequence {
	apiEvidence := make([]EvidenceRef, 0)
	hasTiming := false
	for index, entry := range entries {
		if entry.IsNoise {
			continue
		}
		apiEvidence = append(apiEvidence, evidenceForEntry(entry, index, "observed API request order"))
		if _, ok := parseEntryTime(entry.StartedDateTime); ok {
			hasTiming = true
		}
	}
	if len(apiEvidence) < 2 {
		return nil
	}
	confidence := 0.35
	notes := []string{"Capture order used; timing unavailable."}
	if hasTiming {
		confidence = 0.65
		notes = []string{"HAR timing available for at least one request."}
	}
	if len(apiEvidence) > 8 {
		apiEvidence = apiEvidence[:8]
	}
	return []RequestSequence{{
		Label:      "observed_api_flow",
		Confidence: confidence,
		Evidence:   apiEvidence,
		Notes:      notes,
	}}
}

func detectPagination(entries []EnrichedEntry) []PaginationSignal {
	names := map[string]bool{"page": true, "per_page": true, "limit": true, "offset": true, "cursor": true, "after": true, "before": true, "next": true, "next_page": true, "page_token": true, "next_token": true, "pagination_token": true, "next_page_token": true}
	seen := map[string]PaginationSignal{}
	for index, entry := range entries {
		if entry.IsNoise {
			continue
		}
		parsed, err := url.Parse(entry.URL)
		if err == nil {
			for name := range parsed.Query() {
				lower := strings.ToLower(name)
				if names[lower] || strings.Contains(lower, "cursor") {
					key := "query:" + name
					signal := seen[key]
					signal.Location = "query"
					signal.Name = name
					signal.Confidence = 0.75
					signal.Evidence = appendEvidence(signal.Evidence, evidenceForEntry(entry, index, "pagination-like query parameter"))
					seen[key] = signal
				}
			}
		}
		for _, field := range jsonFieldNames(entry.RequestBody) {
			lower := strings.ToLower(field)
			if names[lower] || strings.Contains(lower, "cursor") {
				key := "body:" + field
				signal := seen[key]
				signal.Location = "body"
				signal.Name = field
				signal.Confidence = 0.65
				signal.Evidence = appendEvidence(signal.Evidence, evidenceForEntry(entry, index, "pagination-like request field"))
				seen[key] = signal
			}
		}
	}
	out := make([]PaginationSignal, 0, len(seen))
	for _, signal := range seen {
		out = append(out, signal)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Location == out[j].Location {
			return out[i].Name < out[j].Name
		}
		return out[i].Location < out[j].Location
	})
	return out
}

func detectAnalysisWarnings(entries []EnrichedEntry, clusters []EndpointCluster) []AnalysisWarning {
	warnings := make([]AnalysisWarning, 0)
	for index, entry := range entries {
		body := strings.TrimSpace(entry.ResponseBody)
		lowerBody := strings.ToLower(body)
		if containsRawRPCEnvelope(body) {
			warnings = append(warnings, AnalysisWarning{Type: "raw_protocol_envelope", Message: "Response contains raw RPC transport markers that should be decoded before user-facing output.", Confidence: 0.9, Evidence: []EvidenceRef{evidenceForEntry(entry, index, "raw RPC envelope marker")}})
		}
		if isGraphQL(entry) && isGraphQLErrorOnly(body) {
			warnings = append(warnings, AnalysisWarning{Type: "graphql_error_only", Message: "GraphQL response contains errors without data; captured operation may not represent successful behavior.", Confidence: 0.85, Evidence: []EvidenceRef{evidenceForEntry(entry, index, "GraphQL errors without data")}})
		}
		if looksAPIPath(entry.URL) && strings.Contains(strings.ToLower(entry.ResponseContentType), "html") {
			if entry.ResponseStatus == 401 || entry.ResponseStatus == 403 || entry.ResponseStatus == 429 || strings.Contains(lowerBody, "login") || strings.Contains(lowerBody, "captcha") || strings.Contains(lowerBody, "access denied") {
				warnings = append(warnings, AnalysisWarning{Type: "html_challenge_page", Message: "API-looking request returned an HTML login, challenge, or access-denied page.", Confidence: 0.82, Evidence: []EvidenceRef{evidenceForEntry(entry, index, "HTML challenge from API-looking request")}})
			}
		}
		if looksAPIPath(entry.URL) && (body == "" || body == "null") {
			warnings = append(warnings, AnalysisWarning{Type: "empty_payload", Message: "API-looking request returned an empty or null payload; schema confidence is weak.", Confidence: 0.65, Evidence: []EvidenceRef{evidenceForEntry(entry, index, "empty/null payload")}})
		}
		contentType := strings.ToLower(entry.ResponseContentType)
		if strings.Contains(contentType, "protobuf") || strings.Contains(contentType, "octet-stream") || strings.Contains(contentType, "application/grpc") {
			warnings = append(warnings, AnalysisWarning{Type: "weak_schema_evidence", Message: "Binary or protobuf response cannot provide reliable JSON schema evidence.", Confidence: 0.75, Evidence: []EvidenceRef{evidenceForEntry(entry, index, "binary/protobuf response")}})
		}
	}
	for _, cluster := range clusters {
		if cluster.Count == 0 {
			continue
		}
		errorCount := 0
		for _, status := range cluster.Statuses {
			if status >= 400 {
				errorCount++
			}
		}
		if errorCount > 0 && errorCount == len(cluster.Statuses) {
			warnings = append(warnings, AnalysisWarning{Type: "error_status_cluster", Message: "Endpoint cluster only observed error HTTP statuses.", Confidence: 0.7, Evidence: cluster.Evidence})
		}
	}
	return warnings
}

func suggestCandidateCommands(clusters []EndpointCluster) []CandidateCommand {
	commands := make([]CandidateCommand, 0, len(clusters))
	seen := map[string]bool{}
	for _, cluster := range clusters {
		resource := commandResource(cluster.Path)
		if resource == "" {
			continue
		}
		name := deriveEndpointName(cluster.Method, cluster.Path)
		if seen[name] {
			continue
		}
		seen[name] = true
		commands = append(commands, CandidateCommand{
			Name:       name,
			Resource:   resource,
			Confidence: 0.55,
			Rationale:  fmt.Sprintf("Derived from observed %s %s traffic.", cluster.Method, cluster.Path),
			Evidence:   cluster.Evidence,
		})
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })
	return commands
}

func deriveGenerationHints(analysis *TrafficAnalysis) []string {
	hints := map[string]bool{}
	for _, protocol := range analysis.Protocols {
		switch protocol.Label {
		case "google_batchexecute", "rpc_envelope":
			hints["has_rpc_envelope"] = true
		case "graphql_persisted_query":
			hints["graphql_persisted_query"] = true
		case "browser_rendered":
			hints["requires_js_rendering"] = true
		}
	}
	for _, protection := range analysis.Protections {
		switch protection.Label {
		case "cloudflare", "akamai", "datadome", "perimeterx", "captcha", "protected_web", "aws_waf", "bot_challenge", "vercel_challenge":
			hints["requires_protected_client"] = true
		case "login_redirect":
			hints["requires_browser_auth"] = true
		}
	}
	if analysis.Reachability != nil {
		switch analysis.Reachability.Mode {
		case "browser_http":
			hints["browser_http_transport"] = true
		case "browser_clearance_http":
			hints["browser_clearance_required"] = true
			hints["requires_browser_auth"] = true
		case "browser_required":
			hints["requires_page_context"] = true
		}
	}
	for _, candidate := range analysis.Auth.Candidates {
		if candidate.Type == "cookie" || candidate.Type == "composed" {
			hints["requires_browser_auth"] = true
		}
	}
	for _, warning := range analysis.Warnings {
		if warning.Type == "weak_schema_evidence" || warning.Type == "raw_protocol_envelope" {
			hints["weak_schema_confidence"] = true
		}
	}
	return sortedBoolKeys(hints)
}

func sortTrafficAnalysis(analysis *TrafficAnalysis) {
	sort.Slice(analysis.Warnings, func(i, j int) bool {
		if analysis.Warnings[i].Type == analysis.Warnings[j].Type {
			return evidenceSortKey(analysis.Warnings[i].Evidence) < evidenceSortKey(analysis.Warnings[j].Evidence)
		}
		return analysis.Warnings[i].Type < analysis.Warnings[j].Type
	})
}

func evidenceForEntry(entry EnrichedEntry, index int, reason string) EvidenceRef {
	return EvidenceRef{
		EntryIndex:  index,
		Method:      strings.ToUpper(entry.Method),
		Host:        extractHost(entry.URL),
		Path:        extractPath(entry.URL),
		Status:      entry.ResponseStatus,
		ContentType: entry.ResponseContentType,
		Reason:      reason,
	}
}

func appendEvidence(existing []EvidenceRef, refs ...EvidenceRef) []EvidenceRef {
	seen := map[string]bool{}
	for _, ref := range existing {
		seen[evidenceKey(ref)] = true
	}
	for _, ref := range refs {
		key := evidenceKey(ref)
		if seen[key] {
			continue
		}
		seen[key] = true
		existing = append(existing, ref)
		if len(existing) >= 8 {
			break
		}
	}
	return existing
}

func evidenceKey(ref EvidenceRef) string {
	return fmt.Sprintf("%d:%s:%s:%s", ref.EntryIndex, ref.Method, ref.Host, ref.Path)
}

func evidenceSortKey(refs []EvidenceRef) string {
	if len(refs) == 0 {
		return ""
	}
	return evidenceKey(refs[0])
}

func parseEntryTime(value string) (time.Time, bool) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func isRESTJSON(entry EnrichedEntry) bool {
	if isGraphQL(entry) || isRPCEnvelope(entry) || isGoogleBatchExecute(entry) {
		return false
	}
	reqType := strings.ToLower(getHeaderValue(entry.RequestHeaders, "Content-Type"))
	respType := strings.ToLower(entry.ResponseContentType)
	return strings.Contains(reqType, "json") || strings.Contains(respType, "json") || isValidJSONBody(entry.ResponseBody)
}

func isGraphQL(entry EnrichedEntry) bool {
	path := strings.ToLower(extractPath(entry.URL))
	if strings.Contains(path, "graphql") {
		return true
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(entry.RequestBody), &payload); err != nil {
		return false
	}
	query, hasQuery := payload["query"].(string)
	if !hasQuery {
		return false
	}
	query = strings.TrimSpace(query)
	return strings.HasPrefix(query, "query ") ||
		strings.HasPrefix(query, "mutation ") ||
		strings.HasPrefix(query, "subscription ") ||
		strings.Contains(query, "{")
}

func graphqlOperationName(body string) string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return ""
	}
	if value, ok := payload["operationName"].(string); ok {
		return value
	}
	return ""
}

func isGoogleBatchExecute(entry EnrichedEntry) bool {
	lowerURL := strings.ToLower(entry.URL)
	lowerBody := strings.ToLower(entry.RequestBody)
	return strings.Contains(lowerURL, "batchexecute") || (queryValue(entry.URL, "rpcids") != "" && strings.Contains(lowerBody, "f.req=") && strings.Contains(lowerURL, "/_/"))
}

func isRPCEnvelope(entry EnrichedEntry) bool {
	body := strings.ToLower(entry.RequestBody + "\n" + entry.ResponseBody)
	path := strings.ToLower(extractPath(entry.URL))
	return strings.Contains(path, "rpc") || strings.Contains(body, "wrb.fr") || strings.Contains(body, "af.httprm") || strings.Contains(body, "f.req")
}

func containsJSONRPC(body string) bool {
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return false
	}
	_, ok := payload["jsonrpc"]
	return ok
}

func isSSREmbeddedData(entry EnrichedEntry) bool {
	if !strings.Contains(strings.ToLower(entry.ResponseContentType), "html") {
		return false
	}
	body := strings.ToLower(entry.ResponseBody)
	return strings.Contains(body, "__next_data__") || strings.Contains(body, "application/ld+json") || strings.Contains(body, "window.__")
}

func looksBrowserRendered(entry EnrichedEntry) bool {
	if !strings.Contains(strings.ToLower(entry.ResponseContentType), "html") {
		return false
	}
	body := strings.ToLower(entry.ResponseBody)
	return strings.Contains(body, "enable javascript") || strings.Contains(body, "id=\"root\"") || strings.Contains(body, "id=\"__next\"")
}

func containsRawRPCEnvelope(body string) bool {
	lower := strings.ToLower(strings.TrimSpace(body))
	return strings.Contains(lower, "wrb.fr") || strings.Contains(lower, "af.httprm") || strings.HasPrefix(lower, ")]}'")
}

func isGraphQLErrorOnly(body string) bool {
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return false
	}
	if _, ok := payload["errors"]; !ok {
		return false
	}
	data, hasData := payload["data"]
	return !hasData || data == nil
}

func looksAPIPath(rawURL string) bool {
	path := strings.ToLower(extractPath(rawURL))
	for _, marker := range []string{"/api/", "/v1/", "/v2/", "/v3/", "/graphql", "/rpc", "/_/"} {
		if strings.Contains(path, marker) {
			return true
		}
	}
	return false
}

func queryValue(rawURL string, name string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Query().Get(name)
}

func lowerHeaderMap(headers map[string]string) map[string]string {
	out := make(map[string]string, len(headers))
	for name, value := range headers {
		out[strings.ToLower(name)] = strings.ToLower(value)
	}
	return out
}

func anyHeaderPrefix(headers map[string]string, prefix string) bool {
	for name := range headers {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func isAuthQueryName(lowerName string) bool {
	if isPaginationTokenName(lowerName) {
		return false
	}
	return strings.Contains(lowerName, "key") || strings.Contains(lowerName, "token") || strings.Contains(lowerName, "auth")
}

func isPaginationTokenName(lowerName string) bool {
	switch lowerName {
	case "page_token", "next_token", "pagination_token", "next_page_token", "cursor_token", "continuation_token":
		return true
	default:
		return strings.Contains(lowerName, "page_token") ||
			strings.Contains(lowerName, "pagination_token") ||
			strings.Contains(lowerName, "next_token")
	}
}

func normalizeCapturedAuthType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "bearer":
		return "bearer_token"
	case "api_key", "cookie", "composed":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		if strings.TrimSpace(value) == "" {
			return "unknown"
		}
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func cookieNames(cookies []string) []string {
	names := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		name := strings.TrimSpace(cookie)
		if idx := strings.Index(name, "="); idx >= 0 {
			name = name[:idx]
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return uniqueStrings(names)
}

func cookieNamesFromHeader(value string) []string {
	parts := strings.Split(value, ";")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if idx := strings.Index(part, "="); idx >= 0 {
			part = part[:idx]
		}
		if part != "" {
			names = append(names, part)
		}
	}
	return uniqueStrings(names)
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortedBoolKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key, ok := range values {
		if ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func sortedStringSet(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func sortedInts(values map[int]bool) []int {
	ints := make([]int, 0, len(values))
	for value := range values {
		ints = append(ints, value)
	}
	sort.Ints(ints)
	return ints
}

func classifyBodySize(total int, count int) string {
	if count == 0 {
		return "unknown"
	}
	avg := total / count
	switch {
	case avg == 0:
		return "empty"
	case avg < 1024:
		return "small"
	case avg < 64*1024:
		return "medium"
	default:
		return "large"
	}
}

func summarizeRequestShape(entries []EnrichedEntry, bodies []string) ShapeSummary {
	for _, entry := range entries {
		body := strings.TrimSpace(entry.RequestBody)
		if body == "" {
			continue
		}
		params := InferRequestSchema(body, getHeaderValue(entry.RequestHeaders, "Content-Type"))
		if len(params) > 0 {
			return ShapeSummary{Kind: "object", Fields: shapeFields(params)}
		}
		if strings.Contains(strings.ToLower(getHeaderValue(entry.RequestHeaders, "Content-Type")), "form-urlencoded") {
			return ShapeSummary{Kind: "form"}
		}
	}
	if len(bodies) > 0 {
		return ShapeSummary{Kind: "unknown"}
	}
	return ShapeSummary{}
}

func summarizeResponseShape(bodies []string) ShapeSummary {
	fields := InferResponseSchema(bodies)
	if len(fields) > 0 {
		return ShapeSummary{Kind: inferResponseType(bodies), Fields: shapeFields(fields)}
	}
	for _, body := range bodies {
		if strings.TrimSpace(body) != "" {
			return ShapeSummary{Kind: "unknown"}
		}
	}
	return ShapeSummary{}
}

func shapeFields(params []spec.Param) []ShapeField {
	fields := make([]ShapeField, 0, len(params))
	for _, param := range params {
		fields = append(fields, ShapeField{Name: param.Name, Type: param.Type, Required: param.Required, Format: param.Format})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return fields
}

func jsonFieldNames(body string) []string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil
	}
	names := make([]string, 0, len(payload))
	for name := range payload {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func commandResource(path string) string {
	segments := significantSegments(path)
	if len(segments) == 0 {
		return ""
	}
	return strings.ReplaceAll(segments[len(segments)-1], "-", "_")
}
