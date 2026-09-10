package generator

import (
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/mvanhorn/cli-printing-press/v4/internal/vision"
	"github.com/stretchr/testify/require"
)

func TestSelectVisionTemplatesSelectedFilesExistInEmbed(t *testing.T) {
	t.Parallel()

	archetypes := []string{
		string(profiler.ArchetypeProjectMgmt),
		string(profiler.ArchetypeCommunication),
		string(profiler.ArchetypePayments),
		string(profiler.ArchetypeInfrastructure),
		string(profiler.ArchetypeContent),
		string(profiler.ArchetypeCRM),
		string(profiler.ArchetypeDeveloperPlatform),
		"",
	}
	insight := vision.NonObviousInsight{
		InsightFrame: "a local query surface",
		Implications: []string{"sync then sql"},
	}

	for _, arch := range archetypes {
		t.Run(arch, func(t *testing.T) {
			t.Parallel()
			set := SelectVisionTemplates(&vision.VisionaryPlan{
				Domain:  vision.DomainInfo{Archetype: arch},
				Insight: insight,
			})
			selected := append(append([]string{}, set.Workflows...), set.Insights...)
			for _, tmpl := range selected {
				_, err := templateFS.ReadFile(path.Join("templates", tmpl))
				require.NoError(t, err, "selected template %s for archetype %q must exist in the embed", tmpl, arch)
				require.NotEmpty(t, commandConstructorForTemplate(tmpl),
					"selected template %s for archetype %q must have a command constructor", tmpl, arch)
			}
		})
	}
}

func TestGenerateCommunicationArchetypeDoesNotWarnOnMissingCommHealth(t *testing.T) {
	apiSpec := communicationSpec("commhealthwarn")
	profile := profiler.Profile(apiSpec)
	require.Equal(t, profiler.ArchetypeCommunication, profile.Domain.Archetype)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	stderr, err := captureNovelFeatureStderr(t, func() error {
		return New(apiSpec, outputDir).Generate()
	})
	require.NoError(t, err)
	require.NotContains(t, stderr, "comm_health")
	require.NotContains(t, stderr, "skipping workflow template")

	_, statErr := os.Stat(filepath.Join(outputDir, "internal", "cli", "comm_health.go"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	skillSrc := readGeneratedFile(t, outputDir, "SKILL.md")
	require.NotContains(t, skillSrc, "channel-health")
}

func communicationSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Resources = map[string]spec.Resource{
		"messages": {
			Description: "Channel messages",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/messages", Description: "List messages"},
			},
		},
		"channels": {
			Description: "Chat channels",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/channels", Description: "List channels"},
			},
		},
	}
	return apiSpec
}
