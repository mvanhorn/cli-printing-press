package generator

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeneratedFleetHardening locks the two fleet-wide hardening defaults from
// issue #3012: the go.mod template floors golang.org/x/sys above the vulnerable
// transitive version (GO-2026-5024), and the per-user data/deliver dirs are
// created mode 0700 rather than world-readable 0o755 (gosec G301).
func TestGeneratedFleetHardening(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "fleethardening",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/fleethardening-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"widgets": {
				Description: "Widgets",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/widgets",
						Description: "List widgets",
						Response:    spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	outputDir := generateForHardeningTest(t, apiSpec)

	goMod := readGeneratedFile(t, outputDir, "go.mod")
	require.Contains(t, goMod, "golang.org/x/sys v0.46.0",
		"go.mod must floor golang.org/x/sys at >= v0.46.0 to clear GO-2026-5024")

	storeGo := readGeneratedFile(t, outputDir, "internal", "store", "store.go")
	require.Contains(t, storeGo, "os.MkdirAll(filepath.Dir(dbPath), 0o700)",
		"the SQLite mirror dir must be created mode 0700 (gosec G301)")
	require.NotContains(t, storeGo, "os.MkdirAll(filepath.Dir(dbPath), 0o755)")

	deliverGo := readGeneratedFile(t, outputDir, "internal", "cli", "deliver.go")
	require.Contains(t, deliverGo, "os.MkdirAll(dir, 0o700)",
		"the agent-deliver dir must be created mode 0700 (gosec G301)")
	require.NotContains(t, deliverGo, "os.MkdirAll(dir, 0o755)")
}

// TestNoWorldReadableMkdirAllInTemplates is the fleet-wide regression guard for
// gosec G301: no emitted template may create a directory world-readable
// (`0o755`). Scoped to template source because the alternative — generating
// every feature combination (store, deliver, share, session-handshake, ...) and
// asserting on each emitted file — is impractical; this catches a future
// template edit reintroducing the mode regardless of which feature emits it.
// The generator's own `MkdirAll` calls (build-time output dirs in *.go) are out
// of scope — they don't ship in printed CLIs.
func TestNoWorldReadableMkdirAllInTemplates(t *testing.T) {
	t.Parallel()

	var offenders []string
	err := filepath.WalkDir("templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".tmpl") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "MkdirAll") && strings.Contains(line, "0o755") {
				offenders = append(offenders, filepath.ToSlash(path)+":"+strconv.Itoa(i+1))
			}
		}
		return nil
	})
	require.NoError(t, err)
	assert.Empty(t, offenders, "emitted templates must create dirs with 0o700, not world-readable 0o755 (gosec G301): %v", offenders)
}

func generateForHardeningTest(t *testing.T, apiSpec *spec.APISpec) string {
	t.Helper()
	outputDir := strings.TrimSuffix(t.TempDir(), "/") + "/" + naming.CLI(apiSpec.Name)
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	gen.profile = &profiler.APIProfile{
		SyncableResources: []profiler.SyncableResource{
			{Name: "widgets", Path: "/widgets", Method: "GET"},
		},
	}
	require.NoError(t, gen.Generate())
	return outputDir
}
