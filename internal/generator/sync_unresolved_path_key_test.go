// Copyright 2026 Anthropic, PBC. Licensed under Apache-2.0.

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

// TestGenerateSyncSkipsUnresolvedPathKeys verifies that the sync template
// includes the unresolved-{key} skip block. Hierarchical-API specs (Yahoo
// Fantasy, Reddit pre-2024, YouTube Data v3) have resource paths whose
// {key} placeholders cannot be filled from flat-list context. Sync should
// emit a sync_warning and skip the resource instead of aborting the run.
//
// Structural test — asserts the generated sync.go contains the skip wiring.
// A runtime assertion would require spinning up a mock HTTP server and
// invoking the generated binary; we cover that path via the integration
// suite. This test guards the template against template-level regression.
//
// Refs #1006.
func TestGenerateSyncSkipsUnresolvedPathKeys(t *testing.T) {
	t.Parallel()

	// One resource whose path contains an unresolved {key}. The placeholder
	// `{external_team_uuid}` is intentionally non-derivable from any other
	// resource name in this spec, so the dependent-resource auto-detector
	// won't claim it — the resource lands in the flat sync path where the
	// new skip block fires.
	apiSpec := &spec.APISpec{
		Name:    "hierarchical-sample",
		Version: "0.1.0",
		BaseURL: "https://api.example.test/v1",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"HIERARCHICAL_SAMPLE_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/hierarchical-sample-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Items scoped by an external parent key",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/parent/{external_team_uuid}/items",
						Description: "List items under an external parent",
						Response:    spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncContent := string(syncGo)

	// Structural assertion 1: the regex var declaration is present.
	assert.Contains(t, syncContent, "unresolvedPathKeyRE",
		"generated sync.go should declare unresolvedPathKeyRE var")
	assert.Contains(t, syncContent, "regexp.MustCompile(`\\{[a-z_][a-z0-9_]*\\}`)",
		"unresolvedPathKeyRE should match lowercase {key} placeholders")

	// Structural assertion 2: the FindAllString check is wired in syncResource.
	assert.Contains(t, syncContent, "unresolvedPathKeyRE.FindAllString(path, -1)",
		"syncResource should scan the resolved path for unresolved {key}s")

	// Structural assertion 3: the sync_warning event payload is correct.
	assert.Contains(t, syncContent, `"event":"sync_warning"`,
		"skip branch should emit a sync_warning event")
	assert.Contains(t, syncContent, `"reason":"unfilled_path_key"`,
		"skip branch should use reason=unfilled_path_key")

	// Structural assertion 4: the skip branch returns a Warn (non-fatal),
	// not an Err. The orchestrator's exit policy depends on this distinction.
	assert.Contains(t, syncContent, `Warn:     fmt.Errorf("skipped %s: unresolved path keys`,
		"unresolved-key skip should populate syncResult.Warn, not Err")

	// Sanity: the generated code should still compile. `go build` for the
	// full generated project is exercised by TestGenerateDependentSyncCompiles
	// and friends; here we just verify the template renders without producing
	// a syntax error by checking that the regex literal is well-formed.
	assert.NotContains(t, syncContent, "regexp.MustCompile(``)",
		"unresolvedPathKeyRE pattern should not render as empty literal")
}

// TestGenerateSyncFlatAPIUnaffected verifies the skip block is a no-op for
// flat APIs whose paths contain no {key} placeholders. The generated code
// is identical except for the new constant and check, and the runtime
// branch never fires.
func TestGenerateSyncFlatAPIUnaffected(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "flat-sample",
		Version: "0.1.0",
		BaseURL: "https://api.example.test/v1",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"FLAT_SAMPLE_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/flat-sample-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"users": {
				Description: "Manage users",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/users",
						Description: "List users",
						Response:    spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncContent := string(syncGo)

	// The skip-block template is unconditionally rendered (it's a runtime
	// check, not a template conditional), so the var declaration and check
	// are present even for flat APIs. The runtime branch is a no-op when
	// no path contains an unresolved {key}.
	assert.Contains(t, syncContent, "unresolvedPathKeyRE",
		"unresolvedPathKeyRE should be present even on flat-API CLIs (no-op at runtime)")

	// The flat resource's sync path map entry should NOT contain a {key},
	// so we expect no runtime trigger.
	assert.Contains(t, syncContent, `"users": "/users"`,
		"flat users resource should have a clean path with no placeholders")
}
