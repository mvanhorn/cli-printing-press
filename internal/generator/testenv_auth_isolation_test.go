package generator

import (
	"bytes"
	"fmt"
	"go/format"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestSandboxClearedAuthEnvVars(t *testing.T) {
	t.Parallel()

	quoteName := "QUOTE\"KEY"
	cases := []struct {
		name string
		auth spec.AuthConfig
		want []string
	}{
		{
			name: "no auth",
			auth: spec.AuthConfig{Type: "none"},
		},
		{
			name: "legacy env vars only",
			auth: spec.AuthConfig{
				Type:    "api_key",
				EnvVars: []string{"LEGACY_TOKEN", "LEGACY_SECRET"},
			},
			want: []string{"LEGACY_TOKEN", "LEGACY_SECRET"},
		},
		{
			name: "specs harvested sibling and duplicates",
			auth: spec.AuthConfig{
				Type:    "api_key",
				EnvVars: []string{"PRIMARY_KEY", "  PRIMARY_KEY  ", "ALT_TOKEN"},
				EnvVarSpecs: []spec.AuthEnvVar{
					{Name: "PRIMARY_KEY", Kind: spec.AuthEnvVarKindPerCall, Required: true},
					{Name: "HARVESTED_COOKIE", Kind: spec.AuthEnvVarKindHarvested},
					{Name: "", Kind: spec.AuthEnvVarKindPerCall},
					{Name: "   ", Kind: spec.AuthEnvVarKindPerCall},
					{Name: "BAD=NAME", Kind: spec.AuthEnvVarKindPerCall},
					{Name: "BAD\x00NAME", Kind: spec.AuthEnvVarKindPerCall},
					{Name: quoteName, Kind: spec.AuthEnvVarKindPerCall},
				},
				AdditionalHeaders: []spec.AdditionalAuthHeader{
					{Header: "X-Sibling", EnvVar: spec.AuthEnvVar{Name: "SIBLING_KEY"}},
					{Header: "X-Dup", EnvVar: spec.AuthEnvVar{Name: "PRIMARY_KEY"}},
					{Header: "X-Blank", EnvVar: spec.AuthEnvVar{Name: "  "}},
					{Header: "X-Equals", EnvVar: spec.AuthEnvVar{Name: "ALSO=BAD"}},
				},
			},
			// Padded names are a different lookup than the trimmed spelling.
			want: []string{"PRIMARY_KEY", "  PRIMARY_KEY  ", "ALT_TOKEN", "HARVESTED_COOKIE", "   ", quoteName, "SIBLING_KEY", "  "},
		},
		{
			name: "only names setenv would reject",
			auth: spec.AuthConfig{
				Type: "api_key",
				EnvVarSpecs: []spec.AuthEnvVar{
					{Name: "BAD=NAME"},
					{Name: "NUL\x00NAME"},
					{Name: ""},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, sandboxClearedAuthEnvVars(tc.auth))
		})
	}
}

func TestTestenvTemplateRendersClearedAuthEnvVars(t *testing.T) {
	t.Parallel()

	noAuth := renderTestenv(t, "noauth", spec.AuthConfig{Type: "none"})
	require.Equal(t, sandboxDirVars("noauth"), clearedVarLiterals(t, noAuth))

	auth := spec.AuthConfig{
		Type: "api_key",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "MULTI_KEY", Kind: spec.AuthEnvVarKindPerCall, Required: true},
			{Name: "MULTI_ALT", Kind: spec.AuthEnvVarKindPerCall, Required: false},
			{Name: "MULTI_SESSION", Kind: spec.AuthEnvVarKindHarvested},
			{Name: "BAD=NAME", Kind: spec.AuthEnvVarKindPerCall},
			{Name: "BAD\x00NAME", Kind: spec.AuthEnvVarKindPerCall},
			{Name: `QUOTE"KEY`, Kind: spec.AuthEnvVarKindPerCall},
			{Name: "  MULTI_KEY  ", Kind: spec.AuthEnvVarKindPerCall},
			{Name: "   ", Kind: spec.AuthEnvVarKindPerCall},
		},
		AdditionalHeaders: []spec.AdditionalAuthHeader{
			{Header: "X-Sibling", EnvVar: spec.AuthEnvVar{Name: "MULTI_SIBLING"}},
		},
	}
	rendered := renderTestenv(t, "multi", auth)
	require.Equal(t, append(sandboxDirVars("multi"), sandboxClearedAuthEnvVars(auth)...), clearedVarLiterals(t, rendered))
	require.NotContains(t, rendered, "BAD=NAME")
	require.NotContains(t, rendered, "BAD\x00NAME")
	require.Contains(t, rendered, `"QUOTE\"KEY"`)
	require.Contains(t, rendered, `"  MULTI_KEY  "`)
	require.Contains(t, rendered, `"   "`)
}

