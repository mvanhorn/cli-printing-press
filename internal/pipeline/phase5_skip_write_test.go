package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func phase5SkipTestHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", home)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", home)
}

func phase5SkipManifest(authType string) CLIManifest {
	return CLIManifest{
		SchemaVersion: 1,
		APIName:       "widget",
		CLIName:       "widget-pp-cli",
		RunID:         "run-skip",
		AuthType:      authType,
		AuthEnvVars:   []string{"WIDGET_API_KEY"},
	}
}

func writePhase5SkipTree(t *testing.T, manifest CLIManifest, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if files == nil {
		files = map[string]string{
			"go.mod":  "module " + manifest.CLIName + "\n\ngo 1.26\n",
			"main.go": "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"ok\") }\n",
		}
	}
	for rel, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	require.NoError(t, WriteCLIManifest(dir, manifest))
	return dir
}

func phase5SkipPath(dir, runID string) string {
	return filepath.Join(dir, ".manuscripts", runID, "proofs", Phase5SkipFilename)
}

func TestWritePhase5SkipMarkerAuthRequiredAcceptedByPromote(t *testing.T) {
	for _, authType := range []string{"api_key", "bearer_token", "oauth2", "oauth2_refresh"} {
		t.Run(authType, func(t *testing.T) {
			phase5SkipTestHome(t)
			t.Setenv("WIDGET_API_KEY", "")
			manifest := phase5SkipManifest(authType)
			dir := writePhase5SkipTree(t, manifest, nil)
			skipPath := phase5SkipPath(dir, manifest.RunID)
			reason := phase5SkipReasonAuthRequiredNoCredential
			if authType == "api_key" {
				reason = "AUTH_REQUIRED_NO_CREDENTIAL"
			}

			marker, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
				CLIDir:  dir,
				Path:    skipPath,
				Reason:  reason,
				AuthEnv: "WIDGET_API_KEY",
			})
			require.NoError(t, err)
			assert.Equal(t, phase5SkipReasonAuthRequiredNoCredential, marker.SkipReason)
			assert.Equal(t, "skip", marker.Status)
			assert.Equal(t, manifest.APIName, marker.APIName)
			assert.Equal(t, manifest.RunID, marker.RunID)
			assert.Equal(t, authType, marker.AuthContext.Type)
			assert.False(t, marker.AuthContext.APIKeyAvailable)
			assert.NotEmpty(t, marker.SourceFingerprint)
			assert.NotEmpty(t, marker.SourceFiles)

			source, err := CaptureSourceFingerprint(dir)
			require.NoError(t, err)
			assert.Equal(t, source.Digest, marker.SourceFingerprint)
			assert.Equal(t, source.Files, marker.SourceFiles)

			onDisk := manifest
			onDisk.AuthType = authType
			checked := validatePhase5Marker(marker, onDisk, true, dir)
			require.True(t, checked.Passed, checked.Detail)

			state := NewStateWithRun(manifest.APIName, dir, manifest.RunID, "test-scope")
			require.NoError(t, state.Save())
			require.NoError(t, validatePhase5GateForPromote(dir, state))

			gate := ValidatePhase5Gate(filepath.Dir(skipPath), onDisk, dir)
			require.True(t, gate.Passed, gate.Detail)
		})
	}
}

func TestWritePhase5SkipMarkerStaysFreshAcrossREADMEAndStaleAfterGoEdit(t *testing.T) {
	phase5SkipTestHome(t)
	t.Setenv("WIDGET_API_KEY", "")
	manifest := phase5SkipManifest("api_key")
	dir := writePhase5SkipTree(t, manifest, nil)
	skipPath := phase5SkipPath(dir, manifest.RunID)
	_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
		CLIDir: dir,
		Path:   skipPath,
		Reason: phase5SkipReasonAuthRequiredNoCredential,
	})
	require.NoError(t, err)

	state := NewStateWithRun(manifest.APIName, dir, manifest.RunID, "test-scope")
	require.NoError(t, state.Save())
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("docs only\n"), 0o644))
	require.NoError(t, validatePhase5GateForPromote(dir, state))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() { println(\"changed\") }\n"), 0o644))
	err = validatePhase5GateForPromote(dir, state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source fingerprint")
	assert.Contains(t, err.Error(), "main.go")
}

func TestWritePhase5SkipMarkerRefusesAuthType(t *testing.T) {
	for _, authType := range []string{"none", "cookie", "composed", "session_handshake"} {
		t.Run(authType, func(t *testing.T) {
			phase5SkipTestHome(t)
			t.Setenv("WIDGET_API_KEY", "")
			manifest := phase5SkipManifest(authType)
			dir := writePhase5SkipTree(t, manifest, nil)
			skipPath := phase5SkipPath(dir, manifest.RunID)

			_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
				CLIDir:  dir,
				Path:    skipPath,
				Reason:  phase5SkipReasonAuthRequiredNoCredential,
				AuthEnv: "WIDGET_API_KEY",
			})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "WIDGET_API_KEY is set")
			switch authType {
			case "none":
				assert.Contains(t, err.Error(), "no-auth")
			default:
				assert.Contains(t, err.Error(), "browser-session")
			}
			_, statErr := os.Stat(skipPath)
			assert.True(t, os.IsNotExist(statErr))
		})
	}
}

