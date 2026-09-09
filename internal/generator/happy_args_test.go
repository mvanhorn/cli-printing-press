package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEndpointHappyArgsSynthesizesFromDerivableSchemaHints(t *testing.T) {
	t.Parallel()

	ep := spec.Endpoint{
		Method: "GET",
		Path:   "/v1/forecast",
		Params: []spec.Param{
			{Name: "latitude", Type: "string", Required: true, Example: "52.52"},
			{Name: "longitude", Type: "string", Required: true, Example: "13.41"},
			{Name: "units", Type: "string", Required: true, Enum: []string{"metric", "imperial"}},
			{Name: "start", Type: "string", Required: true, Format: "date"},
			{Name: "q", Type: "string", Required: true, Default: "berlin"},
		},
	}

	got := endpointHappyArgs(ep)
	assert.Equal(t, "--latitude=52.52;--longitude=13.41;--units=metric;--start=2026-01-15;--q=berlin", got)
}

func TestEndpointHappyArgsKeepsSpecDeclaredValue(t *testing.T) {
	t.Parallel()

	ep := spec.Endpoint{
		Method:    "GET",
		Path:      "/lookup",
		HappyArgs: "--q=example-page",
		Params:    []spec.Param{{Name: "q", Type: "string", Required: true, Example: "ignored"}},
	}

	assert.Equal(t, "--q=example-page", endpointHappyArgs(ep))
}

func TestEndpointHappyArgsDoesNotInventOpaqueIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		endpoint spec.Endpoint
	}{
		{
			name: "required opaque id without example",
			endpoint: spec.Endpoint{
				Method: "GET",
				Path:   "/videos/{video_id}",
				Params: []spec.Param{
					{Name: "video_id", Type: "string", Required: true, Positional: true, PathParam: true},
				},
			},
		},
		{
			name: "required coordinates without schema hints",
			endpoint: spec.Endpoint{
				Method: "GET",
				Path:   "/v1/forecast",
				Params: []spec.Param{
					{Name: "latitude", Type: "string", Required: true, Description: "Geographical WGS84 coordinates."},
					{Name: "longitude", Type: "string", Required: true},
				},
			},
		},
		{
			name: "mixed derivable and underivable required flags",
			endpoint: spec.Endpoint{
				Method: "GET",
				Path:   "/search",
				Params: []spec.Param{
					{Name: "q", Type: "string", Required: true, Example: "cats"},
					{Name: "collection_id", Type: "string", Required: true},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := endpointHappyArgs(tt.endpoint)
			assert.Empty(t, got)
			assert.NotContains(t, got, "550e8400-e29b-41d4-a716-446655440000")
			assert.NotContains(t, got, "52.52")
		})
	}
}

func TestEndpointHappyArgsOmitsUnderivableOptionalPositionals(t *testing.T) {
	t.Parallel()

	ep := spec.Endpoint{
		Method: "GET",
		Path:   "/search",
		Params: []spec.Param{
			{Name: "q", Type: "string", Required: true, Example: "cats"},
			{Name: "video_id", Type: "string", Required: false, Positional: true},
		},
	}

	got := endpointHappyArgs(ep)
	assert.Equal(t, "--q=cats", got)
	assert.NotContains(t, got, "video_id")
	assert.NotContains(t, got, "550e8400-e29b-41d4-a716-446655440000")
	assert.True(t, requiredInputsAreDerivable(ep))
}

func TestEndpointHappyArgsDoesNotShiftLaterPositionals(t *testing.T) {
	t.Parallel()

	ep := spec.Endpoint{
		Method: "GET",
		Path:   "/export/{format}/{start}",
		Params: []spec.Param{
			{Name: "format", Type: "string", Required: false, Positional: true, PathParam: true},
			{Name: "start", Type: "string", Required: true, Positional: true, PathParam: true, Format: "date"},
		},
	}

	got := endpointHappyArgs(ep)
	assert.Empty(t, got)
	assert.NotContains(t, got, "2026-01-15")
	assert.False(t, requiredInputsAreDerivable(ep))

	apiSpec := minimalSpec("shift-pos")
	g := New(apiSpec, t.TempDir())
	example := g.exampleLine("export", "get", ep)
	assert.Empty(t, example)
	assert.NotContains(t, example, "2026-01-15")
	parts := commandExampleArgParts(ep)
	assert.NotContains(t, parts, "2026-01-15")
}

