package generator

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedPlaintextCredentialAndDoctor(t *testing.T) {
	t.Setenv("PRINTING_PRESS_VERIFY", "0")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "0")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "0")

	apiSpec := minimalSpec("plainsafe")
	items := apiSpec.Resources["items"]
	items.Endpoints["upload"] = spec.Endpoint{
		Method:             "POST",
		Path:               "/upload",
		Description:        "Upload a login form",
		RequestContentType: "multipart/form-data",
		Body:               []spec.Param{{Name: "password", Type: "string"}},
	}
	apiSpec.Resources["items"] = items
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	modulePath := generatedModulePath(t, outputDir)
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "plaintext_transport_test.go"),
		[]byte(plaintextTransportTestSource(modulePath)),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "cli", "plaintext_exit_test.go"),
		[]byte(plaintextExitTestSource(modulePath)),
		0o644,
	))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestPlaintext|TestCredentialed", "-count=1")
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestPlaintextCredentialExitCode", "-count=1")

	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))

	prefix := naming.EnvPrefix(apiSpec.Name)
	home := t.TempDir()
	env := append(doctorEnv(home, prefix),
		prefix+"_BASE_URL=http://api.plainsafe.test",
		"MYAPI_TOKEN=plainsafe-token",
	)
	for _, gate := range []string{"error", "warn", "stale"} {
		payload, err := runDoctorJSON(t, binaryPath, env, "--fail-on", gate)
		require.Error(t, err, "fail-on %s payload %#v", gate, payload)
		api, _ := payload["api"].(string)
		creds, _ := payload["credentials"].(string)
		require.Contains(t, api, "not probed")
		require.Contains(t, api, "use https")
		require.Contains(t, api, "api.plainsafe.test")
		require.NotContains(t, strings.ToLower(api), "unreachable")
		require.Contains(t, creds, "not probed")
		require.NotContains(t, strings.ToLower(creds), "unreachable")
		require.Equal(t, "http://api.plainsafe.test", payload["base_url"])
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	loopEnv := append(doctorEnv(t.TempDir(), prefix),
		prefix+"_BASE_URL="+srv.URL,
		"MYAPI_TOKEN=plainsafe-token",
	)
	payload, err := runDoctorJSON(t, binaryPath, loopEnv, "--fail-on", "error")
	require.NoError(t, err, payload)
	api, _ := payload["api"].(string)
	require.Contains(t, api, "reachable")
	require.NotContains(t, api, "not probed")
}

func plaintextExitTestSource(modulePath string) string {
	return `package cli

import (
	"errors"
	"fmt"
	"testing"

	"` + modulePath + `/internal/client"
)

func TestPlaintextCredentialExitCode(t *testing.T) {
	err := classifyAPIErrorOnly(fmt.Errorf("wrap: %w", client.ErrPlaintextCredential))
	if got := ExitCode(err); got != 10 {
		t.Fatalf("ExitCode = %d, want 10 (%v)", got, err)
	}
	if !errors.Is(err, client.ErrPlaintextCredential) {
		t.Fatalf("classified error does not unwrap to ErrPlaintextCredential: %v", err)
	}
}
`
}

