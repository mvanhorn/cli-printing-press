package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestOAuthTLSClientCredentials(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth-tls-cc")
	apiSpec.BaseURL = "https://api.example.com"
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://auth.example.com/token",
		EnvVars:     []string{"OAUTH_TLS_CC_CLIENT_ID", "OAUTH_TLS_CC_CLIENT_SECRET"},
	}
	outputDir := generateOAuthTLS(t, apiSpec)
	auth := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	require.Contains(t, auth, "flags.oauthHTTPClient(cfg)")
	require.NotContains(t, auth, "http.DefaultClient")
	require.Less(t, strings.Index(auth, "cliutil.IsVerifyEnv()"), strings.Index(auth, "flags.oauthHTTPClient(cfg)"))
	require.Less(t, strings.Index(auth, "flags.oauthHTTPClient(cfg)"), strings.Index(auth, "mintClientCredentialsToken("))
	writeTrustFile(t, outputDir, "internal/cli/oauth_tls_trust_test.go", oauthTLSCLISource(naming.CLI(apiSpec.Name), "", ccTrustBody))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestClientCredentialsUsesTrustClient", "-count=1", "-timeout", "180s")
}

func TestOAuthTLSDeviceCode(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth-tls-device")
	apiSpec.BaseURL = "https://api.example.com"
	apiSpec.Auth = spec.AuthConfig{
		Type:                   "oauth2",
		Header:                 "Authorization",
		Format:                 "Bearer {token}",
		OAuth2Grant:            spec.OAuth2GrantDeviceCode,
		DeviceAuthorizationURL: "https://auth.example.com/device",
		TokenURL:               "https://auth.example.com/token",
		Scopes:                 []string{"read"},
		EnvVars:                []string{"OAUTH_TLS_DEVICE_CLIENT_ID"},
	}
	outputDir := generateOAuthTLS(t, apiSpec)
	auth := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	oauthSrc := readGeneratedFile(t, outputDir, "internal", "oauth", "device.go")
	require.Equal(t, 3, strings.Count(auth, "flags.oauthHTTPClient(cfg)"))
	require.NotContains(t, oauthSrc, "http.DefaultClient")
	require.Contains(t, oauthSrc, "oauth http client is required")
	writeTrustFile(t, outputDir, "internal/cli/oauth_tls_trust_test.go", oauthTLSCLISource(naming.CLI(apiSpec.Name), "\t\""+naming.CLI(apiSpec.Name)+"/internal/oauth\"\n", deviceTrustBody))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestDeviceCodeUsesTrustClient", "-count=1", "-timeout", "180s")
}

func TestOAuthTLSAuthorizationCode(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth-tls-authcode")
	apiSpec.BaseURL = "https://api.example.com"
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		Format:           "Bearer {token}",
		OAuth2Grant:      spec.OAuth2GrantAuthorizationCode,
		AuthorizationURL: "https://auth.example.com/authorize",
		TokenURL:         "https://auth.example.com/token",
		EnvVars:          []string{"OAUTH_TLS_AUTHCODE_CLIENT_ID", "OAUTH_TLS_AUTHCODE_CLIENT_SECRET"},
	}
	outputDir := generateOAuthTLS(t, apiSpec)
	auth := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	require.Contains(t, auth, "exchangeAuthorizationCode(")
	require.NotContains(t, auth, "http.DefaultClient")
	require.NotContains(t, auth, "&http.Client{Timeout:")
	require.Less(t, strings.Index(auth, "flags.oauthHTTPClient(cfg)"), strings.Index(auth, "requiredInputWaived()"))
	require.Less(t, strings.Index(auth, "flags.oauthHTTPClient(cfg)"), strings.Index(auth, "Opening browser"))
	mod := naming.CLI(apiSpec.Name)
	writeTrustFile(t, outputDir, "internal/cli/oauth_tls_trust_test.go", oauthTLSCLISource(mod, "\t\"net/url\"\n\t\""+mod+"/internal/config\"\n", authCodeTrustBody))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestAuthorizationCodeUsesTrustClient", "-count=1", "-timeout", "180s")
}

