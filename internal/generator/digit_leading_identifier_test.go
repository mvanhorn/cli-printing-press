package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandIdentPrefixesDigitLeadingSegments(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "User2fa", commandIdent("user-2fa"))
	assert.Equal(t, "User2fa", commandIdent("user", "2fa"))
	assert.Equal(t, "UserV2fa", commandIdent("user", "v2fa"))
	assert.Equal(t, "V3dSecure", commandIdent("3d_secure"))
	assert.Equal(t, "Oauth2Token", commandIdent("oauth2-token"))
}

func TestGenerateDigitLeadingPathSegmentsUseConsistentCommandIdentifiers(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("digit-leading")
	apiSpec.Resources = map[string]spec.Resource{
		"user": {
			Description: "Manage users",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/user/{userId}",
					Description: "Get user",
					Params: []spec.Param{
						{Name: "userId", Type: "string", Required: true, Positional: true},
					},
				},
			},
			SubResources: map[string]spec.Resource{
				"2fa": {
					Description: "Manage two-factor authentication",
					Endpoints: map[string]spec.Endpoint{
						"get-user2fa": {
							Method:      "GET",
							Path:        "/user/{userId}/2fa",
							Description: "Get user two-factor authentication status",
							Params: []spec.Param{
								{Name: "userId", Type: "string", Required: true, Positional: true},
							},
						},
					},
				},
				"v2fa": {
					Description: "Manage versioned two-factor authentication",
					Endpoints: map[string]spec.Endpoint{
						"get": {
							Method:      "GET",
							Path:        "/user/{userId}/v2fa",
							Description: "Get versioned two-factor authentication status",
							Params: []spec.Param{
								{Name: "userId", Type: "string", Required: true, Positional: true},
							},
						},
					},
				},
			},
		},
		"3d-secure": {
			Description: "Manage 3D Secure records",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/v1/3d_secure",
					Description: "List 3D Secure records",
				},
			},
		},
	}

	// Post-flip: opt out so this test exercises the non-learn shape it asserts.
	apiSpec.Learn.Disabled = true

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Export: true}
	require.NoError(t, gen.Generate())

	promotedUser := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_user.go")
	user2FAParent := readGeneratedFile(t, outputDir, "internal", "cli", "user_2fa.go")
	userV2FAParent := readGeneratedFile(t, outputDir, "internal", "cli", "user_v2fa.go")
	require.Contains(t, promotedUser, "sub := newUser2faCmd(flags)")
	require.Contains(t, promotedUser, "sub := newUserV2faCmd(flags)")
	require.Contains(t, user2FAParent, "func newUser2faCmd(flags *rootFlags) *cobra.Command")
	require.Contains(t, userV2FAParent, "func newUserV2faCmd(flags *rootFlags) *cobra.Command")

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	promoted3DSecure := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_3d-secure.go")
	require.Contains(t, root, "rootCmd.AddCommand(newV3dSecurePromotedCmd(flags))")
	require.Contains(t, promoted3DSecure, "func newV3dSecurePromotedCmd(flags *rootFlags) *cobra.Command")
	assert.NotContains(t, root, "new3dSecure", "digit-leading resource must not produce an invalid constructor")

	runGoCommand(t, outputDir, "build", "./internal/cli")
}
