// Copyright 2026 Anthropic, PBC. Licensed under Apache-2.0.

package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateSyncParamPassthrough verifies the sync template emits the
// --param / --resource-param / --global-param flags, parses them through
// parseSyncUserParams, and applies them after spec-derived params. Some
// APIs mark filter params optional in the spec but reject requests
// without them at runtime; without this passthrough, the only workaround
// is hand-editing the generated client.
func TestGenerateSyncParamPassthrough(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("syncparam")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncSrc := string(syncGo)

	// Flag declarations exist with the expected help text shape (operator
	// has to know what to type without reading the source).
	assert.Contains(t, syncSrc, `cmd.Flags().StringArrayVar(&paramFlags, "param"`,
		"sync should expose a repeatable --param flag")
	assert.Contains(t, syncSrc, `cmd.Flags().StringArrayVar(&resourceParamFlags, "resource-param"`,
		"sync should expose a repeatable --resource-param flag")
	assert.Contains(t, syncSrc, `cmd.Flags().StringArrayVar(&globalParamFlags, "global-param"`,
		"sync should expose a repeatable --global-param flag for the apply-everywhere semantic")
	assert.Contains(t, syncSrc, "key=value",
		"--param help text should describe the key=value shape")
	assert.Contains(t, syncSrc, "resource:key=value",
		"--resource-param help text should describe the resource:key=value shape")

	// Parsing runs before client construction so a malformed flag fails fast
	// (and as usageErr, not a generic runtime error).
	assert.Contains(t, syncSrc, "parseSyncUserParams(paramFlags, resourceParamFlags, globalParamFlags)",
		"sync must parse user params at RunE entry with all three flag slices")
	parseIdx := strings.Index(syncSrc, "parseSyncUserParams(paramFlags, resourceParamFlags, globalParamFlags)")
	newClientIdx := strings.Index(syncSrc, "flags.newClient()")
	require.NotEqual(t, -1, parseIdx)
	require.NotEqual(t, -1, newClientIdx)
	assert.Less(t, parseIdx, newClientIdx,
		"--param must parse before newClient so usage errors don't waste an HTTP handshake")

	// userParams flows through to the syncResource worker. The exact call
	// site differs by template branch (HasTierRouting vs not), so assert the
	// event-writer arg follows userParams.
	assert.Contains(t, syncSrc, ", userParams, syncEventWriter)",
		"syncResource must receive userParams before the event writer")

	// applyTo is called in the page loop AFTER cursor/since/limit are set,
	// so user flags win on conflict.
	loopIdx := strings.Index(syncSrc, "params[pageSize.limitParam] = strconv.Itoa(pageSize.limit)")
	applyIdx := strings.Index(syncSrc, "userParams.applyTo(resource, params, false)")
	require.NotEqual(t, -1, loopIdx, "page loop should set the limit param")
	require.NotEqual(t, -1, applyIdx, "syncResource should apply user params before c.Get")
	assert.Less(t, loopIdx, applyIdx,
		"user params must apply after spec-derived params so flags can override")

	// --resource-param keys must be validated against known resources before
	// sync starts, so a typo errors instead of silently no-op'ing.
	assert.Contains(t, syncSrc, "userParams.validateResourceNames(knownSyncResourceNames())",
		"sync should validate --resource-param keys against the known resource set")
	assert.Contains(t, syncSrc, "func knownSyncResourceNames() []string",
		"knownSyncResourceNames helper must be emitted alongside defaultSyncResources")
}

func TestGenerateSyncConcurrencyDefaultHonorsRateClass(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		rateClass string
	}{
		{name: "absent"},
		{name: "per-second", rateClass: spec.RateClassPerSecond},
		{name: "unlimited", rateClass: spec.RateClassUnlimited},
		{name: "daily", rateClass: spec.RateClassDaily},
		{name: "monthly", rateClass: spec.RateClassMonthly},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec("sync-" + tc.name)
			apiSpec.RateClass = tc.rateClass
			outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
			require.NoError(t, New(apiSpec, outputDir).Generate())

			syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
			require.NoError(t, err)
			syncSrc := string(syncGo)

			wantDefault := apiSpec.SyncDefaultConcurrency()
			assertSyncDefaultConcurrency(t, syncSrc, wantDefault, tc.name)
		})
	}
}