func TestOAuthTLSComposedProbe(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth-tls-composed")
	apiSpec.BaseURL = "https://acme.example.com"
	apiSpec.Auth = spec.AuthConfig{
		Type:         "composed",
		Header:       "Authorization",
		Format:       "Bearer {session}",
		CookieDomain: "example.com",
		Cookies:      []string{"session"},
	}
	outputDir := generateOAuthTLS(t, apiSpec)
	auth := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	require.Contains(t, auth, "preservedConfigErr(err)")
	require.Contains(t, auth, "session has expired")
	writeTrustFile(t, outputDir, "internal/cli/oauth_tls_trust_test.go", oauthTLSCLISource(naming.CLI(apiSpec.Name), "", composedTrustBody))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestComposedProbeKeepsConfigError", "-count=1", "-timeout", "180s")
}

func TestOAuthTLSBrowserSession(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth-tls-browser")
	apiSpec.BaseURL = "https://acme.example.com"
	apiSpec.Auth = spec.AuthConfig{
		Type:                           "cookie",
		Header:                         "Cookie",
		In:                             "cookie",
		CookieDomain:                   ".example.com",
		Cookies:                        []string{"session"},
		EnvVars:                        []string{"OAUTH_TLS_BROWSER_SESSION"},
		RequiresBrowserSession:         true,
		BrowserSessionValidationPath:   "/session",
		BrowserSessionValidationMethod: "GET",
	}
	outputDir := generateOAuthTLS(t, apiSpec)
	auth := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	require.GreaterOrEqual(t, strings.Count(auth, "preservedConfigErr(err)"), 3)
	require.Contains(t, auth, "browser session validation failed")
	writeTrustFile(t, outputDir, "internal/cli/oauth_tls_trust_test.go", oauthTLSCLISource(naming.CLI(apiSpec.Name), "", browserTrustBody))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestBrowserSessionKeepsConfigError", "-count=1", "-timeout", "180s")
}

func generateOAuthTLS(t *testing.T, apiSpec *spec.APISpec) string {
	t.Helper()
	require.NoError(t, apiSpec.Validate())
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	return outputDir
}

func oauthTLSCLISource(mod, extraImport, body string) string {
	return strings.NewReplacer(
		"__MOD__", mod,
		"__EXTRA_IMPORT__", extraImport,
		"__BODY__", body,
	).Replace(oauthTLSCLITemplate)
}

const oauthTLSCLITemplate = `package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
__EXTRA_IMPORT__
	"__MOD__/internal/client"
)

type oauthTLSCapture struct {
	mu       sync.Mutex
	hits     int
	dials    int
	lastPath string
	caPath   string
}

func (c *oauthTLSCapture) snapshot() (hits, dials int, path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.dials, c.lastPath
}

func startOAuthTLS(t *testing.T) *oauthTLSCapture {
	t.Helper()
	cap := &oauthTLSCapture{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.mu.Lock()
		cap.hits++
		cap.lastPath = r.URL.Path
		cap.mu.Unlock()
		if r.Method == http.MethodHead || r.URL.Path == "/session" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body := ` + "`" + `{"access_token":"minted","refresh_token":"refresh","expires_in":3600,"token_type":"Bearer"}` + "`" + `
		if strings.Contains(r.URL.Path, "/device") {
			body = ` + "`" + `{"device_code":"device-code","user_code":"ABCD-EFGH","verification_uri":"https://example.com/device","expires_in":600,"interval":1}` + "`" + `
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	cap.caPath = caPath
	base := http.DefaultTransport.(*http.Transport).Clone()
	// The httptest leaf matches example.com and *.example.com. A name outside
	// that wildcard fails only when --tls-server-name is actually applied.
	base.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		cap.mu.Lock()
		cap.dials++
		cap.mu.Unlock()
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	prev := http.DefaultTransport
	http.DefaultTransport = base
	t.Cleanup(func() { http.DefaultTransport = prev })
	return cap
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func execRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := RootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	return execRoot(t, args...)
}

__BODY__
`

