package regenmerge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClassifyPostmanExploreFixture exercises the full classification path
// against the postman-explore fixture. The fixture reproduces the shapes
// observed in the real CLI's templated/novel split — see
// docs/plans/2026-05-01-001-feat-regen-merge-subcommand-plan.md U0 for
// fixture origins.
func TestClassifyPostmanExploreFixture(t *testing.T) {
	t.Parallel()

	pubDir, freshDir := postmanFixture(t)

	report, err := Classify(pubDir, freshDir, Options{Force: true})
	require.NoError(t, err)
	require.NotNil(t, report)

	verdicts := verdictMap(report)

	// Templated-clean files: present in both with marker and decl-set match.
	assert.Equal(t, VerdictTemplatedClean, verdicts["internal/cli/root.go"], "root.go is templated; published has more AddCommands but those are call-expressions, not top-level decls")
	assert.Equal(t, VerdictTemplatedClean, verdicts["internal/cli/category.go"], "category.go is templated; lost AddCommand is a call-expression, not a decl")
	assert.Equal(t, VerdictTemplatedClean, verdicts["internal/cli/helpers.go"], "helpers.go matches in both trees")
	assert.Equal(t, VerdictTemplatedClean, verdicts["internal/cli/templated_stubs.go"], "templated_stubs matches in both trees")

	// Novel files: only in published, no marker.
	assert.Equal(t, VerdictNovel, verdicts["internal/cli/canonical.go"], "canonical.go is hand-written, no marker")
	assert.Equal(t, VerdictNovel, verdicts["internal/cli/novels.go"], "novels.go is hand-written, no marker")
	assert.Equal(t, VerdictNovel, verdicts["internal/cli/novel_helpers.go"], "novel_helpers.go is hand-written, no marker")

	// New template emission: only in fresh.
	assert.Equal(t, VerdictNewTemplateEmission, verdicts["internal/cli/import.go"], "import.go is fresh-only")

	// go.mod always classifies as TEMPLATED-CLEAN at the file level — the
	// merge plan handles the actual content (U3).
	assert.Equal(t, VerdictTemplatedClean, verdicts["go.mod"], "go.mod merge handled by U3 separately; file-level verdict is TEMPLATED-CLEAN")
}

// TestClassifyEbayAuthFixture confirms the canary case: a "templated" file
// with hand-added top-level decls flags as TEMPLATED-WITH-ADDITIONS rather
// than overwriting silently.
func TestClassifyEbayAuthFixture(t *testing.T) {
	t.Parallel()

	pubDir, freshDir := ebayAuthFixture(t)

	report, err := Classify(pubDir, freshDir, Options{Force: true})
	require.NoError(t, err)

	verdicts := verdictMap(report)

	// auth.go has the marker AND has 5 extra functions in published.
	// Decl-set comparison: published ⊃ fresh → TEMPLATED-WITH-ADDITIONS.
	got := verdicts["internal/cli/auth.go"]
	require.Equal(t, VerdictTemplatedWithAdditions, got, "auth.go has 5+ added OAuth functions; preserve published")

	// Find the FileClassification for auth.go and verify the delta lists
	// the added function names.
	var authFC *FileClassification
	for i := range report.Files {
		if report.Files[i].Path == "internal/cli/auth.go" {
			authFC = &report.Files[i]
			break
		}
	}
	require.NotNil(t, authFC, "auth.go must appear in the report")
	require.NotNil(t, authFC.DeclSetDelta, "TEMPLATED-WITH-ADDITIONS must populate DeclSetDelta")

	added := authFC.DeclSetDelta.InPublishedNotFresh
	expectedAdded := []string{"authenticateWithCookie", "oauthBrowserFlow", "persistToken", "pkceChallenge", "refreshTokenIfNeeded"}
	assert.ElementsMatch(t, expectedAdded, added, "delta should list the 5 added OAuth functions")
}

