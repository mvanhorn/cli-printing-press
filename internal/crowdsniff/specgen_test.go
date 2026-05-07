package crowdsniff

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/discovery"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSpec(t *testing.T) {
	t.Parallel()

	t.Run("valid spec from aggregated endpoints", func(t *testing.T) {
		t.Parallel()
		endpoints := []AggregatedEndpoint{
			{Method: "GET", Path: "/v1/users", SourceTier: TierOfficialSDK, SourceCount: 2},
			{Method: "GET", Path: "/v1/users/{id}", SourceTier: TierOfficialSDK, SourceCount: 1},
			{Method: "POST", Path: "/v1/users", SourceTier: TierCommunitySDK, SourceCount: 1},
		}

		apiSpec, err := BuildSpec("notion", "https://api.notion.com", endpoints, nil)
		require.NoError(t, err)

		assert.Equal(t, "notion", apiSpec.Name)
		assert.Equal(t, "https://api.notion.com", apiSpec.BaseURL)
		assert.NotEmpty(t, apiSpec.Resources)

		// Check that the "users" resource was created.
		users, ok := apiSpec.Resources["users"]
		require.True(t, ok)
		assert.Len(t, users.Endpoints, 3)
	})

	t.Run("meta contains source_tier and source_count", func(t *testing.T) {
		t.Parallel()
		endpoints := []AggregatedEndpoint{
			{Method: "GET", Path: "/v1/users", SourceTier: TierOfficialSDK, SourceCount: 3},
		}

		apiSpec, err := BuildSpec("test", "https://api.example.com", endpoints, nil)
		require.NoError(t, err)

		for _, resource := range apiSpec.Resources {
			for _, ep := range resource.Endpoints {
				assert.Equal(t, "official-sdk", ep.Meta["source_tier"])
				assert.Equal(t, "3", ep.Meta["source_count"])
			}
		}
	})

	t.Run("resource grouping from paths", func(t *testing.T) {
		t.Parallel()
		endpoints := []AggregatedEndpoint{
			{Method: "GET", Path: "/v1/users", SourceTier: TierCodeSearch, SourceCount: 1},
			{Method: "GET", Path: "/v1/projects", SourceTier: TierCodeSearch, SourceCount: 1},
		}

		apiSpec, err := BuildSpec("test", "https://api.example.com", endpoints, nil)
		require.NoError(t, err)

		_, hasUsers := apiSpec.Resources["users"]
		_, hasProjects := apiSpec.Resources["projects"]
		assert.True(t, hasUsers, "should have users resource")
		assert.True(t, hasProjects, "should have projects resource")
	})

	t.Run("empty endpoints returns error", func(t *testing.T) {
		t.Parallel()
		_, err := BuildSpec("test", "https://api.example.com", nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints")
	})

	t.Run("empty name returns error", func(t *testing.T) {
		t.Parallel()
		_, err := BuildSpec("", "https://api.example.com", []AggregatedEndpoint{
			{Method: "GET", Path: "/users", SourceTier: TierCodeSearch, SourceCount: 1},
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("empty base_url returns error", func(t *testing.T) {
		t.Parallel()
		_, err := BuildSpec("test", "", []AggregatedEndpoint{
			{Method: "GET", Path: "/users", SourceTier: TierCodeSearch, SourceCount: 1},
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "base_url is required")
	})

	t.Run("spec passes validation", func(t *testing.T) {
		t.Parallel()
		endpoints := []AggregatedEndpoint{
			{Method: "GET", Path: "/users", SourceTier: TierCodeSearch, SourceCount: 1},
		}

		apiSpec, err := BuildSpec("test", "https://api.example.com", endpoints, nil)
		require.NoError(t, err)
		assert.NoError(t, apiSpec.Validate())
	})
}

func TestBuildSpec_KeyURL(t *testing.T) {
	t.Parallel()

	endpoints := []AggregatedEndpoint{
		{Method: "GET", Path: "/v1/users", SourceTier: TierOfficialSDK, SourceCount: 1},
	}

	t.Run("KeyURLHint propagates to spec.Auth.KeyURL", func(t *testing.T) {
		t.Parallel()

		auth := &DiscoveredAuth{
			Type:       "api_key",
			Header:     "X-Api-Key",
			In:         "header",
			EnvVarHint: "EXAMPLE_API_KEY",
			KeyURLHint: "https://example.com/developers/keys",
			SourceTier: TierOfficialSDK,
		}

		apiSpec, err := BuildSpec("example", "https://api.example.com", endpoints, auth)
		require.NoError(t, err)

		assert.Equal(t, "https://example.com/developers/keys", apiSpec.Auth.KeyURL)
	})

	t.Run("empty KeyURLHint leaves spec.Auth.KeyURL empty", func(t *testing.T) {
		t.Parallel()

		auth := &DiscoveredAuth{
			Type:       "bearer_token",
			Header:     "Authorization",
			In:         "header",
			Format:     "Bearer {token}",
			SourceTier: TierOfficialSDK,
		}

		apiSpec, err := BuildSpec("example", "https://api.example.com", endpoints, auth)
		require.NoError(t, err)

		assert.Empty(t, apiSpec.Auth.KeyURL)
	})

	t.Run("nil auth leaves spec.Auth.KeyURL empty", func(t *testing.T) {
		t.Parallel()

		apiSpec, err := BuildSpec("example", "https://api.example.com", endpoints, nil)
		require.NoError(t, err)

		assert.Empty(t, apiSpec.Auth.KeyURL)
	})
}

func TestAuthConfig_KeyURL_RoundTrip(t *testing.T) {
	t.Parallel()

	// A spec without key_url should still parse correctly (backward compat).
	yamlWithout := `
name: test
base_url: https://api.example.com
auth:
  type: api_key
  header: X-Api-Key
  format: "{api_key}"
  env_vars:
    - TEST_API_KEY
resources:
  users:
    description: User operations
    endpoints:
      list:
        method: GET
        path: /users
        description: List users
`
	specWithout, err := spec.ParseBytes([]byte(yamlWithout))
	require.NoError(t, err)
	assert.Empty(t, specWithout.Auth.KeyURL)

	// A spec with key_url should round-trip correctly.
	yamlWith := `
name: test
base_url: https://api.example.com
auth:
  type: api_key
  header: X-Api-Key
  format: "{api_key}"
  env_vars:
    - TEST_API_KEY
  key_url: https://example.com/keys
resources:
  users:
    description: User operations
    endpoints:
      list:
        method: GET
        path: /users
        description: List users
`
	specWith, err := spec.ParseBytes([]byte(yamlWith))
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/keys", specWith.Auth.KeyURL)
}

func TestResolveBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		explicit   string
		candidates []string
		want       string
	}{
		{
			name:       "explicit wins",
			explicit:   "https://explicit.com",
			candidates: []string{"https://candidate.com"},
			want:       "https://explicit.com",
		},
		{
			name:       "first candidate when no explicit",
			explicit:   "",
			candidates: []string{"https://first.com", "https://second.com"},
			want:       "https://first.com",
		},
		{
			name:       "skip empty candidates",
			explicit:   "",
			candidates: []string{"", " ", "https://valid.com"},
			want:       "https://valid.com",
		},
		{
			name:       "empty when nothing available",
			explicit:   "",
			candidates: nil,
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ResolveBaseURL(tt.explicit, tt.candidates))
		})
	}
}

func TestDeriveResourceKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		wantResource string
		wantName     string
	}{
		{
			name:         "Steam-like path uses first segment as resource",
			path:         "/ISteamUser/GetPlayerSummaries/v2",
			wantResource: "ISteamUser",
			wantName:     "GetPlayerSummaries",
		},
		{
			name:         "REST nested path uses first segment only",
			path:         "/v1/users/{id}/posts",
			wantResource: "users",
			wantName:     "posts",
		},
		{
			name:         "simple path unchanged",
			path:         "/v1/users",
			wantResource: "users",
			wantName:     "users",
		},
		{
			name:         "only params returns empty",
			path:         "/v1/{id}",
			wantResource: "",
			wantName:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resource, name := discovery.ResourceKey(tt.path)
			assert.Equal(t, tt.wantResource, resource)
			assert.Equal(t, tt.wantName, name)
		})
	}
}

