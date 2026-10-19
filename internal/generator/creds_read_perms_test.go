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
	require.Contains(t, evalSrc, "func evalCredsSecurityResolved", "evaluator must resolve machine-local SDDL aliases before comparing trustees")
	require.Contains(t, evalSrc, "NO_ACCESS_CONTROL", "NULL DACL must be rejected from DACL flags, not only a prefix")
	require.Contains(t, evalSrc, "func trusteeSID", "trustees must be compared as SIDs")

	winSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "creds_perms_windows.go")
	require.Contains(t, winSrc, "func VerifyCredsPerms", "Windows read-time guard must be emitted")
	require.Contains(t, winSrc, "func resolveSDDLAlias", "Windows shell must resolve machine-local aliases such as LA")
	require.Contains(t, winSrc, "evalCredsSecurityResolved(sd.String(), me, resolveSDDLAlias)", "Windows guard must pass the alias resolver into the evaluator")

	winTestSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "creds_perms_windows_test.go")
	require.Contains(t, winTestSrc, "//go:build windows", "Windows resolver test must be Windows-only")
	require.Contains(t, winTestSrc, `resolveSDDLAlias("LA")`, "Windows resolver test must call the real LA resolver")
	require.Contains(t, winTestSrc, `resolveSDDLAlias("BA")`, "Windows resolver test must call the real BA resolver")
	require.Contains(t, winTestSrc, `resolveSDDLAlias("SY")`, "Windows resolver test must call the real SY resolver")
	require.Contains(t, winTestSrc, "LookupAccount", "Windows resolver test must check the resolved account")

	unixSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "creds_perms_unix.go")
	require.Contains(t, unixSrc, "func VerifyCredsPerms", "POSIX read-time guard must be emitted")
	require.Contains(t, unixSrc, "unix.Openat", "POSIX openat must use x/sys/unix so darwin builds")
	require.NotContains(t, unixSrc, "syscall.Openat", "darwin's syscall package has no Openat")
	require.Contains(t, unixSrc, "O_NOFOLLOW", "POSIX credentials open must refuse a swapped-in symlink")
	require.Contains(t, unixSrc, "os.Geteuid()", "POSIX credentials open must check the owner on the descriptor it reads")
	require.Contains(t, unixSrc, "rejectDarwinForeignFile", "an already-open credential descriptor must be the one whose ACL is judged")
	require.Contains(t, unixSrc, "darwinACLScopeDir", "credential directories must be judged for a foreign ACL too")
	aclSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "darwin_acl.go")
	require.Contains(t, aclSrc, "chmod -N", "an ACL refusal must name the flag that removes it")
	require.Contains(t, aclSrc, "activeDarwinACLReader", "Linux must be able to stub the darwin reader")
	darwinSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "darwin_acl_darwin.go")
	require.Contains(t, darwinSrc, "unix.SYS_FGETATTRLIST", "the darwin ACL read must be the raw fgetattrlist syscall")
	require.Contains(t, darwinSrc, "FSOPT_REPORT_FULLSIZE", "a short buffer must be detected from the reported size")
	require.Contains(t, unixSrc, "func ReadOpenedConfig", "POSIX config open must judge and read one descriptor")
	require.Contains(t, winSrc, "windows.GetSecurityInfo(", "Windows credentials open must check the ACL on the handle it reads")
	require.Contains(t, winSrc, "func ReadOpenedConfig", "Windows config open must judge and read one handle")

	evalTestSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "creds_perms_eval_test.go")
	require.Contains(t, evalTestSrc, "PAINO_ACCESS_CONTROL", "evaluator tests must reject a flags-first NULL DACL")
	require.Contains(t, evalTestSrc, "builtin admin owner LA", "evaluator tests must accept the built-in Administrator's own ACE")

	// A3: the read-time guard must be wired into config.Load's read path. The
	// permission verdict and the bytes come from the same open file, and a
	// loose config must not steer credentials at a different base URL.
	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.Contains(t, configSrc, "cliutil.ReadOpenedConfig(", "config.Load must judge and read the same open config file")
	require.Contains(t, configSrc, "untrustedCredentialRoute", "a loose config must not steer credentials to a different base URL")
	require.Contains(t, configSrc, "c.TokenURL = \"\"", "a loose config must not keep an OAuth token endpoint override")
	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	require.Contains(t, clientSrc, "CredentialRouteBlocked()", "client credential mint must honor a blocked route before reading env secrets")
	require.NotContains(t, configSrc, "cliutil.VerifyCredsPerms(", "config.Load must not check a path separately from the bytes it reads")
	require.Contains(t, configSrc, "CredentialRefusals []cliutil.CredentialRefusal", "config.Load must preserve refused credential state")
	require.Contains(t, configSrc, "func (c *Config) HasCredentialRefusals() bool", "diagnostics must distinguish refused credentials from absence")
	require.Contains(t, configSrc, "credentialsRefused := false", "explicit config credential refusals must not fall through to another credentials home")

	_, err = os.Stat(filepath.Join(outputDir, "internal", "config", "config_perms_test.go"))
	require.NoError(t, err, "config.Load read-time perms behavioral test must be emitted")
	configPermsSrc := readGeneratedFile(t, outputDir, "internal", "config", "config_perms_test.go")
	require.Contains(t, configPermsSrc, "internal/cliutil/testenv", "config permission tests must use the shared path sandbox")
	require.Contains(t, configPermsSrc, "func isolateCredentials", "config permission tests must isolate every credential source, not only env vars")
	require.NotContains(t, configPermsSrc, "func clearCredEnv", "the env-only helper name invited skipping the credentials store")
	require.Contains(t, configPermsSrc, "cliutil.SetHomeOverride(\"\")", "config permission tests must reset --home before isolating")
	require.Contains(t, configPermsSrc, "testenv.Isolate(t, cliutil.ConfigDir, cliutil.DataDir, cliutil.StateDir, cliutil.CacheDir)", "config permission tests must isolate every user-directory lookup Load can use")
	require.Contains(t, configPermsSrc, "t.Setenv(\""+naming.EnvPrefix(apiSpec.Name)+"_HOME\", home)", "config permission tests must point LoadCredentials at the sandbox via PREFIX_HOME")
	require.Contains(t, configPermsSrc, "TestLoad_DoesNotRecordRefusalForCredentialFreeLooseConfig", "config permission tests must prove loose credential-free configs are not recorded as refused")
	require.Contains(t, configPermsSrc, "TestLoad_LooseConfigDropsOAuthEndpointOverrides", "a loose config must drop token endpoint overrides even when base_url matches")
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
	runGoCommand(t, outputDir, "test", "./internal/cliutil", "-run", "TestEvalCredsSecurity|TestDarwinACL", "-count=1")
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestLoad_LooseConfigDropsOAuthEndpointOverrides", "-count=1")
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestLooseConfigDoesNotMintClientCredentials", "-count=1")
	runGeneratedWindowsCLIUtilBuild(t, outputDir)
	runGeneratedDarwinCLIUtilBuild(t, outputDir)
	runGeneratedPrivatePermsCrossCompile(t, outputDir)

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

