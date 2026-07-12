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

func TestDiscoverSpec_UnknownAPI(t *testing.T) {
	url, source, err := DiscoverSpec("zzz-nonexistent-api-zzz")
	require.Error(t, err)
	assert.Empty(t, url)
	assert.Empty(t, source)
	assert.Contains(t, err.Error(), "try providing a URL with --spec")
}

func TestKnownSpecsRegistry_AllURLsHTTPS(t *testing.T) {
	for name, ks := range KnownSpecs {
		assert.True(t, strings.HasPrefix(ks.URL, "https://"),
			"KnownSpecs[%q].URL should start with https://, got %q", name, ks.URL)
	}
}
