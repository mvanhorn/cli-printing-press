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

func TestGeneratedStoreResolveByNameSurfacesQueryAndRowErrors(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("resolve-errors")
	outputDir := filepath.Join(t.TempDir(), "resolve-errors-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	testPath := filepath.Join(outputDir, "internal", "store", "resolve_by_name_error_test.go")
	testSrc := `package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveByNameReturnsQueryAndIterationErrors(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		s, err := Open(filepath.Join(t.TempDir(), "data.db"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer s.Close()

		if _, err := s.DB().Exec("INSERT INTO resources (resource_type, id, data) VALUES (?, ?, ?)", "users", "user-1", "{\"name\":\"Alice\"}"); err != nil {
			t.Fatalf("seed resource: %v", err)
		}

		got, err := s.ResolveByName("users", "Alice", "name")
		if err != nil {
			t.Fatalf("ResolveByName happy path: %v", err)
		}
		if got != "user-1" {
			t.Fatalf("ResolveByName = %q, want user-1", got)
		}
	})

	t.Run("query error", func(t *testing.T) {
		s, err := Open(filepath.Join(t.TempDir(), "data.db"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}

		_, err = s.ResolveByName("users", "Alice", "name")
		if err == nil {
			t.Fatal("ResolveByName returned nil error after db close")
		}
		if strings.Contains(err.Error(), "not found in local store") {
			t.Fatalf("ResolveByName swallowed query error as not-found: %v", err)
		}
	})

	t.Run("row error", func(t *testing.T) {
		s, err := Open(filepath.Join(t.TempDir(), "data.db"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer s.Close()

		// The "{" value is syntactically broken JSON: json_extract raises a
		// statement error during row iteration, which ResolveByName must
		// surface via rows.Err(). This relies on SQLite >= 3.38
		// (modernc.org/sqlite) erroring on malformed JSON rather than
		// returning NULL; if that behavior ever changes the query would
		// return zero rows and this subtest would stop exercising the
		// rows.Err() path (ResolveByName would report not-found instead).
		if _, err := s.DB().Exec("INSERT INTO resources (resource_type, id, data) VALUES (?, ?, ?)", "users", "bad-json", "{"); err != nil {
			t.Fatalf("seed malformed resource: %v", err)
		}

		_, err = s.ResolveByName("users", "Alice", "name")
		if err == nil {
			t.Fatal("ResolveByName returned nil error for malformed JSON row")
		}
		if strings.Contains(err.Error(), "not found in local store") {
			t.Fatalf("ResolveByName swallowed row error as not-found: %v", err)
		}
	})
}
`
	require.NoError(t, os.WriteFile(testPath, []byte(testSrc), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/store", "-run", "TestResolveByNameReturnsQueryAndIterationErrors", "-count=1")
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