func TestDeriveResourceKey_NoSlashes(t *testing.T) {
	t.Parallel()

	// Resource keys must never contain slashes — they are used as filenames
	// and Cobra command Use fields.
	paths := []string{
		"/v1/users",
		"/v1/users/{id}/posts",
		"/v1/users/{id}/posts/{postId}/comments",
		"/api/v2/organizations/{orgId}/members",
		"/ISteamUser/GetPlayerSummaries/v2",
	}

	for _, p := range paths {
		resource, _ := discovery.ResourceKey(p)
		assert.NotContains(t, resource, "/", "resource key for %q must not contain slashes", p)
	}
}

func TestDeriveResourceKey_SharedFirstSegment(t *testing.T) {
	t.Parallel()

	// Two paths sharing the same first significant segment must map to the
	// same resource key so they end up in one file.
	res1, _ := discovery.ResourceKey("/v1/users")
	res2, _ := discovery.ResourceKey("/v1/users/{id}/posts")
	assert.Equal(t, res1, res2, "paths sharing first segment should have same resource key")
	assert.Equal(t, "users", res1)
}

func TestBuildSpec_ParamMapping(t *testing.T) {
	t.Parallel()

	t.Run("aggregated endpoints with params produce spec.Endpoint.Params", func(t *testing.T) {
		t.Parallel()

		endpoints := []AggregatedEndpoint{
			{
				Method:      "GET",
				Path:        "/v1/games",
				SourceTier:  TierOfficialSDK,
				SourceCount: 1,
				Params: []DiscoveredParam{
					{Name: "steamid", Type: "string", Required: true},
					{Name: "include_appinfo", Type: "boolean", Required: false, Default: "true"},
				},
			},
		}

		apiSpec, err := BuildSpec("steam", "https://api.steampowered.com", endpoints, nil)
		require.NoError(t, err)

		// Find the endpoint in the spec.
		var found *spec.Endpoint
		for _, resource := range apiSpec.Resources {
			for _, ep := range resource.Endpoints {
				if ep.Path == "/v1/games" {
					e := ep
					found = &e
					break
				}
			}
		}
		require.NotNil(t, found, "expected to find endpoint with path /v1/games")
		require.Len(t, found.Params, 2)

		assert.Equal(t, "steamid", found.Params[0].Name)
		assert.Equal(t, "string", found.Params[0].Type)
		assert.True(t, found.Params[0].Required)
		assert.False(t, found.Params[0].Positional)
		assert.Equal(t, "", found.Params[0].Description)

		assert.Equal(t, "include_appinfo", found.Params[1].Name)
		assert.Equal(t, "boolean", found.Params[1].Type)
		assert.False(t, found.Params[1].Required)
		assert.Equal(t, "true", found.Params[1].Default)
	})

	t.Run("param type mapping preserves discovered type", func(t *testing.T) {
		t.Parallel()

		endpoints := []AggregatedEndpoint{
			{
				Method:      "GET",
				Path:        "/v1/items",
				SourceTier:  TierCommunitySDK,
				SourceCount: 1,
				Params: []DiscoveredParam{
					{Name: "count", Type: "integer", Required: false, Default: "10"},
					{Name: "active", Type: "boolean", Required: false},
					{Name: "query", Type: "string", Required: false},
				},
			},
		}

		apiSpec, err := BuildSpec("test", "https://api.example.com", endpoints, nil)
		require.NoError(t, err)

		var found *spec.Endpoint
		for _, resource := range apiSpec.Resources {
			for _, ep := range resource.Endpoints {
				if ep.Path == "/v1/items" {
					e := ep
					found = &e
					break
				}
			}
		}
		require.NotNil(t, found)
		require.Len(t, found.Params, 3)

		// Verify types are preserved from DiscoveredParam.
		paramsByName := make(map[string]spec.Param)
		for _, p := range found.Params {
			paramsByName[p.Name] = p
		}

		assert.Equal(t, "integer", paramsByName["count"].Type)
		assert.Equal(t, "10", paramsByName["count"].Default)
		assert.Equal(t, "boolean", paramsByName["active"].Type)
		assert.Equal(t, "string", paramsByName["query"].Type)
	})

	t.Run("nil params on aggregated endpoint produces nil spec params", func(t *testing.T) {
		t.Parallel()

		endpoints := []AggregatedEndpoint{
			{
				Method:      "GET",
				Path:        "/v1/users",
				SourceTier:  TierCodeSearch,
				SourceCount: 1,
				Params:      nil,
			},
		}

		apiSpec, err := BuildSpec("test", "https://api.example.com", endpoints, nil)
		require.NoError(t, err)

		for _, resource := range apiSpec.Resources {
			for _, ep := range resource.Endpoints {
				if ep.Path == "/v1/users" {
					assert.Nil(t, ep.Params, "expected nil params when AggregatedEndpoint has nil params")
				}
			}
		}
	})

	t.Run("mix of endpoints with and without params", func(t *testing.T) {
		t.Parallel()

		endpoints := []AggregatedEndpoint{
			{
				Method:      "GET",
				Path:        "/v1/games",
				SourceTier:  TierOfficialSDK,
				SourceCount: 1,
				Params: []DiscoveredParam{
					{Name: "steamid", Type: "string", Required: true},
				},
			},
			{
				Method:      "GET",
				Path:        "/v1/users",
				SourceTier:  TierOfficialSDK,
				SourceCount: 1,
				Params:      nil, // no params
			},
		}

		apiSpec, err := BuildSpec("test", "https://api.example.com", endpoints, nil)
		require.NoError(t, err)

		for _, resource := range apiSpec.Resources {
			for _, ep := range resource.Endpoints {
				if ep.Path == "/v1/games" {
					require.Len(t, ep.Params, 1)
					assert.Equal(t, "steamid", ep.Params[0].Name)
				}
				// Endpoint without params will have nil Params, which
				// serializes as `params: []` due to YAML tag lacking omitempty.
			}
		}
	})
}
