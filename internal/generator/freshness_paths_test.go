package generator

import (
	"sort"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
)

// TestFreshnessCommandPaths_PromotedResourceMirrorsRuntimeMap verifies that
// rendered freshness docs use the same command-path coverage as the generated
// auto-refresh hook, including the fallback list/get/search paths.
func TestFreshnessCommandPaths_PromotedResourceMirrorsRuntimeMap(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hn")
	apiSpec.Cache.Enabled = true
	// `ask` has a single endpoint named `list`; the generator promotes it to
	// the resource-level command, but the runtime map still accepts the
	// fallback resource paths.
	apiSpec.Resources["ask"] = spec.Resource{
		Description: "Ask HN",
		Endpoints: map[string]spec.Endpoint{
			"list": {Method: "GET", Path: "/askstories.json"},
		},
	}

	g := &Generator{
		Spec: apiSpec,
		profile: &profiler.APIProfile{
			SyncableResources: []profiler.SyncableResource{
				{Name: "ask", Path: "/askstories.json"},
			},
		},
		PromotedResourceNames: map[string]bool{"ask": true},
		PromotedEndpointNames: map[string]string{"ask": "list"},
	}

	got := g.freshnessCommandPaths()
	assert.Equal(t, []string{
		"hn-pp-cli ask",
		"hn-pp-cli ask get",
		"hn-pp-cli ask list",
		"hn-pp-cli ask search",
	}, got, "docs should mirror auto_refresh.go.tmpl command coverage")
}

// TestFreshnessCommandPaths_MultiEndpointResourceMirrorsRuntimeMap verifies
// that docs stay aligned with auto_refresh.go.tmpl instead of independently
// deriving endpoint names from the spec.
func TestFreshnessCommandPaths_MultiEndpointResourceMirrorsRuntimeMap(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hn")
	apiSpec.Cache.Enabled = true
	// The freshness coverage should not drift to endpoint-specific names such
	// as top/new/best; the generated hook accepts the fixed resource paths.
	apiSpec.Resources["stories"] = spec.Resource{
		Description: "Stories",
		Endpoints: map[string]spec.Endpoint{
			"top":  {Method: "GET", Path: "/topstories.json"},
			"new":  {Method: "GET", Path: "/newstories.json"},
			"best": {Method: "GET", Path: "/beststories.json"},
			"get":  {Method: "GET", Path: "/item/{itemId}.json"},
		},
	}

	g := &Generator{
		Spec: apiSpec,
		profile: &profiler.APIProfile{
			SyncableResources: []profiler.SyncableResource{
				{Name: "stories", Path: "/topstories.json"},
			},
		},
		PromotedResourceNames: map[string]bool{},
	}

	got := g.freshnessCommandPaths()
	sort.Strings(got)

	want := []string{
		"hn-pp-cli stories",
		"hn-pp-cli stories get",
		"hn-pp-cli stories list",
		"hn-pp-cli stories search",
	}
	assert.Equal(t, want, got, "should emit the runtime command coverage")

	for _, endpointPath := range []string{
		"hn-pp-cli stories best",
		"hn-pp-cli stories new",
		"hn-pp-cli stories top",
	} {
		assert.NotContains(t, got, endpointPath,
			"endpoint path %q must not appear unless auto_refresh.go.tmpl maps it", endpointPath)
	}
}

// TestFreshnessCommandPaths_CacheCommandsAdded verifies that explicit
// custom command paths declared in spec.Cache.Commands are still emitted
// alongside the syncable-resource paths.
func TestFreshnessCommandPaths_CacheCommandsAdded(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hn")
	apiSpec.Cache.Enabled = true
	apiSpec.Cache.Commands = []spec.CacheCommand{
		{Name: "controversial", Resources: []string{"stories"}},
	}
	apiSpec.Resources["stories"] = spec.Resource{
		Description: "Stories",
		Endpoints: map[string]spec.Endpoint{
			"top": {Method: "GET", Path: "/topstories.json"},
		},
	}

	g := &Generator{
		Spec: apiSpec,
		profile: &profiler.APIProfile{
			SyncableResources: []profiler.SyncableResource{
				{Name: "stories", Path: "/topstories.json"},
			},
		},
		PromotedResourceNames: map[string]bool{"stories": true},
		PromotedEndpointNames: map[string]string{"stories": "top"},
	}

	got := g.freshnessCommandPaths()
	sort.Strings(got)

	assert.Contains(t, got, "hn-pp-cli stories",
		"bare resource path should be present")
	assert.Contains(t, got, "hn-pp-cli controversial",
		"explicit Cache.Commands entry should be emitted")
}

// TestFreshnessCommandPaths_DisabledReturnsNil verifies the early return
// when cache is disabled.
func TestFreshnessCommandPaths_DisabledReturnsNil(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hn")
	apiSpec.Cache.Enabled = false

	g := &Generator{Spec: apiSpec, profile: &profiler.APIProfile{}}
	assert.Nil(t, g.freshnessCommandPaths(),
		"should return nil when cache is disabled")
}

// TestFreshnessCommandPaths_NoProfileReturnsNil verifies the early return
// when no profile has been computed (defensive).
func TestFreshnessCommandPaths_NoProfileReturnsNil(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hn")
	apiSpec.Cache.Enabled = true

	g := &Generator{Spec: apiSpec, profile: nil}
	assert.Nil(t, g.freshnessCommandPaths(),
		"should return nil when profile is missing")
}
