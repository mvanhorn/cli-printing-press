package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNovelHookRootFactoriesScope(t *testing.T) {
	src := `package cli

import "github.com/spf13/cobra"

var kept = newKeptCmd(nil)

func initHooks() {
	registerNovelCommand(func(root *cobra.Command) {
		root.AddCommand(newKeptCmd())
		keptLocal := newLocalCmd(nil)
		root.AddCommand(keptLocal)
		addNovelCommandIfAbsent(root, kept)
		if root := (&cobra.Command{}); root != nil {
			root.AddCommand(newIfShadowCmd())
		} else {
			root.AddCommand(newElseShadowCmd())
		}
		if other := 1; other == 0 {
		} else {
			root.AddCommand(newElseKeptCmd())
		}
		if root := (&cobra.Command{}); false {
		} else if other := 1; other == 1 {
			root.AddCommand(newElseIfShadowCmd())
		}
		if other := 1; other == 0 {
		} else if root != nil {
			root.AddCommand(newElseIfKeptCmd())
		}
		for root := range []int{1} {
			rootCmd := &cobra.Command{}
			_ = root
			rootCmd.AddCommand(newRangeShadowCmd())
		}
		root.AddCommand(newAfterCmd())
		var other = &cobra.Command{}
		other.AddCommand(newOtherCmd())
		root.AddCommand(newOneCmd(), newTwoCmd())
		go func(root *cobra.Command) {
			root.AddCommand(newClosureCmd())
		}(root)
		registerNovelCommand(func(root *cobra.Command) {
			root.AddCommand(newInnerCmd())
		})
	})
}

func wireCaptured() {
	cmd := newCapturedCmd(nil)
	registerNovelCommand(func(root *cobra.Command) {
		root.AddCommand(cmd)
	})
	outer := newOuterCmd(nil)
	registerNovelCommand(func(root *cobra.Command) {
		outer := newInnerCaptureCmd(nil)
		root.AddCommand(outer)
	})
}

type rootFlags struct{}

var capturedInit = newCapturedInitCmd(nil)

var _ = func() bool {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		root.AddCommand(newInitCmd(flags))
		root.AddCommand(capturedInit)
		root := &cobra.Command{}
		root.AddCommand(newInitShadowCmd())
	})
	return true
}()

var _ = func() bool {
	capturedInit := newInnerInitCmd(nil)
	registerNovelCommand(func(root *cobra.Command) {
		root.AddCommand(capturedInit)
	})
	return true
}()

func registerNovelCommand(fn func(*cobra.Command)) { _ = fn }
func addNovelCommandIfAbsent(parent, cmd *cobra.Command) { _, _ = parent, cmd }
func newKeptCmd(flags any) *cobra.Command { return &cobra.Command{Use: "kept"} }
func newLocalCmd(flags any) *cobra.Command { return &cobra.Command{Use: "local"} }
func newIfShadowCmd() *cobra.Command { return &cobra.Command{Use: "if-shadow"} }
func newElseShadowCmd() *cobra.Command { return &cobra.Command{Use: "else-shadow"} }
func newElseKeptCmd() *cobra.Command { return &cobra.Command{Use: "else-kept"} }
func newElseIfShadowCmd() *cobra.Command { return &cobra.Command{Use: "else-if-shadow"} }
func newElseIfKeptCmd() *cobra.Command { return &cobra.Command{Use: "else-if-kept"} }
func newCapturedCmd(flags any) *cobra.Command { return &cobra.Command{Use: "captured"} }
func newOuterCmd(flags any) *cobra.Command { return &cobra.Command{Use: "outer"} }
func newInnerCaptureCmd(flags any) *cobra.Command { return &cobra.Command{Use: "inner-capture"} }
func newRangeShadowCmd() *cobra.Command { return &cobra.Command{Use: "range-shadow"} }
func newAfterCmd() *cobra.Command { return &cobra.Command{Use: "after"} }
func newOtherCmd() *cobra.Command { return &cobra.Command{Use: "other"} }
func newOneCmd() *cobra.Command { return &cobra.Command{Use: "one"} }
func newTwoCmd() *cobra.Command { return &cobra.Command{Use: "two"} }
func newClosureCmd() *cobra.Command { return &cobra.Command{Use: "closure"} }
func newInnerCmd() *cobra.Command { return &cobra.Command{Use: "inner"} }
func newInitCmd(flags *rootFlags) *cobra.Command { return &cobra.Command{Use: "init"} }
func newInitShadowCmd() *cobra.Command { return &cobra.Command{Use: "init-shadow"} }
func newCapturedInitCmd(flags any) *cobra.Command { return &cobra.Command{Use: "captured-init"} }
func newInnerInitCmd(flags any) *cobra.Command { return &cobra.Command{Use: "inner-init"} }
`
	got, masked := novelHookRootFactories(src)
	want := map[string]int{
		"newKeptCmd":         2, // direct call plus the package-level variable
		"newLocalCmd":        1,
		"newAfterCmd":        1,
		"newOneCmd":          1,
		"newTwoCmd":          1,
		"newInnerCmd":        1,
		"newElseKeptCmd":     1,
		"newElseIfKeptCmd":   1,
		"newCapturedCmd":     1,
		"newInnerCaptureCmd": 1,
		"newInitCmd":         1,
		"newCapturedInitCmd": 1,
		"newInnerInitCmd":    1,
	}
	if len(got) != 14 {
		t.Fatalf("factories = %v, want fourteen registrations", got)
	}
	counts := map[string]int{}
	for _, name := range got {
		counts[name]++
	}
	for name, n := range want {
		if counts[name] != n {
			t.Errorf("%s count = %d, want %d; got %v", name, counts[name], n, got)
		}
	}
	for _, absent := range []string{"newIfShadowCmd", "newElseShadowCmd", "newElseIfShadowCmd", "newRangeShadowCmd", "newOtherCmd", "newClosureCmd", "newOuterCmd", "newInitShadowCmd"} {
		if counts[absent] != 0 {
			t.Errorf("shadowed registration %s was treated as the hook root: %v", absent, got)
		}
	}
	if masked == src {
		t.Fatal("hook body was not masked")
	}
	if len(masked) != len(src) {
		t.Fatalf("masked length %d, src length %d", len(masked), len(src))
	}
	for _, call := range []string{"AddCommand(newIfShadowCmd", "AddCommand(newElseShadowCmd", "AddCommand(newElseIfShadowCmd", "AddCommand(newRangeShadowCmd", "AddCommand(newClosureCmd", "AddCommand(newInnerCmd", "AddCommand(newInitShadowCmd"} {
		if strings.Contains(masked, call) {
			t.Errorf("masked source still contains %s", call)
		}
	}
	if !strings.Contains(src, "AddCommand(newIfShadowCmd") {
		t.Fatal("fixture lost the shadow call")
	}
}

