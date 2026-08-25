package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedSetTokenReadsStdinAndRejectsArgv(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("set-token-stdin-bearer")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		Format:  "Bearer {token}",
		EnvVars: []string{"SET_TOKEN_STDIN_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	require.Contains(t, authSrc, `"set-token"`)
	require.Contains(t, authSrc, "cobra.NoArgs")
	require.Contains(t, authSrc, "readSecretFromStdin(cmd.InOrStdin())")
	require.NotContains(t, authSrc, `"set-token <token>"`)
	require.NotContains(t, authSrc, `"set-token <jwt>"`)
	setTokenIdx := strings.Index(authSrc, "func newAuthSetTokenCmd")
	require.NotEqual(t, -1, setTokenIdx)
	setTokenFn := authSrc[setTokenIdx:]
	if next := strings.Index(setTokenFn[1:], "\nfunc "); next != -1 {
		setTokenFn = setTokenFn[:next+1]
	}
	require.Contains(t, setTokenFn, "cobra.NoArgs")
	require.NotContains(t, setTokenFn, "cobra.ExactArgs")
	require.NotContains(t, setTokenFn, "args[0]")

	helpersSrc := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	require.Contains(t, helpersSrc, "func readSecretFromStdin(")

	binPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommandRequired(t, outputDir, "build", "-o", binPath, "./cmd/"+naming.CLI(apiSpec.Name))

	home := t.TempDir()
	env := append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, "config"),
		"XDG_DATA_HOME="+filepath.Join(home, "data"),
	)

	positional := exec.Command(binPath, "auth", "set-token", "argv-secret")
	positional.Env = env
	out, err := positional.CombinedOutput()
	require.Error(t, err, "positional set-token must fail: %s", out)
	require.NotContains(t, string(out), "Token saved")

	stdinCmd := exec.Command(binPath, "auth", "set-token")
	stdinCmd.Stdin = strings.NewReader("stdin-secret\n")
	stdinCmd.Env = env
	out, err = stdinCmd.CombinedOutput()
	require.NoError(t, err, "stdin set-token failed: %s", out)
	require.Contains(t, string(out), "Token saved")

	persisted, err := os.ReadFile(filepath.Join(home, "data", naming.CLI(apiSpec.Name), "credentials.toml"))
	require.NoError(t, err)
	require.Contains(t, string(persisted), "stdin-secret")
	require.NotContains(t, string(persisted), "argv-secret")
}

func TestAuthSetTokenStdinCommandHasNoPositionalToken(t *testing.T) {
	t.Parallel()

	got := authSetTokenStdinCommand("demo")
	require.Equal(t, `echo "$TOKEN" | demo-pp-cli auth set-token`, got)
	require.NotContains(t, got, "YOUR_TOKEN_HERE")
	require.NotContains(t, got, "<token>")
}
