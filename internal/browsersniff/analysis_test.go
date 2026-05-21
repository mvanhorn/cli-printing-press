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

func TestComputeNormalizationFlags(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		method           string
		sampleCount      int
		statuses         []int
		contentTypes     []string
		requestBodyCount int
		want             []string
	}{
		{
			name:         "single sample triggers single-sample and single-status",
			method:       "GET",
			sampleCount:  1,
			statuses:     []int{200},
			contentTypes: []string{"application/json"},
			want:         []string{"single-sample", "single-status"},
		},
		{
			name:         "five samples all-200 still single-status",
			method:       "GET",
			sampleCount:  5,
			statuses:     []int{200},
			contentTypes: []string{"application/json"},
			want:         []string{"single-status"},
		},
		{
			name:         "ten samples multi-status clean",
			method:       "GET",
			sampleCount:  10,
			statuses:     []int{200, 404},
			contentTypes: []string{"application/json"},
			want:         nil,
		},
		{
			name:             "POST body inconsistent fires only when some samples have body",
			method:           "POST",
			sampleCount:      3,
			statuses:         []int{200, 201},
			contentTypes:     []string{"application/json"},
			requestBodyCount: 2,
			want:             []string{"request-body-only-on-some-samples"},
		},
		{
			name:             "POST all samples have body does not fire flag",
			method:           "POST",
			sampleCount:      3,
			statuses:         []int{200, 201},
			contentTypes:     []string{"application/json"},
			requestBodyCount: 3,
			want:             nil,
		},
		{
			name:             "GET with body samples does not fire request-body flag",
			method:           "GET",
			sampleCount:      3,
			statuses:         []int{200, 304},
			contentTypes:     []string{"application/json"},
			requestBodyCount: 2,
			want:             nil,
		},
		{
			name:         "mixed content types fires only on real media-type drift",
			method:       "GET",
			sampleCount:  4,
			statuses:     []int{200, 404},
			contentTypes: []string{"application/json", "text/html"},
			want:         []string{"mixed-content-types"},
		},
		{
			name:         "json charset variants do not count as mixed",
			method:       "GET",
			sampleCount:  4,
			statuses:     []int{200, 404},
			contentTypes: []string{"application/json", "application/json; charset=utf-8"},
			want:         nil,
		},
		{
			name:         "zero samples treated as single-sample edge",
			method:       "GET",
			sampleCount:  0,
			statuses:     []int{},
			contentTypes: nil,
			want:         []string{"single-sample"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := computeNormalizationFlags(tc.method, tc.sampleCount, tc.statuses, tc.contentTypes, tc.requestBodyCount)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestBucketConfidence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		sampleCount int
		statuses    []int
		flags       []string
		want        string
	}{
		{name: "single sample with flag -> low", sampleCount: 1, statuses: []int{200}, flags: []string{"single-sample"}, want: "low"},
		{name: "two samples no flags -> low", sampleCount: 2, statuses: []int{200, 404}, flags: nil, want: "low"},
		{name: "three samples no flags -> medium", sampleCount: 3, statuses: []int{200, 404}, flags: nil, want: "medium"},
		{name: "nine samples no flags -> medium", sampleCount: 9, statuses: []int{200, 404}, flags: nil, want: "medium"},
		{name: "ten samples multi-status no flags -> high", sampleCount: 10, statuses: []int{200, 404}, flags: nil, want: "high"},
		{name: "ten samples single status -> medium not high", sampleCount: 10, statuses: []int{200}, flags: nil, want: "medium"},
		{name: "twenty samples with flag -> low overrides count", sampleCount: 20, statuses: []int{200, 404}, flags: []string{"mixed-content-types"}, want: "low"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := bucketConfidence(tc.sampleCount, tc.statuses, tc.flags)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestAnalyzeTraffic_PopulatesConfidenceAndFlagsOnClusters(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://api.example.com",
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":1}`,
				RequestHeaders:      map[string]string{"Accept": "application/json"},
			},
		},
	}

	analysis, err := AnalyzeTraffic(capture)
	require.NoError(t, err)
	require.NotEmpty(t, analysis.EndpointClusters)

	cluster := analysis.EndpointClusters[0]
	assert.Equal(t, 1, cluster.Count)
	assert.Equal(t, "low", cluster.Confidence)
	assert.Contains(t, cluster.NormalizationFlags, "single-sample")
	assert.Contains(t, cluster.NormalizationFlags, "single-status")
}

func TestAnalyzeTraffic_ClusterConfidenceRoundTripsThroughSidecar(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://api.example.com",
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":1}`,
				RequestHeaders:      map[string]string{"Accept": "application/json"},
			},
		},
	}

	analysis, err := AnalyzeTraffic(capture)
	require.NoError(t, err)

	tmp := filepath.Join(t.TempDir(), "spec-traffic-analysis.json")
	require.NoError(t, WriteTrafficAnalysis(analysis, tmp))

	roundTrip, err := ReadTrafficAnalysis(tmp)
	require.NoError(t, err)
	require.NotEmpty(t, roundTrip.EndpointClusters)
	assert.Equal(t, analysis.EndpointClusters[0].Confidence, roundTrip.EndpointClusters[0].Confidence)
	assert.Equal(t, analysis.EndpointClusters[0].NormalizationFlags, roundTrip.EndpointClusters[0].NormalizationFlags)
}

