package generator

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	require.Contains(t, clientSrc, `if opScheme == "accountToken"`)
	require.Contains(t, clientSrc, `req.Header.Set("X-Account-Token", v)`)
	require.Contains(t, clientSrc, `req.Header.Set("X-Server-Token", authHeader)`)

	doctorSrc := readGenerated(t, outputDir, "internal", "cli", "doctor.go")
	require.Contains(t, doctorSrc, `report["auth_schemes"]`)
	require.Contains(t, doctorSrc, `"accountToken: configured"`)
	require.Contains(t, doctorSrc, `"serverToken: not configured"`)
	require.NotContains(t, doctorSrc, `recordAdditionalAuthEnv("`+accountEnv+`"`)

	behaviorTest := `package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

	neither := append(append([]string{}, base...), serverEnv+"=", accountEnv+"=", prefix+"_BASE_URL="+probe.URL)
	payload, err = runDoctorJSON(t, binaryPath, neither)
	require.NoError(t, err)
	envVars, _ := payload["env_vars"].(string)
	require.Contains(t, envVars, "ERROR missing required: "+serverEnv)
	_, err = runDoctorJSON(t, binaryPath, neither, "--fail-on", "error")
	require.Error(t, err)
}
