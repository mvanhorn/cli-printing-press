package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
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
	assert.True(t, strings.Contains(content, "library/commerce/finance/cmd/finance-pp-cli"),
		"openclaw install manifest should use the API's category and slug-only directory")

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

	// Installation — CLI install lives at the top under Prerequisites
	// so agents read it before deciding to run a command. MCP install
	// stays in its existing location.
	assert.True(t, strings.Contains(content, "## Prerequisites: Install the CLI"),
		"SKILL should include Prerequisites section near the top so agents install the CLI before invoking commands")
	assert.True(t, strings.Contains(content, "## MCP Server Installation"),
		"SKILL should include MCP install instructions")
	// Sanity: Prerequisites must precede first command-reference section,
	// not be buried near the bottom (where the previous "## CLI Installation"
	// section lived — too far down for agents to read top-down).
	prereqIdx := strings.Index(content, "## Prerequisites: Install the CLI")
	cmdRefIdx := strings.Index(content, "## Command Reference")
	require.GreaterOrEqual(t, prereqIdx, 0)
	require.GreaterOrEqual(t, cmdRefIdx, 0)
	assert.Less(t, prereqIdx, cmdRefIdx,
		"Prerequisites must appear before Command Reference so agents read install instructions before deciding to run a command")
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

// TestSkillFrontmatterMetadataIsClawHubCompliantNestedYAML asserts that the
// emitted metadata block parses as nested YAML conforming to ClawHub's
// SkillInstallSpec schema (kind: go, module:, no kind: shell, no command:,
// no id:, no label:). The shape was verified directly against
// packages/schema/src/schemas.ts in the openclaw/clawhub repo.
func TestSkillFrontmatterMetadataIsClawHubCompliantNestedYAML(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("widget")
	apiSpec.Category = "commerce"
	outputDir := filepath.Join(t.TempDir(), "widget-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)

	// Extract frontmatter and parse as YAML.
	require.True(t, strings.HasPrefix(content, "---\n"))
	end := strings.Index(content[4:], "\n---\n")
	require.NotEqual(t, -1, end)
	body := strings.TrimSuffix(strings.TrimPrefix(content[:4+end+5], "---\n"), "---\n")

	var parsed struct {
		Metadata struct {
			Openclaw struct {
				Requires struct {
					Bins []string `yaml:"bins"`
				} `yaml:"requires"`
				Install []struct {
					Kind   string   `yaml:"kind"`
					Bins   []string `yaml:"bins"`
					Module string   `yaml:"module"`
					// Fields that MUST NOT appear:
					Command string `yaml:"command"`
					ID      string `yaml:"id"`
					Label   string `yaml:"label"`
				} `yaml:"install"`
			} `yaml:"openclaw"`
		} `yaml:"metadata"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(body), &parsed),
		"frontmatter must parse as nested YAML; content was:\n%s", body)

	// Schema-compliance assertions.
	assert.Equal(t, []string{"widget-pp-cli"}, parsed.Metadata.Openclaw.Requires.Bins,
		"requires.bins should carry the CLI binary name")
	require.Len(t, parsed.Metadata.Openclaw.Install, 1, "exactly one install entry expected")
	entry := parsed.Metadata.Openclaw.Install[0]
	assert.Equal(t, "go", entry.Kind, "kind must be 'go' (ClawHub schema enum: brew|node|go|uv)")
	assert.Equal(t, []string{"widget-pp-cli"}, entry.Bins)
	assert.Equal(t,
		"github.com/mvanhorn/printing-press-library/library/commerce/widget/cmd/widget-pp-cli",
		entry.Module,
		"module must be the slug-only directory path matching the published library convention; cmd subdir uses the binary name")
	assert.Empty(t, entry.Command, "command field must not be emitted (not in ClawHub schema)")
	assert.Empty(t, entry.ID, "id field must not be emitted (optional, no semantic value here)")
	assert.Empty(t, entry.Label, "label field must not be emitted (optional, no semantic value here)")

	// Negative assertions on raw text — catch any regression to the old shape.
	assert.NotContains(t, content, `kind: shell`,
		"kind: shell is invalid per ClawHub schema; must never appear")
	assert.NotContains(t, content, `kind: "shell"`,
		"kind: shell is invalid per ClawHub schema; must never appear")
	assert.NotContains(t, content, `"command":`,
		"command field is not in ClawHub schema; must never appear in metadata")
	assert.NotContains(t, content, `\"openclaw\":`,
		"metadata must not be a JSON-string blob anymore")
}

// TestGenerateSoftFallsBackOnEmptyOwnerName asserts the empty-OwnerName
// path doesn't fail generation. When OwnerName is unset and the resolution
// chain returns empty (e.g., CI without git config), Generate() falls back
// to the slug-shaped Owner — non-fatal so the generator package stays
// reusable by tests, mcp-sync, and regen-merge. The library-wide sweep
// tool overrides this code path with its own per-CLI authorship mapping;
// this fallback only fires for fresh prints by users who haven't set git
// config user.name.
func TestGenerateSoftFallsBackOnEmptyOwnerName(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("ownerless")
	apiSpec.Owner = "trevin-chow"
	apiSpec.OwnerName = ""
	outputDir := filepath.Join(t.TempDir(), "ownerless-pp-cli")
	gen := New(apiSpec, outputDir)

	// Stub the resolution path's git config lookup by ensuring the
	// outputDir has no .printing-press.json — readManifestOwnerName
	// returns "", and resolveOwnerNameForNew runs `git config user.name`.
	// In the test environment this may resolve to whatever the runner
	// has set, so we re-clear the field after New() to deterministically
	// hit the fallback path.
	apiSpec.OwnerName = ""

	// Generation must not error on the empty-OwnerName path.
	require.NoError(t, gen.Generate())

	// After Generate(), the soft-fallback should have populated
	// OwnerName from the slug.
	assert.Equal(t, "trevin-chow", apiSpec.OwnerName,
		"soft-fallback should set OwnerName to the slug-shaped Owner when empty")

	// And the rendered SKILL.md should reflect that — author lands as
	// the slug, ugly but visible (vs. a hard-error blocking generation).
	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	assert.Contains(t, string(skill), `author: "trevin-chow"`,
		"author field should fall back to the slug rather than be empty")
}

// TestSkillFrontmatterEmitsHermesTopLevelFields asserts the post-alignment
// frontmatter carries the Hermes-recognized top-level fields (`version`,
// `author`, `license`) so Hermes can install the skill. The OpenClaw block
// continues to coexist alongside; Hermes ignores unknown keys per its docs.
func TestSkillFrontmatterEmitsHermesTopLevelFields(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hermes-test")
	apiSpec.OwnerName = "Trevin Chow"
	outputDir := filepath.Join(t.TempDir(), "hermes-test-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)

	require.True(t, strings.HasPrefix(content, "---\n"))
	end := strings.Index(content[4:], "\n---\n")
	require.NotEqual(t, -1, end)
	body := strings.TrimSuffix(strings.TrimPrefix(content[:4+end+5], "---\n"), "---\n")

	var parsed struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Version     string `yaml:"version"`
		Author      string `yaml:"author"`
		License     string `yaml:"license"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(body), &parsed),
		"frontmatter must parse as nested YAML; content was:\n%s", body)

	assert.Equal(t, "Trevin Chow", parsed.Author,
		"author must be the prose-shaped OwnerName, not the slug — yamlDoubleQuoted preserves spaces and casing")
	assert.Equal(t, "Apache-2.0", parsed.License,
		"license is a constant for printed CLIs (LICENSE.tmpl is Apache 2.0)")
	assert.NotEmpty(t, parsed.Version,
		"version must be populated from .printing-press.json or version.Version fallback")
}

// TestSkillFrontmatterOmitsAllEnvVarDeclarations asserts the post-Hermes-
// alignment shape: neither OpenClaw `requires.env` nor `envVars`, nor the
// legacy `primaryEnv`, appears in printed-CLI SKILL.md frontmatter. The
// classification problem (user-set vs harvested) is asymmetric on failure
// — a false-positive on a harvested var (e.g., a session cookie) prompts
// the user for a value the CLI can't accept. v1 ships no env-var
// declarations in either format; the existing auth.Type-branched README
// content carries credential UX.
func TestSkillFrontmatterOmitsAllEnvVarDeclarations(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("clawauth")
	apiSpec.Auth = spec.AuthConfig{
		Type:   "bearer_token",
		Header: "Authorization",
		Format: "Bearer {token}",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "CLAW_API_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true, Description: "API token."},
			{Name: "CLAW_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false, Description: "OAuth client id."},
			{Name: "CLAW_SESSION_COOKIE", Kind: spec.AuthEnvVarKindHarvested, Required: false, Sensitive: true, Description: "Harvested session cookie."},
		},
	}
	outputDir := filepath.Join(t.TempDir(), "clawauth-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)

	require.True(t, strings.HasPrefix(content, "---\n"))
	end := strings.Index(content[4:], "\n---\n")
	require.NotEqual(t, -1, end)
	body := strings.TrimSuffix(strings.TrimPrefix(content[:4+end+5], "---\n"), "---\n")

	// None of the env-var declaration shapes should appear anywhere in
	// the frontmatter — neither the canonical nor the legacy forms.
	assert.NotContains(t, body, "envVars:", "envVars block must not appear in v1 frontmatter")
	assert.NotContains(t, body, "      env:", "requires.env line must not appear in v1 frontmatter")
	assert.NotContains(t, body, "primaryEnv", "primaryEnv (legacy synthesis-shape) must not appear")

	// And specifically: no env-var name from the spec leaks into the
	// frontmatter even when EnvVarSpecs is fully populated.
	for _, name := range []string{"CLAW_API_TOKEN", "CLAW_CLIENT_ID", "CLAW_SESSION_COOKIE"} {
		assert.NotContains(t, body, name,
			"env var %q must not appear in v1 frontmatter (no env-var hoisting)", name)
	}
}

// TestSkillFrontmatterMetadataDefaultsCategoryToOther asserts that when the
// spec has no Category set, the install module path falls back to 'other'.
func TestSkillFrontmatterMetadataDefaultsCategoryToOther(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("uncategorized")
	apiSpec.Category = ""
	outputDir := filepath.Join(t.TempDir(), "uncategorized-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	content := string(skill)

	assert.Contains(t, content,
		"module: github.com/mvanhorn/printing-press-library/library/other/uncategorized/cmd/uncategorized-pp-cli",
		"empty Category should default to 'other' in install module path")
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
