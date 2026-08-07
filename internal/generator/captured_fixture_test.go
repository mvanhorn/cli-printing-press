package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/stretchr/testify/require"
)

func TestGenerateCapturedFixtureUsesSyntheticSamples(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("captured-placeholders")
	outputDir := filepath.Join(t.TempDir(), "captured-placeholders-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.FixtureSet = &browsersniff.FixtureSet{
		Fixtures: []browsersniff.TestFixture{
			{
				EndpointName: "get_order",
				Method:       "GET",
				Path:         "/orders",
				ParamSamples: []browsersniff.FixtureValue{
					{Name: "amount", Value: "12.34"},
					{Name: "asin", Value: "B0EXAMPLE1"},
					{Name: "card_last4", Value: "LAST4"},
					{Name: "order_id", Value: "111-1111111-1111111"},
					{Name: "purchased_date", Value: "2026-01-15"},
				},
			},
		},
	}

	require.NoError(t, gen.Generate())
	data, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client_captured_test.go"))
	require.NoError(t, err)
	src := string(data)

	require.Contains(t, src, `"order_id"`)
	require.Contains(t, src, `"111-1111111-1111111"`)
	require.Contains(t, src, `"asin"`)
	require.Contains(t, src, `"B0EXAMPLE1"`)
	require.Contains(t, src, `"card_last4"`)
	require.Contains(t, src, `"LAST4"`)
	require.Contains(t, src, `"amount"`)
	require.Contains(t, src, `"12.34"`)
	require.Contains(t, src, `"purchased_date"`)
	require.Contains(t, src, `"2026-01-15"`)
}

func TestGenerateSniffedResponseTypeStubs(t *testing.T) {
	t.Parallel()

	apiSpec, err := browsersniff.AnalyzeCapture(&browsersniff.EnrichedCapture{
		TargetURL: "https://api.example.com",
		Entries: []browsersniff.EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/api/search?q=cafe",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"items":[{"id":"1","title":"Cafe","ratingData":{"reviewCount":12},"has_рассрочка":"yes"}]}`,
			},
			{
				Method:              "GET",
				URL:                 "https://api.example.com/api/search?q=tea",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"items":[{"id":"2","title":"Tea","ratingData":{}}]}`,
			},
			{
				Method:              "GET",
				URL:                 "https://api.example.com/api/search?q=bakery",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"items":[{"id":"3","title":"Bakery"}]}`,
			},
		},
	})
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "sniffed-types-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	data, err := os.ReadFile(filepath.Join(outputDir, "internal", "types", "types.go"))
	require.NoError(t, err)
	src := string(data)

	require.Contains(t, src, "type SearchItem struct {")
	require.Contains(t, src, "Id            string     `json:\"id\"`")
	require.Contains(t, src, "Title         string     `json:\"title\"`")
	require.Contains(t, src, "RatingData    RatingData `json:\"ratingData,omitempty\"`")
	require.Contains(t, src, "HasRassrochka string     `json:\"has_рассрочка,omitempty\"`")
	require.Contains(t, src, "// JSON tag: has_рассрочка")
	require.Contains(t, src, "type RatingData struct {")
	require.Contains(t, src, "ReviewCount int `json:\"reviewCount,omitempty\"`")
}

func TestGenerateSniffedTypesPreservesExistingHandAuthoredTypes(t *testing.T) {
	t.Parallel()

	apiSpec, err := browsersniff.AnalyzeCapture(&browsersniff.EnrichedCapture{
		TargetURL: "https://api.example.com",
		Entries: []browsersniff.EnrichedEntry{
			{
				Method:              "GET",
				URL:                 "https://api.example.com/api/search",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"items":[{"id":"1","title":"Cafe"}]}`,
			},
		},
	})
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "sniffed-types-preserve-pp-cli")
	typesPath := filepath.Join(outputDir, "internal", "types", "types.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(typesPath), 0o755))
	handAuthored := `package types

type HandAuthored struct {
	ID string ` + "`json:\"id\"`" + `
}
`
	require.NoError(t, os.WriteFile(typesPath, []byte(handAuthored), 0o644))

	require.NoError(t, New(apiSpec, outputDir).Generate())

	data, err := os.ReadFile(typesPath)
	require.NoError(t, err)
	require.Equal(t, handAuthored, string(data))
}
