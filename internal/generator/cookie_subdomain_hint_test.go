package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const hostOnlySubdomainHintRuntime = `package cli

import (
	"strings"
	"testing"
)

func TestHostOnlySubdomainRecaptureHint(t *testing.T) {
	unnamed := hostOnlySubdomainRecapture("concursolutions.com", "")
	if !strings.Contains(unnamed, "add --include-subdomains") {
		t.Fatalf("unnamed = %q", unnamed)
	}
	if strings.Contains(unnamed, "press-auth login") {
		t.Fatalf("unnamed hint replaced the default capture command: %q", unnamed)
	}

	named := hostOnlySubdomainRecapture("concursolutions.com", "_csrf")
	wantCmd := "press-auth login concursolutions.com --login-url <login-url> --include-subdomains --force"
	if !strings.Contains(named, wantCmd) || !strings.Contains(named, "\"_csrf\"") {
		t.Fatalf("named = %q", named)
	}
	if strings.Contains(unnamed, "--force") {
		t.Fatalf("unnamed hint required --force before a capture exists: %q", unnamed)
	}
	if named == unnamed {
		t.Fatal("named and unnamed hints collapsed")
	}
}
`

const composedLiveMissRuntime = `package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestComposedLiveMissPrintsSubdomainRecapture(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fakepress\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const src = ` + "`" + `package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "cookies" {
		os.Stderr.WriteString("unexpected args\n")
		os.Exit(2)
	}
	fmt.Print("session=ok")
}
` + "`" + `
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	binName := "press-auth"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	build := exec.Command("go", "build", "-o", binName, ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake press-auth: %v\n%s", err, out)
	}

	t.Setenv("PATH", dir)
	// extractLiveCookies lists DevTools targets over HTTP on localhost:9222
	// and :9229. Clearing PATH hides browser-use and agent-browser, but those
	// probes still reach a debugging browser and can return cookies.
	blockDevToolsProbes(t)

	cmd := newAuthLoginCmd(&rootFlags{})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--chrome"})
	err := cmd.Execute()
	out := buf.String()
	if err == nil {
		t.Fatalf("expected login to fail, output:\n%s", out)
	}
	combined := err.Error() + "\n" + out
	if !strings.Contains(combined, "session cookies not accessible") {
		t.Fatalf("live-miss path not taken: %v\n%s", err, out)
	}
	want := "press-auth login example.com --login-url <login-url> --include-subdomains --force"
	if !strings.Contains(combined, want) || !strings.Contains(combined, "\"_csrf\"") {
		t.Fatalf("live-miss path omitted recapture\nerror: %v\noutput:\n%s", err, out)
	}
}

func blockDevToolsProbes(t *testing.T) {
	t.Helper()
	prev := http.DefaultTransport
	http.DefaultTransport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() == "localhost" && (r.URL.Port() == "9222" || r.URL.Port() == "9229") {
			return nil, fmt.Errorf("blocked DevTools probe %s", r.URL.Host)
		}
		return prev.RoundTrip(r)
	})
	t.Cleanup(func() { http.DefaultTransport = prev })
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
`

func TestGeneratedHostOnlySubdomainHint(t *testing.T) {
	t.Parallel()

	t.Run("cookie allowlist", func(t *testing.T) {
		t.Parallel()
		outputDir := generateCookieHintCLI(t, spec.AuthConfig{
			Type:         "cookie",
			Header:       "Cookie",
			In:           "cookie",
			CookieDomain: ".example.com",
			Cookies:      []string{"session", "_csrf"},
			EnvVars:      []string{"HINTCOOKIE_COOKIES"},
		}, "hintcookie")
		auth := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
		assertHostOnlySubdomainHint(t, auth)
		assert.Contains(t, auth, "hostOnlySubdomainRecapture(domain, name)")
		requireGeneratedCompiles(t, outputDir)
		require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "host_only_subdomain_hint_test.go"), []byte(hostOnlySubdomainHintRuntime), 0o644))
		runGoCommand(t, outputDir, "test", "./internal/cli", "-count=1", "-run", "TestHostOnlySubdomainRecaptureHint")
	})

	t.Run("composed", func(t *testing.T) {
		t.Parallel()
		outputDir := generateCookieHintCLI(t, spec.AuthConfig{
			Type:         "composed",
			Header:       "Authorization",
			Format:       "Bearer {session}",
			CookieDomain: "example.com",
			Cookies:      []string{"session", "_csrf"},
		}, "hintcomposed")
		auth := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
		assertHostOnlySubdomainHint(t, auth)
		assert.Contains(t, auth, `cookie %q not found for %s; %s`)
		assert.Contains(t, auth, `session cookies not accessible for %s; %s`)
		requireGeneratedCompiles(t, outputDir)
		require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "host_only_subdomain_hint_test.go"), []byte(hostOnlySubdomainHintRuntime), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "composed_live_miss_test.go"), []byte(composedLiveMissRuntime), 0o644))
		runGoCommand(t, outputDir, "test", "./internal/cli", "-count=1", "-run", "TestHostOnlySubdomainRecaptureHint|TestComposedLiveMissPrintsSubdomainRecapture")
	})
}

func generateCookieHintCLI(t *testing.T, auth spec.AuthConfig, name string) string {
	t.Helper()
	apiSpec := &spec.APISpec{
		Name:    name,
		Version: "0.1.0",
		BaseURL: "https://example.com",
		Auth:    auth,
		Config:  spec.ConfigSpec{Format: "toml"},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), name+"-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	return outputDir
}

func assertHostOnlySubdomainHint(t *testing.T, auth string) {
	t.Helper()
	assert.Contains(t, auth, "func hostOnlySubdomainRecapture(domain, name string) string")
	assert.Contains(t, auth, `press-auth login %s --login-url <login-url>\n\n`)
	assert.Contains(t, auth, `hostOnlySubdomainRecapture(domain, "")`)
	assert.NotContains(t, auth, `"--include-subdomains"`)
}
