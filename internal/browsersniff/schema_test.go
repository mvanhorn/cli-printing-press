package browsersniff

import (
	"encoding/json"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestInferResponseSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		bodies []string
		want   []spec.Param
	}{
		{
			name:   "infers simple object fields",
			bodies: []string{`{"id":1,"name":"test","created_at":"2026-01-01T00:00:00Z"}`},
			want: []spec.Param{
				{Name: "created_at", Type: "string", Required: true, Description: "", Format: "date-time"},
				{Name: "id", Type: "integer", Required: true, Description: ""},
				{Name: "name", Type: "string", Required: true, Description: ""},
			},
		},
		{
			name:   "merges multiple samples and marks optional fields",
			bodies: []string{`{"a":1,"b":"x"}`, `{"a":2,"c":true}`},
			want: []spec.Param{
				{Name: "a", Type: "integer", Required: true, Description: ""},
				{Name: "b", Type: "string", Required: false, Description: ""},
				{Name: "c", Type: "boolean", Required: false, Description: ""},
			},
		},
		{
			name:   "infers from array response using first element",
			bodies: []string{`[{"id":1},{"id":2}]`},
			want: []spec.Param{
				{Name: "id", Type: "integer", Required: true, Description: ""},
			},
		},
		{
			name:   "returns empty for empty body",
			bodies: []string{""},
			want:   nil,
		},
		{
			name:   "returns empty for non json body",
			bodies: []string{"not json"},
			want:   nil,
		},
		{
			name:   "limits nested object recursion at depth three",
			bodies: []string{`{"outer":{"middle":{"inner":{"deep":{"value":1}}}}}`},
			want: []spec.Param{
				{
					Name:        "outer",
					Type:        "object",
					Required:    true,
					Description: "",
					Fields: []spec.Param{
						{
							Name:        "middle",
							Type:        "object",
							Required:    true,
							Description: "",
							Fields: []spec.Param{
								{
									Name:        "inner",
									Type:        "object",
									Required:    true,
									Description: "",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, InferResponseSchema(tt.bodies))
		})
	}
}

func TestInferRequestSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		body        string
		contentType string
		want        []spec.Param
	}{
		{
			name:        "infers form body values",
			body:        "q=hello&page=1&limit=20",
			contentType: "application/x-www-form-urlencoded",
			want: []spec.Param{
				{Name: "limit", Type: "integer", Required: true, Default: "20", Description: ""},
				{Name: "page", Type: "integer", Required: true, Default: "1", Description: ""},
				{Name: "q", Type: "string", Required: true, Default: "hello", Description: ""},
			},
		},
		{
			name:        "infers json request body",
			body:        `{"active":true,"count":1,"name":"test"}`,
			contentType: "application/json",
			want: []spec.Param{
				{Name: "active", Type: "boolean", Required: true, Description: ""},
				{Name: "count", Type: "integer", Required: true, Description: ""},
				{Name: "name", Type: "string", Required: true, Description: ""},
			},
		},
		{
			name:        "raw json under form content type is modeled as json fields",
			body:        `{"email":"ada@example.com","remember":true}`,
			contentType: "application/x-www-form-urlencoded; charset=UTF-8",
			want: []spec.Param{
				{Name: "email", Type: "string", Required: true, Description: "", Format: "email"},
				{Name: "remember", Type: "boolean", Required: true, Description: ""},
			},
		},
		{
			name:        "returns empty for unsupported content type",
			body:        "a=b",
			contentType: "text/plain",
			want:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, InferRequestSchema(tt.body, tt.contentType))
		})
	}
}

