package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedTLSTransportVerifiesCertificatesByDefault(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("transport-tls-canary")
	apiSpec.SpecSource = "sniffed"
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())
	readme := readGeneratedFile(t, outputDir, "README.md")
	require.Contains(t, readme, "TLS certificates are verified by default")
	require.Contains(t, readme, "--insecure")
	require.Contains(t, readme, "TRANSPORT_TLS_CANARY_SKIP_TLS_VERIFY=true")
	require.Contains(t, readme, "skip_tls_verify = true")

	clientTest := `package client

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGeneratedTLSVerificationDefaultsSecureWithExplicitOptOut(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	secureClient := newHTTPClient(time.Second, nil, false)
	if _, err := secureClient.Get(server.URL); err == nil {
		t.Fatal("default client accepted an untrusted TLS certificate")
	}

	insecureClient := newHTTPClient(time.Second, nil, true)
	resp, err := insecureClient.Get(server.URL)
	if err != nil {
		t.Fatalf("explicit insecure client rejected test certificate: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "transport_tls_runtime_test.go"),
		[]byte(clientTest), 0o600))

	configTest := `package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedTLSSkipVerifyEnvironmentOptOut(t *testing.T) {
	for _, value := range []string{"1", "true", "TRUE", "yes", "on"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("TRANSPORT_TLS_CANARY_SKIP_TLS_VERIFY", value)
			cfg, err := Load(filepath.Join(t.TempDir(), "config.toml"))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !cfg.SkipTLSVerify {
				t.Fatalf("SkipTLSVerify = false for %q", value)
			}
		})
	}
}

func TestGeneratedTLSTransientOptOutDoesNotPersist(t *testing.T) {
	for _, tc := range []struct {
		name   string
		apply  func(*Config)
		setEnv bool
	}{
		{name: "flag setter", apply: func(cfg *Config) { cfg.SetSkipTLSVerify(true) }},
		{name: "environment", setEnv: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(configPath, []byte("skip_tls_verify = false\n"), 0600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			if tc.setEnv {
				t.Setenv("TRANSPORT_TLS_CANARY_SKIP_TLS_VERIFY", "true")
			}
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if tc.apply != nil {
				tc.apply(cfg)
			}
			if !cfg.SkipTLSVerify {
				t.Fatal("transient opt-out was not applied")
			}
			if persisted := cfg.configForSave(); persisted.SkipTLSVerify {
				t.Fatal("transient opt-out would persist to config")
			}
		})
	}
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "config", "transport_tls_runtime_test.go"),
		[]byte(configTest), 0o600))

	cliTest := `package cli

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestGeneratedTLSOptOutsReachTransport(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	for _, tc := range []struct {
		name string
		flag bool
		env  bool
	}{
		{name: "flag", flag: true},
		{name: "environment", env: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags := rootFlags{configPath: filepath.Join(t.TempDir(), "config.toml")}
			if tc.flag {
				cmd := newRootCmd(&flags)
				if err := cmd.PersistentFlags().Parse([]string{"--insecure"}); err != nil {
					t.Fatalf("parse --insecure: %v", err)
				}
			}
			if tc.env {
				t.Setenv("TRANSPORT_TLS_CANARY_SKIP_TLS_VERIFY", "true")
			}
			client, err := flags.newClient()
			if err != nil {
				t.Fatalf("newClient: %v", err)
			}
			if !client.Config.SkipTLSVerify {
				t.Fatal("insecure opt-out did not reach client config")
			}
			resp, err := client.HTTPClient.Get(server.URL)
			if err != nil {
				t.Fatalf("insecure opt-out did not reach transport: %v", err)
			}
			resp.Body.Close()
		})
	}
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "cli", "transport_tls_runtime_test.go"),
		[]byte(cliTest), 0o600))

	runGoCommandRequired(t, outputDir, "test", "./internal/client", "./internal/config", "./internal/cli", "-run", "^TestGeneratedTLS", "-count=1")
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp/cobratree", "-run", "^TestCliArgsFromMCP_BlocksRootFlags$", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedTLSPersistedOptOutSurvivesAuthReducedSave(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("transport-tls-auth-persistence")
	apiSpec.SpecSource = "sniffed"
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const configTest = `package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedTLSPersistedOptOutSurvivesAuthReducedSave(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("skip_tls_verify = true\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("initial Load: %v", err)
	}
	if !cfg.SkipTLSVerify {
		t.Fatal("file-backed SkipTLSVerify was not loaded")
	}
	if err := cfg.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	reloaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.SkipTLSVerify {
		t.Fatal("file-backed SkipTLSVerify was dropped by reduced persistence")
	}
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "config", "transport_tls_persistence_runtime_test.go"),
		[]byte(configTest), 0o600))

	listCmd := exec.Command("go", "test", "-mod=mod", "-list", "^TestGeneratedTLSPersistedOptOutSurvivesAuthReducedSave$", "./internal/config")
	listCmd.Dir = outputDir
	cacheDir, err := goBuildCacheDir(outputDir)
	require.NoError(t, err)
	listCmd.Env = append(os.Environ(), "GOCACHE="+cacheDir)
	listOut, err := listCmd.CombinedOutput()
	require.NoError(t, err, string(listOut))
	require.Contains(t, string(listOut), "TestGeneratedTLSPersistedOptOutSurvivesAuthReducedSave")

	runGoCommandRequired(t, outputDir, "test", "-v", "-run", "^TestGeneratedTLSPersistedOptOutSurvivesAuthReducedSave$", "./internal/config")
}
