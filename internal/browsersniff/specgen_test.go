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

func TestAnalyze(t *testing.T) {
	t.Parallel()

	apiSpec, err := Analyze(filepath.Join("..", "..", "testdata", "sniff", "sample-enriched.json"))
	require.NoError(t, err)
	require.NotNil(t, apiSpec)

	assert.Equal(t, "hn-algolia", apiSpec.Name)
	require.NotEmpty(t, apiSpec.Resources)

	foundEndpointWithParams := false
	for _, resource := range apiSpec.Resources {
		require.NotEmpty(t, resource.Endpoints)
		for _, endpoint := range resource.Endpoints {
			if len(endpoint.Params) > 0 {
				foundEndpointWithParams = true
			}
		}
	}

	assert.True(t, foundEndpointWithParams)
	assert.Equal(t, "sniffed", apiSpec.SpecSource)
	assert.NoError(t, apiSpec.Validate())
}

func TestWriteSpec(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:        "example",
		Description: "Example API",
		Version:     "0.1.0",
		BaseURL:     "https://api.example.com",
		Auth:        spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/example-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"widgets": {
				Description: "Operations on widgets",
				Endpoints: map[string]spec.Endpoint{
					"list_widgets": {
						Method: "GET",
						Path:   "/widgets",
						Response: spec.ResponseDef{
							Type: "object",
							Item: "widgets",
						},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{},
	}

	outputPath := filepath.Join(t.TempDir(), "nested", "spec.yaml")
	err := WriteSpec(apiSpec, outputPath)
	require.NoError(t, err)

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	parsed, err := spec.ParseBytes(data)
	require.NoError(t, err)
	assert.Equal(t, apiSpec.Name, parsed.Name)
	assert.Equal(t, apiSpec.BaseURL, parsed.BaseURL)
}

func TestDeriveNameFromURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "strips www and tld",
			raw:  "https://www.youtube.com",
			want: "youtube",
		},
		{
			name: "keeps meaningful subdomain",
			raw:  "https://hn.algolia.com",
			want: "hn-algolia",
		},
		{
			name: "drops generic api prefix",
			raw:  "https://api.example.com",
			want: "example",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, deriveNameFromURL(tt.raw))
		})
	}
}

func TestAnalyzeCapture_UsesCapturedBearerAuth(t *testing.T) {
	t.Parallel()

	capture, err := ParseEnriched(filepath.Join("..", "..", "testdata", "sniff", "sample-auth-capture.json"))
	require.NoError(t, err)

	apiSpec, err := AnalyzeCapture(capture)
	require.NoError(t, err)

	assert.Equal(t, "bearer_token", apiSpec.Auth.Type)
	assert.Equal(t, "Authorization", apiSpec.Auth.Header)
}

func TestAnalyzeCapture_UsesCapturedCookieAuth(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://api.spotify.com",
		Auth: &AuthCapture{
			Cookies:     []string{"_session=abc"},
			Type:        "cookie",
			BoundDomain: "spotify.com",
		},
		Entries: []EnrichedEntry{
			{
				Method:         "GET",
				URL:            "https://api.spotify.com/v1/me",
				RequestHeaders: map[string]string{"Content-Type": "application/json"},
			},
		},
	}

	apiSpec, err := AnalyzeCapture(capture)
	require.NoError(t, err)

	assert.Equal(t, "cookie", apiSpec.Auth.Type)
	assert.Equal(t, "Cookie", apiSpec.Auth.Header)
	assert.Equal(t, "cookie", apiSpec.Auth.In)
	assert.Equal(t, "spotify.com", apiSpec.Auth.CookieDomain)
	assert.Equal(t, []string{"SPOTIFY_COOKIES"}, apiSpec.Auth.EnvVars)
}