func TestWritePhase5SkipMarkerRefusesWhenCredentialIsSet(t *testing.T) {
	phase5SkipTestHome(t)
	manifest := phase5SkipManifest("api_key")
	manifest.AuthEnvVarSpecs = []spec.AuthEnvVar{{
		Name:     "WIDGET_SPEC_TOKEN",
		Kind:     spec.AuthEnvVarKindPerCall,
		Required: true,
	}}
	dir := writePhase5SkipTree(t, manifest, nil)
	skipPath := phase5SkipPath(dir, manifest.RunID)
	opts := Phase5SkipWriteOptions{
		CLIDir:  dir,
		Path:    skipPath,
		Reason:  phase5SkipReasonAuthRequiredNoCredential,
		AuthEnv: "WIDGET_API_KEY",
	}

	t.Run("named env", func(t *testing.T) {
		t.Setenv("WIDGET_API_KEY", "secret-value")
		t.Setenv("WIDGET_SPEC_TOKEN", "")
		_, err := WritePhase5SkipMarker(opts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "credential env WIDGET_API_KEY is set")
		assert.NotContains(t, err.Error(), "secret-value")
		_, statErr := os.Stat(skipPath)
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("manifest var without --auth-env", func(t *testing.T) {
		t.Setenv("WIDGET_API_KEY", "secret-value")
		omitted := opts
		omitted.AuthEnv = ""
		_, err := WritePhase5SkipMarker(omitted)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "credential env WIDGET_API_KEY is set")
		_, statErr := os.Stat(skipPath)
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("decoy auth-env does not hide manifest credential", func(t *testing.T) {
		t.Setenv("WIDGET_API_KEY", "")
		t.Setenv("WIDGET_SPEC_TOKEN", "other-secret")
		t.Setenv("UNSET_DECOY", "")
		decoy := opts
		decoy.AuthEnv = "UNSET_DECOY"
		_, err := WritePhase5SkipMarker(decoy)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "credential env WIDGET_SPEC_TOKEN is set")
		assert.NotContains(t, err.Error(), "other-secret")
	})

	t.Run("whitespace is unset", func(t *testing.T) {
		t.Setenv("WIDGET_API_KEY", "   ")
		t.Setenv("WIDGET_SPEC_TOKEN", " \t")
		_, err := WritePhase5SkipMarker(opts)
		require.NoError(t, err)
	})
}

func TestWritePhase5SkipMarkerClientIDIsNotACredential(t *testing.T) {
	const (
		clientIDValue     = "public-client-id"
		clientSecretValue = "client-secret-value"
		refreshValue      = "refresh-token-value"
		accessValue       = "access-token-value"
		bearerValue       = "bearer-token-value"
		apiKeyValue       = "api-key-value"
		perCallValue      = "per-call-token-value"
		tenantValue       = "tenant-value"
		harvestedValue    = "harvested-value"
		customSecretValue = "custom-secret-value"
		legacyValue       = "legacy-token-value"
	)
	secretValues := []string{
		clientSecretValue, refreshValue, accessValue, bearerValue, apiKeyValue,
		perCallValue, harvestedValue, customSecretValue, legacyValue,
	}
	oauthVars := []string{"WIDGET_CLIENT_ID", "WIDGET_CLIENT_SECRET", "WIDGET_REFRESH_TOKEN"}
	publicClientID := spec.AuthEnvVar{Name: "WIDGET_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: false}
	clientSecret := spec.AuthEnvVar{Name: "WIDGET_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: true}
	refreshToken := spec.AuthEnvVar{Name: "WIDGET_REFRESH_TOKEN", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: true}
	oauthSpecs := []spec.AuthEnvVar{publicClientID, clientSecret, refreshToken}

	cases := []struct {
		name     string
		authType string
		envVars  []string
		specs    []spec.AuthEnvVar
		authEnv  string
		set      map[string]string
		wantErr  string
	}{
		{
			name:     "flat client id only",
			authType: "oauth2_refresh",
			envVars:  oauthVars,
			authEnv:  "WIDGET_CLIENT_ID",
			set:      map[string]string{"WIDGET_CLIENT_ID": clientIDValue},
		},
		{
			name:     "specs and flat list client id only",
			authType: "oauth2_refresh",
			envVars:  oauthVars,
			specs:    oauthSpecs,
			authEnv:  "WIDGET_CLIENT_ID",
			set:      map[string]string{"WIDGET_CLIENT_ID": clientIDValue},
		},
		{
			name:     "specs only client id",
			authType: "oauth2_refresh",
			specs:    oauthSpecs,
			set:      map[string]string{"WIDGET_CLIENT_ID": clientIDValue},
		},
		{
			name:     "oauth2 flat client id only",
			authType: "oauth2",
			envVars:  []string{"WIDGET_CLIENT_ID", "WIDGET_CLIENT_SECRET"},
			authEnv:  "WIDGET_CLIENT_ID",
			set:      map[string]string{"WIDGET_CLIENT_ID": clientIDValue},
		},
		{
			name:     "oauth2 specs and flat list client id only",
			authType: "oauth2",
			envVars:  []string{"WIDGET_CLIENT_ID", "WIDGET_CLIENT_SECRET"},
			specs:    []spec.AuthEnvVar{publicClientID, clientSecret},
			authEnv:  "WIDGET_CLIENT_ID",
			set:      map[string]string{"WIDGET_CLIENT_ID": clientIDValue},
		},
		{
			name:     "oauth2 specs only client id",
			authType: "oauth2",
			specs:    []spec.AuthEnvVar{publicClientID, clientSecret},
			set:      map[string]string{"WIDGET_CLIENT_ID": clientIDValue},
		},
		{
			name:     "api key flat client id is a credential",
			authType: "api_key",
			envVars:  []string{"WIDGET_API_KEY", "WIDGET_CLIENT_ID"},
			set:      map[string]string{"WIDGET_CLIENT_ID": clientIDValue},
			wantErr:  "credential env WIDGET_CLIENT_ID is set",
		},
		{
			name:     "api key per-call client id spec still blocks",
			authType: "api_key",
			specs: []spec.AuthEnvVar{{
				Name: "WIDGET_CLIENT_ID", Kind: spec.AuthEnvVarKindPerCall, Sensitive: false,
			}},
			set:     map[string]string{"WIDGET_CLIENT_ID": clientIDValue},
			wantErr: "credential env WIDGET_CLIENT_ID is set",
		},
		{
			name:     "api key setup client id spec is not the request credential",
			authType: "api_key",
			envVars:  []string{"WIDGET_API_KEY", "WIDGET_CLIENT_ID"},
			specs: []spec.AuthEnvVar{
				{Name: "WIDGET_API_KEY", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
				publicClientID,
			},
			authEnv: "WIDGET_CLIENT_ID",
			set:     map[string]string{"WIDGET_CLIENT_ID": clientIDValue},
		},
		{
			name:     "bearer flat client id is a credential",
			authType: "bearer_token",
			envVars:  []string{"WIDGET_CLIENT_ID"},
			authEnv:  "WIDGET_CLIENT_ID",
			set:      map[string]string{"WIDGET_CLIENT_ID": clientIDValue},
			wantErr:  "credential env WIDGET_CLIENT_ID is set",
		},
		{
			name:     "api key auth-env client id is a credential",
			authType: "api_key",
			envVars:  []string{"WIDGET_API_KEY"},
			authEnv:  "WIDGET_CLIENT_ID",
			set:      map[string]string{"WIDGET_CLIENT_ID": clientIDValue},
			wantErr:  "credential env WIDGET_CLIENT_ID is set",
		},
		{
			name:     "bearer auth-env client id is a credential",
			authType: "bearer_token",
			envVars:  nil,
			authEnv:  "WIDGET_CLIENT_ID",
			set:      map[string]string{"WIDGET_CLIENT_ID": clientIDValue},
			wantErr:  "credential env WIDGET_CLIENT_ID is set",
		},
		{
			name:     "api key specs do not hide a flat client id",
			authType: "api_key",
			envVars:  []string{"WIDGET_CLIENT_ID"},
			specs: []spec.AuthEnvVar{{
				Name: "WIDGET_SPEC_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true,
			}},
			set:     map[string]string{"WIDGET_CLIENT_ID": clientIDValue},
			wantErr: "credential env WIDGET_CLIENT_ID is set",
		},
		{
			name:     "lowercase client id name is still a credential",
			authType: "api_key",
			envVars:  []string{"widget_client_id"},
			authEnv:  "widget_client_id",
			set:      map[string]string{"widget_client_id": clientIDValue},
			wantErr:  "credential env widget_client_id is set",
		},
		{
			name:     "api key flat client id unset still skips",
			authType: "api_key",
			envVars:  []string{"WIDGET_CLIENT_ID"},
			authEnv:  "WIDGET_CLIENT_ID",
		},
		{
			name:     "api key flat client id whitespace still skips",
			authType: "api_key",
			envVars:  []string{"WIDGET_CLIENT_ID"},
			authEnv:  "WIDGET_CLIENT_ID",
			set:      map[string]string{"WIDGET_CLIENT_ID": "   "},
		},
		{
			name:     "non-sensitive auth flow input is setup",
			authType: "oauth2",
			envVars:  []string{"WIDGET_TENANT_ID"},
			specs: []spec.AuthEnvVar{{
				Name: "WIDGET_TENANT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Sensitive: false,
			}},
			set: map[string]string{"WIDGET_TENANT_ID": tenantValue},
		},
		{
			name:     "refresh token flat list",
			authType: "oauth2_refresh",
			envVars:  oauthVars,
			authEnv:  "WIDGET_CLIENT_ID",
			set: map[string]string{
				"WIDGET_CLIENT_ID":     clientIDValue,
				"WIDGET_REFRESH_TOKEN": refreshValue,
			},
			wantErr: "credential env WIDGET_REFRESH_TOKEN is set",
		},
		{
			name:     "refresh token auth flow input even when not sensitive",
			authType: "oauth2_refresh",
			envVars:  []string{"WIDGET_CLIENT_ID", "WIDGET_REFRESH_TOKEN"},
			specs: []spec.AuthEnvVar{
				publicClientID,
				{Name: "WIDGET_REFRESH_TOKEN", Kind: spec.AuthEnvVarKindAuthFlowInput, Sensitive: false},
			},
			authEnv: "WIDGET_CLIENT_ID",
			set: map[string]string{
				"WIDGET_CLIENT_ID":     clientIDValue,
				"WIDGET_REFRESH_TOKEN": refreshValue,
			},
			wantErr: "credential env WIDGET_REFRESH_TOKEN is set",
		},
		{
			name:     "client secret flat list",
			authType: "oauth2_refresh",
			envVars:  oauthVars,
			authEnv:  "WIDGET_CLIENT_ID",
			set: map[string]string{
				"WIDGET_CLIENT_ID":     clientIDValue,
				"WIDGET_CLIENT_SECRET": clientSecretValue,
			},
			wantErr: "credential env WIDGET_CLIENT_SECRET is set",
		},
		{
			name:     "client secret auth flow input even when not sensitive",
			authType: "oauth2",
			specs: []spec.AuthEnvVar{{
				Name: "WIDGET_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Sensitive: false,
			}},
			set:     map[string]string{"WIDGET_CLIENT_SECRET": clientSecretValue},
			wantErr: "credential env WIDGET_CLIENT_SECRET is set",
		},
		{
			name:     "access token",
			authType: "oauth2",
			envVars:  []string{"WIDGET_CLIENT_ID", "WIDGET_ACCESS_TOKEN"},
			authEnv:  "WIDGET_CLIENT_ID",
			set: map[string]string{
				"WIDGET_CLIENT_ID":    clientIDValue,
				"WIDGET_ACCESS_TOKEN": accessValue,
			},
			wantErr: "credential env WIDGET_ACCESS_TOKEN is set",
		},
		{
			name:     "bearer token",
			authType: "bearer_token",
			envVars:  []string{"WIDGET_BEARER_TOKEN"},
			authEnv:  "WIDGET_BEARER_TOKEN",
			set:      map[string]string{"WIDGET_BEARER_TOKEN": bearerValue},
			wantErr:  "credential env WIDGET_BEARER_TOKEN is set",
		},
		{
			name:     "api key",
			authType: "api_key",
			envVars:  []string{"WIDGET_API_KEY"},
			authEnv:  "WIDGET_API_KEY",
			set:      map[string]string{"WIDGET_API_KEY": apiKeyValue},
			wantErr:  "credential env WIDGET_API_KEY is set",
		},
		{
			name:     "named auth env",
			authType: "api_key",
			envVars:  []string{"WIDGET_CLIENT_ID"},
			authEnv:  "WIDGET_API_KEY",
			set:      map[string]string{"WIDGET_CLIENT_ID": clientIDValue, "WIDGET_API_KEY": apiKeyValue},
			wantErr:  "credential env WIDGET_API_KEY is set",
		},
		{
			name:     "spec per-call token",
			authType: "api_key",
			envVars:  []string{"WIDGET_API_KEY"},
			specs: []spec.AuthEnvVar{{
				Name: "WIDGET_SPEC_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true,
			}},
			set:     map[string]string{"WIDGET_SPEC_TOKEN": perCallValue},
			wantErr: "credential env WIDGET_SPEC_TOKEN is set",
		},
		{
			name:     "sensitive auth flow input",
			authType: "oauth2",
			specs: []spec.AuthEnvVar{{
				Name: "WIDGET_APP_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Sensitive: true,
			}},
			set:     map[string]string{"WIDGET_APP_SECRET": customSecretValue},
			wantErr: "credential env WIDGET_APP_SECRET is set",
		},
		{
			name:     "harvested spec",
			authType: "oauth2",
			specs: []spec.AuthEnvVar{{
				Name: "WIDGET_SESSION", Kind: spec.AuthEnvVarKindHarvested, Sensitive: true,
			}},
			set:     map[string]string{"WIDGET_SESSION": harvestedValue},
			wantErr: "credential env WIDGET_SESSION is set",
		},
		{
			name:     "unknown spec kind",
			authType: "oauth2",
			specs: []spec.AuthEnvVar{{
				Name: "WIDGET_OTHER", Kind: spec.AuthEnvVarKind("custom"), Sensitive: false,
			}},
			set:     map[string]string{"WIDGET_OTHER": customSecretValue},
			wantErr: "credential env WIDGET_OTHER is set",
		},
		{
			name:     "empty kind is a per-call credential",
			authType: "api_key",
			specs: []spec.AuthEnvVar{{
				Name: "WIDGET_LEGACY_TOKEN", Sensitive: false,
			}},
			set:     map[string]string{"WIDGET_LEGACY_TOKEN": legacyValue},
			wantErr: "credential env WIDGET_LEGACY_TOKEN is set",
		},
		{
			name:     "sensitive client id still blocks",
			authType: "oauth2_refresh",
			envVars:  []string{"WIDGET_CLIENT_ID"},
			specs: []spec.AuthEnvVar{{
				Name: "WIDGET_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Sensitive: true,
			}},
			authEnv: "WIDGET_CLIENT_ID",
			set:     map[string]string{"WIDGET_CLIENT_ID": clientIDValue},
			wantErr: "credential env WIDGET_CLIENT_ID is set",
		},
		{
			name:     "per-call client id still blocks",
			authType: "oauth2",
			specs: []spec.AuthEnvVar{{
				Name: "WIDGET_CLIENT_ID", Kind: spec.AuthEnvVarKindPerCall, Sensitive: false,
			}},
			set:     map[string]string{"WIDGET_CLIENT_ID": clientIDValue},
			wantErr: "credential env WIDGET_CLIENT_ID is set",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			phase5SkipTestHome(t)
			manifest := phase5SkipManifest(tc.authType)
			manifest.AuthEnvVars = tc.envVars
			manifest.AuthEnvVarSpecs = tc.specs
			dir := writePhase5SkipTree(t, manifest, nil)
			for _, name := range []string{
				"WIDGET_API_KEY", "WIDGET_CLIENT_ID", "widget_client_id", "WIDGET_CLIENT_SECRET", "WIDGET_REFRESH_TOKEN",
				"WIDGET_ACCESS_TOKEN", "WIDGET_BEARER_TOKEN", "WIDGET_SPEC_TOKEN", "WIDGET_TENANT_ID",
				"WIDGET_APP_SECRET", "WIDGET_SESSION", "WIDGET_OTHER", "WIDGET_LEGACY_TOKEN",
			} {
				t.Setenv(name, "")
			}
			if tc.authEnv != "" {
				t.Setenv(tc.authEnv, "")
			}
			for name, value := range tc.set {
				t.Setenv(name, value)
			}

			skipPath := phase5SkipPath(dir, manifest.RunID)
			marker, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
				CLIDir:  dir,
				Path:    skipPath,
				Reason:  phase5SkipReasonAuthRequiredNoCredential,
				AuthEnv: tc.authEnv,
			})
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				for _, secret := range secretValues {
					assert.NotContains(t, err.Error(), secret)
				}
				assert.NotContains(t, err.Error(), clientIDValue)
				assert.NotContains(t, err.Error(), tenantValue)
				_, statErr := os.Stat(skipPath)
				assert.True(t, os.IsNotExist(statErr))
				return
			}
			require.NoError(t, err)
			assert.False(t, marker.AuthContext.APIKeyAvailable)
			assert.Equal(t, phase5SkipReasonAuthRequiredNoCredential, marker.SkipReason)
			state := NewStateWithRun(manifest.APIName, dir, manifest.RunID, "test-scope")
			require.NoError(t, state.Save())
			require.NoError(t, validatePhase5GateForPromote(dir, state))
			gate := ValidatePhase5Gate(filepath.Dir(skipPath), manifest, dir)
			require.True(t, gate.Passed, gate.Detail)
		})
	}
}

func TestWritePhase5SkipMarkerFlatClientIDClassification(t *testing.T) {
	cases := []struct {
		authType string
		name     string
		want     bool
	}{
		{authType: "oauth2", name: "WIDGET_CLIENT_ID", want: false},
		{authType: "oauth2_refresh", name: "WIDGET_CLIENT_ID", want: false},
		{authType: " OAuth2 ", name: "WIDGET_CLIENT_ID", want: false},
		{authType: "api_key", name: "WIDGET_CLIENT_ID", want: true},
		{authType: "bearer_token", name: "WIDGET_CLIENT_ID", want: true},
		{authType: "none", name: "WIDGET_CLIENT_ID", want: true},
		{authType: "cookie", name: "WIDGET_CLIENT_ID", want: true},
		{authType: "", name: "WIDGET_CLIENT_ID", want: true},
		{authType: "api_key", name: "WIDGET_API_KEY", want: true},
		{authType: "oauth2", name: "WIDGET_CLIENT_SECRET", want: true},
		{authType: "oauth2_refresh", name: "WIDGET_REFRESH_TOKEN", want: true},
		{authType: "oauth2", name: "  ", want: false},
		{authType: "api_key", name: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.authType+"/"+tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, phase5SkipFlatNameIsUsableCredential(tc.authType, tc.name))
		})
	}
}

