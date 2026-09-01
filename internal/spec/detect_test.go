package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLooksLikeInternalYAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			name: "minimal internal yaml",
			data: []byte("name: payments\nbase_url: https://api.example.com\nresources:\n  payments: {}\n"),
			want: true,
		},
		{
			name: "document start marker then name",
			data: []byte("---\nname: payments\nresources:\n  payments: {}\n"),
			want: true,
		},
		{
			name: "comment preamble",
			data: []byte("# header\n\nname: payments\nresources:\n  payments: {}\n"),
			want: true,
		},
		{
			name: "type and scalar only in description prose",
			data: []byte("name: payments\nbase_url: https://api.example.com\nresources:\n  payments:\n    endpoints:\n      list:\n        method: GET\n        path: /payments\n        params:\n          - name: payment_type\n            description: Free-text payment type label. Also a scalar value.\n"),
			want: true,
		},
		{
			name: "missing resources",
			data: []byte("name: payments\nbase_url: https://api.example.com\n"),
			want: false,
		},
		{
			name: "missing name",
			data: []byte("base_url: https://api.example.com\nresources:\n  payments: {}\n"),
			want: false,
		},
		{
			name: "openapi yaml",
			data: []byte("openapi: 3.0.0\ninfo:\n  title: Test\n"),
			want: false,
		},
		{
			name: "graphql sdl",
			data: []byte("type Query {\n  hello: String\n}\n"),
			want: false,
		},
		{
			name: "graphql fields named name and resources",
			data: []byte("type Query {\nname: String\nresources: [Widget!]!\n}\n\ntype Widget {\n  id: ID!\n}\n"),
			want: false,
		},
		{
			name: "graphql fields with indented closing brace",
			data: []byte("type Query {\nname: String\nresources: [Widget!]!\n  }\n\ntype Widget {\n  id: ID!\n}\n"),
			want: false,
		},
		{
			name: "flow-style resources mapping",
			data: []byte("name: payments\nbase_url: https://api.example.com\nresources: {payments: {endpoints: {list: {method: GET, path: /payments}}}}\n"),
			want: true,
		},
		{
			name: "flow-style resources with line-anchored type Query in description",
			data: []byte("name: payments\ndescription: |\n  type Query {\n    ignored: String\n  }\nresources: {payments: {}}\n"),
			want: true,
		},
		{
			name: "empty",
			data: []byte{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, LooksLikeInternalYAML(tt.data))
		})
	}
}
