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
// --param / --resource-param flags, parses them through parseSyncUserParams,
// and applies them after spec-derived params. Some APIs mark filter params
// optional in the spec but reject requests without them at runtime; without
// this passthrough, the only workaround is hand-editing the generated client.
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
	assert.Contains(t, syncSrc, "key=value",
		"--param help text should describe the key=value shape")
	assert.Contains(t, syncSrc, "resource:key=value",
		"--resource-param help text should describe the resource:key=value shape")

	// Parsing runs before client construction so a malformed flag fails fast
	// (and as usageErr, not a generic runtime error).
	assert.Contains(t, syncSrc, "parseSyncUserParams(paramFlags, resourceParamFlags)",
		"sync must parse user params at RunE entry")
	parseIdx := strings.Index(syncSrc, "parseSyncUserParams(paramFlags, resourceParamFlags)")
	newClientIdx := strings.Index(syncSrc, "flags.newClient()")
	require.NotEqual(t, -1, parseIdx)
	require.NotEqual(t, -1, newClientIdx)
	assert.Less(t, parseIdx, newClientIdx,
		"--param must parse before newClient so usage errors don't waste an HTTP handshake")

	// userParams flows through to the syncResource worker. The exact call
	// site differs by template branch (HasTierRouting vs not), so assert the
	// last arg is userParams.
	assert.Contains(t, syncSrc, ", userParams)",
		"syncResource and syncDependentResources must receive userParams")

	// applyTo is called in the page loop AFTER cursor/since/limit are set,
	// so user flags win on conflict.
	loopIdx := strings.Index(syncSrc, "params[pageSize.limitParam] = strconv.Itoa(pageSize.limit)")
	applyIdx := strings.Index(syncSrc, "userParams.applyTo(resource, params)")
	require.NotEqual(t, -1, loopIdx, "page loop should set the limit param")
	require.NotEqual(t, -1, applyIdx, "syncResource should apply user params before c.Get")
	assert.Less(t, loopIdx, applyIdx,
		"user params must apply after spec-derived params so flags can override")
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

	// Build a spec with a parent + child resource so syncDependentResource
	// is actually emitted. The dependent-resource profiler requires paginated
	// list endpoints (not bare GETs) for the parameterized child path to be
	// classified as syncable; see profiler.detectDependentResources.
	paginated := func(path string) spec.Endpoint {
		return spec.Endpoint{
			Method:     "GET",
			Path:       path,
			Response:   spec.ResponseDef{Type: "array"},
			Pagination: &spec.Pagination{Type: "cursor", LimitParam: "limit", CursorParam: "after"},
		}
	}
	apiSpec := &spec.APISpec{
		Name:      "dependent-err",
		Version:   "0.1.0",
		BaseURL:   "https://api.example.test/v1",
		Owner:     "test-owner",
		OwnerName: "Test Author",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"DEPENDENT_ERR_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/dependent-err-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"teams": {
				Description: "Teams",
				Endpoints: map[string]spec.Endpoint{
					"list": paginated("/teams"),
				},
			},
			"players": {
				Description: "Players (child of teams)",
				Endpoints: map[string]spec.Endpoint{
					"list": paginated("/teams/{team_id}/players"),
				},
			},
		},
	}

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
}
