package generator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/govulncheck"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"
)

// xNetAdvisoryFixedVersion is the golang.org/x/net release that fixes
// GO-2026-6603, GO-2026-6610, GO-2026-6611, GO-2026-6612, and GO-2026-6617.
// It is a test-local literal: comparing the constant and the template to
// each other would stay green if both were lowered together.
const xNetAdvisoryFixedVersion = "v0.60.0"

// slippedXNetVersion is the resolve that cleared the previous v0.56.0 floor.
// golang.org/x/crypto v0.56.0 requires it, so ensureSafeXNet no-op'd and
// left the http2 advisories in the module graph.
const slippedXNetVersion = "v0.57.0"

func TestEnsureSafeXNet(t *testing.T) {
	if testing.Short() {
		t.Skip("ensureSafeXNet runs go get / go mod tidy (network)")
	}

	t.Run("bumps a transitively-old x/net to the safe version", func(t *testing.T) {
		dir := writeXNetModule(t, "xnetbump", "v0.43.0", "golang.org/x/net/html")
		if _, err := runCommand(dir, qualityGateTimeout, "go", "mod", "tidy"); err != nil {
			t.Skipf("initial go mod tidy failed (offline?): %v", err)
		}

		require.NoError(t, ensureSafeXNet(dir))
		assertXNetAtLeastAdvisory(t, dir)

		before, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		require.NoError(t, err)
		_, err = runCommand(dir, qualityGateTimeout, "go", "mod", "tidy")
		require.NoError(t, err)
		after, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		require.NoError(t, err)
		assert.Equal(t, string(before), string(after), "go.mod must be tidy after the bump")
	})

	t.Run("bumps the version that cleared the stale floor", func(t *testing.T) {
		dir := writeXNetModule(t, "xnetslip", slippedXNetVersion, "golang.org/x/net/http2")
		_, err := runCommand(dir, qualityGateTimeout, "go", "mod", "tidy")
		require.NoError(t, err)

		before := listedModuleVersion(t, dir, "golang.org/x/net")
		require.True(t, moduleVersionBelowFloor(before, xNetAdvisoryFixedVersion),
			"fixture must still be inside the http2 advisories, got %s", before)

		require.NoError(t, ensureSafeXNet(dir))
		assertXNetAtLeastAdvisory(t, dir)

		// A second pass is the at-floor negative: the bump must not churn go.mod.
		frozen, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		require.NoError(t, err)
		require.NoError(t, ensureSafeXNet(dir))
		again, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		require.NoError(t, err)
		assert.Equal(t, string(frozen), string(again))
	})

	t.Run("does not downgrade a newer x/net", func(t *testing.T) {
		const newer = "v0.61.0"
		require.True(t, semver.Compare(newer, xNetAdvisoryFixedVersion) > 0)
		dir := writeXNetModule(t, "xnetnewer", newer, "golang.org/x/net/http2")
		_, err := runCommand(dir, qualityGateTimeout, "go", "mod", "tidy")
		require.NoError(t, err)

		before, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		require.NoError(t, err)
		require.NoError(t, ensureSafeXNet(dir))
		after, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		require.NoError(t, err)
		assert.Equal(t, string(before), string(after))
		assert.Equal(t, newer, listedModuleVersion(t, dir, "golang.org/x/net"))
	})

	t.Run("no-op when x/net is not a dependency", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
			[]byte("module noxnet\n\ngo 1.26.0\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
			[]byte("package main\n\nfunc main() {}\n"), 0o644))

		require.NoError(t, ensureSafeXNet(dir))

		gomod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		require.NoError(t, err)
		assert.NotContains(t, string(gomod), "golang.org/x/net")
		_, err = runCommand(dir, qualityGateTimeout, "go", "list", "-m", "-f", "{{.Version}}", "golang.org/x/net")
		require.Error(t, err)
	})
}

