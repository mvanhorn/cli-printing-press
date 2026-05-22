package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateEmitsInvalidateCacheSymmetry guards #603's two-prong fix:
// the generated client.go must contain BOTH the invalidateCache method
// definition AND a c.invalidateCache() call inside the do-family
// implementation's body. Method-presence alone is not enough — a future
// refactor that drops the call but keeps the method would silently
// re-introduce the stale-list-after-mutation bug. See
// docs/solutions/design-patterns/http-client-cache-invalidate-on-mutation-2026-05-05.md
// for full rationale.
//
// After the verify-mode read-only-POST work, do() is a thin wrapper
// around doInternal(...readOnlyIntent bool), so the cache-invalidation
// call lives in doInternal(). The assertion follows the single
// implementation site: a call site OUTSIDE doInternal() would not
// protect against the stale-list-after-mutation regression.
func TestGenerateEmitsInvalidateCacheSymmetry(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "stytch.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	clientGoBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientGo := string(clientGoBytes)

	// Prong 1: method definition exists.
	assert.Contains(t, clientGo, "func (c *Client) invalidateCache()",
		"client.go must define invalidateCache method (R1)")

	// Prong 2: doInternal() must call invalidateCache. Bound the search
	// to doInternal()'s body so a call site emitted at file scope (or in
	// an unrelated helper) does not pass. doInternal() spans from its
	// declaration to the next package-level `func ` or end of file.
	implStart := strings.Index(clientGo, "func (c *Client) doInternal(")
	require.NotEqual(t, -1, implStart, "client.go must contain Client.doInternal function")
	implRest := clientGo[implStart:]
	nextFunc := strings.Index(implRest[1:], "\nfunc ")
	implBody := implRest
	if nextFunc != -1 {
		implBody = implRest[:nextFunc+1]
	}
	assert.Contains(t, implBody, "c.invalidateCache()",
		"Client.doInternal must call c.invalidateCache() in its success branch (R2)")

	// do() and doRead() must remain thin wrappers around doInternal so
	// the cache-invalidation call site stays single. A future edit that
	// inlines do()'s implementation back into do() would silently move
	// the call out of doInternal — pin the wrapper shape here too.
	assert.Contains(t, clientGo, "func (c *Client) do(ctx context.Context, method, path string, params map[string]string, body any, headerOverrides map[string]string) (json.RawMessage, int, error) {\n\treturn c.doInternal(ctx, method, path, params, body, headerOverrides, false)\n}",
		"Client.do must be a thin wrapper delegating to doInternal(..., false)")
	assert.Contains(t, clientGo, "func (c *Client) doRead(ctx context.Context, method, path string, params map[string]string, body any, headerOverrides map[string]string) (json.RawMessage, int, error) {\n\treturn c.doInternal(ctx, method, path, params, body, headerOverrides, true)\n}",
		"Client.doRead must be a thin wrapper delegating to doInternal(..., true)")

	// Prong 3: writeCache must still be present (asymmetry diagnostic
	// from the design-pattern doc — writeCache without invalidateCache
	// is the original bug shape).
	assert.Contains(t, clientGo, "func (c *Client) writeCache(",
		"client.go must still define writeCache; symmetry presupposes both")
}

// TestGenerateCacheDirIsHTTPSubdir guards #1126: cacheDir must point at
// ~/.cache/<api>/http (not ~/.cache/<api>) so that invalidateCache's
// os.RemoveAll only wipes the HTTP cache and leaves sibling state files
// (SQLite mirrors, FTS5 stores, watchlists) intact.
func TestGenerateCacheDirIsHTTPSubdir(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "stytch.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	clientGoBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientGo := string(clientGoBytes)

	cliName := naming.CLI(apiSpec.Name)
	wantSubdir := `filepath.Join(homeDir, ".cache", "` + cliName + `", "http")`
	wantOldShape := `filepath.Join(homeDir, ".cache", "` + cliName + `")`

	assert.Contains(t, clientGo, wantSubdir,
		"client.go must place cacheDir under <api>/http so invalidateCache spares siblings (#1126)")
	assert.NotContains(t, clientGo, wantOldShape,
		"client.go must not point cacheDir at the bare ~/.cache/<api>/ root (#1126)")
}