func runGeneratedWindowsCLIUtilBuild(t *testing.T, outputDir string) {
	t.Helper()
	if testing.Short() {
		t.Skip("generated CLI compile tests run in the full generated-test CI lane")
	}
	runGeneratedWindowsGo(t, outputDir, "build", "./internal/cliutil")
	// go build skips *_test.go, so a resolver test that does not compile would
	// still leave this check green. Compile and vet the Windows test binary.
	bin := filepath.Join(t.TempDir(), "cliutil.test.exe")
	runGeneratedWindowsGo(t, outputDir, "test", "-c", "-o", bin, "./internal/cliutil")
	runGeneratedWindowsGo(t, outputDir, "vet", "./internal/cliutil")
}

func runGeneratedDarwinCLIUtilBuild(t *testing.T, outputDir string) {
	t.Helper()
	if testing.Short() {
		t.Skip("generated CLI compile tests run in the full generated-test CI lane")
	}
	bin := filepath.Join(t.TempDir(), "cliutil.test")
	runGeneratedCrossGo(t, outputDir, "darwin", "arm64", "test", "-c", "-o", bin, "./internal/cliutil")
}

func runGeneratedPrivatePermsCrossCompile(t *testing.T, outputDir string) {
	t.Helper()
	if testing.Short() {
		t.Skip("generated CLI compile tests run in the full generated-test CI lane")
	}
	targets := []struct {
		goos, goarch string
	}{
		{"darwin", "amd64"},
		{"darwin", "arm64"},
		{"linux", "amd64"},
		{"windows", "amd64"},
	}
	for _, target := range targets {
		for _, pkg := range []string{"./internal/cliutil/", "./internal/platform/"} {
			runGeneratedCrossGo(t, outputDir, target.goos, target.goarch, "vet", pkg)
			runGeneratedCrossGo(t, outputDir, target.goos, target.goarch, "build", pkg)
		}
	}
}

