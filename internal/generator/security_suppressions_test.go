package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedInfraSecuritySuppressionsAreExplicit(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("security")
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Manage items",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:     "GET",
				Path:       "/account/keys",
				Response:   spec.ResponseDef{Type: "array", Item: "Item"},
				Pagination: &spec.Pagination{CursorParam: "cursor", LimitParam: "limit"},
			},
			"create": {
				Method:   "POST",
				Path:     "/account/keys",
				Response: spec.ResponseDef{Type: "object"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	credentialsGo := readGenerated(t, outputDir, "internal", "cliutil", "credentials.go")
	assert.Contains(t, credentialsGo, `os.ReadFile(filepath.Clean(path)) // #nosec G304 -- app-owned credentials path from cliutil.DataDir.`)
	assert.Contains(t, credentialsGo, `toml.Marshal(credentialsFileFrom(creds)) // #nosec G117 -- credentials are intentionally persisted to a 0600 private file.`)

	pathsGo := readGenerated(t, outputDir, "internal", "cliutil", "paths.go")
	assert.Contains(t, pathsGo, `os.ReadFile(filepath.Clean(primary)) // #nosec G304 -- app-derived config/data path.`)
	assert.Contains(t, pathsGo, `os.ReadFile(filepath.Clean(legacy)) // #nosec G304 -- app-derived legacy config/data path.`)
	assert.Contains(t, pathsGo, `_ = tmp.Close()`)

	clientGo := readGenerated(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, clientGo, `os.ReadFile(filepath.Clean(cacheFile)) // #nosec G304 -- app-derived cache path from sha256 cache key.`)
	assert.Contains(t, clientGo, `_ = os.MkdirAll(c.cacheDir, 0o700)`)
	assert.Contains(t, clientGo, `_ = os.WriteFile(cacheFile, []byte(data), 0o600)`)
	assert.Contains(t, clientGo, `_ = resp.Body.Close()`)

	cacheGo := readGenerated(t, outputDir, "internal", "cache", "cache.go")
	assert.Contains(t, cacheGo, `os.ReadFile(filepath.Clean(path)) // #nosec G304 -- app-derived cache path from sha256 cache key.`)

	shelloutGo := readGenerated(t, outputDir, "internal", "mcp", "cobratree", "shellout.go")
	assert.Contains(t, shelloutGo, `exec.CommandContext(ctx, binPath, args...) // #nosec G204 -- trusted companion CLI path, args pre-tokenized.`)

	syncGo := readGenerated(t, outputDir, "internal", "cli", "sync.go")
	assert.Contains(t, syncGo, `paths := map[string]string{ // #nosec G101 -- endpoint paths, not credentials.`)

	importGo := readGenerated(t, outputDir, "internal", "cli", "import.go")
	assert.Contains(t, importGo, `os.Open(filepath.Clean(inputFile)) // #nosec G304 -- user-specified input file is this flag's documented purpose.`)

	storeGo := readGenerated(t, outputDir, "internal", "store", "store.go")
	assert.Contains(t, storeGo, `_ = db.Close()`)
	assert.Contains(t, storeGo, `_ = rows.Close()`)
}

func readGenerated(t *testing.T, outputDir string, path ...string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(append([]string{outputDir}, path...)...))
	require.NoError(t, err)
	return string(data)
}
