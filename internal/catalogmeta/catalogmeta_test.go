package catalogmeta

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/catalog"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyRuntimeMetadataAuthEnvVarsOverridesSpec(t *testing.T) {
	apiSpec := &spec.APISpec{
		Auth: spec.AuthConfig{
			Type:    "bearer_token",
			Header:  "Authorization",
			EnvVars: []string{"STRIPE_BEARER_AUTH"},
			EnvVarSpecs: []spec.AuthEnvVar{
				{Name: "STRIPE_BEARER_AUTH", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true, Inferred: true},
			},
		},
	}
	entry := &catalog.Entry{
		Name:        "stripe",
		AuthEnvVars: []string{"STRIPE_SECRET_KEY", "STRIPE_API_KEY"},
	}

	ApplyRuntimeMetadata(apiSpec, entry)

	assert.Equal(t, []string{"STRIPE_SECRET_KEY", "STRIPE_API_KEY"}, apiSpec.Auth.EnvVars)
	assert.Empty(t, apiSpec.Auth.EnvVarSpecs, "EnvVarSpecs should be cleared so NormalizeEnvVarSpecs rebuilds from the new EnvVars")

	apiSpec.Auth.NormalizeEnvVarSpecs("")
	require.Len(t, apiSpec.Auth.EnvVarSpecs, 2)
	assert.Equal(t, "STRIPE_SECRET_KEY", apiSpec.Auth.EnvVarSpecs[0].Name)
	assert.Equal(t, "STRIPE_API_KEY", apiSpec.Auth.EnvVarSpecs[1].Name)
	assert.True(t, apiSpec.Auth.EnvVarSpecs[0].Inferred)
}

func TestApplyRuntimeMetadataAuthEnvVarsEmptyLeavesSpecUntouched(t *testing.T) {
	apiSpec := &spec.APISpec{
		Auth: spec.AuthConfig{
			EnvVars: []string{"FOO_BEARER_AUTH"},
		},
	}
	entry := &catalog.Entry{Name: "foo"}

	ApplyRuntimeMetadata(apiSpec, entry)

	assert.Equal(t, []string{"FOO_BEARER_AUTH"}, apiSpec.Auth.EnvVars)
}
