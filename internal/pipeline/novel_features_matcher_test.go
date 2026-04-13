package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

// Cases that should match identically under both matching modes.
var matcherCommonCases = []struct {
	name    string
	feature NovelFeature
	want    bool
}{
	{"exact leaf match", NovelFeature{Command: "digest"}, true},
	{"exact after flag strip", NovelFeature{Command: "digest --watchlist tech"}, true},
	{"no match when absent", NovelFeature{Command: "insiders --recent 30d"}, false},
	{"empty command", NovelFeature{Command: ""}, false},
	{"flag-only command", NovelFeature{Command: "--json"}, false},
	{"case insensitive", NovelFeature{Command: "DIGEST"}, true},
	{"alias exact", NovelFeature{Command: "portfolio dividends", Aliases: []string{"digest"}}, true},
}

func TestMatchNovelFeature_LeafFallback(t *testing.T) {
	leaves := map[string]bool{
		"perf": true, "gains": true, "digest": true, "login-chrome": true,
		"options": true, "options-chain": true, "sparkline": true, "earnings": true,
	}
	cases := append([]struct {
		name    string
		feature NovelFeature
		want    bool
	}{
		{"leaf-only: portfolio perf matches perf regardless of parent", NovelFeature{Command: "portfolio perf"}, true},
		{"leaf-only hyphen-prefix: auth login → login-chrome", NovelFeature{Command: "auth login --chrome"}, true},
		{"bare leaf registered: options", NovelFeature{Command: "options --moneyness otm"}, true},
	}, matcherCommonCases...)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchNovelFeature(tc.feature, nil, leaves); got != tc.want {
				t.Errorf("matchNovelFeature(%q) = %v, want %v", tc.feature.Command, got, tc.want)
			}
		})
	}
}

func TestMatchNovelFeature_PathAware(t *testing.T) {
	paths := map[string]bool{
		"portfolio": true, "portfolio perf": true, "portfolio gains": true, "portfolio add": true,
		"auth": true, "auth login": true, "auth login-chrome": true, "auth logout": true,
		"options": true, "options-chain": true,
		"digest":    true,
		"watchlist": true, "watchlist create": true, "watchlist add": true,
	}
	cases := append([]struct {
		name    string
		feature NovelFeature
		want    bool
	}{
		{"exact full path", NovelFeature{Command: "portfolio perf"}, true},
		{"exact hyphenated leaf", NovelFeature{Command: "auth login-chrome"}, true},
		{"sibling hyphen-prefix under parent", NovelFeature{Command: "auth login --chrome"}, true},
		{"sibling hyphen-prefix at root", NovelFeature{Command: "options --moneyness otm"}, true},
		{"alias rescues missing primary", NovelFeature{Command: "portfolio dividends", Aliases: []string{"portfolio gains"}}, true},
		{"case insensitive path", NovelFeature{Command: "Portfolio Perf"}, true},
		{"wrong parent does not match", NovelFeature{Command: "nonexistent perf"}, false},
	}, matcherCommonCases...)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchNovelFeature(tc.feature, paths, nil); got != tc.want {
				t.Errorf("matchNovelFeature(%q) = %v, want %v", tc.feature.Command, got, tc.want)
			}
		})
	}
}

// A planned leaf must not match a registered leaf under a different parent.
// This is the accuracy guarantee that path-awareness buys over the old
// flat-map matcher.
func TestMatchNovelFeature_NoFalsePositive_CrossParent(t *testing.T) {
	paths := map[string]bool{
		"analytics":      true,
		"analytics perf": true,
	}
	nf := NovelFeature{Command: "portfolio perf"}
	if matchNovelFeature(nf, paths, nil) {
		t.Error("planned 'portfolio perf' should not match 'analytics perf' across parents")
	}
}

// "op" must not prefix-match "options" — hyphen boundary required.
func TestMatchNovelFeature_NoFalsePositive_BareLeaf(t *testing.T) {
	paths := map[string]bool{"options": true}
	nf := NovelFeature{Command: "op"}
	if matchNovelFeature(nf, paths, nil) {
		t.Error("planned 'op' should not prefix-match 'options'")
	}
}