func TestWritePhase5SkipMarkerCoversSkillSkipReasons(t *testing.T) {
	t.Run("lan unreachable", func(t *testing.T) {
		phase5SkipTestHome(t)
		manifest := phase5SkipManifest("none")
		manifest.AuthEnvVars = nil
		dir := writePhase5SkipTree(t, manifest, nil)
		skipPath := phase5SkipPath(dir, manifest.RunID)
		marker, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
			CLIDir: dir,
			Path:   skipPath,
			Reason: phase5SkipReasonLANUnreachableFromHost,
		})
		require.NoError(t, err)
		assert.True(t, marker.AuthContext.LocalNetworkOnly)
		assert.Equal(t, "none", marker.AuthContext.Type)
		state := NewStateWithRun(manifest.APIName, dir, manifest.RunID, "test-scope")
		require.NoError(t, state.Save())
		require.NoError(t, validatePhase5GateForPromote(dir, state))
		gate := ValidatePhase5Gate(filepath.Dir(skipPath), manifest, dir)
		require.True(t, gate.Passed, gate.Detail)
	})

	t.Run("lan reason rejected for api key", func(t *testing.T) {
		phase5SkipTestHome(t)
		t.Setenv("WIDGET_API_KEY", "")
		manifest := phase5SkipManifest("api_key")
		dir := writePhase5SkipTree(t, manifest, nil)
		skipPath := phase5SkipPath(dir, manifest.RunID)
		_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
			CLIDir: dir,
			Path:   skipPath,
			Reason: phase5SkipReasonLANUnreachableFromHost,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not valid")
		assert.Contains(t, err.Error(), "api_key")
		_, statErr := os.Stat(skipPath)
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("publish free-form reason", func(t *testing.T) {
		phase5SkipTestHome(t)
		t.Setenv("WIDGET_API_KEY", "")
		manifest := phase5SkipManifest("bearer_token")
		dir := writePhase5SkipTree(t, manifest, nil)
		skipPath := phase5SkipPath(dir, manifest.RunID)
		marker, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
			CLIDir:  dir,
			Path:    skipPath,
			Reason:  "auth-unavailable",
			AuthEnv: "WIDGET_API_KEY",
		})
		require.NoError(t, err)
		assert.Equal(t, "auth-unavailable", marker.SkipReason)
		gate := ValidatePhase5Gate(filepath.Dir(skipPath), manifest, dir)
		require.True(t, gate.Passed, gate.Detail)
	})

	t.Run("upstream outage is not a no-auth skip", func(t *testing.T) {
		phase5SkipTestHome(t)
		manifest := phase5SkipManifest("none")
		manifest.AuthEnvVars = nil
		dir := writePhase5SkipTree(t, manifest, nil)
		skipPath := phase5SkipPath(dir, manifest.RunID)
		_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
			CLIDir: dir,
			Path:   skipPath,
			Reason: "upstream outage",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no-auth")
		_, statErr := os.Stat(skipPath)
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("synthetic external credentials", func(t *testing.T) {
		phase5SkipTestHome(t)
		manifest := phase5SkipManifest("none")
		manifest.AuthEnvVars = nil
		manifest.SpecKind = spec.KindSynthetic
		dir := writePhase5SkipTree(t, manifest, nil)
		skipPath := phase5SkipPath(dir, manifest.RunID)
		marker, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
			CLIDir: dir,
			Path:   skipPath,
			Reason: phase5SkipReasonExternalCredentialsUnavailable,
		})
		require.NoError(t, err)
		assert.Equal(t, phase5SkipReasonExternalCredentialsUnavailable, marker.SkipReason)
		gate := ValidatePhase5Gate(filepath.Dir(skipPath), manifest, dir)
		require.True(t, gate.Passed, gate.Detail)
	})

	t.Run("synthetic external credentials refuse a set env", func(t *testing.T) {
		phase5SkipTestHome(t)
		t.Setenv("WIDGET_API_KEY", "present")
		manifest := phase5SkipManifest("none")
		manifest.SpecKind = spec.KindSynthetic
		dir := writePhase5SkipTree(t, manifest, nil)
		_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
			CLIDir: dir,
			Path:   phase5SkipPath(dir, manifest.RunID),
			Reason: phase5SkipReasonExternalCredentialsUnavailable,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "credential env WIDGET_API_KEY is set")
		assert.NotContains(t, err.Error(), "present")
	})

	t.Run("synthetic external credentials refuse a set client id", func(t *testing.T) {
		phase5SkipTestHome(t)
		t.Setenv("WIDGET_API_KEY", "")
		t.Setenv("WIDGET_CLIENT_ID", "public-client-id")
		manifest := phase5SkipManifest("none")
		manifest.AuthEnvVars = []string{"WIDGET_CLIENT_ID"}
		manifest.SpecKind = spec.KindSynthetic
		dir := writePhase5SkipTree(t, manifest, nil)
		_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
			CLIDir:  dir,
			Path:    phase5SkipPath(dir, manifest.RunID),
			Reason:  phase5SkipReasonExternalCredentialsUnavailable,
			AuthEnv: "WIDGET_CLIENT_ID",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "credential env WIDGET_CLIENT_ID is set")
		assert.NotContains(t, err.Error(), "public-client-id")
	})

	t.Run("standard no-auth rejects external credentials", func(t *testing.T) {
		phase5SkipTestHome(t)
		manifest := phase5SkipManifest("none")
		manifest.AuthEnvVars = nil
		dir := writePhase5SkipTree(t, manifest, nil)
		_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
			CLIDir: dir,
			Path:   phase5SkipPath(dir, manifest.RunID),
			Reason: phase5SkipReasonExternalCredentialsUnavailable,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no-auth")
	})

	t.Run("local datastore skip promote cannot see spec format", func(t *testing.T) {
		// lock promote's manifest view omits spec_format, so this reason
		// still fails the promote gate. The writer must not emit it.
		phase5SkipTestHome(t)
		manifest := phase5SkipManifest("none")
		manifest.AuthEnvVars = nil
		manifest.SpecFormat = "sqlite"
		dir := writePhase5SkipTree(t, manifest, nil)
		skipPath := phase5SkipPath(dir, manifest.RunID)
		_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
			CLIDir: dir,
			Path:   skipPath,
			Reason: phase5SkipReasonLocalSourceRequiresDatabase,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no-auth")
		_, statErr := os.Stat(skipPath)
		assert.True(t, os.IsNotExist(statErr))
	})
}

