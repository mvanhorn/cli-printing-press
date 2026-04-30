package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestJSONStringParamEmitsLocalValidation(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("json-param")
	apiSpec.Resources["insights"] = spec.Resource{
		Description: "Insights",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/insights",
				Description: "List insights",
			},
			"search": {
				Method:      "GET",
				Path:        "/insights/search",
				Description: "Search insights",
				Params: []spec.Param{
					{
						Name:        "time_range",
						Type:        "string",
						Description: "Custom time range as JSON: {'since':'YYYY-MM-DD','until':'YYYY-MM-DD'}",
					},
					{
						Name:        "date_preset",
						Type:        "string",
						Description: "Preset date range",
						Enum:        []string{"today", "last_7d"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "json-param-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "insights_search.go"))
	require.NoError(t, err)
	code := string(src)

	require.Contains(t, code, `if cmd.Flags().Changed("time-range") {`)
	require.Contains(t, code, `var parsedTimeRange any`)
	require.Contains(t, code, `json.Unmarshal([]byte(flagTimeRange), &parsedTimeRange)`)
	require.Contains(t, code, `--time-range must be valid JSON. Did you mean --date-preset %s?`)
	require.Contains(t, code, `--time-range must be valid JSON: %w`)
}

func TestJSONStringParamDetectionUsesFormatHint(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("json-format-param")
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Items",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/items",
				Description: "List items",
			},
			"filter": {
				Method:      "GET",
				Path:        "/items/filter",
				Description: "Filter items",
				Params: []spec.Param{
					{Name: "filter", Type: "string", Description: "Filter expression", Format: "json"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "json-format-param-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "items_filter.go"))
	require.NoError(t, err)
	require.Contains(t, string(src), `json.Unmarshal([]byte(flagFilter), &parsedFilter)`)
}

func TestJSONStringParamDetectionDoesNotOvermatchJSONPathFormat(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("jsonpath-param")
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Items",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/items",
				Description: "List items",
			},
			"extract": {
				Method:      "GET",
				Path:        "/items/extract",
				Description: "Extract fields",
				Params: []spec.Param{
					{Name: "path", Type: "string", Description: "JSONPath expression", Format: "jsonpath"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "jsonpath-param-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "items_extract.go"))
	require.NoError(t, err)
	require.NotContains(t, string(src), `parsedPath`)
}

func TestPromotedJSONStringParamEmitsLocalValidation(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promoted-json-param")
	apiSpec.Resources["insights"] = spec.Resource{
		Description: "Insights",
		Endpoints: map[string]spec.Endpoint{
			"get": {
				Method:      "GET",
				Path:        "/insights",
				Description: "Get insights",
				Params: []spec.Param{
					{Name: "time_range", Type: "string", Description: "Custom time range as JSON"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "promoted-json-param-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "promoted_insights.go"))
	require.NoError(t, err)
	require.Contains(t, string(src), `json.Unmarshal([]byte(flagTimeRange), &parsedTimeRange)`)
}

func TestJSONStringBodyParamEmitsLocalValidation(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("json-body-param")
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Items",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/items",
				Description: "List items",
			},
			"create": {
				Method:      "POST",
				Path:        "/items",
				Description: "Create item",
				Body: []spec.Param{
					{Name: "metadata", Type: "string", Description: "Metadata as JSON object"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "json-body-param-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "items_create.go"))
	require.NoError(t, err)
	code := string(src)

	require.Contains(t, code, `json.Unmarshal([]byte(bodyMetadata), &parsedMetadata)`)
	require.Contains(t, code, `body["metadata"] = bodyMetadata`)
	require.NotContains(t, code, `body["metadata"] = parsedMetadata`)
}

func TestJSONStringParamDoesNotSuggestUnrelatedEnum(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("json-unrelated-enum")
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Items",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/items",
				Description: "List items",
			},
			"search": {
				Method:      "GET",
				Path:        "/items/search",
				Description: "Search items",
				Params: []spec.Param{
					{Name: "filter", Type: "string", Description: "Filter as JSON object"},
					{Name: "status", Type: "string", Description: "Item status", Enum: []string{"active"}},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "json-unrelated-enum-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "items_search.go"))
	require.NoError(t, err)
	code := string(src)

	require.Contains(t, code, `--filter must be valid JSON: %w`)
	require.NotContains(t, code, `Did you mean --status`)
}

func TestJSONStringParamRejectsInvalidValueBeforeClient(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("json-runtime-param")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources["insights"] = spec.Resource{
		Description: "Insights",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/insights",
				Description: "List insights",
			},
			"search": {
				Method:      "GET",
				Path:        "/insights/search",
				Description: "Search insights",
				Params: []spec.Param{
					{Name: "time_range", Type: "string", Description: "Custom time range as JSON"},
					{Name: "date_preset", Type: "string", Description: "Preset date range", Enum: []string{"last_7d"}},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "json-runtime-param-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	runGoCommand(t, outputDir, "mod", "tidy")

	binaryPath := filepath.Join(outputDir, "json-runtime-param-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/json-runtime-param-pp-cli")

	cmd := exec.Command(binaryPath, "insights", "search", "--time-range", "last_7d")
	out, err := cmd.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(out), "--time-range must be valid JSON. Did you mean --date-preset last_7d?")
}

func TestJSONStringParamSuggestsTemporalPresetWithDifferentMarker(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("json-temporal-preset")
	apiSpec.Resources["insights"] = spec.Resource{
		Description: "Insights",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/insights",
				Description: "List insights",
			},
			"search": {
				Method:      "GET",
				Path:        "/insights/search",
				Description: "Search insights",
				Params: []spec.Param{
					{Name: "time_range", Type: "string", Description: "Custom time range as JSON"},
					{Name: "date_preset", Type: "string", Description: "Date preset", Enum: []string{"last_7d"}},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "json-temporal-preset-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "insights_search.go"))
	require.NoError(t, err)
	require.Contains(t, string(src), `Did you mean --date-preset %s?`)
}

func TestPlainStringParamDoesNotEmitJSONValidation(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("plain-string-param")
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Items",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/items",
				Description: "List items",
			},
			"search": {
				Method:      "GET",
				Path:        "/items/search",
				Description: "Search items",
				Params: []spec.Param{
					{Name: "query", Type: "string", Description: "Search query"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "plain-string-param-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "items_search.go"))
	require.NoError(t, err)
	require.NotContains(t, string(src), `parsedQuery`)
}
