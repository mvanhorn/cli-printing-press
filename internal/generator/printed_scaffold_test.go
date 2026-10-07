package generator

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/generatedmarker"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintedScaffoldLintConfigAndUnusedHelpers(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("scaffold-lint")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	outputDir := filepath.Join(t.TempDir(), "scaffold-lint-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	lint := readGeneratedFile(t, outputDir, ".golangci.yml")
	assert.Contains(t, lint, "version: \"2\"\n")
	assert.Equal(t, 2, strings.Count(lint, "generated: strict"))
	assert.GreaterOrEqual(t, strings.Count(lint, "internal/cliutil/"), 2)
	assert.GreaterOrEqual(t, strings.Count(lint, "internal/platform/"), 2)
	assert.Contains(t, lint, "formatters:")

	goMod := readGeneratedFile(t, outputDir, "go.mod")
	assert.Contains(t, goMod, "\ngo "+librarySafeGoDirective+"\n")
	assert.Contains(t, goMod, "\ntoolchain go"+librarySafeGoDirective+"\n")
	assert.NotContains(t, goMod, "\ngo 1.26.4\n")
	assert.NotContains(t, goMod, "\ngo 1.26.5\n")

	helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	assert.NotContains(t, helpers, "func readSecretFromStdin(")
	assert.NotContains(t, helpers, "maxSecretFromStdin")
	assert.NotContains(t, helpers, "func successfulNoop(")
	assert.NotContains(t, helpers, "func writeNoop(")
	assert.NotContains(t, helpers, "func handleBinaryResponseDelivery(")

	deliver := readGeneratedFile(t, outputDir, "internal", "cli", "deliver.go")
	assert.NotContains(t, deliver, "unwrapBinaryDeliverBody")
	assert.NotContains(t, deliver, "func binaryDeliverPayload(")
	assert.NotContains(t, deliver, "writeBinaryDeliverReceipt")
	assert.NotContains(t, deliver, "encoding/json")
	assert.NotContains(t, deliver, "/internal/client")
	assert.Contains(t, deliver, "func Deliver(")
	assert.Contains(t, deliver, "func safeJoinUnder(")

	assertEmittedGeneratedMarkers(t, outputDir)
	requireGeneratedCompiles(t, outputDir)
}

func TestPrintedScaffoldKeepsReferencedHelpers(t *testing.T) {
	t.Parallel()

	t.Run("create command keeps the noop cluster", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("scaffold-create")
		apiSpec.Auth = spec.AuthConfig{Type: "none"}
		apiSpec.Resources = map[string]spec.Resource{
			"items": {Endpoints: map[string]spec.Endpoint{
				"create": {Method: "POST", Path: "/items", Description: "Create item"},
			}},
		}
		outputDir := filepath.Join(t.TempDir(), "scaffold-create-pp-cli")
		require.NoError(t, New(apiSpec, outputDir).Generate())
		helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
		assert.Contains(t, helpers, "func successfulNoop(")
		assert.Contains(t, helpers, "func writeNoop(")
		assert.NotContains(t, helpers, "func classifyDeleteError(")
		assert.NotContains(t, helpers, "func handleBinaryResponseDelivery(")
		requireGeneratedCompiles(t, outputDir)
	})

	t.Run("binary response keeps delivery helpers", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("scaffold-binary")
		apiSpec.Auth = spec.AuthConfig{Type: "none"}
		apiSpec.Resources = map[string]spec.Resource{
			"reports": {Endpoints: map[string]spec.Endpoint{
				"export": {
					Method:         "GET",
					Path:           "/reports/export",
					Description:    "Export report",
					ResponseFormat: spec.ResponseFormatBinary,
				},
			}},
		}
		outputDir := filepath.Join(t.TempDir(), "scaffold-binary-pp-cli")
		require.NoError(t, New(apiSpec, outputDir).Generate())
		helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
		deliver := readGeneratedFile(t, outputDir, "internal", "cli", "deliver.go")
		assert.Contains(t, helpers, "func handleBinaryResponseDelivery(")
		assert.Contains(t, deliver, "func unwrapBinaryDeliverBody(")
		assert.Contains(t, deliver, "encoding/json")
		assert.Contains(t, deliver, "/internal/client")
		assert.Contains(t, commandCallingBinaryDelivery(t, outputDir), "handleBinaryResponseDelivery(cmd, flags, data)")
		requireGeneratedCompiles(t, outputDir)
	})
}