func TestAnalyzeCapture_ExpandsGraphQLBFFOperations(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://www.example.com",
		Entries: []EnrichedEntry{
			graphqlBFFEntry("PostsToday", `{"date":"2026-04-22"}`, "aaa111"),
			graphqlBFFEntry("ProductPageLaunches", `{"slug":"sample-product"}`, "bbb222"),
			graphqlBFFEntry("PostsToday", `{"date":"2026-04-23"}`, "aaa111"),
		},
	}

	apiSpec, err := AnalyzeCapture(capture)
	require.NoError(t, err)
	require.NotNil(t, apiSpec)

	require.NotContains(t, apiSpec.Resources, "graphql")
	posts := apiSpec.Resources["posts"]
	products := apiSpec.Resources["products"]
	require.NotNil(t, posts.Endpoints)
	require.NotNil(t, products.Endpoints)

	postsToday := posts.Endpoints["today"]
	assert.Equal(t, "POST", postsToday.Method)
	assert.Equal(t, "/frontend/graphql", postsToday.Path)
	assert.Equal(t, "Fetch posts today", postsToday.Description)
	assert.NotContains(t, postsToday.Description, "PostsToday")
	require.Len(t, postsToday.Body, 3)
	assert.Equal(t, "operationName", postsToday.Body[0].Name)
	assert.Equal(t, "PostsToday", postsToday.Body[0].Default)
	assert.Equal(t, "variables", postsToday.Body[1].Name)
	assert.Equal(t, "object", postsToday.Body[1].Type)
	assert.Equal(t, "extensions", postsToday.Body[2].Name)
	assert.Equal(t, "object", postsToday.Body[2].Type)

	extensions, ok := postsToday.Body[2].Default.(map[string]any)
	require.True(t, ok)
	persisted, ok := extensions["persistedQuery"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "aaa111", persisted["sha256Hash"])

	launches := products.Endpoints["launches"]
	assert.Equal(t, "POST", launches.Method)
	assert.Equal(t, "/frontend/graphql", launches.Path)
}

func TestAnalyzeCapture_ExpandsURLOnlyGraphQLBFFOperations(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://www.example.com",
		Entries: []EnrichedEntry{
			graphQLBFFGETEntry("PostsToday", "aaa111"),
			graphQLBFFGETEntry("ProductPageLaunches", "bbb222"),
		},
	}

	apiSpec, err := AnalyzeCapture(capture)
	require.NoError(t, err)
	require.NotNil(t, apiSpec)

	posts := apiSpec.Resources["posts"]
	products := apiSpec.Resources["products"]
	require.NotNil(t, posts.Endpoints)
	require.NotNil(t, products.Endpoints)
	assert.Contains(t, posts.Endpoints, "today")
	assert.Contains(t, products.Endpoints, "launches")
	assert.Equal(t, "GET", posts.Endpoints["today"].Method)
	assert.Empty(t, posts.Endpoints["today"].Body)
	assert.Equal(t, "operationName", posts.Endpoints["today"].Params[0].Name)
	assert.Equal(t, "variables", posts.Endpoints["today"].Params[1].Name)
	assert.Equal(t, "extensions", posts.Endpoints["today"].Params[2].Name)
}

func TestAnalyzeCapture_IncludesUsefulHTMLSurfaces(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "data:text/plain,bootstrap",
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://noise.example.net/promo",
				ResponseStatus:      200,
				ResponseContentType: "text/html; charset=utf-8",
				ResponseBody:        `<html><head><title>Noise</title></head><body><a href="/products/noise">Noise</a></body></html>`,
			},
			{
				Method:              "GET",
				URL:                 "https://www.example.com/",
				ResponseStatus:      200,
				ResponseContentType: "text/html; charset=utf-8",
				ResponseBody:        `<html><head><title>Products</title></head><body><a href="/products/speakon">1. SpeakON</a><a href="/products/instant-db">2. InstantDB</a></body></html>`,
			},
			{
				Method:              "GET",
				URL:                 "https://www.example.com/products/speakon",
				ResponseStatus:      200,
				ResponseContentType: "text/html; charset=utf-8",
				ResponseBody:        `<html><head><title>SpeakON</title><meta name="description" content="AI device"></head><body><h1>SpeakON</h1></body></html>`,
			},
			{
				Method:              "GET",
				URL:                 "https://www.example.com/challenge",
				ResponseStatus:      200,
				ResponseContentType: "text/html",
				ResponseBody:        `<html><title>Just a moment...</title><p>Cloudflare challenge</p></html>`,
			},
		},
	}

	apiSpec, err := AnalyzeCapture(capture)
	require.NoError(t, err)
	assert.Equal(t, "https://www.example.com", apiSpec.BaseURL)

	home := apiSpec.Resources["default"].Endpoints["list_endpoint"]
	assert.Equal(t, spec.ResponseFormatHTML, home.ResponseFormat)
	require.NotNil(t, home.HTMLExtract)
	assert.Equal(t, spec.HTMLExtractModeLinks, home.HTMLExtract.Mode)
	assert.Contains(t, home.HTMLExtract.LinkPrefixes, "/products")

	product := apiSpec.Resources["products"].Endpoints["get_products"]
	assert.Equal(t, "/products/{slug}", product.Path)
	assert.Equal(t, spec.ResponseFormatHTML, product.ResponseFormat)
	require.NotNil(t, product.HTMLExtract)
	assert.Equal(t, spec.HTMLExtractModePage, product.HTMLExtract.Mode)
	require.Len(t, product.Params, 1)
	assert.Equal(t, "slug", product.Params[0].Name)

	assert.NotContains(t, apiSpec.Resources, "challenge")
	assert.NotContains(t, apiSpec.Resources, "promo")
}