func TestGenerateSyncDefaultsSkipAuthTaggedResources(t *testing.T) {
	t.Parallel()

	paginated := func(path string, tags ...string) spec.Endpoint {
		return spec.Endpoint{
			Method:     "GET",
			Path:       path,
			Tags:       tags,
			Response:   spec.ResponseDef{Type: "array"},
			Pagination: &spec.Pagination{Type: "cursor", LimitParam: "limit", CursorParam: "after"},
		}
	}
	apiSpec := minimalSpec("sync-auth")
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Items",
			Endpoints:   map[string]spec.Endpoint{"list": paginated("/items")},
		},
		"oauth-token": {
			Description: "OAuth token endpoint",
			Endpoints:   map[string]spec.Endpoint{"list": paginated("/oauth_token", "OAuth")},
		},
		"oauth2-token": {
			Description: "OAuth2 token endpoint",
			Endpoints:   map[string]spec.Endpoint{"list": paginated("/oauth2_token", "OAuth2")},
		},
		"authorization-grant": {
			Description: "Authorization grant endpoint",
			Endpoints:   map[string]spec.Endpoint{"list": paginated("/authorization_grant", "Billing", "Authorization")},
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	defaultsStart := strings.Index(syncSrc, "func defaultSyncResources() []string")
	knownStart := strings.Index(syncSrc, "func knownSyncResourceNames() []string")
	require.NotEqual(t, -1, defaultsStart)
	require.NotEqual(t, -1, knownStart)
	defaultsBlock := syncSrc[defaultsStart:knownStart]
	assert.Contains(t, defaultsBlock, `"items"`)
	assert.NotContains(t, defaultsBlock, `"oauth-token"`)
	assert.NotContains(t, defaultsBlock, `"oauth2-token"`)
	assert.NotContains(t, defaultsBlock, `"authorization-grant"`)

	knownBlock := syncSrc[knownStart:]
	assert.Contains(t, knownBlock, `"items"`)
	assert.Contains(t, knownBlock, `"oauth-token"`, "explicit --resources should still accept auth-tagged sync endpoints")
	assert.Contains(t, knownBlock, `"oauth2-token"`, "explicit --resources should still accept auth-tagged sync endpoints")
	assert.Contains(t, knownBlock, `"authorization-grant"`, "explicit --resources should still accept auth-tagged sync endpoints")

	workflowSrc := readGeneratedFile(t, outputDir, "internal", "cli", "channel_workflow.go")
	archiveIdx := strings.Index(workflowSrc, "resources := []string")
	require.NotEqual(t, -1, archiveIdx)
	archiveBlock := workflowSrc[archiveIdx:]
	assert.Contains(t, archiveBlock, `"items"`)
	assert.NotContains(t, archiveBlock, `"oauth-token"`)
	assert.NotContains(t, archiveBlock, `"oauth2-token"`)
	assert.NotContains(t, archiveBlock, `"authorization-grant"`)
}