func TestEmittedStdinSecretHelperMatchesAuthSurface(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		auth  spec.AuthConfig
		hints []string
		want  bool
	}{
		{name: "none", auth: spec.AuthConfig{Type: "none"}, want: false},
		{name: "empty", auth: spec.AuthConfig{}, want: false},
		{
			name: "api_key",
			auth: spec.AuthConfig{Type: "api_key", Header: "Authorization", Format: "Bearer {token}", EnvVars: []string{"SCAFFOLD_TOKEN"}},
			want: true,
		},
		{
			name: "basic_pair",
			auth: spec.AuthConfig{
				Type:   "api_key",
				Header: "Authorization",
				Format: "Basic {username}:{password}",
				EnvVarSpecs: []spec.AuthEnvVar{
					{Name: "SCAFFOLD_USER", Kind: spec.AuthEnvVarKindPerCall, Required: true},
					{Name: "SCAFFOLD_PASS", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
				},
			},
			want: false,
		},
		{
			name: "cookie",
			auth: spec.AuthConfig{
				Type:         "cookie",
				Header:       "Cookie",
				In:           "cookie",
				CookieDomain: ".example.com",
				Cookies:      []string{"session"},
				EnvVars:      []string{"SCAFFOLD_COOKIES"},
			},
			want: true,
		},
		{
			name: "client_credentials",
			auth: spec.AuthConfig{
				Type:        "oauth2",
				Header:      "Authorization",
				Format:      "Bearer {token}",
				OAuth2Grant: spec.OAuth2GrantClientCredentials,
				TokenURL:    "https://login.example.com/token",
				EnvVars:     []string{"SCAFFOLD_CLIENT_ID", "SCAFFOLD_CLIENT_SECRET"},
			},
			want: true,
		},
		{
			name: "device_code",
			auth: spec.AuthConfig{
				Type:                   "oauth2",
				Header:                 "Authorization",
				Format:                 "Bearer {token}",
				OAuth2Grant:            spec.OAuth2GrantDeviceCode,
				DeviceAuthorizationURL: "https://login.example.com/device",
				TokenURL:               "https://login.example.com/token",
				EnvVars:                []string{"SCAFFOLD_CLIENT_ID"},
			},
			want: true,
		},
		{
			name: "authorization_code",
			auth: spec.AuthConfig{
				Type:             "oauth2",
				Header:           "Authorization",
				Format:           "Bearer {token}",
				AuthorizationURL: "https://login.example.com/authorize",
				TokenURL:         "https://login.example.com/token",
				EnvVars:          []string{"SCAFFOLD_CLIENT_ID", "SCAFFOLD_CLIENT_SECRET"},
			},
			want: false,
		},
		{name: "none_persisted_query", auth: spec.AuthConfig{Type: "none"}, hints: []string{"graphql_persisted_query"}, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			apiSpec := minimalSpec("stdin-" + tc.name)
			apiSpec.Auth = tc.auth
			outputDir := filepath.Join(t.TempDir(), "stdin-"+tc.name+"-pp-cli")
			gen := New(apiSpec, outputDir)
			if len(tc.hints) > 0 {
				gen.TrafficAnalysis = &browsersniff.TrafficAnalysis{GenerationHints: tc.hints}
			}
			assert.Equal(t, tc.want, gen.emitsStdinSecretReader())
			require.NoError(t, gen.Generate())
			helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
			authPath := filepath.Join(outputDir, "internal", "cli", "auth.go")
			authExists := true
			if _, err := os.Stat(authPath); err != nil {
				authExists = false
			}
			if tc.want {
				assert.Contains(t, helpers, "func readSecretFromStdin(")
				require.True(t, authExists)
				assert.Contains(t, readGeneratedFile(t, outputDir, "internal", "cli", "auth.go"), "readSecretFromStdin(cmd.InOrStdin())")
			} else {
				assert.NotContains(t, helpers, "func readSecretFromStdin(")
				assert.NotContains(t, helpers, "maxSecretFromStdin")
				if authExists {
					assert.NotContains(t, readGeneratedFile(t, outputDir, "internal", "cli", "auth.go"), "readSecretFromStdin(")
				}
			}
			requireGeneratedCompiles(t, outputDir)
		})
	}
}

func commandCallingBinaryDelivery(t *testing.T, outputDir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(outputDir, "internal", "cli", "*.go"))
	require.NoError(t, err)
	for _, match := range matches {
		body, err := os.ReadFile(match)
		require.NoError(t, err)
		if strings.Contains(string(body), "handleBinaryResponseDelivery(cmd, flags, data)") {
			return string(body)
		}
	}
	t.Fatal("no generated command calls handleBinaryResponseDelivery")
	return ""
}

func assertEmittedGeneratedMarkers(t *testing.T, outputDir string) {
	t.Helper()
	var cliutilFiles, platformFiles int
	err := filepath.WalkDir(outputDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, err := filepath.Rel(outputDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		if rel == "internal/store/extras.go" || strings.Contains(text, "Novel command scaffold") {
			if strings.Contains(text, generatedmarker.StandardLine) || strings.Contains(text, "DO NOT EDIT") {
				t.Errorf("%s is hand-editable and must not carry a generated-file marker", rel)
			}
			return nil
		}
		if strings.HasPrefix(rel, "internal/cliutil/") {
			cliutilFiles++
		}
		if strings.HasPrefix(rel, "internal/platform/") {
			platformFiles++
		}
		needsMarker := strings.HasPrefix(rel, "internal/cliutil/") ||
			strings.HasPrefix(rel, "internal/platform/") ||
			strings.Contains(text, generatedmarker.Text)
		if needsMarker && !markerBeforePackage(text) {
			t.Errorf("%s missing %q before the package clause", rel, generatedmarker.StandardLine)
		}
		return nil
	})
	require.NoError(t, err)
	assert.Greater(t, cliutilFiles, 0)
	assert.Greater(t, platformFiles, 0)
}

func markerBeforePackage(text string) bool {
	idx := strings.Index(text, generatedmarker.StandardLine+"\n")
	pkg := strings.Index(text, "\npackage ")
	if strings.HasPrefix(text, "package ") {
		pkg = 0
	}
	return idx >= 0 && pkg > idx
}
