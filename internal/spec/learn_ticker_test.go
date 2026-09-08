package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func learnTickerSpec(patterns []string) APISpec {
	return APISpec{
		Name:      "demo",
		BaseURL:   "http://x",
		Resources: map[string]Resource{"items": {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/items"}}}},
		Learn: LearnConfig{
			Enabled:        true,
			TickerPatterns: patterns,
		},
	}
}

func TestValidateLearnTickerPlaybookReachability(t *testing.T) {
	t.Run("greedy lowercase pattern is rejected naming pattern and example", func(t *testing.T) {
		s := learnTickerSpec([]string{`^[a-z0-9]{2,12}$`})
		err := s.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ticker_patterns[0]")
		assert.Contains(t, err.Error(), `^[a-z0-9]{2,12}$`)
		assert.Contains(t, err.Error(), "alpha example query")
		assert.Contains(t, err.Error(), "QueryFamily would be empty")
		assert.Contains(t, err.Error(), "unreachable at recall time")
	})

	t.Run("uppercase identifier pattern is accepted", func(t *testing.T) {
		s := learnTickerSpec([]string{`^[A-Z][A-Z0-9]{2,11}$`})
		require.NoError(t, s.Validate())
	})

	t.Run("no ticker patterns is accepted", func(t *testing.T) {
		s := learnTickerSpec(nil)
		require.NoError(t, s.Validate())
	})

	t.Run("slug pattern that cannot match ordinary words is accepted", func(t *testing.T) {
		s := learnTickerSpec([]string{`^will-[a-z0-9-]+$`})
		require.NoError(t, s.Validate())
	})

	t.Run("digit-only identifier pattern is accepted", func(t *testing.T) {
		s := learnTickerSpec([]string{`^[0-9]{9}$`})
		require.NoError(t, s.Validate())
	})

	t.Run("pattern that claims one token but leaves a family is accepted", func(t *testing.T) {
		s := learnTickerSpec([]string{`^alpha$`})
		require.NoError(t, s.Validate())
	})

	t.Run("hyphenated lowercase slug pattern is accepted", func(t *testing.T) {
		s := learnTickerSpec([]string{`^[a-z]{2,4}-[a-z]+$`})
		require.NoError(t, s.Validate())
	})

	t.Run("disabled loop skips reachability even with a greedy pattern", func(t *testing.T) {
		s := learnTickerSpec([]string{`^[a-z0-9]{2,12}$`})
		s.Learn.Enabled = false
		s.Learn.Disabled = true
		require.NoError(t, s.Validate())
	})

	t.Run("legacy enabled false skips reachability even with a greedy pattern", func(t *testing.T) {
		s := learnTickerSpec([]string{`^[a-z0-9]{2,12}$`})
		s.Learn.Enabled = false
		s.Learn.EnabledSet = true
		s.Learn.Disabled = false
		require.NoError(t, s.Validate())
	})

	t.Run("authored extra example is rejected when a specific pattern swallows it", func(t *testing.T) {
		learn := LearnConfig{
			Enabled:        true,
			TickerPatterns: []string{`^nccpl-[a-z]+$`},
		}
		require.NoError(t, learn.queryFamilyReachabilityError(learnSeededQueryFamilyExamples))
		err := CheckLearnQueryFamilyReachability(&learn, []LearnQueryFamilyExample{
			{Source: "playbooks/nccpl.json", Query: "nccpl-alpha nccpl-beta"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ticker_patterns[0]")
		assert.Contains(t, err.Error(), "nccpl-[a-z]+")
		assert.Contains(t, err.Error(), "nccpl-alpha nccpl-beta")
		assert.Contains(t, err.Error(), "playbooks/nccpl.json")
	})
}

func TestLearnSeededQueryFamilyQueriesAreNonEmptyWithoutTickers(t *testing.T) {
	for _, ex := range learnSeededQueryFamilyExamples {
		got := learnNonEntityTokens(ex.Query, nil, learnDefaultStopwords)
		require.NotEmpty(t, got, "seeded example %q from %s must produce a QueryFamily without ticker patterns", ex.Query, ex.Source)
	}
}

func TestParsePlaybookQueryFamilyExamples(t *testing.T) {
	got, err := ParsePlaybookQueryFamilyExamples([]byte(`{
  "query_family_examples": ["alpha example query", "alpha second phrasing"],
  "steps": [{"cmd": "x"}]
}`))
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha example query", "alpha second phrasing"}, got)
}