// Tree reconstruction only captures commands wired via direct
// `parent.AddCommand(newFooCmd())` literals; CLIs that use variable
// indirection (`sub := newFooCmd(); parent.AddCommand(sub)`) produce a
// partial paths map. The matcher must still fall back to the
// Use-field-scanned leaves so legitimately-built features aren't
// reported missing. Regression guard for a pattern shipped in yahoo-
// finance and hackernews.
func TestMatchNovelFeature_FallsBackToLeavesWhenPathsPartial(t *testing.T) {
	// paths has the directly-wired command only; `ask` is present in
	// the source but wired through a variable, so it shows up in leaves
	// but not paths.
	paths := map[string]bool{"show": true}
	leaves := map[string]bool{"show": true, "ask": true}
	nf := NovelFeature{Command: "ask"}
	if !matchNovelFeature(nf, paths, leaves) {
		t.Error("'ask' (present in leaves, absent from partial paths) should still match")
	}
}

func TestCommandPath(t *testing.T) {
	cases := map[string]string{
		"portfolio perf":                       "portfolio perf",
		"auth login --chrome":                  "auth login",
		"options --moneyness otm --max-dte 45": "options",
		"digest":                               "digest",
		"digest --watchlist tech":              "digest",
		"earnings-calendar --watchlist":        "earnings-calendar",
		"":                                     "",
		"--json":                               "",
		"   padded   perf  ":                   "padded perf",
		"PORTFOLIO PERF":                       "portfolio perf",
	}
	for in, want := range cases {
		if got := commandPath(in); got != want {
			t.Errorf("commandPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// End-to-end: synthetic CLI fixture + tree walker should emit every
// nested path the matcher needs.
func TestCollectRegisteredCommands_Tree(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cli, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(cli, "root.go"), `package cli
import "github.com/spf13/cobra"
func Execute() {
	rootCmd := &cobra.Command{Use: "tool"}
	rootCmd.AddCommand(newAuthCmd())
	rootCmd.AddCommand(newPortfolioCmd())
	rootCmd.AddCommand(newDigestCmd())
	rootCmd.AddCommand(newOptionsCmd())
	rootCmd.AddCommand(newOptionsChainCmd())
}
`)
	writeTestFile(t, filepath.Join(cli, "auth.go"), `package cli
import "github.com/spf13/cobra"
func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "auth"}
	cmd.AddCommand(newAuthLoginCmd())
	cmd.AddCommand(newAuthLoginChromeCmd())
	cmd.AddCommand(newAuthLogoutCmd())
	return cmd
}
func newAuthLoginCmd() *cobra.Command { return &cobra.Command{Use: "login"} }
func newAuthLoginChromeCmd() *cobra.Command { return &cobra.Command{Use: "login-chrome"} }
func newAuthLogoutCmd() *cobra.Command { return &cobra.Command{Use: "logout"} }
`)
	writeTestFile(t, filepath.Join(cli, "portfolio.go"), `package cli
import "github.com/spf13/cobra"
func newPortfolioCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "portfolio"}
	cmd.AddCommand(newPortfolioPerfCmd())
	cmd.AddCommand(newPortfolioGainsCmd())
	return cmd
}
func newPortfolioPerfCmd() *cobra.Command { return &cobra.Command{Use: "perf"} }
func newPortfolioGainsCmd() *cobra.Command { return &cobra.Command{Use: "gains"} }
`)
	writeTestFile(t, filepath.Join(cli, "misc.go"), `package cli
import "github.com/spf13/cobra"
func newDigestCmd() *cobra.Command { return &cobra.Command{Use: "digest"} }
func newOptionsCmd() *cobra.Command { return &cobra.Command{Use: "options <symbol>"} }
func newOptionsChainCmd() *cobra.Command { return &cobra.Command{Use: "options-chain <symbol>"} }
`)

	paths, leaves := collectRegisteredCommands(dir)

	wantPaths := []string{
		"auth", "auth login", "auth login-chrome", "auth logout",
		"portfolio", "portfolio perf", "portfolio gains",
		"digest", "options", "options-chain",
	}
	for _, w := range wantPaths {
		if !paths[w] {
			t.Errorf("expected path %q, got paths: %v", w, paths)
		}
	}

	wantLeaves := []string{"auth", "login", "login-chrome", "logout", "portfolio", "perf", "gains", "digest", "options", "options-chain"}
	for _, w := range wantLeaves {
		if !leaves[w] {
			t.Errorf("expected leaf %q, got leaves: %v", w, leaves)
		}
	}
}
