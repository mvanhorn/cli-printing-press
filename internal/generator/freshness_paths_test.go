package generator

import (
	"sort"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
)

// TestFreshnessCommandPaths_PromotedResourceEmitsBareOnly verifies that a
// resource with a single endpoint (which the generator promotes to the
// resource-level command) emits only the bare `<cli> <resource>` form in
// the rendered freshness paths. Phantom variants like `<cli> <resource> list`
// must NOT appear because the user can't invoke them — Cobra doesn't
// register `list` as a subcommand for promoted resources.
//
// Regression guard for retro #350 finding F1: prior behavior emitted
// `<resource> list / get / search` for every syncable resource regardless
// of whether those subcommands actually exist.
func TestFreshnessCommandPaths_PromotedResourceEmitsBareOnly(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hn")
	apiSpec.Cache.Enabled = true
	// `ask` has a single endpoint named `list` — the generator will promote
	// it. Freshness paths should emit only the bare `hn-pp-cli ask`.
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
	assert.Equal(t, []string{"hn-pp-cli ask"}, got,
		"promoted single-endpoint resource should emit only the bare path")
}

// TestFreshnessCommandPaths_MultiEndpointResourceEmitsRealEndpointsOnly
// verifies that a multi-endpoint resource emits the bare path plus one
// entry per real endpoint name — and nothing else. The hardcoded
// `list / get / search` variants from the prior implementation must NOT
// appear unless those endpoint names actually exist on the resource.
func TestFreshnessCommandPaths_MultiEndpointResourceEmitsRealEndpointsOnly(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hn")
	apiSpec.Cache.Enabled = true
	// `stories` has 4 endpoints: top, new, best, get. None of them are
	// `list` or `search`. The freshness paths must reflect that exactly.
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
		"hn-pp-cli stories best",
		"hn-pp-cli stories get",
		"hn-pp-cli stories new",
		"hn-pp-cli stories top",
	}
	assert.Equal(t, want, got, "should emit only real endpoint names")

	// Negative assertions: the prior phantom variants must NOT be present.
	for _, phantom := range []string{
		"hn-pp-cli stories list",
		"hn-pp-cli stories search",
	} {
		assert.NotContains(t, got, phantom,
			"phantom path %q must not appear — endpoint does not exist on the resource", phantom)
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