func TestWritePhase5SkipMarkerInputErrors(t *testing.T) {
	phase5SkipTestHome(t)
	t.Setenv("WIDGET_API_KEY", "")
	manifest := phase5SkipManifest("api_key")
	dir := writePhase5SkipTree(t, manifest, nil)
	validPath := phase5SkipPath(dir, manifest.RunID)

	t.Run("empty dir", func(t *testing.T) {
		_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{Path: validPath, Reason: phase5SkipReasonAuthRequiredNoCredential})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "CLI directory is empty")
	})
	t.Run("empty path", func(t *testing.T) {
		_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{CLIDir: dir, Reason: phase5SkipReasonAuthRequiredNoCredential})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path is empty")
	})
	t.Run("wrong filename", func(t *testing.T) {
		_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
			CLIDir: dir,
			Path:   filepath.Join(dir, Phase5AcceptanceFilename),
			Reason: phase5SkipReasonAuthRequiredNoCredential,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), Phase5SkipFilename)
	})
	t.Run("empty reason", func(t *testing.T) {
		_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{CLIDir: dir, Path: validPath, Reason: "  "})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reason is empty")
	})
	t.Run("missing manifest", func(t *testing.T) {
		empty := t.TempDir()
		_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
			CLIDir: empty,
			Path:   filepath.Join(empty, Phase5SkipFilename),
			Reason: phase5SkipReasonAuthRequiredNoCredential,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "manifest")
	})
	t.Run("cookie harness reason", func(t *testing.T) {
		_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
			CLIDir: dir,
			Path:   validPath,
			Reason: phase5SkipReasonCookieAuthNoHarnessSession,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--write-skip")
		assert.Contains(t, err.Error(), "dogfood --live")
		_, statErr := os.Stat(validPath)
		assert.True(t, os.IsNotExist(statErr))
	})
	t.Run("proofs parent is a file", func(t *testing.T) {
		blocked := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(blocked, "go.mod"), []byte("module widget-pp-cli\n\ngo 1.26\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(blocked, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644))
		require.NoError(t, WriteCLIManifest(blocked, manifest))
		require.NoError(t, os.WriteFile(filepath.Join(blocked, ".manuscripts"), []byte("not a directory\n"), 0o644))
		_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
			CLIDir: blocked,
			Path:   phase5SkipPath(blocked, manifest.RunID),
			Reason: phase5SkipReasonAuthRequiredNoCredential,
		})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "phase5 skip refused")
	})
	t.Run("symlink root", func(t *testing.T) {
		link := filepath.Join(t.TempDir(), "cli")
		if err := os.Symlink(dir, link); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
			CLIDir: link,
			Path:   filepath.Join(t.TempDir(), Phase5SkipFilename),
			Reason: phase5SkipReasonAuthRequiredNoCredential,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "symlink")
	})
}

