package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestOmittedConfigSpecReadsAndWritesTOML(t *testing.T) {
	t.Parallel()

	apiSpec := zeroConfigSpec("omitted-config")
	require.Equal(t, spec.ConfigSpec{}, apiSpec.Config)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	require.Equal(t, spec.ConfigFormatTOML, apiSpec.Config.Format)
	require.Equal(t, "~/.config/omitted-config-pp-cli/config.toml", apiSpec.Config.Path)

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.Contains(t, configSrc, "github.com/pelletier/go-toml/v2")
	require.NotContains(t, configSrc, "encoding/json")

	requireGeneratedCompiles(t, outputDir)
	assertGeneratedConfigRuntime(t, outputDir, naming.CLI(apiSpec.Name), "toml")
}

func TestExplicitJSONConfigSpecStaysJSON(t *testing.T) {
	t.Parallel()

	apiSpec := zeroConfigSpec("explicit-json-config")
	apiSpec.Config = spec.ConfigSpec{
		Format: "json",
		Path:   "~/.config/explicit-json-config-pp-cli/config.json",
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	require.Equal(t, "json", apiSpec.Config.Format)
	require.Equal(t, "~/.config/explicit-json-config-pp-cli/config.json", apiSpec.Config.Path)

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.Contains(t, configSrc, "encoding/json")
	require.NotContains(t, configSrc, "github.com/pelletier/go-toml/v2")

	requireGeneratedCompiles(t, outputDir)
	assertGeneratedConfigRuntime(t, outputDir, naming.CLI(apiSpec.Name), "json")
}

func zeroConfigSpec(name string) *spec.APISpec {
	return &spec.APISpec{
		Name:    name,
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"DEMO_CONFIG_TOKEN"},
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}
}

func assertGeneratedConfigRuntime(t *testing.T, outputDir, modulePath, format string) {
	t.Helper()
	src := fmt.Sprintf(generatedConfigRuntimeTest, modulePath+"/internal/cliutil", modulePath+"/internal/cliutil/testenv", modulePath+"/internal/config", "config."+format, modulePath)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "runtime_format_test.go"), []byte(src), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/config", "-count=1", "-run", "TestRuntimeUsesConfiguredFile")
}

const generatedConfigRuntimeTest = `package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	cliutil %q
	testenv %q
	config %q
)

func TestRuntimeUsesConfiguredFile(t *testing.T) {
	if restore, err := cliutil.SetHomeOverride(""); err != nil {
		t.Fatal(err)
	} else {
		t.Cleanup(restore)
	}
	home := testenv.Isolate(t, cliutil.ConfigDir, cliutil.DataDir)
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %%v", err)
	}
	const wantFile = %q
	const cliName = %q
	wantPath := filepath.Join(home, ".config", cliName, wantFile)
	if cfg.Path != wantPath {
		t.Fatalf("Load path = %%q, want %%q", cfg.Path, wantPath)
	}
	if err := cfg.SaveCredential("round-trip-secret"); err != nil {
		t.Fatalf("SaveCredential: %%v", err)
	}
	written, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read %%s: %%v", wantPath, err)
	}
	other := "config.json"
	if wantFile == "config.json" {
		other = "config.toml"
	}
	otherPath := filepath.Join(filepath.Dir(wantPath), other)
	if _, err := os.Stat(otherPath); !os.IsNotExist(err) {
		t.Fatalf("%%s should not exist after save, err=%%v", other, err)
	}
	body := strings.TrimSpace(string(written))
	if !strings.Contains(body, "base_url") {
		t.Fatalf("saved %%s = %%q, want a base_url field", wantFile, body)
	}
	if wantFile == "config.json" {
		if !strings.HasPrefix(body, "{") {
			t.Fatalf("saved config.json = %%q, want a JSON object", body)
		}
	} else if strings.HasPrefix(body, "{") {
		t.Fatalf("saved config.toml = %%q, want TOML", body)
	}

	const canonicalURL = "https://canonical.example"
	const decoyURL = "https://decoy.example"
	canonicalBody := "base_url = '" + canonicalURL + "'\n"
	decoyBody := "{\"base_url\":\"" + decoyURL + "\"}\n"
	if wantFile == "config.json" {
		canonicalBody = "{\"base_url\":\"" + canonicalURL + "\"}\n"
		decoyBody = "base_url = '" + decoyURL + "'\n"
	}
	if err := os.WriteFile(otherPath, []byte(decoyBody), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wantPath, []byte(canonicalBody), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load("")
	if err != nil {
		t.Fatalf("reload: %%v", err)
	}
	if loaded.BaseURL != canonicalURL {
		t.Fatalf("BaseURL = %%q, want %%q from %%s; decoy %%s must be ignored", loaded.BaseURL, canonicalURL, wantFile, other)
	}
	if loaded.Path != wantPath {
		t.Fatalf("reload path = %%q, want %%q", loaded.Path, wantPath)
	}
}
`