func TestAnalyzeCapture_IgnoresTelemetryWhenChoosingPrimaryAPIHost(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://app.example.com",
		Entries: []EnrichedEntry{
			{
				Method:              "POST",
				URL:                 "https://sentry.io/api/123/envelope/?sentry_key=wrong-service",
				RequestHeaders:      map[string]string{"Content-Type": "application/json"},
				RequestBody:         `{"event_id":"abc"}`,
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":"evt_1"}`,
			},
			{
				Method:              "POST",
				URL:                 "https://browser-intake-datadoghq.com/api/v2/rum?dd-api-key=wrong-service",
				RequestHeaders:      map[string]string{"Content-Type": "application/json"},
				RequestBody:         `[{"type":"view"}]`,
				ResponseStatus:      202,
				ResponseContentType: "application/json",
				ResponseBody:        `{"status":"ok"}`,
			},
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"items":[{"id":"item_1"}]}`,
			},
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items/item_1",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":"item_1"}`,
			},
			{
				Method:              "GET",
				URL:                 "https://partner.example.net/v1/profiles",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"profiles":[{"id":"profile_1"}]}`,
			},
			{
				Method:              "POST",
				URL:                 "https://api-iam.intercom.io/messenger/web/ping",
				RequestHeaders:      map[string]string{"Content-Type": "application/json"},
				RequestBody:         `{"app_id":"abc"}`,
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"ok":true}`,
			},
		},
	}

	apiSpec, err := AnalyzeCapture(capture)
	require.NoError(t, err)

	assert.Equal(t, "https://api.example.com", apiSpec.BaseURL)
	assert.Equal(t, "none", apiSpec.Auth.Type)
	assert.NotContains(t, apiSpec.Description, "sentry")
	assert.Contains(t, apiSpec.Resources, "items")
	assert.NotContains(t, apiSpec.Resources, "profiles")
	assert.NotContains(t, apiSpec.Resources, "api")
	assert.NotContains(t, apiSpec.Resources, "messenger")
}

func TestAnalyzeCapture_IncludeTelemetryBypassesPrimaryHostPruning(t *testing.T) {
	SetAdditionalIncludeList([]string{"sentry.io"})
	defer SetAdditionalIncludeList(nil)

	capture := &EnrichedCapture{
		TargetURL: "https://app.example.com",
		Entries: []EnrichedEntry{
			{
				Method:              "POST",
				URL:                 "https://sentry.io/api/123/envelope/?sentry_key=included",
				RequestHeaders:      map[string]string{"Content-Type": "application/json"},
				RequestBody:         `{"event_id":"abc"}`,
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":"evt_1"}`,
			},
			{
				Method:              "POST",
				URL:                 "https://sentry.io/api/124/envelope/?sentry_key=included",
				RequestHeaders:      map[string]string{"Content-Type": "application/json"},
				RequestBody:         `{"event_id":"def"}`,
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":"evt_2"}`,
			},
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"items":[{"id":"item_1"}]}`,
			},
		},
	}

	apiSpec, err := AnalyzeCapture(capture)
	require.NoError(t, err)

	assert.Contains(t, apiSpec.Resources, "envelope")
	assert.Contains(t, apiSpec.Resources, "items")
}

func TestAnalyzeCapture_PreservesPureMultiHostCaptureByDefault(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://app.example.com",
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"items":[{"id":"item_1"}]}`,
			},
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items/item_1",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":"item_1"}`,
			},
			{
				Method:              "GET",
				URL:                 "https://partner.example.net/v1/profiles",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"profiles":[{"id":"profile_1"}]}`,
			},
		},
	}

	apiSpec, err := AnalyzeCapture(capture)
	require.NoError(t, err)

	assert.Contains(t, apiSpec.Resources, "items")
	assert.Contains(t, apiSpec.Resources, "profiles")
}

