package browsersniff

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		entries          []EnrichedEntry
		wantAPIURLs      []string
		wantNoiseURLs    []string
		wantClassByURL   map[string]string
		wantIsNoiseByURL map[string]bool
	}{
		{
			name: "json api and analytics tracker are separated",
			entries: []EnrichedEntry{
				{
					Method:              "GET",
					URL:                 "https://example.com/api/users",
					ResponseContentType: "application/json; charset=utf-8",
					ResponseBody:        `{"users":[{"id":1}]}`,
				},
				{
					Method:              "GET",
					URL:                 "https://www.google-analytics.com/g/collect?v=2",
					ResponseContentType: "text/html",
				},
			},
			wantAPIURLs:   []string{"https://example.com/api/users"},
			wantNoiseURLs: []string{"https://www.google-analytics.com/g/collect?v=2"},
			wantClassByURL: map[string]string{
				"https://example.com/api/users":                  "api",
				"https://www.google-analytics.com/g/collect?v=2": "noise",
			},
			wantIsNoiseByURL: map[string]bool{
				"https://example.com/api/users":                  false,
				"https://www.google-analytics.com/g/collect?v=2": true,
			},
		},
		{
			name: "google analytics is noise",
			entries: []EnrichedEntry{
				{
					Method:              "POST",
					URL:                 "https://google-analytics.com/j/collect",
					ResponseContentType: "application/json",
					ResponseBody:        `{}`,
				},
			},
			wantNoiseURLs: []string{"https://google-analytics.com/j/collect"},
			wantClassByURL: map[string]string{
				"https://google-analytics.com/j/collect": "noise",
			},
			wantIsNoiseByURL: map[string]bool{
				"https://google-analytics.com/j/collect": true,
			},
		},
		{
			name: "post form with json response is api",
			entries: []EnrichedEntry{
				{
					Method:              "POST",
					URL:                 "https://example.com/session",
					ResponseContentType: "application/json",
					ResponseBody:        `{"ok":true}`,
					RequestHeaders: map[string]string{
						"Content-Type": "application/x-www-form-urlencoded",
					},
				},
			},
			wantAPIURLs: []string{"https://example.com/session"},
			wantClassByURL: map[string]string{
				"https://example.com/session": "api",
			},
			wantIsNoiseByURL: map[string]bool{
				"https://example.com/session": false,
			},
		},
		{
			name: "all noise entries produce empty api list",
			entries: []EnrichedEntry{
				{
					Method:              "GET",
					URL:                 "https://cdn.example.com/styles.css",
					ResponseContentType: "text/css",
				},
				{
					Method:              "GET",
					URL:                 "https://cdn.example.com/logo.png",
					ResponseContentType: "image/png",
				},
			},
			wantNoiseURLs: []string{
				"https://cdn.example.com/styles.css",
				"https://cdn.example.com/logo.png",
			},
			wantClassByURL: map[string]string{
				"https://cdn.example.com/styles.css": "noise",
				"https://cdn.example.com/logo.png":   "noise",
			},
			wantIsNoiseByURL: map[string]bool{
				"https://cdn.example.com/styles.css": true,
				"https://cdn.example.com/logo.png":   true,
			},
		},
		{
			name: "youtube internal endpoint is api",
			entries: []EnrichedEntry{
				{
					Method:              "POST",
					URL:                 "https://www.youtube.com/youtubei/v1/player?prettyPrint=false",
					ResponseContentType: "application/json",
					ResponseBody:        `{"videoDetails":{"videoId":"abc123"}}`,
				},
			},
			wantAPIURLs: []string{"https://www.youtube.com/youtubei/v1/player?prettyPrint=false"},
			wantClassByURL: map[string]string{
				"https://www.youtube.com/youtubei/v1/player?prettyPrint=false": "api",
			},
			wantIsNoiseByURL: map[string]bool{
				"https://www.youtube.com/youtubei/v1/player?prettyPrint=false": false,
			},
		},
		{
			name: "known telemetry host is noise",
			entries: []EnrichedEntry{
				{
					Method:              "POST",
					URL:                 "https://browser-intake-datadoghq.com/api/v2/rum?dd-api-key=wrong-service",
					RequestHeaders:      map[string]string{"Content-Type": "application/json"},
					ResponseContentType: "application/json",
					ResponseBody:        `{"status":"ok"}`,
				},
			},
			wantNoiseURLs: []string{"https://browser-intake-datadoghq.com/api/v2/rum?dd-api-key=wrong-service"},
			wantClassByURL: map[string]string{
				"https://browser-intake-datadoghq.com/api/v2/rum?dd-api-key=wrong-service": "noise",
			},
			wantIsNoiseByURL: map[string]bool{
				"https://browser-intake-datadoghq.com/api/v2/rum?dd-api-key=wrong-service": true,
			},
		},
		{
			name: "known telemetry hosts with json post bodies are noise",
			entries: []EnrichedEntry{
				{
					Method:              "POST",
					URL:                 "https://vortex.data.microsoft.com/collect/v1",
					RequestHeaders:      map[string]string{"Content-Type": "application/json"},
					RequestBody:         `{"name":"Microsoft.ApplicationInsights.PageView"}`,
					ResponseContentType: "application/json",
					ResponseBody:        `{"itemsReceived":1,"itemsAccepted":1}`,
				},
				{
					Method:              "POST",
					URL:                 "https://dc.services.visualstudio.com/v2/track",
					RequestHeaders:      map[string]string{"Content-Type": "application/json"},
					RequestBody:         `[{"name":"Microsoft.ApplicationInsights.Event"}]`,
					ResponseContentType: "application/json",
					ResponseBody:        `{"itemsReceived":1,"itemsAccepted":1}`,
				},
				{
					Method:              "POST",
					URL:                 "https://analytics.google.com/g/collect",
					RequestHeaders:      map[string]string{"Content-Type": "application/json"},
					RequestBody:         `{"client_id":"abc"}`,
					ResponseContentType: "application/json",
					ResponseBody:        `{}`,
				},
			},
			wantNoiseURLs: []string{
				"https://vortex.data.microsoft.com/collect/v1",
				"https://dc.services.visualstudio.com/v2/track",
				"https://analytics.google.com/g/collect",
			},
			wantClassByURL: map[string]string{
				"https://vortex.data.microsoft.com/collect/v1":  "noise",
				"https://dc.services.visualstudio.com/v2/track": "noise",
				"https://analytics.google.com/g/collect":        "noise",
			},
			wantIsNoiseByURL: map[string]bool{
				"https://vortex.data.microsoft.com/collect/v1":  true,
				"https://dc.services.visualstudio.com/v2/track": true,
				"https://analytics.google.com/g/collect":        true,
			},
		},
		{
			name: "first-party intake path without telemetry query remains api",
			entries: []EnrichedEntry{
				{
					Method:              "GET",
					URL:                 "https://api.example.com/intake/v1/forms",
					ResponseContentType: "application/json",
					ResponseBody:        `{"forms":[{"id":"form_1"}]}`,
				},
			},
			wantAPIURLs: []string{"https://api.example.com/intake/v1/forms"},
			wantClassByURL: map[string]string{
				"https://api.example.com/intake/v1/forms": "api",
			},
			wantIsNoiseByURL: map[string]bool{
				"https://api.example.com/intake/v1/forms": false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			api, noise := ClassifyEntries(tt.entries)

			assert.Equal(t, emptyStrings(tt.wantAPIURLs), entryURLs(api))
			assert.Equal(t, emptyStrings(tt.wantNoiseURLs), entryURLs(noise))

			for _, entry := range append(api, noise...) {
				assert.Equal(t, tt.wantClassByURL[entry.URL], entry.Classification)
				assert.Equal(t, tt.wantIsNoiseByURL[entry.URL], entry.IsNoise)
			}
		})
	}
}

