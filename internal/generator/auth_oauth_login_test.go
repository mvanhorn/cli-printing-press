package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestOAuthLoginTopLevelCommandAndCredentialFallback(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth-login-prompts")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		Format:           "Bearer {token}",
		OAuth2Grant:      spec.OAuth2GrantAuthorizationCode,
		AuthorizationURL: "https://accounts.example.com/oauth/authorize",
		TokenURL:         "https://accounts.example.com/oauth/token",
		KeyURL:           "https://console.example.com/oauth",
	}

	outputDir := filepath.Join(t.TempDir(), "oauth-login-prompts-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	root := string(rootSrc)
	require.Contains(t, root, "rootCmd.AddCommand(newAuthCmd(flags))")
	require.Contains(t, root, "rootCmd.AddCommand(newAuthLoginCmd(flags))")
	require.Contains(t, sortedKeys(New(apiSpec, outputDir).activeFrameworkCobraUseNames()), "login")

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	auth := string(authSrc)
	require.Contains(t, auth, "func runOAuthLogin(")
	require.Contains(t, auth, "return runOAuthLogin(cmd, flags, clientID, clientSecret, port)")
	require.Contains(t, auth, "clientID = cfg.ClientID")
	require.Contains(t, auth, "clientSecret = cfg.ClientSecret")
	require.Contains(t, auth, "promptOAuthCredential(cmd, reader, \"OAuth2 Client ID\")")
	require.Contains(t, auth, "promptOAuthCredential(cmd, reader, \"OAuth2 Client Secret (press Enter if not required)\")")
	require.Contains(t, auth, `"mcp:hidden": "true"`)
	require.Contains(t, auth, "flags.noInput")
	require.Contains(t, auth, `fmt.Fprintln(w, "  oauth-login-prompts-pp-cli login")`)
	require.Contains(t, auth, `Run 'oauth-login-prompts-pp-cli login' to authenticate.`)
	require.Contains(t, auth, `Run 'oauth-login-prompts-pp-cli login' to re-authenticate.`)
	require.NotContains(t, auth, "return fmt.Errorf(\"--client-id is required\")")

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	require.Contains(t, string(doctorSrc), `report["auth_hint"] = "oauth-login-prompts-pp-cli login"`)

	binPath := filepath.Join(outputDir, "oauth-login-prompts-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binPath, "./cmd/oauth-login-prompts-pp-cli")
	helpOut, err := exec.Command(binPath, "login", "--help").CombinedOutput()
	require.NoError(t, err, "top-level login --help failed: %s", string(helpOut))
	require.Contains(t, string(helpOut), "Authenticate via OAuth2")

	configPath := filepath.Join(t.TempDir(), "config.toml")
	noInputOut, err := exec.Command(binPath, "--config", configPath, "--no-input", "login").CombinedOutput()
	require.Error(t, err, "top-level login --no-input should fail when no credentials are available")
	require.Contains(t, string(noInputOut), "OAUTH_LOGIN_PROMPTS_CLIENT_ID")

	dryRunOut, err := exec.Command(binPath, "--config", configPath, "--no-input", "--dry-run", "login").CombinedOutput()
	require.NoError(t, err, "top-level login --dry-run should not require credentials: %s", string(dryRunOut))
	require.JSONEq(t, `{"status":"dry_run","action":"auth login","would":"start OAuth2 authorization-code flow (PKCE without client secret), open browser, capture loopback callback"}`, string(dryRunOut))

	require.NoError(t, os.WriteFile(configPath, []byte("client_id = \"saved-id\"\n"), 0o600))
	configuredProbe := exec.Command(binPath, "--config", configPath, "--no-input", "--dry-run", "login")
	configuredProbe.Env = append(os.Environ(), "PRINTING_PRESS_VERIFY=1")
	configuredProbeOut, err := configuredProbe.CombinedOutput()
	require.NoError(t, err, "configured OAuth login probe failed: %s", string(configuredProbeOut))
	require.Contains(t, string(configuredProbeOut), "would launch: https://accounts.example.com/oauth/authorize?")
	require.Contains(t, string(configuredProbeOut), "client_id=saved-id")
	require.Contains(t, string(configuredProbeOut), "code_challenge_method=S256")

	const runtimeTest = `package cli

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"

	"oauth-login-prompts-pp-cli/internal/config"
	"github.com/spf13/cobra"
)

func TestResolveOAuthCredentialsUsesSavedConfig(t *testing.T) {
	cfg := &config.Config{ClientID: "saved-id", ClientSecret: "saved-secret"}
	clientID, clientSecret, err := resolveOAuthCredentials(&cobra.Command{}, &rootFlags{noInput: true}, cfg, "", "")
	if err != nil {
		t.Fatalf("resolveOAuthCredentials() error = %v", err)
	}
	if clientID != "saved-id" || clientSecret != "saved-secret" {
		t.Fatalf("credentials = %q/%q, want saved-id/saved-secret", clientID, clientSecret)
	}
}

func TestResolveOAuthCredentialsExplicitValuesWinOverSavedConfig(t *testing.T) {
	cfg := &config.Config{ClientID: "saved-id", ClientSecret: "saved-secret"}
	clientID, clientSecret, err := resolveOAuthCredentials(&cobra.Command{}, &rootFlags{noInput: true}, cfg, "flag-id", "flag-secret")
	if err != nil {
		t.Fatalf("resolveOAuthCredentials() error = %v", err)
	}
	if clientID != "flag-id" || clientSecret != "flag-secret" {
		t.Fatalf("credentials = %q/%q, want flag-id/flag-secret", clientID, clientSecret)
	}

	clientID, clientSecret, err = resolveOAuthCredentials(&cobra.Command{}, &rootFlags{noInput: true}, cfg, "new-id", "")
	if err != nil {
		t.Fatalf("resolveOAuthCredentials() new ID error = %v", err)
	}
	if clientID != "new-id" || clientSecret != "" {
		t.Fatalf("new-ID credentials = %q/%q, want new-id/empty secret", clientID, clientSecret)
	}
}

func TestResolveOAuthCredentialsPromptsForMissingClientID(t *testing.T) {
	var stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader("typed-id\ntyped-secret\n"))

	clientID, clientSecret, err := resolveOAuthCredentials(cmd, &rootFlags{}, &config.Config{}, "", "")
	if err != nil {
		t.Fatalf("resolveOAuthCredentials() error = %v", err)
	}
	if clientID != "typed-id" {
		t.Fatalf("clientID = %q, want typed-id", clientID)
	}
	if clientSecret != "typed-secret" {
		t.Fatalf("clientSecret = %q, want typed-secret", clientSecret)
	}
	if got := stderr.String(); !strings.Contains(got, "Create OAuth credentials at: https://console.example.com/oauth") || !strings.Contains(got, "OAuth2 Client ID:") || !strings.Contains(got, "OAuth2 Client Secret (press Enter if not required):") {
		t.Fatalf("stderr = %q, want setup URL and credential prompts", got)
	}
}

func TestResolveOAuthCredentialsNoInputRequiresClientID(t *testing.T) {
	_, _, err := resolveOAuthCredentials(&cobra.Command{}, &rootFlags{noInput: true}, &config.Config{}, "", "")
	if err == nil {
		t.Fatalf("resolveOAuthCredentials() error = nil, want missing client ID error")
	}
	if !strings.Contains(err.Error(), "OAUTH_LOGIN_PROMPTS_CLIENT_ID") {
		t.Fatalf("error = %q, want env-var hint", err)
	}
}

type partialReadErrorReader struct{}

func (partialReadErrorReader) Read(p []byte) (int, error) {
	copy(p, "partial")
	return len("partial"), errors.New("short read")
}

func TestPromptOAuthCredentialRejectsPartialReadError(t *testing.T) {
	var stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&stderr)
	reader := bufio.NewReader(partialReadErrorReader{})

	_, err := promptOAuthCredential(cmd, reader, "OAuth2 Client ID")
	if err == nil {
		t.Fatalf("promptOAuthCredential() error = nil, want partial-read error")
	}
	if !strings.Contains(err.Error(), "reading oauth2 client id: short read") {
		t.Fatalf("error = %q, want partial-read context", err)
	}
}
	`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "oauth_credentials_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "Test(ResolveOAuthCredentials|PromptOAuthCredential)")

	const mcpRuntimeTest = `package cobratree

import (
	"testing"

	"oauth-login-prompts-pp-cli/internal/cli"
	"github.com/spf13/cobra"
)

func TestTopLevelOAuthLoginIsHiddenFromMCP(t *testing.T) {
	root := cli.RootCmd()
	login, _, err := root.Find([]string{"login"})
	if err != nil {
		t.Fatalf("RootCmd().Find(login) error = %v", err)
	}
	if login == nil || login.Name() != "login" {
		t.Fatalf("top-level login command not found: %#v", login)
	}
	if got := classify(login); got != commandHidden {
		t.Fatalf("classify(top-level oauth login) = %v, want commandHidden", got)
	}
	plainLogin := &cobra.Command{
		Use: "login",
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	}
	if got := classify(plainLogin); got != commandNovel {
		t.Fatalf("classify(unannotated login) = %v, want commandNovel", got)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "cobratree", "oauth_login_hidden_test.go"), []byte(mcpRuntimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/mcp/cobratree", "-run", "TestTopLevelOAuthLoginIsHiddenFromMCP")

	if strings.Contains(auth, "auth login --client-id <id> --client-secret <secret>") {
		t.Fatalf("setup hint still points users at the flag-heavy nested login form")
	}
}

func TestOAuthClientCredentialsUsesNestedAuthLoginHints(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth-client-credentials-login")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://accounts.example.com/oauth/token",
		KeyURL:      "https://console.example.com/oauth",
		EnvVars:     []string{"OAUTH_CC_CLIENT_ID", "OAUTH_CC_CLIENT_SECRET"},
	}

	outputDir := filepath.Join(t.TempDir(), "oauth-client-credentials-login-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	require.NotContains(t, string(rootSrc), "rootCmd.AddCommand(newAuthLoginCmd(flags))")
	require.NotContains(t, sortedKeys(New(apiSpec, outputDir).activeFrameworkCobraUseNames()), "login")

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	auth := string(authSrc)
	require.Contains(t, auth, "Mint an OAuth2 bearer token via the client_credentials grant")
	require.Contains(t, auth, `oauth-client-credentials-login-pp-cli auth login`)
	require.NotContains(t, auth, `oauth-client-credentials-login-pp-cli login`)

	const cliRuntimeTest = `package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMintClientCredentialsTokenUsesBasicAuthAndUserAgent(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "client-id" || pass != "client-secret" {
			t.Fatalf("BasicAuth() = (%q, %q, %v), want client-id/client-secret/true", user, pass, ok)
		}
		if got := r.Header.Get("User-Agent"); calls == 1 && got != "oauth-client-credentials-login-pp-cli/0.1.0" {
			t.Fatalf("default User-Agent = %q", got)
		} else if calls == 2 && got != "custom-agent/2.0" {
			t.Fatalf("override User-Agent = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "client_credentials" {
			t.Fatalf("grant_type = %q", got)
		}
		if got := r.Form.Get("client_id"); got != "" {
			t.Fatalf("client_id leaked into form body: %q", got)
		}
		if got := r.Form.Get("client_secret"); got != "" {
			t.Fatalf("client_secret leaked into form body: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`{\"access_token\":\"minted\",\"expires_in\":3600}`" + `))
	}))
	defer server.Close()

	tok, err := mintClientCredentialsToken(server.Client(), server.URL, "client-id", "client-secret")
	if err != nil {
		t.Fatalf("mintClientCredentialsToken() error = %v", err)
	}
	if tok.AccessToken != "minted" {
		t.Fatalf("AccessToken = %q", tok.AccessToken)
	}

	t.Setenv("OAUTH_CLIENT_CREDENTIALS_LOGIN_USER_AGENT", "custom-agent/2.0")
	if _, err := mintClientCredentialsToken(server.Client(), server.URL, "client-id", "client-secret"); err != nil {
		t.Fatalf("mintClientCredentialsToken() with UA override error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestMintClientCredentialsTokenIncludesScopeOnlyWhenSet(t *testing.T) {
	t.Setenv("OAUTH_CLIENT_CREDENTIALS_LOGIN_OAUTH_SCOPE", "read write")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		if got := strings.Join(r.Form["scope"], ","); got != "read write" {
			t.Fatalf("scope = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`{\"access_token\":\"scoped\"}`" + `))
	}))
	defer server.Close()
	if _, err := mintClientCredentialsToken(server.Client(), server.URL, "client-id", "client-secret"); err != nil {
		t.Fatalf("mintClientCredentialsToken() error = %v", err)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "client_credentials_mint_test.go"), []byte(cliRuntimeTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestMintClientCredentialsToken", "-count=1")
}

func TestOAuthLoginTopLevelCommandForBearerTokenAuthCodeSpec(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("bearer-auth-code-login")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "bearer_token",
		Header:           "Authorization",
		Format:           "Bearer {token}",
		AuthorizationURL: "https://accounts.example.com/oauth/authorize",
		TokenURL:         "https://accounts.example.com/oauth/token",
		KeyURL:           "https://console.example.com/oauth",
	}

	outputDir := filepath.Join(t.TempDir(), "bearer-auth-code-login-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	require.Contains(t, string(rootSrc), "rootCmd.AddCommand(newAuthLoginCmd(flags))")

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	require.Contains(t, string(doctorSrc), `report["auth_hint"] = "bearer-auth-code-login-pp-cli login"`)
}
