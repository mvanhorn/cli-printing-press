package generator

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// safeXNetVersion is the lowest golang.org/x/net release without the 2026
// advisories (GO-2026-5025..5030). Keep in sync with the explicit pin in
// templates/go.mod.tmpl.
const safeXNetVersion = "v0.55.0"

// ensureSafeXNet bumps golang.org/x/net to safeXNetVersion when the generated
// module resolves it below that version. x/net is dragged in transitively by
// several optional features — surf (browser HTTP transport), goquery (search
// backends), kooky (cookie auth), chromedp, and net/html extraction — that
// live in different templates and copied-in packages. Enumerating every puller
// as a go.mod.tmpl condition is fragile (it has already missed surf/goquery/
// kooky once) and a too-broad condition breaks the `go mod tidy` gate by
// leaving an unused require in CLIs that don't pull x/net at all.
//
// Bumping after tidy is exact: it runs only when x/net is actually in the
// resolved build graph, so it never adds an unused dependency and never misses
// a puller (current or future). Runs before the govulncheck gate so a freshly
// printed CLI ships with the patched version. No-op when x/net is absent or
// already at/above safeXNetVersion.
func ensureSafeXNet(dir string) error {
	out, err := runCommand(dir, qualityGateTimeout, "go", "list", "-m", "-f", "{{.Version}}", "golang.org/x/net")
	if err != nil {
		// `go list -m` exits non-zero when x/net is not a dependency of the
		// module — nothing to pin.
		return nil
	}
	// go list -m writes the version to stdout; runCommand joins stdout+stderr,
	// so take only the first line to ignore any progress/download messages that
	// the toolchain emits to stderr (e.g. "go: downloading golang.org/x/net …")
	// in fresh-cache environments.
	current := strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]
	current = strings.TrimSpace(current)
	if !semver.IsValid(current) || semver.Compare(current, safeXNetVersion) >= 0 {
		return nil
	}
	if _, err := runCommand(dir, qualityGateTimeout, "go", "get", "golang.org/x/net@"+safeXNetVersion); err != nil {
		return fmt.Errorf("bumping golang.org/x/net to %s: %w", safeXNetVersion, err)
	}
	if _, err := runCommand(dir, qualityGateTimeout, "go", "mod", "tidy"); err != nil {
		return fmt.Errorf("re-running go mod tidy after golang.org/x/net bump: %w", err)
	}
	return nil
}
