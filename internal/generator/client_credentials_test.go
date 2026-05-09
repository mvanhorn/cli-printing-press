package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestClientCredentials_AuthHeaderCallsMint verifies that when oauth2_grant
// is "client_credentials", the generated client.go authHeader() calls
// mintClientCredentials (not just refreshAccessToken) — Patch 1 upstream fix.
func TestClientCredentials_AuthHeaderCallsMint(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:      "sf-cc-test",
		Version:   "0.1.0",
		BaseURL:   "https://myinstance.salesforce.com",
		Owner:     "test-owner",
		OwnerName: "Test Author",
		Auth: spec.AuthConfig{
			Type:        "oauth2",
			OAuth2Grant: spec.OAuth2GrantClientCredentials,
			TokenURL:    "https://myinstance.salesforce.com/services/oauth2/token",
			EnvVars:     []string{"SALESFORCE_CLIENT_ID", "SALESFORCE_CLIENT_SECRET"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/sf-cc-test-pp-cli/config.toml",
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

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	content := string(clientSrc)

	// mintClientCredentials must be defined in client.go
	if !strings.Contains(content, "func (c *Client) mintClientCredentials") {
		t.Error("client.go should define mintClientCredentials when oauth2_grant: client_credentials")
	}
	// needsClientCredentialsMint helper must be present
	if !strings.Contains(content, "needsClientCredentialsMint") {
		t.Error("client.go should contain needsClientCredentialsMint helper")
	}
	// authHeader() must call mintClientCredentials (not only fallback to refresh_token branch)
	// Check by looking for the double-checked lock pattern that wraps the mint call
	if !strings.Contains(content, "c.mintClientCredentials(clientID, clientSecret)") {
		t.Error("authHeader() should call mintClientCredentials")
	}
}

// TestClientCredentials_NoMintForAuthCodeGrant verifies that the
// authorization_code grant path does NOT emit mintClientCredentials.
func TestClientCredentials_NoMintForAuthCodeGrant(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:      "auth-code-test",
		Version:   "0.1.0",
		BaseURL:   "https://api.example.com",
		Owner:     "test-owner",
		OwnerName: "Test Author",
		Auth: spec.AuthConfig{
			Type:             "oauth2",
			OAuth2Grant:      spec.OAuth2GrantAuthorizationCode,
			TokenURL:         "https://api.example.com/oauth/token",
			AuthorizationURL: "https://api.example.com/oauth/authorize",
			EnvVars:          []string{"EXAMPLE_CLIENT_ID", "EXAMPLE_CLIENT_SECRET"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/auth-code-test-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	content := string(clientSrc)

	// mintClientCredentials must NOT be present for authorization_code
	if strings.Contains(content, "mintClientCredentials") {
		t.Error("authorization_code spec should NOT contain mintClientCredentials")
	}
}
