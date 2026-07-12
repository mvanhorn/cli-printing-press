package generator

import (
	"strconv"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExampleValueNumericTypeBeatsDateTimeNameHeuristics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		param     spec.Param
		expected  string
		isNumeric bool
	}{
		{
			name:      "integer start_time stays numeric",
			param:     spec.Param{Name: "start_time", Type: "integer"},
			isNumeric: true,
		},
		{
			name:      "date-time format remains RFC3339",
			param:     spec.Param{Name: "created_at", Type: "string", Format: "date-time"},
			expected:  "2026-01-15T09:00:00Z",
			isNumeric: false,
		},
		{
			name:      "string start_date remains date example",
			param:     spec.Param{Name: "start_date", Type: "string"},
			expected:  "2026-01-15",
			isNumeric: false,
		},
		{
			// "end_time" contains "time", so the pre-fix code returned an
			// RFC3339 string; the numeric type must now win.
			name:      "number end_time stays numeric",
			param:     spec.Param{Name: "end_time", Type: "number"},
			isNumeric: true,
		},
		{
			// "event_date" contains "date"; integer type must win.
			name:      "integer event_date stays numeric",
			param:     spec.Param{Name: "event_date", Type: "integer"},
			isNumeric: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := exampleValue(tt.param)

			if tt.isNumeric {
				assert.NotContains(t, got, "T09:00:00Z")
				assert.NotContains(t, got, "-")
				_, err := strconv.ParseInt(got, 10, 64)
				require.NoError(t, err)
				return
			}

			assert.Equal(t, tt.expected, got)
			if tt.param.Format == "date-time" {
				assert.True(t, strings.Contains(got, "T09:00:00Z"))
			}
		})
	}
}
