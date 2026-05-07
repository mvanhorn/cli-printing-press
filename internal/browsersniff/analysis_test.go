package browsersniff

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzeTraffic_SampleCapture(t *testing.T) {
	t.Parallel()

	capture, err := ParseEnriched(filepath.Join("..", "..", "testdata", "sniff", "sample-enriched.json"))
	require.NoError(t, err)

	analysis, err := AnalyzeTraffic(capture)
	require.NoError(t, err)

	assert.Equal(t, trafficAnalysisVersion, analysis.Version)
	assert.Equal(t, "https://hn.algolia.com", analysis.Summary.TargetURL)
	assert.Equal(t, 3, analysis.Summary.EntryCount)
	assert.NotZero(t, analysis.Summary.APIEntryCount)
	assert.NotEmpty(t, analysis.EndpointClusters)
	assert.NotEmpty(t, analysis.Protocols)
	assert.NotEmpty(t, analysis.CandidateCommands)
	assert.NotContains(t, mustJSON(t, analysis), "28f0e1ec37a5e792e6845e67da5f20dd")
}

func TestAnalyzeTraffic_EmptyAndNilCapture(t *testing.T) {
	t.Parallel()

	_, err := AnalyzeTraffic(nil)
	require.Error(t, err)
	assert.EqualError(t, err, "capture is required")

	analysis, err := AnalyzeTraffic(&EnrichedCapture{})
	require.NoError(t, err)

	assert.Zero(t, analysis.Summary.EntryCount)
	assert.Contains(t, warningTypes(analysis.Warnings), "empty_capture")
}

func TestAnalyzeTraffic_RedactsAuthSignals(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://api.example.com",
		Auth: &AuthCapture{
			Type:        "composed",
			Headers:     map[string]string{"Authorization": "Bearer should-not-leak"},
			Cookies:     []string{"session_id=secret-cookie"},
			BoundDomain: "example.com",
		},
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/users?api_token=secret-query",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"users":[{"id":1,"name":"Ada"}]}`,
				RequestHeaders: map[string]string{
					"Authorization": "Bearer secret-header",
					"Cookie":        "session_id=secret-cookie; prefs=secret-prefs",
					"X-API-Key":     "secret-api-key",
				},
			},
		},
	}

	analysis, err := AnalyzeTraffic(capture)
	require.NoError(t, err)

	encoded := mustJSON(t, analysis)
	for _, secret := range []string{"should-not-leak", "secret-cookie", "secret-query", "secret-header", "secret-prefs", "secret-api-key"} {
		assert.NotContains(t, encoded, secret)
	}
	assert.Contains(t, encoded, "Authorization")
	assert.Contains(t, encoded, "session_id")
	assert.Contains(t, encoded, "api_token")
	assert.Contains(t, authTypes(analysis.Auth.Candidates), "composed")
	assert.Contains(t, authTypes(analysis.Auth.Candidates), "bearer_token")
	assert.Contains(t, authTypes(analysis.Auth.Candidates), "api_key")
}

