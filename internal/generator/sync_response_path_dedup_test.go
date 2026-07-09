// Copyright 2026 Anthropic, PBC. Licensed under Apache-2.0.

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

// TestGenerateSyncResponsePathDedup guards the responsePathForResource switch
// against duplicate `case` labels. The switch keys on resource+path, but a
// single resource can hold multiple endpoints that share one templated path
// with different response envelope keys. Google's Admin Directory is the real
// case: /customer/{customer}/roles serves both the roles list (envelope
// "items") and the role-privileges read (envelope "rolePrivileges"). Ranging
// over every endpoint emitted two `case "admin\x00/customer/{customer}/roles"`
// labels, which is a Go compile error ("duplicate case in expression switch").
//
// The fix dedupes by switch key (first endpoint in sorted order wins), so the
// generated switch always compiles. This test compiles the whole generated
// module to prove it.
func TestGenerateSyncResponsePathDedup(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "dup-path-sample",
		Version: "0.1.0",
		BaseURL: "https://api.example.test/v1",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"DUP_PATH_SAMPLE_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/dup-path-sample-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			// Two endpoints in one resource sharing the exact same templated
			// path, each with a distinct response envelope key. This is the
			// shape that produced duplicate switch cases before the fix.
			"roles": {
				Description: "Directory roles",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:       "GET",
						Path:         "/customer/{customer}/roles",
						Description:  "List roles",
						Syncable:     true,
						ResponsePath: "items",
						Response:     spec.ResponseDef{Type: "array"},
					},
					"privileges": {
						Method:       "GET",
						Path:         "/customer/{customer}/roles",
						Description:  "List role privileges",
						ResponsePath: "rolePrivileges",
						Response:     spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncContent := string(syncGo)

	// Exactly one case label for the shared key — not two. The NUL separator
	// is rendered into the generated source as the escaped literal `\x00`
	// (via %q), not a raw NUL byte.
	caseLabel := `case "roles\x00/customer/{customer}/roles":`
	got := strings.Count(syncContent, caseLabel)
	assert.Equal(t, 1, got,
		"responsePathForResource must emit the shared resource+path key exactly once; got %d", got)

	// The winning ResponsePath is the first endpoint in sorted endpoint-name
	// order ("list" < "privileges"), so the list envelope "items" wins.
	assert.Contains(t, syncContent, `return []string{"items"}`,
		"first endpoint in sorted order should own the deduped case")

	// The whole generated module must compile — the compile error this test
	// exists for only surfaces at build time.
	requireGeneratedCompiles(t, outputDir)
}
