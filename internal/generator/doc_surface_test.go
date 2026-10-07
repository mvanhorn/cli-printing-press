package generator

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDocCommandGroupsSkipSpecAndFrameworkPaths(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("docs")
	apiSpec.Learn.Enabled = true
	apiSpec.ExtraCommands = []spec.ExtraCommand{
		{Name: "ads get", Description: "Fetch an ad", Args: "<id>"},
		{Name: "items", Description: "collides with the spec resource"},
	}
	gen := New(apiSpec, t.TempDir())
	gen.NovelFeatures = []NovelFeature{
		{Command: "recall", Description: "framework verb"},
		{Command: "items list", Description: "spec endpoint"},
		{Command: "market stats", Name: "Market stats", Description: "Summarize a market"},
		{Command: "watch add <id>", Description: "Track an id"},
	}

	groups := gen.additionalCommandGroups()
	got := map[string]listedDocCommand{}
	for _, group := range groups {
		for _, cmd := range group.Commands {
			got[cmd.Invocation] = cmd
		}
	}
	assert.Equal(t, "Summarize a market", got["market stats"].Description)
	assert.Equal(t, "Track an id", got["watch add <id>"].Description)
	assert.Equal(t, "Fetch an ad", got["ads get <id>"].Description)
	_, hasRecall := got["recall"]
	assert.False(t, hasRecall)
	_, hasItems := got["items"]
	assert.False(t, hasItems)
	_, hasItemsList := got["items list"]
	assert.False(t, hasItemsList)

	ref := gen.referenceCommandGroups()
	for _, group := range ref {
		for _, cmd := range group.Commands {
			assert.NotEqual(t, "ads get <id>", cmd.Invocation)
		}
	}
}