func TestAnalyzeTraffic_DetectsProtocolProtectionAndWarningCategories(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://app.example.com",
		Entries: []EnrichedEntry{
			{
				Method:              "POST",
				URL:                 "https://app.example.com/graphql",
				RequestBody:         `{"operationName":"SearchProjects","query":"query SearchProjects { projects { id } }","page":1,"variables":{"page":1},"extensions":{"persistedQuery":{"version":1,"sha256Hash":"abc123"}}}`,
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"errors":[{"message":"unauthorized"}]}`,
				RequestHeaders:      map[string]string{"Content-Type": "application/json"},
			},
			{
				Method:              "POST",
				URL:                 "https://docs.example.com/_/BatchedDataUi/data/batchexecute?rpcids=abc123",
				RequestBody:         `f.req=%5B%5B%5B%22abc123%22%2C%22%5B%5D%22%2Cnull%2C%22generic%22%5D%5D%5D`,
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `)]}'` + "\n" + `12345` + "\n" + `["wrb.fr","abc123","{\"ok\":true}"]`,
				RequestHeaders:      map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			},
			{
				Method:              "GET",
				URL:                 "https://app.example.com/api/private",
				ResponseStatus:      403,
				ResponseContentType: "text/html",
				ResponseBody:        `<html><title>Access denied</title><script>captcha</script><p>Cloudflare challenge</p></html>`,
				ResponseHeaders:     map[string]string{"Server": "cloudflare", "CF-Ray": "abc"},
			},
			{
				Method:              "GET",
				URL:                 "https://app.example.com/explore",
				ResponseStatus:      200,
				ResponseContentType: "text/html",
				ResponseBody:        `<html><script id="__NEXT_DATA__" type="application/json">{"props":{}}</script><div id="__next"></div></html>`,
			},
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items?cursor=abc",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `null`,
			},
			{
				Method:         "GET",
				URL:            "wss://stream.example.com/events",
				ResponseStatus: 101,
				RequestHeaders: map[string]string{"Upgrade": "websocket"},
			},
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/events",
				ResponseStatus:      200,
				ResponseContentType: "text/event-stream",
				ResponseBody:        "data: {}\n\n",
			},
		},
	}

	analysis, err := AnalyzeTraffic(capture)
	require.NoError(t, err)

	protocols := protocolLabels(analysis.Protocols)
	for _, want := range []string{"graphql", "graphql_persisted_query", "google_batchexecute", "rpc_envelope", "ssr_embedded_data", "browser_rendered", "websocket", "sse"} {
		assert.Contains(t, protocols, want)
	}

	protections := protectionLabels(analysis.Protections)
	for _, want := range []string{"cloudflare", "captcha", "protected_web"} {
		assert.Contains(t, protections, want)
	}

	warnings := warningTypes(analysis.Warnings)
	for _, want := range []string{"graphql_error_only", "raw_protocol_envelope", "html_challenge_page", "empty_payload"} {
		assert.Contains(t, warnings, want)
	}

	assert.Contains(t, paginationNames(analysis.Pagination), "cursor")
	assert.Contains(t, paginationNames(analysis.Pagination), "page")
	assert.Contains(t, analysis.GenerationHints, "has_rpc_envelope")
	assert.Contains(t, analysis.GenerationHints, "graphql_persisted_query")
	assert.Contains(t, analysis.GenerationHints, "requires_protected_client")
	assert.Contains(t, analysis.GenerationHints, "requires_js_rendering")
	assert.Contains(t, analysis.GenerationHints, "weak_schema_confidence")
}

func TestAnalyzeTraffic_ClassifiesBrowserClearanceReachability(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://www.producthunt.com",
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://www.producthunt.com/frontend/graphql",
				ResponseStatus:      403,
				ResponseContentType: "text/html; charset=UTF-8",
				ResponseBody:        `<html><title>Just a moment...</title><p>Cloudflare challenge</p></html>`,
				ResponseHeaders: map[string]string{
					"Server":       "cloudflare",
					"CF-Ray":       "abc",
					"CF-Mitigated": "challenge",
				},
			},
		},
	}

	analysis, err := AnalyzeTraffic(capture)
	require.NoError(t, err)
	require.NotNil(t, analysis.Reachability)

	assert.Equal(t, "browser_clearance_http", analysis.Reachability.Mode)
	assert.GreaterOrEqual(t, analysis.Reachability.Confidence, 0.9)
	assert.Contains(t, protectionLabels(analysis.Protections), "bot_challenge")
	assert.Contains(t, analysis.GenerationHints, "browser_clearance_required")
	assert.Contains(t, analysis.GenerationHints, "requires_browser_auth")
}