func plaintextTransportTestSource(modulePath string) string {
	return `package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"` + modulePath + `/internal/cliutil"
	"` + modulePath + `/internal/cliutil/testenv"
	"` + modulePath + `/internal/config"
)

type plaintextRecorder struct {
	hits atomic.Int32
}

func (r *plaintextRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.hits.Add(1)
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("[]")),
		ContentLength: 2,
		Request:    req,
	}, nil
}

func TestPlaintextCredentialRefusedBeforeTransport(t *testing.T) {
	rec := &plaintextRecorder{}
	c := New(&config.Config{BaseURL: "http://api.example.com", AuthHeaderVal: "Bearer secret"}, time.Second, 0)
	c.NoCache = true
	c.HTTPClient.Transport = rec
	_, err := c.Get(context.Background(), "/widgets", nil)
	if !errors.Is(err, ErrPlaintextCredential) {
		t.Fatalf("Get = %v, want ErrPlaintextCredential", err)
	}
	if !strings.Contains(err.Error(), "api.example.com") || !strings.Contains(err.Error(), "use https") {
		t.Fatalf("error = %q, want host and use https", err.Error())
	}
	if rec.hits.Load() != 0 {
		t.Fatalf("transport hits = %d, want 0", rec.hits.Load())
	}
}

func TestPlaintextUncredentialedRequestIsSent(t *testing.T) {
	rec := &plaintextRecorder{}
	c := New(&config.Config{BaseURL: "http://api.example.com"}, time.Second, 0)
	c.NoCache = true
	c.HTTPClient.Transport = rec
	_, err := c.Get(context.Background(), "/widgets", nil)
	if errors.Is(err, ErrPlaintextCredential) {
		t.Fatalf("uncredentialed Get refused: %v", err)
	}
	if rec.hits.Load() == 0 {
		t.Fatal("uncredentialed request did not reach the transport")
	}
}

func TestPlaintextPasswordBodyRefusedBeforeTransport(t *testing.T) {
	rec := &plaintextRecorder{}
	c := New(&config.Config{BaseURL: "http://api.example.com"}, time.Second, 0)
	c.NoCache = true
	c.HTTPClient.Transport = rec
	_, _, err := c.Post(context.Background(), "/login", map[string]string{"password": "secret"})
	if !errors.Is(err, ErrPlaintextCredential) {
		t.Fatalf("Post = %v, want ErrPlaintextCredential", err)
	}
	if rec.hits.Load() != 0 {
		t.Fatalf("transport hits = %d, want 0", rec.hits.Load())
	}

	_, _, err = c.Post(context.Background(), "/login", map[string]string{"password": ""})
	if errors.Is(err, ErrPlaintextCredential) {
		t.Fatalf("empty password was refused: %v", err)
	}
	if rec.hits.Load() == 0 {
		t.Fatal("empty password request did not reach the transport")
	}
}

func TestPlaintextLoopbackAndLocalhostStillWork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)

	c := New(&config.Config{BaseURL: srv.URL, AuthHeaderVal: "Bearer secret"}, time.Second, 0)
	c.NoCache = true
	if _, err := c.Get(context.Background(), "/widgets", nil); err != nil {
		t.Fatalf("127.0.0.1: %v", err)
	}

	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	local := New(&config.Config{BaseURL: "http://localhost:" + port, AuthHeaderVal: "Bearer secret"}, time.Second, 0)
	local.NoCache = true
	if _, err := local.Get(context.Background(), "/widgets", nil); err != nil {
		t.Fatalf("localhost: %v", err)
	}

	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Logf("skip ipv6 loopback: %v", err)
	} else {
		v6 := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
		}))
		v6.Listener = ln
		v6.Start()
		t.Cleanup(v6.Close)
		c6 := New(&config.Config{BaseURL: v6.URL, AuthHeaderVal: "Bearer secret"}, time.Second, 0)
		c6.NoCache = true
		if _, err := c6.Get(context.Background(), "/widgets", nil); err != nil {
			t.Fatalf("ipv6 loopback %s: %v", v6.URL, err)
		}
	}

	for _, raw := range []string{
		"http://127.0.0.1/x",
		"http://localhost/x",
		"http://[::1]/x",
		"http://[::ffff:127.0.0.1]/x",
		"https://api.example.com/x",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := plaintextCredentialError(u, true); err != nil {
			t.Fatalf("%s credentialed = %v, want nil", raw, err)
		}
	}
	u, err := url.Parse("http://api.example.com/x")
	if err != nil {
		t.Fatal(err)
	}
	if plaintextCredentialError(u, false) != nil {
		t.Fatal("uncredentialed plaintext must be allowed")
	}
}

func TestPlaintextDryRunDoesNotRefuse(t *testing.T) {
	rec := &plaintextRecorder{}
	c := New(&config.Config{BaseURL: "http://api.example.com", AuthHeaderVal: "Bearer secret"}, time.Second, 0)
	c.NoCache = true
	c.DryRun = true
	c.HTTPClient.Transport = rec
	if _, err := c.Get(context.Background(), "/widgets", nil); err != nil {
		t.Fatalf("dry-run = %v", err)
	}
	if rec.hits.Load() != 0 {
		t.Fatalf("dry-run transport hits = %d", rec.hits.Load())
	}
}

func TestPlaintextRedirectDropsCredential(t *testing.T) {
	var hits atomic.Int32
	gotAuth := ""
	victim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(victim.Close)
	_, port, err := net.SplitHostPort(victim.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com:"+port+"/landed", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)
	c := New(&config.Config{BaseURL: origin.URL, AuthHeaderVal: "Bearer secret"}, time.Second, 0)
	c.NoCache = true
	tr, ok := c.HTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport %T", c.HTTPClient.Transport)
	}
	prev := tr.DialContext
	victimAddr := victim.Listener.Addr().String()
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(addr)
		if host == "example.com" {
			return (&net.Dialer{}).DialContext(ctx, network, victimAddr)
		}
		if prev != nil {
			return prev(ctx, network, addr)
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	if _, err = c.Get(context.Background(), "/start", nil); err != nil {
		t.Fatalf("stripped redirect: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("redirect target hits = %d", hits.Load())
	}
	if gotAuth != "" {
		t.Fatalf("redirect forwarded Authorization %q", gotAuth)
	}
}

func TestPlaintextRedirectReplayedPasswordIsRefused(t *testing.T) {
	var dials atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/landed", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)
	c := New(&config.Config{BaseURL: origin.URL}, time.Second, 0)
	c.NoCache = true
	tr, ok := c.HTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport %T", c.HTTPClient.Transport)
	}
	prev := tr.DialContext
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(addr)
		if host == "example.com" {
			dials.Add(1)
			return nil, errors.New("dialed example.com")
		}
		if prev != nil {
			return prev(ctx, network, addr)
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	_, _, err := c.Post(context.Background(), "/login", map[string]string{"password": "secret"})
	if !errors.Is(err, ErrPlaintextCredential) {
		t.Fatalf("redirect = %v, want ErrPlaintextCredential", err)
	}
	if dials.Load() != 0 {
		t.Fatalf("plaintext redirect target dials = %d", dials.Load())
	}
}

func TestPlaintextCacheIsNotServed(t *testing.T) {
	testenv.Isolate(t, cliutil.CacheDir)
	c := New(&config.Config{BaseURL: "http://api.example.com", AuthHeaderVal: "Bearer secret"}, time.Second, 0)
	c.writeCache("/widgets", nil, json.RawMessage("{\"cached\":true}"))
	if _, ok := c.readCache("/widgets", nil); !ok {
		t.Fatal("expected a cached body before the refusal")
	}
	got, err := c.Get(context.Background(), "/widgets", nil)
	if !errors.Is(err, ErrPlaintextCredential) {
		t.Fatalf("cached Get = %v body %s, want ErrPlaintextCredential", err, got)
	}
	if strings.Contains(string(got), "cached") {
		t.Fatalf("served cached body %s", got)
	}
}

func TestPlaintextUncredentialedCacheStillServed(t *testing.T) {
	testenv.Isolate(t, cliutil.CacheDir)
	c := New(&config.Config{BaseURL: "http://api.example.com"}, time.Second, 0)
	c.writeCache("/widgets", nil, json.RawMessage("{\"cached\":true}"))
	got, err := c.Get(context.Background(), "/widgets", nil)
	if err != nil {
		t.Fatalf("uncredentialed cache Get: %v", err)
	}
	if !strings.Contains(string(got), "cached") {
		t.Fatalf("body = %s, want cached", got)
	}
}

func TestPlaintextCacheBypassedForHeaderAndQuery(t *testing.T) {
	testenv.Isolate(t, cliutil.CacheDir)
	c := New(&config.Config{BaseURL: "http://api.example.com"}, time.Second, 0)
	headers := map[string]string{"Authorization": "Bearer secret"}
	c.writeCacheWithHeaders("/widgets", nil, headers, json.RawMessage("{\"cached\":true}"))
	got, err := c.GetWithHeaders(context.Background(), "/widgets", nil, headers)
	if !errors.Is(err, ErrPlaintextCredential) {
		t.Fatalf("header Get = %v body %s, want ErrPlaintextCredential", err, got)
	}

	c.writeCache("/widgets", map[string]string{"password": "secret"}, json.RawMessage("{\"cached\":true}"))
	got, err = c.Get(context.Background(), "/widgets", map[string]string{"password": "secret"})
	if !errors.Is(err, ErrPlaintextCredential) {
		t.Fatalf("query Get = %v body %s, want ErrPlaintextCredential", err, got)
	}

	configured := New(&config.Config{
		BaseURL: "http://api.example.com",
		Headers: map[string]string{"Authorization": "Bearer secret"},
	}, time.Second, 0)
	configured.writeCache("/widgets", nil, json.RawMessage("{\"cached\":true}"))
	got, err = configured.Get(context.Background(), "/widgets", nil)
	if !errors.Is(err, ErrPlaintextCredential) {
		t.Fatalf("config header Get = %v body %s, want ErrPlaintextCredential", err, got)
	}
}

func TestPlaintextMultipartPasswordRefusedBeforeTransport(t *testing.T) {
	rec := &plaintextRecorder{}
	c := New(&config.Config{BaseURL: "http://api.example.com"}, time.Second, 0)
	c.NoCache = true
	c.HTTPClient.Transport = rec
	_, _, err := c.PostMultipart(context.Background(), "/upload", map[string]string{"password": "secret"}, nil)
	if !errors.Is(err, ErrPlaintextCredential) {
		t.Fatalf("PostMultipart = %v, want ErrPlaintextCredential", err)
	}
	if rec.hits.Load() != 0 {
		t.Fatalf("transport hits = %d, want 0", rec.hits.Load())
	}

	_, _, err = c.PostMultipart(context.Background(), "/upload", map[string]string{"password": ""}, nil)
	if errors.Is(err, ErrPlaintextCredential) {
		t.Fatalf("empty multipart password was refused: %v", err)
	}
	if rec.hits.Load() == 0 {
		t.Fatal("empty multipart password did not reach the transport")
	}
}

func TestPlaintextLargePasswordBodyRedirectIsRefused(t *testing.T) {
	var dials atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/landed", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)
	c := New(&config.Config{BaseURL: origin.URL}, time.Second, 0)
	c.NoCache = true
	tr, ok := c.HTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport %T", c.HTTPClient.Transport)
	}
	prev := tr.DialContext
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(addr)
		if host == "example.com" {
			dials.Add(1)
			return nil, errors.New("dialed example.com")
		}
		if prev != nil {
			return prev(ctx, network, addr)
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	pad := strings.Repeat("x", 2<<20)
	_, _, err := c.Post(context.Background(), "/login", map[string]string{"note": pad, "password": "secret"})
	if !errors.Is(err, ErrPlaintextCredential) {
		t.Fatalf("large password redirect = %v, want ErrPlaintextCredential", err)
	}
	if dials.Load() != 0 {
		t.Fatalf("plaintext redirect target dials = %d", dials.Load())
	}

	dials.Store(0)
	_, _, err = c.Post(context.Background(), "/login", map[string]string{"note": pad})
	if errors.Is(err, ErrPlaintextCredential) {
		t.Fatalf("large non-password body was refused: %v", err)
	}
	if dials.Load() == 0 {
		t.Fatal("large non-password redirect did not try the public target")
	}
}
`
}