func TestEnsureSafeXNetCommandBranches(t *testing.T) {
	t.Parallel()

	t.Run("list failure is not a bump", func(t *testing.T) {
		fake := &fakeGo{listErr: errors.New("module golang.org/x/net: not a known dependency")}
		require.NoError(t, ensureSafeXNetWith(t.TempDir(), fake.run))
		assert.NotContains(t, strings.Join(fake.calls, "\n"), "get")
	})

	t.Run("invalid version is not a bump", func(t *testing.T) {
		fake := &fakeGo{listOut: "not-a-version"}
		require.NoError(t, ensureSafeXNetWith(t.TempDir(), fake.run))
		assert.NotContains(t, strings.Join(fake.calls, "\n"), "get")
	})

	t.Run("empty version is not a bump", func(t *testing.T) {
		fake := &fakeGo{listOut: "\n"}
		require.NoError(t, ensureSafeXNetWith(t.TempDir(), fake.run))
		assert.NotContains(t, strings.Join(fake.calls, "\n"), "get")
	})

	t.Run("version at the floor is not a bump", func(t *testing.T) {
		fake := &fakeGo{listOut: safeXNetVersion}
		require.NoError(t, ensureSafeXNetWith(t.TempDir(), fake.run))
		assert.NotContains(t, strings.Join(fake.calls, "\n"), "get")
	})

	t.Run("version above the floor is not a downgrade", func(t *testing.T) {
		fake := &fakeGo{listOut: "v999.0.0"}
		require.NoError(t, ensureSafeXNetWith(t.TempDir(), fake.run))
		assert.NotContains(t, strings.Join(fake.calls, "\n"), "get")
	})

	t.Run("stderr trailer does not hide a low version", func(t *testing.T) {
		fake := &fakeGo{listOut: slippedXNetVersion + "\ngo: downloading golang.org/x/net " + slippedXNetVersion}
		require.NoError(t, ensureSafeXNetWith(t.TempDir(), fake.run))
		require.Contains(t, strings.Join(fake.calls, "\n"), "get golang.org/x/net@"+safeXNetVersion)
		require.Contains(t, strings.Join(fake.calls, "\n"), "mod tidy")
	})

	t.Run("get failure is returned and tidy is not run", func(t *testing.T) {
		fake := &fakeGo{listOut: slippedXNetVersion, getErr: errors.New("proxy denied")}
		err := ensureSafeXNetWith(t.TempDir(), fake.run)
		require.ErrorContains(t, err, "bumping golang.org/x/net to "+safeXNetVersion)
		require.ErrorContains(t, err, "proxy denied")
		assert.NotContains(t, strings.Join(fake.calls, "\n"), "mod tidy")
	})

	t.Run("tidy failure is returned", func(t *testing.T) {
		fake := &fakeGo{listOut: slippedXNetVersion, tidyErr: errors.New("sum mismatch")}
		err := ensureSafeXNetWith(t.TempDir(), fake.run)
		require.ErrorContains(t, err, "re-running go mod tidy after golang.org/x/net bump")
		require.ErrorContains(t, err, "sum mismatch")
	})
}

func TestSafeXNetVersionMatchesGoModTemplate(t *testing.T) {
	tmpl, err := os.ReadFile(filepath.Join("templates", "go.mod.tmpl"))
	require.NoError(t, err)

	var pins []string
	for line := range strings.SplitSeq(string(tmpl), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "golang.org/x/net" {
			pins = append(pins, fields[1])
		}
	}
	require.Len(t, pins, 1)
	require.False(t, moduleVersionBelowFloor(pins[0], xNetAdvisoryFixedVersion),
		"go.mod.tmpl pin %s is below the http2 advisory fix %s", pins[0], xNetAdvisoryFixedVersion)
	require.False(t, moduleVersionBelowFloor(safeXNetVersion, xNetAdvisoryFixedVersion),
		"safeXNetVersion %s is below the http2 advisory fix %s", safeXNetVersion, xNetAdvisoryFixedVersion)
	require.Equal(t, safeXNetVersion, pins[0],
		"templates/go.mod.tmpl must pin the same x/net floor the post-tidy bump uses")
}

func TestBrowserTransportXNetAdvisoryFloor(t *testing.T) {
	if testing.Short() {
		t.Skip("printed transport floor runs go mod tidy, go test, and govulncheck")
	}

	cases := []struct {
		name      string
		transport string
	}{
		{name: "xnetchrome", transport: spec.HTTPTransportBrowserChrome},
		{name: "xneth2", transport: spec.HTTPTransportBrowserChromeH2},
	}
	for _, tc := range cases {
		t.Run(tc.transport, func(t *testing.T) {
			dir := generateTransportCLI(t, tc.name, tc.transport)
			_, err := runCommand(dir, qualityGateTimeout, "go", "mod", "tidy")
			require.NoError(t, err)
			require.NoError(t, ensureSafeXNet(dir))
			assertXNetAtLeastAdvisory(t, dir)
			frozen, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			require.NoError(t, err)
			_, err = runCommand(dir, qualityGateTimeout, "go", "mod", "tidy")
			require.NoError(t, err)
			tidied, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			require.NoError(t, err)
			require.Equal(t, string(frozen), string(tidied), "browser transport go.mod must stay tidy at the x/net floor")

			testOut, err := runCommandWithEnv(dir, qualityGateTimeout, moduleSandboxEnv(t), "go", "test", "-count=1", "./...")
			require.NoError(t, err, testOut)
			requireGovulncheckClean(t, dir)
		})
	}

	t.Run("standard", func(t *testing.T) {
		dir := generateTransportCLI(t, "xnetstd", spec.HTTPTransportStandard)
		_, err := runCommand(dir, qualityGateTimeout, "go", "mod", "tidy")
		require.NoError(t, err)
		before, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		require.NoError(t, err)

		require.NoError(t, ensureSafeXNet(dir))
		after, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		require.NoError(t, err)
		assert.Equal(t, string(before), string(after))
		assert.NotContains(t, string(after), "golang.org/x/net")

		_, err = runCommand(dir, qualityGateTimeout, "go", "list", "-m", "-f", "{{.Version}}", "golang.org/x/net")
		require.Error(t, err)

		_, err = runCommand(dir, qualityGateTimeout, "go", "mod", "tidy")
		require.NoError(t, err)
		tidied, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		require.NoError(t, err)
		assert.Equal(t, string(after), string(tidied), "standard transport must stay tidy without an x/net require")
	})
}

