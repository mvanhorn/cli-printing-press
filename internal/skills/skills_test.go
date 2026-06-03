// Package skills tests the frontmatter contract of the in-repo internal
// skills under skills/<name>/SKILL.md. These are the machine-side
// development skills (printing-press, -amend, -publish, -retro, etc.) —
// not the generator-emitted per-CLI skills.
//
// The contract pins the Hermes-aligned top-level fields (author, license,
// metadata.hermes.tags) so that a `hermes skills install mvanhorn/cli-printing-press`
// can discover and install the skills, and so that the existing Claude Code
// install path (a flat `cp -R` from .claude/scripts/install-internal-skills.sh)
// continues to work unchanged.
package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// internalSkillAuthorByName is the canonical author display name for each
// internal skill, sourced from git first-commit evidence
// (`git log --format=%an --reverse --follow skills/<name>/SKILL.md | head -1`)
// per the preserve-original-authorship convention
// (docs/solutions/conventions/preserve-original-authorship-in-multi-author-retrofits-2026-05-06.md).
//
// Do NOT derive this from `git config user.name` — that flips attribution
// silently to whoever runs the sweep, which is the exact failure mode the
// cited solution document warns against.
var internalSkillAuthorByName = map[string]string{
	"printing-press":               "Matt Van Horn",
	"printing-press-amend":         "Matt Van Horn",
	"printing-press-catalog":       "Matt Van Horn",
	"printing-press-import":        "Trevin Chow",
	"printing-press-output-review": "Trevin Chow",
	"printing-press-polish":        "Matt Van Horn",
	"printing-press-publish":       "Trevin Chow",
	"printing-press-reprint":       "Trevin Chow",
	"printing-press-retro":         "Trevin Chow",
	"printing-press-score":         "Trevin Chow",
}

// frontmatter is the parsed YAML frontmatter of an internal SKILL.md.
// Only fields the tests assert on are typed; unknown YAML keys are
// ignored, so future Hermes / ClawHub additions don't break parsing.
type frontmatter struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	Author       string   `yaml:"author"`
	License      string   `yaml:"license"`
	AllowedTools []string `yaml:"allowed-tools"`
	Metadata     struct {
		Hermes struct {
			Tags []string `yaml:"tags"`
		} `yaml:"hermes"`
	} `yaml:"metadata"`
}

// parseFrontmatter extracts the YAML frontmatter between the first pair of
// `---` delimiters and unmarshals it into the frontmatter struct. Mirrors
// the extraction pattern in internal/generator/skill_test.go:602-643.
func parseFrontmatter(t *testing.T, path string) frontmatter {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)

	require.True(t, strings.HasPrefix(string(content), "---\n"),
		"%s must start with `---` frontmatter delimiter", path)
	// Accept either `\n---\n` (body follows) or `\n---` (end of file) as the
	// closing fence. Editor-saved files always have the trailing newline;
	// the shorter pattern tolerates tools that strip trailing newlines so
	// the failure message points at the real cause instead of "no closing
	// delimiter" when the delimiter is in fact present.
	end := strings.Index(string(content[4:]), "\n---")
	require.NotEqual(t, -1, end, "%s must have a closing `---` frontmatter delimiter", path)
	body := string(content[4 : 4+end+1])

	var fm frontmatter
	require.NoError(t, yaml.Unmarshal([]byte(body), &fm),
		"%s frontmatter must parse as nested YAML; body was:\n%s", path, body)
	return fm
}

// listInternalSkills returns the names of every skills/<name>/ directory
// that contains a SKILL.md file. Hidden directories (those starting with
// `.`) are skipped. Order is filesystem-defined but stable within a run.
func listInternalSkills(t *testing.T, repoRoot string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(repoRoot, "skills"))
	require.NoError(t, err, "list skills/ in %s", repoRoot)
	var names []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		skillPath := filepath.Join(repoRoot, "skills", e.Name(), "SKILL.md")
		if _, err := os.Stat(skillPath); err == nil {
			names = append(names, e.Name())
		}
	}
	require.NotEmpty(t, names, "no SKILL.md files found under %s/skills/", repoRoot)
	return names
}

