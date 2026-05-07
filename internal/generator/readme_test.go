package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeneratedREADMEHasNoPlaceholderMarkers asserts that no <!-- *_OUTPUT -->
// HTML-comment markers ship in the rendered README. These markers were left
// over from an abandoned post-generate augmentation flow; the machine never
// populated them, so they leaked into every printed CLI as visible artifacts.
// Regression guard: if anyone re-introduces a marker without wiring up a
// fill path, this test fails.
func TestGeneratedREADMEHasNoPlaceholderMarkers(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "markerless",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"MARKERLESS_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/markerless-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/items",
						Description: "List items",
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "markerless-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	for _, marker := range []string{
		"<!-- HELP_OUTPUT -->",
		"<!-- DOCTOR_OUTPUT -->",
		"<!-- VERSION_OUTPUT -->",
	} {
		assert.False(t, strings.Contains(content, marker),
			"rendered README still contains placeholder marker %q — no machine code replaces it", marker)
	}
}

// TestGeneratedREADMEHasNoHallucinatedCookbook asserts that the printed
// README does not advertise commands that the CLI may not implement. The
// old ## Cookbook block hard-coded sync/search/export examples that most
// specs don't produce; removing it prevents users from trying commands
// that error out.
func TestGeneratedREADMEHasNoHallucinatedCookbook(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "cookbookless",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"COOKBOOKLESS_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/cookbookless-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "cookbookless-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	assert.False(t, strings.Contains(content, "## Cookbook"),
		"README should not include a Cookbook section (hard-coded sync/search/export commands are hallucinated for most specs)")
	assert.False(t, strings.Contains(content, "cookbookless-pp-cli sync"),
		"README should not reference an unimplemented sync command")
	assert.False(t, strings.Contains(content, "cookbookless-pp-cli export --format jsonl"),
		"README should not reference an unimplemented export command")
}

