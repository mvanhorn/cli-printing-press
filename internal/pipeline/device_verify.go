package pipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// isDeviceCLIDir reports whether dir holds a printed device CLI (a BLE
// device-native CLI) rather than an HTTP-API CLI. Device CLIs are emitted by the
// device generator with an internal/device package and no internal/client HTTP
// client. The verify/dogfood/scorecard checks that assume an HTTP surface (sync
// data pipeline, client.go auth protocol, agent-context enumeration) do not
// apply to device CLIs and must be skipped for them rather than reported as
// false-negative failures.
func isDeviceCLIDir(dir string) bool {
	// Reuse the canonical device-CLI detector (device-spec.yaml / README marker /
	// the spec.go `Protocol = "ble"` constant) rather than a second existence
	// heuristic that could drift from it.
	if !isDeviceBackedCLIDir(dir) {
		return false
	}
	// An HTTP client package means this is not a device-only CLI, so the
	// HTTP-shaped verify/dogfood checks still apply.
	if _, err := os.Stat(filepath.Join(dir, "internal", "client", "client.go")); err == nil {
		return false
	}
	return true
}

// enumerateCommandPathsViaHelp walks the CLI's command tree using `--help`
// output, returning the path to each leaf command (framework commands like
// help/completion/version are skipped by parseHelpCommands). It is the fallback
// the dogfood example check and the live-dogfood runner use when a CLI does not
// expose the `agent-context` command — notably device CLIs, which the device
// generator does not emit agent-context into.
func enumerateCommandPathsViaHelp(binaryPath string) [][]string {
	var paths [][]string
	var walk func(prefix []string)
	walk = func(prefix []string) {
		if len(prefix) > 6 {
			// Practical CLI command trees are <=4 levels deep; bound the
			// recursion so a pathological tree cannot spawn unbounded help
			// subprocesses.
			paths = append(paths, prefix)
			return
		}
		args := append(append([]string{}, prefix...), "--help")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, binaryPath, args...)
		applyDefaultSubprocessEnv(cmd)
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			if len(prefix) > 0 {
				paths = append(paths, prefix)
			}
			return
		}
		subs := parseHelpCommands(string(out))
		if len(subs) == 0 {
			if len(prefix) > 0 {
				paths = append(paths, prefix)
			}
			return
		}
		for _, sub := range subs {
			walk(append(append([]string{}, prefix...), sub.Name))
		}
	}
	walk(nil)
	return paths
}
