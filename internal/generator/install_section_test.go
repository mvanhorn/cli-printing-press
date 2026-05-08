// Copyright 2026 trevin-chow. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestCanonicalSkillInstallSectionMatchesTemplate is the sync gate between
// CanonicalSkillInstallSection and skill.md.tmpl. The two are parallel
// renderings of the same canonical install block — one Go literal, one
// Go template — and they must produce byte-identical output for any
// (name, category) tuple. If either drifts, this test
// fails before any printed CLI ships with a desynced install section.
//
// The verify-skill canonical-sections check enforces this contract at
// the printed-CLI boundary; this test enforces it at the generator
// boundary so changes to the template (or to the function) cannot
// silently desync.
func TestCanonicalSkillInstallSectionMatchesTemplate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		label           string
		apiName         string
		category        string
		usesBrowserHTTP bool
	}{
		{"empty category and standard transport", "myapi", "", false},
		{"explicit category", "myapi", "productivity", false},
		{"browser transport uses same Go floor", "myapi", "productivity", true},
		{"slug with hyphens", "trigger-dev", "developer-tools", false},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()

			s := minimalSpec(tc.apiName)
			s.Category = tc.category
			if tc.usesBrowserHTTP {
				s.HTTPTransport = spec.HTTPTransportBrowserChrome
			}

			outputDir := filepath.Join(t.TempDir(), tc.apiName+"-pp-cli")
			gen := New(s, outputDir)
			require.NoError(t, gen.Generate())

			rendered, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
			require.NoError(t, err)

			extracted, ok := ExtractSkillInstallSection(string(rendered))
			require.True(t, ok, "extractor must find the install section in a freshly-rendered SKILL.md")

			expected := CanonicalSkillInstallSection(tc.apiName, tc.category)
			require.Equal(t, expected, extracted,
				"template-rendered install section must equal CanonicalSkillInstallSection output for %s", tc.label)
		})
	}
}

func TestCategorylessInstallSectionsAvoidOtherLibraryPath(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("gohighlevel")
	outputDir := filepath.Join(t.TempDir(), "gohighlevel-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	for _, filename := range []string{"SKILL.md", "README.md"} {
		t.Run(filename, func(t *testing.T) {
			t.Parallel()

			rendered, err := os.ReadFile(filepath.Join(outputDir, filename))
			require.NoError(t, err)

			content := string(rendered)
			require.NotContains(t, content, "library/other/gohighlevel",
				"category-less generation must not bake in a publish-time placeholder category")
			require.Contains(t, content, "npx -y @mvanhorn/printing-press install gohighlevel",
				"category-less generation should keep the category-agnostic installer path")
		})
	}
}

// TestExtractSkillInstallSectionMissingStart confirms the extractor
// reports ok=false when the canonical heading is missing — the case
// where an agent has rewritten the section into something unrecognizable.
func TestExtractSkillInstallSectionMissingStart(t *testing.T) {
	t.Parallel()
	_, ok := ExtractSkillInstallSection("# Some Skill\n\nNo prerequisites heading here.\n")
	require.False(t, ok)
}

// TestExtractSkillInstallSectionMissingEnd confirms the extractor reports
// ok=false when the heading exists but the canonical end-sentinel is gone
// — for example, when an agent stripped the troubleshooting line so the
// canonical block can't be sliced cleanly.
func TestExtractSkillInstallSectionMissingEnd(t *testing.T) {
	t.Parallel()
	skill := "# Some Skill\n\n## Prerequisites: Install the CLI\n\nrun some command\n\n## When to Use\n"
	_, ok := ExtractSkillInstallSection(skill)
	require.False(t, ok)
}