func TestAnalyzeTraffic_PopulatesObservedAuthOnEndpointCluster(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://api.example.com",
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":1}`,
				RequestHeaders: map[string]string{
					"Authorization": "Bearer eyJtoken",
					"Cookie":        "session=x",
				},
			},
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/public",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"ok":true}`,
				RequestHeaders:      map[string]string{"Accept": "application/json"},
			},
		},
	}

	analysis, err := AnalyzeTraffic(capture)
	require.NoError(t, err)

	var authedCluster *EndpointCluster
	var publicCluster *EndpointCluster
	for i := range analysis.EndpointClusters {
		c := &analysis.EndpointClusters[i]
		switch c.Path {
		case "/v1/items":
			authedCluster = c
		case "/v1/public":
			publicCluster = c
		}
	}
	require.NotNil(t, authedCluster, "expected /v1/items cluster")
	require.NotNil(t, publicCluster, "expected /v1/public cluster")

	assert.Equal(t, []string{"authorization", "cookie"}, authedCluster.ObservedAuth)
	assert.Nil(t, publicCluster.ObservedAuth, "public endpoint should omit observed_auth")
}

func TestAnalyzeTraffic_ObservedAuthCanonicalizesAcrossSamples(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://api.example.com",
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/me",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":1}`,
				RequestHeaders:      map[string]string{"AUTHORIZATION": "Bearer a"},
			},
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/me",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":1}`,
				RequestHeaders:      map[string]string{"authorization": "Bearer b", "X-API-Key": "k"},
			},
		},
	}

	analysis, err := AnalyzeTraffic(capture)
	require.NoError(t, err)

	require.NotEmpty(t, analysis.EndpointClusters)
	cluster := analysis.EndpointClusters[0]
	assert.Equal(t, "/v1/me", cluster.Path)
	assert.Equal(t, []string{"authorization", "x-api-key"}, cluster.ObservedAuth)
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
				ResponseBody:        makeSSRPageWithNextData(`{"props":{}}`),
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

func TestAnalyzeTraffic_TreatsCaptchaPrecheckAsInformational(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://app.example.com",
		Entries: []EnrichedEntry{
			{
				Method:              "POST",
				URL:                 "https://studio-api.example.com/api/c/check",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"present":false,"captcha":false}`,
				RequestHeaders:      map[string]string{"Content-Type": "application/json"},
			},
			{
				Method:              "GET",
				URL:                 "https://studio-api.example.com/api/songs",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"songs":[]}`,
			},
		},
	}

	analysis, err := AnalyzeTraffic(capture)
	require.NoError(t, err)
	require.NotNil(t, analysis.Reachability)

	assert.NotContains(t, protectionLabels(analysis.Protections), "captcha")
	assert.Equal(t, "standard_http", analysis.Reachability.Mode)
	assert.NotContains(t, analysis.GenerationHints, "requires_page_context")
	assert.NotContains(t, analysis.GenerationHints, "requires_protected_client")
	assert.Contains(t, analysis.GenerationHints, "auth_supports_captcha_preflight")
	assert.True(t, analysis.Auth.CaptchaPreflight)
}

