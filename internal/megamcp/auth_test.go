package megamcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyAuthFormat(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		envVars map[string]string
		want    string
		wantErr string
	}{
		{
			name:    "simple substitution",
			format:  "{API_KEY}",
			envVars: map[string]string{"API_KEY": "my-secret-key"},
			want:    "my-secret-key",
		},
		{
			name:    "bearer format",
			format:  "Bearer {DUB_TOKEN}",
			envVars: map[string]string{"DUB_TOKEN": "abc123"},
			want:    "Bearer abc123",
		},
		{
			name:    "semantic placeholder token",
			format:  "Bearer {token}",
			envVars: map[string]string{"SOME_TOKEN": "xyz789"},
			want:    "Bearer xyz789",
		},
		{
			name:    "semantic placeholder access_token",
			format:  "Bearer {access_token}",
			envVars: map[string]string{"MY_TOKEN": "tok456"},
			want:    "Bearer tok456",
		},
		{
			name:    "empty format returns first value",
			format:  "",
			envVars: map[string]string{"KEY": "val"},
			want:    "val",
		},
		{
			name:    "unrecognized placeholder rejected",
			format:  "Bearer {UNKNOWN_VAR}",
			envVars: map[string]string{"API_KEY": "secret"},
			wantErr: "unrecognized placeholder {UNKNOWN_VAR}",
		},
		{
			name:    "empty env vars with format",
			format:  "Bearer {token}",
			envVars: map[string]string{},
			wantErr: "unrecognized placeholder {token}",
		},
		{
			name:    "empty env vars no format",
			format:  "",
			envVars: map[string]string{},
			want:    "",
		},
		{
			name:    "value with closing brace rejected",
			format:  "Bearer {token}",
			envVars: map[string]string{"token": "abc}123"},
			wantErr: "invalid character }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyAuthFormat(tt.format, tt.envVars)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestBuildAuthHeader(t *testing.T) {
	tests := []struct {
		name       string
		manifest   *ToolsManifest
		envSetup   map[string]string
		wantHeader string
		wantValue  string
		wantErr    bool
	}{
		{
			name:     "nil manifest",
			manifest: nil,
		},
		{
			name: "auth type none",
			manifest: &ToolsManifest{
				Auth: ManifestAuth{Type: "none"},
			},
		},
		{
			name: "api_key with format",
			manifest: &ToolsManifest{
				Auth: ManifestAuth{
					Type:    "api_key",
					Header:  "X-Api-Key",
					Format:  "{MY_API_KEY}",
					EnvVars: []string{"MY_API_KEY"},
				},
			},
			envSetup:   map[string]string{"MY_API_KEY": "secret123"},
			wantHeader: "X-Api-Key",
			wantValue:  "secret123",
		},
		{
			name: "bearer_token no format defaults to Bearer prefix",
			manifest: &ToolsManifest{
				Auth: ManifestAuth{
					Type:    "bearer_token",
					EnvVars: []string{"BEARER_TOK"},
				},
			},
			envSetup:   map[string]string{"BEARER_TOK": "tok999"},
			wantHeader: "Authorization",
			wantValue:  "Bearer tok999",
		},
		{
			name: "bearer_token with custom format",
			manifest: &ToolsManifest{
				Auth: ManifestAuth{
					Type:    "bearer_token",
					Header:  "Authorization",
					Format:  "Token {GITHUB_TOKEN}",
					EnvVars: []string{"GITHUB_TOKEN"},
				},
			},
			envSetup:   map[string]string{"GITHUB_TOKEN": "ghp_abc"},
			wantHeader: "Authorization",
			wantValue:  "Token ghp_abc",
		},
		{
			name: "env var not set returns empty",
			manifest: &ToolsManifest{
				Auth: ManifestAuth{
					Type:    "api_key",
					Header:  "X-Api-Key",
					EnvVars: []string{"MISSING_KEY"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up env vars.
			for k, v := range tt.envSetup {
				t.Setenv(k, v)
			}

			hdr, val, err := BuildAuthHeader(tt.manifest)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantHeader, hdr)
				assert.Equal(t, tt.wantValue, val)
			}
		})
	}
}

func TestBuildAuthQueryParam(t *testing.T) {
	tests := []struct {
		name      string
		manifest  *ToolsManifest
		envSetup  map[string]string
		wantName  string
		wantValue string
	}{
		{
			name:     "nil manifest",
			manifest: nil,
		},
		{
			name: "in header not query",
			manifest: &ToolsManifest{
				Auth: ManifestAuth{
					Type: "api_key",
					In:   "header",
				},
			},
		},
		{
			name: "api key in query",
			manifest: &ToolsManifest{
				Auth: ManifestAuth{
					Type:    "api_key",
					Header:  "key",
					In:      "query",
					EnvVars: []string{"ESPN_KEY"},
				},
			},
			envSetup:  map[string]string{"ESPN_KEY": "qwerty"},
			wantName:  "key",
			wantValue: "qwerty",
		},
		{
			name: "query auth defaults to api_key param name",
			manifest: &ToolsManifest{
				Auth: ManifestAuth{
					Type:    "api_key",
					In:      "query",
					EnvVars: []string{"MY_KEY"},
				},
			},
			envSetup:  map[string]string{"MY_KEY": "abc"},
			wantName:  "api_key",
			wantValue: "abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envSetup {
				t.Setenv(k, v)
			}

			name, val, err := BuildAuthQueryParam(tt.manifest)
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, name)
			assert.Equal(t, tt.wantValue, val)
		})
	}
}