const ccTrustBody = `
func TestClientCredentialsUsesTrustClient(t *testing.T) {
	cap := startOAuthTLS(t)
	configPath := writeConfig(t, "")
	login := []string{"auth", "login", "--client-id", "id", "--client-secret", "secret"}

	missing := filepath.Join(t.TempDir(), "missing.pem")
	out, err := runRoot(t, append([]string{"--config", configPath, "--ca-cert", missing}, login...)...)
	hits, dials, _ := cap.snapshot()
	if ExitCode(err) != 10 || !errors.Is(err, client.ErrTLSConfig) || !strings.Contains(err.Error(), missing) || hits != 0 || dials != 0 {
		t.Fatalf("missing bundle exit=%d err=%v hits=%d dials=%d out=%s", ExitCode(err), err, hits, dials, out)
	}

	empty := filepath.Join(t.TempDir(), "empty.pem")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = runRoot(t, append([]string{"--config", configPath, "--ca-cert", empty}, login...)...)
	hits, dials, _ = cap.snapshot()
	if ExitCode(err) != 10 || !errors.Is(err, client.ErrTLSConfig) || !strings.Contains(err.Error(), empty) || hits != 0 || dials != 0 {
		t.Fatalf("empty bundle exit=%d err=%v hits=%d dials=%d out=%s", ExitCode(err), err, hits, dials, out)
	}

	out, err = runRoot(t, append([]string{"--config", configPath, "--ca-cert", cap.caPath, "--tls-server-name", "not-a-match.example.net"}, login...)...)
	hits, dials, _ = cap.snapshot()
	if err == nil || ExitCode(err) == 10 || hits != 0 || dials == 0 {
		t.Fatalf("wrong server name exit=%d err=%v hits=%d dials=%d out=%s", ExitCode(err), err, hits, dials, out)
	}

	out, err = runRoot(t, append([]string{"--config", configPath, "--ca-cert", cap.caPath}, login...)...)
	hits, dials, path := cap.snapshot()
	if err != nil || hits != 1 || path != "/token" {
		t.Fatalf("default server name err=%v hits=%d dials=%d path=%s out=%s", err, hits, dials, path, out)
	}

	out, err = runRoot(t, append([]string{"--config", configPath, "--ca-cert", cap.caPath, "--tls-server-name", "example.com"}, login...)...)
	hits, _, path = cap.snapshot()
	if err != nil || hits != 2 || path != "/token" {
		t.Fatalf("example.com server name err=%v hits=%d path=%s out=%s", err, hits, path, out)
	}
}
`