func TestAnalyzeTraffic_DoesNotRequirePageContextForSPADocumentNoise(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://app.example.com",
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://app.example.com/",
				ResponseStatus:      200,
				ResponseContentType: "text/html",
				ResponseBody:        `<html><body><div id="__next"></div><script src="/app.js"></script></body></html>`,
			},
			{
				Method:              "GET",
				URL:                 "https://app.example.com/api/items",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"items":[{"id":"item_1"}]}`,
			},
		},
	}

	analysis, err := AnalyzeTraffic(capture)
	require.NoError(t, err)
	require.NotNil(t, analysis.Reachability)

	assert.Contains(t, protocolLabels(analysis.Protocols), "browser_rendered")
	assert.Equal(t, 1, analysis.Summary.APIEntryCount)
	assert.Equal(t, "standard_http", analysis.Reachability.Mode)
	assert.NotContains(t, analysis.GenerationHints, "requires_page_context")
}

func TestApplyReachabilityDefaultsAddsBrowserClearanceCookieAuth(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:      "producthunt",
		BaseURL:   "https://www.producthunt.com",
		Auth:      spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{"posts": {Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/posts"}}}},
	}
	analysis := &TrafficAnalysis{
		Summary: TrafficAnalysisSummary{TargetURL: "https://www.producthunt.com"},
		Reachability: &ReachabilityAnalysis{
			Mode:       "browser_clearance_http",
			Confidence: 0.9,
		},
	}

	ApplyReachabilityDefaults(apiSpec, analysis)

	assert.Equal(t, spec.HTTPTransportBrowserChromeH3, apiSpec.HTTPTransport)
	assert.Equal(t, "cookie", apiSpec.Auth.Type)
	assert.Equal(t, "Cookie", apiSpec.Auth.Header)
	assert.Equal(t, ".producthunt.com", apiSpec.Auth.CookieDomain)
	assert.Equal(t, []string{"PRODUCTHUNT_COOKIES"}, apiSpec.Auth.EnvVars)
	assert.True(t, apiSpec.Auth.RequiresBrowserSession)
	assert.Equal(t, "/posts", apiSpec.Auth.BrowserSessionValidationPath)
	assert.Equal(t, "GET", apiSpec.Auth.BrowserSessionValidationMethod)
}

func TestApplyReachabilityDefaultsDoesNotRequireProofWithoutValidationPath(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "producthunt",
		BaseURL: "https://www.producthunt.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{"graphql": {Endpoints: map[string]spec.Endpoint{"query": {
			Method: "POST",
			Path:   "/frontend/graphql",
			Body:   []spec.Param{{Name: "body", Required: true}},
		}}}},
	}
	analysis := &TrafficAnalysis{
		Summary: TrafficAnalysisSummary{TargetURL: "https://www.producthunt.com"},
		Reachability: &ReachabilityAnalysis{
			Mode:       "browser_clearance_http",
			Confidence: 0.9,
		},
	}

	ApplyReachabilityDefaults(apiSpec, analysis)

	assert.Equal(t, spec.HTTPTransportBrowserChromeH3, apiSpec.HTTPTransport)
	assert.Equal(t, "cookie", apiSpec.Auth.Type)
	assert.False(t, apiSpec.Auth.RequiresBrowserSession)
	assert.Empty(t, apiSpec.Auth.BrowserSessionValidationPath)
	assert.Empty(t, apiSpec.Auth.BrowserSessionValidationMethod)
}

func TestApplyReachabilityDefaultsDoesNotEmitBrowserRequiredRuntimeTransport(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "browserrequired",
		BaseURL: "https://www.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
	}
	analysis := &TrafficAnalysis{
		Reachability: &ReachabilityAnalysis{
			Mode:       "browser_required",
			Confidence: 0.85,
		},
	}

	ApplyReachabilityDefaults(apiSpec, analysis)

	assert.Empty(t, apiSpec.HTTPTransport)
	assert.Equal(t, "none", apiSpec.Auth.Type)
	assert.False(t, apiSpec.Auth.RequiresBrowserSession)
}

func TestAnalyzeTraffic_DetectsTimingAndWeakSchemaEvidence(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/blob",
				StartedDateTime:     "2026-04-21T12:00:00Z",
				ResponseStatus:      200,
				ResponseContentType: "application/x-protobuf",
				ResponseBody:        "binary",
			},
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/blob/2",
				StartedDateTime:     "2026-04-21T12:00:01Z",
				ResponseStatus:      500,
				ResponseContentType: "application/json",
				ResponseBody:        `{"error":"boom"}`,
			},
		},
	}

	analysis, err := AnalyzeTraffic(capture)
	require.NoError(t, err)

	assert.Equal(t, "2026-04-21T12:00:00Z", analysis.Summary.TimeStart)
	assert.Equal(t, "2026-04-21T12:00:01Z", analysis.Summary.TimeEnd)
	require.NotEmpty(t, analysis.RequestSequences)
	assert.GreaterOrEqual(t, analysis.RequestSequences[0].Confidence, 0.65)
	assert.Contains(t, warningTypes(analysis.Warnings), "weak_schema_evidence")
	assert.Contains(t, warningTypes(analysis.Warnings), "error_status_cluster")
}

func TestAnalyzeTraffic_SeparatesEndpointClustersByHost(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		Entries: []EnrichedEntry{
			{
				Method:              "POST",
				URL:                 "https://api.example.com/graphql",
				RequestBody:         `{"query":"query ApiSearch { items { id } }"}`,
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"data":{"items":[]}}`,
				RequestHeaders:      map[string]string{"Content-Type": "application/json"},
			},
			{
				Method:              "POST",
				URL:                 "https://edge.examplecdn.com/graphql",
				RequestBody:         `{"query":"query EdgeSearch { items { id } }"}`,
				ResponseStatus:      503,
				ResponseContentType: "application/json",
				ResponseBody:        `{"errors":[{"message":"edge unavailable"}]}`,
				RequestHeaders:      map[string]string{"Content-Type": "application/json"},
			},
		},
	}

	analysis, err := AnalyzeTraffic(capture)
	require.NoError(t, err)

	require.Len(t, analysis.EndpointClusters, 2)
	assert.Equal(t, []string{"api.example.com", "edge.examplecdn.com"}, clusterHosts(analysis.EndpointClusters))
	assert.Equal(t, []int{200}, analysis.EndpointClusters[0].Statuses)
	assert.Equal(t, []int{503}, analysis.EndpointClusters[1].Statuses)
}

