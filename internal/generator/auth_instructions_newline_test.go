package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuthSetupInstructionsNoRedundantNewline pins that a multi-line
// (YAML block-scalar) auth.instructions value does not produce an Fprintln
// whose string literal ends in \n. Fprintln appends its own newline, so a
// trailing-\n literal trips `go vet`'s redundant-newline check and fails the
// post-generation quality gate (#1946).
func TestAuthSetupInstructionsNoRedundantNewline(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("blockscalar")
	// Trailing newline mimics a YAML block scalar ("instructions: |").
	apiSpec.Auth.Instructions = "1. Visit the console\n2. Create a client\n3. Copy the token\n"

	outputDir := filepath.Join(t.TempDir(), "blockscalar-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	content := string(authSrc)

	require.Contains(t, content, "3. Copy the token", "instructions should render")
	assert.NotContains(t, content, `3. Copy the token\n")`,
		"instructions Fprintln literal must not end in \\n (go vet redundant-newline)")
	assert.Contains(t, content, `3. Copy the token")`,
		"chomped instructions should end the Fprintln literal cleanly")
}