func TestAnalyzeCaptureWithOptions_PreservesMultipleAPIHosts(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://app.example.com",
		Entries: []EnrichedEntry{
			{
				Method:              "POST",
				URL:                 "https://browser-intake-datadoghq.com/api/v2/rum?dd-api-key=wrong-service",
				RequestHeaders:      map[string]string{"Content-Type": "application/json"},
				RequestBody:         `[{"type":"view"}]`,
				ResponseStatus:      202,
				ResponseContentType: "application/json",
				ResponseBody:        `{"status":"ok"}`,
			},
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"items":[{"id":"item_1"}]}`,
			},
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items/item_1",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":"item_1"}`,
			},
			{
				Method:              "GET",
				URL:                 "https://partner.example.net/v1/profiles",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"profiles":[{"id":"profile_1"}]}`,
			},
		},
	}

	apiSpec, err := AnalyzeCaptureWithOptions(capture, AnalyzeOptions{PreserveHosts: true})
	require.NoError(t, err)

	assert.Equal(t, "https://api.example.com", apiSpec.BaseURL)
	assert.Contains(t, apiSpec.Resources, "items")
	require.Contains(t, apiSpec.Resources, "profiles")
	profiles := apiSpec.Resources["profiles"].Endpoints["list_profiles"]
	assert.Equal(t, "https://partner.example.net", profiles.BaseURL)
}

func TestFilterEndpointsByMinSamplesWithOptions_UsesPreservedHostGroups(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://app.example.com",
		Entries: []EnrichedEntry{
			{Method: "GET", URL: "https://api.example.com/v1/items", ResponseStatus: 200, ResponseContentType: "application/json", ResponseBody: `{"items":[]}`},
			{Method: "GET", URL: "https://api.example.com/v1/users", ResponseStatus: 200, ResponseContentType: "application/json", ResponseBody: `{"users":[]}`},
			{Method: "GET", URL: "https://api.example.com/v1/users", ResponseStatus: 200, ResponseContentType: "application/json", ResponseBody: `{"users":[{"id":"u1"}]}`},
			{Method: "GET", URL: "https://partner.example.net/v1/items", ResponseStatus: 200, ResponseContentType: "application/json", ResponseBody: `{"items":[]}`},
			{Method: "GET", URL: "https://partner.example.net/v1/items", ResponseStatus: 200, ResponseContentType: "application/json", ResponseBody: `{"items":[{"id":"p1"}]}`},
		},
	}
	options := AnalyzeOptions{PreserveHosts: true}
	apiSpec, err := AnalyzeCaptureWithOptions(capture, options)
	require.NoError(t, err)

	dropped := FilterEndpointsByMinSamplesWithOptions(apiSpec, capture, 2, options)

	assert.Equal(t, 1, dropped)
	require.Contains(t, apiSpec.Resources, "items")
	require.Len(t, apiSpec.Resources["items"].Endpoints, 1)
	for _, endpoint := range apiSpec.Resources["items"].Endpoints {
		assert.Equal(t, "https://partner.example.net", endpoint.BaseURL)
	}
}

func TestGraphQLBFFCommandPathUsesSemanticResources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		operation string
		resource  string
		endpoint  string
	}{
		{operation: "ProductPageLaunches", resource: "products", endpoint: "launches"},
		{operation: "CategoryPageQuery", resource: "categories", endpoint: "get"},
		{operation: "HeaderDesktopProductsNavigationQuery", resource: "site", endpoint: "navigation"},
		{operation: "FooterLinksQuery", resource: "site", endpoint: "links"},
		{operation: "DetailedReviewsFeedQuery", resource: "reviews", endpoint: "feed"},
		{operation: "GetProductDetails", resource: "products", endpoint: "get"},
		{operation: "ListProducts", resource: "products", endpoint: "get"},
		{operation: "SearchMakersByName", resource: "makers", endpoint: "by_name"},
	}

	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			t.Parallel()
			resource, endpoint := graphQLBFFCommandPath(tt.operation)
			assert.Equal(t, tt.resource, resource)
			assert.Equal(t, tt.endpoint, endpoint)
		})
	}
}

func graphqlBFFEntry(operationName, variablesJSON, hash string) EnrichedEntry {
	return EnrichedEntry{
		Method:              "POST",
		URL:                 "https://www.example.com/frontend/graphql",
		RequestHeaders:      map[string]string{"Content-Type": "application/json"},
		RequestBody:         `{"operationName":"` + operationName + `","variables":` + variablesJSON + `,"extensions":{"persistedQuery":{"version":1,"sha256Hash":"` + hash + `"}}}`,
		ResponseStatus:      200,
		ResponseContentType: "application/json",
		ResponseBody:        `{"data":{"node":{"id":"1"}}}`,
	}
}

func graphQLBFFGETEntry(operationName, hash string) EnrichedEntry {
	return EnrichedEntry{
		Method: "GET",
		URL: "https://www.example.com/frontend/graphql?operationName=" + operationName +
			`&variables=%7B%7D&extensions=%7B%22persistedQuery%22%3A%7B%22version%22%3A1%2C%22sha256Hash%22%3A%22` + hash + `%22%7D%7D`,
		ResponseStatus:      200,
		ResponseContentType: "application/json",
		ResponseBody:        `{"data":{"node":{"id":"1"}}}`,
	}
}

func TestDetectAuth_PrefersCapturedAuthOverHeaders(t *testing.T) {
	t.Parallel()

	auth := detectAuth(&EnrichedCapture{
		Auth: &AuthCapture{
			Headers: map[string]string{"X-API-Key": "key-123"},
			Type:    "api_key",
		},
	}, []EnrichedEntry{
		{
			RequestHeaders: map[string]string{"Authorization": "Bearer tok123"},
		},
	}, "spotify")

	assert.Equal(t, "api_key", auth.Type)
	assert.Equal(t, "X-API-Key", auth.Header)
	assert.Equal(t, "header", auth.In)
}

func TestDetectAuth_FallsBackToHeaderInference(t *testing.T) {
	t.Parallel()

	auth := detectAuth(nil, []EnrichedEntry{
		{
			RequestHeaders: map[string]string{"Authorization": "Bearer tok123"},
		},
	}, "spotify")

	assert.Equal(t, "bearer_token", auth.Type)
	assert.Equal(t, "Authorization", auth.Header)
}

func TestObservedAuthHeaders(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		entries []EnrichedEntry
		want    []string
	}{
		{
			name: "single authorization header",
			entries: []EnrichedEntry{
				{RequestHeaders: map[string]string{"Authorization": "Bearer eyJabc"}},
			},
			want: []string{"authorization"},
		},
		{
			name: "multiple distinct auth headers in one sample",
			entries: []EnrichedEntry{
				{RequestHeaders: map[string]string{
					"Authorization": "Bearer t",
					"X-CSRF-Token":  "abc",
					"Cookie":        "session=x",
				}},
			},
			want: []string{"authorization", "cookie", "x-csrf-token"},
		},
		{
			name: "mixed across samples - union, presence not requirement",
			entries: []EnrichedEntry{
				{RequestHeaders: map[string]string{"Authorization": "Bearer t"}},
				{RequestHeaders: map[string]string{"Accept": "application/json"}},
			},
			want: []string{"authorization"},
		},
		{
			name: "no auth headers - nil result for omitempty",
			entries: []EnrichedEntry{
				{RequestHeaders: map[string]string{"Accept": "application/json", "User-Agent": "x"}},
			},
			want: nil,
		},
		{
			name: "case-insensitive merge",
			entries: []EnrichedEntry{
				{RequestHeaders: map[string]string{"Authorization": "x"}},
				{RequestHeaders: map[string]string{"AUTHORIZATION": "y"}},
				{RequestHeaders: map[string]string{"authorization": "z"}},
			},
			want: []string{"authorization"},
		},
		{
			name: "values never leak - only names emitted",
			entries: []EnrichedEntry{
				{RequestHeaders: map[string]string{"Authorization": "Bearer eyJ.SECRET.token"}},
			},
			want: []string{"authorization"},
		},
		{
			name: "api-key and api_key variants",
			entries: []EnrichedEntry{
				{RequestHeaders: map[string]string{"X-Api-Key": "k1"}},
				{RequestHeaders: map[string]string{"x_api_key": "k2"}},
			},
			want: []string{"x-api-key", "x_api_key"},
		},
		{
			name: "contains-token contains-secret contains-signature patterns",
			entries: []EnrichedEntry{
				{RequestHeaders: map[string]string{
					"X-Auth-Token":    "t",
					"X-Hub-Secret":    "s",
					"X-Sig-Signature": "g",
				}},
			},
			want: []string{"x-auth-token", "x-hub-secret", "x-sig-signature"},
		},
		{
			name:    "empty entries",
			entries: []EnrichedEntry{},
			want:    nil,
		},
		{
			name: "non-auth headers ignored",
			entries: []EnrichedEntry{
				{RequestHeaders: map[string]string{
					"Accept":     "application/json",
					"User-Agent": "ua/1.0",
					"Referer":    "https://example.com",
				}},
			},
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := observedAuthHeaders(tc.entries)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestAnalyzeCapture_PopulatesObservedAuthOnSpecEndpoint(t *testing.T) {
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
					"Accept":        "application/json",
				},
			},
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":2}`,
				RequestHeaders: map[string]string{
					"Accept": "application/json",
				},
			},
		},
	}

	apiSpec, err := AnalyzeCapture(capture)
	require.NoError(t, err)

	var found bool
	for _, resource := range apiSpec.Resources {
		for _, endpoint := range resource.Endpoints {
			if endpoint.Method == "GET" && endpoint.Path == "/v1/items" {
				assert.Equal(t, []string{"authorization"}, endpoint.ObservedAuth)
				found = true
			}
		}
	}
	assert.True(t, found, "expected GET /v1/items endpoint")
}

