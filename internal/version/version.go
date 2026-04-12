package version

import (
	"runtime/debug"
	"strings"
)

// Version is the current printing-press version. It is set at build time
// via ldflags for tagged releases, or falls back to the hardcoded value.
var Version = "2.1.0" // x-release-please-version

func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	v := info.Main.Version
	// Only use the build info version when it's a real tagged release.
	// Skip empty, "(devel)", and pseudo-versions like "v0.0.0-20260328...".
	if v == "" || v == "(devel)" {
		return
	}
	trimmed := strings.TrimPrefix(v, "v")
	if strings.HasPrefix(trimmed, "0.0.0-") {
		return
	}
	Version = trimmed
}

// Get returns the current version string.
func Get() string {
	return Version
}