const deviceTrustBody = `
func TestDeviceCodeUsesTrustClient(t *testing.T) {
	if _, err := oauth.RequestDeviceCode(context.Background(), nil, "https://auth.example.com/device", "id", nil); err == nil || !strings.Contains(err.Error(), "oauth http client is required") {
		t.Fatalf("nil device client: %v", err)
	}
	if _, err := oauth.PollDeviceToken(context.Background(), nil, "https://auth.example.com/token", "id", "code", 0); err == nil || !strings.Contains(err.Error(), "oauth http client is required") {
		t.Fatalf("nil poll client: %v", err)
	}
	if _, err := oauth.RefreshToken(context.Background(), nil, "https://auth.example.com/token", "id", "", "refresh"); err == nil || !strings.Contains(err.Error(), "oauth http client is required") {
		t.Fatalf("nil refresh client: %v", err)
	}

	cap := startOAuthTLS(t)
	missing := filepath.Join(t.TempDir(), "missing.pem")
	configPath := writeConfig(t, "")
	out, err := runRoot(t, "--config", configPath, "--ca-cert", missing, "auth", "login", "--device-code", "--poll=false", "--client-id", "id")
	hits, dials, _ := cap.snapshot()
	if ExitCode(err) != 10 || !errors.Is(err, client.ErrTLSConfig) || !strings.Contains(err.Error(), missing) || hits != 0 || dials != 0 {
		t.Fatalf("login missing bundle exit=%d err=%v hits=%d dials=%d out=%s", ExitCode(err), err, hits, dials, out)
	}

	pending := configPath + ".device-code.json"
	state := ` + "`" + `{"device_code":"device-code","client_id":"id","token_url":"https://auth.example.com/token","expires_at":"2099-01-01T00:00:00Z"}` + "`" + `
	if err := os.WriteFile(pending, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = runRoot(t, "--config", configPath, "--ca-cert", missing, "auth", "poll", "--timeout", "5s")
	hits, dials, _ = cap.snapshot()
	if ExitCode(err) != 10 || !errors.Is(err, client.ErrTLSConfig) || !strings.Contains(err.Error(), missing) || hits != 0 || dials != 0 {
		t.Fatalf("poll missing bundle exit=%d err=%v hits=%d dials=%d out=%s", ExitCode(err), err, hits, dials, out)
	}

	refreshPath := writeConfig(t, "client_id = \"id\"\nrefresh_token = \"refresh-token\"\n")
	out, err = runRoot(t, "--config", refreshPath, "--ca-cert", missing, "auth", "refresh")
	hits, dials, _ = cap.snapshot()
	if ExitCode(err) != 10 || !errors.Is(err, client.ErrTLSConfig) || !strings.Contains(err.Error(), missing) || hits != 0 || dials != 0 {
		t.Fatalf("refresh missing bundle exit=%d err=%v hits=%d dials=%d out=%s", ExitCode(err), err, hits, dials, out)
	}

	out, err = runRoot(t, "--config", configPath, "--ca-cert", cap.caPath, "--tls-server-name", "not-a-match.example.net", "auth", "login", "--device-code", "--poll=false", "--client-id", "id")
	hits, dials, _ = cap.snapshot()
	if err == nil || ExitCode(err) == 10 || hits != 0 || dials == 0 {
		t.Fatalf("wrong server name exit=%d err=%v hits=%d dials=%d out=%s", ExitCode(err), err, hits, dials, out)
	}

	out, err = runRoot(t, "--config", configPath, "--ca-cert", cap.caPath, "--tls-server-name", "example.com", "auth", "login", "--device-code", "--poll=false", "--client-id", "id")
	hits, _, path := cap.snapshot()
	if err != nil || path != "/device" || hits != 1 {
		t.Fatalf("device login err=%v hits=%d path=%s out=%s", err, hits, path, out)
	}
	out, err = runRoot(t, "--config", configPath, "--ca-cert", cap.caPath, "--tls-server-name", "example.com", "auth", "poll", "--timeout", "5s")
	hits, _, path = cap.snapshot()
	if err != nil || path != "/token" || hits != 2 {
		t.Fatalf("device poll err=%v hits=%d path=%s out=%s", err, hits, path, out)
	}
	out, err = runRoot(t, "--config", refreshPath, "--ca-cert", cap.caPath, "--tls-server-name", "example.com", "auth", "refresh")
	hits, _, path = cap.snapshot()
	if err != nil || path != "/token" || hits != 3 {
		t.Fatalf("device refresh err=%v hits=%d path=%s out=%s", err, hits, path, out)
	}
}
`

const authCodeTrustBody = `
func TestAuthorizationCodeUsesTrustClient(t *testing.T) {
	configPath := writeConfig(t, "")
	missing := filepath.Join(t.TempDir(), "missing.pem")
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	out, stderr, err := execRootStderr(t, "--config", configPath, "--ca-cert", missing, "auth", "login", "--client-id", "id")
	if ExitCode(err) != 10 || !errors.Is(err, client.ErrTLSConfig) || !strings.Contains(err.Error(), missing) || strings.Contains(out+stderr, "Opening browser") {
		t.Fatalf("missing bundle exit=%d err=%v out=%s stderr=%s", ExitCode(err), err, out, stderr)
	}

	t.Setenv("PRINTING_PRESS_VERIFY", "1")
	out, stderr, err = execRootStderr(t, "--config", configPath, "--ca-cert", missing, "auth", "login")
	if ExitCode(err) != 10 || !strings.Contains(err.Error(), missing) || strings.Contains(out, "dry_run") || strings.Contains(out+stderr, "Opening browser") {
		t.Fatalf("verify missing bundle exit=%d err=%v out=%s stderr=%s", ExitCode(err), err, out, stderr)
	}

	out, _, err = execRootStderr(t, "--config", configPath, "auth", "login")
	if err != nil || !strings.Contains(out, "dry_run") {
		t.Fatalf("empty CA verify err=%v out=%s", err, out)
	}

	t.Setenv("PRINTING_PRESS_VERIFY", "")
	cap := startOAuthTLS(t)
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	flags := &rootFlags{caCert: cap.caPath, tlsServerName: "not-a-match.example.net"}
	hc, err := flags.oauthHTTPClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	params := tokenParams()
	if _, err = exchangeAuthorizationCode(context.Background(), hc, "https://auth.example.com/token", params); err == nil {
		t.Fatal("wrong server name succeeded")
	}
	hits, dials, _ := cap.snapshot()
	if hits != 0 || dials == 0 {
		t.Fatalf("wrong server name hits=%d dials=%d", hits, dials)
	}

	cfg.TLSServerName = ""
	flags.tlsServerName = ""
	hc, err = flags.oauthHTTPClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := exchangeAuthorizationCode(context.Background(), hc, "https://auth.example.com/token", params)
	hits, _, path := cap.snapshot()
	if err != nil || tok == nil || tok.AccessToken != "minted" || hits != 1 || path != "/token" {
		t.Fatalf("default name err=%v tok=%v hits=%d path=%s", err, tok, hits, path)
	}

	cfg.TLSServerName = ""
	flags.tlsServerName = "example.com"
	hc, err = flags.oauthHTTPClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tok, err = exchangeAuthorizationCode(context.Background(), hc, "https://auth.example.com/token", params)
	hits, _, path = cap.snapshot()
	if err != nil || tok == nil || tok.AccessToken != "minted" || hits != 2 || path != "/token" {
		t.Fatalf("example.com err=%v tok=%v hits=%d path=%s", err, tok, hits, path)
	}
}

func tokenParams() url.Values {
	return url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {"abc"},
		"redirect_uri": {"http://127.0.0.1:8085/callback"},
		"client_id":    {"id"},
	}
}

func execRootStderr(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	out, runErr := execRoot(t, args...)
	w.Close()
	os.Stderr = old
	buf, _ := io.ReadAll(r)
	r.Close()
	return out, string(buf), runErr
}
`

