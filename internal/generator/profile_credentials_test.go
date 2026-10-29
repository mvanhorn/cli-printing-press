package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

func TestExplicitProfileCredentialsRoundTrip(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("profile-cred-path")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cliutil", "-count=1",
		"-run", "TestExplicitProfile|TestSaveExplicit|TestRemoveExplicit|TestDefaultLoginLogout|TestExplicitConfigEnv")

	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			env := []string{"GOOS=" + goos, "GOARCH=amd64", "CGO_ENABLED=0"}
			runGoCommandWithEnv(t, outputDir, env, "vet", "-mod=mod", "./internal/cliutil", "./internal/config")
			runGoCommandWithEnv(t, outputDir, env, "build", "./internal/cliutil", "./internal/config")
		})
	}
}
