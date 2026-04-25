package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestSkillRendersFrontmatterAndCapabilities verifies that a generated
// SKILL.md carries the expected frontmatter fields and surfaces novel
// features as an inline "Unique Capabilities" block (not requiring agents
// to call --help for discovery).
func TestSkillRendersFrontmatterAndCapabilities(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("finance")
	apiSpec.Category = "commerce"
	outputDir := filepath.Join(t.TempDir(), "finance-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		Headline:       "Quotes, charts, and a local portfolio nothing else has",
		ValueProp:      "Quotes, charts, fundamentals, options chains, and a SQLite-backed portfolio tracker.",
		WhenToUse:      "Reach for this CLI when an agent needs quotes, fundamentals, or persistent portfolio state against Yahoo Finance.",
		TriggerPhrases: []string{"quote AAPL", "check my portfolio", "options for TSLA"},
		Recipes: []Recipe{
			{Title: "Morning digest", Command: "finance-pp-cli digest --watchlist tech", Explanation: "Biggest movers across a named watchlist."},
		},
	}
	gen.NovelFeatures = []NovelFeature{
		{
			Command:      "portfolio perf",
			Description:  "Unrealized P&L across synced lots",
			Example:      "finance-pp-cli portfolio perf --agent",
			WhyItMatters: "Agents answer 'how's my portfolio' in one call",
			Group:        "Local state that compounds",
		},
	}
	require.NoError(t, gen.Generate())

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)

	// Frontmatter
	assert.True(t, strings.Contains(content, "name: pp-finance"),
		"frontmatter name should be pp-<api>")
	assert.True(t, strings.Contains(content, "Quotes, charts, and a local portfolio nothing else has"),
		"frontmatter description should incorporate headline")
	assert.True(t, strings.Contains(content, "`quote AAPL`"),
		"frontmatter description should list domain-specific trigger phrases verbatim (backtick-delimited)")
	assert.True(t, strings.Contains(content, "library/commerce/finance-pp-cli"),
		"openclaw install manifest should use the API's category")

	// Body
	assert.True(t, strings.Contains(content, "## When to Use This CLI"),
		"WhenToUse narrative should render as its own section")
	assert.True(t, strings.Contains(content, "## Unique Capabilities"),
		"Novel features should appear as Unique Capabilities so agents don't need --help discovery")
	assert.True(t, strings.Contains(content, "### Local state that compounds"),
		"grouped novel features should render as subheadings in SKILL too")
	assert.True(t, strings.Contains(content, "finance-pp-cli portfolio perf --agent"),
		"novel-feature example should render as a copy-pasteable invocation")
	assert.True(t, strings.Contains(content, "_Agents answer 'how's my portfolio' in one call_"),
		"WhyItMatters should render as italic")

	// Command reference
	assert.True(t, strings.Contains(content, "**items** — Manage items"),
		"Command Reference should list resources inline so agents skip discovery")

	// Recipes
	assert.True(t, strings.Contains(content, "### Morning digest"),
		"Recipes should render as subsections with titles")
	assert.True(t, strings.Contains(content, "finance-pp-cli digest --watchlist tech"),
		"Recipes should include runnable commands")

	// Installation
	assert.True(t, strings.Contains(content, "## CLI Installation"),
		"SKILL should include CLI install instructions")
	assert.True(t, strings.Contains(content, "## MCP Server Installation"),
		"SKILL should include MCP install instructions")
	assert.True(t, strings.Contains(content, "| 10 | Config error"),
		"Exit codes table should render")
}

