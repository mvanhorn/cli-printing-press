package generator

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// safeEnetxHTTPVersion is the lowest github.com/enetx/http release whose
// http.Server includes DisableClientPriority. enetx/http2 v1.0.25+ compiles a
// go1.27-tagged file that reads that field; v1.0.28 and older omit it, so
// `go install` on Go 1.27 fails. Keep in sync with the explicit pin in
// templates/go.mod.tmpl.
const safeEnetxHTTPVersion = "v1.0.29"

// ensureSafeEnetxHTTP bumps github.com/enetx/http to safeEnetxHTTPVersion when
// the generated module resolves it below that version. surf pulls both
// enetx/http and enetx/http2; a go.mod.tmpl pin covers the browser-transport
// path, but regen-merge can reintroduce a published v1.0.28 require and tidy
// can still land on surf's own v1.0.28 floor. Bumping after tidy is exact: it
// runs only when enetx/http is in the resolved graph. No-op when the module
// is absent or already at/above safeEnetxHTTPVersion.
func ensureSafeEnetxHTTP(dir string) error {
	out, err := runCommand(dir, qualityGateTimeout, "go", "list", "-m", "-f", "{{.Version}}", "github.com/enetx/http")
	if err != nil {
		// `go list -m` exits non-zero when enetx/http is not a dependency of
		// the module — nothing to pin.
		return nil
	}
	// go list -m writes the version to stdout; runCommand joins stdout+stderr,
	// so take only the first line to ignore any progress/download messages that
	// the toolchain emits to stderr (e.g. "go: downloading github.com/enetx/http …")
	// in fresh-cache environments.
	current := strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]
	current = strings.TrimSpace(current)
	if !semver.IsValid(current) || semver.Compare(current, safeEnetxHTTPVersion) >= 0 {
		return nil
	}
	if _, err := runCommand(dir, qualityGateTimeout, "go", "get", "github.com/enetx/http@"+safeEnetxHTTPVersion); err != nil {
		return fmt.Errorf("bumping github.com/enetx/http to %s: %w", safeEnetxHTTPVersion, err)
	}
	if _, err := runCommand(dir, qualityGateTimeout, "go", "mod", "tidy"); err != nil {
		return fmt.Errorf("re-running go mod tidy after github.com/enetx/http bump: %w", err)
	}
	return nil
}
