package generator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClientHonorsSpecBasePath checks that a spec declaring BaseURL plus a
// separate BasePath produces a generated client that sends requests to
// ${BaseURL}${BasePath}${path}. Some APIs split the host from the path mount
// (e.g. base_url: https://host, base_path: /~api); previously that prefix was
// dropped and every request 404'd.
func TestClientHonorsSpecBasePath(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("basepath-honored")
	apiSpec.BasePath = "/~api"

	outputDir := filepath.Join(t.TempDir(), "basepath-honored-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	client := string(clientSrc)

	// Whitespace-insensitive: gofmt may align columns differently than the
	// template's literal indentation, so match field shape via regex.
	clientBasePathField := regexp.MustCompile(`(?m)^\s*BasePath\s+string\b`)
	assert.Regexp(t, clientBasePathField, client,
		"Client struct should expose a BasePath field when the spec declares one")
	clientNewAssign := regexp.MustCompile(`BasePath:\s+normalizeBasePath\(cfg\.BasePath\)`)
	assert.Regexp(t, clientNewAssign, client,
		"New() should populate Client.BasePath from cfg.BasePath via the normalizer")
	assert.Contains(t, client, "c.BaseURL + c.BasePath + path",
		"do() should construct request URLs as BaseURL+BasePath+path")

	cacheKeyBody := clientCacheKeyBody(t, client)
	assert.Contains(t, cacheKeyBody, `"|base_path=" + c.BasePath`,
		"cache key should include BasePath so a config change invalidates correctly")

	configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	config := string(configSrc)

	configBasePathField := regexp.MustCompile("(?m)^\\s*BasePath\\s+string\\s+`toml:\"base_path\"`")
	assert.Regexp(t, configBasePathField, config,
		"Config struct should expose BasePath with a serialized tag matching the spec's config format")
	assert.Contains(t, config, `BasePath: "/~api"`,
		"Load() should seed cfg.BasePath from the spec default")
	assert.Contains(t, config, `os.Getenv("BASEPATH_HONORED_BASE_PATH")`,
		"Load() should accept an env-var override for BasePath")
}

// TestClientWithoutBasePathByteIdentical asserts the negative case: a spec
// with no BasePath produces the same client.go and config.go content as
// before this change (the conditional template guards must not leak when
// .BasePath is empty).
func TestClientWithoutBasePathByteIdentical(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("no-basepath")

	outputDir := filepath.Join(t.TempDir(), "no-basepath-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	client := string(clientSrc)
	assert.NotContains(t, client, "BasePath",
		"client.go must not emit BasePath when the spec doesn't declare base_path")
	assert.NotContains(t, client, "normalizeBasePath",
		"client.go must not emit the normalizeBasePath helper when unused")

	configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	config := string(configSrc)
	assert.NotContains(t, config, "BasePath",
		"config.go must not emit BasePath when the spec doesn't declare base_path")
	assert.NotContains(t, config, "_BASE_PATH",
		"config.go must not emit the BASE_PATH env override when BasePath is empty")
}

// TestClientBasePathLiveRequest compiles a printed CLI from a BasePath spec
// against a stub server and asserts the request URL hits the prefix. This is
// the runtime test: template content matching can drift; only the live HTTP
// path proves the fix.
func TestClientBasePathLiveRequest(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		gotPaths []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPaths = append(gotPaths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items": []}`))
	}))
	t.Cleanup(server.Close)

	apiSpec := minimalSpec("bplive")
	apiSpec.BaseURL = server.URL
	apiSpec.BasePath = "/api/v1"
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"things": {
			Description: "Manage things",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/things", Description: "List things"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "bplive-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	runGoCommand(t, outputDir, "mod", "tidy")
	binaryPath := filepath.Join(outputDir, "bplive-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/bplive-pp-cli")

	cmd := exec.Command(binaryPath, "things", "list", "--json")
	cmd.Env = append(os.Environ(), "BPLIVE_BASE_URL="+server.URL)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	// Confirm the response decodes; the actual shape comes from the stub.
	var resp any
	require.NoError(t, json.Unmarshal(out, &resp), string(out))

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, gotPaths, "stub server should have received at least one request")
	for _, p := range gotPaths {
		assert.True(t, strings.HasPrefix(p, "/api/v1/"),
			"request path %q should start with the BasePath prefix /api/v1/", p)
	}
	assert.Contains(t, gotPaths, "/api/v1/things",
		"the things.list endpoint should hit /api/v1/things, not /things")
}
