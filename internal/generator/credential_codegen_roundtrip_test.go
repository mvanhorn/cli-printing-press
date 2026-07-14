package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedMultiEnvAPIKeyCredentialsRoundTripAcrossConfigFormats(t *testing.T) {
	t.Parallel()

	for _, format := range []string{"json", "toml"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec("credential-alias-" + format)
			apiSpec.Auth = spec.AuthConfig{
				Type:    "api_key",
				Header:  "AccessKey",
				EnvVars: []string{"BUNNY_API_KEY", "BUNNYNET_API_KEY"},
				EnvVarSpecs: spec.NewORCaseEnvVarSpecs([]string{
					"BUNNY_API_KEY",
					"BUNNYNET_API_KEY",
				}),
			}
			apiSpec.Config = spec.ConfigSpec{
				Format: format,
				Path:   "~/.config/credential-alias/config." + format,
			}

			outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
			require.NoError(t, New(apiSpec, outputDir).Generate())

			credentialsSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "credentials.go")
			require.Regexp(t, `BunnyApiKey\s+string\s+`+"`toml:\"api_key\"`", credentialsSrc)
			require.Regexp(t, `BunnynetApiKey\s+string\s+`+"`toml:\"bunnynet_api_key\"`", credentialsSrc)

			configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
			require.Regexp(t, `BunnyApiKey\s+string\s+`+"`"+format+":\"api_key\"`", configSrc)
			require.Regexp(t, `BunnynetApiKey\s+string\s+`+"`"+format+":\"bunnynet_api_key\"`", configSrc)

			authSrc := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
			require.Contains(t, authSrc, "cmd.AddCommand(newAuthSetTokenCmd(flags))")

			const aliasRoundTripTest = `package cliutil

import "testing"

func TestCredentialAliasFieldsRoundTripIndependently(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", home)
	if restore, err := SetHomeOverride(""); err == nil {
		t.Cleanup(restore)
	} else {
		t.Fatalf("SetHomeOverride() error = %v", err)
	}

	cases := []struct {
		name string
		creds *Credentials
		value func(*Credentials) string
	}{
		{"canonical", &Credentials{BunnyApiKey: "canonical-secret"}, func(c *Credentials) string { return c.BunnyApiKey }},
		{"alias", &Credentials{BunnynetApiKey: "alias-secret"}, func(c *Credentials) string { return c.BunnynetApiKey }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := SaveCredentials(tc.creds); err != nil {
				t.Fatalf("SaveCredentials() error = %v", err)
			}
			loaded, ok, err := LoadCredentials()
			if err != nil || !ok {
				t.Fatalf("LoadCredentials() = ok %v, err %v", ok, err)
			}
			if got := tc.value(loaded); got != tc.value(tc.creds) {
				t.Fatalf("reloaded credential = %q, want %q", got, tc.value(tc.creds))
			}
		})
	}
}
`
			require.NoError(t, os.WriteFile(
				filepath.Join(outputDir, "internal", "cliutil", "credential_alias_roundtrip_test.go"),
				[]byte(aliasRoundTripTest), 0o600))

			credentialTests := readGeneratedFile(t, outputDir, "internal", "cliutil", "credentials_test.go")
			require.Contains(t, credentialTests, "go func()")
			require.NotContains(t, credentialTests, "legacyCredentialTOML")
			if format == "json" {
				require.Contains(t, credentialTests, "json.Marshal")
			} else {
				require.NotContains(t, credentialTests, "json.Marshal")
			}

			requireGeneratedCompiles(t, outputDir)
			runGoCommandRequired(t, outputDir, "test", "./internal/cliutil", "./internal/config", "./internal/cli")

			binPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
			runGoCommandRequired(t, outputDir, "build", "-o", binPath, "./cmd/"+naming.CLI(apiSpec.Name))
			home := t.TempDir()
			dataHome := filepath.Join(home, "data")
			cmd := exec.Command(binPath, "auth", "set-token", "set-token-secret")
			cmd.Env = append(os.Environ(),
				"HOME="+home,
				"XDG_CONFIG_HOME="+filepath.Join(home, "config"),
				"XDG_DATA_HOME="+dataHome,
			)
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "auth set-token failed: %s", string(out))
			persisted, err := os.ReadFile(filepath.Join(dataHome, naming.CLI(apiSpec.Name), "credentials.toml"))
			require.NoError(t, err)
			require.Contains(t, string(persisted), "api_key")
			require.Contains(t, string(persisted), "set-token-secret")
		})
	}
}

func TestGeneratedCredentialStderrCaptureExercisesPipeCapacity(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("credential-stderr")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	credentialTests := readGeneratedFile(t, outputDir, "internal", "cliutil", "credentials_test.go")
	require.Contains(t, credentialTests, `strings.Repeat("x", 128*1024)`)
	require.True(t, strings.Index(credentialTests, "go func()") < strings.Index(credentialTests, "fn()"),
		"stderr reader must start before the callback writes to the pipe")

	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/cliutil", "-run", "TestCaptureCredentialStderrDrainsConcurrently")
}

func TestAuthSetTokenAvailabilityDistinguishesAliasesFromRequiredPairs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		auth spec.AuthConfig
		want bool
	}{
		{
			name: "optional aliases share one token",
			auth: spec.AuthConfig{
				Type:        "api_key",
				EnvVarSpecs: spec.NewORCaseEnvVarSpecs([]string{"SERVICE_API_KEY", "SERVICE_TOKEN"}),
			},
			want: true,
		},
		{
			name: "required pair needs separate credentials",
			auth: spec.AuthConfig{
				Type: "api_key",
				EnvVarSpecs: []spec.AuthEnvVar{
					{Name: "SERVICE_USER", Kind: spec.AuthEnvVarKindPerCall, Required: true},
					{Name: "SERVICE_SECRET", Kind: spec.AuthEnvVarKindPerCall, Required: true},
				},
			},
			want: false,
		},
		{
			name: "single required credential stays supported",
			auth: spec.AuthConfig{
				Type: "api_key",
				EnvVarSpecs: []spec.AuthEnvVar{
					{Name: "SERVICE_API_KEY", Kind: spec.AuthEnvVarKindPerCall, Required: true},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := authSetTokenAvailable(tt.auth); got != tt.want {
				t.Fatalf("authSetTokenAvailable() = %v, want %v", got, tt.want)
			}
		})
	}
}
