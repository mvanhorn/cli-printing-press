package docparse

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFixture creates a minimal CLI source tree for testing.
func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestParseCLI_SingleCommand(t *testing.T) {
	dir := writeFixture(t, map[string]string{
		"quote.go": `package cli

import "github.com/spf13/cobra"

func newQuoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quote",
		Short: "Get current quotes",
		Long:  "Fetch real-time quotes for one or more symbols.",
		Example: "mycli quote AAPL MSFT",
	}
}
`,
	})

	cmds, unparseable, err := ParseCLI(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(unparseable) != 0 {
		t.Errorf("unexpected unparseable files: %v", unparseable)
	}
	if len(cmds) != 1 {
		t.Fatalf("want 1 command, got %d: %+v", len(cmds), cmds)
	}
	c := cmds[0]
	if c.ConstructorName != "newQuoteCmd" {
		t.Errorf("ConstructorName = %q, want newQuoteCmd", c.ConstructorName)
	}
	if c.Use != "quote" {
		t.Errorf("Use = %q, want quote", c.Use)
	}
	if c.Short != "Get current quotes" {
		t.Errorf("Short = %q", c.Short)
	}
	if c.Long != "Fetch real-time quotes for one or more symbols." {
		t.Errorf("Long = %q", c.Long)
	}
	if c.Example != "mycli quote AAPL MSFT" {
		t.Errorf("Example = %q", c.Example)
	}
}

func TestParseCLI_MultipleCommandsWithGrouping(t *testing.T) {
	dir := writeFixture(t, map[string]string{
		"quote.go": `package cli

import "github.com/spf13/cobra"

func newQuoteCmd() *cobra.Command {
	return &cobra.Command{Use: "quote", Short: "Quote"}
}
`,
		"watchlist.go": `package cli

import "github.com/spf13/cobra"

func newWatchlistCmd() *cobra.Command {
	c := &cobra.Command{Use: "watchlist", Short: "Manage watchlists"}
	c.AddCommand(newWatchlistCreateCmd())
	c.AddCommand(newWatchlistAddCmd())
	return c
}

func newWatchlistCreateCmd() *cobra.Command {
	return &cobra.Command{Use: "create", Short: "Create a watchlist"}
}

func newWatchlistAddCmd() *cobra.Command {
	return &cobra.Command{Use: "add", Short: "Add symbols to a watchlist"}
}
`,
	})

	cmds, _, err := ParseCLI(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 4 {
		t.Fatalf("want 4 commands, got %d", len(cmds))
	}

	// Verify deterministic ordering (by file, then constructor name).
	for i := 0; i < len(cmds)-1; i++ {
		if cmds[i].File > cmds[i+1].File {
			t.Errorf("commands not sorted by file: %q > %q", cmds[i].File, cmds[i+1].File)
		}
	}

	// Find the watchlist root and verify its SubcommandConstructors.
	var watchlist *Command
	for i := range cmds {
		if cmds[i].ConstructorName == "newWatchlistCmd" {
			watchlist = &cmds[i]
			break
		}
	}
	if watchlist == nil {
		t.Fatal("newWatchlistCmd not found")
	}
	if len(watchlist.SubcommandConstructors) != 2 {
		t.Errorf("watchlist SubcommandConstructors = %v, want 2", watchlist.SubcommandConstructors)
	}
	want := map[string]bool{"newWatchlistCreateCmd": true, "newWatchlistAddCmd": true}
	for _, c := range watchlist.SubcommandConstructors {
		if !want[c] {
			t.Errorf("unexpected subcommand constructor: %q", c)
		}
	}
}

func TestParseCLI_AliasesAndAnnotations(t *testing.T) {
	dir := writeFixture(t, map[string]string{
		"login.go": `package cli

import "github.com/spf13/cobra"

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "login",
		Short:   "Authenticate",
		Aliases: []string{"signin", "auth-login"},
		Annotations: map[string]string{
			"category": "auth",
			"mcp":      "yes",
		},
	}
}
`,
	})

	cmds, _, err := ParseCLI(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 {
		t.Fatalf("want 1 command, got %d", len(cmds))
	}
	c := cmds[0]
	if len(c.Aliases) != 2 || c.Aliases[0] != "signin" || c.Aliases[1] != "auth-login" {
		t.Errorf("Aliases = %v", c.Aliases)
	}
	if c.Annotations["category"] != "auth" || c.Annotations["mcp"] != "yes" {
		t.Errorf("Annotations = %v", c.Annotations)
	}
}

func TestParseCLI_BacktickStrings(t *testing.T) {
	dir := writeFixture(t, map[string]string{
		"q.go": "package cli\n\nimport \"github.com/spf13/cobra\"\n\nfunc newCmd() *cobra.Command {\n\treturn &cobra.Command{Use: `quote`, Long: `multi\nline\ndescription`}\n}\n",
	})

	cmds, _, err := ParseCLI(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 {
		t.Fatalf("want 1, got %d", len(cmds))
	}
	if cmds[0].Use != "quote" {
		t.Errorf("Use = %q", cmds[0].Use)
	}
	if cmds[0].Long != "multi\nline\ndescription" {
		t.Errorf("Long = %q", cmds[0].Long)
	}
}

func TestParseCLI_SkipsTestFiles(t *testing.T) {
	dir := writeFixture(t, map[string]string{
		"real.go": `package cli

import "github.com/spf13/cobra"

func newCmd() *cobra.Command {
	return &cobra.Command{Use: "real", Short: "Real command"}
}
`,
		"real_test.go": `package cli

import (
	"testing"
	"github.com/spf13/cobra"
)

func TestSomething(t *testing.T) {
	c := &cobra.Command{Use: "fake-test-command"}
	_ = c
}
`,
	})

	cmds, _, err := ParseCLI(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cmds {
		if c.Use == "fake-test-command" {
			t.Error("test file was parsed — should have been skipped")
		}
	}
}

func TestParseCLI_TolerateUnparseableFile(t *testing.T) {
	dir := writeFixture(t, map[string]string{
		"good.go": `package cli

import "github.com/spf13/cobra"

func newCmd() *cobra.Command {
	return &cobra.Command{Use: "good"}
}
`,
		"broken.go": "package cli\n\nthis is not valid go {{{\n",
	})

	cmds, unparseable, err := ParseCLI(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].Use != "good" {
		t.Errorf("expected good command extracted despite broken file, got %+v", cmds)
	}
	if len(unparseable) != 1 {
		t.Errorf("expected 1 unparseable file, got %v", unparseable)
	}
}

func TestParseCLI_SkipsNonCommandConstructors(t *testing.T) {
	dir := writeFixture(t, map[string]string{
		"helpers.go": `package cli

import "github.com/spf13/cobra"

// Returns something, but not a cobra command.
func newClient() interface{} { return nil }

// Returns a cobra command but has no Use — should be skipped.
func newEmptyCmd() *cobra.Command {
	return &cobra.Command{}
}

func newRealCmd() *cobra.Command {
	return &cobra.Command{Use: "real"}
}
`,
	})

	cmds, _, err := ParseCLI(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].Use != "real" {
		t.Errorf("expected only real command, got %+v", cmds)
	}
}
