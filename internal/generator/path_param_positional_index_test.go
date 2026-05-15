package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPathParamArgsIndexUsesPositionalOrdinal exercises the codegen template's
// args[] substitution for positional path params. The bug it pins: when a
// non-positional query param sits before the positional path param in
// Endpoint.Params, the template used to interpolate the full-Params index into
// args[] (and into the len-check), making the generated command fail at
// runtime with "<name> is required" regardless of what positional the user
// passes. Fixed by indexing into args via the Positional-only ordinal.
func TestPathParamArgsIndexUsesPositionalOrdinal(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "argidx",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/argidx-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"tags": {
				Description: "Tags",
				SubResources: map[string]spec.Resource{
					"contacts": {
						Description: "Contacts under a tag",
						Endpoints: map[string]spec.Endpoint{
							// list-for-tag: declares limit + offset (query) BEFORE the
							// positional path param tagId. Without the fix, the template
							// emitted args[2] (the full-Params index) instead of args[0]
							// (the Positional ordinal).
							"list-for-tag": {
								Method:      "GET",
								Path:        "/v1/tags/{tagId}/contacts",
								Description: "List contacts for a tag",
								Params: []spec.Param{
									{Name: "limit", Type: "int"},
									{Name: "offset", Type: "int"},
									{Name: "tagId", Type: "string", Required: true, Positional: true},
								},
							},
						},
					},
				},
			},
			"users": {
				Description: "Users",
				SubResources: map[string]spec.Resource{
					"course-progress": {
						Description: "Course progress",
						Endpoints: map[string]spec.Endpoint{
							// Two positional path params with a non-positional query
							// interleaved between them. The Use line is `<id> <course>`
							// (positional ordinals 0 and 1) even though Endpoint.Params
							// has the query at index 1.
							"steps": {
								Method:      "GET",
								Path:        "/users/{id}/course-progress/{course}/steps",
								Description: "List steps for a course",
								Params: []spec.Param{
									{Name: "id", Type: "string", Required: true, Positional: true},
									{Name: "fmt", Type: "string"},
									{Name: "course", Type: "string", Required: true, Positional: true},
								},
							},
						},
					},
				},
			},
			"products": {
				Description: "Products",
				SubResources: map[string]spec.Resource{
					"subscriptions": {
						Description: "Subscriptions",
						Endpoints: map[string]spec.Endpoint{
							// Currently-working baseline: two positional path params in
							// URL-matching declaration order, no non-positional params
							// before them. This case used to produce correct args[0]/
							// args[1] and must continue to.
							"list": {
								Method:      "GET",
								Path:        "/v1/products/{productId}/subscriptions/{subscriptionId}",
								Description: "List subscriptions for a product",
								Params: []spec.Param{
									{Name: "productId", Type: "string", Required: true, Positional: true},
									{Name: "subscriptionId", Type: "string", Required: true, Positional: true},
								},
							},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	read := func(rel string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(outputDir, rel))
		require.NoErrorf(t, err, "read %s", rel)
		return string(data)
	}

	// 1) Single positional with a non-positional query before it: args[0],
	// no len-check (Cobra's MinimumNArgs(1) covers the first positional).
	tagsList := read(filepath.Join("internal", "cli", "tags_contacts_list-for-tag.go"))
	assert.Contains(t, tagsList,
		`path = replacePathParam(path, "tagId", args[0])`,
		"single positional path param must use args[0] regardless of declared query params before it")
	assert.NotContains(t, tagsList,
		`args[2]`,
		"must not interpolate the full-Params index for the positional")
	assert.NotContains(t, tagsList,
		`if len(args) < 3`,
		"must not emit a stale len-check derived from the full-Params index")

	// 2) Two positional path params with a non-positional query interleaved:
	// args[0]/args[1] in declaration order. The second positional gets a
	// len-check at len(args) < 2 (its ordinal+1), not the full-Params index.
	steps := read(filepath.Join("internal", "cli", "users_course-progress_steps.go"))
	assert.Contains(t, steps,
		`path = replacePathParam(path, "id", args[0])`,
		"first positional must resolve to args[0]")
	assert.Contains(t, steps,
		`path = replacePathParam(path, "course", args[1])`,
		"second positional must resolve to args[1] even with a non-positional param interleaved")
	assert.Contains(t, steps,
		`if len(args) < 2`,
		"len-check for the second positional must use its ordinal+1")
	assert.NotContains(t, steps,
		`args[3]`,
		"must not interpolate the full-Params index (3) for the second positional")
	assert.NotContains(t, steps,
		`if len(args) < 4`,
		"must not emit a stale len-check derived from the full-Params index")

	// 3) Regression baseline: pure positional declaration order matches URL
	// order, no interleaved queries. Output unchanged from pre-fix behavior.
	subs := read(filepath.Join("internal", "cli", "products_subscriptions_list.go"))
	assert.Contains(t, subs,
		`path = replacePathParam(path, "productId", args[0])`,
		"baseline: first positional resolves to args[0]")
	assert.Contains(t, subs,
		`path = replacePathParam(path, "subscriptionId", args[1])`,
		"baseline: second positional resolves to args[1]")
	assert.Contains(t, subs,
		`if len(args) < 2`,
		"baseline: len-check for second positional unchanged")
}

// TestPromotedCommandPathParamArgsIndexUsesPositionalOrdinal covers the
// promoted-command template (command_promoted.go.tmpl), which has the same
// args[] indexing shape as the endpoint template but emits a different file
// and emits the len-check unconditionally (no MinimumNArgs cover for the
// first positional). The bug shape is identical: a single positional path
// param with a non-positional query declared before it must resolve to
// args[0] and len(args) < 1, not the full-Params index.
func TestPromotedCommandPathParamArgsIndexUsesPositionalOrdinal(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "promoarg",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/promoarg-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			// Single-endpoint resource — gets promoted to a top-level command.
			// "limit" precedes "itemId" in Params so the full-Params index for
			// itemId is 1, but its positional ordinal is 0.
			"items": {
				Description: "Items under a parent",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/v1/items/{itemId}/children",
						Description: "List children of an item",
						Params: []spec.Param{
							{Name: "limit", Type: "int"},
							{Name: "itemId", Type: "string", Required: true, Positional: true},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	matches, err := filepath.Glob(filepath.Join(outputDir, "internal", "cli", "promoted_*.go"))
	require.NoError(t, err)
	require.Lenf(t, matches, 1, "expected exactly one promoted_*.go file, got %v", matches)
	data, err := os.ReadFile(matches[0])
	require.NoError(t, err)
	src := string(data)

	assert.Contains(t, src,
		`path = replacePathParam(path, "itemId", args[0])`,
		"promoted command: single positional path param must use args[0] regardless of query params declared before it")
	assert.Contains(t, src,
		`if len(args) < 1`,
		"promoted command: len-check for the sole positional must derive from its ordinal+1, not the full-Params index")
	assert.NotContains(t, src,
		`args[1]`,
		"promoted command: must not interpolate the full-Params index (1) for the positional")
	assert.NotContains(t, src,
		`if len(args) < 2`,
		"promoted command: must not emit a stale len-check derived from the full-Params index")
}

// TestPathParamArgsIndexFlagExposedPathParam pins the variant of the
// indexing bug reported in #1308: when one path param is flag-exposed
// (PathParam=true, Positional=false) and another is positional, the
// positional must still resolve to its Positional-only ordinal — not
// the full-Params index inflated by the flag-exposed predecessor. The
// shape is the dominant pattern in REST APIs with scoping containers
// (`/groups/{groupId}/reports/{reportId}`, where groupId is provided
// via `--group-id` and reportId is positional).
//
// Distinct from TestPathParamArgsIndexUsesPositionalOrdinal: that test
// pins non-path (query/header) params preceding a positional path
// param; this one pins a *path* param preceding it, which routes
// through a different template branch in the path-substitution loop.
func TestPathParamArgsIndexFlagExposedPathParam(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "flagpath",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/flagpath-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"reports": {
				Description: "Reports under a workspace group",
				Endpoints: map[string]spec.Endpoint{
					// Two-endpoint resource avoids the single-endpoint promotion
					// path so this exercises command_endpoint.go.tmpl. groupId
					// is flag-exposed (PathParam=true, Positional=false) and
					// declared first; reportId is the sole positional and
					// declared second.
					"get-pages-in-group": {
						Method:      "GET",
						Path:        "/v1/groups/{groupId}/reports/{reportId}/pages",
						Description: "List pages of a report in a group",
						Params: []spec.Param{
							{Name: "groupId", Type: "string", Required: true, PathParam: true},
							{Name: "reportId", Type: "string", Required: true, Positional: true},
						},
					},
					"list": {
						Method:      "GET",
						Path:        "/v1/reports",
						Description: "List reports",
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "reports_get-pages-in-group.go"))
	require.NoError(t, err)
	body := string(src)

	assert.Contains(t, body,
		`path = replacePathParam(path, "reportId", args[0])`,
		"positional path param must resolve to args[0] when a flag-exposed path param precedes it in Params")
	assert.Contains(t, body,
		`path = replacePathParam(path, "groupId", fmt.Sprintf("%v", flagGroupId))`,
		"flag-exposed path param must continue to substitute from its flag, not args[]")
	assert.NotContains(t, body,
		`args[1]`,
		"must not interpolate the full-Params index (1) for the sole positional")
	assert.NotContains(t, body,
		`if len(args) < 2`,
		"must not emit a stale len-check derived from the full-Params index")
}

// TestPromotedCommandPathParamArgsIndexFlagExposedPathParam is the
// promoted-command counterpart of TestPathParamArgsIndexFlagExposedPathParam.
// The promoted template emits the len-check unconditionally and has the
// same Params iteration shape, so the same args[] indexing contract
// must hold.
func TestPromotedCommandPathParamArgsIndexFlagExposedPathParam(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "promoflagpath",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/promoflagpath-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			// Single-endpoint resource — promotes to a top-level command.
			"reports": {
				Description: "Reports under a workspace group",
				Endpoints: map[string]spec.Endpoint{
					"get-pages-in-group": {
						Method:      "GET",
						Path:        "/v1/groups/{groupId}/reports/{reportId}/pages",
						Description: "List pages of a report in a group",
						Params: []spec.Param{
							{Name: "groupId", Type: "string", Required: true, PathParam: true},
							{Name: "reportId", Type: "string", Required: true, Positional: true},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	matches, err := filepath.Glob(filepath.Join(outputDir, "internal", "cli", "promoted_*.go"))
	require.NoError(t, err)
	require.Lenf(t, matches, 1, "expected exactly one promoted_*.go file, got %v", matches)
	data, err := os.ReadFile(matches[0])
	require.NoError(t, err)
	src := string(data)

	assert.Contains(t, src,
		`path = replacePathParam(path, "reportId", args[0])`,
		"promoted command: positional path param must resolve to args[0] when a flag-exposed path param precedes it")
	assert.Contains(t, src,
		`path = replacePathParam(path, "groupId", fmt.Sprintf("%v", flagGroupId))`,
		"promoted command: flag-exposed path param must continue to substitute from its flag")
	assert.Contains(t, src,
		`if len(args) < 1`,
		"promoted command: len-check for the sole positional must derive from its ordinal+1")
	assert.NotContains(t, src,
		`args[1]`,
		"promoted command: must not interpolate the full-Params index (1) for the sole positional")
	assert.NotContains(t, src,
		`if len(args) < 2`,
		"promoted command: must not emit a stale len-check derived from the full-Params index")
}

func TestPositionalIndexHelper(t *testing.T) {
	t.Parallel()

	e := spec.Endpoint{
		Params: []spec.Param{
			{Name: "limit"},
			{Name: "offset"},
			{Name: "tagId", Positional: true},
		},
	}
	assert.Equal(t, 0, positionalIndex(e, "tagId"),
		"sole positional should resolve to ordinal 0 even when queries precede it")
	assert.Equal(t, -1, positionalIndex(e, "limit"),
		"non-positional param has no positional ordinal")

	multi := spec.Endpoint{
		Params: []spec.Param{
			{Name: "id", Positional: true},
			{Name: "fmt"},
			{Name: "course", Positional: true},
		},
	}
	assert.Equal(t, 0, positionalIndex(multi, "id"))
	assert.Equal(t, 1, positionalIndex(multi, "course"))
	assert.Equal(t, -1, positionalIndex(multi, "missing"),
		"unknown name should return -1, not panic")
}