func TestGeneratedPlaintextTokenRequests(t *testing.T) {
	t.Setenv("PRINTING_PRESS_VERIFY", "0")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "0")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "0")

	apiSpec := minimalSpec("plaintok")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "bearer_token",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://auth.example.com/oauth/token",
		EnvVars:     []string{"PLAINTOK_API_KEY", "PLAINTOK_SECRET_KEY"},
	}
	apiSpec.Config.Path = "~/.config/plaintok-pp-cli/config.toml"
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	modulePath := generatedModulePath(t, outputDir)
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "plaintext_token_test.go"),
		[]byte(plaintextTokenTestSource(modulePath)),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "cli", "plaintext_auth_exit_test.go"),
		[]byte(plaintextAuthExitTestSource(modulePath)),
		0o644,
	))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestPlaintextToken", "-count=1")
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestPlaintextAuthCommandExitCode", "-count=1")

	authGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	require.Contains(t, string(authGo), "return authTransportErr(err)")

	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))
	assertPlaintextAuthLoginExit(t, binaryPath, naming.EnvPrefix(apiSpec.Name))
}

func assertPlaintextAuthLoginExit(t *testing.T, binaryPath, prefix string) {
	t.Helper()
	run := func(tokenURL string) (int, string) {
		t.Helper()
		cmd := exec.Command(binaryPath, "auth", "login", "--client-id", "id", "--client-secret", "secret")
		cmd.Env = append(doctorEnv(t.TempDir(), prefix), prefix+"_TOKEN_URL="+tokenURL)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return 0, string(out)
		}
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)
		return exitErr.ExitCode(), string(out)
	}

	code, out := run("http://auth.example.com/oauth/token")
	require.Equal(t, 10, code, out)
	require.Contains(t, out, "auth.example.com")
	require.Contains(t, out, "use https")
	require.NotContains(t, out, "calling token endpoint")

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
	}))
	t.Cleanup(srv.Close)
	code, out = run(srv.URL)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "Logged in")
	require.Equal(t, 1, hits)
}

