package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Read-side profile lookup (root --profile) must not create config dirs; real
// profile saves must still create them.
func TestGeneratedProfileReadCreatesNoDirs(t *testing.T) {
	_, baseURL := dryRunHitServer(t)
	apiSpec := searchCLISpec("readbound", baseURL, map[string]spec.Resource{}, searchCLITypes())
	apiSpec.Resources["comuni"] = apiSpec.Resources["records"]
	delete(apiSpec.Resources, "records")
	_, binary := buildSearchCLI(t, apiSpec)
	for _, state := range []string{"missing", "nested"} {
		home := filepath.Join(t.TempDir(), "home")
		if state == "nested" {
			home = filepath.Join(home, "nested", "deeper")
		}
		for _, args := range [][]string{
			{"profile", "save", "x", "--dry-run", "--compact"},
			{"profile", "delete", "x", "--yes", "--dry-run"},
			{"sync", "--resources", "comuni", "--dry-run", "--agent"},
		} {
			full := append(append([]string{}, args...), "--home", home, "--profile", "ghost")
			_, _, err := runSearchCLI(t, binary, full...)
			require.Error(t, err, "%v", full)
			assert.NoDirExists(t, home, "%s: %v created dirs", state, full)
		}
		out, stderr, err := runSearchCLI(t, binary, "profile", "save", "real", "--home", home, "--compact")
		require.NoError(t, err, "%s %s", out, stderr)
		assert.FileExists(t, filepath.Join(home, "config", "profiles.json"))
	}
}