// TestAllInternalSkillsHaveHermesFrontmatter asserts every internal skill
// carries the three Hermes-recognized top-level fields (author, license,
// metadata.hermes.tags) and that author matches the curated per-skill
// first-author. The mirror in the printed-CLI template is at
// internal/generator/templates/skill.md.tmpl:4-5 (the
// `author: "{{yamlDoubleQuoted .OwnerName}}"` and `license: "Apache-2.0"`
// lines) and the printed-CLI test at
// internal/generator/skill_test.go:602-643 pins the same fields for
// generator-emitted output.
func TestAllInternalSkillsHaveHermesFrontmatter(t *testing.T) {
	t.Parallel()

	repoRoot := testutil.FindRepoRoot(t)
	skills := listInternalSkills(t, repoRoot)

	for _, name := range skills {
		t.Run(name, func(t *testing.T) {
			skillPath := filepath.Join(repoRoot, "skills", name, "SKILL.md")
			fm := parseFrontmatter(t, skillPath)

			expectedAuthor, ok := internalSkillAuthorByName[name]
			require.True(t, ok, "internalSkillAuthorByName missing entry for %q", name)

			assert.Equal(t, name, fm.Name,
				"%s: frontmatter `name` must match the directory name", skillPath)
			assert.NotEmpty(t, fm.Description,
				"%s: frontmatter `description` is required (Hermes treats it as the primary discovery field)", skillPath)
			assert.Equal(t, expectedAuthor, fm.Author,
				"%s: frontmatter `author` must be the git-first-author display name, not a slug or operator identity", skillPath)
			assert.Equal(t, "Apache-2.0", fm.License,
				"%s: frontmatter `license` is the project standard Apache-2.0 (see internal/generator/templates/skill.md.tmpl)", skillPath)
			assert.NotEmpty(t, fm.Metadata.Hermes.Tags,
				"%s: frontmatter `metadata.hermes.tags` must have at least one tag for Hermes discoverability", skillPath)
		})
	}
}

// TestInternalSkillFrontmatterPreservesExistingFields asserts the new
// fields did not clobber the existing Claude-Code-specific fields
// (name, description, allowed-tools, plus optional version,
// min-binary-version, context, user-invocable, deprecated). The Hermes
// docs say unknown keys are ignored, so these can coexist; the test
// catches over-eager edits that would have replaced instead of added.
func TestInternalSkillFrontmatterPreservesExistingFields(t *testing.T) {
	t.Parallel()

	repoRoot := testutil.FindRepoRoot(t)
	skills := listInternalSkills(t, repoRoot)

	for _, name := range skills {
		t.Run(name, func(t *testing.T) {
			skillPath := filepath.Join(repoRoot, "skills", name, "SKILL.md")
			fm := parseFrontmatter(t, skillPath)

			assert.NotEmpty(t, fm.Name,
				"%s: required `name` field missing (clobbered by new fields?)", skillPath)
			assert.NotEmpty(t, fm.Description,
				"%s: required `description` field missing (clobbered by new fields?)", skillPath)

			// allowed-tools is required on every internal skill; a typed
			// field catches misnamed variants (e.g. allowed_tool) that
			// the parser would otherwise silently drop.
			assert.NotEmpty(t, fm.AllowedTools,
				"%s: `allowed-tools` must be present and non-empty after the frontmatter edit", skillPath)
		})
	}
}

// TestInternalSkillHermesTagsAreNonEmpty asserts every skill's
// metadata.hermes.tags block has at least one element. The plan allows
// either a shared `[printing-press, codegen, openapi, go, api]` set across
// all skills, or a per-skill-curated set with an additional function tag.
// Both shapes satisfy this test.
func TestInternalSkillHermesTagsAreNonEmpty(t *testing.T) {
	t.Parallel()

	repoRoot := testutil.FindRepoRoot(t)
	skills := listInternalSkills(t, repoRoot)

	for _, name := range skills {
		t.Run(name, func(t *testing.T) {
			skillPath := filepath.Join(repoRoot, "skills", name, "SKILL.md")
			fm := parseFrontmatter(t, skillPath)

			require.NotEmpty(t, fm.Metadata.Hermes.Tags,
				"%s: metadata.hermes.tags was emitted as an empty placeholder", skillPath)
			for _, tag := range fm.Metadata.Hermes.Tags {
				assert.NotEmpty(t, tag,
					"%s: metadata.hermes.tags contains an empty string", skillPath)
			}
		})
	}
}