func TestClassifyEntries_TelemetryPathsRequireTelemetryQueryEvidence(t *testing.T) {
	t.Parallel()

	entries := []EnrichedEntry{
		{Method: "POST", URL: "https://events.example.com/sentry/envelope/?sentry_key=abc", ResponseContentType: "application/json", ResponseBody: `{"ok":true}`},
		{Method: "POST", URL: "https://events.example.com/intake/v1/?dd-api-key=abc", ResponseContentType: "application/json", ResponseBody: `{"ok":true}`},
		{Method: "POST", URL: "https://events.example.com/intercom/ping?intercom-device-id=abc", ResponseContentType: "application/json", ResponseBody: `{"ok":true}`},
		{Method: "POST", URL: "https://events.example.com/rum?dd-api-key=abc", ResponseContentType: "application/json", ResponseBody: `{"ok":true}`},
	}

	api, noise := ClassifyEntries(entries)

	assert.Empty(t, api)
	assert.Equal(t, entryURLs(entries), entryURLs(noise))
}

func TestDeduplicateEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		entries             []EnrichedEntry
		wantMethods         []string
		wantNormalizedPaths []string
		wantGroupSizes      []int
	}{
		{
			name: "numeric ids normalize to parent-derived id placeholder",
			entries: []EnrichedEntry{
				{Method: "GET", URL: "https://example.com/users/123?expand=true"},
				{Method: "GET", URL: "https://example.com/users/456"},
			},
			wantMethods:         []string{"GET"},
			wantNormalizedPaths: []string{"/users/{user_id}"},
			wantGroupSizes:      []int{2},
		},
		{
			name: "uuid segment normalizes to parent-derived id placeholder",
			entries: []EnrichedEntry{
				{Method: "GET", URL: "https://example.com/orders/550e8400-e29b-41d4-a716-446655440000"},
				{Method: "GET", URL: "https://example.com/orders/123e4567-e89b-12d3-a456-426614174000?include=items"},
			},
			wantMethods:         []string{"GET"},
			wantNormalizedPaths: []string{"/orders/{order_id}"},
			wantGroupSizes:      []int{2},
		},
		{
			name: "prefixed application ids normalize to parent-derived placeholder",
			entries: []EnrichedEntry{
				{Method: "GET", URL: "https://example.com/api/v1/tables/t_0t3vswhpKogASf2XZpW"},
				{Method: "GET", URL: "https://example.com/api/v1/tables/t_zzAAbbCC11223344"},
			},
			wantMethods:         []string{"GET"},
			wantNormalizedPaths: []string{"/api/v1/tables/{table_id}"},
			wantGroupSizes:      []int{2},
		},
		{
			name: "colon-composite form ids collapse to parent-derived placeholder",
			entries: []EnrichedEntry{
				{Method: "POST", URL: "https://example.com/suite/api/forms/creations/create-image:reference:gpt-image-2"},
				{Method: "POST", URL: "https://example.com/suite/api/forms/creations/create-image:reference:gpt-image-3"},
			},
			wantMethods:         []string{"POST"},
			wantNormalizedPaths: []string{"/suite/api/forms/creations/{creation_id}"},
			wantGroupSizes:      []int{2},
		},
		{
			name: "long opaque application ids normalize via cross-entry variance",
			entries: []EnrichedEntry{
				{Method: "GET", URL: "https://example.com/suite/api/history/Zu2uNCmGDnmNCel8gbFQ"},
				{Method: "GET", URL: "https://example.com/suite/api/history/AbcDefGhi1234567890X"},
			},
			wantMethods:         []string{"GET"},
			wantNormalizedPaths: []string{"/suite/api/history/{history_id}"},
			wantGroupSizes:      []int{2},
		},
		{
			name: "static endpoints with distinct literal segments stay separate",
			entries: []EnrichedEntry{
				{Method: "GET", URL: "https://example.com/api/health"},
				{Method: "GET", URL: "https://example.com/api/version"},
			},
			wantMethods:         []string{"GET", "GET"},
			wantNormalizedPaths: []string{"/api/health", "/api/version"},
			wantGroupSizes:      []int{1, 1},
		},
		{
			// Short opaque IDs (`abc123`, `xyz456`) fail every per-segment ID
			// regex in isolation — they're too short for the long-alnum or
			// hash patterns and have no type prefix. The cross-entry variance
			// pass is the only thing that can identify them, because their
			// id-ness becomes obvious only when two entries land at the same
			// position with different data-shaped values.
			name: "short opaque ids parametrize via cross-entry variance",
			entries: []EnrichedEntry{
				{Method: "GET", URL: "https://example.com/api/messages/abc123"},
				{Method: "GET", URL: "https://example.com/api/messages/xyz456"},
			},
			wantMethods:         []string{"GET"},
			wantNormalizedPaths: []string{"/api/messages/{message_id}"},
			wantGroupSizes:      []int{2},
		},
		{
			// Consecutive numeric ID segments share the same parent
			// (`resources` for both). Without disambiguation the resulting
			// path would carry two `{resource_id}` placeholders, which breaks
			// downstream OpenAPI generation (duplicate path-parameter names
			// within a single path template are rejected).
			name: "consecutive ids under same parent get disambiguated names",
			entries: []EnrichedEntry{
				{Method: "GET", URL: "https://example.com/resources/123/456"},
				{Method: "GET", URL: "https://example.com/resources/789/012"},
			},
			wantMethods:         []string{"GET"},
			wantNormalizedPaths: []string{"/resources/{resource_id}/{resource_id_2}"},
			wantGroupSizes:      []int{2},
		},
		{
			// Variance pass must disambiguate against placeholders the
			// per-segment normalizer already placed. Position 2 (`123`/`456`)
			// becomes `{resource_id}` in per-segment normalization; position
			// 3 (`abc123`/`xyz456`) is short-opaque-with-digit, not a strong
			// per-segment ID shape but promoted by the variance pass. The
			// walker for position 3 would find `resources` again — placeholder
			// emission must see the existing `{resource_id}` and disambiguate
			// to `{resource_id_2}` rather than emit a duplicate.
			name: "variance pass disambiguates against per-segment placeholder",
			entries: []EnrichedEntry{
				{Method: "GET", URL: "https://example.com/resources/123/abc123"},
				{Method: "GET", URL: "https://example.com/resources/456/xyz456"},
			},
			wantMethods:         []string{"GET"},
			wantNormalizedPaths: []string{"/resources/{resource_id}/{resource_id_2}"},
			wantGroupSizes:      []int{2},
		},
		{
			// PascalCase route literals (common in ASP.NET, gRPC-HTTP
			// transcoding) must NOT collapse via the variance pass.
			// `CreateDocument` and `ListDocuments` are distinct action-style
			// endpoints, both digit-free; the parameterizability check
			// requires a digit specifically to avoid this false positive.
			name: "pascal-case route literals stay separate",
			entries: []EnrichedEntry{
				{Method: "GET", URL: "https://example.com/api/CreateDocument"},
				{Method: "GET", URL: "https://example.com/api/ListDocuments"},
			},
			wantMethods:         []string{"GET", "GET"},
			wantNormalizedPaths: []string{"/api/CreateDocument", "/api/ListDocuments"},
			wantGroupSizes:      []int{1, 1},
		},
		{
			// Three consecutive same-parent IDs: per-segment normalization
			// produces /resources/{resource_id}/{resource_id_2}/abc123. The
			// variance pass then promotes position 4. Naive counter-only
			// disambiguation would re-emit {resource_id_2} (already in the
			// path); the implementation must keep advancing the counter past
			// every existing suffix and land on {resource_id_3}.
			name: "triple consecutive ids disambiguate to _3",
			entries: []EnrichedEntry{
				{Method: "GET", URL: "https://example.com/resources/123/456/abc123"},
				{Method: "GET", URL: "https://example.com/resources/789/012/xyz456"},
			},
			wantMethods:         []string{"GET"},
			wantNormalizedPaths: []string{"/resources/{resource_id}/{resource_id_2}/{resource_id_3}"},
			wantGroupSizes:      []int{2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			groups := DeduplicateEndpoints(tt.entries)

			assert.Equal(t, tt.wantMethods, groupMethods(groups))
			assert.Equal(t, tt.wantNormalizedPaths, groupPaths(groups))
			assert.Equal(t, tt.wantGroupSizes, groupSizes(groups))
		})
	}
}