func runGeneratedWindowsGo(t *testing.T, outputDir string, args ...string) {
	t.Helper()
	runGeneratedCrossGo(t, outputDir, "windows", "amd64", args...)
}

func runGeneratedCrossGo(t *testing.T, outputDir, goos, goarch string, args ...string) {
	t.Helper()
	goArgs := append([]string{args[0], "-mod=mod"}, args[1:]...)
	cmd := exec.Command("go", goArgs...)
	cmd.Dir = outputDir
	var output []byte
	err := withGoBuildCache(outputDir, func(cacheDir string) error {
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0", "GOCACHE="+cacheDir)
		cmd.Env = append(cmd.Env, sandboxHomeEnv(t)...)
		var runErr error
		output, runErr = cmd.CombinedOutput()
		return runErr
	})
	require.NoError(t, err, "go %s for generated %s/%s cliutil failed:\n%s", strings.Join(args, " "), goos, goarch, output)
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

	t.Run("defaultHome", func(t *testing.T) {
		runGeneratedConfigPermsAgainstDecoy(t, apiSpec, outputDir, false)
	})
	t.Run("explicitDataDirOverride", func(t *testing.T) {
		runGeneratedConfigPermsAgainstDecoy(t, apiSpec, outputDir, true)
	})
}

func TestGeneratedConfigPermissionTestsFailWhenIsolationRemoved(t *testing.T) {
	if testing.Short() {
		t.Skip("generated CLI compile tests run in the full generated-test CI lane")
	}
	t.Parallel()

	apiSpec, err := openapi.ParseFile(filepath.Join("..", "..", "testdata", "golden", "fixtures", "golden-api-oauth2-cc.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	permsPath := filepath.Join(outputDir, "internal", "config", "config_perms_test.go")
	src := readGeneratedFile(t, outputDir, "internal", "config", "config_perms_test.go")
	envPrefix := naming.EnvPrefix(apiSpec.Name)
	stripped := strings.Replace(src,
		"restore, err := cliutil.SetHomeOverride(\"\")\n\tif err != nil {\n\t\tt.Fatalf(\"reset home override: %v\", err)\n\t}\n\tt.Cleanup(restore)\n\thome := testenv.Isolate(t, cliutil.ConfigDir, cliutil.DataDir, cliutil.StateDir, cliutil.CacheDir)\n\tt.Setenv(\""+envPrefix+"_HOME\", home)\n",
		"",
		1)
	require.NotEqual(t, src, stripped, "expected to remove credentials-store isolation from the generated helper")
	stripped = dropGeneratedImportLines(stripped, "/internal/cliutil")
	require.NoError(t, os.WriteFile(permsPath, []byte(stripped), 0o644))

	output, err := runGeneratedConfigPermsAgainstDecoyErr(t, apiSpec, outputDir, false)
	require.Error(t, err, "stripping PREFIX_HOME isolation must let the decoy credentials file fail the suite:\n%s", output)
}

const generatedConfigPermsDecoy = "access_token = \"AMBIENT-CREDENTIAL\"\nrefresh_token = \"AMBIENT-REFRESH\"\n"

func runGeneratedConfigPermsAgainstDecoy(t *testing.T, apiSpec *spec.APISpec, outputDir string, setDataDirOverride bool) {
	t.Helper()
	output, err := runGeneratedConfigPermsAgainstDecoyErr(t, apiSpec, outputDir, setDataDirOverride)
	require.NoError(t, err, "generated config permission tests must ignore ambient credentials:\n%s", output)
}

func runGeneratedConfigPermsAgainstDecoyErr(t *testing.T, apiSpec *spec.APISpec, outputDir string, setDataDirOverride bool) ([]byte, error) {
	t.Helper()

	envPrefix := naming.EnvPrefix(apiSpec.Name)
	operatorHome := t.TempDir()
	credentialsPath := filepath.Join(operatorHome, ".local", "share", naming.CLI(apiSpec.Name), "credentials.toml")
	ambientDataDir := filepath.Dir(credentialsPath)
	ambientXDGDataHome := filepath.Dir(ambientDataDir)
	require.NoError(t, os.MkdirAll(filepath.Dir(credentialsPath), 0o700))
	require.NoError(t, os.WriteFile(credentialsPath, []byte(generatedConfigPermsDecoy), 0o600))

	cmd := exec.Command("go", "test", "-mod=mod", "./internal/config", "-run", "TestLoad_", "-count=1")
	cmd.Dir = outputDir
	var output []byte
	err := withGoBuildCache(outputDir, func(cacheDir string) error {
		cmd.Env = append(os.Environ(),
			"HOME="+operatorHome,
			"USERPROFILE="+operatorHome,
			envPrefix+"_HOME=",
			envPrefix+"_CONFIG=",
			envPrefix+"_CONFIG_DIR="+filepath.Join(operatorHome, ".config"),
			envPrefix+"_STATE_DIR="+filepath.Join(operatorHome, ".local", "state"),
			envPrefix+"_CACHE_DIR="+filepath.Join(operatorHome, ".cache"),
			"XDG_CONFIG_HOME="+filepath.Join(operatorHome, ".config"),
			"XDG_DATA_HOME="+ambientXDGDataHome,
			"XDG_STATE_HOME="+filepath.Join(operatorHome, ".local", "state"),
			"XDG_CACHE_HOME="+filepath.Join(operatorHome, ".cache"),
			"GOCACHE="+cacheDir,
		)
		if setDataDirOverride {
			cmd.Env = append(cmd.Env, envPrefix+"_DATA_DIR="+ambientDataDir)
		} else {
			cmd.Env = append(cmd.Env, envPrefix+"_DATA_DIR=")
		}
		for _, name := range []string{"GOPATH", "GOMODCACHE"} {
			if value := goEnvValue(t, name); value != "" {
				cmd.Env = append(cmd.Env, name+"="+value)
			}
		}
		var runErr error
		output, runErr = cmd.CombinedOutput()
		return runErr
	})
	after, readErr := os.ReadFile(credentialsPath)
	require.NoError(t, readErr)
	require.Equal(t, generatedConfigPermsDecoy, string(after), "generated tests must not read or rewrite the decoy credentials file")
	return output, err
}

func dropGeneratedImportLines(src, needle string) string {
	lines := strings.Split(src, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, needle) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
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
	_, err = os.Stat(filepath.Join(outputDir, "internal", "cliutil", "creds_perms_windows_test.go"))
	require.True(t, os.IsNotExist(err), "Windows resolver test must not be emitted for a non-auth spec")

	winSrc := readGeneratedFile(t, outputDir, "internal", "platform", "perms_windows.go")
	require.Contains(t, winSrc, "func verifyPrivatePerms")
	require.NotContains(t, winSrc, "cliutil.VerifyCredsPerms",
		"the no-auth Windows hook must not reference the auth-gated credentials helper")

	cmd := exec.Command("go", "build", "-mod=mod", "./...")
	cmd.Dir = outputDir
	var output []byte
	err = withGoBuildCache(outputDir, func(cacheDir string) error {
		cmd.Env = append(os.Environ(), "GOOS=windows", "GOCACHE="+cacheDir)
		cmd.Env = append(cmd.Env, sandboxHomeEnv(t)...)
		var runErr error
		output, runErr = cmd.CombinedOutput()
		return runErr
	})
	require.NoError(t, err, "generated no-auth CLI must build for Windows:\n%s", output)
}