func TestCommandsFromNovelHooksReadsStaticRegistrations(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "novel_register.go"), []byte(novelHookFixture), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "novel_register_test.go"), []byte(`package cli

func init() {
	registerNovelCommand(func(root *cobra.Command) {
		root.AddCommand(&cobra.Command{Use: "fromtest", Short: "test-only"})
	})
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "broken.go"), []byte("package cli\nfunc registerNovelCommand(\n"), 0o644))

	got := map[string]string{}
	for _, cmd := range commandsFromNovelHooks(dir) {
		got[cmd.invocation] = cmd.description
	}
	assert.Equal(t, "new-top-level", got["standalone"])
	assert.Equal(t, "hand-authored", got["call"])
	assert.Equal(t, "hook-child", got["batch extra"])
	assert.Equal(t, "Watch", got["watch"])
	assert.Equal(t, "Add a watch", got["watch add <id>"])
	assert.Equal(t, "named-hook", got["reports run"])
	_, hasDyn := got["dyn"]
	assert.False(t, hasDyn)
	_, hasAds := got["ads"]
	assert.False(t, hasAds)
	_, hasFromTest := got["fromtest"]
	assert.False(t, hasFromTest)
	assert.Empty(t, commandsFromNovelHooks(""))
}

const novelHookFixture = `package cli

import "github.com/spf13/cobra"

func init() {
	registerNovelCommand(registerReports)
	registerNovelCommand(func(root *cobra.Command) {
		addNovelCommandIfAbsent(root, &cobra.Command{Use: "standalone", Short: "new-top-level"})
		root.AddCommand(&cobra.Command{Use: "call", Short: "hand-authored"})
		parent, _, err := root.Find([]string{"batch"})
		if err != nil {
			return
		}
		addNovelCommandIfAbsent(parent, &cobra.Command{Use: "extra", Short: "hook-child"})
		cmd := &cobra.Command{Use: "watch", Short: "Watch"}
		cmd.AddCommand(&cobra.Command{Use: "add <id>", Short: "Add a watch"})
		root.AddCommand(cmd)
		root.AddCommand(newAdsGetCmd())
		for _, name := range []string{"dyn"} {
			root.AddCommand(&cobra.Command{Use: name, Short: "dynamic"})
		}
	})
}

func registerReports(root *cobra.Command) {
	var run = &cobra.Command{Use: "run", Short: "named-hook"}
	parent, _, err := root.Find([]string{"reports"})
	if err != nil {
		return
	}
	parent.AddCommand(run)
}
`

func TestHookCommandPathsRespectBlockScope(t *testing.T) {
	t.Parallel()

	commands := hookCommandsForTest(t, `package cli

func init() {
	registerNovelCommand(func(root *cobra.Command) {
		parent, _, err := root.Find([]string{"items"})
		if err != nil {
			return
		}
		{
			parent := &cobra.Command{Use: "tools", Short: "Tools"}
			root.AddCommand(parent)
		}
		parent.AddCommand(&cobra.Command{Use: "extra", Short: "Extra"})
	})
}
`)
	assert.Equal(t, []string{"tools", "items extra"}, hookPaths(commands))
}

func TestHookCommandPathsStayUncertainAcrossBranches(t *testing.T) {
	t.Parallel()

	commands := hookCommandsForTest(t, `package cli

func init() {
	registerNovelCommand(func(root *cobra.Command) {
		root.AddCommand(&cobra.Command{Use: "kept", Short: "Kept"})
		parent, _, err := root.Find([]string{"items"})
		if err != nil {
			return
		}
		if true {
			parent, _, err = root.Find([]string{"tools"})
		}
		parent.AddCommand(&cobra.Command{Use: "extra", Short: "Extra"})
	})
}
`)
	assert.Equal(t, []string{"kept"}, hookPaths(commands))

	agreed := hookCommandsForTest(t, `package cli

func init() {
	registerNovelCommand(func(root *cobra.Command) {
		parent, _, err := root.Find([]string{"items"})
		if err != nil {
			return
		}
		if true {
			parent, _, err = root.Find([]string{"items", "sub"})
		} else {
			parent, _, err = root.Find([]string{"items", "sub"})
		}
		parent.AddCommand(&cobra.Command{Use: "extra", Short: "Extra"})
	})
}
`)
	assert.Equal(t, []string{"items sub extra"}, hookPaths(agreed))
}

func TestPreservedCLIDirSuppliesHookCommands(t *testing.T) {
	t.Parallel()

	preserved := t.TempDir()
	cliDir := filepath.Join(preserved, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "notes.go"), []byte(`package cli

func init() {
	registerNovelCommand(func(root *cobra.Command) {
		root.AddCommand(&cobra.Command{Use: "notes", Short: "Operator notes"})
	})
}
`), 0o644))

	gen := New(minimalSpec("preserved"), t.TempDir())
	gen.PreservedCLIDir = preserved
	var invocations []string
	for _, group := range gen.additionalCommandGroups() {
		for _, cmd := range group.Commands {
			invocations = append(invocations, cmd.Invocation)
		}
	}
	assert.Equal(t, []string{"notes"}, invocations)
}

func hookCommandsForTest(t *testing.T, src string) []hookCommand {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "hook.go", src, parser.SkipObjectResolution)
	require.NoError(t, err)
	return novelHookCommands(file)
}

func hookPaths(commands []hookCommand) []string {
	out := make([]string, 0, len(commands))
	for _, cmd := range commands {
		out = append(out, cmd.path)
	}
	return out
}

func TestGeneratedDocsDescribeCommandSurfaceAndAuthShape(t *testing.T) {
	t.Parallel()

	t.Run("no-auth with listed alternatives", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("noauthdocs")
		apiSpec.Auth = spec.AuthConfig{Type: "none"}
		apiSpec.ExtraCommands = []spec.ExtraCommand{
			{Name: "ads get", Description: "Fetch an ad"},
		}
		outputDir := filepath.Join(t.TempDir(), "noauthdocs-pp-cli")
		gen := New(apiSpec, outputDir)
		gen.ListedAlternatives = true
		gen.NovelFeatures = []NovelFeature{
			{Name: "Market stats", Command: "market stats", Description: "Summarize a market"},
			{Name: "Watch add", Command: "watch add <id>", Description: "Track an id"},
			{Name: "Watch list", Command: "watch list", Description: "List tracked ids"},
		}
		require.NoError(t, gen.Generate())

		readme := readGeneratedDoc(t, outputDir, "README.md")
		skill := readGeneratedDoc(t, outputDir, "SKILL.md")
		commands := markdownSection(readme, "## Commands")
		reference := markdownSection(skill, "## Command Reference")
		extensions := markdownSection(skill, "## Hand-written Extensions")

		for _, invocation := range []string{
			"noauthdocs-pp-cli market stats",
			"noauthdocs-pp-cli watch add <id>",
			"noauthdocs-pp-cli watch list",
			"noauthdocs-pp-cli ads get",
		} {
			assert.Contains(t, commands, invocation)
		}
		assert.Contains(t, reference, "noauthdocs-pp-cli market stats")
		assert.Contains(t, reference, "noauthdocs-pp-cli watch add <id>")
		assert.NotContains(t, reference, "ads get")
		assert.Contains(t, extensions, "noauthdocs-pp-cli ads get")
		assert.NotContains(t, readme, NovelFeatureExclusivityClaim)
		assert.NotContains(t, skill, NovelFeatureExclusivityClaim)
		for _, phrase := range []string{"credentials.toml", "cookies", "auth sidecars", "first auth write"} {
			assert.NotContains(t, skill, phrase)
		}
		assert.Contains(t, skill, "durable local data such as `data.db`")
		assert.Contains(t, skill, "will not find files left under the former root")
	})

	t.Run("sources alone suppress the exclusivity claim", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("sourcedocs")
		outputDir := filepath.Join(t.TempDir(), "sourcedocs-pp-cli")
		gen := New(apiSpec, outputDir)
		gen.Sources = []ReadmeSource{{Name: "other-tool", URL: "https://example.com/other-tool"}}
		gen.NovelFeatures = []NovelFeature{
			{Name: "Market stats", Command: "market stats", Description: "Summarize a market"},
		}
		require.NoError(t, gen.Generate())

		readme := readGeneratedDoc(t, outputDir, "README.md")
		skill := readGeneratedDoc(t, outputDir, "SKILL.md")
		assert.NotContains(t, readme, NovelFeatureExclusivityClaim)
		assert.NotContains(t, skill, NovelFeatureExclusivityClaim)
		assert.Contains(t, markdownSection(readme, "## Commands"), "sourcedocs-pp-cli market stats")
		assert.Contains(t, skill, "credentials.toml")
		assert.Contains(t, skill, "first auth write")
	})

	t.Run("auth cli keeps credential prose and the exclusivity claim", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("authdocs")
		outputDir := filepath.Join(t.TempDir(), "authdocs-pp-cli")
		gen := New(apiSpec, outputDir)
		gen.NovelFeatures = []NovelFeature{
			{Name: "Market stats", Command: "market stats", Description: "Summarize a market"},
		}
		require.NoError(t, gen.Generate())

		readme := readGeneratedDoc(t, outputDir, "README.md")
		skill := readGeneratedDoc(t, outputDir, "SKILL.md")
		assert.Contains(t, readme, NovelFeatureExclusivityClaim)
		assert.Contains(t, readme, "- **`market stats`** — Summarize a market")
		assert.Contains(t, skill, NovelFeatureExclusivityClaim)
		assert.Contains(t, markdownSection(readme, "## Commands"), "authdocs-pp-cli market stats")
		assert.Contains(t, markdownSection(skill, "## Command Reference"), "authdocs-pp-cli market stats")
		assert.Contains(t, skill, "`data` contains `credentials.toml`, `data.db`, cookies, and auth sidecars.")
		assert.Contains(t, skill, "Stored secrets live in `credentials.toml` under the data dir. Existing legacy `config.toml` secrets are read for compatibility and leave `config.toml` on the first auth write.")
		assert.Contains(t, skill, "will not find credentials left under the former root")
	})
}

func TestGeneratedDocsIncludeHookRegisteredCommands(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hookdocs")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	outputDir := filepath.Join(t.TempDir(), "hookdocs-pp-cli")
	require.NoError(t, os.MkdirAll(filepath.Join(outputDir, "internal", "cli"), 0o755))
	cliDir := filepath.Join(outputDir, "internal", "cli")
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "novel_register.go"), []byte(novelHookFixture), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "novel_register_test.go"), []byte(`package cli

func init() {
	registerNovelCommand(func(root *cobra.Command) {
		root.AddCommand(&cobra.Command{Use: "fromtest", Short: "test-only"})
	})
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "broken.go"), []byte("package cli\nfunc registerNovelCommand(\n"), 0o644))

	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{Name: "Reports run", Command: "reports run", Description: "Novel reports"},
	}
	require.NoError(t, gen.Generate())

	readme := readGeneratedDoc(t, outputDir, "README.md")
	skill := readGeneratedDoc(t, outputDir, "SKILL.md")
	commands := markdownSection(readme, "## Commands")
	reference := markdownSection(skill, "## Command Reference")

	assert.Equal(t, 1, strings.Count(commands, "hookdocs-pp-cli reports run"))
	assert.Contains(t, commands, "Novel reports")
	assert.NotContains(t, commands, "named-hook")
	assert.Contains(t, commands, "hookdocs-pp-cli batch extra")
	assert.Contains(t, commands, "hookdocs-pp-cli watch add <id>")
	assert.Contains(t, reference, "hookdocs-pp-cli reports run")
	assert.Contains(t, reference, "hookdocs-pp-cli batch extra")
	assert.NotContains(t, commands, "hookdocs-pp-cli ads")
	assert.NotContains(t, commands, "hookdocs-pp-cli dyn")
	assert.NotContains(t, commands, "hookdocs-pp-cli fromtest")
}

func readGeneratedDoc(t *testing.T, outputDir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outputDir, name))
	require.NoError(t, err)
	return string(data)
}

func markdownSection(content, heading string) string {
	token := "\n" + heading + "\n"
	start := strings.Index(content, token)
	if start < 0 {
		if strings.HasPrefix(content, heading+"\n") {
			start = 0
			token = heading + "\n"
		} else {
			return ""
		}
	}
	rest := content[start+len(token):]
	if before, _, found := strings.Cut(rest, "\n## "); found {
		return before
	}
	return rest
}
