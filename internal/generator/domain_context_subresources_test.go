package generator

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// summaryByName finds a ResourceSummary by its (possibly dotted) name.
func summaryByName(t *testing.T, ctx DomainContext, name string) ResourceSummary {
	t.Helper()
	for _, rs := range ctx.Resources {
		if rs.Name == name {
			return rs
		}
	}
	require.Failf(t, "resource summary not found", "no entry named %q in %+v", name, ctx.Resources)
	return ResourceSummary{}
}

// TestBuildDomainContext_SubResourcesSurfacedAsOwnEntries verifies that a
// resource carrying SubResources (including nested-within-nested) emits one
// dotted-name taxonomy entry per sub-resource, with the sub-resource's own
// endpoints and an honest writable flag, while top-level entries stay intact.
func TestBuildDomainContext_SubResourcesSurfacedAsOwnEntries(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("plane")
	apiSpec.Resources = map[string]spec.Resource{
		// Workspace-level "issues": genuinely read-only (get/search only).
		"issues": {
			Description: "Search work items",
			Endpoints: map[string]spec.Endpoint{
				"get-workspace-work-item": {Method: "GET", Path: "/issues/{id}"},
				"search-work-items":       {Method: "GET", Path: "/issues/search"},
			},
		},
		// Top-level "projects" with full CRUD, and a project-scoped "issues"
		// sub-resource holding the work-item CRUD, itself nesting "comments".
		"projects": {
			Description: "Manage projects",
			Endpoints: map[string]spec.Endpoint{
				"list":     {Method: "GET", Path: "/projects"},
				"retrieve": {Method: "GET", Path: "/projects/{id}"},
				"create":   {Method: "POST", Path: "/projects"},
				"update":   {Method: "PATCH", Path: "/projects/{id}"},
				"delete":   {Method: "DELETE", Path: "/projects/{id}"},
			},
			SubResources: map[string]spec.Resource{
				"issues": {
					Description: "Manage work items",
					Endpoints: map[string]spec.Endpoint{
						"list-work-items":  {Method: "GET", Path: "/projects/{pid}/issues"},
						"create-work-item": {Method: "POST", Path: "/projects/{pid}/issues"},
						"update-work-item": {Method: "PATCH", Path: "/projects/{pid}/issues/{id}"},
						"delete-work-item": {Method: "DELETE", Path: "/projects/{pid}/issues/{id}"},
					},
					SubResources: map[string]spec.Resource{
						// Nested-within-nested: read-only comments.
						"comments": {
							Description: "List work-item comments",
							Endpoints: map[string]spec.Endpoint{
								"list-comments": {Method: "GET", Path: "/projects/{pid}/issues/{id}/comments"},
							},
						},
					},
				},
			},
		},
	}

	g := &Generator{
		Spec: apiSpec,
		profile: &profiler.APIProfile{
			SyncableResources: []profiler.SyncableResource{{Name: "projects", Path: "/projects"}},
			SearchableFields:  map[string][]string{"issues": {"name"}},
		},
	}

	ctx := g.buildDomainContext()

	// Sub-resource surfaced as its own dotted entry with its own endpoints.
	pi := summaryByName(t, ctx, "projects.issues")
	assert.Equal(t, []string{"create-work-item", "delete-work-item", "list-work-items", "update-work-item"}, pi.Endpoints)
	assert.True(t, pi.Writable, "projects.issues has POST/PATCH/DELETE -> writable")

	// Nested-within-nested surfaced too, read-only -> not writable.
	pic := summaryByName(t, ctx, "projects.issues.comments")
	assert.Equal(t, []string{"list-comments"}, pic.Endpoints)
	assert.False(t, pic.Writable, "comments are GET-only -> not writable")

	// Top-level entries unchanged: names bare, endpoints identical, and the
	// workspace-level read-only "issues" stays honestly non-writable.
	proj := summaryByName(t, ctx, "projects")
	assert.Equal(t, []string{"create", "delete", "list", "retrieve", "update"}, proj.Endpoints)
	assert.True(t, proj.Writable)
	assert.True(t, proj.Syncable)

	wi := summaryByName(t, ctx, "issues")
	assert.Equal(t, []string{"get-workspace-work-item", "search-work-items"}, wi.Endpoints)
	assert.False(t, wi.Writable, "workspace issues are GET-only -> not writable")
	assert.True(t, wi.Searchable)

	// syncable/searchable are omit-over-guess for sub-entries (profiler keys by
	// bare top-level name; a dotted name is never a key).
	assert.False(t, pi.Syncable)
	assert.False(t, pi.Searchable)

	// Separator sanity: no resource/sub-resource key in this fixture contains
	// the '.' separator, so dotted names are unambiguous.
	for _, rs := range ctx.Resources {
		if rs.Name != "projects.issues" && rs.Name != "projects.issues.comments" {
			assert.NotContains(t, rs.Name, ".", "unexpected dot in a non-qualified name %q", rs.Name)
		}
	}
}

// TestBuildDomainContext_NoSubResourcesIsFlat verifies a spec with no
// sub-resources produces exactly the top-level entries, unchanged — the
// byte-identical guarantee for sub-resource-free CLIs.
func TestBuildDomainContext_NoSubResourcesIsFlat(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hn") // minimalSpec has one resource "items" with a "list" GET
	g := &Generator{Spec: apiSpec, profile: &profiler.APIProfile{}}

	ctx := g.buildDomainContext()

	require.Len(t, ctx.Resources, 1)
	assert.Equal(t, "items", ctx.Resources[0].Name)
	assert.Equal(t, []string{"list"}, ctx.Resources[0].Endpoints)
	assert.False(t, ctx.Resources[0].Writable, "GET-only resource is not writable")
}

// TestBuildDomainContext_ReadOnlyParentWritableChild verifies the inverse of the
// nested-comments case that resourceHasMutation's "and vice versa" comment claims:
// a GET-only PARENT carrying a mutating CHILD keeps the parent read-only while the
// child is writable — writability never flows upward from a sub-resource. It also
// exercises the PUT arm of the mutation switch, which the main fixture does not.
func TestBuildDomainContext_ReadOnlyParentWritableChild(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("plane")
	apiSpec.Resources = map[string]spec.Resource{
		// Read-only parent: get/list only, no mutating endpoint of its own.
		"catalog": {
			Description: "Browse the catalog",
			Endpoints: map[string]spec.Endpoint{
				"list":     {Method: "GET", Path: "/catalog"},
				"retrieve": {Method: "GET", Path: "/catalog/{id}"},
			},
			SubResources: map[string]spec.Resource{
				// Mutating child, reached via a PUT (upsert-style).
				"settings": {
					Description: "Manage catalog settings",
					Endpoints: map[string]spec.Endpoint{
						"replace-settings": {Method: "PUT", Path: "/catalog/{id}/settings"},
					},
				},
			},
		},
	}

	g := &Generator{Spec: apiSpec, profile: &profiler.APIProfile{}}
	ctx := g.buildDomainContext()

	parent := summaryByName(t, ctx, "catalog")
	assert.Equal(t, []string{"list", "retrieve"}, parent.Endpoints)
	assert.False(t, parent.Writable, "GET-only parent must not inherit its child's writability")

	child := summaryByName(t, ctx, "catalog.settings")
	assert.Equal(t, []string{"replace-settings"}, child.Endpoints)
	assert.True(t, child.Writable, "child with a PUT endpoint is writable")
}
