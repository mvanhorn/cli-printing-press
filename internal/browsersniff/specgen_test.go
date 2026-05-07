package browsersniff

import (
	"os"
	"path/filepath"
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
