package pipeline

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverSpec_KnownAPI(t *testing.T) {
	url, _, err := DiscoverSpec("petstore")
	require.NoError(t, err)
	assert.Contains(t, url, "petstore")
}

func TestDiscoverSpec_CatalogEntryWins(t *testing.T) {
	url, source, err := DiscoverSpec("google-cloud-run")
	require.NoError(t, err)
	assert.Equal(t, "https://api.apis.guru/v2/specs/googleapis.com/run/v2/openapi.yaml", url)
	assert.Equal(t, "catalog entry (community)", source)
}

func TestDiscoverSpec_UnknownAPI(t *testing.T) {
	// Unknown APIs still return a url via apis-guru fallback, so check that it works
	url, source, err := DiscoverSpec("zzz-nonexistent-api-zzz")
	require.NoError(t, err)
	assert.Contains(t, source, "unverified")
	assert.Contains(t, url, "zzz-nonexistent-api-zzz")
}

func TestKnownSpecsRegistry_AllURLsHTTPS(t *testing.T) {
	for name, ks := range KnownSpecs {
		assert.True(t, strings.HasPrefix(ks.URL, "https://"),
			"KnownSpecs[%q].URL should start with https://, got %q", name, ks.URL)
	}
}

func TestApisGuruPattern(t *testing.T) {
	result := ApisGuruPattern("stripe.com", "v1")
	assert.Contains(t, result, "APIs-guru")
	assert.Contains(t, result, "stripe.com")
}
