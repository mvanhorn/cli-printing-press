package spec

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestPersonIsZero(t *testing.T) {
	assert.True(t, Person{}.IsZero())
	assert.False(t, Person{Handle: "trevin-chow"}.IsZero())
	assert.False(t, Person{Name: "Trevin Chow"}.IsZero())
	assert.False(t, Person{Handle: "trevin-chow", Name: "Trevin Chow"}.IsZero())
}

func TestPersonClean(t *testing.T) {
	// Control chars and markdown/HTML metacharacters are stripped from Name;
	// the handle is constrained to the GitHub-handle charset.
	got := Person{Handle: "ev il)x", Name: "Jane]\n<b>`code`"}.Clean()
	assert.Equal(t, "evilx", got.Handle, "handle keeps only [A-Za-z0-9-_]")
	assert.Equal(t, "Janebcode", got.Name, "name drops ] newline < > backtick")

	// Benign values — including parentheses, periods, and apostrophes — survive.
	clean := Person{Handle: "trevin-chow", Name: "Trevin Q. O'Chow (TQC)"}.Clean()
	assert.Equal(t, Person{Handle: "trevin-chow", Name: "Trevin Q. O'Chow (TQC)"}, clean)
}

func TestSamePerson(t *testing.T) {
	assert.True(t, SamePerson(Person{Handle: "Trevin-Chow"}, Person{Handle: "trevin-chow"}))
	assert.True(t, SamePerson(Person{Name: "Trevin Chow"}, Person{Name: "trevin chow"}))
	assert.False(t, SamePerson(Person{Handle: "trevin-chow"}, Person{Name: "Trevin Chow"}))
	assert.False(t, SamePerson(Person{Name: "Trevin Chow"}, Person{Name: ""}))
}

func TestPrependContributor(t *testing.T) {
	contributors := []Person{{Handle: "jane-doe", Name: "Jane Doe"}}

	got := PrependContributor(contributors, Person{Handle: "tmchow", Name: "Trevin Chow"})
	require.Equal(t, []Person{
		{Handle: "tmchow", Name: "Trevin Chow"},
		{Handle: "jane-doe", Name: "Jane Doe"},
	}, got)
	assert.Equal(t, []Person{{Handle: "jane-doe", Name: "Jane Doe"}}, contributors, "input slice is copied")

	got = PrependContributor(got, Person{Handle: "TMCHOW", Name: "Trevin Chow"})
	require.Equal(t, []Person{
		{Handle: "tmchow", Name: "Trevin Chow"},
		{Handle: "jane-doe", Name: "Jane Doe"},
	}, got)

	got = PrependContributor(contributors, Person{})
	assert.Equal(t, contributors, got)
}

// An empty creator must not serialize as `creator: {}` / `"creator":{}` —
// otherwise every generated spec and golden fixture gains attribution noise.
// JSON relies on omitzero (Go 1.24+, honoring IsZero); YAML relies on
// omitempty (yaml.v3 honors the IsZeroer interface).
func TestAPISpecCreatorOmittedWhenEmpty(t *testing.T) {
	s := APISpec{Name: "acme"}

	j, err := json.Marshal(s)
	require.NoError(t, err)
	assert.NotContains(t, string(j), `"creator"`)
	assert.NotContains(t, string(j), `"contributors"`)

	y, err := yaml.Marshal(s)
	require.NoError(t, err)
	assert.NotContains(t, string(y), "creator:")
	assert.NotContains(t, string(y), "contributors:")
}

func TestAPISpecCreatorRoundTrip(t *testing.T) {
	s := APISpec{
		Name:    "acme",
		Creator: Person{Handle: "trevin-chow", Name: "Trevin Chow"},
		Contributors: []Person{
			{Handle: "jane-doe", Name: "Jane Doe"},
			{Handle: "mvanhorn", Name: "Matt Van Horn"},
		},
	}

	j, err := json.Marshal(s)
	require.NoError(t, err)
	var back APISpec
	require.NoError(t, json.Unmarshal(j, &back))
	assert.Equal(t, s.Creator, back.Creator)
	assert.Equal(t, s.Contributors, back.Contributors)
}
