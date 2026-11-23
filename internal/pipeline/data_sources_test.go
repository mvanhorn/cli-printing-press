package pipeline

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeDataSources(t *testing.T) {
	tests := []struct {
		name         string
		raw          []DataSource
		want         []DataSource
		wantWarnings int
	}{
		{name: "nil", raw: nil},
		{name: "empty", raw: []DataSource{}},
		{
			name: "valid list passes unchanged",
			raw:  []DataSource{{Name: "FEMA National Shelter System", Homepage: "https://gis.fema.gov"}, {Name: "American Red Cross", Homepage: "https://www.redcross.org"}},
			want: []DataSource{{Name: "FEMA National Shelter System", Homepage: "https://gis.fema.gov"}, {Name: "American Red Cross", Homepage: "https://www.redcross.org"}},
		},
		{
			name: "name-only entry is kept",
			raw:  []DataSource{{Name: "Kayak"}},
			want: []DataSource{{Name: "Kayak"}},
		},
		{
			name: "whitespace is trimmed",
			raw:  []DataSource{{Name: "  TMDb  ", Homepage: "  https://www.themoviedb.org  "}},
			want: []DataSource{{Name: "TMDb", Homepage: "https://www.themoviedb.org"}},
		},
		{
			name:         "nameless entry is dropped",
			raw:          []DataSource{{Name: "  ", Homepage: "https://example.com"}, {Name: "OMDb"}},
			want:         []DataSource{{Name: "OMDb"}},
			wantWarnings: 1,
		},
		{
			name:         "invalid homepages are cleared and entries kept",
			raw:          []DataSource{{Name: "A", Homepage: "http://a.com"}, {Name: "B", Homepage: "https://localhost"}, {Name: "C", Homepage: "https://{tenant}.c.com"}},
			want:         []DataSource{{Name: "A"}, {Name: "B"}, {Name: "C"}},
			wantWarnings: 3,
		},
		{
			name:         "case-insensitive duplicate keeps the first",
			raw:          []DataSource{{Name: "Kayak", Homepage: "https://www.kayak.com"}, {Name: "kayak"}},
			want:         []DataSource{{Name: "Kayak", Homepage: "https://www.kayak.com"}},
			wantWarnings: 1,
		},
		{
			name:         "all entries invalid returns nil",
			raw:          []DataSource{{Name: ""}},
			wantWarnings: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, warnings := NormalizeDataSources(tt.raw)
			assert.Equal(t, tt.want, got)
			assert.Len(t, warnings, tt.wantWarnings)
		})
	}
}

func TestNormalizeDataSourcesCapsAtTwelveInOrder(t *testing.T) {
	var raw []DataSource
	for i := 1; i <= 13; i++ {
		raw = append(raw, DataSource{Name: fmt.Sprintf("Source %d", i)})
	}

	got, warnings := NormalizeDataSources(raw)

	assert.Len(t, got, 12)
	assert.Equal(t, "Source 1", got[0].Name)
	assert.Equal(t, "Source 12", got[11].Name)
	assert.Len(t, warnings, 1)
}