func TestWritePhase5SkipMarkerRemovesShadowedAcceptance(t *testing.T) {
	phase5SkipTestHome(t)
	t.Setenv("WIDGET_API_KEY", "")
	manifest := phase5SkipManifest("api_key")
	dir := writePhase5SkipTree(t, manifest, nil)
	skipPath := phase5SkipPath(dir, manifest.RunID)
	acceptance := filepath.Join(filepath.Dir(skipPath), Phase5AcceptanceFilename)
	writePhase5GateMarker(t, filepath.Dir(skipPath), Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       manifest.APIName,
		RunID:         manifest.RunID,
		Status:        "fail",
		Level:         "full",
	})

	_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
		CLIDir: dir,
		Path:   skipPath,
		Reason: phase5SkipReasonAuthRequiredNoCredential,
	})
	require.NoError(t, err)
	_, statErr := os.Stat(acceptance)
	assert.True(t, os.IsNotExist(statErr))
	gate := ValidatePhase5Gate(filepath.Dir(skipPath), manifest, dir)
	require.True(t, gate.Passed, gate.Detail)
	assert.Equal(t, "skip", gate.Status)
}

func TestWritePhase5SkipMarkerRemovesMirroredAcceptance(t *testing.T) {
	phase5SkipTestHome(t)
	t.Setenv("WIDGET_API_KEY", "")
	manifest := phase5SkipManifest("api_key")
	dir := writePhase5SkipTree(t, manifest, nil)
	state := NewStateWithRun(manifest.APIName, dir, manifest.RunID, "test-scope")
	require.NoError(t, state.Save())
	writePhase5GateMarker(t, state.ProofsDir(), Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       manifest.APIName,
		RunID:         manifest.RunID,
		Status:        "fail",
		Level:         "full",
	})

	skipPath := phase5SkipPath(dir, manifest.RunID)
	_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
		CLIDir: dir,
		Path:   skipPath,
		Reason: phase5SkipReasonAuthRequiredNoCredential,
	})
	require.NoError(t, err)
	_, statErr := os.Stat(filepath.Join(state.ProofsDir(), Phase5AcceptanceFilename))
	assert.True(t, os.IsNotExist(statErr))
	_, err = os.Stat(filepath.Join(state.ProofsDir(), Phase5SkipFilename))
	assert.NoError(t, err)
	require.NoError(t, validatePhase5GateForPromote(dir, state))
}