func TestInferSchemaEmptyContainersAreOptional(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		bodies []string
	}{
		{
			name:   "single sample",
			bodies: []string{`{"anticipatedAdvance":[],"meta":{},"filters":{"tags":[],"note":{}},"name":"ada","ids":[1,2]}`},
		},
		{
			name: "every sample empty",
			bodies: []string{
				`{"anticipatedAdvance":[],"meta":{},"filters":{"tags":[],"note":{}},"name":"ada","ids":[1,2]}`,
				`{"anticipatedAdvance":[],"meta":{},"filters":{"tags":[],"note":{}},"name":"grace","ids":[3]}`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			params := InferResponseSchema(tt.bodies)
			if len(tt.bodies) == 1 {
				assert.Equal(t, params, InferRequestSchema(tt.bodies[0], "application/json"))
			}

			advance := findBodyParam(params, "anticipatedAdvance")
			require.NotNil(t, advance)
			assertOptionalEmptyContainer(t, *advance, "[]")

			meta := findBodyParam(params, "meta")
			require.NotNil(t, meta)
			assertOptionalEmptyContainer(t, *meta, "{}")

			name := findBodyParam(params, "name")
			require.NotNil(t, name)
			assert.True(t, name.Required)
			assert.Nil(t, name.Default)
			assert.Equal(t, "string", name.Type)

			ids := findBodyParam(params, "ids")
			require.NotNil(t, ids)
			assert.True(t, ids.Required)
			assert.Nil(t, ids.Default)
			assert.Equal(t, "array", ids.Type)

			filters := findBodyParam(params, "filters")
			require.NotNil(t, filters)
			assert.True(t, filters.Required)
			assert.Equal(t, "object", filters.Type)
			assert.Nil(t, filters.Default)

			tags := findBodyParam(filters.Fields, "tags")
			require.NotNil(t, tags)
			assertOptionalEmptyContainer(t, *tags, "[]")

			note := findBodyParam(filters.Fields, "note")
			require.NotNil(t, note)
			assertOptionalEmptyContainer(t, *note, "{}")
		})
	}
}

func TestInferSchemaMixedEmptyContainersStayRequired(t *testing.T) {
	t.Parallel()

	bodies := [][]string{
		{`{"tags":[],"meta":{},"name":"ada"}`, `{"tags":["a"],"meta":{"id":1},"name":"grace"}`},
		{`{"tags":["a"],"meta":{"id":1},"name":"ada"}`, `{"tags":[],"meta":{},"name":"grace"}`},
	}
	for _, samples := range bodies {
		params := InferResponseSchema(samples)
		tags := findBodyParam(params, "tags")
		require.NotNil(t, tags)
		assert.True(t, tags.Required, "samples %v", samples)
		assert.Nil(t, tags.Default)

		meta := findBodyParam(params, "meta")
		require.NotNil(t, meta)
		assert.True(t, meta.Required, "samples %v", samples)
		assert.Nil(t, meta.Default)
		assert.Equal(t, "object", meta.Type)

		nested := findBodyParam(meta.Fields, "id")
		require.NotNil(t, nested)
		assert.Equal(t, "integer", nested.Type)
	}
}

func TestInferSchemaEmptyContainerMissingFromOneSample(t *testing.T) {
	t.Parallel()

	params := InferResponseSchema([]string{
		`{"tags":[],"meta":{},"name":"ada"}`,
		`{"name":"grace"}`,
	})
	tags := findBodyParam(params, "tags")
	require.NotNil(t, tags)
	assert.False(t, tags.Required)
	assertContainerDefault(t, tags.Default, "[]")

	meta := findBodyParam(params, "meta")
	require.NotNil(t, meta)
	assert.False(t, meta.Required)
	assertContainerDefault(t, meta.Default, "{}")

	name := findBodyParam(params, "name")
	require.NotNil(t, name)
	assert.True(t, name.Required)
	assert.Nil(t, name.Default)
}

func TestInferSchemaConflictingEmptyContainersAreNotDefaults(t *testing.T) {
	t.Parallel()

	params := InferResponseSchema([]string{`{"box":[]}`, `{"box":{}}`})
	box := findBodyParam(params, "box")
	require.NotNil(t, box)
	assert.True(t, box.Required)
	assert.Nil(t, box.Default)
	assert.Equal(t, "object", box.Type)

	nested := InferResponseSchema([]string{`{"filter":{"box":[]}}`, `{"filter":{"box":{}}}`})
	filter := findBodyParam(nested, "filter")
	require.NotNil(t, filter)
	assert.True(t, filter.Required)
	nestedBox := findBodyParam(filter.Fields, "box")
	require.NotNil(t, nestedBox)
	assert.True(t, nestedBox.Required)
	assert.Nil(t, nestedBox.Default)
}