func TestAnalyzeTraffic_ClassifiesCaptchaPrecheckDecisions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		url           string
		body          string
		expectedMode  string
		wantCaptcha   bool
		wantPreflight bool
	}{
		{
			name:          "turnstile precheck says no challenge",
			url:           "https://studio-api.example.com/turnstile/check",
			body:          `{"turnstile":false}`,
			expectedMode:  "standard_http",
			wantPreflight: true,
		},
		{
			name:          "hcaptcha precheck says no challenge",
			url:           "https://studio-api.example.com/check_captcha",
			body:          `{"hcaptcha_required":false}`,
			expectedMode:  "standard_http",
			wantPreflight: true,
		},
		{
			name:          "precheck says challenge required",
			url:           "https://studio-api.example.com/captcha/required",
			body:          `{"captcha_required":true}`,
			expectedMode:  "browser_required",
			wantCaptcha:   true,
			wantPreflight: true,
		},
		{
			name:          "precheck reports required challenge in error field",
			url:           "https://studio-api.example.com/api/c/check",
			body:          `{"error":"captcha required"}`,
			expectedMode:  "browser_required",
			wantCaptcha:   true,
			wantPreflight: true,
		},
		{
			name:          "larger turnstile precheck says no challenge",
			url:           "https://studio-api.example.com/turnstile/check",
			body:          `{"turnstile":false,"captcha_required":false,"site_key":"` + strings.Repeat("x", 240) + `"}`,
			expectedMode:  "standard_http",
			wantPreflight: true,
		},
		{
			name:         "non-precheck captcha required response",
			url:          "https://studio-api.example.com/api/login",
			body:         `{"error":"captcha required"}`,
			expectedMode: "browser_required",
			wantCaptcha:  true,
		},
		{
			name:         "non-precheck positive captcha decision",
			url:          "https://studio-api.example.com/api/login",
			body:         `{"captcha":true}`,
			expectedMode: "browser_required",
			wantCaptcha:  true,
		},
		{
			name:         "non-precheck captcha config is informational",
			url:          "https://studio-api.example.com/api/settings",
			body:         `{"turnstile_enabled":false}`,
			expectedMode: "standard_http",
		},
		{
			name:         "non-precheck negative captcha decision is informational",
			url:          "https://studio-api.example.com/api/login",
			body:         `{"captcha_required":false}`,
			expectedMode: "standard_http",
		},
		{
			name:         "non-precheck present field is ordinary JSON",
			url:          "https://studio-api.example.com/api/settings",
			body:         `{"subscription_present":true}`,
			expectedMode: "standard_http",
		},
		{
			name:         "non-precheck required field is ordinary JSON",
			url:          "https://studio-api.example.com/api/forms",
			body:         `{"fields":[{"name":"email","required":true}]}`,
			expectedMode: "standard_http",
		},
		{
			name:         "large non-precheck turnstile body is ordinary JSON",
			url:          "https://studio-api.example.com/api/songs",
			body:         `{"turnstile":{"enabled":false},"fields":[{"name":"email","required":true}],"description":"` + strings.Repeat("x", maxCaptchaChallengeJSONBytes) + `"}`,
			expectedMode: "standard_http",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := &EnrichedCapture{
				TargetURL: "https://app.example.com",
				Entries: []EnrichedEntry{{
					Method:              "POST",
					URL:                 tt.url,
					ResponseStatus:      200,
					ResponseContentType: "application/json",
					ResponseBody:        tt.body,
				}},
			}

			analysis, err := AnalyzeTraffic(capture)
			require.NoError(t, err)
			require.NotNil(t, analysis.Reachability)

			assert.Equal(t, tt.expectedMode, analysis.Reachability.Mode)
			assert.Equal(t, tt.wantPreflight, analysis.Auth.CaptchaPreflight)
			if tt.wantPreflight {
				assert.Contains(t, analysis.GenerationHints, "auth_supports_captcha_preflight")
			}
			if tt.wantCaptcha {
				assert.Contains(t, protectionLabels(analysis.Protections), "captcha")
			} else {
				assert.NotContains(t, protectionLabels(analysis.Protections), "captcha")
			}
		})
	}
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
		Summary: TrafficAnalysisSummary{
			TargetURL:               "https://www.producthunt.com",
			HTTPVersionDistribution: map[string]int{"h3": 12},
		},
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
		Summary: TrafficAnalysisSummary{
			TargetURL:               "https://www.producthunt.com",
			HTTPVersionDistribution: map[string]int{"h3": 12},
		},
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

