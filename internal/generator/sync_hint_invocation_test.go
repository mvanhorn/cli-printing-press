package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncHintInvocationShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cliName  string
		syncable []profiler.SyncableResource
		want     string
	}{
		{
			name:    "populated default keeps bare sync",
			cliName: "catalog",
			syncable: []profiler.SyncableResource{
				{Name: "items"},
				{Name: "orders", SkipDefaultSync: true},
			},
			want: "catalog-pp-cli sync",
		},
		{
			name:    "empty default names working --resources",
			cliName: "ufcstats",
			syncable: []profiler.SyncableResource{
				{Name: "events", SkipDefaultSync: true},
				{Name: "fighters", SkipDefaultSync: true},
			},
			want: "ufcstats-pp-cli sync --resources events,fighters",
		},
		{
			name:    "vestigial skip-default resources emit no sync hint",
			cliName: "htmlpages",
			syncable: []profiler.SyncableResource{
				{
					Name:             "recipes",
					Path:             "/search",
					SkipDefaultSync:  true,
					UsesHTMLResponse: true,
					HTMLExtract:      &spec.HTMLExtract{Mode: spec.HTMLExtractModePage},
				},
			},
			want: "",
		},
		{
			name:     "no resources emits no sync hint",
			cliName:  "empty",
			syncable: nil,
			want:     "",
		},
		{
			name:    "blank cli name emits no hint",
			cliName: "  ",
			syncable: []profiler.SyncableResource{
				{Name: "items"},
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, syncHintInvocation(tt.cliName, tt.syncable))
		})
	}
}

func TestGeneratedHintsTrackEmptyDefaultSyncAndAuthNone(t *testing.T) {
	t.Parallel()

	apiSpec := skipDefaultSyncAuthNoneSpec("hint-none")
	profile := profiler.Profile(apiSpec)
	require.NotEmpty(t, profile.SyncableResources, "fixture must remain syncable via --resources")
	require.False(t, hasDefaultSyncResources(profile.SyncableResources),
		"fixture must emit an empty defaultSyncResources()")
	require.Equal(t, "hint-none-pp-cli sync --resources events,fighters",
		syncHintInvocation(apiSpec.Name, profile.SyncableResources))

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{
		Store:     true,
		Sync:      true,
		Search:    true,
		Analytics: true,
		MCP:       true,
		Workflows: []string{
			"workflows/pm_stale.go.tmpl",
			"workflows/pm_orphans.go.tmpl",
			"workflows/pm_load.go.tmpl",
		},
	}
	require.NoError(t, gen.Generate())

	wantInvocation := "hint-none-pp-cli sync --resources events,fighters"
	hintFiles := []string{
		filepath.Join("internal", "cli", "doctor.go"),
		filepath.Join("internal", "cli", "search.go"),
		filepath.Join("internal", "cli", "analytics.go"),
		filepath.Join("internal", "cli", "data_source.go"),
		filepath.Join("internal", "cli", "sync_hint.go"),
		filepath.Join("internal", "cli", "pm_stale.go"),
		filepath.Join("internal", "cli", "pm_orphans.go"),
		filepath.Join("internal", "cli", "pm_load.go"),
		filepath.Join("internal", "mcp", "tools.go"),
		"SKILL.md",
	}
	for _, rel := range hintFiles {
		src := readGeneratedFile(t, outputDir, strings.Split(rel, string(filepath.Separator))...)
		assert.Contains(t, src, wantInvocation,
			"%s should name the working sync --resources invocation", rel)
		stripped := strings.ReplaceAll(src, wantInvocation, "<SYNC>")
		assert.NotContains(t, stripped, "hint-none-pp-cli sync",
			"%s must not name a bare sync when defaultSyncResources is empty", rel)
	}

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.Contains(t, generatedFunctionBody(t, syncSrc, "func defaultSyncResources() []string"),
		"return []string{}",
		"defaultSyncResources must stay empty so the --resources form is the working path")

	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.NotContains(t, rootSrc, "verify auth",
		"auth.type none must not advertise auth verification in root help")
	assert.Contains(t, rootSrc, "verify connectivity")

	doctorSrc := readGeneratedFile(t, outputDir, "internal", "cli", "doctor.go")
	assert.NotContains(t, doctorSrc, "credential/path warnings",
		"auth.type none must not advertise a credential warning class")
	assert.Contains(t, doctorSrc, "path warnings plus errors")

	skillSrc := readGeneratedFile(t, outputDir, "SKILL.md")
	assert.NotContains(t, skillSrc, "credential-location warnings")
	assert.Contains(t, skillSrc, "path warnings")

	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./...")
}

func TestGeneratedHintsKeepPopulatedSyncAndAuth(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hint-bearer")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{
		Store:     true,
		Sync:      true,
		Search:    true,
		Analytics: true,
		MCP:       true,
	}
	require.NoError(t, gen.Generate())

	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, rootSrc, "verify auth and connectivity")

	doctorSrc := readGeneratedFile(t, outputDir, "internal", "cli", "doctor.go")
	assert.Contains(t, doctorSrc, "credential/path warnings plus errors")
	assert.Contains(t, doctorSrc, "run 'hint-bearer-pp-cli sync'")

	syncHintSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync_hint.go")
	assert.Contains(t, syncHintSrc, `const syncHintCommand = "hint-bearer-pp-cli sync"`)
	assert.NotContains(t, syncHintSrc, "--resources")

	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	assert.Contains(t, mcpSrc, `or run sync again if data may be stale`)
	assert.NotContains(t, mcpSrc, "or run hint-bearer-pp-cli sync again")

	requireGeneratedCompiles(t, outputDir)
}

func skipDefaultSyncAuthNoneSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"events": {
			Description: "Events",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/events",
					Description: "List events",
					Response:    spec.ResponseDef{Type: "array"},
					Params:      []spec.Param{{Name: "season", Type: "string", Required: true}},
				},
			},
		},
		"fighters": {
			Description: "Fighters",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/fighters",
					Description: "List fighters",
					Response:    spec.ResponseDef{Type: "array"},
					Params:      []spec.Param{{Name: "weight", Type: "string", Required: true}},
				},
			},
		},
	}
	return apiSpec
}
