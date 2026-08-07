// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/stretchr/testify/require"
)

// posixModeMask matches a bare mode-bit comparison of the form
// `Mode().Perm()&0o077`. On Windows this can never be satisfied: os.Chmod only
// toggles the read-only attribute, so a file written 0600 stats back 0666.
var posixModeMask = regexp.MustCompile(`0o?077`)

// TestGenerate_PlatformProfilePermsAreBuildTagged verifies generated
// private-file checks remain platform-specific.
func TestGenerate_PlatformProfilePermsAreBuildTagged(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.ParseFile(filepath.Join("..", "..", "testdata", "golden", "fixtures", "golden-api-oauth2-cc.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	profileSrc := readGeneratedFile(t, outputDir, "internal", "platform", "profile.go")
	require.NotRegexp(t, posixModeMask, profileSrc,
		"internal/platform/profile.go must not gate on a raw POSIX mode comparison; "+
			"it is unsatisfiable on Windows. Delegate to a build-tagged helper.")
	require.Contains(t, profileSrc, "verifyPrivatePerms",
		"profile.go must call the build-tagged helper")

	unixSrc := readGeneratedFile(t, outputDir, "internal", "platform", "perms_unix.go")
	require.Contains(t, unixSrc, "//go:build !windows", "POSIX helper must carry a build tag")
	require.Contains(t, unixSrc, "func verifyPrivatePerms")
	require.Contains(t, unixSrc, "Mode().Perm()",
		"the POSIX helper must read the mode bits")
	require.Regexp(t, posixModeMask, unixSrc,
		"the POSIX helper is where the mode comparison belongs, and it must be preserved there")

	winSrc := readGeneratedFile(t, outputDir, "internal", "platform", "perms_windows.go")
	require.Contains(t, winSrc, "//go:build windows", "Windows helper must carry a build tag")
	require.Contains(t, winSrc, "func verifyPrivatePerms")
	require.NotRegexp(t, posixModeMask, winSrc,
		"the Windows helper must not compare POSIX mode bits")
	require.Contains(t, winSrc, "cliutil.VerifyCredsPerms",
		"Windows must reuse the existing DACL evaluator rather than inventing a second one")
	conformanceSrc := readGeneratedFile(t, outputDir, "internal", "platform", "conformance_test.go")
	require.Equal(t, 3, strings.Count(conformanceSrc, `if runtime.GOOS != "windows" {`),
		"only the three POSIX mode assertions should be bypassed on Windows")
	require.NotContains(t, conformanceSrc, `t.Skip("POSIX mode assertion is not meaningful on Windows")`,
		"Windows must continue through the rest of each conformance test")

	for _, f := range []string{"perms_unix.go", "perms_windows.go"} {
		_, err := os.Stat(filepath.Join(outputDir, "internal", "platform", f))
		require.NoError(t, err, "%s must be emitted", f)
	}
	requireGeneratedCompiles(t, outputDir)
}