func TestDetectSSREmbeddedData(t *testing.T) {
	tests := []struct {
		name   string
		entry  EnrichedEntry
		expect string
	}{
		{
			name: "next-data",
			entry: EnrichedEntry{
				ResponseContentType: "text/html",
				ResponseStatus:      200,
				ResponseBody:        makeSSRPage("__NEXT_DATA__", `{"props":{}}`),
			},
			expect: "__NEXT_DATA__",
		},
		{
			name: "nuxt",
			entry: EnrichedEntry{
				ResponseContentType: "text/html",
				ResponseStatus:      200,
				ResponseBody:        makeSSRPage("__NUXT__", `{"state":{}}`),
			},
			expect: "__NUXT__",
		},
		{
			name: "app-initial-state",
			entry: EnrichedEntry{
				ResponseContentType: "text/html",
				ResponseStatus:      200,
				ResponseBody:        makeSSRPage("__APP_INITIAL_STATE__", `{"data":{}}`),
			},
			expect: "__APP_INITIAL_STATE__",
		},
		{
			name: "state-view-yandex",
			entry: EnrichedEntry{
				ResponseContentType: "text/html",
				ResponseStatus:      200,
				ResponseBody:        makeSSRPage("state-view", `{"map":{}}`),
			},
			expect: "state-view",
		},
		{
			name: "ld-json",
			entry: EnrichedEntry{
				ResponseContentType: "text/html",
				ResponseStatus:      200,
				ResponseBody:        makeSSRPage("application/ld+json", `{"@type":"Product"}`),
			},
			expect: "application/ld+json",
		},
		{
			name: "window-prefix",
			entry: EnrichedEntry{
				ResponseContentType: "text/html",
				ResponseStatus:      200,
				ResponseBody:        makeSSRPage("window.__", `{"initial":{}}`),
			},
			expect: "window.__",
		},
		{
			name: "priority-next-data-wins-over-ld-json",
			entry: EnrichedEntry{
				ResponseContentType: "text/html",
				ResponseStatus:      200,
				ResponseBody: `<html><head></head><body>` +
					`<script id="__NEXT_DATA__" type="application/json">{}</script>` +
					`<script type="application/ld+json">{}</script>` +
					strings.Repeat("<!-- pad -->\n", 1000) + `</body></html>`,
			},
			expect: "__NEXT_DATA__",
		},
		{
			name: "rejects-below-size-floor",
			entry: EnrichedEntry{
				ResponseContentType: "text/html",
				ResponseStatus:      200,
				ResponseBody:        `<html><script id="__NEXT_DATA__" type="application/json">{}</script></html>`,
			},
			expect: "",
		},
		{
			name: "rejects-non-2xx-status",
			entry: EnrichedEntry{
				ResponseContentType: "text/html",
				ResponseStatus:      403,
				ResponseBody:        makeSSRPage("__NEXT_DATA__", `{"challenge":true}`),
			},
			expect: "",
		},
		{
			name: "rejects-304-cached",
			entry: EnrichedEntry{
				ResponseContentType: "text/html",
				ResponseStatus:      304,
				ResponseBody:        makeSSRPage("__NEXT_DATA__", `{"cached":true}`),
			},
			expect: "",
		},
		{
			name: "rejects-non-html-content-type",
			entry: EnrichedEntry{
				ResponseContentType: "application/json",
				ResponseStatus:      200,
				ResponseBody:        makeSSRPage("__NEXT_DATA__", `{"shape":{}}`),
			},
			expect: "",
		},
		{
			name: "no-signature-no-match",
			entry: EnrichedEntry{
				ResponseContentType: "text/html",
				ResponseStatus:      200,
				ResponseBody: `<html><head></head><body><p>just a normal page</p>` +
					strings.Repeat("<!-- pad -->\n", 1000) + `</body></html>`,
			},
			expect: "",
		},
		{
			// window.__ matchers must require a state-shaped identifier so
			// analytics globals like window.__gtag don't promote benign
			// pages into the html_scrape path.
			name: "rejects-window-prefix-on-analytics-globals",
			entry: EnrichedEntry{
				ResponseContentType: "text/html",
				ResponseStatus:      200,
				ResponseBody: `<html><head><script>window.__gtag=function(){};window.__ga=1;</script></head><body><p>analytics page</p>` +
					strings.Repeat("<!-- pad -->\n", 1000) + `</body></html>`,
			},
			expect: "",
		},
		{
			// state-view matcher must require the script/attribute shape
			// so CSS class names like state-view-port don't promote.
			name: "rejects-state-view-as-css-fragment",
			entry: EnrichedEntry{
				ResponseContentType: "text/html",
				ResponseStatus:      200,
				ResponseBody: `<html><head></head><body><div class="state-view-port-container"></div>` +
					strings.Repeat("<!-- pad -->\n", 1000) + `</body></html>`,
			},
			expect: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectSSREmbeddedData(tt.entry)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestApplyReachabilityDefaults_HTMLScrapeEmitsEmbeddedJSON(t *testing.T) {
	tests := []struct {
		name             string
		signature        string
		expectedSelector string
	}{
		{"next-data", "__NEXT_DATA__", "script#__NEXT_DATA__"},
		{"nuxt", "__NUXT__", "script#__NUXT__"},
		{"app-initial-state", "__APP_INITIAL_STATE__", "script#__APP_INITIAL_STATE__"},
		{"state-view", "state-view", "script.state-view"},
		{"ld-json", "application/ld+json", `script[type="application/ld+json"]`},
		{"window-prefix-leaves-selector-empty", "window.__", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiSpec := &spec.APISpec{
				Resources: map[string]spec.Resource{
					"pages": {
						Endpoints: map[string]spec.Endpoint{
							"get_index": {
								Method: "GET",
								Path:   "/",
								HTMLExtract: &spec.HTMLExtract{
									Mode:         spec.HTMLExtractModePage,
									LinkPrefixes: []string{"/listing/"},
									Limit:        50,
								},
							},
						},
					},
				},
			}
			analysis := &TrafficAnalysis{
				Reachability: &ReachabilityAnalysis{
					Mode:                 "html_scrape",
					HTMLExtractSignature: tt.signature,
				},
			}
			ApplyReachabilityDefaults(apiSpec, analysis)

			ep := apiSpec.Resources["pages"].Endpoints["get_index"]
			require.NotNil(t, ep.HTMLExtract)
			assert.Equal(t, spec.HTMLExtractModeEmbeddedJSON, ep.HTMLExtract.Mode)
			assert.Equal(t, tt.expectedSelector, ep.HTMLExtract.ScriptSelector)
			assert.Nil(t, ep.HTMLExtract.LinkPrefixes, "link prefixes should clear when promoting to embedded-json")
		})
	}
}

func TestApplyReachabilityDefaults_HTMLScrapeLeavesNonHTMLEndpointsAlone(t *testing.T) {
	apiSpec := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"api": {
				Endpoints: map[string]spec.Endpoint{
					"get_foo": {
						Method: "GET",
						Path:   "/api/foo",
						// No HTMLExtract — this is a JSON endpoint.
					},
				},
			},
		},
	}
	analysis := &TrafficAnalysis{
		Reachability: &ReachabilityAnalysis{
			Mode:                 "html_scrape",
			HTMLExtractSignature: "__NEXT_DATA__",
		},
	}
	ApplyReachabilityDefaults(apiSpec, analysis)

	ep := apiSpec.Resources["api"].Endpoints["get_foo"]
	assert.Nil(t, ep.HTMLExtract, "non-HTML endpoints stay untouched")
}