// TestDeduplicateEndpoints_SingleEntryHeuristics covers the per-segment ID
// shapes that must parametrize even when only one HAR entry is available, so
// the cross-entry variance pass has nothing to compare against.
func TestDeduplicateEndpoints_SingleEntryHeuristics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		url      string
		wantPath string
	}{
		{"uuid v4", "https://example.com/widgets/550e8400-e29b-41d4-a716-446655440000", "/widgets/{widget_id}"},
		{"long hex hash", "https://example.com/blobs/0123456789abcdef0123456789abcdef", "/blobs/{blob_id}"},
		{"numeric id under resource", "https://example.com/workspaces/125600", "/workspaces/{workspace_id}"},
		{"prefixed application id under resource", "https://example.com/records/r_0tf32xmAhEgGSh3TDWR", "/records/{record_id}"},
		{"colon-composite under resource", "https://example.com/forms/creations/create-image:reference:gpt-image-2", "/forms/creations/{creation_id}"},
		{"long base62 id under resource", "https://example.com/history/Zu2uNCmGDnmNCel8gbFQ", "/history/{history_id}"},
		{"iso date under route", "https://example.com/predict/2026-08-16", "/predict/{date}"},
		{"adjacent uppercase compact codes", "https://example.com/predict/FR/STN/DUB/2026-08-16", "/predict/{segment_0}/{segment_1}/{segment_2}/{date}"},
		{"literal short segments remain literal", "https://example.com/api/health", "/api/health"},
		{"single uppercase literal remains literal", "https://example.com/api/JSON", "/api/JSON"},
		{"version segment retains literal v1 framing", "https://example.com/api/v1/users", "/api/v1/users"},
		{"consecutive ids under same parent disambiguate", "https://example.com/resources/123/456", "/resources/{resource_id}/{resource_id_2}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			groups := DeduplicateEndpoints([]EnrichedEntry{{Method: "GET", URL: tt.url}})
			require.Len(t, groups, 1)
			assert.Equal(t, tt.wantPath, groups[0].NormalizedPath)
		})
	}
}

