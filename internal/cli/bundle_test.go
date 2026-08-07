package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePlatform(t *testing.T) {
	t.Run("empty falls back to host", func(t *testing.T) {
		goos, goarch, err := resolvePlatform("")
		require.NoError(t, err)
		assert.Equal(t, runtime.GOOS, goos)
		assert.Equal(t, runtime.GOARCH, goarch)
	})

	t.Run("valid os/arch parses cleanly", func(t *testing.T) {
		goos, goarch, err := resolvePlatform("linux/amd64")
		require.NoError(t, err)
		assert.Equal(t, "linux", goos)
		assert.Equal(t, "amd64", goarch)
	})

	t.Run("missing slash errors with useful message", func(t *testing.T) {
		_, _, err := resolvePlatform("linux-amd64")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "<os>/<arch>")
	})

	t.Run("trailing or leading slash rejected", func(t *testing.T) {
		_, _, err := resolvePlatform("linux/")
		require.Error(t, err)
		_, _, err = resolvePlatform("/amd64")
		require.Error(t, err)
	})
}

func TestBundleBinaryTargetPathUsesWindowsExecutableSuffix(t *testing.T) {
	t.Parallel()

	dir := filepath.Join("tmp", "demo")
	assert.Equal(t,
		filepath.Join("tmp", "demo", "build", "stage", "bin", "demo-pp-cli.exe"),
		bundleBinaryPath(dir, "demo-pp-cli", "windows"),
	)
	assert.Equal(t,
		filepath.Join("tmp", "demo", "build", "stage", "bin", "demo-pp-cli"),
		bundleBinaryPath(dir, "demo-pp-cli", "linux"),
	)
	assert.Equal(t, "demo-pp-cli.exe", bundleBinaryArchiveName("demo-pp-cli", "windows"))
	assert.Equal(t, "demo-pp-cli", bundleBinaryArchiveName("demo-pp-cli", "darwin"))
	assert.Empty(t, bundleBinaryPath(dir, "", "windows"))
	assert.Empty(t, bundleBinaryArchiveName("", "windows"))
}

func TestAutoBundleForHost(t *testing.T) {
	t.Run("no manifest is silent", func(t *testing.T) {
		dir := t.TempDir()
		var out bytes.Buffer
		autoBundleForHost(dir, &out)
		assert.Empty(t, out.String(), "missing manifest should not write any output")
	})

	t.Run("malformed manifest is loud", func(t *testing.T) {
		dir := t.TempDir()
		// Write a manifest.json that exists but is not valid JSON. This is
		// the failure mode the user actually wants to hear about — silently
		// skipping would hide a partial-write or corruption bug.
		require.NoError(t, os.WriteFile(filepath.Join(dir, pipeline.MCPBManifestFilename), []byte("not json"), 0o644))

		var out bytes.Buffer
		autoBundleForHost(dir, &out)
		assert.Contains(t, out.String(), "warning")
		assert.Contains(t, out.String(), "not valid JSON")
	})

	t.Run("empty manifest name is loud", func(t *testing.T) {
		dir := t.TempDir()
		writeBundleManifest(t, dir, pipeline.MCPBManifest{ManifestVersion: pipeline.MCPBManifestVersion})

		var out bytes.Buffer
		autoBundleForHost(dir, &out)
		assert.Contains(t, out.String(), "warning")
		assert.Contains(t, out.String(), "empty name")
	})

	t.Run("missing go.sum is silent", func(t *testing.T) {
		// generate --validate=false intentionally leaves go.sum empty;
		// auto-bundle should not warn about a known-incomplete state.
		dir := t.TempDir()
		writeBundleManifest(t, dir, pipeline.MCPBManifest{
			ManifestVersion: pipeline.MCPBManifestVersion,
			Name:            "demo-pp-mcp",
			Server:          pipeline.MCPBServer{Type: "binary", EntryPoint: "bin/demo-pp-mcp"},
		})

		var out bytes.Buffer
		autoBundleForHost(dir, &out)
		assert.Empty(t, out.String(), "missing go.sum should be a silent skip")
	})
}