func TestInferRequestBodyEmptyContainers(t *testing.T) {
	t.Parallel()

	allEmpty := `{"anticipatedAdvance":[],"meta":{},"filters":{"tags":[],"note":{}},"name":"ada","ids":[1]}`
	t.Run("one sample", func(t *testing.T) {
		t.Parallel()
		assertRequestBodyEmptyContainers(t, inferRequestBody(jsonRequestEntries(allEmpty), nil))
	})
	t.Run("two empty samples", func(t *testing.T) {
		t.Parallel()
		assertRequestBodyEmptyContainers(t, inferRequestBody(jsonRequestEntries(allEmpty, allEmpty), nil))
	})

	mixed := [][]string{
		{`{"tags":[],"filter":{"ids":[],"name":"ada"}}`, `{"tags":["a"],"filter":{"ids":[1],"name":"grace"}}`},
		{`{"tags":["a"],"filter":{"ids":[1],"name":"ada"}}`, `{"tags":[],"filter":{"ids":[],"name":"grace"}}`},
	}
	for _, samples := range mixed {
		params := inferRequestBody(jsonRequestEntries(samples...), nil)
		tags := findBodyParam(params, "tags")
		require.NotNil(t, tags)
		assert.True(t, tags.Required, "samples %v", samples)
		assert.Nil(t, tags.Default)

		filter := findBodyParam(params, "filter")
		require.NotNil(t, filter)
		assert.True(t, filter.Required)
		ids := findBodyParam(filter.Fields, "ids")
		require.NotNil(t, ids)
		assert.True(t, ids.Required, "samples %v", samples)
		assert.Nil(t, ids.Default)
		name := findBodyParam(filter.Fields, "name")
		require.NotNil(t, name)
		assert.True(t, name.Required)
		assert.Nil(t, name.Default)
	}

	partial := inferRequestBody(jsonRequestEntries(`{"tags":[],"name":"ada"}`, `{"name":"grace"}`), nil)
	tags := findBodyParam(partial, "tags")
	require.NotNil(t, tags)
	assert.False(t, tags.Required)
	assertContainerDefault(t, tags.Default, "[]")

	conflict := inferRequestBody(jsonRequestEntries(`{"box":[]}`, `{"box":{}}`), nil)
	box := findBodyParam(conflict, "box")
	require.NotNil(t, box)
	assert.True(t, box.Required)
	assert.Nil(t, box.Default)

	for _, samples := range [][]string{
		{`{"filter":{"box":[]}}`, `{"filter":{"box":{}}}`},
		{`{"filter":{"box":{}}}`, `{"filter":{"box":[]}}`},
	} {
		nested := inferRequestBody(jsonRequestEntries(samples...), nil)
		filter := findBodyParam(nested, "filter")
		require.NotNil(t, filter)
		assert.True(t, filter.Required)
		nestedBox := findBodyParam(filter.Fields, "box")
		require.NotNil(t, nestedBox)
		assert.True(t, nestedBox.Required, "samples %v", samples)
		assert.Nil(t, nestedBox.Default)
		assert.Empty(t, nestedBox.Fields)
	}

	omitted := inferRequestBody(jsonRequestEntries(
		`{"filter":{"ids":[1],"name":"a"}}`,
		`{"filter":{"name":"b"}}`,
	), nil)
	omittedIDs := findBodyParam(findBodyParam(omitted, "filter").Fields, "ids")
	require.NotNil(t, omittedIDs)
	assert.False(t, omittedIDs.Required)
	assert.Nil(t, omittedIDs.Default)
	assert.Equal(t, "array", omittedIDs.Type)
	omittedName := findBodyParam(findBodyParam(omitted, "filter").Fields, "name")
	require.NotNil(t, omittedName)
	assert.True(t, omittedName.Required)

	omittedObject := inferRequestBody(jsonRequestEntries(
		`{"filter":{"config":{"name":"a","mode":"x"},"label":"a"}}`,
		`{"filter":{"label":"b"}}`,
	), nil)
	omittedConfig := findBodyParam(findBodyParam(omittedObject, "filter").Fields, "config")
	require.NotNil(t, omittedConfig)
	assert.False(t, omittedConfig.Required)
	assert.Nil(t, omittedConfig.Default)
	omittedConfigName := findBodyParam(omittedConfig.Fields, "name")
	omittedConfigMode := findBodyParam(omittedConfig.Fields, "mode")
	require.NotNil(t, omittedConfigName)
	require.NotNil(t, omittedConfigMode)
	assert.True(t, omittedConfigName.Required)
	assert.True(t, omittedConfigMode.Required)
	omittedLabel := findBodyParam(findBodyParam(omittedObject, "filter").Fields, "label")
	require.NotNil(t, omittedLabel)
	assert.True(t, omittedLabel.Required)

	omittedByEmpty := inferRequestBody(jsonRequestEntries(
		`{"filter":{"config":{"name":"a","mode":"x"},"label":"a"}}`,
		`{"filter":{}}`,
	), nil)
	emptyConfig := findBodyParam(findBodyParam(omittedByEmpty, "filter").Fields, "config")
	require.NotNil(t, emptyConfig)
	assert.False(t, emptyConfig.Required)
	emptyConfigName := findBodyParam(emptyConfig.Fields, "name")
	emptyConfigMode := findBodyParam(emptyConfig.Fields, "mode")
	require.NotNil(t, emptyConfigName)
	require.NotNil(t, emptyConfigMode)
	assert.True(t, emptyConfigName.Required)
	assert.True(t, emptyConfigMode.Required)

	gapped := inferRequestBody(jsonRequestEntries(
		`{"filter":{"ids":[1],"name":"a"}}`,
		`{"filter":{"name":"b"}}`,
		`{"filter":{"ids":[],"name":"c"}}`,
	), nil)
	gappedFilter := findBodyParam(gapped, "filter")
	require.NotNil(t, gappedFilter)
	gappedIDs := findBodyParam(gappedFilter.Fields, "ids")
	require.NotNil(t, gappedIDs)
	assert.False(t, gappedIDs.Required)
	assert.Nil(t, gappedIDs.Default)
	gappedName := findBodyParam(gappedFilter.Fields, "name")
	require.NotNil(t, gappedName)
	assert.True(t, gappedName.Required)
	assert.Nil(t, gappedName.Default)

	restored := inferRequestBody(jsonRequestEntries(
		`{"filter":{"ids":[1],"name":"a"}}`,
		`{"filter":{"name":"b"}}`,
		`{"filter":{"ids":[2],"name":"c"}}`,
	), nil)
	restoredIDs := findBodyParam(findBodyParam(restored, "filter").Fields, "ids")
	require.NotNil(t, restoredIDs)
	assert.False(t, restoredIDs.Required)
	assert.Nil(t, restoredIDs.Default)

	gappedEmptyObject := inferRequestBody(jsonRequestEntries(
		`{"filter":{"ids":[1],"name":"a"}}`,
		`{"filter":{}}`,
		`{"filter":{"ids":[],"name":"c"}}`,
	), nil)
	gappedEmptyIDs := findBodyParam(findBodyParam(gappedEmptyObject, "filter").Fields, "ids")
	require.NotNil(t, gappedEmptyIDs)
	assert.False(t, gappedEmptyIDs.Required)
	assert.Nil(t, gappedEmptyIDs.Default)

	gappedEmpty := inferRequestBody(jsonRequestEntries(
		`{"filter":{"ids":[],"name":"a"}}`,
		`{"filter":{"name":"b"}}`,
		`{"filter":{"ids":[],"name":"c"}}`,
	), nil)
	stillEmpty := findBodyParam(findBodyParam(gappedEmpty, "filter").Fields, "ids")
	require.NotNil(t, stillEmpty)
	assert.False(t, stillEmpty.Required)
	assertContainerDefault(t, stillEmpty.Default, "[]")
}

