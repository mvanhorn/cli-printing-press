package crowdsniff

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGrepEndpoints(t *testing.T) {
	t.Parallel()

	t.Run("class method style", func(t *testing.T) {
		t.Parallel()
		content := `
class NotionClient {
  async listUsers() {
    return this.get("/v1/users");
  }
  async createPage(data) {
    return this.post("/v1/pages", data);
  }
  async updateBlock(blockId) {
    return this.patch("/v1/blocks/" + blockId);
  }
}
`
		endpoints, _ := GrepEndpoints(content, "test-sdk", TierCommunitySDK)

		methods := make(map[string][]string)
		for _, ep := range endpoints {
			methods[ep.Method] = append(methods[ep.Method], ep.Path)
		}

		assert.Contains(t, methods["GET"], "/v1/users")
		assert.Contains(t, methods["POST"], "/v1/pages")
		assert.Contains(t, methods["PATCH"], "/v1/blocks")
	})

	t.Run("fetch wrapper", func(t *testing.T) {
		t.Parallel()
		content := `
async function getUsers() {
  return fetch("/api/users");
}
async function deleteUser(id) {
  return fetch("/api/users/" + id, {method: "DELETE"});
}
`
		endpoints, _ := GrepEndpoints(content, "fetch-sdk", TierCommunitySDK)

		var paths []string
		for _, ep := range endpoints {
			paths = append(paths, ep.Path)
		}

		assert.Contains(t, paths, "/api/users")
	})

	t.Run("axios instance", func(t *testing.T) {
		t.Parallel()
		content := `
const client = axios.create({ baseURL: "https://api.stripe.com" });
const users = axios.get("/v1/customers");
const charge = axios.post("/v1/charges");
`
		endpoints, baseURLs := GrepEndpoints(content, "axios-sdk", TierCommunitySDK)

		methods := make(map[string][]string)
		for _, ep := range endpoints {
			methods[ep.Method] = append(methods[ep.Method], ep.Path)
		}

		assert.Contains(t, methods["GET"], "/v1/customers")
		assert.Contains(t, methods["POST"], "/v1/charges")
		assert.Contains(t, baseURLs, "https://api.stripe.com")
	})

	t.Run("base URL extraction", func(t *testing.T) {
		t.Parallel()
		content := `
const BASE_URL = "https://api.notion.com";
this.baseUrl = "https://api.example.com/v2";
const apiBase = "https://api.github.com";
`
		_, baseURLs := GrepEndpoints(content, "sdk", TierCommunitySDK)

		assert.Contains(t, baseURLs, "https://api.notion.com")
		assert.Contains(t, baseURLs, "https://api.example.com/v2")
		assert.Contains(t, baseURLs, "https://api.github.com")
	})

	t.Run("template literal paths", func(t *testing.T) {
		t.Parallel()
		content := "return this.get(`/v1/users/${userId}`);\n"

		endpoints, _ := GrepEndpoints(content, "sdk", TierCommunitySDK)

		var paths []string
		for _, ep := range endpoints {
			paths = append(paths, ep.Path)
		}

		assert.Contains(t, paths, "/v1/users/{id}")
	})

	t.Run("request with method literal", func(t *testing.T) {
		t.Parallel()
		content := `api.request({method: "PUT", url: "/v1/settings"});`

		endpoints, _ := GrepEndpoints(content, "sdk", TierCommunitySDK)

		found := false
		for _, ep := range endpoints {
			if ep.Method == "PUT" && ep.Path == "/v1/settings" {
				found = true
			}
		}
		assert.True(t, found, "expected PUT /v1/settings")
	})

	t.Run("skips comments", func(t *testing.T) {
		t.Parallel()
		content := `
// this.get("/v1/should-skip")
* this.get("/v1/also-skip")
this.get("/v1/real-endpoint");
`
		endpoints, _ := GrepEndpoints(content, "sdk", TierCommunitySDK)

		var paths []string
		for _, ep := range endpoints {
			paths = append(paths, ep.Path)
		}

		assert.Contains(t, paths, "/v1/real-endpoint")
		assert.NotContains(t, paths, "/v1/should-skip")
		assert.NotContains(t, paths, "/v1/also-skip")
	})

	t.Run("rejects file paths", func(t *testing.T) {
		t.Parallel()
		content := `
this.get("/assets/logo.png");
this.get("/styles/main.css");
this.get("/v1/users");
`
		endpoints, _ := GrepEndpoints(content, "sdk", TierCommunitySDK)

		var paths []string
		for _, ep := range endpoints {
			paths = append(paths, ep.Path)
		}

		assert.Contains(t, paths, "/v1/users")
		assert.NotContains(t, paths, "/assets/logo.png")
		assert.NotContains(t, paths, "/styles/main.css")
	})

	t.Run("empty content returns empty", func(t *testing.T) {
		t.Parallel()
		endpoints, baseURLs := GrepEndpoints("", "sdk", TierCommunitySDK)
		assert.Empty(t, endpoints)
		assert.Empty(t, baseURLs)
	})

	t.Run("source metadata preserved", func(t *testing.T) {
		t.Parallel()
		content := `this.get("/v1/users");`

		endpoints, _ := GrepEndpoints(content, "my-sdk", TierOfficialSDK)

		assert.NotEmpty(t, endpoints)
		assert.Equal(t, "my-sdk", endpoints[0].SourceName)
		assert.Equal(t, TierOfficialSDK, endpoints[0].SourceTier)
	})

	t.Run("deduplicates same endpoint", func(t *testing.T) {
		t.Parallel()
		content := `
this.get("/v1/users");
this.get("/v1/users");
`
		endpoints, _ := GrepEndpoints(content, "sdk", TierCommunitySDK)

		count := 0
		for _, ep := range endpoints {
			if ep.Method == "GET" && ep.Path == "/v1/users" {
				count++
			}
		}
		assert.Equal(t, 1, count)
	})

	t.Run("multi-statement line avoids cartesian product", func(t *testing.T) {
		t.Parallel()
		content := `this.get("/v1/users"); this.post("/v2/projects");`

		endpoints, _ := GrepEndpoints(content, "sdk", TierCommunitySDK)

		// Should produce exactly 2 endpoints: GET /v1/users and POST /v2/projects.
		// Must NOT produce cross-products like GET /v2/projects or POST /v1/users.
		type ep struct{ method, path string }
		found := make(map[ep]bool)
		for _, e := range endpoints {
			found[ep{e.Method, e.Path}] = true
		}

		assert.True(t, found[ep{"GET", "/v1/users"}], "expected GET /v1/users")
		assert.True(t, found[ep{"POST", "/v2/projects"}], "expected POST /v2/projects")
		assert.False(t, found[ep{"GET", "/v2/projects"}], "should NOT have GET /v2/projects (cartesian)")
		assert.False(t, found[ep{"POST", "/v1/users"}], "should NOT have POST /v1/users (cartesian)")
		assert.Len(t, endpoints, 2)
	})
}

func TestIsValidAPIPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		path  string
		valid bool
	}{
		{name: "valid api path", path: "/v1/users", valid: true},
		{name: "valid nested path", path: "/api/v2/projects/tasks", valid: true},
		{name: "no leading slash", path: "v1/users", valid: false},
		{name: "js file", path: "/scripts/app.js", valid: false},
		{name: "ts file", path: "/src/index.ts", valid: false},
		{name: "json file", path: "/config/settings.json", valid: false},
		{name: "css file", path: "/styles/main.css", valid: false},
		{name: "node_modules", path: "/node_modules/express", valid: false},
		{name: "root path", path: "/api", valid: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.valid, isValidAPIPath(tt.path))
		})
	}
}

func TestCleanPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "plain path", path: "/v1/users", want: "/v1/users"},
		{name: "template literal", path: "/v1/users/${userId}", want: "/v1/users/{id}"},
		{name: "trailing slash", path: "/v1/users/", want: "/v1/users"},
		{name: "multiple template vars", path: "/v1/orgs/${orgId}/teams/${teamId}", want: "/v1/orgs/{id}/teams/{id}"},
		{name: "empty becomes root", path: "", want: "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, cleanPath(tt.path))
		})
	}
}
