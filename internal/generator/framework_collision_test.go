package generator

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateNoAuthRegistersAuthResource(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("publicauth")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"auth": {
			Description: "Public auth endpoints",
			Endpoints: map[string]spec.Endpoint{
				"check_email": {Method: "GET", Path: "/api/auth/check-email", Description: "Check email"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "publicauth-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	authSrc := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	assert.Contains(t, rootSrc, "rootCmd.AddCommand(newAuthCmd(flags))", "the API resource should be registered when the framework auth command is inactive")
	assert.Contains(t, authSrc, "func newAuthCmd(flags *rootFlags) *cobra.Command")
	assert.Contains(t, authSrc, `Use:   "auth"`)
	assert.NotContains(t, authSrc, "set-token", "no framework auth template should overwrite the API resource parent")

	runGoCommand(t, outputDir, "build", "./internal/cli")
}

func TestGenerateRegistersHealthResourceWhenHealthCommandInactive(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("publichealth")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"health": {
			Description: "API health endpoint",
			Endpoints: map[string]spec.Endpoint{
				"get":    {Method: "GET", Path: "/health", Description: "Health"},
				"status": {Method: "GET", Path: "/health/status", Description: "Health status"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "publichealth-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	healthSrc := readGeneratedFile(t, outputDir, "internal", "cli", "health.go")
	assert.Contains(t, rootSrc, "rootCmd.AddCommand(newHealthCmd(flags))", "the API resource should be registered when the framework health command is inactive")
	assert.NotContains(t, rootSrc, "rootCmd.AddCommand(newPublichealthHealthCmd(flags))")
	assert.Contains(t, healthSrc, "func newHealthCmd(flags *rootFlags) *cobra.Command")
	assert.Contains(t, healthSrc, `Use:   "health"`)

	runGoCommand(t, outputDir, "build", "./internal/cli")
}

func TestActiveFrameworkCobraUseNamesMatchesGeneratedRoot(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("activecmds")
	apiSpec.BearerRefresh = spec.BearerRefreshConfig{
		BundleURL: "https://example.com/main.js",
		Pattern:   `accessToken:"([^"]+)"`,
	}
	apiSpec.Share = spec.ShareConfig{Enabled: true, SnapshotTables: []string{"items"}}

	outputDir := filepath.Join(t.TempDir(), "activecmds-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{
		Export:    true,
		Import:    true,
		Store:     true,
		Search:    true,
		Sync:      true,
		Tail:      true,
		Analytics: true,
		Workflows: []string{
			"workflows/pm_stale.go.tmpl",
			"workflows/pm_orphans.go.tmpl",
			"workflows/pm_load.go.tmpl",
		},
		Insights: []string{
			"insights/health_score.go.tmpl",
			"insights/similar.go.tmpl",
		},
	}
	gen.AsyncJobs = map[string]AsyncJobInfo{
		"items/list": {ResourceName: "items", EndpointName: "list"},
	}
	require.NoError(t, gen.Generate())

	active := gen.activeFrameworkCobraUseNames()
	delete(active, "completion")
	delete(active, "help")

	rootReserved := generatedRootReservedUseNames(t, outputDir)
	assert.Equal(t, sortedKeys(rootReserved), sortedKeys(active),
		"activeFrameworkCobraUseNames must stay aligned with generated root framework registrations")
}

func generatedRootReservedUseNames(t *testing.T, outputDir string) map[string]struct{} {
	t.Helper()
	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	ctorRE := regexp.MustCompile(`rootCmd\.AddCommand\(new(\w+)Cmd\(`)
	ctors := map[string]struct{}{}
	for _, match := range ctorRE.FindAllStringSubmatch(rootSrc, -1) {
		ctors[match[1]] = struct{}{}
	}

	useByConstructor := map[string]string{}
	funcRE := regexp.MustCompile(`func new(\w+)Cmd[^{]*\{[\s\S]*?Use:\s*"([^"]+)"`)
	err := filepath.WalkDir(filepath.Join(outputDir, "internal", "cli"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range funcRE.FindAllStringSubmatch(string(data), -1) {
			useByConstructor[match[1]] = strings.Fields(match[2])[0]
		}
		return nil
	})
	require.NoError(t, err)

	out := map[string]struct{}{}
	for ctor := range ctors {
		use, ok := useByConstructor[ctor]
		if !ok {
			continue
		}
		if _, reserved := spec.ReservedCobraUseNames[use]; reserved {
			out[use] = struct{}{}
		}
	}
	return out
}

func TestGenerateRenamesHealthResourceOnlyWhenHealthCommandIsActive(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("healthapi")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"health": {
			Description: "API health endpoint",
			Endpoints: map[string]spec.Endpoint{
				"get":    {Method: "GET", Path: "/health", Description: "Health"},
				"status": {Method: "GET", Path: "/health/status", Description: "Health status"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "healthapi-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Insights: []string{"insights/health_score.go.tmpl"}}
	require.NoError(t, gen.Generate())

	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, rootSrc, "rootCmd.AddCommand(newHealthCmd(flags))", "the generated insight keeps the framework health command")
	assert.Contains(t, rootSrc, "rootCmd.AddCommand(newHealthapiHealthCmd(flags))", "the API health resource is renamed only when the framework command is active")
	assert.NotContains(t, rootSrc, "rootCmd.AddCommand(newHealthCmd(flags))\n\trootCmd.AddCommand(newHealthCmd(flags))")

	runGoCommand(t, outputDir, "build", "./internal/cli")
}

func TestGenerateRenamesAuthResourceWhenTrafficAnalysisEmitsAuthCommand(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("graphqlapi")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"auth": {
			Description: "Public auth endpoint",
			Endpoints: map[string]spec.Endpoint{
				"check":  {Method: "GET", Path: "/api/auth/check", Description: "Check auth"},
				"status": {Method: "GET", Path: "/api/auth/status", Description: "Auth status"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "graphqlapi-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.TrafficAnalysis = &browsersniff.TrafficAnalysis{GenerationHints: []string{"graphql_persisted_query"}}
	require.NoError(t, gen.Generate())

	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, rootSrc, "rootCmd.AddCommand(newAuthCmd(flags))", "the traffic-analysis hint should emit the framework auth command")
	assert.Contains(t, rootSrc, "rootCmd.AddCommand(newGraphqlapiAuthCmd(flags))", "the public auth resource should be renamed and stay registered")
	assert.NotContains(t, rootSrc, "rootCmd.AddCommand(newAuthCmd(flags))\n\trootCmd.AddCommand(newAuthCmd(flags))")

	runGoCommand(t, outputDir, "build", "./internal/cli")
}