func TestGeneratedMCPTestsIgnoreExportedAuthEnv(t *testing.T) {
	t.Parallel()

	api := &spec.APISpec{
		Name:    "scopeauth",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:   "api_key",
			Header: "Authorization",
			Format: "Bearer {token}",
			EnvVarSpecs: []spec.AuthEnvVar{
				{Name: "SCOPEAUTH_API_KEY", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
				{Name: "SCOPEAUTH_ALT_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true},
				{Name: "SCOPEAUTH_SESSION", Kind: spec.AuthEnvVarKindHarvested, Required: false, Sensitive: true},
			},
			AdditionalHeaders: []spec.AdditionalAuthHeader{{
				Header: "X-Api-Secret",
				In:     "header",
				EnvVar: spec.AuthEnvVar{Name: "SCOPEAUTH_SIBLING", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			}},
		},
		Learn: spec.LearnConfig{Disabled: true},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/scopeauth-pp-cli/config.toml",
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
	outputDir := filepath.Join(t.TempDir(), naming.CLI(api.Name))
	gen := New(api, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Search: true, MCP: true}
	require.NoError(t, gen.Generate())

	wantAuth := []string{
		"SCOPEAUTH_API_KEY",
		"SCOPEAUTH_ALT_TOKEN",
		"SCOPEAUTH_SESSION",
		"SCOPEAUTH_SIBLING",
	}
	require.Equal(t, wantAuth, sandboxClearedAuthEnvVars(api.Auth))

	testenvSrc := string(mustRead(t, filepath.Join(outputDir, "internal", "cliutil", "testenv", "testenv.go")))
	require.Equal(t, append(sandboxDirVars(api.Name), wantAuth...), clearedVarLiterals(t, testenvSrc))

	toolsTest := string(mustRead(t, filepath.Join(outputDir, "internal", "mcp", "tools_test.go")))
	require.Contains(t, toolsTest, `t.Setenv("SCOPEAUTH_API_KEY", "scope-secret")`)
	require.Contains(t, toolsTest, "func TestMCPDBPathUsesCredentialScopedStore")
	require.Contains(t, toolsTest, "func TestMCPSearchMissingStoreIsActionable")
	require.Contains(t, toolsTest, "func TestMCPSQLMissingStoreIsActionable")

	acl := string(mustRead(t, filepath.Join(outputDir, "internal", "cliutil", "darwin_acl_live_test.go")))
	require.Contains(t, acl, `const darwinSystemChmod = "/bin/chmod"`)
	require.Contains(t, acl, `exec.Command(darwinSystemChmod, "-N", path)`)
	require.Contains(t, acl, `exec.Command(darwinSystemChmod, "+a", entry, path)`)
	require.NotContains(t, acl, `exec.Command("chmod"`)

	if testing.Short() {
		t.Skip("generated CLI compile tests run in the full generated-test CI lane")
	}

	requireGeneratedCompiles(t, outputDir)
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			runGoCommandWithEnv(t, outputDir, []string{"GOOS=" + goos, "CGO_ENABLED=0"}, "vet", "-mod=mod", "./internal/cliutil/...")
		})
	}
	// Values are real credentials, not ${...} placeholders: EffectiveEnv
	// treats a placeholder as unset, which would hide a missed clear.
	// The lowercase name is a different Unix variable; clearing only that
	// spelling must not satisfy the test.
	extra := make([]string, 0, len(wantAuth)+1)
	for i, name := range wantAuth {
		extra = append(extra, fmt.Sprintf("%s=  operator-secret-%d=value  ", name, i))
	}
	extra = append(extra, "scopeauth_api_key=lowercase-bypass")
	out, err := runGoCommandOutputWithEnv(t, outputDir, extra, "test", "-count=1", "-v", "./internal/mcp", "-run",
		"^(TestMCPSearchMissingStoreIsActionable|TestMCPSQLMissingStoreIsActionable|TestMCPDBPathUsesCredentialScopedStore|TestMCPPathResolutionMatchesCLIResolverWithHomeEnv|TestMCPPathResolutionMatchesCLIResolverWithPlatformDefaults)$")
	require.NoError(t, err, out)
	for _, name := range []string{
		"TestMCPSearchMissingStoreIsActionable",
		"TestMCPSQLMissingStoreIsActionable",
		"TestMCPDBPathUsesCredentialScopedStore",
		"TestMCPPathResolutionMatchesCLIResolverWithHomeEnv",
		"TestMCPPathResolutionMatchesCLIResolverWithPlatformDefaults",
	} {
		require.Contains(t, out, "--- PASS: "+name)
	}
}

func renderTestenv(t *testing.T, cliName string, auth spec.AuthConfig) string {
	t.Helper()
	api := &spec.APISpec{
		Name:    cliName,
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    auth,
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/" + cliName + "-pp-cli/config.toml",
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
	gen := New(api, t.TempDir())
	tmpl, err := gen.template("cliutil_testenv.go.tmpl")
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, tmpl.Execute(&buf, api))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err, buf.String())
	return string(formatted)
}

func sandboxDirVars(cliName string) []string {
	prefix := naming.EnvPrefix(cliName)
	return []string{
		prefix + "_CONFIG",
		prefix + "_CONFIG_DIR",
		prefix + "_DATA_DIR",
		prefix + "_STATE_DIR",
		prefix + "_CACHE_DIR",
		prefix + "_HOME",
		"XDG_CONFIG_HOME",
		"XDG_DATA_HOME",
		"XDG_STATE_HOME",
		"XDG_CACHE_HOME",
	}
}

func clearedVarLiterals(t *testing.T, src string) []string {
	t.Helper()
	const start = "var clearedVars = []string{"
	i := strings.Index(src, start)
	require.NotEqual(t, -1, i, src)
	rest := src[i+len(start):]
	j := strings.Index(rest, "}")
	require.NotEqual(t, -1, j, src)
	var names []string
	for line := range strings.SplitSeq(rest[:j], "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimSuffix(line, ",")
		val, err := strconv.Unquote(line)
		require.NoError(t, err, line)
		names = append(names, val)
	}
	return names
}
