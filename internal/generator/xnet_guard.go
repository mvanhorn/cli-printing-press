package generator

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// safeXNetVersion is the lowest golang.org/x/net release without
// GO-2026-6603, GO-2026-6610, GO-2026-6611, GO-2026-6612, and
// GO-2026-6617 (reachable from the browser-chrome http2 transport).
// It also covers GO-2026-5942 (CVE-2026-46600) and GO-2026-5025..5030.
// Keep in sync with the explicit pin in templates/go.mod.tmpl.
//
// This is not the Go toolchain floor. The govulncheck watch ignores
// non-stdlib modules, so an x/net advisory does not move librarySafeGoDirective.
const safeXNetVersion = "v0.60.0"

// ensureSafeXNet bumps golang.org/x/net to safeXNetVersion when the generated
// module resolves it below that version. x/net is dragged in transitively by
// several optional features — the Chrome HTTP/2 transport, goquery (search
// backends), kooky (cookie auth), chromedp, and net/html extraction — that
// live in different templates and copied-in packages. Enumerating every puller
// as a go.mod.tmpl condition is fragile (it has already missed goquery/
// kooky once) and a too-broad condition breaks the `go mod tidy` gate by
// leaving an unused require in CLIs that don't pull x/net at all.
//
// Bumping after tidy is exact: it runs only when x/net is actually in the
// resolved build graph, so it never adds an unused dependency and never misses
// a puller (current or future). Runs before the govulncheck gate so a freshly
// printed CLI ships with the patched version. No-op when x/net is absent or
// already at/above safeXNetVersion.
func ensureSafeXNet(dir string) error {
	return ensureSafeXNetWith(dir, runCommand)
}

type moduleCommand func(dir string, timeout time.Duration, name string, args ...string) (string, error)

func ensureSafeXNetWith(dir string, run moduleCommand) error {
	out, err := run(dir, qualityGateTimeout, "go", "list", "-m", "-f", "{{.Version}}", "golang.org/x/net")
	if err != nil {
		// `go list -m` exits non-zero when x/net is not a dependency of the
		// module — nothing to pin.
		return nil
	}
	// go list -m writes the version to stdout; runCommand joins stdout+stderr,
	// so take only the first line to ignore progress messages the toolchain
	// writes to stderr (for example "go: downloading golang.org/x/net …").
	if !moduleVersionBelowFloor(moduleListVersion(out), safeXNetVersion) {
		return nil
	}
	if _, err := run(dir, qualityGateTimeout, "go", "get", "golang.org/x/net@"+safeXNetVersion); err != nil {
		return fmt.Errorf("bumping golang.org/x/net to %s: %w", safeXNetVersion, err)
	}
	if _, err := run(dir, qualityGateTimeout, "go", "mod", "tidy"); err != nil {
		return fmt.Errorf("re-running go mod tidy after golang.org/x/net bump: %w", err)
	}
	return nil
}

func moduleListVersion(out string) string {
	current := strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]
	return strings.TrimSpace(current)
}

func moduleVersionBelowFloor(version, floor string) bool {
	return semver.IsValid(version) && semver.Compare(version, floor) < 0
}
