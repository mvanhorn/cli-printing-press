// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGenerate_EmitsCredsPermsForTokenSpec proves the read-time credentials
// permission check (S1) is emitted into internal/cliutil for a token-bearing
// spec. The OAuth2 client_credentials golden fixture persists an access token,
// so the emitted CLI must ship the drift-detection guard on the POSIX, Windows,
// and pure-evaluator surfaces plus its unit test.
func TestGenerate_EmitsCredsPermsForTokenSpec(t *testing.T) {
	t.Parallel()

	// openapi.ParseFile derives the CLI name from info.title (the generate
	// command normally supplies it via --spec-url); spec.Parse would reject the
	// bare OpenAPI fixture as "name is required".
	apiSpec, err := openapi.ParseFile(filepath.Join("..", "..", "testdata", "golden", "fixtures", "golden-api-oauth2-cc.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	evalSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "creds_perms_eval.go")
	require.Contains(t, evalSrc, "func evalCredsSecurity", "pure SDDL evaluator must be emitted")

	winSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "creds_perms_windows.go")
	require.Contains(t, winSrc, "func VerifyCredsPerms", "Windows read-time guard must be emitted")

	unixSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "creds_perms_unix.go")
	require.Contains(t, unixSrc, "func VerifyCredsPerms", "POSIX read-time guard must be emitted")

	_, err = os.Stat(filepath.Join(outputDir, "internal", "cliutil", "creds_perms_eval_test.go"))
	require.NoError(t, err, "pure evaluator unit test must be emitted")

	// A3: the read-time guard must be wired into config.Load's read path. The
	// persisted token file is canonicalized (EvalSymlinks) then perms-checked
	// (cliutil.VerifyCredsPerms) before it is consumed, so an over-permissive
	// token config is refused on READ (a silent miss), not only enforced 0600
	// on write.
	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.Contains(t, configSrc, "filepath.EvalSymlinks(", "config.Load must canonicalize the config path before the perms check")
	require.Contains(t, configSrc, "cliutil.VerifyCredsPerms(", "config.Load must guard the persisted-token read with the perms check")

	_, err = os.Stat(filepath.Join(outputDir, "internal", "config", "config_perms_test.go"))
	require.NoError(t, err, "config.Load read-time perms behavioral test must be emitted")

	// A4: cliutil.LoadCredentials reads a SEPARATE credentials file that also
	// holds a live token, so it must apply the same read-time guard. Because
	// credentials.go lives in package cliutil, the guard calls VerifyCredsPerms
	// in-package (not cliutil.VerifyCredsPerms).
	credsSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "credentials.go")
	require.Contains(t, credsSrc, "VerifyCredsPerms(", "LoadCredentials must guard the credentials-file read with the perms check")

	_, err = os.Stat(filepath.Join(outputDir, "internal", "cliutil", "credentials_perms_test.go"))
	require.NoError(t, err, "cliutil credentials read-time perms behavioral test must be emitted")

	// A5: creds_perms_windows.go imports golang.org/x/sys/windows, which makes
	// golang.org/x/sys a DIRECT dependency of a token-bearing bundle. The
	// freshly generated go.mod (BEFORE any manual `go mod tidy`) must list it as
	// a direct require: a require line that is NOT marked "// indirect".
	goMod := readGeneratedFile(t, outputDir, "go.mod")
	var sysLine string
	for line := range strings.SplitSeq(goMod, "\n") {
		// Match the require DIRECTIVE for x/sys, not comment lines that merely
		// mention the module. Handles both standalone (`require golang.org/x/sys
		// v...`) and require-block (`\tgolang.org/x/sys v...`) forms.
		dep := strings.TrimPrefix(strings.TrimSpace(line), "require ")
		if strings.HasPrefix(dep, "golang.org/x/sys ") || strings.HasPrefix(dep, "golang.org/x/sys\t") {
			sysLine = line
			break
		}
	}
	require.NotEmpty(t, sysLine, "go.mod must require golang.org/x/sys for a token-bearing spec")
	require.NotContains(t, sysLine, "// indirect",
		"golang.org/x/sys must be a DIRECT require for a token-bearing spec (creds_perms_windows.go imports golang.org/x/sys/windows)")
}

// TestGenerate_NoCredsPermsForNonAuthSpec proves the guard is gated on
// shouldEmitAuth(): a spec with no auth persists no token, so shipping the
// check would be dead weight. public-param-names declares auth.type: none.
func TestGenerate_NoCredsPermsForNonAuthSpec(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "golden", "fixtures", "public-param-names.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	_, err = os.Stat(filepath.Join(outputDir, "internal", "cliutil", "creds_perms_eval.go"))
	require.True(t, os.IsNotExist(err), "creds_perms_eval.go must not be emitted for a non-auth spec")
}
