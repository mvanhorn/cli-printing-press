package browsersniff

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
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
				{Name: "limit", Type: "integer", Required: true, Description: ""},
				{Name: "page", Type: "integer", Required: true, Description: ""},
				{Name: "q", Type: "string", Required: true, Description: ""},
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

func TestParseFormBody(t *testing.T) {
	t.Parallel()

	assert.Equal(t, map[string]string{
		"page":  "1",
		"q":     "hello world",
		"token": "abc+123",
	}, ParseFormBody("q=hello+world&page=1&token=abc%2B123"))
}
