package generator

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestAuthSetupHintPOSIXProseUsesRuntimeCall(t *testing.T) {
	t.Parallel()

	apiKey := spec.AuthConfig{Type: "api_key", EnvVars: []string{"MYAPI_TOKEN"}}
	hint := authSetupHint(apiKey, "myapi")
	require.Equal(t, `Set your API key with: export MYAPI_TOKEN="your-token-here"`, hint.prose())
	require.Equal(t, `"Set your API key with: " + cliutil.EnvAssignHint(true, "MYAPI_TOKEN", "your-token-here")`, hint.expr())

	basicOne := spec.AuthConfig{Type: "api_key", Format: "Basic {token}", EnvVars: []string{"BASIC_TOKEN"}}
	hint = authSetupHint(basicOne, "basic")
	require.Equal(t, `Set the Basic credential with: export BASIC_TOKEN="your-token-here"`, hint.prose())

	basicMany := spec.AuthConfig{
		Type:    "api_key",
		Format:  "Basic {username}:{password}",
		EnvVars: []string{"BASICAUTH_USERNAME", "BASICAUTH_PASSWORD"},
	}
	hint = authSetupHint(basicMany, "basicauth")
	require.Equal(t, `Set Basic credentials with: export BASICAUTH_USERNAME="your-username" BASICAUTH_PASSWORD="your-password"`, hint.prose())
	require.Contains(t, hint.expr(), `cliutil.EnvAssignHint(true, "BASICAUTH_USERNAME", "your-username", "BASICAUTH_PASSWORD", "your-password")`)

	bearer := spec.AuthConfig{Type: "bearer_token", EnvVars: []string{"BEARER_TOKEN"}}
	hint = authSetupHint(bearer, "bearer")
	require.Equal(t, `Set it with: echo "$TOKEN" | bearer-pp-cli auth set-token or export BEARER_TOKEN="your-token-here"`, hint.prose())
	require.Contains(t, hint.expr(), `cliutil.EnvAssignHint(true, "BEARER_TOKEN", "your-token-here")`)

	custom := spec.AuthConfig{Type: "custom", EnvVars: []string{"CUSTOM_TOKEN"}}
	hint = authSetupHint(custom, "custom")
	require.Equal(t, `Set credentials with: export CUSTOM_TOKEN="your-token-here"`, hint.prose())

	orAuth := spec.AuthConfig{
		Type: "api_key",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "ALPHA_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true},
			{Name: "BETA_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true},
		},
	}
	hint = authSetupHint(orAuth, "orapi")
	require.Equal(t, `Set one of: export ALPHA_TOKEN="your-token-here" or export BETA_TOKEN="your-token-here"`, hint.prose())
	require.Equal(t, `"Set one of: " + cliutil.EnvAssignHint(true, "ALPHA_TOKEN", "your-token-here") + " or " + cliutil.EnvAssignHint(true, "BETA_TOKEN", "your-token-here")`, hint.expr())

	none := spec.AuthConfig{Type: "api_key"}
	hint = authSetupHint(none, "nokey")
	require.Equal(t, "Run 'nokey-pp-cli auth setup' for credential setup steps.", hint.prose())
	require.Equal(t, strconv.Quote(hint.prose()), hint.expr())
	require.NotContains(t, hint.expr(), "EnvAssignHint")

	cookie := spec.AuthConfig{Type: "cookie"}
	hint = authSetupHint(cookie, "cookiecli")
	require.Equal(t, "Run 'cookiecli-pp-cli auth login --chrome' to refresh browser-session credentials.", hint.prose())
	require.Equal(t, strconv.Quote(hint.prose()), hint.expr())

	setup := authPlaceholderCredentialSetup(apiKey, "myapi")
	require.Equal(t, `export MYAPI_TOKEN=<your-token-here> or echo "$TOKEN" | myapi-pp-cli auth set-token`, setup.prose())
	require.Equal(t, `cliutil.EnvAssignHint(false, "MYAPI_TOKEN", "<your-token-here>") + " or echo \"$TOKEN\" | myapi-pp-cli auth set-token"`, setup.expr())
}

