package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"
	"gopkg.in/yaml.v3"
)

func TestGoreleaserLdflagsTargetMatchesVersionVar(t *testing.T) {
	// The goreleaser config injects the version via ldflags into
	// internal/version.Version. If the variable is renamed or moved,
	// goreleaser silently injects into nothing and the binary
	// reports the hardcoded fallback. This test catches that drift.

	// 1. Verify the version variable exists and is settable.
	assert.IsType(t, "", version.Version)

	// 2. Verify the goreleaser config references the correct ldflags path.
	data, err := os.ReadFile("../../.goreleaser.yaml")
	require.NoError(t, err)

	var config struct {
		Builds []struct {
			Ldflags []string `yaml:"ldflags"`
		} `yaml:"builds"`
	}
	require.NoError(t, yaml.Unmarshal(data, &config))
	require.NotEmpty(t, config.Builds)

	ldflags := strings.Join(config.Builds[0].Ldflags, " ")
	assert.Contains(t, ldflags,
		"github.com/mvanhorn/cli-printing-press/v3/internal/version.Version",
		"goreleaser ldflags must target internal/version.Version")
}

func TestReleasePleaseAnnotationExists(t *testing.T) {
	// release-please uses the x-release-please-version annotation
	// to find and bump the hardcoded version. If the annotation is
	// removed, release-please silently stops updating it.
	data, err := os.ReadFile("../version/version.go")
	require.NoError(t, err)

	assert.Contains(t, string(data), "x-release-please-version",
		"version.go must have x-release-please-version annotation for automated version bumps")
}

func TestVersionConsistencyAcrossFiles(t *testing.T) {
	// The plugin's version lives in exactly two places:
	//   - .claude-plugin/plugin.json ($.version)
	//   - internal/version/version.go (Version const, ldflags target)
	// release-please keeps both in lockstep; this test catches manual drift.
	//
	// marketplace.json intentionally does NOT carry a per-plugin version —
	// its $.metadata.version describes the marketplace format itself, not
	// any individual plugin entry. If either of those separate versions
	// ever needs to be asserted, add its own test; do not re-couple them
	// here.

	pluginData, err := os.ReadFile("../../.claude-plugin/plugin.json")
	require.NoError(t, err)

	var plugin struct {
		Version string `json:"version"`
	}
	require.NoError(t, json.Unmarshal(pluginData, &plugin))

	assert.Equal(t, plugin.Version, version.Version,
		"plugin.json and version.go hardcoded version must match")
}

func TestMarketplaceJSONHasNoPluginVersion(t *testing.T) {
	// Guard against a reviewer (or release-please misconfiguration) re-adding
	// a per-plugin version field to marketplace.json. Plugin versions live
	// only in plugin.json; this file catalogs plugins, not their versions.
	marketData, err := os.ReadFile("../../.claude-plugin/marketplace.json")
	require.NoError(t, err)

	var market struct {
		Plugins []map[string]any `json:"plugins"`
	}
	require.NoError(t, json.Unmarshal(marketData, &market))
	require.NotEmpty(t, market.Plugins)

	for i, p := range market.Plugins {
		if _, has := p["version"]; has {
			t.Errorf("marketplace.json plugins[%d] should not declare a version (belongs in plugin.json only)", i)
		}
	}
}

func TestModulePathMatchesMajorVersion(t *testing.T) {
	// Go's Semantic Import Versioning rule (https://go.dev/ref/mod#major-version-suffixes)
	// requires that any module at v2 or higher embed `/vN` as a suffix on its
	// module path AND in every internal import. If the module path drifts
	// from the source-of-truth Version constant, `go install …@latest`
	// silently resolves to a pseudo-version derived from the highest
	// compatible tag, and the installed binary reports the wrong version.
	//
	// This test is the tripwire: when release-please proposes a major bump
	// (e.g. v2 → v3), this test fails until go.mod is also updated to the
	// new /vN and every internal import is rewritten.

	data, err := os.ReadFile("../../go.mod")
	require.NoError(t, err)

	mf, err := modfile.Parse("go.mod", data, nil)
	require.NoError(t, err)
	require.NotNil(t, mf.Module, "go.mod must declare a module")

	major := majorVersion(t, version.Version)
	wantSuffix := ""
	if major >= 2 {
		wantSuffix = fmt.Sprintf("/v%d", major)
	}

	got := mf.Module.Mod.Path
	if wantSuffix == "" {
		assert.NotRegexp(t, regexp.MustCompile(`/v\d+$`), got,
			"v0/v1 modules must not have a /vN suffix")
	} else {
		assert.True(t, strings.HasSuffix(got, wantSuffix),
			"version.go is at v%d but go.mod path %q is missing %q suffix — see https://go.dev/ref/mod#major-version-suffixes",
			major, got, wantSuffix)
	}
}

func majorVersion(t *testing.T, v string) int {
	t.Helper()
	parts := strings.SplitN(v, ".", 2)
	require.NotEmpty(t, parts[0], "Version must have a major component")
	major, err := strconv.Atoi(parts[0])
	require.NoError(t, err, "Version major %q must be an integer", parts[0])
	return major
}

func TestPRTitleWorkflowAllowsReleasePleaseScope(t *testing.T) {
	// release-please uses the target branch as the conventional-commit scope
	// for generated release PR titles, e.g. chore(main): release 2.2.0.
	// The PR title workflow must allow that generated title or release PRs
	// cannot merge.
	data, err := os.ReadFile("../../.github/workflows/pr-title.yml")
	require.NoError(t, err)

	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Uses string         `yaml:"uses"`
				With map[string]any `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(data, &workflow))

	lintJob, ok := workflow.Jobs["lint"]
	require.True(t, ok, "PR title workflow should have a lint job")

	for _, step := range lintJob.Steps {
		if !strings.HasPrefix(step.Uses, "amannn/action-semantic-pull-request@") {
			continue
		}

		scopes, ok := step.With["scopes"].(string)
		require.True(t, ok, "semantic pull request action should declare scopes")

		allowed := map[string]bool{}
		for scope := range strings.FieldsSeq(scopes) {
			allowed[scope] = true
		}

		assert.True(t, allowed["main"], "release-please PR titles use main as the scope")
		return
	}

	t.Fatal("PR title workflow should use amannn/action-semantic-pull-request")
}