func TestInferRequestSchemaFormValueThatLooksEmptyStaysRequiredString(t *testing.T) {
	t.Parallel()

	params := InferRequestSchema("tags=%5B%5D&meta=%7B%7D", "application/x-www-form-urlencoded")
	tags := findBodyParam(params, "tags")
	require.NotNil(t, tags)
	assert.True(t, tags.Required)
	assert.Equal(t, "string", tags.Type)
	assert.Equal(t, "[]", tags.Default)

	meta := findBodyParam(params, "meta")
	require.NotNil(t, meta)
	assert.True(t, meta.Required)
	assert.Equal(t, "{}", meta.Default)
}

func TestStampedEmptyContainerRejectsUnusableDefaults(t *testing.T) {
	t.Parallel()

	assert.Equal(t, containerUntracked, stampedEmptyContainerKind(spec.Param{Type: "array"}))
	assert.Equal(t, containerUntracked, stampedEmptyContainerKind(spec.Param{
		Type:    "array",
		Default: []any{},
		Fields:  []spec.Param{{Name: "item", Type: "string"}},
	}))
	assert.Equal(t, containerUntracked, stampedEmptyContainerKind(spec.Param{Type: "array", Default: make(chan int)}))
	assert.Equal(t, containerUntracked, stampedEmptyContainerKind(spec.Param{Type: "object", Default: []any{}}))
	assert.Equal(t, containerUntracked, stampedEmptyContainerKind(spec.Param{Type: "array", Default: map[string]any{}}))
	assert.Equal(t, containerEmptyArray, stampedEmptyContainerKind(spec.Param{Type: "array", Default: []any{}}))
	assert.Equal(t, containerEmptyObject, stampedEmptyContainerKind(spec.Param{Type: "object", Default: map[string]any{}}))
}

