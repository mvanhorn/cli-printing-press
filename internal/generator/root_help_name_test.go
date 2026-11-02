package generator

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootHelpUsesNarrativeDisplayName(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cipp")
	outputDir := filepath.Join(t.TempDir(), "cipp-pp-cli")
	gen := New(apiSpec, outputDir)
	headline := "Every CIPP endpoint as a typed command, plus cross-tenant fan-out"
	gen.Narrative = &ReadmeNarrative{
		DisplayName: "CIPP",
		Headline:    headline,
	}
	gen.NovelFeatures = []NovelFeature{
		{Command: "tenants fan-out", Description: "Run one call across tenants"},
	}
	require.NoError(t, gen.Generate())

	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	content := string(rootGo)
	want := "CIPP CLI — " + headline
	assert.Equal(t, want, rootField(t, content, "Short"))
	assert.Contains(t, rootField(t, content, "Long"), want)
	assert.NotContains(t, content, "Cipp CLI")

	readme := readGenerated(t, outputDir, "README.md")
	skill := readGenerated(t, outputDir, "SKILL.md")
	assert.Contains(t, readme, "# CIPP CLI")
	assert.Contains(t, skill, "# CIPP — Printing Press CLI")
	assert.NotContains(t, readme, "# Cipp CLI")
	assert.NotContains(t, skill, "# Cipp — Printing Press CLI")

	requireGeneratedCompiles(t, outputDir)
}

func TestRootHelpUsesSpecDisplayNameWithoutNarrative(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("halopsa")
	apiSpec.DisplayName = "HaloPSA"
	outputDir := filepath.Join(t.TempDir(), "halopsa-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	content := readGenerated(t, outputDir, "internal", "cli", "root.go")
	assert.Equal(t, "Manage HaloPSA resources via the HaloPSA API", rootField(t, content, "Short"))
	assert.Contains(t, rootField(t, content, "Long"), "Manage HaloPSA resources via the HaloPSA API.")
	assert.NotContains(t, content, "Halopsa")
	assert.NotContains(t, content, "Manage halopsa resources")

	readme := readGenerated(t, outputDir, "README.md")
	skill := readGenerated(t, outputDir, "SKILL.md")
	assert.Contains(t, readme, "# HaloPSA CLI")
	assert.Contains(t, skill, "# HaloPSA — Printing Press CLI")
	assert.Contains(t, skill, "Printing Press CLI for HaloPSA.")
	assert.NotContains(t, readme, "# Halopsa CLI")
}

func TestRootHelpUsesSpecDisplayNameWithHeadline(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hubspot")
	apiSpec.DisplayName = "HubSpot"
	outputDir := filepath.Join(t.TempDir(), "hubspot-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{Headline: "CRM objects as typed commands"}
	require.NoError(t, gen.Generate())

	content := readGenerated(t, outputDir, "internal", "cli", "root.go")
	want := "HubSpot CLI — CRM objects as typed commands"
	assert.Equal(t, want, rootField(t, content, "Short"))
	assert.Contains(t, rootField(t, content, "Long"), want)
	assert.NotContains(t, content, "Hubspot CLI")

	readme := readGenerated(t, outputDir, "README.md")
	assert.Contains(t, readme, "# HubSpot CLI")
}

func TestRootHelpNarrativeDisplayNameBeatsSpecDisplayName(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cipp")
	apiSpec.DisplayName = "HubSpot"
	outputDir := filepath.Join(t.TempDir(), "cipp-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		DisplayName: "CIPP",
		Headline:    "Cross-tenant commands",
	}
	require.NoError(t, gen.Generate())

	content := readGenerated(t, outputDir, "internal", "cli", "root.go")
	assert.Equal(t, "CIPP CLI — Cross-tenant commands", rootField(t, content, "Short"))
	assert.NotContains(t, content, "HubSpot CLI")
	assert.Contains(t, readGenerated(t, outputDir, "README.md"), "# CIPP CLI")
}

func TestRootHelpNovelFeaturesUseDisplayNameWithoutHeadline(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hubspot")
	apiSpec.DisplayName = "HubSpot"
	outputDir := filepath.Join(t.TempDir(), "hubspot-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{Command: "deals board", Description: "Pipeline across synced deals"},
	}
	require.NoError(t, gen.Generate())

	content := readGenerated(t, outputDir, "internal", "cli", "root.go")
	long := rootField(t, content, "Long")
	assert.True(t, strings.HasPrefix(long, "HubSpot CLI\n"), "long:\n%s", long)
	assert.Contains(t, long, "deals board")
	assert.NotContains(t, content, "Hubspot CLI")
	assert.Equal(t, "Manage HubSpot resources via the HubSpot API", rootField(t, content, "Short"))
}

func TestRootHelpCLIDescriptionIgnoresDisplayName(t *testing.T) {
	t.Parallel()

	const description = "Order Seattle pizza from the terminal"

	t.Run("no novel features", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("cipp")
		apiSpec.DisplayName = "CIPP"
		apiSpec.CLIDescription = description
		outputDir := filepath.Join(t.TempDir(), "cipp-pp-cli")
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())

		content := readGenerated(t, outputDir, "internal", "cli", "root.go")
		assert.Equal(t, description, rootField(t, content, "Short"))
		assert.Contains(t, rootField(t, content, "Long"), description)
		assert.NotContains(t, content, "CIPP CLI")
	})

	t.Run("with novel features", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("cipp")
		apiSpec.DisplayName = "CIPP"
		apiSpec.CLIDescription = description
		outputDir := filepath.Join(t.TempDir(), "cipp-pp-cli")
		gen := New(apiSpec, outputDir)
		gen.NovelFeatures = []NovelFeature{
			{Command: "tenants fan-out", Description: "Run one call across tenants"},
		}
		require.NoError(t, gen.Generate())

		content := readGenerated(t, outputDir, "internal", "cli", "root.go")
		long := rootField(t, content, "Long")
		assert.True(t, strings.HasPrefix(long, description+"\n"), "long:\n%s", long)
		assert.NotContains(t, content, "CIPP CLI")
	})
}