// TestSkillFallsBackWhenNarrativeAbsent asserts SKILL.md still renders a
// usable skill file when absorb data is missing — fallback uses .Description
// and the deterministic sections only.
func TestSkillFallsBackWhenNarrativeAbsent(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("bare")
	apiSpec.Description = "A basic API."
	outputDir := filepath.Join(t.TempDir(), "bare-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)

	assert.True(t, strings.Contains(content, "name: pp-bare"),
		"frontmatter still renders without narrative")
	assert.True(t, strings.Contains(content, "A basic API."),
		"description falls back to spec description")
	assert.False(t, strings.Contains(content, "## When to Use This CLI"),
		"WhenToUse section should be omitted when narrative is absent")
	assert.False(t, strings.Contains(content, "## Recipes"),
		"Recipes section should be omitted when narrative is absent")
	assert.True(t, strings.Contains(content, "## Auth Setup"),
		"Auth Setup always renders (falls back to auth-type branch)")
	assert.True(t, strings.Contains(content, "## Exit Codes"),
		"Exit codes always render")
	assert.True(t, strings.Contains(content, "## Command Reference"),
		"Command Reference always renders from the spec")
}

func TestReadOnlyNoAuthSkillSuppressesInapplicableBoilerplate(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("skillro")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	outputDir := filepath.Join(t.TempDir(), "skillro-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)

	assert.Contains(t, content, "Read-only")
	assert.Contains(t, content, "## When Not to Use This CLI")
	assert.NotContains(t, content, "<cli>-pp-cli")
	assert.NotContains(t, content, "<CLI>_FEEDBACK")
	assert.NotContains(t, content, "--wait-timeout")
	assert.NotContains(t, content, "| 4 | Authentication required |")
	assert.Contains(t, content, "| 7 | Rate limited")
	assert.NotContains(t, content, "GET responses cached for 5 minutes")
}

// TestSkillFrontmatterEscapesNarrativeQuotesAndNewlines asserts that
// LLM-authored narrative fields with double quotes, newlines, or
// backslashes don't break the YAML frontmatter. Without escaping, an
// inner " collapses the outer scalar and every YAML parser fails.
//
// The trigger-phrase cases specifically exercise the combination that
// tripped up an earlier draft: backslashes and double quotes inside
// phrases wrapped by the template's visual delimiters. The outer scalar
// is double-quoted, so the delimiters themselves are literal characters
// (not a nested YAML scalar) — which means yamlDoubleQuoted's escape
// rules are the right ones to apply here. This test locks that in.
func TestSkillFrontmatterEscapesNarrativeQuotesAndNewlines(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("yamlsafe")
	apiSpec.Description = "First line.\nSecond line with a \"quoted\" term."
	outputDir := filepath.Join(t.TempDir(), "yamlsafe-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		Headline: `An "agent-native" CLI with \backslash and "quotes"`,
		TriggerPhrases: []string{
			`what's the "best" price`, // apostrophe + double quotes
			`path\to\file`,            // backslashes
			`use "quoted"`,            // double quotes
			`has\"mixed\"`,            // backslash + double quote combo
			`simple phrase`,           // baseline
		},
	}
	require.NoError(t, gen.Generate())

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)

	// Extract the frontmatter (between the two --- lines).
	require.True(t, strings.HasPrefix(content, "---\n"), "frontmatter should open with ---")
	end := strings.Index(content[4:], "\n---\n")
	require.NotEqual(t, -1, end, "frontmatter should close with ---")
	frontmatter := content[:4+end+5]

	// The frontmatter must be parseable YAML. Parse it and verify the
	// description round-trips the intended content.
	var parsed struct {
		Name         string `yaml:"name"`
		Description  string `yaml:"description"`
		ArgumentHint string `yaml:"argument-hint"`
	}
	body := strings.TrimSuffix(strings.TrimPrefix(frontmatter, "---\n"), "---\n")
	require.NoError(t, yaml.Unmarshal([]byte(body), &parsed),
		"frontmatter must be valid YAML; content was:\n%s", body)

	assert.Equal(t, "pp-yamlsafe", parsed.Name)
	assert.True(t, strings.Contains(parsed.Description, `An "agent-native" CLI`),
		"double quotes in headline should round-trip through YAML parse: got %q", parsed.Description)
	assert.True(t, strings.Contains(parsed.Description, `\backslash`),
		"backslashes in headline should round-trip through YAML parse: got %q", parsed.Description)
	// Every trigger phrase must round-trip verbatim. This is the one the
	// reviewer called out: backslash and double-quote combinations are the
	// most failure-prone shapes and must not require a patch each time we
	// touch the template.
	for _, want := range []string{
		`what's the "best" price`,
		`path\to\file`,
		`use "quoted"`,
		`has\"mixed\"`,
		`simple phrase`,
	} {
		assert.True(t, strings.Contains(parsed.Description, want),
			"trigger phrase %q should round-trip verbatim through YAML parse; got description: %q", want, parsed.Description)
	}
}

func TestSkillUsesExplicitDisplayNameForProse(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("producthunt")
	outputDir := filepath.Join(t.TempDir(), "producthunt-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{DisplayName: "Product Hunt"}
	require.NoError(t, gen.Generate())

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)

	assert.Contains(t, content, "# Product Hunt — Printing Press CLI")
	assert.Contains(t, content, `Printing Press CLI for Product Hunt.`)
	assert.NotContains(t, content, "# Producthunt — Printing Press CLI")
}

// TestSkillFrontmatterFallbackHandlesMultilineSpecDescription asserts that
// OpenAPI specs with multi-line info.description values don't break the
// YAML frontmatter in the narrative-absent fallback path.
func TestSkillFrontmatterFallbackHandlesMultilineSpecDescription(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("multiline")
	apiSpec.Description = "Line one of the description.\nLine two has more detail.\nLine three."
	outputDir := filepath.Join(t.TempDir(), "multiline-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)

	end := strings.Index(content[4:], "\n---\n")
	require.NotEqual(t, -1, end)
	body := strings.TrimSuffix(strings.TrimPrefix(content[:4+end+5], "---\n"), "---\n")

	var parsed struct {
		Description string `yaml:"description"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(body), &parsed),
		"frontmatter must be valid YAML even with multi-line spec description")
	assert.True(t, strings.HasPrefix(parsed.Description, "Printing Press CLI for Multiline"))
	// Multi-line description should be flattened by oneline helper.
	assert.False(t, strings.Contains(parsed.Description, "\n"),
		"description should not contain raw newlines after oneline flattening")
}

// TestSkillRendersAuthBranchPerType asserts the deterministic Auth Setup
// block branches correctly on .Auth.Type when no narrative auth is provided.
func TestSkillRendersAuthBranchPerType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		authType string
		expect   string
	}{
		{"api_key", "api_key", "export"},
		{"oauth2", "oauth2", "auth login"},
		{"bearer_token", "bearer_token", "auth set-token"},
		{"cookie", "cookie", "auth login --chrome"},
		{"none", "none", "No authentication required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			apiSpec := minimalSpec("auth" + tc.name)
			apiSpec.Auth = spec.AuthConfig{
				Type:    tc.authType,
				EnvVars: []string{"AUTH_KEY"},
			}
			outputDir := filepath.Join(t.TempDir(), "auth"+tc.name+"-pp-cli")
			gen := New(apiSpec, outputDir)
			require.NoError(t, gen.Generate())

			skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
			require.NoError(t, err)
			content := string(skill)

			assert.True(t, strings.Contains(content, tc.expect),
				"auth-type %q should produce %q in SKILL.md Auth Setup", tc.authType, tc.expect)
		})
	}
}

// TestSkillRendersExtraCommands asserts that hand-written commands declared
// in spec.ExtraCommands appear in the generated SKILL.md Command Reference,
// after the spec-driven resources, with binary prefix and optional args.
// This closes the drift class where SKILL.md silently omitted hand-written
// commands like `today`, `streak`, `rivals` because the template only iterated
// .Resources.
func TestSkillRendersExtraCommands(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("sports")
	apiSpec.ExtraCommands = []spec.ExtraCommand{
		{Name: "trending", Description: "Most-followed athletes and teams across all leagues"},
		{Name: "boxscore", Description: "Full box score for an event", Args: "<event_id>"},
		{Name: "h2h", Description: "Head-to-head detail between two teams", Args: "<team1> <team2>"},
	}
	outputDir := filepath.Join(t.TempDir(), "sports-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)

	assert.Contains(t, content, "**Hand-written commands**",
		"Command Reference should include a Hand-written commands subsection when ExtraCommands present")
	assert.Contains(t, content, "`sports-pp-cli trending`",
		"extra command without args should render as just binary + name")
	assert.Contains(t, content, "`sports-pp-cli boxscore <event_id>`",
		"extra command with args should render args after the name")
	assert.Contains(t, content, "Most-followed athletes and teams across all leagues",
		"extra command description should appear in the rendered output")
	assert.Contains(t, content, "`sports-pp-cli h2h <team1> <team2>`",
		"extra command with multi-arg signature should render verbatim")
}

// TestSkillNoExtraCommandsIsBackwardCompatible asserts the template emits
// no Hand-written commands subsection when ExtraCommands is absent. This
// preserves the rendering of every existing CLI that has no extra_commands
// declaration in its spec.yaml.
func TestSkillNoExtraCommandsIsBackwardCompatible(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("plain")
	require.Empty(t, apiSpec.ExtraCommands)
	outputDir := filepath.Join(t.TempDir(), "plain-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)

	assert.NotContains(t, content, "**Hand-written commands**",
		"Hand-written commands subsection should not appear when ExtraCommands is absent")
}
