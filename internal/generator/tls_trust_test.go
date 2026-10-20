package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedTLSTrustStock(t *testing.T) {
	t.Parallel()
	outputDir := generateTrustModule(t, "tls-trust-canary", func(apiSpec *spec.APISpec) {
		apiSpec.Auth = spec.AuthConfig{Type: "none"}
	})
	mod, prefix := naming.CLI("tls-trust-canary"), naming.EnvPrefix("tls-trust-canary")
	writeTrustFile(t, outputDir, "internal/client/tls_trust_test.go", tlsTrustClientTest(mod, prefix, false, false))
	writeTrustFile(t, outputDir, "internal/config/tls_trust_test.go", tlsTrustConfigTest(mod, prefix))
	writeTrustFile(t, outputDir, "internal/cli/tls_trust_test.go", tlsTrustCLITest(mod, prefix, false))
	writeTrustFile(t, outputDir, "internal/cliutil/tls_trust_unix_test.go", tlsTrustUnixTest())
	readme := readGeneratedFile(t, outputDir, "README.md")
	require.Contains(t, readme, "--ca-cert")
	require.Contains(t, readme, prefix+"_CA_CERT")
	require.Contains(t, readme, "tls_server_name")
	require.NotContains(t, readme, "## TLS")
	skill := readGeneratedFile(t, outputDir, "SKILL.md")
	require.Contains(t, skill, "--ca-cert")
	require.Contains(t, skill, "doctor")
	runGoCommandRequired(t, outputDir, "test",
		"./internal/client", "./internal/config", "./internal/cli", "./internal/cliutil",
		"-run", "TestTLSTrust", "-count=1")
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp/cobratree",
		"-run", "TestBlockedRootFlagsIncludeTLSTrust|TestCliArgsFromMCP_BlocksRootFlags", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedTLSTrustBrowser(t *testing.T) {
	t.Parallel()
	outputDir := generateTrustModule(t, "tls-trust-browser", func(apiSpec *spec.APISpec) {
		apiSpec.Auth = spec.AuthConfig{Type: "none"}
		apiSpec.SpecSource = "sniffed"
	})
	mod, prefix := naming.CLI("tls-trust-browser"), naming.EnvPrefix("tls-trust-browser")
	writeTrustFile(t, outputDir, "internal/client/tls_trust_test.go", tlsTrustClientTest(mod, prefix, true, false))
	writeTrustFile(t, outputDir, "internal/cli/tls_trust_test.go", tlsTrustCLITest(mod, prefix, true))
	readme := readGeneratedFile(t, outputDir, "README.md")
	require.Contains(t, readme, "`--insecure` overrides `--ca-cert` and `--tls-server-name`.")
	runGoCommandRequired(t, outputDir, "test", "./internal/client", "./internal/cli", "-run", "TestTLSTrust", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedTLSTrustHTTP1Only(t *testing.T) {
	t.Parallel()
	outputDir := generateTrustModule(t, "tls-trust-h1", func(apiSpec *spec.APISpec) {
		apiSpec.Auth = spec.AuthConfig{Type: "none"}
		apiSpec.HTTPTransport = spec.HTTPTransportBrowserHTTP
	})
	mod, prefix := naming.CLI("tls-trust-h1"), naming.EnvPrefix("tls-trust-h1")
	writeTrustFile(t, outputDir, "internal/client/tls_trust_test.go", tlsTrustClientTest(mod, prefix, false, true))
	runGoCommandRequired(t, outputDir, "test", "./internal/client", "-run", "TestTLSTrust", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedTLSTrustBrowserVariantsCompile(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		transport string
	}{
		{name: "tls-trust-h2", transport: spec.HTTPTransportBrowserChromeH2},
		{name: "tls-trust-h3", transport: spec.HTTPTransportBrowserChromeH3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			outputDir := generateTrustModule(t, tc.name, func(apiSpec *spec.APISpec) {
				apiSpec.Auth = spec.AuthConfig{Type: "none"}
				apiSpec.HTTPTransport = tc.transport
			})
			mod, prefix := naming.CLI(tc.name), naming.EnvPrefix(tc.name)
			writeTrustFile(t, outputDir, "internal/client/tls_trust_test.go", tlsTrustConstructTest(prefix))
			runGoCommandRequired(t, outputDir, "test", "./internal/client", "-run", "TestTLSTrust", "-count=1")
			requireGeneratedCompiles(t, outputDir)
			_ = mod
		})
	}
}

func TestGeneratedTLSTrustAuthLooseConfig(t *testing.T) {
	t.Parallel()
	apiSpec := minimalSpec("tls-trust-auth")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())
	mod, prefix := naming.CLI(apiSpec.Name), naming.EnvPrefix(apiSpec.Name)
	writeTrustFile(t, outputDir, "internal/config/tls_trust_test.go", tlsTrustConfigTest(mod, prefix))
	runGoCommandRequired(t, outputDir, "test", "./internal/config", "-run", "TestTLSTrust", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func generateTrustModule(t *testing.T, name string, setup func(*spec.APISpec)) string {
	t.Helper()
	apiSpec := minimalSpec(name)
	apiSpec.BaseURL = "https://api.example.com"
	if setup != nil {
		setup(apiSpec)
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())
	return outputDir
}

func writeTrustFile(t *testing.T, outputDir, rel, src string) {
	t.Helper()
	path := filepath.Join(outputDir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
}

func tlsTrustUnixTest() string {
	return `//go:build !windows

package cliutil

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type tlsTrustStubInfo struct{}

func (tlsTrustStubInfo) Name() string       { return "config.toml" }
func (tlsTrustStubInfo) Size() int64        { return 1 }
func (tlsTrustStubInfo) Mode() os.FileMode  { return 0o600 }
func (tlsTrustStubInfo) ModTime() time.Time { return time.Time{} }
func (tlsTrustStubInfo) IsDir() bool        { return false }
func (tlsTrustStubInfo) Sys() any           { return "not a stat" }

func TestTLSTrustFileVerdict(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.toml")
	if err := os.WriteFile(good, []byte("ca_cert = \"/x.pem\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, allow, err := ReadConfigTrust(good)
	if err != nil || !allow || len(data) == 0 {
		t.Fatalf("private config allow=%v err=%v", allow, err)
	}
	if err := os.Chmod(good, 0o640); err != nil {
		t.Fatal(err)
	}
	_, allow, err = ReadConfigTrust(good)
	if err != nil || allow {
		t.Fatalf("group-readable config allow=%v err=%v", allow, err)
	}
	if err := os.Chmod(good, 0o600); err != nil {
		t.Fatal(err)
	}
	loose := filepath.Join(dir, "loose.toml")
	if err := os.WriteFile(loose, []byte("ca_cert = \"/y.pem\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.toml")
	if err := os.Symlink(loose, link); err != nil {
		t.Skip(err)
	}
	_, allow, err = ReadConfigTrust(link)
	if err != nil || allow {
		t.Fatalf("symlink to loose target allow=%v err=%v", allow, err)
	}
	private := filepath.Join(dir, "private.toml")
	if err := os.WriteFile(private, []byte("ca_cert = \"/z.pem\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	privateLink := filepath.Join(dir, "private-link.toml")
	if err := os.Symlink(private, privateLink); err != nil {
		t.Fatal(err)
	}
	_, allow, err = ReadConfigTrust(privateLink)
	if err != nil || !allow {
		t.Fatalf("symlink to private target allow=%v err=%v", allow, err)
	}
	_, allow, err = ReadConfigTrust(dir)
	if err == nil && allow {
		t.Fatal("directory config was trusted")
	}
	if trustFileAllowsSettings(tlsTrustStubInfo{}, nil, good) {
		t.Fatal("config with an unreadable owner was trusted")
	}
	if !TrustConfigAllowsSettings("") {
		t.Fatal("empty path should allow settings")
	}
	if !TrustConfigAllowsSettings(filepath.Join(dir, "missing.toml")) {
		t.Fatal("missing file should allow settings")
	}
	if TrustConfigAllowsSettings(loose) {
		t.Fatal("second open of a loose file was trusted")
	}
}
`
}