func TestEmptyContainerDefaultSurvivesYAML(t *testing.T) {
	t.Parallel()

	params := InferRequestSchema(`{"tags":[],"meta":{},"filters":{"note":{}}}`, "application/json")
	data, err := yaml.Marshal(params)
	require.NoError(t, err)

	var decoded []spec.Param
	require.NoError(t, yaml.Unmarshal(data, &decoded))
	tags := findBodyParam(decoded, "tags")
	require.NotNil(t, tags)
	assert.False(t, tags.Required)
	assertContainerDefault(t, tags.Default, "[]")

	meta := findBodyParam(decoded, "meta")
	require.NotNil(t, meta)
	assertContainerDefault(t, meta.Default, "{}")

	filters := findBodyParam(decoded, "filters")
	require.NotNil(t, filters)
	note := findBodyParam(filters.Fields, "note")
	require.NotNil(t, note)
	assert.False(t, note.Required)
	assertContainerDefault(t, note.Default, "{}")
}

func assertRequestBodyEmptyContainers(t *testing.T, params []spec.Param) {
	t.Helper()

	advance := findBodyParam(params, "anticipatedAdvance")
	require.NotNil(t, advance)
	assertOptionalEmptyContainer(t, *advance, "[]")

	meta := findBodyParam(params, "meta")
	require.NotNil(t, meta)
	assertOptionalEmptyContainer(t, *meta, "{}")

	name := findBodyParam(params, "name")
	require.NotNil(t, name)
	assert.True(t, name.Required)
	assert.Nil(t, name.Default)

	ids := findBodyParam(params, "ids")
	require.NotNil(t, ids)
	assert.True(t, ids.Required)
	assert.Nil(t, ids.Default)

	filters := findBodyParam(params, "filters")
	require.NotNil(t, filters)
	assert.True(t, filters.Required)
	tags := findBodyParam(filters.Fields, "tags")
	require.NotNil(t, tags)
	assertOptionalEmptyContainer(t, *tags, "[]")
	note := findBodyParam(filters.Fields, "note")
	require.NotNil(t, note)
	assertOptionalEmptyContainer(t, *note, "{}")
}

func assertOptionalEmptyContainer(t *testing.T, param spec.Param, want string) {
	t.Helper()
	assert.False(t, param.Required)
	assert.Empty(t, param.Fields)
	assertContainerDefault(t, param.Default, want)
}

func assertContainerDefault(t *testing.T, value any, want string) {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	assert.Equal(t, want, string(raw))
}

func jsonRequestEntries(bodies ...string) []EnrichedEntry {
	entries := make([]EnrichedEntry, 0, len(bodies))
	for _, body := range bodies {
		entries = append(entries, EnrichedEntry{
			RequestHeaders: map[string]string{"Content-Type": "application/json"},
			RequestBody:    body,
		})
	}
	return entries
}

func TestParseFormBody(t *testing.T) {
	t.Parallel()

	assert.Equal(t, map[string]string{
		"page":  "1",
		"q":     "hello world",
		"token": "abc+123",
	}, ParseFormBody("q=hello+world&page=1&token=abc%2B123"))
}