func TestAnalyzeCapture_OmitsObservedAuthWhenAbsent(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://api.example.com",
		Entries: []EnrichedEntry{
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

	apiSpec, err := AnalyzeCapture(capture)
	require.NoError(t, err)

	for _, resource := range apiSpec.Resources {
		for _, endpoint := range resource.Endpoints {
			assert.Nil(t, endpoint.ObservedAuth, "endpoint %s %s should have nil ObservedAuth", endpoint.Method, endpoint.Path)
		}
	}
}

func TestAnalyzeCapture_PopulatesObservedAuthOnGraphQLEndpoint(t *testing.T) {
	t.Parallel()

	postsEntry := graphqlBFFEntry("PostsToday", `{"date":"2026-04-22"}`, "aaa111")
	postsEntry.RequestHeaders["Authorization"] = "Bearer eyJtoken"
	launchesEntry := graphqlBFFEntry("ProductPageLaunches", `{"slug":"sample-product"}`, "bbb222")

	capture := &EnrichedCapture{
		TargetURL: "https://www.example.com",
		Entries:   []EnrichedEntry{postsEntry, launchesEntry, postsEntry},
	}

	apiSpec, err := AnalyzeCapture(capture)
	require.NoError(t, err)

	posts, ok := apiSpec.Resources["posts"]
	require.True(t, ok, "expected posts resource from PostsToday operation")
	postsToday, ok := posts.Endpoints["today"]
	require.True(t, ok, "expected today endpoint from PostsToday operation")
	assert.Equal(t, []string{"authorization"}, postsToday.ObservedAuth)

	products, ok := apiSpec.Resources["products"]
	require.True(t, ok, "expected products resource from ProductPageLaunches operation")
	launches, ok := products.Endpoints["launches"]
	require.True(t, ok, "expected launches endpoint")
	assert.Nil(t, launches.ObservedAuth, "launches operation had no auth headers")
}

func TestSampleFilename_DeterministicAcrossReruns(t *testing.T) {
	t.Parallel()

	group := EndpointGroup{Method: "GET", NormalizedPath: "/v1/items/{id}"}
	first := sampleFilename(group)
	second := sampleFilename(group)
	assert.Equal(t, first, second, "filename hash must be deterministic")
	assert.Regexp(t, `^get__v1_items_id__[0-9a-f]{8}\.json$`, first)
}

func TestSampleFilename_MethodVariesHash(t *testing.T) {
	t.Parallel()

	getName := sampleFilename(EndpointGroup{Method: "GET", NormalizedPath: "/v1/items"})
	postName := sampleFilename(EndpointGroup{Method: "POST", NormalizedPath: "/v1/items"})
	assert.NotEqual(t, getName, postName, "method change must change the filename")
}

func TestSampleFilename_StripsBraces(t *testing.T) {
	t.Parallel()

	got := sampleFilename(EndpointGroup{Method: "POST", NormalizedPath: "/v1/orders/{orderId}/items/{itemId}"})
	assert.NotContains(t, got, "{")
	assert.NotContains(t, got, "}")
	assert.Contains(t, got, "orders_orderId_items_itemId")
}

func TestSelectSampleEntry_PrefersMostRecentSuccess(t *testing.T) {
	t.Parallel()

	entries := []EnrichedEntry{
		{URL: "/a", ResponseStatus: 200, ResponseBody: `{"first":true}`},
		{URL: "/a", ResponseStatus: 200, ResponseBody: `{"second":true}`},
		{URL: "/a", ResponseStatus: 500, ResponseBody: `{"oops":true}`},
		{URL: "/a", ResponseStatus: 200, ResponseBody: `{"third":true}`},
		{URL: "/a", ResponseStatus: 404, ResponseBody: `{"notfound":true}`},
	}
	got := selectSampleEntry(entries)
	assert.Equal(t, `{"third":true}`, got.ResponseBody, "most-recent 2xx should win over later 4xx/5xx")
}

func TestSelectSampleEntry_FallsBackToNonError(t *testing.T) {
	t.Parallel()

	entries := []EnrichedEntry{
		{URL: "/a", ResponseStatus: 500, ResponseBody: `{"oops":true}`},
		{URL: "/a", ResponseStatus: 404, ResponseBody: `{"notfound":true}`},
		{URL: "/a", ResponseStatus: 502, ResponseBody: `{"badgw":true}`},
	}
	got := selectSampleEntry(entries)
	assert.Equal(t, `{"notfound":true}`, got.ResponseBody, "404 should be the most-recent non-5xx")
}

func TestSelectSampleEntry_FallsBackToMostRecentWhenAllErrors(t *testing.T) {
	t.Parallel()

	entries := []EnrichedEntry{
		{URL: "/a", ResponseStatus: 500, ResponseBody: `{"first":true}`},
		{URL: "/a", ResponseStatus: 502, ResponseBody: `{"second":true}`},
	}
	got := selectSampleEntry(entries)
	assert.Equal(t, `{"second":true}`, got.ResponseBody)
}

func TestWriteSamples_WritesOneFilePerEndpointGroup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	capture := &EnrichedCapture{
		TargetURL: "https://api.example.com",
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items/42",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"id":42,"api_key":"sk_secret"}`,
				RequestHeaders: map[string]string{
					"Authorization": "Bearer eyJtoken",
					"Accept":        "application/json",
				},
			},
			{
				Method:              "POST",
				URL:                 "https://api.example.com/v1/items",
				ResponseStatus:      201,
				ResponseContentType: "application/json",
				RequestBody:         `{"name":"widget","password":"p"}`,
				ResponseBody:        `{"id":43}`,
				RequestHeaders:      map[string]string{"Content-Type": "application/json"},
			},
		},
	}

	written, err := WriteSamples(capture, dir)
	require.NoError(t, err)
	assert.Equal(t, 2, written)

	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 2)

	var foundGET, foundPOST bool
	for _, f := range files {
		assert.Regexp(t, `^[a-z]+__[a-zA-Z0-9_]+__[0-9a-f]{8}\.json$`, f.Name())
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		require.NoError(t, err)

		var sample SampleFile
		require.NoError(t, json.Unmarshal(data, &sample))

		switch sample.Method {
		case "GET":
			foundGET = true
			assert.Equal(t, "GET /v1/items/{item_id}", sample.Endpoint)
			assert.Equal(t, 200, sample.Status)
			assert.True(t, sample.ResponseBodyKnown)
			assert.Equal(t, RedactedSentinel, sample.RequestHeaders["Authorization"])
			assert.Equal(t, "application/json", sample.RequestHeaders["Accept"])
			body, ok := sample.ResponseBody.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, RedactedSentinel, body["api_key"])
			assert.Contains(t, sample.Redactions, "request_headers.authorization")
			assert.Contains(t, sample.Redactions, "response_body.api_key")
		case "POST":
			foundPOST = true
			assert.Equal(t, 201, sample.Status)
			reqBody, ok := sample.RequestBody.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, RedactedSentinel, reqBody["password"])
			assert.Contains(t, sample.Redactions, "request_body.password")
		}
	}
	assert.True(t, foundGET, "GET sample should be present")
	assert.True(t, foundPOST, "POST sample should be present")
}