// TestExtractDeclsCanonicalNames pins the receiver-canonicalization rule:
// methods on *T and T are distinct keys; methods with type parameters
// canonicalize to the bare type name (parameters stripped).
func TestExtractDeclsCanonicalNames(t *testing.T) {
	t.Parallel()

	src := `package x

type Store struct{}

func (s *Store) Get(k string) (any, error) { return nil, nil }

func (s Store) GetByValue(k string) any { return nil }

func TopLevelFn() {}

var TopLevelVar = 42

const TopLevelConst = "x"

type Alias = string
`
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, writeFileAtomic(path, []byte(src)))

	decls, err := extractDecls(path)
	require.NoError(t, err)

	// Methods canonicalize with receiver type qualifier.
	assert.Contains(t, decls, "(*Store).Get", "pointer-receiver method")
	assert.Contains(t, decls, "(Store).GetByValue", "value-receiver method (distinct from pointer)")

	// Top-level decls.
	assert.Contains(t, decls, "TopLevelFn")
	assert.Contains(t, decls, "TopLevelVar")
	assert.Contains(t, decls, "TopLevelConst")
	assert.Contains(t, decls, "Store")
	assert.Contains(t, decls, "Alias")
}

// TestClassifyRejectsTraversal exercises path-validation per
// docs/solutions/security-issues/filepath-join-traversal-with-user-input-2026-03-29.md.
func TestClassifyRejectsTraversal(t *testing.T) {
	t.Parallel()

	// Path containing ".." should be rejected.
	_, err := Classify("../../sneaky", "../../also-sneaky", Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "..", "error should reference the invalid segment")
}

// TestClassifyOutsideCwdRejected exercises the prefix-containment check
// (rejects paths outside CWD unless --force).
func TestClassifyOutsideCwdRejected(t *testing.T) {
	t.Parallel()

	// /tmp is almost never under the test's CWD, so this should reject.
	_, err := Classify("/tmp/regen-merge-test-nonexistent", "/tmp/fresh", Options{Force: false})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the current working directory")
}

// TestClassifySpecYamlPropagates pins the contract that spec.yaml at the CLI
// root is classified (and therefore overwritten by Apply) so source-spec
// changes propagate into the library copy. Without this, downstream tools
// (mcp-sync, dogfood, scorecard) read a stale library spec and miss
// source-side enrichments such as `mcp.endpoint_tools: hidden`.
func TestClassifySpecYamlPropagates(t *testing.T) {
	t.Parallel()

	pubDir := t.TempDir()
	freshDir := t.TempDir()

	// Both trees have a spec.yaml at the root; published is older, fresh
	// includes a new mcp: block. Either content works; we only check the
	// verdict here. Apply is covered by the existing TEMPLATED-CLEAN path.
	require.NoError(t, os.WriteFile(filepath.Join(pubDir, "spec.yaml"),
		[]byte("name: x\nversion: \"0.1.0\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(freshDir, "spec.yaml"),
		[]byte("name: x\nversion: \"0.1.0\"\nmcp:\n  endpoint_tools: hidden\n"), 0o644))

	// Minimal go.mod on both sides so the planGoModMerge path doesn't error
	// out before classification reaches spec.yaml.
	require.NoError(t, os.WriteFile(filepath.Join(pubDir, "go.mod"),
		[]byte("module x\n\ngo 1.23\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(freshDir, "go.mod"),
		[]byte("module x\n\ngo 1.23\n"), 0o644))

	report, err := Classify(pubDir, freshDir, Options{Force: true})
	require.NoError(t, err)
	require.NotNil(t, report)

	verdicts := verdictMap(report)
	got, ok := verdicts["spec.yaml"]
	require.True(t, ok, "spec.yaml must participate in classification; got verdicts: %v", verdicts)
	assert.Equal(t, VerdictTemplatedClean, got,
		"spec.yaml present in both trees should be TEMPLATED-CLEAN so Apply overwrites with fresh")
}

// --- fixture helpers ---

// postmanFixture returns published, fresh dirs for the postman-explore
// fixture. Uses --force semantics implicitly via Options{Force: true} in
// callers since the testdata is outside any meaningful CWD prefix.
func postmanFixture(t *testing.T) (string, string) {
	t.Helper()
	abs, err := filepath.Abs("testdata/postman-explore/published")
	require.NoError(t, err)
	freshAbs, err := filepath.Abs("testdata/postman-explore/fresh")
	require.NoError(t, err)
	return abs, freshAbs
}

func ebayAuthFixture(t *testing.T) (string, string) {
	t.Helper()
	abs, err := filepath.Abs("testdata/ebay-auth/published")
	require.NoError(t, err)
	freshAbs, err := filepath.Abs("testdata/ebay-auth/fresh")
	require.NoError(t, err)
	return abs, freshAbs
}

func verdictMap(r *MergeReport) map[string]Verdict {
	out := make(map[string]Verdict, len(r.Files))
	for _, fc := range r.Files {
		out[fc.Path] = fc.Verdict
	}
	return out
}
