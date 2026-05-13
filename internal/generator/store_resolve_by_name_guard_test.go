package generator

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

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
	guard := regexp.MustCompile(`if !validIdentifierRE\.MatchString\(field\) \{\s*continue\s*\}`)
	require.Regexp(t, guard, body,
		"ResolveByName must validate each field name and continue past invalid entries before splicing into the json_extract path; the continue must be inside the validIdentifierRE guard, not the pre-existing query-error continue")
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