func TestNewBundleCmdWindowsPlatformUsesExeNamesInMCPB(t *testing.T) {
	dir := t.TempDir()
	writeBundleManifest(t, dir, pipeline.MCPBManifest{
		ManifestVersion: pipeline.MCPBManifestVersion,
		Name:            "demo-pp-mcp",
		Server: pipeline.MCPBServer{
			Type:       "binary",
			EntryPoint: "bin/demo-pp-mcp",
			MCPConfig:  pipeline.MCPBLaunchSpec{Command: "${__dirname}/bin/demo-pp-mcp"},
		},
	})
	writeCLIManifest(t, dir, pipeline.CLIManifest{CLIName: "demo-pp-cli"})

	mcpBinary := filepath.Join(dir, "mcp.exe")
	cliBinary := filepath.Join(dir, "cli.exe")
	outPath := filepath.Join(dir, "out.mcpb")
	require.NoError(t, os.WriteFile(mcpBinary, []byte("mcp"), 0o755))
	require.NoError(t, os.WriteFile(cliBinary, []byte("cli"), 0o755))

	cmd := newBundleCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		dir,
		"--platform", "windows/amd64",
		"--skip-build",
		"--binary", mcpBinary,
		"--cli-skip-build",
		"--cli-binary", cliBinary,
		"--output", outPath,
	})

	require.NoError(t, cmd.Execute())

	entries := readBundleZipEntries(t, outPath)
	assert.Contains(t, entries, "bin/demo-pp-mcp.exe")
	assert.Contains(t, entries, "bin/demo-pp-cli.exe")
	assert.NotContains(t, entries, "bin/demo-pp-mcp")
	assert.NotContains(t, entries, "bin/demo-pp-cli")

	manifest := readBundleZipManifest(t, outPath)
	assert.Equal(t, "bin/demo-pp-mcp.exe", manifest.Server.EntryPoint)
	assert.Equal(t, "${__dirname}/bin/demo-pp-mcp.exe", manifest.Server.MCPConfig.Command)
}

func TestNewBundleCmdVersionStampsBundledManifest(t *testing.T) {
	dir := t.TempDir()
	writeBundleManifest(t, dir, pipeline.MCPBManifest{
		ManifestVersion: pipeline.MCPBManifestVersion,
		Name:            "demo-pp-mcp",
		Version:         "0.0.0",
		Server: pipeline.MCPBServer{
			Type:       "binary",
			EntryPoint: "bin/demo-pp-mcp",
			MCPConfig:  pipeline.MCPBLaunchSpec{Command: "${__dirname}/bin/demo-pp-mcp"},
		},
	})

	mcpBinary := filepath.Join(dir, "mcp")
	outPath := filepath.Join(dir, "out.mcpb")
	require.NoError(t, os.WriteFile(mcpBinary, []byte("mcp"), 0o755))

	cmd := newBundleCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		dir,
		"--skip-build",
		"--binary", mcpBinary,
		"--output", outPath,
		"--version", "0.1.4",
	})

	require.NoError(t, cmd.Execute())

	manifest := readBundleZipManifest(t, outPath)
	assert.Equal(t, "0.1.4", manifest.Version)

	diskData, err := os.ReadFile(filepath.Join(dir, pipeline.MCPBManifestFilename))
	require.NoError(t, err)
	assert.Contains(t, string(diskData), `"version":"0.0.0"`)
}

func TestNewBundleCmdRejectsInvalidBundleVersion(t *testing.T) {
	for _, tc := range []struct {
		version string
		wantErr string
	}{
		{version: "v0.1.4", wantErr: "--version must not have a v prefix"},
		{version: "latest", wantErr: "--version must be a semantic version"},
		{version: "0.1", wantErr: "--version must be a semantic version"},
	} {
		t.Run(tc.version, func(t *testing.T) {
			dir := t.TempDir()
			writeBundleManifest(t, dir, pipeline.MCPBManifest{
				ManifestVersion: pipeline.MCPBManifestVersion,
				Name:            "demo-pp-mcp",
				Version:         "0.0.0",
				Server: pipeline.MCPBServer{
					Type:       "binary",
					EntryPoint: "bin/demo-pp-mcp",
					MCPConfig:  pipeline.MCPBLaunchSpec{Command: "${__dirname}/bin/demo-pp-mcp"},
				},
			})

			cmd := newBundleCmd()
			cmd.SilenceUsage = true
			cmd.SetArgs([]string{dir, "--version", tc.version})

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestNewBundleCmdWindowsPlatformBuildsToExeStagingPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake go shim uses POSIX shell")
	}

	dir := t.TempDir()
	writeBundleManifest(t, dir, pipeline.MCPBManifest{
		ManifestVersion: pipeline.MCPBManifestVersion,
		Name:            "demo-pp-mcp",
		Server: pipeline.MCPBServer{
			Type:       "binary",
			EntryPoint: "bin/demo-pp-mcp",
			MCPConfig:  pipeline.MCPBLaunchSpec{Command: "${__dirname}/bin/demo-pp-mcp"},
		},
	})
	writeCLIManifest(t, dir, pipeline.CLIManifest{CLIName: "demo-pp-cli"})
	installFakeGo(t)

	outPath := filepath.Join(dir, "out.mcpb")
	cmd := newBundleCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		dir,
		"--platform", "windows/amd64",
		"--output", outPath,
	})

	require.NoError(t, cmd.Execute())
	require.FileExists(t, filepath.Join(dir, "build", "stage", "bin", "demo-pp-mcp.exe"))
	require.FileExists(t, filepath.Join(dir, "build", "stage", "bin", "demo-pp-cli.exe"))

	entries := readBundleZipEntries(t, outPath)
	assert.Contains(t, entries, "bin/demo-pp-mcp.exe")
	assert.Contains(t, entries, "bin/demo-pp-cli.exe")
}

