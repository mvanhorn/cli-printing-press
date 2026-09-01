package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func additionalAuthDoctorSpec(name string) *spec.APISpec {
	prefix := naming.EnvPrefix(name)
	return &spec.APISpec{
		Name:    name,
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "bearer_token",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{prefix + "_TOKEN"},
			AdditionalHeaders: []spec.AdditionalAuthHeader{
				{
					Header: "X-Organization-Id",
					In:     "header",
					EnvVar: spec.AuthEnvVar{
						Name:      prefix + "_ORGANIZATION_ID",
						Kind:      spec.AuthEnvVarKindPerCall,
						Required:  true,
						Sensitive: false,
					},
				},
			},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/" + name + "-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}
}

func TestGeneratedDoctorFailsWhenAdditionalRequiredAuthVarMissing(t *testing.T) {
	t.Parallel()

	apiSpec := additionalAuthDoctorSpec("doctor-add-auth")
	_, binaryPath := buildGeneratedBinary(t, apiSpec)
	prefix := naming.EnvPrefix(apiSpec.Name)
	orgEnv := prefix + "_ORGANIZATION_ID"

	home := t.TempDir()
	env := append(doctorEnv(home, prefix), prefix+"_TOKEN=not-a-real-token")
	payload, err := runDoctorJSON(t, binaryPath, env)
	require.NoError(t, err)
	require.Equal(t, "configured", payload["auth"], "primary token in credentials still configures auth")
	envVars, _ := payload["env_vars"].(string)
	require.Contains(t, envVars, "ERROR missing required: "+orgEnv,
		"missing additional required auth var must fail Env Vars; got %q", envVars)
	require.NotContains(t, envVars, "credentials available from",
		"primary-token coverage must not be reported as additional-var coverage")

	_, err = runDoctorJSON(t, binaryPath, env, "--fail-on", "error")
	require.Error(t, err, "--fail-on=error must trip when an additional required auth var is missing")

	human := runDoctorHuman(t, binaryPath, env)
	require.Contains(t, human, "FAIL Env Vars: ERROR missing required: "+orgEnv)

	envOK := append(append([]string{}, env...), orgEnv+"=12345")
	payload, err = runDoctorJSON(t, binaryPath, envOK)
	require.NoError(t, err)
	envVars, _ = payload["env_vars"].(string)
	require.NotContains(t, envVars, "ERROR missing required")
	require.True(t, strings.HasPrefix(envVars, "OK") || strings.Contains(envVars, "available"),
		"both token and organization id present should keep Env Vars OK; got %q", envVars)
}

func TestGeneratedDoctorOptionalAdditionalAuthVarDoesNotFail(t *testing.T) {
	t.Parallel()

	apiSpec := additionalAuthDoctorSpec("doctor-opt-auth")
	apiSpec.BaseURL = ""
	labelEnv := naming.EnvPrefix(apiSpec.Name) + "_ACCOUNT_LABEL"
	apiSpec.Auth.AdditionalHeaders = append(apiSpec.Auth.AdditionalHeaders, spec.AdditionalAuthHeader{
		Header: "X-Account-Label",
		In:     "header",
		EnvVar: spec.AuthEnvVar{
			Name:      labelEnv,
			Kind:      spec.AuthEnvVarKindPerCall,
			Required:  false,
			Sensitive: false,
		},
	})

	outputDir, binaryPath := buildGeneratedBinary(t, apiSpec)
	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	require.Contains(t, string(doctorSrc), `recordAdditionalAuthEnv("`+labelEnv+`", configuredValue, false)`)

	prefix := naming.EnvPrefix(apiSpec.Name)
	orgEnv := prefix + "_ORGANIZATION_ID"
	home := t.TempDir()
	env := append(doctorEnv(home, prefix), prefix+"_TOKEN=not-a-real-token", orgEnv+"=12345")
	payload, err := runDoctorJSON(t, binaryPath, env)
	require.NoError(t, err)
	require.Equal(t, "configured", payload["auth"])
	envVars, _ := payload["env_vars"].(string)
	require.NotContains(t, envVars, "ERROR missing required")
	require.Contains(t, envVars, labelEnv+" optional")

	_, err = runDoctorJSON(t, binaryPath, env, "--fail-on", "error")
	require.NoError(t, err, "--fail-on=error must not trip on a missing optional additional auth var")
}

func TestGeneratedDoctorKeylessAuthDoesNotInventAPIKey(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-keyless")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	outputDir, binaryPath := buildGeneratedBinary(t, apiSpec)

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	doctor := string(doctorSrc)
	require.Contains(t, doctor, `report["auth"] = "not required"`)
	require.NotContains(t, doctor, "missing required:")
	require.NotContains(t, doctor, naming.EnvPrefix(apiSpec.Name)+"_API_KEY")

	payload, err := runDoctorJSON(t, binaryPath, doctorEnv(t.TempDir(), naming.EnvPrefix(apiSpec.Name)))
	require.NoError(t, err)
	require.Equal(t, "not required", payload["auth"])
	require.NotContains(t, payload, "env_vars")

	human := runDoctorHuman(t, binaryPath, doctorEnv(t.TempDir(), naming.EnvPrefix(apiSpec.Name)))
	require.Contains(t, human, "OK Auth: not required")
	require.NotContains(t, human, "API_KEY")
	require.NotContains(t, human, "Set your API key")
}

func TestGeneratedKeylessOpenAPIDoesNotAdvertiseAPIKey(t *testing.T) {
	t.Parallel()

	parsed, err := openapi.Parse([]byte(`openapi: "3.0.3"
info:
  title: Open-Meteo Weather
  version: "1.0.0"
  description: "Free weather API. No API key required."
servers:
  - url: https://api.open-meteo.com
paths:
  /v1/forecast:
    get:
      parameters:
        - name: latitude
          in: query
          schema: { type: number }
      responses:
        "200": { description: OK }
`))
	require.NoError(t, err)
	require.Equal(t, "none", parsed.Auth.Type)
	require.Empty(t, parsed.Auth.EnvVars)

	_, binaryPath := buildGeneratedBinary(t, parsed)
	payload, err := runDoctorJSON(t, binaryPath, doctorEnv(t.TempDir(), naming.EnvPrefix(parsed.Name)))
	require.NoError(t, err)
	require.Equal(t, "not required", payload["auth"])
	require.NotContains(t, payload, "env_vars")

	human := runDoctorHuman(t, binaryPath, doctorEnv(t.TempDir(), naming.EnvPrefix(parsed.Name)))
	require.Contains(t, human, "OK Auth: not required")
	require.NotContains(t, human, "API_KEY")
	require.NotContains(t, human, "Set your API key")
}

func runDoctorHuman(t *testing.T, binaryPath string, env []string) string {
	t.Helper()
	cmd := exec.Command(binaryPath, "doctor")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "doctor human output: %s", string(out))
	return string(out)
}