func TestWritePhase5SkipMarkerReportsShadowedAcceptanceRemovalFailure(t *testing.T) {
	phase5SkipTestHome(t)
	t.Setenv("WIDGET_API_KEY", "")
	manifest := phase5SkipManifest("api_key")
	dir := writePhase5SkipTree(t, manifest, nil)
	skipPath := phase5SkipPath(dir, manifest.RunID)
	acceptance := filepath.Join(filepath.Dir(skipPath), Phase5AcceptanceFilename)
	require.NoError(t, os.MkdirAll(acceptance, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(acceptance, "keep"), []byte("x"), 0o644))

	_, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
		CLIDir: dir,
		Path:   skipPath,
		Reason: phase5SkipReasonAuthRequiredNoCredential,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "removing shadowed")
	assert.Contains(t, err.Error(), Phase5AcceptanceFilename)
}

func TestHandWrittenPhase5SkipWithoutFingerprintRejected(t *testing.T) {
	phase5SkipTestHome(t)
	manifest := phase5SkipManifest("api_key")
	dir := writePhase5SkipTree(t, manifest, nil)
	proofs := filepath.Dir(phase5SkipPath(dir, manifest.RunID))
	writePhase5GateMarker(t, proofs, Phase5SkipFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       manifest.APIName,
		RunID:         manifest.RunID,
		Status:        "skip",
		Level:         "none",
		SkipReason:    phase5SkipReasonAuthRequiredNoCredential,
		AuthContext:   Phase5AuthContext{Type: "api_key"},
	})
	state := NewStateWithRun(manifest.APIName, dir, manifest.RunID, "test-scope")
	require.NoError(t, state.Save())

	err := validatePhase5GateForPromote(dir, state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "phase5 marker missing source_fingerprint")
}

