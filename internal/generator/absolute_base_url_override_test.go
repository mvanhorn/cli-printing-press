package generator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeneratedClientRebasesAbsoluteURLWhenBaseURLOverrideIsSet proves a
// mixed-host CLI keeps absolute endpoints on their declared host until the
// configured base URL leaves the compiled default, then sends them to the
// override origin with the original path and query. BasePath stays on the
// relative concat.
func TestGeneratedClientRebasesAbsoluteURLWhenBaseURLOverrideIsSet(t *testing.T) {
	t.Parallel()

	defaultSrv := newEchoServer(t, "default")
	secondSrv := newEchoServer(t, "second")
	overrideSrv := newEchoServer(t, "override")

	const basePath = "/svc"
	absolutePath := secondSrv.URL + "/book?region=us&view=full"
	apiSpec := mixedHostSpec("mixhost", defaultSrv.URL, basePath, spec.Endpoint{
		Method:      "GET",
		Path:        "/book?region=us&view=full",
		BaseURL:     secondSrv.URL,
		Description: "Order book on a second host",
	})

	outputDir := generateMixedHostCLI(t, apiSpec)
	handler := readGeneratedFile(t, outputDir, "internal", "cli", "markets_book.go")
	assert.Contains(t, handler, `path := "`+absolutePath+`"`)
	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, clientSrc, "func rebaseAbsoluteURLOntoOverride(")
	assert.Contains(t, clientSrc, "rebaseAbsoluteURLOntoOverride(path, c.BaseURL)")
	assert.Contains(t, clientSrc, "const specDefaultBaseURL = "+fmt.Sprintf("%q", defaultSrv.URL))
	assert.NotContains(t, clientSrc, `buildURL("", path, endpointVars)`)

	runAbsoluteOverrideBehavior(t, outputDir, absoluteOverrideCase{
		module:       naming.CLI(apiSpec.Name),
		envName:      naming.EnvPrefix(apiSpec.Name) + "_BASE_URL",
		specDefault:  defaultSrv.URL,
		absolutePath: absolutePath,
		overrideURL:  overrideSrv.URL,
		userinfoURL:  withUserinfo(t, overrideSrv.URL, "user", "pass"),
		basePath:     basePath,
	})
}

// TestGeneratedClientRebasesTemplatedAbsoluteURLWhenBaseURLOverrideIsSet
// covers the buildURL branch: placeholders in an absolute path are resolved
// before the origin is replaced, so an override cannot dial the substituted
// declared host.
func TestGeneratedClientRebasesTemplatedAbsoluteURLWhenBaseURLOverrideIsSet(t *testing.T) {
	t.Parallel()

	defaultSrv := newEchoServer(t, "default")
	secondSrv := newEchoServer(t, "second")
	overrideSrv := newEchoServer(t, "override")

	secondURL, err := url.Parse(secondSrv.URL)
	require.NoError(t, err)
	overrideURL, err := url.Parse(overrideSrv.URL)
	require.NoError(t, err)

	const basePath = "/svc"
	absolutePath := "http://{hostport}/book?region=us&view=full"
	apiSpec := mixedHostSpec("mixhosttpl", defaultSrv.URL, basePath, spec.Endpoint{
		Method:      "GET",
		Path:        "/book?region=us&view=full",
		BaseURL:     "http://{hostport}",
		Description: "Order book on a templated second host",
	})
	apiSpec.EndpointTemplateVars = []string{"hostport", "addr"}
	apiSpec.GlobalPathTemplateVars = []string{"addr"}

	outputDir := generateMixedHostCLI(t, apiSpec)
	handler := readGeneratedFile(t, outputDir, "internal", "cli", "markets_book.go")
	assert.Contains(t, handler, `path := "`+absolutePath+`"`)
	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, clientSrc, `buildURL("", path, endpointVars)`)
	assert.Contains(t, clientSrc, "rebaseAbsoluteURLOntoOverride(targetURL, c.BaseURL, endpointVars)")
	assert.Contains(t, clientSrc, `buildURL(configuredBase, "", endpointVars)`)
	urlSrc := readGeneratedFile(t, outputDir, "internal", "client", "url.go")
	assert.Contains(t, urlSrc, "url.PathEscape(v)", "addr must be a global path var so the override is not path-escaped")
	assert.Contains(t, urlSrc, `"addr": true`)

	runAbsoluteOverrideBehavior(t, outputDir, absoluteOverrideCase{
		module:         naming.CLI(apiSpec.Name),
		envName:        naming.EnvPrefix(apiSpec.Name) + "_BASE_URL",
		specDefault:    defaultSrv.URL,
		absolutePath:   absolutePath,
		overrideURL:    overrideSrv.URL,
		userinfoURL:    withUserinfo(t, overrideSrv.URL, "user", "pass"),
		basePath:       basePath,
		hostportEnv:    spec.DefaultEndpointTemplateEnvName(apiSpec.Name, "hostport"),
		hostportValue:  secondURL.Host,
		declaredServer: "second",
		proxyEnv:       spec.DefaultEndpointTemplateEnvName(apiSpec.Name, "addr"),
		proxyValue:     overrideURL.Host,
	})
}

