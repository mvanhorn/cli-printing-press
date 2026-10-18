// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

// TestGeneratedCredentialTrust proves the printed config and credentials
// loaders honor server origin, refuse an untrusted config as a credential
// route, and read credentials from the descriptor they checked.
func TestGeneratedCredentialTrust(t *testing.T) {
	if testing.Short() {
		t.Skip("generated CLI compile tests run in the full generated-test CI lane")
	}

	apiSpec := minimalSpec("credential-trust")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	credsSrc := readGeneratedFile(t, outputDir, "internal", "cliutil", "credentials.go")
	require.Contains(t, credsSrc, "func CredentialOriginsMatch(")
	require.Contains(t, credsSrc, "readExplicitCredentialBytes(")
	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.Contains(t, configSrc, "untrustedCredentialRoute")
	require.Contains(t, configSrc, "allowsGlobalCredential(")

	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/cliutil", "./internal/config", "-count=1", "-timeout", "180s")
	runGeneratedWindowsCLIUtilBuild(t, outputDir)
	runGeneratedDarwinCLIUtilBuild(t, outputDir)
	proveForeignCredentialOwner(t, outputDir)
}

// proveForeignCredentialOwner runs the emitted owner check as root when this
// process can. A developer machine without passwordless sudo still passes:
// the emitted test skips when it cannot chown, and that skip is not a failure
// of the credential checks. CI with passwordless sudo, or a process that is
// already root, still has to show the foreign-owned file was refused.
func proveForeignCredentialOwner(t *testing.T, outputDir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	bin := filepath.Join(outputDir, "cliutil.test")
	runGoCommandRequired(t, outputDir, "test", "-c", "-o", bin, "./internal/cliutil")
	t.Cleanup(func() { _ = os.Remove(bin) })

	text, passed := runForeignOwnerBinary(t, outputDir, bin)
	if passed {
		return
	}
	if !strings.Contains(text, "SKIP") && !strings.Contains(text, "cannot chown") {
		t.Fatalf("foreign-owner proof failed:\n%s", text)
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		t.Logf("foreign-owner proof not run as root: sudo is not installed")
		return
	}
	probe, err := exec.Command("sudo", "-n", "true").CombinedOutput()
	if err != nil {
		t.Logf("foreign-owner proof not run as root: passwordless sudo is unavailable: %v: %s", err, strings.TrimSpace(string(probe)))
		return
	}
	cmd := exec.Command("sudo", "-n", bin, "-test.count=1", "-test.v", "-test.run", "^TestLoadCredentials_RefusesForeignOwner$")
	cmd.Dir = outputDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "sudo foreign-owner proof failed:\n%s", out)
	rooted := string(out)
	require.NotContains(t, rooted, "SKIP")
	require.NotContains(t, rooted, "cannot chown")
	require.Contains(t, rooted, "PASS")
}

func runForeignOwnerBinary(t *testing.T, outputDir, bin string) (string, bool) {
	t.Helper()
	cmd := exec.Command(bin, "-test.count=1", "-test.v", "-test.run", "^TestLoadCredentials_RefusesForeignOwner$")
	cmd.Dir = outputDir
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err == nil && strings.Contains(text, "PASS") && !strings.Contains(text, "SKIP") {
		return text, true
	}
	return text, false
}