// TestDeduplicateTrafficEndpoints_VariancePassRespectsHost verifies that the
// cross-entry variance pass cannot merge groups from different hosts. The
// upstream host-aware key keeps them separate at dedup time, but
// collapseVariantGroups originally bucketed only by (method, length, mask) and
// would collapse them into a single mixed-host group. The skeleton key now
// includes Host so this no longer happens.
func TestDeduplicateTrafficEndpoints_VariancePassRespectsHost(t *testing.T) {
	t.Parallel()

	entries := []EnrichedEntry{
		{Method: "GET", URL: "https://api.service1.com/orders/ORD123"},
		{Method: "GET", URL: "https://api.service2.com/orders/SHP456"},
	}

	groups := DeduplicateTrafficEndpoints(entries)

	require.Len(t, groups, 2, "different hosts must stay in separate groups")
	hosts := map[string]struct{}{}
	for _, g := range groups {
		hosts[g.Host] = struct{}{}
	}
	assert.Equal(t, map[string]struct{}{
		"api.service1.com": {},
		"api.service2.com": {},
	}, hosts)
}

func entryURLs(entries []EnrichedEntry) []string {
	urls := make([]string, 0, len(entries))
	for _, entry := range entries {
		urls = append(urls, entry.URL)
	}

	return urls
}