func TestEndpointHappyArgsEncodesNegativeNumbers(t *testing.T) {
	t.Parallel()

	ep := spec.Endpoint{
		Method: "GET",
		Path:   "/bbox",
		Params: []spec.Param{
			{Name: "west", Type: "number", Required: true, Example: "-122.1"},
			{Name: "south", Type: "number", Required: true, Example: "34.5"},
		},
	}

	assert.Equal(t, "--west=-122.1;--south=34.5", endpointHappyArgs(ep))
}

func TestEscapeHappyArgValueEscapesSemicolons(t *testing.T) {
	t.Parallel()
	assert.Equal(t, `a\;b`, escapeHappyArgValue("a;b"))
}

func TestEndpointHappyArgsEncodesSemicolonsWithoutBackslash(t *testing.T) {
	t.Parallel()

	ep := spec.Endpoint{
		Method: "GET",
		Path:   "/search",
		Params: []spec.Param{{Name: "q", Type: "string", Required: true, Enum: []string{"a;b"}}},
	}
	assert.Equal(t, `--q=a\;b`, endpointHappyArgs(ep))
}

func TestEndpointHappyArgsRejectsLiteralBackslash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		endpoint spec.Endpoint
	}{
		{
			name: "enum backslash before semicolon",
			endpoint: spec.Endpoint{
				Method: "GET",
				Path:   "/search",
				Params: []spec.Param{{Name: "q", Type: "string", Required: true, Enum: []string{`a\;b`}}},
			},
		},
		{
			name: "dispatch default backslash before semicolon",
			endpoint: spec.Endpoint{
				Method: "GET",
				Path:   "/",
				Params: []spec.Param{{Name: "type", Type: "string", Required: true, Default: `a\;b`, DispatchParam: true}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := endpointHappyArgs(tt.endpoint)
			assert.Empty(t, got)
			assert.NotContains(t, got, `a;b`)
		})
	}
}

func TestRequiredInputsAreDerivableVacuousAndOpaque(t *testing.T) {
	t.Parallel()

	assert.True(t, requiredInputsAreDerivable(spec.Endpoint{
		Method: "GET",
		Path:   "/items",
	}))
	assert.False(t, requiredInputsAreDerivable(spec.Endpoint{
		Method: "GET",
		Path:   "/videos/{video_id}",
		Params: []spec.Param{
			{Name: "video_id", Type: "string", Required: true, Positional: true, PathParam: true},
		},
	}))
}