func TestWritePhase5SkipDigestMatchesCaptureSourceFingerprint(t *testing.T) {
	moduleFiles := map[string]string{
		"go.mod":                    "module widget-pp-cli\n\ngo 1.26\n",
		"internal/client/client.go": "package client\n\ntype Client struct{}\n",
		"internal/cli/root.go": `package cli

import "widget-pp-cli/internal/client"

func Use() { _ = client.Client{} }
`,
	}
	publishedFiles := map[string]string{
		"go.mod":                    "module github.com/mvanhorn/printing-press-library/library/marketing/widget\n\ngo 1.26\n",
		"internal/client/client.go": "package client\n\ntype Client struct{}\n",
		"internal/cli/root.go": `package cli

import "github.com/mvanhorn/printing-press-library/library/marketing/widget/internal/client"

func Use() { _ = client.Client{} }
`,
	}
	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{name: "module path", files: moduleFiles},
		{name: "published library module path", files: publishedFiles},
	} {
		t.Run(tc.name, func(t *testing.T) {
			phase5SkipTestHome(t)
			t.Setenv("WIDGET_API_KEY", "")
			manifest := phase5SkipManifest("api_key")
			dir := writePhase5SkipTree(t, manifest, tc.files)
			marker, err := WritePhase5SkipMarker(Phase5SkipWriteOptions{
				CLIDir: dir,
				Path:   phase5SkipPath(dir, manifest.RunID),
				Reason: phase5SkipReasonAuthRequiredNoCredential,
			})
			require.NoError(t, err)
			source, err := CaptureSourceFingerprint(dir)
			require.NoError(t, err)
			assert.Equal(t, source.Digest, marker.SourceFingerprint)
			assert.Equal(t, source.Files, marker.SourceFiles)
			assert.NotEqual(t, rawSourceFingerprintDigest(t, dir, source.Files), marker.SourceFingerprint)
		})
	}
}

func rawSourceFingerprintDigest(t *testing.T, root string, files map[string]string) string {
	t.Helper()
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		require.NoError(t, err)
		sum := sha256.Sum256(data)
		_, _ = hash.Write([]byte(rel))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(hex.EncodeToString(sum[:])))
		_, _ = hash.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
