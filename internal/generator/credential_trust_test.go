// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	proveForeignCredentialOwner(t, outputDir)
}

// proveForeignCredentialOwner runs the emitted owner check as root. The
// emitted test skips when it cannot chown, which is not proof that a
// foreign-owned 0600 file was refused.
func proveForeignCredentialOwner(t *testing.T, outputDir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	bin := filepath.Join(outputDir, "cliutil.test")
	runGoCommandRequired(t, outputDir, "test", "-c", "-o", bin, "./internal/cliutil")
	cmd := exec.Command("sudo", "-n", bin, "-test.count=1", "-test.v", "-test.run", "^TestLoadCredentials_RefusesForeignOwner$")
	cmd.Dir = outputDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "sudo foreign-owner proof failed:\n%s", out)
	text := string(out)
	require.NotContains(t, text, "SKIP")
	require.NotContains(t, text, "cannot chown")
	require.Contains(t, text, "PASS")
	_ = os.Remove(bin)
}