func TestGeneratedCommandSynthesizesHappyArgsFromExamples(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("happy-args-synth")
	apiSpec.Resources["forecast"] = spec.Resource{
		Description: "Weather forecast",
		Endpoints: map[string]spec.Endpoint{
			"get": {
				Method:      "GET",
				Path:        "/v1/forecast",
				Description: "Get a forecast",
				Params: []spec.Param{
					{Name: "latitude", Type: "string", Required: true, Example: "52.52"},
					{Name: "longitude", Type: "string", Required: true, Example: "13.41"},
					{Name: "units", Type: "string", Required: true, Enum: []string{"metric", "imperial"}},
					{Name: "start", Type: "string", Required: true, Format: "date"},
				},
			},
			"list": {
				Method:      "GET",
				Path:        "/v1/forecasts",
				Description: "List forecasts",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	source := readGeneratedFile(t, outputDir, "internal", "cli", "forecast_get.go")
	assert.Contains(t, source, `happy-args-synth-pp-cli forecast get --latitude 52.52 --longitude 13.41 --units metric --start 2026-01-15`)
	assert.NotContains(t, source, "example-value")
	assert.NotContains(t, source, "TODO: replace placeholder example values")
	assert.Contains(t, source, `"pp:happy-args": "--latitude=52.52;--longitude=13.41;--units=metric;--start=2026-01-15"`)
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedCommandDoesNotInventHappyArgsForUnderivableParams(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("happy-args-opaque")
	apiSpec.Resources["videos"] = spec.Resource{
		Description: "Videos",
		Endpoints: map[string]spec.Endpoint{
			"get": {
				Method:      "GET",
				Path:        "/videos/{video_id}",
				Description: "Get a video",
				Params: []spec.Param{
					{Name: "video_id", Type: "string", Required: true, Positional: true, PathParam: true},
				},
			},
			"search": {
				Method:      "GET",
				Path:        "/search",
				Description: "Search videos",
				Params: []spec.Param{
					{Name: "q", Type: "string", Required: true},
				},
			},
		},
	}
	apiSpec.Resources["forecast"] = spec.Resource{
		Description: "Forecast",
		Endpoints: map[string]spec.Endpoint{
			"get": {
				Method:      "GET",
				Path:        "/v1/forecast",
				Description: "Get a forecast",
				Params: []spec.Param{
					{Name: "latitude", Type: "string", Required: true, Description: "Geographical WGS84 coordinates."},
					{Name: "longitude", Type: "string", Required: true},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	videoSrc := readGeneratedFile(t, outputDir, "internal", "cli", "videos_get.go")
	assert.NotContains(t, videoSrc, "pp:happy-args")
	assert.NotContains(t, videoSrc, "example-value")
	assert.NotContains(t, videoSrc, "TODO: replace placeholder example values")
	assert.NotContains(t, videoSrc, "550e8400-e29b-41d4-a716-446655440000")
	assert.NotContains(t, videoSrc, "Example:")

	searchSrc := readGeneratedFile(t, outputDir, "internal", "cli", "videos_search.go")
	assert.NotContains(t, searchSrc, "pp:happy-args")
	assert.NotContains(t, searchSrc, "example-value")
	assert.NotContains(t, searchSrc, "TODO: replace placeholder example values")
	assert.NotContains(t, searchSrc, "Example:")

	forecastSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_forecast.go")
	assert.NotContains(t, forecastSrc, "pp:happy-args")
	assert.NotContains(t, forecastSrc, "example-value")
	assert.NotContains(t, forecastSrc, "52.52")
	assert.NotContains(t, forecastSrc, "13.41")
	assert.NotContains(t, forecastSrc, "TODO: replace placeholder example values")
	assert.NotContains(t, forecastSrc, "Example:")
	requireGeneratedCompiles(t, outputDir)
}

func TestExampleLineOmitsInventedOpaqueIDs(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("opaque-ex")
	g := New(apiSpec, t.TempDir())
	got := g.exampleLine("videos", "get", spec.Endpoint{
		Method: "GET",
		Path:   "/videos/{video_id}",
		Params: []spec.Param{
			{Name: "video_id", Type: "string", Required: true, Positional: true, PathParam: true},
		},
	})
	assert.Empty(t, got)
}

func TestExampleLineKeepsSpecDeclaredOpaqueExample(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("opaque-spec-ex")
	g := New(apiSpec, t.TempDir())
	got := g.exampleLine("videos", "get", spec.Endpoint{
		Example: "  opaque-spec-ex-pp-cli videos get real-id",
		Params: []spec.Param{
			{Name: "video_id", Type: "string", Required: true, Positional: true, PathParam: true},
		},
	})
	assert.Equal(t, "  opaque-spec-ex-pp-cli videos get real-id", got)
}

func TestExampleLineKeepsVacuousList(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("vacuous-ex")
	g := New(apiSpec, t.TempDir())
	got := g.exampleLine("items", "list", spec.Endpoint{Method: "GET", Path: "/items"})
	assert.Equal(t, "  vacuous-ex-pp-cli items list", got)
}

func TestGeneratedCommandOmitsInventedExampleWhenHappyArgsDeclared(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("declared-happy-opaque")
	apiSpec.Resources["videos"] = spec.Resource{
		Description: "Videos",
		Endpoints: map[string]spec.Endpoint{
			"get": {
				Method:      "GET",
				Path:        "/videos/{video_id}",
				Description: "Get a video",
				HappyArgs:   "video_id=real-from-spec",
				Params: []spec.Param{
					{Name: "video_id", Type: "string", Required: true, Positional: true, PathParam: true},
				},
			},
			"list": {
				Method:      "GET",
				Path:        "/videos",
				Description: "List videos",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	source := readGeneratedFile(t, outputDir, "internal", "cli", "videos_get.go")
	assert.Contains(t, source, `"pp:happy-args": "video_id=real-from-spec"`)
	assert.NotContains(t, source, "550e8400-e29b-41d4-a716-446655440000")
	assert.NotContains(t, source, "Example:")
	requireGeneratedCompiles(t, outputDir)
}