func TestGenerateSyncDefaultsSkipTypedIDlessResources(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("sync-idless")
	apiSpec.Types = map[string]spec.TypeDef{
		"Technology": {
			Fields: []spec.TypeField{
				{Name: "title", Type: "string"},
				{Name: "url", Type: "string"},
			},
		},
	}
	apiSpec.Resources = map[string]spec.Resource{
		"technologies": {
			Description: "Technologies",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/technologies",
					Response: spec.ResponseDef{Type: "array", Item: "Technology"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	defaultsStart := strings.Index(syncSrc, "func defaultSyncResources() []string")
	knownStart := strings.Index(syncSrc, "func knownSyncResourceNames() []string")
	require.NotEqual(t, -1, defaultsStart)
	require.NotEqual(t, -1, knownStart)
	defaultsBlock := syncSrc[defaultsStart:knownStart]
	assert.NotContains(t, defaultsBlock, `"technologies"`,
		"idless typed resources must not be selected by empty-args sync")

	knownBlock := syncSrc[knownStart:]
	assert.Contains(t, knownBlock, `"technologies"`,
		"explicit --resources should still accept the idless list endpoint")
	assert.Contains(t, syncSrc, "no default sync resources",
		"sync should explain why empty-args sync is a no-op")
	assert.Contains(t, syncSrc, `"reason":"no_bulk_list_endpoints"`,
		"JSON sync warnings should preserve the existing structured reason")
}

// dependentResourceSpec builds a minimal spec with a parent + child
// resource so syncDependentResource is actually emitted. The
// dependent-resource profiler requires paginated list endpoints (not
// bare GETs) with a parameterized child path for the child to be
// classified as syncable.
func dependentResourceSpec(name string) *spec.APISpec {
	paginated := func(path string) spec.Endpoint {
		return spec.Endpoint{
			Method:     "GET",
			Path:       path,
			Response:   spec.ResponseDef{Type: "array"},
			Pagination: &spec.Pagination{Type: "cursor", LimitParam: "limit", CursorParam: "after"},
		}
	}
	apiSpec := minimalSpec(name)
	apiSpec.Resources = map[string]spec.Resource{
		"projects": {
			Description: "Projects",
			Endpoints:   map[string]spec.Endpoint{"list": paginated("/projects")},
		},
		"tasks": {
			Description: "Tasks (child of projects)",
			Endpoints:   map[string]spec.Endpoint{"list": paginated("/projects/{project_id}/tasks")},
		},
	}
	return apiSpec
}

// TestGenerateSyncDependentSkipsFlatGlobalParam verifies the dependent-
// resource sync path calls applyTo with isDependent=true so --param
// (flatGlobal) is skipped on path-scoped requests. Without this gate, a
// top-level scope flag like --param workspace=<gid> double-applies to
// dependent calls like /projects/<gid>/tasks, and Asana-style APIs
// reject the call ("Must specify exactly one of project, tag, ...").
func TestGenerateSyncDependentSkipsFlatGlobalParam(t *testing.T) {
	t.Parallel()

	apiSpec := dependentResourceSpec("dependent-param")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncSrc := string(syncGo)

	require.Contains(t, syncSrc, "func syncDependentResource(",
		"dependent-resource sync should render when the spec has a {parent_id} child path")
	assert.Contains(t, syncSrc, "userParams.applyTo(dep.Name, params, true)",
		"dependent-resource call site must pass isDependent=true so --param is skipped on path-scoped calls")
	assert.Contains(t, syncSrc, "userParams.applyTo(resource, params, false)",
		"flat-list call site must pass isDependent=false so --param applies as before")
	assert.Contains(t, syncSrc, "syncDependentResources(cmd.Context(), c, db",
		"direct sync should route dependent-resource runs through the shared helper")
	assert.Contains(t, syncSrc, "userParams, syncEventWriter)",
		"direct sync should pass the selected event writer into dependent-resource sync")
	assert.Contains(t, syncSrc, "userParams, syncEvents)",
		"syncDependentResources should pass its event writer into each dependent-resource run")
}

// TestGenerateSyncUserParamsHelperRespectsFlatVsTrueGlobal pins the
// emitted applyTo helper: flatGlobal entries (--param) skip when
// isDependent=true, while trueGlobal entries (--global-param) and
// perResource entries always apply.
func TestGenerateSyncUserParamsHelperRespectsFlatVsTrueGlobal(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("scope-helper")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpersGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	helpersSrc := string(helpersGo)

	require.Contains(t, helpersSrc, "type syncUserParams struct",
		"syncUserParams struct must render")
	for _, want := range []string{
		"flatGlobal  map[string]string",
		"trueGlobal  map[string]string",
		"perResource map[string]map[string]string",
	} {
		assert.Contains(t, helpersSrc, want, "syncUserParams field %q must render", want)
	}
	assert.Contains(t, helpersSrc, "func (p *syncUserParams) applyTo(resource string, params map[string]string, isDependent bool)",
		"applyTo signature must include the isDependent flag")
	assert.Contains(t, helpersSrc, "if !isDependent {",
		"applyTo must gate flatGlobal on isDependent=false")
}

// TestGenerateSyncErrorJSONIncludesAPIBody verifies that the sync_error JSON
// event surfaces the API response body as a structured field, not just
// embedded inside an opaque err.Error() string. Without this surfacing, a
// 4xx whose JSON event stream contains only `errored: 1` leaves operators
// without the body needed to diagnose required-but-not-spec'd filter params.
func TestGenerateSyncErrorJSONIncludesAPIBody(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("syncerr")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	helpersGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	helpersSrc := string(helpersGo)

	// The helper unwraps *client.APIError and populates Status/Method/Path/Body.
	assert.Contains(t, helpersSrc, "func syncErrorJSON(resource, parent string, err error) string",
		"syncErrorJSON helper must exist with the resource/parent/err signature")
	assert.Contains(t, helpersSrc, "var apiErr *client.APIError",
		"helper must extract *client.APIError for structured fields")
	for _, snippet := range []string{
		"payload.Status = apiErr.StatusCode",
		"payload.Method = apiErr.Method",
		"payload.Path = apiErr.Path",
		"payload.Body = apiErr.Body",
	} {
		assert.Contains(t, helpersSrc, snippet,
			"sync_error payload should surface %s from APIError", snippet)
	}

	// The flat path now uses the helper instead of a hand-rolled fmt.Fprintf
	// that embedded the body inside err.Error(). Confirm the old form is
	// gone (otherwise the body would still be lost in a wrapped string).
	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncSrc := string(syncGo)
	assert.NotContains(t, syncSrc, `{"event":"sync_error","resource":"%s","error":"%s"}`,
		"sync_error event should be emitted via syncErrorJSON, not the legacy fmt.Fprintf shape")
	assert.Contains(t, syncSrc, `syncErrorJSON(resource, "", err)`,
		"syncResource flat path should emit sync_error via the helper")
}

// TestGenerateSyncDependentErrorNotSilent verifies the dependent-resource
// error path emits a sync_error JSON event for non-warning failures. The
// previous shape only emitted in human mode, so a 4xx on a parent request
// was invisible in agent-driven runs — operators saw missing rows with no
// diagnostic.
func TestGenerateSyncDependentErrorNotSilent(t *testing.T) {
	t.Parallel()

	apiSpec := dependentResourceSpec("dependent-err")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncSrc := string(syncGo)

	// syncDependentResource must actually be rendered for this spec.
	require.Contains(t, syncSrc, "func syncDependentResource(",
		"dependent-resource sync should render when the spec has a {parent_id} child path")

	// Non-warning errors must reach syncErrorJSON in the dep path. The
	// helper takes a non-empty parent ID so consumers can attribute the
	// failure to a specific parent.
	assert.Contains(t, syncSrc, "syncErrorJSON(dep.Name, parentID, err)",
		"dependent-resource non-warning error must emit a sync_error JSON event with the parent ID")
	assert.Contains(t, syncSrc, "fmt.Fprintln(syncEvents, syncErrorJSON(dep.Name, parentID, err))",
		"dependent-resource sync_error events should use the injected event writer")
}

// TestGeneratedSyncExtractPageItemsFallbackIgnoresUnknownScalarSiblings
// executes the emitted fallback extractor. It pins the regression from issue
// #2416: diagnostic scalar fields such as generated_at and request_id must not
// cause the only object array in an envelope to be dropped.
func TestGeneratedSyncExtractPageItemsFallbackIgnoresUnknownScalarSiblings(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("sync-envelope")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	runtimeTest := `package cli

import (
	"encoding/json"
	"testing"
)

func TestExtractPageItemsFallbackIgnoresUnknownScalarSiblings(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantCount  int
		wantOK     bool
		wantMore   bool
		wantCursor string
		wantID     string
	}{
		{
			name:      "unknown diagnostic scalar siblings",
			body:      ` + "`" + `{"things":[{"id":"t-1"},{"id":"t-2"}],"generated_at":"2024-01-01T00:00:00Z","request_id":"abc-123","api_version":"v2"}` + "`" + `,
			wantCount: 2,
			wantOK:    true,
			wantID:    "t-1",
		},
		{
			name:       "known cursor metadata siblings",
			body:       ` + "`" + `{"things":[{"id":"t-1"}],"cursor":"next-1","has_more":true}` + "`" + `,
			wantCount:  1,
			wantOK:     true,
			wantMore:   true,
			wantCursor: "next-1",
			wantID:     "t-1",
		},
		{
			name:      "ambiguous double object arrays",
			body:      ` + "`" + `{"primary":[{"id":"p-1"}],"secondary":[{"id":"s-1"}],"request_id":"abc-123"}` + "`" + `,
			wantOK:    false,
		},
		{
			name:      "no object array",
			body:      ` + "`" + `{"generated_at":"2024-01-01T00:00:00Z","request_id":"abc-123","count":2}` + "`" + `,
			wantOK:    false,
		},
		{
			name:      "known item key remains preferred",
			body:      ` + "`" + `{"items":[{"id":"canonical"}],"alternate":[{"id":"other"}],"request_id":"abc-123"}` + "`" + `,
			wantCount: 1,
			wantOK:    true,
			wantID:    "canonical",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, cursor, hasMore := extractPageItems(json.RawMessage(tc.body), "cursor")
			if tc.wantOK {
				if len(items) != tc.wantCount {
					t.Fatalf("len(items) = %d, want %d", len(items), tc.wantCount)
				}
				if tc.wantID != "" {
					var item map[string]string
					if err := json.Unmarshal(items[0], &item); err != nil {
						t.Fatalf("unmarshal first item: %v", err)
					}
					if item["id"] != tc.wantID {
						t.Fatalf("first item id = %q, want %q", item["id"], tc.wantID)
					}
				}
			} else if len(items) != 0 {
				t.Fatalf("len(items) = %d, want 0", len(items))
			}
			if cursor != tc.wantCursor {
				t.Fatalf("cursor = %q, want %q", cursor, tc.wantCursor)
			}
			if hasMore != tc.wantMore {
				t.Fatalf("hasMore = %v, want %v", hasMore, tc.wantMore)
			}
		})
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_extract_page_items_test.go"), []byte(runtimeTest), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestExtractPageItemsFallbackIgnoresUnknownScalarSiblings")
}

// noBulkListSpec mirrors the Allrecipes shape (issue #1156): a resource whose
// only GET endpoints are a parameterized detail page and a search with a
// required query param. Neither qualifies as a syncable list endpoint, so
// the generator's profile lands SyncableResources empty.
func noBulkListSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Resources = map[string]spec.Resource{
		"recipes": {
			Description: "Recipes",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method: "GET",
					Path:   "/recipe/{recipe_id}/{slug}",
					Params: []spec.Param{
						{Name: "recipe_id", PathParam: true, Required: true, Type: "string"},
						{Name: "slug", PathParam: true, Required: true, Type: "string"},
					},
					Response: spec.ResponseDef{Type: "object", Item: "Recipe"},
				},
				"search": {
					Method: "GET",
					Path:   "/search",
					Params: []spec.Param{
						{Name: "q", Required: true, Type: "string"},
					},
					Response: spec.ResponseDef{Type: "array"},
				},
			},
		},
	}
	return apiSpec
}

// TestGenerateSyncOmittedWhenNoBulkList covers issue #1156. When a spec
// has no bulk-list endpoint (only parameterized detail pages and required-
// query searches), there is no mirror-population path for generic sync, so the
// generator omits the command instead of exposing a no-op surface.
func TestGenerateSyncOmittedWhenNoBulkList(t *testing.T) {
	t.Parallel()

	apiSpec := noBulkListSpec("nobulk")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "sync.go"))
	assert.NotContains(t, rootSrc, "newSyncCmd(flags)")
}

// TestGenerateSyncSkipsEmptyHintWhenBulkListExists ensures the template hint
// is template-time conditional and does not appear when the spec exposes at
// least one syncable resource. Without this guard, every CLI would carry the
// dead branch.
func TestGenerateSyncSkipsEmptyHintWhenBulkListExists(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("withbulk")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncSrc := string(syncGo)

	assert.NotContains(t, syncSrc, "no bulk-list endpoints",
		"sync should not carry the empty-list hint when a syncable resource exists")
	assert.NotContains(t, syncSrc, `"reason":"no_bulk_list_endpoints"`,
		"sync should not emit the no_bulk_list_endpoints event when a syncable resource exists")
}