func TestReadOnlyNoAuthReadmeSuppressesCrudAuthBoilerplate(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("readonly")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Read items",
		Endpoints: map[string]spec.Endpoint{
			"list": {Method: "GET", Path: "/items", Description: "List items"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "readonly-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	assert.Contains(t, content, "Read-only by default")
	assert.NotContains(t, content, "creates return \"already exists\"")
	assert.NotContains(t, content, "create --stdin")
	assert.NotContains(t, content, "Authentication errors (exit code 4)")
	assert.NotContains(t, content, "`4` auth error")
	assert.Contains(t, content, "`7` rate limited")
}

// TestReadmeHandlesEmptyButPresentNarrative asserts that a non-nil but
// fully-empty ReadmeNarrative doesn't cause dangling headers, broken
// sections, or nil-slice panics. The absorb LLM can legitimately return
// {"narrative": {}} when it has no confident input — template must treat
// "present but empty" the same as "absent" on a per-field basis.
func TestReadmeHandlesEmptyButPresentNarrative(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("emptynarr")
	outputDir := filepath.Join(t.TempDir(), "emptynarr-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{} // all fields zero
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	// No dangling section headers from empty fields.
	assert.False(t, strings.Contains(content, "## Authentication\n\n##"),
		"empty AuthNarrative should not emit a dangling Authentication header")
	assert.False(t, strings.Contains(content, "### API-specific"),
		"empty Troubleshoots should not emit the API-specific subheading")
	// Falls back to .Description since Headline is empty.
	assert.True(t, strings.Contains(content, apiSpec.Description) ||
		strings.Contains(content, "# Emptynarr CLI"),
		"empty Headline should fall back to description/title")
}

// TestReadmeHandlesMarkdownUnsafeNarrativeFields asserts that narrative
// text containing markdown metacharacters doesn't break the rendered
// README. Headlines are wrapped in **bold**; a ** inside collapses it.
// WhyItMatters is wrapped in _italic_; an _ inside collapses it. Example
// code goes inside a fenced block; ``` inside closes the fence early.
func TestReadmeHandlesMarkdownUnsafeNarrativeFields(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mdsafe")
	outputDir := filepath.Join(t.TempDir(), "mdsafe-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Command:      "foo",
			Description:  "Does foo",
			Example:      "mdsafe-pp-cli foo",
			WhyItMatters: "Good for workflows",
		},
	}
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	// Spot-check: no stray triple-backticks beyond the code fences we expect.
	// Each fenced block is one opening ``` + one closing ```. Count must be even.
	fenceCount := strings.Count(content, "```")
	assert.Equal(t, 0, fenceCount%2,
		"every fenced code block must be closed; odd fence count means an unescaped ``` broke the markdown")
}

func minimalSpec(name string) *spec.APISpec {
	return &spec.APISpec{
		Name:      name,
		Version:   "0.1.0",
		BaseURL:   "https://api.example.com",
		Owner:     "test-owner",
		OwnerName: "Test Author",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"MYAPI_TOKEN"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/" + name + "-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}
}

// TestReadmeRendersNarrativeHeadlineAndValueProp asserts the template uses
// the LLM-authored headline + value prop when Narrative is populated,
// pushing past the generic spec-derived description.
func TestReadmeRendersNarrativeHeadlineAndValueProp(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("narrated")
	outputDir := filepath.Join(t.TempDir(), "narrated-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		Headline:  "Every feature plus a local store nothing else has",
		ValueProp: "Quotes, fundamentals, and a SQLite-backed portfolio tracker.",
	}
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	assert.True(t, strings.Contains(content, "**Every feature plus a local store nothing else has**"),
		"headline should render as bold text")
	assert.True(t, strings.Contains(content, "Quotes, fundamentals, and a SQLite-backed portfolio tracker."),
		"value prop should render as a paragraph")
}

func TestReadmeUsesExplicitDisplayNameForProse(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("producthunt")
	outputDir := filepath.Join(t.TempDir(), "producthunt-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{DisplayName: "Product Hunt"}
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	assert.Contains(t, content, "# Product Hunt CLI")
	assert.NotContains(t, content, "# Producthunt CLI")
}

// TestReadmeEmitsHermesAndOpenClawInstallSections asserts the new install
// sections render with the correct hardcoded mvanhorn paths and the
// hermes-install anchor for sweep-tool idempotency. CLI form and chat form
// both use mvanhorn/printing-press-library/cli-skills/pp-<api> (verified
// against tested install behavior — earlier draft of the chat form used a
// shorter mvanhorn/cli-skills path that doesn't resolve).
func TestReadmeEmitsHermesAndOpenClawInstallSections(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hermes-install")
	outputDir := filepath.Join(t.TempDir(), "hermes-install-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	// Anchor enables the cross-repo sweep tool to insert sections
	// idempotently into legacy READMEs that predate this template change.
	assert.Contains(t, content, "<!-- pp-hermes-install-anchor -->",
		"sweep-tool anchor must be present so retrofit can locate the insertion point")

	// Hermes section: both forms (CLI + chat) use the full
	// mvanhorn/printing-press-library/cli-skills path.
	assert.Contains(t, content, "## Install for Hermes")
	assert.Contains(t, content, "hermes skills install mvanhorn/printing-press-library/cli-skills/pp-hermes-install --force",
		"Hermes CLI form must use mvanhorn/printing-press-library/cli-skills (the short mvanhorn/cli-skills form was wrong)")
	assert.Contains(t, content, "/skills install mvanhorn/printing-press-library/cli-skills/pp-hermes-install --force",
		"Hermes chat form must use mvanhorn/printing-press-library/cli-skills")

	// OpenClaw section: copyable code-fenced agent instruction.
	assert.Contains(t, content, "## Install for OpenClaw")
	assert.Contains(t, content, "https://github.com/mvanhorn/printing-press-library/tree/main/cli-skills/pp-hermes-install",
		"OpenClaw URL must point at the cli-skills directory")
}

// TestReadmeFallsBackWhenNarrativeAbsent asserts the generic description
// is used when Narrative is nil — no breakage for specs without absorb data.
func TestReadmeFallsBackWhenNarrativeAbsent(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("plain")
	apiSpec.Description = "A basic example API."
	outputDir := filepath.Join(t.TempDir(), "plain-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	assert.True(t, strings.Contains(content, "A basic example API."),
		"falls back to .Description when no narrative is present")
}

// TestReadmeRendersNovelFeaturesGrouped asserts the Unique Features block
// switches to group subheadings when any feature carries a Group value.
// Also verifies Example and WhyItMatters render beneath each feature.
func TestReadmeRendersNovelFeaturesGrouped(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("grouped")
	outputDir := filepath.Join(t.TempDir(), "grouped-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Command:      "portfolio perf",
			Description:  "Show unrealized P&L",
			Example:      "grouped-pp-cli portfolio perf --agent",
			WhyItMatters: "Agents answer portfolio questions in one call",
			Group:        "Local state that compounds",
		},
		{
			Command:     "watchlist show",
			Description: "Render a watchlist",
			Group:       "Local state that compounds",
		},
		{
			Command:      "auth login-chrome",
			Description:  "Import a Chrome session when rate-limited",
			WhyItMatters: "Unblocks CI IPs Yahoo has throttled",
			Group:        "Reachability mitigation",
		},
	}
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	assert.True(t, strings.Contains(content, "### Local state that compounds"),
		"grouped rendering should use group names as subheadings")
	assert.True(t, strings.Contains(content, "### Reachability mitigation"),
		"each distinct group should appear as its own subheading")
	assert.True(t, strings.Contains(content, "_Agents answer portfolio questions in one call_"),
		"WhyItMatters should render as italic text")
	assert.True(t, strings.Contains(content, "grouped-pp-cli portfolio perf --agent"),
		"Example should render as a code block")
}

// TestReadmeGroupsByCanonicalizedNameNotLiteralMatch asserts that novel
// features whose Group strings differ only by casing or whitespace are
// merged into a single rendered group. The LLM will drift — given five
// features in "Local state that compounds" it will usually emit at
// least one "Local State That Compounds" by accident. Without
// canonicalization these split into separate groups silently and a
// reader sees the grouping as broken.
func TestReadmeGroupsByCanonicalizedNameNotLiteralMatch(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("canon")
	outputDir := filepath.Join(t.TempDir(), "canon-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{Command: "a", Description: "alpha", Group: "Local state that compounds"},
		{Command: "b", Description: "bravo", Group: "local state that compounds"},    // lowercased
		{Command: "c", Description: "charlie", Group: "Local State That Compounds"},  // title case
		{Command: "d", Description: "delta", Group: "Local  state  that  compounds"}, // double spaces
	}
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	// Exactly one group heading should appear despite four casing variants.
	count := strings.Count(content, "### Local state that compounds") +
		strings.Count(content, "### local state that compounds") +
		strings.Count(content, "### Local State That Compounds") +
		strings.Count(content, "### Local  state  that  compounds")
	assert.Equal(t, 1, count,
		"canonicalized group names should merge into exactly one subheading; got %d variant headings", count)

	// All four features should appear under that single heading.
	for _, cmd := range []string{"`a`", "`b`", "`c`", "`d`"} {
		assert.True(t, strings.Contains(content, cmd),
			"feature %s should appear under the merged group", cmd)
	}

	// The first-seen display form should win (matches the LLM's casing,
	// which is usually the most legible one for that group).
	assert.True(t, strings.Contains(content, "### Local state that compounds"),
		"first-seen casing should be used as display name; full output:\n%s", content)
}

// TestReadmeRendersNovelFeaturesFlat asserts that when no feature has a
// Group, the Unique Features block renders as a flat bullet list without
// any group subheadings.
func TestReadmeRendersNovelFeaturesFlat(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("flat")
	outputDir := filepath.Join(t.TempDir(), "flat-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{Command: "foo", Description: "Do foo"},
		{Command: "bar", Description: "Do bar"},
	}
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	assert.True(t, strings.Contains(content, "## Unique Features"),
		"Unique Features section should render")
	assert.True(t, strings.Contains(content, "- **`foo`** — Do foo"),
		"flat bullet should render")
	assert.False(t, strings.Contains(content, "### More"),
		"ungrouped features should not produce a 'More' subheading")
}

// TestReadmeRendersNarrativeQuickStart asserts the Quick Start section
// renders narrative.quickstart commands verbatim instead of the generic
// auth-branched flow when the narrative provides realistic steps.
func TestReadmeRendersNarrativeQuickStart(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("qstart")
	outputDir := filepath.Join(t.TempDir(), "qstart-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		QuickStart: []QuickStartStep{
			{Command: "qstart-pp-cli quote AAPL MSFT", Comment: "Get current quotes"},
			{Command: "qstart-pp-cli watchlist create tech", Comment: "Build a watchlist"},
		},
	}
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	assert.True(t, strings.Contains(content, "qstart-pp-cli quote AAPL MSFT"),
		"quickstart command should render verbatim")
	assert.True(t, strings.Contains(content, "# Get current quotes"),
		"quickstart comment should render as a bash comment above the command")
	// The generic "### 1. Install" numbered steps should NOT appear when
	// narrative quickstart takes over.
	assert.False(t, strings.Contains(content, "### 1. Install"),
		"generic numbered steps should be suppressed when narrative quickstart is present")
}

// TestReadmeAppendsNarrativeTroubleshoots asserts the Troubleshooting
// section appends API-specific symptom/fix pairs when provided.
func TestReadmeAppendsNarrativeTroubleshoots(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("trouble")
	outputDir := filepath.Join(t.TempDir(), "trouble-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		Troubleshoots: []TroubleshootTip{
			{Symptom: "HTTP 429 on every request", Fix: "Import a Chrome session via `auth login-chrome`"},
		},
	}
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	assert.True(t, strings.Contains(content, "### API-specific"),
		"API-specific troubleshoots subheading should render")
	assert.True(t, strings.Contains(content, "**HTTP 429 on every request**"),
		"troubleshoot symptom should render as bold")
	assert.True(t, strings.Contains(content, "Import a Chrome session via `auth login-chrome`"),
		"troubleshoot fix should render after the symptom")
}

// TestEmptyEnvVarsSectionHidden asserts the Environment variables subheader
// is not rendered when the spec has no env vars (e.g., cookie-based auth).
// Previously the header shipped with no bullets underneath — a dangling
// "Environment variables:" line followed by a blank paragraph.
func TestEmptyEnvVarsSectionHidden(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "noenvvars",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		// Cookie auth: no env vars configured.
		Auth: spec.AuthConfig{
			Type:    "cookie",
			EnvVars: nil,
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/noenvvars-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "noenvvars-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	assert.False(t, strings.Contains(content, "Environment variables:"),
		"README should not render an Environment variables header when .Auth.EnvVars is empty")
}

// TestOutputFormatsUsesRealCommandExample asserts the Output Formats block
// renders a resource+endpoint pair that actually exists in the spec. The
// previous template hard-coded `{firstResource} list`, which produced
// nonsense like "autocomplete list" when autocomplete had no list endpoint.
func TestOutputFormatsUsesRealCommandExample(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "realexample",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"REALEXAMPLE_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/realexample-pp-cli/config.toml",
		},
		// Intentionally: a resource whose only endpoint is NOT "list".
		// Previous template would have produced "autocomplete list"; the
		// fixed template should render "autocomplete get" instead.
		Resources: map[string]spec.Resource{
			"autocomplete": {
				Description: "Autocomplete",
				Endpoints: map[string]spec.Endpoint{
					"get": {Method: "GET", Path: "/autocomplete", Description: "Autocomplete symbols"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "realexample-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	assert.True(t, strings.Contains(content, "realexample-pp-cli autocomplete get"),
		"Output Formats should reference a real resource+endpoint pair from the spec")
	assert.False(t, strings.Contains(content, "realexample-pp-cli autocomplete list"),
		"Output Formats should not hallucinate a 'list' endpoint that doesn't exist in the spec")
}