func TestGeneratedEnvAssignHintFollowsGOOS(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("env-assign-hint")
	apiSpec.Auth.EnvVars = []string{"ENV_ASSIGN_HINT_API_KEY"}
	outputDir := filepath.Join(t.TempDir(), "env-assign-hint-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const cliTest = `package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"env-assign-hint-pp-cli/internal/cliutil"
)

func TestEnvAssignHintCommands(t *testing.T) {
	orig := cliutil.EnvAssignHintGOOS
	t.Cleanup(func() { cliutil.EnvAssignHintGOOS = orig })
	t.Setenv("ENV_ASSIGN_HINT_API_KEY", "")

	const posix = "export ENV_ASSIGN_HINT_API_KEY=\"your-token-here\""
	const windows = "$env:ENV_ASSIGN_HINT_API_KEY = \"your-token-here\""
	const sentence = "Set your API key with: "

	run := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		root := newRootCmd(&rootFlags{})
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SilenceUsage = true
		root.SetArgs(args)
		_ = root.Execute()
		return stdout.String()
	}

	assertPOSIX := func(name, got string) {
		t.Helper()
		if !strings.Contains(got, posix) {
			t.Fatalf("%s = %q, want %s", name, got, posix)
		}
		if strings.Contains(got, "$env:") {
			t.Fatalf("%s included PowerShell on POSIX: %q", name, got)
		}
	}
	assertWindows := func(name, got string) {
		t.Helper()
		if !strings.Contains(got, windows) {
			t.Fatalf("%s = %q, want %s", name, got, windows)
		}
		if strings.Contains(got, "export ENV_ASSIGN_HINT_API_KEY") {
			t.Fatalf("%s included export on Windows: %q", name, got)
		}
	}

	cliutil.EnvAssignHintGOOS = "linux"
	assertPOSIX("linux setup", run("auth", "setup"))
	assertPOSIX("linux status", run("auth", "status"))
	linuxHint := doctorAuthHint(t, run("doctor", "--json"))
	if linuxHint != sentence+posix {
		t.Fatalf("linux doctor auth_hint = %q", linuxHint)
	}
	err := classifyAPIError(io.Discard, fmt.Errorf("HTTP 401 unauthorized"), &rootFlags{})
	if err == nil || !strings.Contains(err.Error(), sentence+posix) {
		t.Fatalf("linux 401 = %v", err)
	}

	cliutil.EnvAssignHintGOOS = "darwin"
	assertPOSIX("darwin setup", run("auth", "setup"))
	darwinHint := doctorAuthHint(t, run("doctor", "--json"))
	if darwinHint != sentence+posix {
		t.Fatalf("darwin doctor auth_hint = %q", darwinHint)
	}

	cliutil.EnvAssignHintGOOS = "windows"
	assertWindows("windows setup", run("auth", "setup"))
	assertWindows("windows status", run("auth", "status"))
	windowsHint := doctorAuthHint(t, run("doctor", "--json"))
	if windowsHint != sentence+windows {
		t.Fatalf("windows doctor auth_hint = %q", windowsHint)
	}
	err = classifyAPIError(io.Discard, fmt.Errorf("HTTP 401 unauthorized"), &rootFlags{})
	if err == nil || !strings.Contains(err.Error(), sentence+windows) || strings.Contains(err.Error(), "export ENV_ASSIGN_HINT_API_KEY") {
		t.Fatalf("windows 401 = %v", err)
	}
}

func doctorAuthHint(t *testing.T, raw string) string {
	t.Helper()
	var payload any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload); err != nil {
		t.Fatalf("doctor --json: %v\n%s", err, raw)
	}
	hint, ok := findAuthHint(payload)
	if !ok {
		t.Fatalf("doctor --json missing auth_hint: %s", raw)
	}
	return hint
}

func findAuthHint(v any) (string, bool) {
	switch val := v.(type) {
	case map[string]any:
		if s, ok := val["auth_hint"].(string); ok && s != "" {
			return s, true
		}
		for _, child := range val {
			if s, ok := findAuthHint(child); ok {
				return s, true
			}
		}
	case []any:
		for _, child := range val {
			if s, ok := findAuthHint(child); ok {
				return s, true
			}
		}
	}
	return "", false
}
`
	const clientTest = `package client

import (
	"strings"
	"testing"

	"env-assign-hint-pp-cli/internal/cliutil"
)

func TestEnvAssignHintPlaceholder(t *testing.T) {
	orig := cliutil.EnvAssignHintGOOS
	t.Cleanup(func() { cliutil.EnvAssignHintGOOS = orig })

	const posix = "export ENV_ASSIGN_HINT_API_KEY=<your-token-here> or echo \"$TOKEN\" | env-assign-hint-pp-cli auth set-token"
	const windows = "$env:ENV_ASSIGN_HINT_API_KEY = \"<your-token-here>\""

	cliutil.EnvAssignHintGOOS = "linux"
	err := authPlaceholderCredentialError(nil)
	if err == nil || !strings.Contains(err.Error(), posix) {
		t.Fatalf("linux: %v", err)
	}
	cliutil.EnvAssignHintGOOS = "darwin"
	err = authPlaceholderCredentialError(nil)
	if err == nil || !strings.Contains(err.Error(), posix) {
		t.Fatalf("darwin: %v", err)
	}
	cliutil.EnvAssignHintGOOS = "windows"
	err = authPlaceholderCredentialError(nil)
	if err == nil || !strings.Contains(err.Error(), windows) || strings.Contains(err.Error(), "export ENV_ASSIGN_HINT_API_KEY") {
		t.Fatalf("windows: %v", err)
	}
	if !strings.Contains(err.Error(), "env-assign-hint-pp-cli auth set-token") {
		t.Fatalf("windows lost set-token command: %v", err)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "envassign_runtime_test.go"), []byte(cliTest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "envassign_runtime_test.go"), []byte(clientTest), 0o644))
	tools, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	require.Contains(t, string(tools), `cliutil.EnvAssignHint(true, "ENV_ASSIGN_HINT_API_KEY", "your-token-here")`)
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cliutil", "./internal/cli", "./internal/client", "-count=1", "-run", "TestEnvAssignHint")
}

func TestGeneratedClientCredentialsEnvAssignCompiles(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("env-assign-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "bearer_token",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://api.example.com/oauth/token",
		EnvVars:     []string{"ENV_ASSIGN_CC_CLIENT_ID", "ENV_ASSIGN_CC_CLIENT_SECRET"},
	}
	outputDir := filepath.Join(t.TempDir(), "env-assign-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	auth, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	body := string(auth)
	require.Contains(t, body, `cliutil.EnvAssignHint(true, "ENV_ASSIGN_CC_CLIENT_ID", "<client-id>")`)
	require.Contains(t, body, `cliutil.EnvAssignHint(true, "ENV_ASSIGN_CC_CLIENT_SECRET", "<client-secret>")`)
	requireGeneratedCompiles(t, outputDir)
}
