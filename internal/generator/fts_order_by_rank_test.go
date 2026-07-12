package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateStoreFTSOrderByRankQualified is the regression test for issue
// #2973. When a resource has a typed `rank` column alongside an FTS5 index,
// the FTS5 special `rank` column and the data table's `rank` column collide,
// and an unqualified `ORDER BY rank` is rejected by SQLite as an ambiguous
// column name. The generated Search SQL must qualify `rank` to the FTS table
// so the query is unambiguous for every resource shape.
//
// This fixture carries exactly such a resource (a `rank` response field plus
// the text fields that turn on FTS5), so the emitted store.go is the shape
// that broke search on rank-fielded CLIs (e.g. pp-espn) before the fix.
func TestGenerateStoreFTSOrderByRankQualified(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("fts-rank")
	apiSpec.Types = map[string]spec.TypeDef{
		"Item": {Fields: []spec.TypeField{
			{Name: "id", Type: "string"},
			{Name: "title", Type: "string"},
			{Name: "description", Type: "string"},
			{Name: "rank", Type: "integer"},
			{Name: "created_at", Type: "string", Format: "date-time"},
			{Name: "owner_id", Type: "string"},
		}},
	}
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Manage items",
			Endpoints: map[string]spec.Endpoint{
				"list":   {Method: "GET", Path: "/items", Description: "List items", Response: spec.ResponseDef{Type: "array", Item: "Item"}},
				"detail": {Method: "GET", Path: "/items/{id}", Description: "Get an item", Response: spec.ResponseDef{Type: "array", Item: "Item"}},
				"create": {Method: "POST", Path: "/items", Description: "Create an item"},
				"update": {Method: "PUT", Path: "/items/{id}", Description: "Update an item"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "fts-rank-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)

	// Per-resource Search qualifies rank to the resource's FTS table. The
	// table identifier is double-quoted by the safeName helpers.
	assert.Contains(t, string(storeSrc), `"items_fts".rank LIMIT ?`,
		"per-resource Search must qualify rank to the FTS table (issue #2973)")
	// Generic SearchAll qualifies rank to its resources_fts alias.
	assert.Contains(t, string(storeSrc), "ORDER BY f.rank",
		"generic Search must qualify rank to the resources_fts alias (issue #2973)")
	// No unqualified ORDER BY rank may remain anywhere in the store; on a
	// rank-fielded resource SQLite would reject it as ambiguous.
	assert.NotContains(t, string(storeSrc), "ORDER BY rank",
		"unqualified ORDER BY rank is ambiguous on rank-fielded resources (issue #2973)")

	// Runtime proof: the generated per-resource FTS query must execute against
	// a SQLite database whose items table carries a typed `rank` column. Before
	// the fix this failed at query time with "ambiguous column name: rank".
	runGoCommand(t, outputDir, "test", "./internal/store", "-run", "^TestSearchItemsQuotesFTSQuerySyntax$", "-count=1")
}

// TestGenerateInsightsSimilarFTSOrderByRankQualified is the insights-path
// regression for issue #2973. The similar-items command joins resources_fts
// to the data table and orders by the FTS5 special `rank` column; that column
// must be qualified to the FTS alias (`fts.rank`) so the query stays
// unambiguous on rank-fielded resources. The existing store.go test does not
// exercise the insights template, which renders only when VisionSet.Insights
// is populated.
func TestGenerateInsightsSimilarFTSOrderByRankQualified(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("fts-rank-insights")
	apiSpec.Types = map[string]spec.TypeDef{
		"Item": {Fields: []spec.TypeField{
			{Name: "id", Type: "string"},
			{Name: "title", Type: "string"},
			{Name: "description", Type: "string"},
			{Name: "rank", Type: "integer"},
		}},
	}
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Manage items",
			Endpoints: map[string]spec.Endpoint{
				"list":   {Method: "GET", Path: "/items", Description: "List items", Response: spec.ResponseDef{Type: "array", Item: "Item"}},
				"detail": {Method: "GET", Path: "/items/{id}", Description: "Get an item", Response: spec.ResponseDef{Type: "array", Item: "Item"}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "fts-rank-insights-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{
		Store:    true,
		Insights: []string{"insights/similar.go.tmpl"},
	}
	require.NoError(t, gen.Generate())

	similarSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "similar.go"))
	require.NoError(t, err)

	// The similar-items FTS query must select and order by the FTS-aliased
	// rank column, not the bare `rank` identifier.
	assert.Contains(t, string(similarSrc), "r.data, fts.rank",
		"similar command SELECT must qualify rank to the FTS alias (issue #2973)")
	assert.Contains(t, string(similarSrc), "ORDER BY fts.rank",
		"similar command ORDER BY must qualify rank to the FTS alias (issue #2973)")
	assert.NotContains(t, string(similarSrc), "ORDER BY rank",
		"unqualified ORDER BY rank is ambiguous on rank-fielded resources (issue #2973)")
}
