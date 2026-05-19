package catalogmeta

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

func TestApplyCatalogAuthEnvVars_PrependsCatalogNamesAndPreservesFallback(t *testing.T) {
	auth := &spec.AuthConfig{
		Type:    "bearer_token",
		EnvVars: []string{"STRIPE_BEARER_AUTH"},
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "STRIPE_BEARER_AUTH", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true, Inferred: true},
		},
	}

	ApplyCatalogAuthEnvVars(auth, []string{"STRIPE_SECRET_KEY", "STRIPE_API_KEY"})

	assert.Equal(t, []string{"STRIPE_SECRET_KEY", "STRIPE_API_KEY", "STRIPE_BEARER_AUTH"}, auth.EnvVars)
	require.Len(t, auth.EnvVarSpecs, 3)
	for _, ev := range auth.EnvVarSpecs {
		assert.Equal(t, spec.AuthEnvVarKindPerCall, ev.Kind)
		assert.False(t, ev.Required, "OR-case entries must not be Required for IsAuthEnvVarORCase to fire")
		assert.True(t, ev.Sensitive)
	}
	assert.True(t, auth.IsAuthEnvVarORCase(), "rebuilt EnvVarSpecs should produce OR-case auth")
}

func TestApplyCatalogAuthEnvVars_NoopWhenCatalogListEmpty(t *testing.T) {
	auth := &spec.AuthConfig{
		Type:    "bearer_token",
		EnvVars: []string{"STRIPE_BEARER_AUTH"},
	}

	ApplyCatalogAuthEnvVars(auth, nil)

	assert.Equal(t, []string{"STRIPE_BEARER_AUTH"}, auth.EnvVars)
}

func TestApplyCatalogAuthEnvVars_NoopWhenAuthTypeNone(t *testing.T) {
	auth := &spec.AuthConfig{Type: "none"}

	ApplyCatalogAuthEnvVars(auth, []string{"FOO"})

	assert.Empty(t, auth.EnvVars)
	assert.Empty(t, auth.EnvVarSpecs)
}

func TestApplyCatalogAuthEnvVars_DedupesBetweenCatalogAndExisting(t *testing.T) {
	auth := &spec.AuthConfig{
		Type:    "bearer_token",
		EnvVars: []string{"GITHUB_TOKEN", "GITHUB_BEARER_AUTH"},
	}

	ApplyCatalogAuthEnvVars(auth, []string{"GITHUB_TOKEN", "GH_TOKEN"})

	assert.Equal(t, []string{"GITHUB_TOKEN", "GH_TOKEN", "GITHUB_BEARER_AUTH"}, auth.EnvVars)
}

func TestApplyCatalogAuthEnvVars_NoopForBasicAuth(t *testing.T) {
	// Basic auth treats env vars as a credential pair, not as alternatives.
	// Stacking an OR-case list on basic auth would produce config.go that
	// returns an empty Authorization header, so the override bails out.
	auth := &spec.AuthConfig{
		Type:    "api_key",
		Format:  "Basic {credentials}",
		EnvVars: []string{"STYTCH_PROJECT_ID", "STYTCH_SECRET"},
	}

	ApplyCatalogAuthEnvVars(auth, []string{"STYTCH_API_KEY"})

	assert.Equal(t, []string{"STYTCH_PROJECT_ID", "STYTCH_SECRET"}, auth.EnvVars,
		"basic-auth env vars must survive a catalog override attempt")
	assert.Empty(t, auth.EnvVarSpecs, "EnvVarSpecs must not be rebuilt for basic-auth specs")
}

func TestApplyCatalogAuthEnvVars_TrimsWhitespaceAndSkipsEmpty(t *testing.T) {
	auth := &spec.AuthConfig{
		Type:    "bearer_token",
		EnvVars: []string{"  ", "DISCORD_BEARER_AUTH"},
	}

	ApplyCatalogAuthEnvVars(auth, []string{"  DISCORD_TOKEN  ", "DISCORD_BOT_TOKEN"})

	assert.Equal(t, []string{"DISCORD_TOKEN", "DISCORD_BOT_TOKEN", "DISCORD_BEARER_AUTH"}, auth.EnvVars)
}
