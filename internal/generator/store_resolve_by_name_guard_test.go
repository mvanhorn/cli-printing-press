package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStoreResolveByNameValidatesField pins the SQL-injection guard on
// ResolveByName's caller-supplied field names. json_extract path components
// can't be bound as SQL parameters, so the splice is structurally required
// — but every field must be validated against validIdentifierRE before
// being substituted into the query.
func TestStoreResolveByNameValidatesField(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("resolve-guard")
	outputDir := filepath.Join(t.TempDir(), "resolve-guard-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	store := string(storeSrc)
	body := resolveByNameBody(t, store)

	require.Contains(t, body, `for _, field := range matchFields {`,
		"ResolveByName must iterate matchFields")
	require.Contains(t, body, `if !validIdentifierRE.MatchString(field) {`,
		"ResolveByName must validate each field name before splicing into json_extract path")
	require.Contains(t, body, `continue`,
		"unsafe field names must be skipped, not used to build the query")
}

func resolveByNameBody(t *testing.T, content string) string {
	t.Helper()
	start := strings.Index(content, "func (s *Store) ResolveByName(")
	require.NotEqual(t, -1, start, "ResolveByName function must be emitted")
	body := content[start:]
	if next := strings.Index(body[1:], "\nfunc "); next != -1 {
		body = body[:next+1]
	}
	return body
}
