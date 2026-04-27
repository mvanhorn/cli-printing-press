package canonicalargs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLookup_KnownNames(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"since", "2026-01-01"},
		{"until", "2026-12-31"},
		{"tag", "mock-tag"},
		{"vertical", "mock-vertical"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Lookup(c.name)
			assert.True(t, ok, "Lookup(%q) ok", c.name)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestLookup_CaseInsensitive(t *testing.T) {
	got, ok := Lookup("Since")
	assert.True(t, ok)
	assert.Equal(t, "2026-01-01", got)

	got, ok = Lookup("  TAG  ")
	assert.True(t, ok)
	assert.Equal(t, "mock-tag", got)
}

// Registry hygiene: domain-specific names must NOT appear in the generic
// registry. Spec authors set Param.Default for these.
func TestLookup_DomainSpecificNamesAbsent(t *testing.T) {
	domain := []string{
		"servings",     // recipe (food52)
		"ingredient",   // recipe
		"recipe_id",    // recipe
		"cuisine",      // recipe
		"sport",        // sports (espn)
		"league",       // sports
		"airport_code", // travel
		"ticker",       // finance
	}
	for _, name := range domain {
		t.Run(name, func(t *testing.T) {
			_, ok := Lookup(name)
			assert.False(t, ok, "%q must not be in the generic registry — set Param.Default in the spec instead", name)
		})
	}
}

func TestLookup_UnknownReturnsFalse(t *testing.T) {
	_, ok := Lookup("totally-made-up-name")
	assert.False(t, ok)
	_, ok = Lookup("")
	assert.False(t, ok)
}
