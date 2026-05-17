package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnumIntLiteral(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{
			name:     "valid integers",
			input:    []string{"1", "2", "3"},
			expected: "1, 2, 3",
		},
		{
			name:     "mixed valid and invalid",
			input:    []string{"1", "foo", "3"},
			expected: "1, 3",
		},
		{
			name:     "all invalid",
			input:    []string{"foo", "bar"},
			expected: "",
		},
		{
			name:     "empty",
			input:    []string{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, enumIntLiteral(tt.input))
		})
	}
}