func TestWriteSamplesWithOptions_PreservesSamePathAcrossHosts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	capture := &EnrichedCapture{
		TargetURL: "https://api.example.com",
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/items",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"source":"primary"}`,
			},
			{
				Method:              "GET",
				URL:                 "https://partner.example.net/v1/items",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"source":"partner"}`,
			},
		},
	}

	written, err := WriteSamplesWithOptions(capture, dir, AnalyzeOptions{PreserveHosts: true})
	require.NoError(t, err)
	assert.Equal(t, 2, written)

	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 2)
}

func TestWriteSamples_OmitsResponseBodyKnownWhenAbsent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	capture := &EnrichedCapture{
		TargetURL: "https://api.example.com",
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/no-body",
				ResponseStatus:      204,
				ResponseContentType: "application/json",
				ResponseBody:        "",
				RequestHeaders:      map[string]string{"Accept": "application/json"},
			},
		},
	}

	written, err := WriteSamples(capture, dir)
	require.NoError(t, err)
	require.Equal(t, 1, written)

	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)

	data, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	require.NoError(t, err)

	var sample SampleFile
	require.NoError(t, json.Unmarshal(data, &sample))
	assert.False(t, sample.ResponseBodyKnown)
	assert.Nil(t, sample.ResponseBody)
}

func TestWriteSamples_TruncatesOversizedBodies(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Build an oversized JSON-shaped body so the classifier keeps it as an
	// API endpoint. HTML-shaped surfaces are discovered separately by
	// AnalyzeCapture and are not yet covered by WriteSamples; that is
	// follow-up work (see plan deferred section).
	bigValue := strings.Repeat("x", sampleBodyMaxBytes+1000)
	bigBody := `{"blob":"` + bigValue + `"}`
	capture := &EnrichedCapture{
		TargetURL: "https://api.example.com",
		Entries: []EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/v1/blob",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        bigBody,
				RequestHeaders:      map[string]string{"Accept": "application/json"},
			},
		},
	}

	written, err := WriteSamples(capture, dir)
	require.NoError(t, err)
	require.Equal(t, 1, written)

	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)

	data, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	require.NoError(t, err)

	var sample SampleFile
	require.NoError(t, json.Unmarshal(data, &sample))
	assert.True(t, sample.ResponseBodyTruncated, "oversized body should set truncated flag")
	// After truncation the body is no longer parseable JSON, so it lands as
	// a raw string in the sample.
	body, ok := sample.ResponseBody.(string)
	require.True(t, ok, "truncated body falls back to raw string")
	assert.LessOrEqual(t, len(body), sampleBodyMaxBytes)
}

func TestDefaultSamplesPath(t *testing.T) {
	t.Parallel()

	got := DefaultSamplesPath("/tmp/cache/example-spec.yaml")
	assert.Equal(t, "/tmp/cache/example-spec-samples", got)

	got2 := DefaultSamplesPath("api.yml")
	assert.Equal(t, "api-samples", got2)
}

func TestWriteEnrichedCaptureUsesPrivatePermissions(t *testing.T) {
	t.Parallel()

	outputPath := filepath.Join(t.TempDir(), "capture.json")
	err := WriteEnrichedCapture(&EnrichedCapture{
		TargetURL: "https://api.spotify.com",
		Auth: &AuthCapture{
			Headers: map[string]string{"Authorization": "Bearer tok123"},
			Type:    "bearer",
		},
	}, outputPath)
	require.NoError(t, err)

	info, err := os.Stat(outputPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
