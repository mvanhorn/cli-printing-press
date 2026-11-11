package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNovelHookRootFactoriesScope(t *testing.T) {
	for _, tc := range []struct {
		body string
		want int
	}{
		{`addNovelCommandIfAbsent(tree, newStats(flags))`, 1},
		{`var child = newStats(flags); addNovelCommandIfAbsent(tree, child)`, 1},
		{`child := newStats(flags); tree.AddCommand(child)`, 1},
		{`tree.AddCommand(newStats(flags), newStats(flags))`, 2},
		{`{ tree := &cobra.Command{}; addNovelCommandIfAbsent(tree, newStats(flags)) }`, 0},
		{`other := &cobra.Command{}; addNovelCommandIfAbsent(other, newStats(flags))`, 0},
	} {
		src := `package cli
func init() { registerNovelCommand(func(tree *cobra.Command, flags *rootFlags) { ` + tc.body + ` }) }`
		got := novelHookRootFactories(src)
		if len(got) != tc.want {
			t.Errorf("%s: got %v want %d factories", tc.body, got, tc.want)
		}
		for _, name := range got {
			if name != "newStats" {
				t.Errorf("unexpected factory %q", name)
			}
		}
	}
}

func TestCollectRegisteredCommandsNovelHelpers(t *testing.T) {
	for _, wiring := range []struct{ name, root, child string }{
		{"direct", "addNovelCommandIfAbsent(rootCmd, newSellersCmd(nil))", "addNovelCommandIfAbsent(cmd, newSellerStatsCmd(nil))"},
		{"variables", "sellers := newSellersCmd(nil); addNovelCommandIfAbsent(rootCmd, sellers)", "stats := newSellerStatsCmd(nil); addNovelCommandIfAbsent(cmd, stats)"},
		{"legacy direct", "rootCmd.AddCommand(newSellersCmd(nil))", "cmd.AddCommand(newSellerStatsCmd(nil))"},
		{"legacy variables", "sellers := newSellersCmd(nil); rootCmd.AddCommand(sellers)", "stats := newSellerStatsCmd(nil); cmd.AddCommand(stats)"},
		{"hook variable", "registerNovelCommand(func(tree *cobra.Command, flags *rootFlags) { sellers := newSellersCmd(flags); addNovelCommandIfAbsent(tree, sellers) })", "addNovelCommandIfAbsent(cmd, newSellerStatsCmd(nil))"},
		{"hook", "registerNovelCommand(func(tree *cobra.Command, flags *rootFlags) { addNovelCommandIfAbsent(tree, newSellersCmd(flags)) })", "addNovelCommandIfAbsent(cmd, newSellerStatsCmd(nil))"},
	} {
		t.Run(wiring.name, func(t *testing.T) {
			dir := t.TempDir()
			cli := filepath.Join(dir, "internal", "cli")
			if err := os.MkdirAll(cli, 0755); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, filepath.Join(cli, "root.go"), `package cli
func Execute() {
 rootCmd := &cobra.Command{Use: "demo-pp-cli"}
 rootCmd.AddCommand(newLearningsCmd(nil))
 `+wiring.root+`
}
func newLearningsCmd(flags *rootFlags) *cobra.Command {
 cmd := &cobra.Command{Use: "learnings"}
 cmd.AddCommand(newLearningStatsCmd(nil))
 return cmd
}
func newLearningStatsCmd(flags *rootFlags) *cobra.Command { return &cobra.Command{Use: "stats"} }
`)
			writeTestFile(t, filepath.Join(cli, "sellers.go"), `package cli
func newSellersCmd(flags *rootFlags) *cobra.Command {
 cmd := &cobra.Command{Use: "sellers"}
 `+wiring.child+`
 return cmd
}
func newSellerStatsCmd(flags *rootFlags) *cobra.Command { return &cobra.Command{Use: "stats"} }
func unregistered() *cobra.Command { return &cobra.Command{Use: "orphan"} }
`)
			paths, _ := collectRegisteredCommands(dir)
			for _, path := range []string{"sellers", "sellers stats", "learnings", "learnings stats"} {
				if !paths[path] {
					t.Errorf("missing %q: %v", path, paths)
				}
			}
			if paths["stats"] || paths["orphan"] {
				t.Errorf("invented root: %v", paths)
			}
			if mismatch := novelFeatureDepthMismatch(NovelFeature{Command: "sellers stats"}, paths); mismatch != nil {
				t.Errorf("false mismatch: %+v", mismatch)
			}
			if mismatch := novelFeatureDepthMismatch(NovelFeature{Command: "missing stats"}, paths); mismatch == nil {
				t.Error("real wrong-parent mismatch hidden")
			}
		})
	}
}