func TestNovelHookRootFactoriesParseError(t *testing.T) {
	src := "package cli\nfunc ("
	got, masked := novelHookRootFactories(src)
	if len(got) != 0 || masked != src {
		t.Fatalf("got %v, masked changed: %v", got, masked != src)
	}
}

func TestCollectRegisteredCommandsNovelHookScope(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cli, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(cli, "root.go"), `package cli

import "github.com/spf13/cobra"

func Execute() {
	rootCmd := &cobra.Command{Use: "tool"}
	rootCmd.AddCommand(newLearningsCmd())
	addNovelCommandIfAbsent(rootCmd, newDirectCmd())
	captured := newCapturedCmd()
	registerNovelCommand(func(rootCmd *cobra.Command) {
		rootCmd.AddCommand(captured)
		rootCmd.AddCommand(newSellersCmd())
		{
			rootCmd := &cobra.Command{Use: "nested"}
			rootCmd.AddCommand(newShadowCmd())
			addNovelCommandIfAbsent(rootCmd, newShadowHelperCmd())
		}
		rootCmd.AddCommand(newAfterCmd())
	})
}

func registerNovelCommand(fn func(*cobra.Command)) { _ = fn }
func addNovelCommandIfAbsent(parent, cmd *cobra.Command) { _, _ = parent, cmd }

func newLearningsCmd() *cobra.Command { return &cobra.Command{Use: "learnings"} }
func newDirectCmd() *cobra.Command { return &cobra.Command{Use: "direct"} }
func newSellersCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "sellers"}
	addNovelCommandIfAbsent(cmd, newSellerStatsCmd())
	return cmd
}
func newSellerStatsCmd() *cobra.Command { return &cobra.Command{Use: "stats"} }
func newShadowCmd() *cobra.Command { return &cobra.Command{Use: "shadow"} }
func newShadowHelperCmd() *cobra.Command { return &cobra.Command{Use: "shadow-helper"} }
func newAfterCmd() *cobra.Command { return &cobra.Command{Use: "after"} }
func newCapturedCmd() *cobra.Command { return &cobra.Command{Use: "captured"} }

var _ = func() bool {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		root.AddCommand(newFromInitCmd(flags))
	})
	return true
}()
func newFromInitCmd(flags *rootFlags) *cobra.Command { return &cobra.Command{Use: "from-init"} }
`)

	paths, _ := collectRegisteredCommands(dir)
	for _, want := range []string{"learnings", "direct", "sellers", "sellers stats", "after", "captured", "from-init"} {
		if !paths[want] {
			t.Errorf("expected path %q, got %v", want, paths)
		}
	}
	for _, absent := range []string{"shadow", "shadow-helper", "stats", "nested", "tool shadow"} {
		if paths[absent] {
			t.Errorf("did not expect path %q, got %v", absent, paths)
		}
	}
}