func TestAnalyzeTraffic_DoesNotTreatPaginationTokensAsAuth(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items?page_token=page-2&next_token=next-3&pagination_token=cursor",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"items":[],"next_token":"next-4"}`,
			},
		},
	}

	analysis, err := AnalyzeTraffic(capture)
	require.NoError(t, err)

	assert.Empty(t, analysis.Auth.Candidates)
	assert.ElementsMatch(t, []string{"next_token", "page_token", "pagination_token"}, paginationNames(analysis.Pagination))
}

func TestAnalyzeTraffic_DoesNotWarnGraphQLErrorOnlyForRESTErrors(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items",
				ResponseStatus:      400,
				ResponseContentType: "application/json",
				ResponseBody:        `{"errors":[{"code":"bad_request","message":"Invalid filter"}]}`,
			},
		},
	}

	analysis, err := AnalyzeTraffic(capture)
	require.NoError(t, err)

	assert.NotContains(t, protocolLabels(analysis.Protocols), "graphql")
	assert.NotContains(t, warningTypes(analysis.Warnings), "graphql_error_only")
	assert.Contains(t, warningTypes(analysis.Warnings), "error_status_cluster")
}

func TestWriteTrafficAnalysisAndDefaultPath(t *testing.T) {
	t.Parallel()

	analysis := &TrafficAnalysis{
		Version: trafficAnalysisVersion,
		Summary: TrafficAnalysisSummary{
			EntryCount: 1,
		},
	}
	outputPath := filepath.Join(t.TempDir(), "nested", "traffic-analysis.json")

	err := WriteTrafficAnalysis(analysis, outputPath)
	require.NoError(t, err)

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"version": "1"`)
	assert.True(t, strings.HasSuffix(string(data), "\n"))
	info, err := os.Stat(outputPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.Equal(t, filepath.Join("/tmp", "example-spec-traffic-analysis.json"), DefaultTrafficAnalysisPath(filepath.Join("/tmp", "example-spec.yaml")))
}

func TestReadTrafficAnalysis(t *testing.T) {
	t.Parallel()

	inputPath := filepath.Join(t.TempDir(), "traffic-analysis.json")
	require.NoError(t, os.WriteFile(inputPath, []byte(`{"version":"1","summary":{"entry_count":1}}`), 0o644))

	analysis, err := ReadTrafficAnalysis(inputPath)
	require.NoError(t, err)
	assert.Equal(t, "1", analysis.Version)
	assert.Equal(t, 1, analysis.Summary.EntryCount)
}

func TestReadTrafficAnalysisRejectsUnsupportedVersion(t *testing.T) {
	t.Parallel()

	inputPath := filepath.Join(t.TempDir(), "traffic-analysis.json")
	require.NoError(t, os.WriteFile(inputPath, []byte(`{"version":"2","summary":{"entry_count":1}}`), 0o644))

	_, err := ReadTrafficAnalysis(inputPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unsupported traffic analysis version "2"`)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()

	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}

func protocolLabels(protocols []ProtocolObservation) []string {
	labels := make([]string, 0, len(protocols))
	for _, protocol := range protocols {
		labels = append(labels, protocol.Label)
	}
	return labels
}

func authTypes(candidates []AuthCandidate) []string {
	types := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		types = append(types, candidate.Type)
	}
	return types
}

func protectionLabels(protections []ProtectionObservation) []string {
	labels := make([]string, 0, len(protections))
	for _, protection := range protections {
		labels = append(labels, protection.Label)
	}
	return labels
}

func warningTypes(warnings []AnalysisWarning) []string {
	types := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		types = append(types, warning.Type)
	}
	return types
}

func paginationNames(signals []PaginationSignal) []string {
	names := make([]string, 0, len(signals))
	for _, signal := range signals {
		names = append(names, signal.Name)
	}
	return names
}

func clusterHosts(clusters []EndpointCluster) []string {
	hosts := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		hosts = append(hosts, cluster.Host)
	}
	return hosts
}

