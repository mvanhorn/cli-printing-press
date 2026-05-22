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