func groupMethods(groups []EndpointGroup) []string {
	methods := make([]string, 0, len(groups))
	for _, group := range groups {
		methods = append(methods, group.Method)
	}

	return methods
}

func groupPaths(groups []EndpointGroup) []string {
	paths := make([]string, 0, len(groups))
	for _, group := range groups {
		paths = append(paths, group.NormalizedPath)
	}

	return paths
}

func groupSizes(groups []EndpointGroup) []int {
	sizes := make([]int, 0, len(groups))
	for _, group := range groups {
		sizes = append(sizes, len(group.Entries))
	}

	return sizes
}

func emptyStrings(values []string) []string {
	if values == nil {
		return []string{}
	}

	return values
}

func TestIncludeListRescuesBlockedHost(t *testing.T) {
	// Not t.Parallel: mutates the package-level include-list state.
	SetAdditionalIncludeList([]string{"google-analytics.com"})
	defer SetAdditionalIncludeList(nil)

	// google-analytics.com is on the DefaultBlocklist and would normally
	// score negative. The include match should force a positive score.
	entries := []EnrichedEntry{
		{
			Method:              "GET",
			URL:                 "https://www.google-analytics.com/collect?v=1",
			ResponseContentType: "image/gif",
			ResponseBody:        "",
		},
	}
	api, noise := ClassifyEntries(entries)
	assert.Len(t, api, 1, "include match should rescue google-analytics endpoint")
	assert.Empty(t, noise)
}