func TestApplyReachabilityDefaults_HTMLScrapePromotesNestedSubResources(t *testing.T) {
	apiSpec := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"pages": {
				Endpoints: map[string]spec.Endpoint{
					"get_index": {
						Method: "GET",
						Path:   "/",
						HTMLExtract: &spec.HTMLExtract{
							Mode:         spec.HTMLExtractModePage,
							LinkPrefixes: []string{"/parent/"},
						},
					},
				},
				SubResources: map[string]spec.Resource{
					"items": {
						Endpoints: map[string]spec.Endpoint{
							"get_item": {
								Method: "GET",
								Path:   "/parent/{id}",
								HTMLExtract: &spec.HTMLExtract{
									Mode:         spec.HTMLExtractModePage,
									LinkPrefixes: []string{"/parent/"},
								},
							},
						},
					},
				},
			},
		},
	}
	analysis := &TrafficAnalysis{
		Reachability: &ReachabilityAnalysis{
			Mode:                 "html_scrape",
			HTMLExtractSignature: "__NEXT_DATA__",
		},
	}
	ApplyReachabilityDefaults(apiSpec, analysis)

	parent := apiSpec.Resources["pages"].Endpoints["get_index"]
	assert.Equal(t, spec.HTMLExtractModeEmbeddedJSON, parent.HTMLExtract.Mode)
	assert.Equal(t, "script#__NEXT_DATA__", parent.HTMLExtract.ScriptSelector)

	child := apiSpec.Resources["pages"].SubResources["items"].Endpoints["get_item"]
	require.NotNil(t, child.HTMLExtract, "sub-resource endpoint should still have HTMLExtract")
	assert.Equal(t, spec.HTMLExtractModeEmbeddedJSON, child.HTMLExtract.Mode, "sub-resource endpoint must also promote")
	assert.Equal(t, "script#__NEXT_DATA__", child.HTMLExtract.ScriptSelector)
	assert.Nil(t, child.HTMLExtract.LinkPrefixes, "sub-resource link prefixes should clear")
}