type absoluteOverrideCase struct {
	module         string
	envName        string
	specDefault    string
	absolutePath   string
	overrideURL    string
	userinfoURL    string
	basePath       string
	hostportEnv    string
	hostportValue  string
	declaredServer string
	proxyEnv       string
	proxyValue     string
}

func mixedHostSpec(name, baseURL, basePath string, book spec.Endpoint) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.BaseURL = baseURL
	apiSpec.BasePath = basePath
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"markets": {
			Description: "Markets split across hosts",
			Endpoints: map[string]spec.Endpoint{
				"ticker": {Method: "GET", Path: "/ticker", Description: "Ticker on the default host"},
				"book":   book,
			},
		},
	}
	return apiSpec
}

func generateMixedHostCLI(t *testing.T, apiSpec *spec.APISpec) string {
	t.Helper()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)
	return outputDir
}

func newEchoServer(t *testing.T, name string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _, _ := r.BasicAuth()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"server": name,
			"path":   r.URL.Path,
			"query":  r.URL.RawQuery,
			"user":   user,
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func withUserinfo(t *testing.T, raw, user, pass string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	parsed.User = url.UserPassword(user, pass)
	return parsed.String()
}

func runAbsoluteOverrideBehavior(t *testing.T, outputDir string, tc absoluteOverrideCase) {
	t.Helper()
	if tc.declaredServer == "" {
		tc.declaredServer = "second"
	}
	behavior := fmt.Sprintf(absoluteOverrideBehaviorTest,
		tc.module,
		tc.module,
		tc.module,
		tc.envName,
		tc.specDefault,
		tc.absolutePath,
		tc.overrideURL,
		tc.userinfoURL,
		tc.basePath,
		tc.hostportEnv,
		tc.hostportValue,
		tc.declaredServer,
		tc.proxyEnv,
		tc.proxyValue,
	)
	testPath := filepath.Join(outputDir, "internal", "client", "absolute_base_url_override_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(behavior), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/client", "-run", "TestAbsoluteURLHonorsConfiguredBaseURL", "-count=1")
}

const absoluteOverrideBehaviorTest = `package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	cliutil "%s/internal/cliutil"
	testenv "%s/internal/cliutil/testenv"
	cfgpkg "%s/internal/config"
)

const (
	baseURLEnv     = %q
	specDefault    = %q
	absolutePath   = %q
	overrideURL    = %q
	userinfoURL    = %q
	basePath       = %q
	hostportEnv    = %q
	hostportValue  = %q
	declaredServer = %q
	proxyEnv       = %q
	proxyValue     = %q
)

func TestAbsoluteURLHonorsConfiguredBaseURL(t *testing.T) {
	testenv.Isolate(t, cliutil.ConfigDir, cliutil.DataDir, cliutil.StateDir, cliutil.CacheDir)
	if hostportEnv != "" {
		t.Setenv(hostportEnv, hostportValue)
	}

	bookQuery := map[string]string{"region": "us", "view": "full"}

	t.Run("unset keeps declared host", func(t *testing.T) {
		got := getEcho(t, "", absolutePath, map[string]string{})
		expectEcho(t, got, declaredServer, "/book", "", bookQuery)
		rel := getEcho(t, "", "/ticker", map[string]string{})
		expectEcho(t, rel, "default", basePath+"/ticker", "", nil)
	})

	t.Run("env restating spec default keeps declared host", func(t *testing.T) {
		got := getEcho(t, specDefault, absolutePath, map[string]string{})
		expectEcho(t, got, declaredServer, "/book", "", bookQuery)
	})

	t.Run("trailing slash on spec default keeps declared host", func(t *testing.T) {
		got := getEcho(t, specDefault+"/", absolutePath, map[string]string{})
		expectEcho(t, got, declaredServer, "/book", "", bookQuery)
	})

	t.Run("override host keeps path and query", func(t *testing.T) {
		got := getEcho(t, overrideURL, absolutePath, map[string]string{})
		expectEcho(t, got, "override", "/book", "", bookQuery)
		rel := getEcho(t, overrideURL, "/ticker", map[string]string{})
		expectEcho(t, rel, "override", basePath+"/ticker", "", nil)
	})

	t.Run("override path is not prepended and base path stays relative", func(t *testing.T) {
		got := getEcho(t, specDefault+"/prefix", absolutePath, map[string]string{"extra": "1"})
		expectEcho(t, got, "default", "/book", "", map[string]string{"region": "us", "view": "full", "extra": "1"})
		rel := getEcho(t, specDefault+"/prefix", "/ticker", map[string]string{})
		expectEcho(t, rel, "default", "/prefix"+basePath+"/ticker", "", nil)
	})

	t.Run("override userinfo is sent with the original path", func(t *testing.T) {
		got := getEcho(t, userinfoURL, absolutePath, map[string]string{})
		expectEcho(t, got, "override", "/book", "user", bookQuery)
	})

	t.Run("invalid override does not call the declared host", func(t *testing.T) {
		_, err := getEchoErr(t, "not-a-url", absolutePath, nil)
		if err == nil {
			t.Fatal("expected invalid base URL error")
		}
		if !strings.Contains(err.Error(), baseURLEnv) {
			t.Fatalf("error %%q should name %%s", err.Error(), baseURLEnv)
		}
	})

	if proxyEnv != "" {
		t.Run("templated override keeps host punctuation", func(t *testing.T) {
			t.Setenv(proxyEnv, proxyValue)
			got := getEcho(t, "http://{addr}", absolutePath, map[string]string{})
			expectEcho(t, got, "override", "/book", "", bookQuery)
			rel := getEcho(t, "http://{addr}", "/ticker", map[string]string{})
			expectEcho(t, rel, "override", basePath+"/ticker", "", nil)
		})

		t.Run("unresolved templated override does not call the declared host", func(t *testing.T) {
			t.Setenv(proxyEnv, "")
			_, err := getEchoErr(t, "http://{addr}", absolutePath, nil)
			if err == nil {
				t.Fatal("expected unresolved template override error")
			}
			if !strings.Contains(err.Error(), proxyEnv) {
				t.Fatalf("error %%q should name %%s", err.Error(), proxyEnv)
			}
		})
	}
}

func getEcho(t *testing.T, baseURL, path string, params map[string]string) map[string]string {
	t.Helper()
	got, err := getEchoErr(t, baseURL, path, params)
	if err != nil {
		t.Fatalf("GET %%s base %%s: %%v", path, baseURL, err)
	}
	return got
}

func getEchoErr(t *testing.T, baseURL, path string, params map[string]string) (map[string]string, error) {
	t.Helper()
	t.Setenv(baseURLEnv, baseURL)
	cfg, err := cfgpkg.Load("")
	if err != nil {
		return nil, err
	}
	if cfg.BasePath != basePath {
		t.Fatalf("BasePath = %%q, want %%s", cfg.BasePath, basePath)
	}
	c := New(cfg, time.Second, 0)
	c.HTTPClient = &http.Client{Transport: &http.Transport{Proxy: nil}}
	c.NoCache = true
	raw, err := c.Get(context.Background(), path, params)
	if err != nil {
		return nil, err
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		return nil, err
	}
	return got, nil
}

func expectEcho(t *testing.T, got map[string]string, server, path, user string, query map[string]string) {
	t.Helper()
	if got["server"] != server || got["path"] != path || got["user"] != user {
		t.Fatalf("got server=%%s path=%%s user=%%s, want server=%%s path=%%s user=%%s", got["server"], got["path"], got["user"], server, path, user)
	}
	values, err := url.ParseQuery(got["query"])
	if err != nil {
		t.Fatalf("query %%q: %%v", got["query"], err)
	}
	if len(values) != len(query) {
		t.Fatalf("query %%v, want %%v", values, query)
	}
	for key, want := range query {
		if values.Get(key) != want {
			t.Fatalf("query[%%s]=%%q, want %%q (full %%q)", key, values.Get(key), want, got["query"])
		}
	}
}
`
