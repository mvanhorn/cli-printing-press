// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"os"
	"os/exec"
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
	require.Contains(t, configSrc, "CredentialRefusals []cliutil.CredentialRefusal", "config.Load must preserve refused credential state")
	require.Contains(t, configSrc, "func (c *Config) HasCredentialRefusals() bool", "diagnostics must distinguish refused credentials from absence")
	require.Contains(t, configSrc, "credentialsRefused := false", "explicit config credential refusals must not fall through to another credentials home")

	_, err = os.Stat(filepath.Join(outputDir, "internal", "config", "config_perms_test.go"))
	require.NoError(t, err, "config.Load read-time perms behavioral test must be emitted")
	configPermsSrc := readGeneratedFile(t, outputDir, "internal", "config", "config_perms_test.go")
	require.Contains(t, configPermsSrc, "internal/cliutil/testenv", "config permission tests must use the shared path sandbox")
	require.Contains(t, configPermsSrc, "testenv.Isolate(t, cliutil.DataDir)", "config permission tests must isolate the credentials store")
	require.Contains(t, configPermsSrc, "TestLoad_DoesNotRecordRefusalForCredentialFreeLooseConfig", "config permission tests must prove loose credential-free configs are not recorded as refused")
	require.Contains(t, configPermsSrc, "CredentialRefusalSummaries", "config permission tests must assert refused state survives the soft miss")

	// A4: cliutil.LoadCredentials reads a SEPARATE credentials file that also
	// holds a live token, so it must apply the same read-time guard. Because
	// credentials.go lives in package cliutil, the guard calls VerifyCredsPerms
	// in-package (not cliutil.VerifyCredsPerms).
	credsSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "credentials.go")
	require.Contains(t, credsSrc, "VerifyCredsPerms(", "LoadCredentials must guard the credentials-file read with the perms check")
	require.Contains(t, credsSrc, "func LoadCredentialsWithStatus()", "LoadCredentials must expose a status-bearing path for refusals")
	require.Contains(t, credsSrc, "type CredentialRefusal", "refused credentials must be distinct from absent credentials")
	require.Contains(t, credsSrc, "error: %s", "permission refusals must print at error severity")
	require.Contains(t, credsSrc, "type CredentialsPermissionError", "credential saves must report a landed file that fails the permission guard")
	require.Contains(t, credsSrc, "filepath.EvalSymlinks(path)", "credential saves must verify the file that was actually published")
	require.Contains(t, credsSrc, "VerifyCredsPerms(real)", "credential saves must verify permissions after publication")

	credsPermsPath := filepath.Join(outputDir, "internal", "cliutil", "credentials_perms_test.go")
	_, err = os.Stat(credsPermsPath)
	require.NoError(t, err, "cliutil credentials read-time perms behavioral test must be emitted")
	credsPermsSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "credentials_perms_test.go")
	require.Contains(t, credsPermsSrc, "LoadCredentialsWithStatus()", "generated credentials perms test must assert refusal state")
	require.Contains(t, credsPermsSrc, "TestLoadCredentials_MissingFileIsAbsentNotRefused", "generated credentials perms test must keep absence distinct from refusal")

	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	require.Contains(t, rootSrc, "writeCredentialSaveErrorEnvelope", "JSON auth failures must report post-save credential permission refusals")
	require.Contains(t, rootSrc, "envelopeWriter := io.Writer(os.Stdout)", "credential permission failures must use the normal JSON output stream")
	require.Contains(t, rootSrc, `"permissions_verified": false`, "JSON auth failures must flag the unsafe landed file")
	pathsSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "paths.go")
	require.Contains(t, pathsSrc, "renamePrivateFileWithRetryFunc", "private-file publication must expose a testable bounded retry path")
	requireGeneratedCompiles(t, outputDir)

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