func TestRedactCredentials(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		manifest *ToolsManifest
		envSetup map[string]string
		want     string
	}{
		{
			name:     "nil manifest returns body unchanged",
			body:     "error: bad token abc123",
			manifest: nil,
			want:     "error: bad token abc123",
		},
		{
			name: "credential value redacted",
			body: "Invalid API key: sk-secret-key-here is not valid",
			manifest: &ToolsManifest{
				Auth: ManifestAuth{
					Type:    "api_key",
					EnvVars: []string{"MY_KEY"},
				},
			},
			envSetup: map[string]string{"MY_KEY": "sk-secret-key-here"},
			want:     "Invalid API key: [REDACTED] is not valid",
		},
		{
			name: "short credential not redacted to avoid false positives",
			body: "error: bad key ab",
			manifest: &ToolsManifest{
				Auth: ManifestAuth{
					Type:    "api_key",
					EnvVars: []string{"SHORT_KEY"},
				},
			},
			envSetup: map[string]string{"SHORT_KEY": "ab"},
			want:     "error: bad key ab",
		},
		{
			name: "multiple env vars redacted",
			body: "key1=first-secret key2=second-secret",
			manifest: &ToolsManifest{
				Auth: ManifestAuth{
					Type:    "api_key",
					EnvVars: []string{"KEY1", "KEY2"},
				},
			},
			envSetup: map[string]string{"KEY1": "first-secret", "KEY2": "second-secret"},
			want:     "key1=[REDACTED] key2=[REDACTED]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envSetup {
				t.Setenv(k, v)
			}

			got := RedactCredentials(tt.body, tt.manifest)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHasAuthConfigured(t *testing.T) {
	tests := []struct {
		name     string
		manifest *ToolsManifest
		envSetup map[string]string
		want     bool
	}{
		{
			name:     "nil manifest",
			manifest: nil,
			want:     true,
		},
		{
			name: "no auth type",
			manifest: &ToolsManifest{
				Auth: ManifestAuth{Type: "none"},
			},
			want: true,
		},
		{
			name: "env var set",
			manifest: &ToolsManifest{
				Auth: ManifestAuth{
					Type:    "api_key",
					EnvVars: []string{"MY_KEY"},
				},
			},
			envSetup: map[string]string{"MY_KEY": "secret"},
			want:     true,
		},
		{
			name: "env var not set",
			manifest: &ToolsManifest{
				Auth: ManifestAuth{
					Type:    "api_key",
					EnvVars: []string{"MISSING_KEY"},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envSetup {
				t.Setenv(k, v)
			}

			got := hasAuthConfigured(tt.manifest)
			assert.Equal(t, tt.want, got)
		})
	}
}
