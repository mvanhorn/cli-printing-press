// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"os"
	"path/filepath"
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