const composedTrustBody = `
func TestComposedProbeKeepsConfigError(t *testing.T) {
	cap := startOAuthTLS(t)
	cookies := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(cookies, []byte("session=opaque-session"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeConfig(t, "")
	missing := filepath.Join(t.TempDir(), "missing.pem")
	out, err := runRoot(t, "--config", configPath, "--ca-cert", missing, "auth", "login", "--cookies-file", cookies)
	hits, dials, _ := cap.snapshot()
	if ExitCode(err) != 10 || !errors.Is(err, client.ErrTLSConfig) || !strings.Contains(err.Error(), missing) || strings.Contains(out, "session has expired") || strings.Contains(err.Error(), "session expired") || hits != 0 || dials != 0 {
		t.Fatalf("missing bundle exit=%d err=%v hits=%d dials=%d out=%s", ExitCode(err), err, hits, dials, out)
	}

	out, err = runRoot(t, "--config", configPath, "--ca-cert", cap.caPath, "auth", "login", "--cookies-file", cookies)
	hits, _, _ = cap.snapshot()
	if ExitCode(err) != 4 || !strings.Contains(out, "session has expired") || !strings.Contains(err.Error(), "session expired") || hits != 1 {
		t.Fatalf("expired session exit=%d err=%v hits=%d out=%s", ExitCode(err), err, hits, out)
	}
}
`

const browserTrustBody = `
func TestBrowserSessionKeepsConfigError(t *testing.T) {
	cap := startOAuthTLS(t)
	cookies := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(cookies, []byte("session=opaque-session"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeConfig(t, "")
	missing := filepath.Join(t.TempDir(), "missing.pem")
	out, err := runRoot(t, "--config", configPath, "--ca-cert", missing, "auth", "login", "--cookies-file", cookies)
	hits, dials, _ := cap.snapshot()
	if ExitCode(err) != 10 || !errors.Is(err, client.ErrTLSConfig) || !strings.Contains(err.Error(), missing) || strings.Contains(err.Error(), "browser session validation failed") || hits != 0 || dials != 0 {
		t.Fatalf("missing bundle exit=%d err=%v hits=%d dials=%d out=%s", ExitCode(err), err, hits, dials, out)
	}

	out, err = runRoot(t, "--config", configPath, "--ca-cert", cap.caPath, "auth", "login", "--cookies-file", cookies)
	hits, _, path := cap.snapshot()
	if ExitCode(err) != 4 || !strings.Contains(err.Error(), "browser session validation failed") || hits != 1 || path != "/session" {
		t.Fatalf("validation failure exit=%d err=%v hits=%d path=%s out=%s", ExitCode(err), err, hits, path, out)
	}
}
`