func TestEvidenceRef_RoundTripObjectForm(t *testing.T) {
	t.Parallel()
	in := EvidenceRef{
		EntryIndex:  3,
		Method:      "GET",
		Host:        "example.com",
		Path:        "/api/v1/users",
		Status:      200,
		ContentType: "application/json",
		Reason:      "200 with JSON body",
	}
	data, err := json.Marshal(in)
	require.NoError(t, err)
	// Object form should marshal as a JSON object, not a string.
	assert.True(t, len(data) > 0 && data[0] == '{', "expected object form, got: %s", data)

	var out EvidenceRef
	require.NoError(t, json.Unmarshal(data, &out))
	assert.Equal(t, in, out, "object-form roundtrip should be lossless")
}

func TestEvidenceRef_RoundTripStringForm(t *testing.T) {
	t.Parallel()
	in := EvidenceRef{
		EntryIndex: EvidenceRefStringSentinel,
		Reason:     "Surf cleared the challenge; plain curl returned 429.",
	}
	data, err := json.Marshal(in)
	require.NoError(t, err)
	// String form should marshal as a quoted JSON string, not an object.
	assert.True(t, len(data) > 0 && data[0] == '"', "expected string form, got: %s", data)

	var out EvidenceRef
	require.NoError(t, json.Unmarshal(data, &out))
	assert.Equal(t, EvidenceRefStringSentinel, out.EntryIndex, "string roundtrip preserves sentinel")
	assert.Equal(t, in.Reason, out.Reason, "string roundtrip preserves Reason")
	assert.Empty(t, out.Method, "string-derived has no Method")
	assert.Empty(t, out.Host, "string-derived has no Host")
}

func TestEvidenceRef_UnmarshalAcceptsString(t *testing.T) {
	t.Parallel()
	// A bare JSON string should unmarshal cleanly into an EvidenceRef.
	var out EvidenceRef
	require.NoError(t, json.Unmarshal([]byte(`"prose evidence"`), &out))
	assert.Equal(t, EvidenceRefStringSentinel, out.EntryIndex)
	assert.Equal(t, "prose evidence", out.Reason)
}

func TestEvidenceRef_UnmarshalAcceptsObject(t *testing.T) {
	t.Parallel()
	// Existing object-shaped HAR-derived form continues to work.
	var out EvidenceRef
	require.NoError(t, json.Unmarshal([]byte(`{"entry_index": 7, "method": "POST", "host": "x.com"}`), &out))
	assert.Equal(t, 7, out.EntryIndex)
	assert.Equal(t, "POST", out.Method)
	assert.Equal(t, "x.com", out.Host)
}

func TestEvidenceRef_MixedArrayInTrafficAnalysis(t *testing.T) {
	t.Parallel()
	// Traffic-analysis files in the wild may carry mixed object + string
	// evidence (HAR-derived alongside hand-authored prose). Verify the
	// reachability evidence array survives a round-trip through the full
	// TrafficAnalysis struct.
	raw := []byte(`{
  "version": "1",
  "summary": {"entry_count": 0, "api_entry_count": 0, "noise_entry_count": 0},
  "reachability": {
    "mode": "browser_http",
    "confidence": 0.9,
    "evidence": [
      "Surf with Chrome impersonation cleared Vercel without cookies.",
      {"entry_index": 0, "method": "GET", "host": "food52.com", "status": 429}
    ]
  },
  "protocols": [],
  "auth": {},
  "endpoint_clusters": []
}`)
	var ta TrafficAnalysis
	require.NoError(t, json.Unmarshal(raw, &ta))
	require.NotNil(t, ta.Reachability)
	require.Len(t, ta.Reachability.Evidence, 2)
	// First entry is string-derived
	assert.Equal(t, EvidenceRefStringSentinel, ta.Reachability.Evidence[0].EntryIndex)
	assert.Contains(t, ta.Reachability.Evidence[0].Reason, "Surf")
	// Second entry is HAR-derived
	assert.Equal(t, 0, ta.Reachability.Evidence[1].EntryIndex)
	assert.Equal(t, "GET", ta.Reachability.Evidence[1].Method)

	// Round-trip preserves shapes: string stays string, object stays object.
	out, err := json.Marshal(ta)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"Surf with Chrome impersonation cleared Vercel without cookies."`,
		"string-form evidence should re-emit as a JSON string")
	assert.Contains(t, string(out), `"entry_index":0`,
		"object-form evidence should re-emit as a JSON object")
}