func TestBuildMCPBBinaryUsesReproducibleBuildFlags(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake go shim uses POSIX shell")
	}

	dir := t.TempDir()
	fakeBin := t.TempDir()
	callsPath := filepath.Join(t.TempDir(), "go-calls.txt")
	fakeGo := filepath.Join(fakeBin, "go")
	require.NoError(t, os.WriteFile(fakeGo, []byte(`#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_GO_CALLS"
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    shift
    out="$1"
  fi
  shift || true
done
if [ -z "$out" ]; then
  echo "missing -o" >&2
  exit 2
fi
mkdir -p "$(dirname "$out")"
printf 'fake binary' > "$out"
chmod 755 "$out"
`), 0o755))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GO_CALLS", callsPath)

	require.NoError(t, buildMCPBBinary(dir, "demo-pp-mcp", filepath.Join(dir, "bin", "demo-pp-mcp"), "linux", "amd64"))

	calls, err := os.ReadFile(callsPath)
	require.NoError(t, err)
	assert.Contains(t, string(calls), "build -trimpath -ldflags=-s -w -buildid= -o ")
}

func TestAutoBundleForHostSuccessPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake go shim uses POSIX shell")
	}

	dir := t.TempDir()
	writeBundleManifest(t, dir, pipeline.MCPBManifest{
		ManifestVersion: pipeline.MCPBManifestVersion,
		Name:            "demo-pp-mcp",
		Server: pipeline.MCPBServer{
			Type:       "binary",
			EntryPoint: "bin/demo-pp-mcp",
			MCPConfig:  pipeline.MCPBLaunchSpec{Command: "${__dirname}/bin/demo-pp-mcp"},
		},
	})
	writeCLIManifest(t, dir, pipeline.CLIManifest{CLIName: "demo-pp-cli"})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.sum"), []byte(""), 0o644))
	installFakeGo(t)

	var out bytes.Buffer
	autoBundleForHost(dir, &out)

	archiveName := bundleBinaryArchiveName("demo-pp-mcp", runtime.GOOS)
	outPath := pipeline.DefaultBundleOutputPath(dir, "demo-pp-mcp", runtime.GOOS, runtime.GOARCH)
	assert.Contains(t, out.String(), "Bundled "+outPath)
	require.FileExists(t, filepath.Join(dir, "build", "stage", "bin", archiveName))

	entries := readBundleZipEntries(t, outPath)
	assert.Contains(t, entries, "bin/"+archiveName)
	manifest := readBundleZipManifest(t, outPath)
	assert.Equal(t, "bin/"+archiveName, manifest.Server.EntryPoint)
	assert.Equal(t, "${__dirname}/bin/"+archiveName, manifest.Server.MCPConfig.Command)
}

func writeBundleManifest(t *testing.T, dir string, m pipeline.MCPBManifest) {
	t.Helper()
	data, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, pipeline.MCPBManifestFilename), data, 0o644))
}

func writeCLIManifest(t *testing.T, dir string, m pipeline.CLIManifest) {
	t.Helper()
	data, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, pipeline.CLIManifestFilename), data, 0o644))
}

func readBundleZipEntries(t *testing.T, path string) []string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = zr.Close() })
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names
}

func readBundleZipManifest(t *testing.T, path string) pipeline.MCPBManifest {
	t.Helper()
	zr, err := zip.OpenReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = zr.Close() })
	for _, f := range zr.File {
		if f.Name != pipeline.MCPBManifestFilename {
			continue
		}
		rc, err := f.Open()
		require.NoError(t, err)
		defer func() { _ = rc.Close() }()
		var manifest pipeline.MCPBManifest
		require.NoError(t, json.NewDecoder(rc).Decode(&manifest))
		return manifest
	}
	t.Fatalf("%s not found in %s", pipeline.MCPBManifestFilename, path)
	return pipeline.MCPBManifest{}
}

func installFakeGo(t *testing.T) {
	t.Helper()
	fakeBin := t.TempDir()
	fakeGo := filepath.Join(fakeBin, "go")
	script := `#!/bin/sh
set -eu
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    shift
    out="$1"
  fi
  shift || true
done
if [ -z "$out" ]; then
  echo "missing -o" >&2
  exit 2
fi
mkdir -p "$(dirname "$out")"
printf 'fake binary' > "$out"
chmod 755 "$out"
`
	require.NoError(t, os.WriteFile(fakeGo, []byte(script), 0o755))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestNewBundleCmdMissingManifest(t *testing.T) {
	dir := t.TempDir()
	cmd := newBundleCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{dir})

	err := cmd.Execute()
	require.Error(t, err, "bundle without manifest.json must fail")
	assert.True(t, strings.Contains(err.Error(), "manifest.json") ||
		strings.Contains(err.Error(), "generate"),
		"error should hint at running generate first; got %q", err.Error())
}

func TestNewBundleCmdNotADirectory(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(notADir, []byte("x"), 0o644))

	cmd := newBundleCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{notADir})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}