func plaintextAuthExitTestSource(modulePath string) string {
	return `package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"` + modulePath + `/internal/client"
)

func TestPlaintextAuthCommandExitCode(t *testing.T) {
	authRefused := authTransportErr(fmt.Errorf("wrap: %w", client.ErrPlaintextCredential))
	if got := ExitCode(authRefused); got != 10 {
		t.Fatalf("auth ExitCode = %d, want 10 (%v)", got, authRefused)
	}
	if !errors.Is(authRefused, client.ErrPlaintextCredential) {
		t.Fatalf("auth refusal does not unwrap to ErrPlaintextCredential: %v", authRefused)
	}
	if !strings.Contains(authRefused.Error(), "use https") {
		t.Fatalf("auth refusal = %q, want use https", authRefused.Error())
	}
	if got := ExitCode(authTransportErr(errors.New("token endpoint returned HTTP 400"))); got != 4 {
		t.Fatalf("other auth ExitCode = %d, want 4", got)
	}
}
`
}

func plaintextTokenTestSource(modulePath string) string {
	return `package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"` + modulePath + `/internal/config"
)

type plaintextTokenRecorder struct {
	hits atomic.Int32
}

func (r *plaintextTokenRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.hits.Add(1)
	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader("[]")),
		ContentLength: 2,
		Request:       req,
	}, nil
}

func TestPlaintextTokenMintAndRefreshRefused(t *testing.T) {
	rec := &plaintextTokenRecorder{}
	c := New(&config.Config{
		BaseURL:      "https://api.example.com",
		TokenURL:     "http://auth.example.com/oauth/token",
		ClientID:     "id",
		ClientSecret: "secret",
		RefreshToken: "rt-secret",
		Path:         filepath.Join(t.TempDir(), "config.toml"),
	}, time.Second, 0)
	c.HTTPClient.Transport = rec
	if err := c.mintClientCredentials(context.Background(), "id", "secret"); !errors.Is(err, ErrPlaintextCredential) {
		t.Fatalf("mint = %v, want ErrPlaintextCredential", err)
	}
	err := c.refreshAccessToken(context.Background())
	if !errors.Is(err, ErrPlaintextCredential) {
		t.Fatalf("refresh = %v, want ErrPlaintextCredential", err)
	}
	if !strings.Contains(err.Error(), "auth.example.com") || !strings.Contains(err.Error(), "use https") {
		t.Fatalf("refresh error = %q, want host and use https", err.Error())
	}
	if rec.hits.Load() != 0 {
		t.Fatalf("transport hits = %d, want 0", rec.hits.Load())
	}
}

func TestPlaintextTokenHTTPSStillSent(t *testing.T) {
	rec := &plaintextTokenRecorder{}
	c := New(&config.Config{
		BaseURL:      "https://api.example.com",
		TokenURL:     "https://auth.example.com/oauth/token",
		ClientID:     "id",
		ClientSecret: "secret",
		RefreshToken: "rt-secret",
		Path:         filepath.Join(t.TempDir(), "config.toml"),
	}, time.Second, 0)
	c.HTTPClient.Transport = rec
	err := c.refreshAccessToken(context.Background())
	if errors.Is(err, ErrPlaintextCredential) {
		t.Fatalf("https refresh refused: %v", err)
	}
	if rec.hits.Load() == 0 {
		t.Fatal("https refresh did not reach the transport")
	}
}
`
}