func TestRootHelpWithoutDistinctDisplayNameKeepsSlugSentence(t *testing.T) {
	t.Parallel()

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("cipp")
		outputDir := filepath.Join(t.TempDir(), "cipp-pp-cli")
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())

		content := readGenerated(t, outputDir, "internal", "cli", "root.go")
		assert.Contains(t, content, `Short: "Manage cipp resources via the cipp API"`)
		assert.Contains(t, content, "Manage cipp resources via the cipp API.")
		assert.NotContains(t, content, "Cipp CLI")
		assert.Contains(t, readGenerated(t, outputDir, "README.md"), "# Cipp CLI")
	})

	t.Run("equals human name", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("cipp")
		apiSpec.DisplayName = "Cipp"
		outputDir := filepath.Join(t.TempDir(), "cipp-pp-cli")
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())

		content := readGenerated(t, outputDir, "internal", "cli", "root.go")
		assert.Contains(t, content, `Short: "Manage cipp resources via the cipp API"`)
		assert.Contains(t, content, "Manage cipp resources via the cipp API.")
		assert.NotContains(t, content, "CIPP CLI")
	})

	t.Run("headline falls back to human name", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("cipp")
		outputDir := filepath.Join(t.TempDir(), "cipp-pp-cli")
		gen := New(apiSpec, outputDir)
		gen.Narrative = &ReadmeNarrative{Headline: "Every endpoint"}
		require.NoError(t, gen.Generate())

		content := readGenerated(t, outputDir, "internal", "cli", "root.go")
		assert.Equal(t, "Cipp CLI — Every endpoint", rootField(t, content, "Short"))
		assert.Contains(t, rootField(t, content, "Long"), "Cipp CLI — Every endpoint")
	})

	t.Run("title derived name stays on slug", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("cafe-bistro")
		apiSpec.DisplayName = "Café Bistro"
		apiSpec.DisplayNameDerivedFromTitle = true
		outputDir := filepath.Join(t.TempDir(), "cafe-bistro-pp-cli")
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())

		content := readGenerated(t, outputDir, "internal", "cli", "root.go")
		assert.Contains(t, content, `Short: "Manage cafe-bistro resources via the cafe-bistro API"`)
		assert.NotContains(t, content, "Café Bistro")
		assert.Contains(t, readGenerated(t, outputDir, "README.md"), "# Cafe Bistro CLI")
	})
}

func TestRootHelpDisplayNameBacktickStaysValidGo(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("ticks")
	apiSpec.DisplayName = "CI`PP"
	outputDir := filepath.Join(t.TempDir(), "ticks-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{Headline: "Typed endpoints"}
	require.NoError(t, gen.Generate())

	rootPath := filepath.Join(outputDir, "internal", "cli", "root.go")
	content := readGenerated(t, outputDir, "internal", "cli", "root.go")
	assert.Equal(t, "CI'PP CLI — Typed endpoints", rootField(t, content, "Short"))
	assert.Contains(t, rootField(t, content, "Long"), "CI'PP CLI — Typed endpoints")
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Short:") && strings.Contains(line, "CI'PP CLI") {
			assert.Equal(t, 2, strings.Count(line, "`"), "line: %s", line)
		}
	}
	_, err := parser.ParseFile(token.NewFileSet(), rootPath, nil, parser.AllErrors)
	require.NoError(t, err)
}
