package spec

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicInputNameSanitizesIllegalWireNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "bracket suffix (fathom precedent)", in: "recorded_by[]", want: "recorded_by"},
		{name: "bracket infix", in: "date[start]", want: "date_start"},
		{name: "bracket infix sibling stays distinct", in: "date[stop]", want: "date_stop"},
		{name: "clean snake unchanged", in: "dry_run", want: "dry_run"},
		{name: "clean camel unchanged", in: "locationId", want: "locationId"},
		{name: "clean dotted unchanged", in: "type.params.limit", want: "type.params.limit"},
		{name: "run of illegal chars collapses to one underscore", in: "a[<>]b", want: "a_b"},
		{name: "leading illegal trimmed", in: "$top", want: "top"},
		{name: "64-clamp with post-clamp re-trim", in: strings.Repeat("a", 63) + "[]" + "tail", want: strings.Repeat("a", 63)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Param{Name: tt.in}
			require.Equal(t, tt.want, p.PublicInputName())
		})
	}
}

// Two long names identical in their first 64 chars clamp to the SAME public
// key — documented here at the unit level; the generator's post-dedup
// assertion (Task 3) is what turns this into a loud generation failure.
func TestPublicInputNameClampCollision(t *testing.T) {
	t.Parallel()
	head := strings.Repeat("a", 64)
	require.Equal(t,
		Param{Name: head + "[one]"}.PublicInputName(),
		Param{Name: head + "[two]"}.PublicInputName())
}

// The all-illegal floor returns the RAW name so the post-dedup generation
// assertion (Task 3) fails loudly — never a synthetic placeholder.
func TestPublicInputNameAllIllegalReturnsRaw(t *testing.T) {
	t.Parallel()
	p := Param{Name: "<<>>"}
	require.Equal(t, "<<>>", p.PublicInputName())
}

// FlagName / IdentName branches are untouched by the sanitizer.
func TestPublicInputNamePrecedenceUnchanged(t *testing.T) {
	t.Parallel()
	require.Equal(t, "explicit-name", Param{Name: "recorded_by[]", FlagName: "explicit-name"}.PublicInputName())
	require.Equal(t, "recorded-by-2", Param{Name: "recorded_by[]", IdentName: "recorded_by[]_2"}.PublicInputName())
}

func TestMCPPropertyKeyRe(t *testing.T) {
	t.Parallel()
	require.True(t, MCPPropertyKeyRe.MatchString("recorded_by"))
	require.True(t, MCPPropertyKeyRe.MatchString("locationId"))
	require.False(t, MCPPropertyKeyRe.MatchString("recorded_by[]"))
	require.False(t, MCPPropertyKeyRe.MatchString(""))
	require.False(t, MCPPropertyKeyRe.MatchString(strings.Repeat("a", 65)))
}