func TestApplyReachabilityDefaults_HTMLScrapeNotAppliedWhenSignatureEmpty(t *testing.T) {
	apiSpec := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"pages": {
				Endpoints: map[string]spec.Endpoint{
					"get_index": {
						Method: "GET",
						Path:   "/",
						HTMLExtract: &spec.HTMLExtract{
							Mode: spec.HTMLExtractModePage,
						},
					},
				},
			},
		},
	}
	analysis := &TrafficAnalysis{
		Reachability: &ReachabilityAnalysis{
			Mode: "html_scrape",
			// Signature empty — should not promote
		},
	}
	ApplyReachabilityDefaults(apiSpec, analysis)

	ep := apiSpec.Resources["pages"].Endpoints["get_index"]
	assert.Equal(t, spec.HTMLExtractModePage, ep.HTMLExtract.Mode, "without a signature, mode stays page")
}

func TestClassifyReachability_HTMLScrapePromotion(t *testing.T) {
	tests := []struct {
		name              string
		entries           []EnrichedEntry
		expectedMode      string
		expectedSignature string
	}{
		{
			name: "yandex-shape-cross-subdomain-promotes",
			entries: []EnrichedEntry{
				{
					Method:              "GET",
					URL:                 "https://api.yandex.example.com/maps/api/search",
					ResponseStatus:      403,
					ResponseContentType: "application/json",
					ResponseBody:        `{"error":"captcha required","redirect":"/showcaptcha?retpath=/"}`,
				},
				{
					Method:              "GET",
					URL:                 "https://www.yandex.example.com/maps/org/foo/12345/",
					ResponseStatus:      200,
					ResponseContentType: "text/html",
					ResponseBody:        makeSSRPage("state-view", `{"org":{"name":"Cafe Bistro"}}`),
				},
			},
			expectedMode:      "html_scrape",
			expectedSignature: "state-view",
		},
		{
			name: "same-host-promotes-with-next-data",
			entries: []EnrichedEntry{
				{
					Method:              "GET",
					URL:                 "https://example.com/api/foo",
					ResponseStatus:      403,
					ResponseContentType: "application/json",
					ResponseBody:        `{"error":"captcha required"}`,
				},
				{
					Method:              "GET",
					URL:                 "https://example.com/foo",
					ResponseStatus:      200,
					ResponseContentType: "text/html",
					ResponseBody:        makeSSRPage("__NEXT_DATA__", `{"foo":1}`),
				},
			},
			expectedMode:      "html_scrape",
			expectedSignature: "__NEXT_DATA__",
		},
		{
			name: "different-registered-domain-does-not-promote",
			entries: []EnrichedEntry{
				{
					Method:              "GET",
					URL:                 "https://api.example.com/foo",
					ResponseStatus:      403,
					ResponseContentType: "application/json",
					ResponseBody:        `{"error":"captcha required"}`,
				},
				{
					Method:              "GET",
					URL:                 "https://other-site.com/foo",
					ResponseStatus:      200,
					ResponseContentType: "text/html",
					ResponseBody:        makeSSRPage("__NEXT_DATA__", `{}`),
				},
			},
			expectedMode:      "browser_required",
			expectedSignature: "",
		},
		{
			name: "cloudflare-only-stays-on-clearance-mode",
			entries: []EnrichedEntry{
				{
					Method:              "GET",
					URL:                 "https://api.example.com/foo",
					ResponseStatus:      403,
					ResponseContentType: "application/json",
					ResponseBody:        `{"error":"blocked"}`,
					ResponseHeaders:     map[string]string{"Server": "cloudflare", "CF-Ray": "abc"},
				},
				{
					Method:              "GET",
					URL:                 "https://www.example.com/foo",
					ResponseStatus:      200,
					ResponseContentType: "text/html",
					ResponseBody:        makeSSRPage("__NEXT_DATA__", `{}`),
				},
			},
			// Cloudflare is not in the captcha tier — promotion does
			// not fire. The existing browser_http branch handles this
			// case (cloudflare on the API entry routes to browser_http).
			expectedMode:      "browser_http",
			expectedSignature: "",
		},
		{
			name: "no-protection-no-promotion",
			entries: []EnrichedEntry{
				{
					Method:              "GET",
					URL:                 "https://example.com/api/foo",
					ResponseStatus:      200,
					ResponseContentType: "application/json",
					ResponseBody:        `{"foo":1}`,
				},
				{
					Method:              "GET",
					URL:                 "https://example.com/foo",
					ResponseStatus:      200,
					ResponseContentType: "text/html",
					ResponseBody:        makeSSRPage("__NEXT_DATA__", `{}`),
				},
			},
			expectedMode:      "standard_http",
			expectedSignature: "",
		},
		{
			name: "captcha-without-ssr-sibling-stays-browser-required",
			entries: []EnrichedEntry{
				{
					Method:              "GET",
					URL:                 "https://example.com/api/foo",
					ResponseStatus:      403,
					ResponseContentType: "application/json",
					ResponseBody:        `{"error":"captcha required"}`,
				},
			},
			expectedMode:      "browser_required",
			expectedSignature: "",
		},
		{
			name: "aws-waf-captcha-tier-promotes",
			entries: []EnrichedEntry{
				{
					Method:              "GET",
					URL:                 "https://api.example.com/foo",
					ResponseStatus:      403,
					ResponseContentType: "application/json",
					ResponseBody:        `{"error":"blocked by AWS WAF","captcha":"required"}`,
					ResponseHeaders:     map[string]string{"x-amzn-RequestId": "abc"},
				},
				{
					Method:              "GET",
					URL:                 "https://www.example.com/foo",
					ResponseStatus:      200,
					ResponseContentType: "text/html",
					ResponseBody:        makeSSRPage("__NUXT__", `{}`),
				},
			},
			expectedMode:      "html_scrape",
			expectedSignature: "__NUXT__",
		},
		{
			name: "vercel-challenge-captcha-tier-promotes",
			entries: []EnrichedEntry{
				{
					Method:              "GET",
					URL:                 "https://api.example.com/foo",
					ResponseStatus:      403,
					ResponseContentType: "application/json",
					ResponseBody:        `{"error":"vercel challenge"}`,
					ResponseHeaders:     map[string]string{"x-vercel-mitigated": "challenge"},
				},
				{
					Method:              "GET",
					URL:                 "https://www.example.com/foo",
					ResponseStatus:      200,
					ResponseContentType: "text/html",
					ResponseBody:        makeSSRPage("__APP_INITIAL_STATE__", `{}`),
				},
			},
			expectedMode:      "html_scrape",
			expectedSignature: "__APP_INITIAL_STATE__",
		},
		{
			name: "bot-challenge-captcha-tier-promotes",
			entries: []EnrichedEntry{
				{
					Method:              "GET",
					URL:                 "https://api.example.com/foo",
					ResponseStatus:      403,
					ResponseContentType: "application/json",
					ResponseBody:        `{"error":"managed challenge"}`,
					ResponseHeaders:     map[string]string{"cf-mitigated": "challenge"},
				},
				{
					Method:              "GET",
					URL:                 "https://www.example.com/foo",
					ResponseStatus:      200,
					ResponseContentType: "text/html",
					ResponseBody:        makeSSRPage("application/ld+json", `{"@type":"Product"}`),
				},
			},
			expectedMode:      "html_scrape",
			expectedSignature: "application/ld+json",
		},
		{
			// Ordering guard: matching SSR entry is iterated before a
			// non-matching cross-domain SSR entry. The selected signature
			// must come from the entry that satisfies same-eTLD+1, not
			// from the last SSR entry seen by the protocol scanner.
			name: "multi-ssr-signature-attributed-to-matching-entry",
			entries: []EnrichedEntry{
				{
					Method:              "GET",
					URL:                 "https://api.example.com/foo",
					ResponseStatus:      403,
					ResponseContentType: "application/json",
					ResponseBody:        `{"error":"captcha required"}`,
				},
				{
					Method:              "GET",
					URL:                 "https://www.example.com/foo",
					ResponseStatus:      200,
					ResponseContentType: "text/html",
					ResponseBody:        makeSSRPage("__NEXT_DATA__", `{}`),
				},
				{
					Method:              "GET",
					URL:                 "https://unrelated-site.com/bar",
					ResponseStatus:      200,
					ResponseContentType: "text/html",
					ResponseBody:        makeSSRPage("__NUXT__", `{}`),
				},
			},
			expectedMode:      "html_scrape",
			expectedSignature: "__NEXT_DATA__",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := &EnrichedCapture{Entries: tt.entries}
			analysis, err := AnalyzeTraffic(capture)
			require.NoError(t, err)
			require.NotNil(t, analysis.Reachability)
			assert.Equal(t, tt.expectedMode, analysis.Reachability.Mode)
			assert.Equal(t, tt.expectedSignature, analysis.Reachability.HTMLExtractSignature)
		})
	}
}

