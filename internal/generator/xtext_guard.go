package generator

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// safeXTextVersion is the lowest golang.org/x/text release without GO-2026-5970
// (infinite loop on invalid input). x/text is pulled transitively by the
// learn/store stack, not as a direct printed-CLI require.
const safeXTextVersion = "v0.39.0"

// ensureSafeXText bumps golang.org/x/text to safeXTextVersion when the
// generated module resolves it below that version. The learn journal and
// local store drag x/text in transitively; a go.mod.tmpl condition would
// either miss a puller or leave an unused require in CLIs that do not
// import it (breaking the tidy gate).
//
// Bumping after tidy is exact: it runs only when x/text is actually in the
// resolved build graph. Runs before the govulncheck gate so a freshly printed
// or reprinted CLI ships with the patched version instead of regressing to a
// vulnerable resolve. No-op when x/text is absent or already at/above
// safeXTextVersion.
func ensureSafeXText(dir string) error {
	out, err := runCommand(dir, qualityGateTimeout, "go", "list", "-m", "-f", "{{.Version}}", "golang.org/x/text")
	if err != nil {
		// `go list -m` exits non-zero when x/text is not a dependency of the
		// module — nothing to pin.
		return nil
	}
	// go list -m writes the version to stdout; runCommand joins stdout+stderr,
	// so take only the first line to ignore any progress/download messages that
	// the toolchain emits to stderr (e.g. "go: downloading golang.org/x/text …")
	// in fresh-cache environments.
	current := strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]
	current = strings.TrimSpace(current)
	if !semver.IsValid(current) || semver.Compare(current, safeXTextVersion) >= 0 {
		return nil
	}
	if _, err := runCommand(dir, qualityGateTimeout, "go", "get", "golang.org/x/text@"+safeXTextVersion); err != nil {
		return fmt.Errorf("bumping golang.org/x/text to %s: %w", safeXTextVersion, err)
	}
	if _, err := runCommand(dir, qualityGateTimeout, "go", "mod", "tidy"); err != nil {
		return fmt.Errorf("re-running go mod tidy after golang.org/x/text bump: %w", err)
	}
	return nil
}
