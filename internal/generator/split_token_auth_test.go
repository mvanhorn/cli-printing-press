package generator

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/stretchr/testify/require"
)

func TestGeneratedSplitTokenClientSendsOnlyOperationCredential(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("../../testdata/golden/fixtures/split-token-auth.yaml")
	require.NoError(t, err)
	apiSpec, err := openapi.Parse(body)
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	prefix := naming.EnvPrefix(apiSpec.Name)
	serverEnv := prefix + "_SERVER_TOKEN"
	accountEnv := prefix + "_ACCOUNT_TOKEN"
	serverField := resolveEnvVarField(serverEnv)
	accountField := resolveEnvVarField(accountEnv)
	modulePath := naming.CLI(apiSpec.Name)

	configSrc := readGenerated(t, outputDir, "internal", "config", "config.go")
	require.Contains(t, configSrc, `cliutil.EnvOverride("`+serverEnv+`")`)
	require.Contains(t, configSrc, `cliutil.EnvOverride("`+accountEnv+`")`)
	require.Contains(t, configSrc, serverField)
	require.Contains(t, configSrc, accountField)

	clientSrc := readGenerated(t, outputDir, "internal", "client", "client.go")
	require.Contains(t, clientSrc, `opScheme := c.operationAuthScheme(method, path)`)
	require.Contains(t, clientSrc, `sendsPrimaryAuth := opScheme == "" || opScheme == "serverToken"`)
	require.Contains(t, clientSrc, `if sendsPrimaryAuth {`)
	require.Contains(t, clientSrc, `if opScheme == "accountToken"`)
	require.GreaterOrEqual(t, strings.Count(clientSrc, `req.Header.Set("X-Account-Token", v)`), 2)
	require.Contains(t, clientSrc, `req.Header.Set("X-Server-Token", authHeader)`)
	require.Contains(t, clientSrc, `operationCredentialCacheID`)
	require.Contains(t, clientSrc, `|op_cred=`)
	require.Contains(t, clientSrc, `operationSendsPrimaryAuth`)
	require.Contains(t, clientSrc, `destinationScheme := c.operationAuthScheme(req.Method, operationAuthRequestPath(req))`)
	require.Contains(t, clientSrc, `if destinationScheme == "accountToken"`)
	require.NotContains(t, clientSrc, `else if redirectScheme != "accountToken"`)

	doctorSrc := readGenerated(t, outputDir, "internal", "cli", "doctor.go")
	require.Contains(t, doctorSrc, `report["auth_schemes"]`)
	require.Contains(t, doctorSrc, `"accountToken: configured"`)
	require.Contains(t, doctorSrc, `"serverToken: not configured"`)
	require.Contains(t, doctorSrc, serverEnv+` reported per auth scheme`)
	require.NotContains(t, doctorSrc, `recordAdditionalAuthEnv("`+accountEnv+`"`)

	requireGeneratedCompiles(t, outputDir)

	behaviorTest := `package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"` + modulePath + `/internal/cliutil"
	"` + modulePath + `/internal/config"
)

func TestSplitTokenHeaders(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		serverTok := r.Header.Get("X-Server-Token")
		accountTok := r.Header.Get("X-Account-Token")
		switch r.URL.Path {
		case "/messages/abc", "/servers/push":
			if serverTok != "server-secret" || accountTok != "" {
				t.Errorf("%s headers server=%q account=%q", r.URL.Path, serverTok, accountTok)
			}
		case "/servers", "/servers/acct-1", "/servers/acct-1-next":
			if accountTok != "account-secret" || serverTok != "" {
				t.Errorf("%s headers server=%q account=%q", r.URL.Path, serverTok, accountTok)
			}
			if r.URL.Path == "/servers/acct-1" {
				http.Redirect(w, r, "/servers/acct-1-next", http.StatusFound)
				return
			}
		case "/unknown":
			if serverTok != "server-secret" || accountTok != "" {
				t.Errorf("unmatched path headers server=%q account=%q", serverTok, accountTok)
			}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
	}))
	defer server.Close()

	cfg := &config.Config{
		BaseURL: server.URL,
		` + serverField + `: "server-secret",
		` + accountField + `: "account-secret",
	}
	c := New(cfg, time.Second, 0)
	c.NoCache = true
	ctx := context.Background()
	for _, path := range []string{"/messages/abc", "/messages/abc?x=1", "/servers/push", "/servers", "/servers/acct-1", "/unknown"} {
		if _, err := c.Get(ctx, path, nil); err != nil {
			t.Fatalf("Get %s: %v", path, err)
		}
	}
	if len(seen) < 6 {
		t.Fatalf("saw %d requests, want at least 6 (including redirect)", len(seen))
	}
}

func TestOperationAuthSchemeMatching(t *testing.T) {
	c := &Client{BaseURL: "https://api.example.com/v1"}
	if got := c.operationAuthScheme("get", "/servers/push"); got != "serverToken" {
		t.Fatalf("push scheme = %q", got)
	}
	if got := c.operationAuthScheme("GET", "/servers/acct-1"); got != "accountToken" {
		t.Fatalf("id scheme = %q", got)
	}
	if got := c.operationAuthScheme("GET", "/messages/abc?x=1"); got != "serverToken" {
		t.Fatalf("query scheme = %q", got)
	}
	if got := c.operationAuthScheme("GET", "https://api.example.com/v1/servers/acct-1"); got != "accountToken" {
		t.Fatalf("absolute scheme = %q", got)
	}
	if got := c.operationAuthScheme("GET", "/unknown"); got != "" {
		t.Fatalf("unknown scheme = %q", got)
	}
}

func TestAccountTokenCacheDoesNotCrossAccounts(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Server-Token") != "" {
			t.Errorf("account endpoint sent server token %q", r.Header.Get("X-Server-Token"))
		}
		account := r.Header.Get("X-Account-Token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"account":"` + "`" + ` + account + ` + "`" + `"}` + "`" + `))
	}))
	defer server.Close()

	cfg := &config.Config{
		BaseURL: server.URL,
		Path:    "same-config",
		` + serverField + `: "server-secret",
		` + accountField + `: "account-a",
	}
	c := New(cfg, time.Second, 0)
	c.cacheDir = t.TempDir()
	ctx := context.Background()
	first, err := c.Get(ctx, "/servers", nil)
	if err != nil {
		t.Fatalf("first get: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls after first get = %d", calls)
	}
	warm, err := c.Get(ctx, "/servers", nil)
	if err != nil {
		t.Fatalf("warm get: %v", err)
	}
	if calls != 1 {
		t.Fatalf("cache was not warm, calls = %d", calls)
	}
	if string(warm) != string(first) {
		t.Fatalf("warm body = %s, want %s", warm, first)
	}
	cfg.` + accountField + ` = "account-b"
	second, err := c.Get(ctx, "/servers", nil)
	if err != nil {
		t.Fatalf("second account: %v", err)
	}
	if calls != 2 {
		t.Fatalf("second account reused the first account cache, calls = %d body = %s", calls, second)
	}
	if !strings.Contains(string(second), "account-b") {
		t.Fatalf("second body = %s", second)
	}
}

func TestRedirectAppliesDestinationAccountCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/messages/hop":
			if r.Header.Get("X-Server-Token") != "server-secret" || r.Header.Get("X-Account-Token") != "" {
				t.Errorf("origin server=%q account=%q", r.Header.Get("X-Server-Token"), r.Header.Get("X-Account-Token"))
			}
			http.Redirect(w, r, "/servers/landed", http.StatusFound)
		case "/servers/landed":
			if r.Header.Get("X-Account-Token") != "account-secret" || r.Header.Get("X-Server-Token") != "" {
				t.Errorf("redirect server=%q account=%q", r.Header.Get("X-Server-Token"), r.Header.Get("X-Account-Token"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		BaseURL: server.URL,
		` + serverField + `: "server-secret",
		` + accountField + `: "account-secret",
	}
	c := New(cfg, time.Second, 0)
	c.NoCache = true
	if _, err := c.Get(context.Background(), "/messages/hop", nil); err != nil {
		t.Fatalf("redirected get: %v", err)
	}
}

func TestAccountTokenIgnoresUnusedServerCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Server-Token") != "" || r.Header.Get("X-Account-Token") != "account-secret" {
			t.Errorf("path %s server=%q account=%q", r.URL.Path, r.Header.Get("X-Server-Token"), r.Header.Get("X-Account-Token"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
	}))
	defer server.Close()

	refused := &config.Config{
		BaseURL: server.URL,
		` + accountField + `: "account-secret",
		CredentialRefusals: []cliutil.CredentialRefusal{{
			Source:             "config",
			Path:               "config.toml",
			Err:                errors.New("unsafe permissions"),
			CredentialsPresent: true,
		}},
	}
	c := New(refused, time.Second, 0)
	c.NoCache = true
	if _, err := c.Get(context.Background(), "/servers", nil); err != nil {
		t.Fatalf("refused server token blocked account endpoint: %v", err)
	}

	placeholder := &config.Config{
		BaseURL: server.URL,
		` + serverField + `: "YOUR_TOKEN_HERE",
		` + accountField + `: "account-secret",
	}
	c = New(placeholder, time.Second, 0)
	c.NoCache = true
	if _, err := c.Get(context.Background(), "/servers", nil); err != nil {
		t.Fatalf("placeholder server token blocked account endpoint: %v", err)
	}
	if _, err := c.Get(context.Background(), "/messages/abc", nil); err == nil {
		t.Fatal("server endpoint accepted a placeholder server token")
	}
}

func TestRedirectKeepsCallerAccountToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/servers/acct-1":
			if r.Header.Get("X-Account-Token") != "caller-account" {
				t.Errorf("origin account=%q", r.Header.Get("X-Account-Token"))
			}
			http.Redirect(w, r, "/servers/acct-1-next", http.StatusFound)
		case "/servers/acct-1-next":
			if r.Header.Get("X-Account-Token") != "caller-account" || r.Header.Get("X-Server-Token") != "" {
				t.Errorf("redirect server=%q account=%q", r.Header.Get("X-Server-Token"), r.Header.Get("X-Account-Token"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		BaseURL: server.URL,
		` + serverField + `: "server-secret",
		` + accountField + `: "account-secret",
	}
	c := New(cfg, time.Second, 0)
	c.NoCache = true
	if _, err := c.GetWithHeaders(context.Background(), "/servers/acct-1", nil, map[string]string{"X-Account-Token": "caller-account"}); err != nil {
		t.Fatalf("redirected get: %v", err)
	}
}

func TestUnmatchedRedirectDropsAccountToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/servers/acct-1":
			if r.Header.Get("X-Account-Token") != "caller-account" {
				t.Errorf("origin account=%q", r.Header.Get("X-Account-Token"))
			}
			http.Redirect(w, r, "/not-listed", http.StatusFound)
		case "/not-listed":
			if r.Header.Get("X-Account-Token") != "" || r.Header.Get("X-Server-Token") != "" {
				t.Errorf("unlisted server=%q account=%q", r.Header.Get("X-Server-Token"), r.Header.Get("X-Account-Token"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		BaseURL: server.URL,
		` + serverField + `: "server-secret",
		` + accountField + `: "account-secret",
	}
	c := New(cfg, time.Second, 0)
	c.NoCache = true
	if _, err := c.GetWithHeaders(context.Background(), "/servers/acct-1", nil, map[string]string{"X-Account-Token": "caller-account"}); err != nil {
		t.Fatalf("redirected get: %v", err)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "split_auth_test.go"), []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/client", "-count=1")
}

func TestGeneratedDoctorReportsSplitTokenSchemesSeparately(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("../../testdata/golden/fixtures/split-token-auth.yaml")
	require.NoError(t, err)
	apiSpec, err := openapi.Parse(body)
	require.NoError(t, err)

	_, binaryPath := buildGeneratedBinary(t, apiSpec)
	prefix := naming.EnvPrefix(apiSpec.Name)
	serverEnv := prefix + "_SERVER_TOKEN"
	accountEnv := prefix + "_ACCOUNT_TOKEN"

	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer probe.Close()

	home := t.TempDir()
	base := doctorEnv(home, prefix)
	both := append(append([]string{}, base...), serverEnv+"=server-secret", accountEnv+"=account-secret", prefix+"_BASE_URL="+probe.URL)
	payload, err := runDoctorJSON(t, binaryPath, both)
	require.NoError(t, err)
	require.Equal(t, "configured", payload["auth"])
	schemes, _ := payload["auth_schemes"].(string)
	require.Equal(t, "accountToken: configured; serverToken: configured", schemes)

	serverOnly := append(append([]string{}, base...), serverEnv+"=server-secret", accountEnv+"=", prefix+"_BASE_URL="+probe.URL)
	payload, err = runDoctorJSON(t, binaryPath, serverOnly)
	require.NoError(t, err)
	require.Equal(t, "configured", payload["auth"])
	schemes, _ = payload["auth_schemes"].(string)
	require.Equal(t, "WARN accountToken: not configured; serverToken: configured", schemes)
	_, err = runDoctorJSON(t, binaryPath, serverOnly, "--fail-on", "error")
	require.NoError(t, err, "a missing per-operation sibling must not fail --fail-on=error")
	_, err = runDoctorJSON(t, binaryPath, serverOnly, "--fail-on", "warn")
	require.Error(t, err, "--fail-on=warn trips when a split credential is missing")

	human := runDoctorHuman(t, binaryPath, serverOnly)
	require.Contains(t, human, "Auth Schemes:")
	require.Contains(t, human, "WARN accountToken: not configured; serverToken: configured")

	accountOnly := append(append([]string{}, base...), serverEnv+"=", accountEnv+"=account-secret", prefix+"_BASE_URL="+probe.URL)
	payload, err = runDoctorJSON(t, binaryPath, accountOnly)
	require.NoError(t, err)
	require.Equal(t, "configured", payload["auth"])
	schemes, _ = payload["auth_schemes"].(string)
	require.Equal(t, "WARN accountToken: configured; serverToken: not configured", schemes)
	envVars, _ := payload["env_vars"].(string)
	require.NotContains(t, envVars, "ERROR")
	require.NotContains(t, envVars, "missing required")
	require.Contains(t, envVars, serverEnv+" reported per auth scheme")
	_, err = runDoctorJSON(t, binaryPath, accountOnly, "--fail-on", "error")
	require.NoError(t, err, "a missing server token must not fail --fail-on=error when the account token is configured")
	_, err = runDoctorJSON(t, binaryPath, accountOnly, "--fail-on", "warn")
	require.Error(t, err, "--fail-on=warn trips when the server token scheme is absent")

	neither := append(append([]string{}, base...), serverEnv+"=", accountEnv+"=", prefix+"_BASE_URL="+probe.URL)
	payload, err = runDoctorJSON(t, binaryPath, neither)
	require.NoError(t, err)
	envVars, _ = payload["env_vars"].(string)
	require.Contains(t, envVars, "ERROR missing required: "+serverEnv)
	_, err = runDoctorJSON(t, binaryPath, neither, "--fail-on", "error")
	require.Error(t, err)
}

func TestRedirectDoesNotAttachQueryAccountTokenToUnmatchedPath(t *testing.T) {
	t.Parallel()

	const specYAML = `
openapi: "3.0.3"
info:
  title: Query Split API
  version: "1.0.0"
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    serverToken:
      type: apiKey
      in: header
      name: X-Server-Token
    accountToken:
      type: apiKey
      in: query
      name: account_token
paths:
  /messages:
    get:
      operationId: listMessages
      security:
        - serverToken: []
      responses:
        "200":
          description: OK
  /messages/{id}:
    get:
      operationId: getMessage
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      security:
        - serverToken: []
      responses:
        "200":
          description: OK
  /servers/{id}:
    get:
      operationId: getServer
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      security:
        - accountToken: []
      responses:
        "200":
          description: OK
`
	apiSpec, err := openapi.Parse([]byte(specYAML))
	require.NoError(t, err)
	require.Equal(t, "serverToken", apiSpec.Auth.Scheme)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	prefix := naming.EnvPrefix(apiSpec.Name)
	serverField := resolveEnvVarField(prefix + "_SERVER_TOKEN")
	accountField := resolveEnvVarField(prefix + "_ACCOUNT_TOKEN")
	clientSrc := readGenerated(t, outputDir, "internal", "client", "client.go")
	require.Contains(t, clientSrc, `q.Set("account_token", carried)`)
	require.Contains(t, clientSrc, `if destinationScheme == "accountToken"`)
	requireGeneratedCompiles(t, outputDir)

	behaviorTest := `package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"` + naming.CLI(apiSpec.Name) + `/internal/config"
)

func TestQueryRedirectAccountToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/servers/acct-1":
			if r.URL.Query().Get("account_token") != "account-secret" || r.Header.Get("X-Server-Token") != "" {
				t.Errorf("origin server=%q account=%q", r.Header.Get("X-Server-Token"), r.URL.Query().Get("account_token"))
			}
			http.Redirect(w, r, "/not-listed", http.StatusFound)
		case "/not-listed":
			if r.URL.Query().Get("account_token") != "" {
				t.Errorf("unlisted path received account token %q", r.URL.Query().Get("account_token"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
		case "/messages/hop":
			if r.Header.Get("X-Server-Token") != "server-secret" || r.URL.Query().Get("account_token") != "" {
				t.Errorf("hop server=%q account=%q", r.Header.Get("X-Server-Token"), r.URL.Query().Get("account_token"))
			}
			http.Redirect(w, r, "/servers/landed", http.StatusFound)
		case "/servers/landed":
			if r.URL.Query().Get("account_token") != "account-secret" || r.Header.Get("X-Server-Token") != "" {
				t.Errorf("landed server=%q account=%q", r.Header.Get("X-Server-Token"), r.URL.Query().Get("account_token"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		BaseURL: server.URL,
		` + serverField + `: "server-secret",
		` + accountField + `: "account-secret",
	}
	c := New(cfg, time.Second, 0)
	c.NoCache = true
	ctx := context.Background()
	if _, err := c.Get(ctx, "/servers/acct-1", nil); err != nil {
		t.Fatalf("unmatched redirect: %v", err)
	}
	if _, err := c.Get(ctx, "/messages/hop", nil); err != nil {
		t.Fatalf("scheme-changing redirect: %v", err)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "query_redirect_test.go"), []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/client", "-count=1", "-run", "TestQueryRedirectAccountToken")
}