func TestSameRegisteredDomain(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"example.com", "example.com", true},
		{"api.example.com", "www.example.com", true},
		{"api.example.com", "example.com", true},
		{"example.com", "other-site.com", false},
		{"api.example.co.uk", "www.example.co.uk", true},
		{"example.co.uk", "example.com", false},
		{"", "example.com", false},
		{"EXAMPLE.com", "example.COM", true},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			assert.Equal(t, tt.want, sameRegisteredDomain(tt.a, tt.b))
		})
	}
}

func TestDetectProtocols_SSREmbeddedDataSurfacesSignatureInDetails(t *testing.T) {
	capture := &EnrichedCapture{
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://example.com/maps/org/foo/12345/",
				ResponseStatus:      200,
				ResponseContentType: "text/html",
				ResponseBody:        makeSSRPage("state-view", `{"org":{"name":"Cafe Bistro"}}`),
			},
		},
	}
	analysis, err := AnalyzeTraffic(capture)
	require.NoError(t, err)

	var ssr *ProtocolObservation
	for i := range analysis.Protocols {
		if analysis.Protocols[i].Label == "ssr_embedded_data" {
			ssr = &analysis.Protocols[i]
			break
		}
	}
	require.NotNil(t, ssr, "ssr_embedded_data protocol must be observed")
	assert.Equal(t, "state-view", ssr.Details["signature"])
}

