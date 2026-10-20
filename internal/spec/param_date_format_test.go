package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParamHasDateFormat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		format string
		want   bool
	}{
		{name: "date", format: "date", want: true},
		{name: "date-time", format: "date-time", want: true},
		{name: "padded date-time", format: " Date-Time ", want: true},
		{name: "upper date", format: "DATE", want: true},
		{name: "int64", format: "int64", want: false},
		{name: "empty", format: "", want: false},
		{name: "partial", format: "date-time-local", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, Param{Format: tc.format}.HasDateFormat())
		})
	}
}
