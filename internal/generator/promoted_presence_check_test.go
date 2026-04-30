package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/require"
)

// Ensures promoted commands emit presence checks against the flag's
// declared type — which is string for ID-promoted ints — not the
// spec's original type. Regression guard for #189.
func TestPromotedPresenceCheckUsesPromotedType(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("steam-like")
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Items",
		Endpoints: map[string]spec.Endpoint{
			"get": {
				Method:      "GET",
				Path:        "/items/get",
				Description: "Get items for a Steam account",
				Params: []spec.Param{
					{Name: "steamid", Type: "int", Required: false, Description: "Steam ID (64-bit)"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "steam-like-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "promoted_items.go"))
	require.NoError(t, err)

	require.Contains(t, string(src), `flagSteamid != ""`,
		"flag declared as string (ID promotion) must be compared against string zero")
	require.NotContains(t, string(src), "flagSteamid != 0",
		"flag declared as string must not be compared against int zero — this is the #189 bug")
}