func writeXNetModule(t *testing.T, module, version, importPath string) string {
	t.Helper()
	dir := t.TempDir()
	gomod := "module " + module + "\n\ngo 1.26.0\n\nrequire golang.org/x/net " + version + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644))
	src := "package main\n\nimport _ \"" + importPath + "\"\n\nfunc main() {}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644))
	return dir
}

func generateTransportCLI(t *testing.T, name, transport string) string {
	t.Helper()
	apiSpec := minimalSpec(name)
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.BaseURL = "https://www.example.com"
	apiSpec.HTTPTransport = transport
	dir := filepath.Join(t.TempDir(), name+"-pp-cli")
	require.NoError(t, New(apiSpec, dir).Generate())
	return dir
}

func listedModuleVersion(t *testing.T, dir, module string) string {
	t.Helper()
	out, err := runCommand(dir, qualityGateTimeout, "go", "list", "-m", "-f", "{{.Version}}", module)
	require.NoError(t, err, out)
	version := moduleListVersion(out)
	require.True(t, semver.IsValid(version), "version %q from %q", version, out)
	return version
}

func assertXNetAtLeastAdvisory(t *testing.T, dir string) {
	t.Helper()
	version := listedModuleVersion(t, dir, "golang.org/x/net")
	require.False(t, moduleVersionBelowFloor(version, xNetAdvisoryFixedVersion),
		"resolved golang.org/x/net %s, advisory floor %s", version, xNetAdvisoryFixedVersion)
	require.False(t, moduleVersionBelowFloor(version, safeXNetVersion),
		"resolved golang.org/x/net %s below safeXNetVersion %s", version, safeXNetVersion)
}

func moduleSandboxEnv(t *testing.T) []string {
	t.Helper()
	env := sandboxHomeEnv(t)
	var home string
	for _, entry := range env {
		value, ok := strings.CutPrefix(entry, "HOME=")
		if ok {
			home = value
			break
		}
	}
	require.NotEmpty(t, home)
	return append(env,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
	)
}

func TestGovulncheckCleanHonorsLiveEnv(t *testing.T) {
	t.Run("unset does not scan", func(t *testing.T) {
		t.Setenv(govulncheck.LiveEnv, "")
		require.False(t, govulncheck.LiveDBEnabled())
		requireGovulncheckClean(t, t.TempDir())
		called := false
		err := govulncheckClean(t, t.TempDir(), false, func(string, time.Duration, []string, string, ...string) (string, error) {
			called = true
			return "", errors.New("scanned")
		})
		require.NoError(t, err)
		assert.False(t, called)
	})

	t.Run("set returns the scan error", func(t *testing.T) {
		t.Setenv(govulncheck.LiveEnv, "1")
		require.True(t, govulncheck.LiveDBEnabled())
		err := govulncheckClean(t, t.TempDir(), true, func(_ string, _ time.Duration, _ []string, name string, args ...string) (string, error) {
			assert.Equal(t, "go", name)
			assert.Equal(t, govulncheck.GoRunArgs("./..."), args)
			return "db unavailable", errors.New("lookup failed")
		})
		require.ErrorContains(t, err, "lookup failed")
		require.ErrorContains(t, err, "db unavailable")
	})
}

func requireGovulncheckClean(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, govulncheckClean(t, dir, govulncheck.LiveDBEnabled(), runCommandWithEnv))
}

func govulncheckClean(t *testing.T, dir string, live bool, run commandWithEnv) error {
	t.Helper()
	if !live {
		t.Logf("SKIP govulncheck ./... (set %s=1 to query the live vuln DB)", govulncheck.LiveEnv)
		return nil
	}
	env := append(moduleSandboxEnv(t), govulncheck.ToolchainEnv(dir)...)
	out, err := run(dir, 8*time.Minute, env, "go", govulncheck.GoRunArgs("./...")...)
	if err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	return nil
}

type commandWithEnv func(dir string, timeout time.Duration, extraEnv []string, name string, args ...string) (string, error)

type fakeGo struct {
	listOut string
	listErr error
	getErr  error
	tidyErr error
	calls   []string
}

func (f *fakeGo) run(_ string, _ time.Duration, _ string, args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	switch {
	case len(args) > 0 && args[0] == "list":
		return f.listOut, f.listErr
	case len(args) > 0 && args[0] == "get":
		if f.getErr != nil {
			return "get failed", f.getErr
		}
		return "", nil
	case len(args) >= 2 && args[0] == "mod" && args[1] == "tidy":
		if f.tidyErr != nil {
			return "tidy failed", f.tidyErr
		}
		return "", nil
	default:
		return "", errors.New("unexpected command")
	}
}
