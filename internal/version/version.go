package version

import (
	"regexp"
	"runtime/debug"
	"strings"
)

// Version is the current printing-press version. It is set at build time
// via ldflags for tagged releases, or falls back to the hardcoded value.
var Version = "2.3.7" // x-release-please-version

// pseudoVersionSuffix matches the trailing `yyyymmddhhmmss-abcdefabcdef`
// (14-digit timestamp + 12-char commit hash) shared by every Go pseudo-version
// form. The character before the timestamp may be `-` (form 1: vX.0.0-ts-hash)
// or `.` (forms 2/3: vX.Y.Z-pre.0.ts-hash). See
// https://go.dev/ref/mod#pseudo-versions.
var pseudoVersionSuffix = regexp.MustCompile(`\d{14}-[0-9a-f]{12}$`)

func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if v := versionFromBuildInfo(info.Main.Version); v != "" {
		Version = v
	}
}

// versionFromBuildInfo returns the cleaned tagged version from a runtime
// build-info string, or "" when the value should be ignored (empty, devel
// build, or any pseudo-version). When this returns "", the hardcoded Version
// fallback is used.
func versionFromBuildInfo(v string) string {
	if v == "" || v == "(devel)" {
		return ""
	}
	if pseudoVersionSuffix.MatchString(v) {
		return ""
	}
	return strings.TrimPrefix(v, "v")
}

// Get returns the current version string.
func Get() string {
	return Version
}
