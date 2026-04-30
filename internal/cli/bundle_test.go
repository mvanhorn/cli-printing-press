package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/pipeline"
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

func writeBundleManifest(t *testing.T, dir string, m pipeline.MCPBManifest) {
	t.Helper()
	data, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, pipeline.MCPBManifestFilename), data, 0o644))
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