func TestIncludeListRescuesStaticAssetByPath(t *testing.T) {
	SetAdditionalIncludeList([]string{"/track/important"})
	defer SetAdditionalIncludeList(nil)

	entries := []EnrichedEntry{
		{
			Method:              "GET",
			URL:                 "https://example.com/track/important.js",
			ResponseContentType: "application/javascript",
			ResponseBody:        "",
		},
	}
	api, noise := ClassifyEntries(entries)
	assert.Len(t, api, 1, "include path match should rescue static asset")
	assert.Empty(t, noise)
}

func TestIncludeListEmptyPreservesDefaultBehavior(t *testing.T) {
	SetAdditionalIncludeList(nil)

	entries := []EnrichedEntry{
		{
			Method:              "GET",
			URL:                 "https://www.google-analytics.com/collect",
			ResponseContentType: "image/gif",
		},
	}
	api, noise := ClassifyEntries(entries)
	assert.Empty(t, api, "without include, analytics endpoint stays in noise")
	assert.Len(t, noise, 1)
}

func TestIncludeListWinsOverBlocklistOverlap(t *testing.T) {
	SetAdditionalBlocklist([]string{"api.partner.com"})
	SetAdditionalIncludeList([]string{"api.partner.com"})
	defer SetAdditionalBlocklist(nil)
	defer SetAdditionalIncludeList(nil)

	entries := []EnrichedEntry{
		{
			Method:              "GET",
			URL:                 "https://api.partner.com/v1/data",
			ResponseContentType: "application/json",
			ResponseBody:        `{"ok":true}`,
		},
	}
	api, _ := ClassifyEntries(entries)
	assert.Len(t, api, 1, "include should win over overlapping blocklist entry")
}

func TestFilterEndpointsByMinSamples_DropsSingletons(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://api.example.com",
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/popular",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":1}`,
				RequestHeaders:      map[string]string{"Accept": "application/json"},
			},
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/popular",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":2}`,
				RequestHeaders:      map[string]string{"Accept": "application/json"},
			},
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/rare",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":1}`,
				RequestHeaders:      map[string]string{"Accept": "application/json"},
			},
		},
	}

	apiSpec, err := AnalyzeCapture(capture)
	if err != nil {
		t.Fatal(err)
	}

	dropped := FilterEndpointsByMinSamples(apiSpec, capture, 2)
	assert.Equal(t, 1, dropped, "rare endpoint with single sample should drop")

	// The rare endpoint should be gone from the spec, popular should remain.
	found := map[string]bool{}
	for _, resource := range apiSpec.Resources {
		for _, endpoint := range resource.Endpoints {
			found[endpoint.Path] = true
		}
	}
	assert.True(t, found["/v1/popular"], "popular endpoint should remain")
	assert.False(t, found["/v1/rare"], "rare endpoint should be filtered")
}

func TestFilterEndpointsByMinSamples_DefaultIsNoop(t *testing.T) {
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
	apiSpec, err := AnalyzeCapture(capture)
	if err != nil {
		t.Fatal(err)
	}
	before := 0
	for _, r := range apiSpec.Resources {
		before += len(r.Endpoints)
	}

	dropped := FilterEndpointsByMinSamples(apiSpec, capture, 1)
	assert.Zero(t, dropped, "--min-samples=1 (default) must be a no-op")

	after := 0
	for _, r := range apiSpec.Resources {
		after += len(r.Endpoints)
	}
	assert.Equal(t, before, after, "endpoint count must not change with default min-samples")
}