func TestGeneratedConfigPermissionTestsIgnoreSeededCredentialsStore(t *testing.T) {
	if testing.Short() {
		t.Skip("generated CLI compile tests run in the full generated-test CI lane")
	}
	t.Parallel()

	apiSpec, err := openapi.ParseFile(filepath.Join("..", "..", "testdata", "golden", "fixtures", "golden-api-oauth2-cc.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	envPrefix := naming.EnvPrefix(apiSpec.Name)
	operatorHome := t.TempDir()
	credentialsPath := filepath.Join(operatorHome, ".local", "share", naming.CLI(apiSpec.Name), "credentials.toml")
	ambientDataDir := filepath.Dir(credentialsPath)
	ambientXDGDataHome := filepath.Dir(ambientDataDir)
	require.NoError(t, os.MkdirAll(filepath.Dir(credentialsPath), 0o700))
	require.NoError(t, os.WriteFile(credentialsPath, []byte("access_token = \"AMBIENT-CREDENTIAL\"\n"), 0o600))

	cacheDir, err := goBuildCacheDir(outputDir)
	require.NoError(t, err)
	cmd := exec.Command("go", "test", "-mod=mod", "./internal/config", "-run", "TestLoad_RefusesOverPermissiveConfigOnRead|TestLoad_ReseedsFromEnvAfterOverPermissiveRefusal|TestLoad_RefusesSymlinkToLoosePermsTarget|TestLoad_DanglingSymlinkIsMiss", "-count=1")
	cmd.Dir = outputDir
	cmd.Env = append(os.Environ(),
		"HOME="+operatorHome,
		"USERPROFILE="+operatorHome,
		envPrefix+"_HOME=",
		envPrefix+"_CONFIG=",
		envPrefix+"_DATA_DIR="+ambientDataDir,
		envPrefix+"_CONFIG_DIR="+filepath.Join(operatorHome, ".config"),
		envPrefix+"_STATE_DIR="+filepath.Join(operatorHome, ".local", "state"),
		envPrefix+"_CACHE_DIR="+filepath.Join(operatorHome, ".cache"),
		"XDG_CONFIG_HOME="+filepath.Join(operatorHome, ".config"),
		"XDG_DATA_HOME="+ambientXDGDataHome,
		"XDG_STATE_HOME="+filepath.Join(operatorHome, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(operatorHome, ".cache"),
		"GOCACHE="+cacheDir,
	)
	for _, name := range []string{"GOPATH", "GOMODCACHE"} {
		if value := goEnvValue(t, name); value != "" {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "generated config permission tests must ignore ambient credentials:\n%s", output)
}

// TestGenerate_NoCredsPermsForNonAuthSpec proves the guard is gated on
// shouldEmitAuth(): a spec with no auth persists no token, so shipping the
// check would be dead weight. public-param-names declares auth.type: none.
func TestGenerate_NoCredsPermsForNonAuthSpec(t *testing.T) {
	if testing.Short() {
		t.Skip("generated CLI compile tests run in the full generated-test CI lane")
	}
	t.Parallel()

	apiSpec, err := spec.Parse(filepath.Join("..", "..", "testdata", "golden", "fixtures", "public-param-names.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	_, err = os.Stat(filepath.Join(outputDir, "internal", "cliutil", "creds_perms_eval.go"))
	require.True(t, os.IsNotExist(err), "creds_perms_eval.go must not be emitted for a non-auth spec")

	winSrc := readGeneratedFile(t, outputDir, "internal", "platform", "perms_windows.go")
	require.Contains(t, winSrc, "func verifyPrivatePerms")
	require.NotContains(t, winSrc, "cliutil.VerifyCredsPerms",
		"the no-auth Windows hook must not reference the auth-gated credentials helper")

	cmd := exec.Command("go", "build", "-mod=mod", "./...")
	cmd.Dir = outputDir
	cacheDir, err := goBuildCacheDir(outputDir)
	require.NoError(t, err)
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOCACHE="+cacheDir)
	cmd.Env = append(cmd.Env, sandboxHomeEnv(t)...)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "generated no-auth CLI must build for Windows:\n%s", output)
}
