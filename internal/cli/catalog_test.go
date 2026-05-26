package cli

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runWithCapturedStdout executes fn while capturing os.Stdout via a pipe.
func runWithCapturedStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	origStdout := os.Stdout
	os.Stdout = w

	execErr := fn()
	w.Close()
	os.Stdout = origStdout

	out, _ := io.ReadAll(r)
	r.Close()
	return string(out), execErr
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	out, err := runWithCapturedStdout(t, func() error {
		fn()
		return nil
	})
	require.NoError(t, err)
	return out
}

func TestCatalogListJSON(t *testing.T) {
	cmd := newCatalogCmd()
	cmd.SetArgs([]string{"list", "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var entries []catalog.Entry
	require.NoError(t, json.Unmarshal([]byte(output), &entries))
	assert.Greater(t, len(entries), 0, "catalog should have entries")

	for _, e := range entries {
		assert.NotEmpty(t, e.Name)
		// Every entry must have either a spec_url or wrapper_libraries.
		if e.SpecURL == "" {
			assert.NotEmpty(t, e.WrapperLibraries, "entry %s has no spec_url and no wrapper_libraries", e.Name)
		}
	}
}

func TestCatalogShowStripeJSON(t *testing.T) {
	cmd := newCatalogCmd()
	cmd.SetArgs([]string{"show", "stripe", "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var entry catalog.Entry
	require.NoError(t, json.Unmarshal([]byte(output), &entry))
	assert.Equal(t, "stripe", entry.Name)
	assert.NotEmpty(t, entry.SpecURL)
	assert.Contains(t, entry.SpecURL, "https://")
}

func TestCatalogShowNonexistent(t *testing.T) {
	cmd := newCatalogCmd()
	cmd.SetArgs([]string{"show", "nonexistent-api-xyz"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCatalogSearchAuth(t *testing.T) {
	cmd := newCatalogCmd()
	cmd.SetArgs([]string{"search", "auth", "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var matches []catalog.Entry
	require.NoError(t, json.Unmarshal([]byte(output), &matches))
	assert.Greater(t, len(matches), 0, "search for 'auth' should return results")

	// Stytch is an auth-category entry
	found := false
	for _, m := range matches {
		if m.Name == "stytch" {
			found = true
			break
		}
	}
	assert.True(t, found, "stytch should appear in auth search results")
}

func TestCatalogSearchMatchesRegionAndAPILanguage(t *testing.T) {
	tests := []struct {
		name  string
		entry catalog.Entry
		query string
	}{
		{
			name:  "region",
			entry: catalog.Entry{Name: "alpha", DisplayName: "Alpha", Description: "First", Category: "maps", Regions: []string{"NL"}},
			query: "nl",
		},
		{
			name:  "api language",
			entry: catalog.Entry{Name: "beta", DisplayName: "Beta", Description: "Second", Category: "maps", APILanguage: "da-DK"},
			query: "da-dk",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, matchesCatalogQuery(tt.entry, tt.query))
		})
	}
}

func TestFilterCatalogEntriesByRegion(t *testing.T) {
	entries := []catalog.Entry{
		{Name: "global", Regions: []string{"*"}},
		{Name: "netherlands", Regions: []string{"NL"}},
		{Name: "india", Regions: []string{"IN"}},
		{Name: "unknown"},
	}

	got := filterCatalogEntriesByRegion(entries, "nl")

	require.Len(t, got, 2)
	assert.Equal(t, "global", got[0].Name)
	assert.Equal(t, "netherlands", got[1].Name)
}

func TestCatalogListRejectsInvalidRegionFilter(t *testing.T) {
	cmd := newCatalogCmd()
	cmd.SetArgs([]string{"list", "--region", "netherlands"})

	_, err := runWithCapturedStdout(t, cmd.Execute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--region must be a two-letter region token")
}

func TestCatalogSearchNoMatches(t *testing.T) {
	cmd := newCatalogCmd()
	cmd.SetArgs([]string{"search", "zzz-nonexistent-query-xyz", "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var matches []catalog.Entry
	require.NoError(t, json.Unmarshal([]byte(output), &matches))
	assert.Empty(t, matches, "search for nonsense query should return no results")
}

func TestVersionJSON(t *testing.T) {
	cmd := newVersionCmd()
	cmd.SetArgs([]string{"--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var result map[string]string
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.NotEmpty(t, result["version"], "version key should be present and non-empty")
	assert.NotEmpty(t, result["go"], "go key should be present and non-empty")
}

func TestCatalogListPlainText(t *testing.T) {
	cmd := newCatalogCmd()
	cmd.SetArgs([]string{"list"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	// Plain-text output groups entries by category with headers
	assert.Contains(t, output, "payments:")
	assert.Contains(t, output, "stripe")
}

func TestCatalogShowStripePlainText(t *testing.T) {
	cmd := newCatalogCmd()
	cmd.SetArgs([]string{"show", "stripe"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	assert.Contains(t, output, "Stripe")
	assert.Contains(t, output, "Spec URL:")
}

func TestCatalogShowPlainTextIncludesRegionMetadata(t *testing.T) {
	output := captureStdout(t, func() {
		entry := catalog.Entry{
			Name:        "pdok-location",
			DisplayName: "PDOK Location",
			Description: "Geocoding for Dutch addresses",
			Category:    "maps",
			Tier:        "official",
			SpecURL:     "https://example.com/openapi.yaml",
			SpecFormat:  "yaml",
			Regions:     []string{"NL"},
			APILanguage: "nl",
		}
		printCatalogEntryPlainText(entry)
	})

	assert.Contains(t, output, "Regions:        NL")
	assert.Contains(t, output, "API Language:   nl")
}