// makeSSRPage builds an HTML body carrying the requested SSR signature
// marker and pads past ssrEmbeddedDataMinBodySize. Real SSR captures are
// 10KB+; the filler simulates that without embedding a real page. The
// __NEXT_DATA__ shape also includes the `id="__next"` mount node so
// `looksBrowserRendered` fires on the same fixture (matches typical
// Next.js output).
func makeSSRPage(signature, payload string) string {
	var inner string
	switch signature {
	case "__NEXT_DATA__":
		inner = `<script id="__NEXT_DATA__" type="application/json">` + payload + `</script><div id="__next"></div>`
	case "__NUXT__":
		inner = `<script>window.__NUXT__=` + payload + `</script>`
	case "__APP_INITIAL_STATE__":
		inner = `<script id="__APP_INITIAL_STATE__" type="application/json">` + payload + `</script>`
	case "state-view":
		inner = `<script type="application/json" class="state-view">` + payload + `</script>`
	case "application/ld+json":
		inner = `<script type="application/ld+json">` + payload + `</script>`
	case "window.__":
		inner = `<script>window.__INITIAL_STATE__=` + payload + `</script>`
	default:
		inner = payload
	}
	filler := strings.Repeat("<!-- ssr -->\n", 1000)
	return `<html><head></head><body>` + inner + filler + `</body></html>`
}

// makeSSRPageWithNextData is the most common shorthand for tests that
// only need a Next.js-shape body.
func makeSSRPageWithNextData(payload string) string {
	return makeSSRPage("__NEXT_DATA__", payload)
}
