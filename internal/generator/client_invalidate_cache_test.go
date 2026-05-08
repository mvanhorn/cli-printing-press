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
// definition AND a c.invalidateCache() call inside the request helper's body.
// Method-presence alone is not enough — a future refactor that drops
// the call but keeps the method would silently re-introduce the
// stale-list-after-mutation bug. See
// docs/solutions/design-patterns/http-client-cache-invalidate-on-mutation-2026-05-05.md
// for full rationale.
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

	// Prong 2: the request helper must call invalidateCache. Specs with raw
	// binary downloads emit doRequest(); otherwise the traditional do() remains.
	// Locate the active helper and verify the call is in its body — not just
	// anywhere in the file. The helper
	// function spans from its declaration to the next package-level
	// `func ` or end of file. A call site OUTSIDE the helper would not protect
	// against the stale-list-after-mutation regression.
	helperName := "doRequest"
	helperStart := strings.Index(clientGo, "func (c *Client) doRequest(")
	if helperStart == -1 {
		helperName = "do"
		helperStart = strings.Index(clientGo, "func (c *Client) do(")
	}
	require.NotEqual(t, -1, helperStart, "client.go must contain Client.do or Client.doRequest function")
	helperRest := clientGo[helperStart:]
	// Find the next top-level func declaration to bound the helper's body.
	nextFunc := strings.Index(helperRest[1:], "\nfunc ")
	helperBody := helperRest
	if nextFunc != -1 {
		helperBody = helperRest[:nextFunc+1]
	}
	assert.Contains(t, helperBody, "c.invalidateCache()",
		"Client.%s must call c.invalidateCache() in its success branch (R2)", helperName)

	// Prong 3: writeCache must still be present (asymmetry diagnostic
	// from the design-pattern doc — writeCache without invalidateCache
	// is the original bug shape).
	assert.Contains(t, clientGo, "func (c *Client) writeCache(",
		"client.go must still define writeCache; symmetry presupposes both")
}
