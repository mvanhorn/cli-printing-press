package pipeline

import (
	"archive/zip"
	"debug/macho"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildTinyMachO cross-compiles a trivial program for one darwin arch so the
// universal-binary merge is exercised against real Mach-O files rather than a
// hand-rolled fixture.
func buildTinyMachO(t *testing.T, dir, goarch string) string {
	t.Helper()
	src := filepath.Join(dir, "main_"+goarch+".go")
	require.NoError(t, os.WriteFile(src, []byte("package main\nfunc main() {}\n"), 0o644))
	out := filepath.Join(dir, "tiny-"+goarch)
	cmd := exec.Command("go", "build", "-o", out, src)
	cmd.Env = append(os.Environ(), "GOOS=darwin", "GOARCH="+goarch, "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot cross-compile darwin/%s here: %v\n%s", goarch, err, b)
	}
	return out
}

func writeMultiPlatformManifest(t *testing.T, dir, name string) {
	t.Helper()
	m := MCPBManifest{
		ManifestVersion: MCPBManifestVersion,
		Name:            name,
		Version:         "1.0.0",
		Description:     "demo",
		Author:          MCPBAuthor{Name: "Test"},
		Server: MCPBServer{
			Type:       mcpbServerTypeBinary,
			EntryPoint: "bin/" + name,
			MCPConfig: MCPBLaunchSpec{
				Command: "${__dirname}/bin/" + name,
				Args:    []string{},
			},
		},
	}
	data, err := json.MarshalIndent(m, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, MCPBManifestFilename), data, 0o644))
}

func bundleMembers(t *testing.T, path string) (map[string]bool, MCPBManifest) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	require.NoError(t, err)
	defer func() { _ = zr.Close() }()
	members := map[string]bool{}
	var manifest MCPBManifest
	for _, f := range zr.File {
		members[f.Name] = true
		if f.Name == MCPBManifestFilename {
			rc, err := f.Open()
			require.NoError(t, err)
			require.NoError(t, json.NewDecoder(rc).Decode(&manifest))
			_ = rc.Close()
		}
	}
	return members, manifest
}

// TestValidateMCPBBundleCatchesTheShippedDefect reproduces mvanhorn/cli-printing-press#3547
// finding 2 exactly: a bundle whose manifest command names bin/<name> while the
// archive carries only arch-suffixed binaries. Claude Desktop installs such an
// extension, prompts for credentials, and only fails at the first tool call, so
// nothing downstream catches it. The validator must.
func TestValidateMCPBBundleCatchesTheShippedDefect(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "broken.mcpb")

	manifest := MCPBManifest{
		ManifestVersion: MCPBManifestVersion,
		Name:            "demo-mcp",
		Server: MCPBServer{
			Type:       mcpbServerTypeBinary,
			EntryPoint: "bin/demo-mcp-darwin-arm64",
			MCPConfig:  MCPBLaunchSpec{Command: "${__dirname}/bin/demo-mcp"},
		},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, err)

	f, err := os.Create(out)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	require.NoError(t, writeZipBytes(zw, MCPBManifestFilename, data, 0o644))
	// Exactly what the release ships: arch-suffixed names only.
	for _, n := range []string{"bin/demo-mcp-darwin-arm64", "bin/demo-mcp-darwin-amd64", "bin/demo-mcp-windows-amd64.exe"} {
		require.NoError(t, writeZipBytes(zw, n, []byte("binary"), 0o755))
	}
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	err = ValidateMCPBBundle(out)
	require.Error(t, err, "a bundle whose command names a member it does not contain must not validate")
	assert.Contains(t, err.Error(), "bin/demo-mcp")
	assert.Contains(t, err.Error(), "fail at the first tool call")
}

func TestValidateMCPBBundleAcceptsAResolvableBundle(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "ok.mcpb")
	manifest := MCPBManifest{
		ManifestVersion: MCPBManifestVersion,
		Name:            "demo-mcp",
		Server: MCPBServer{
			Type:       mcpbServerTypeBinary,
			EntryPoint: "bin/demo-mcp-darwin-arm64",
			MCPConfig: MCPBLaunchSpec{
				Command: "${__dirname}/bin/demo-mcp-darwin-arm64",
				PlatformOverrides: map[string]MCPBLaunchOverride{
					"win32": {Command: "${__dirname}/bin/demo-mcp-windows-amd64.exe"},
				},
			},
		},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, err)
	f, err := os.Create(out)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	require.NoError(t, writeZipBytes(zw, MCPBManifestFilename, data, 0o644))
	require.NoError(t, writeZipBytes(zw, "bin/demo-mcp-darwin-arm64", []byte("b"), 0o755))
	require.NoError(t, writeZipBytes(zw, "bin/demo-mcp-windows-amd64.exe", []byte("b"), 0o755))
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	require.NoError(t, ValidateMCPBBundle(out), "every declared command resolves; this must pass")
}

// TestValidateMCPBBundleCatchesABrokenOverride proves the check is not satisfied
// by the base command alone - a per-OS override pointing at a missing member is
// broken for exactly the users on that OS.
func TestValidateMCPBBundleCatchesABrokenOverride(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "override.mcpb")
	manifest := MCPBManifest{
		ManifestVersion: MCPBManifestVersion,
		Name:            "demo-mcp",
		Server: MCPBServer{
			Type:       mcpbServerTypeBinary,
			EntryPoint: "bin/demo-mcp-darwin-arm64",
			MCPConfig: MCPBLaunchSpec{
				Command: "${__dirname}/bin/demo-mcp-darwin-arm64",
				PlatformOverrides: map[string]MCPBLaunchOverride{
					"win32": {Command: "${__dirname}/bin/demo-mcp-windows-amd64.exe"},
				},
			},
		},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, err)
	f, err := os.Create(out)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	require.NoError(t, writeZipBytes(zw, MCPBManifestFilename, data, 0o644))
	require.NoError(t, writeZipBytes(zw, "bin/demo-mcp-darwin-arm64", []byte("b"), 0o755))
	// the win32 binary is deliberately absent
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	err = ValidateMCPBBundle(out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "platform_overrides.win32.command")
}

func TestBuildMCPBMultiPlatformBundle(t *testing.T) {
	dir := t.TempDir()
	writeMultiPlatformManifest(t, dir, "demo-mcp")

	amd64 := buildTinyMachO(t, dir, "amd64")
	arm64 := buildTinyMachO(t, dir, "arm64")
	win := filepath.Join(dir, "win.exe")
	require.NoError(t, os.WriteFile(win, []byte("MZ windows binary"), 0o755))

	out := filepath.Join(dir, "build", "demo.mcpb")
	require.NoError(t, BuildMCPBMultiPlatformBundle(BundleParams{
		CLIDir:     dir,
		OutputPath: out,
		Version:    "2.3.4",
	}, []BundleBinary{
		{GOOS: "darwin", GOARCH: "amd64", MCPPath: amd64},
		{GOOS: "darwin", GOARCH: "arm64", MCPPath: arm64},
		{GOOS: "windows", GOARCH: "amd64", MCPPath: win},
	}))

	members, manifest := bundleMembers(t, out)

	t.Run("every declared command resolves inside the bundle", func(t *testing.T) {
		// BuildMCPBMultiPlatformBundle validates before returning, so reaching
		// here already proves it; assert explicitly so the intent is recorded.
		require.NoError(t, ValidateMCPBBundle(out))
	})

	t.Run("darwin is served by one universal binary, not a single arch", func(t *testing.T) {
		assert.True(t, members["bin/demo-mcp-darwin"], "expected a merged darwin binary, members: %v", members)
		assert.Equal(t, "${__dirname}/bin/demo-mcp-darwin",
			manifest.Server.MCPConfig.PlatformOverrides["darwin"].Command)
	})

	t.Run("the universal binary really is a fat Mach-O carrying both slices", func(t *testing.T) {
		extracted := filepath.Join(dir, "extracted-darwin")
		zr, err := zip.OpenReader(out)
		require.NoError(t, err)
		defer func() { _ = zr.Close() }()
		var wrote bool
		for _, f := range zr.File {
			if f.Name != "bin/demo-mcp-darwin" {
				continue
			}
			rc, err := f.Open()
			require.NoError(t, err)
			data := make([]byte, f.UncompressedSize64)
			_, err = readFull(rc, data)
			_ = rc.Close()
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(extracted, data, 0o755))
			wrote = true
		}
		require.True(t, wrote, "universal darwin binary missing from the bundle")

		fat, err := macho.OpenFat(extracted)
		require.NoError(t, err, "the merged file must be a valid universal Mach-O")
		defer func() { _ = fat.Close() }()
		require.Len(t, fat.Arches, 2)
		cpus := map[macho.Cpu]bool{}
		for _, a := range fat.Arches {
			cpus[a.Cpu] = true
		}
		assert.True(t, cpus[macho.CpuAmd64], "missing the x86_64 slice")
		assert.True(t, cpus[macho.CpuArm64], "missing the arm64 slice")
	})

	t.Run("win32 gets its own override", func(t *testing.T) {
		assert.Equal(t, "${__dirname}/bin/demo-mcp-windows-amd64.exe",
			manifest.Server.MCPConfig.PlatformOverrides["win32"].Command)
		assert.True(t, members["bin/demo-mcp-windows-amd64.exe"])
	})

	t.Run("version is stamped", func(t *testing.T) {
		assert.Equal(t, "2.3.4", manifest.Version)
	})
}

// TestBuildMCPBMultiPlatformBundleSingleDarwinArch covers the case where only one
// darwin slice exists: there is nothing to merge, so the override must name that
// arch directly and still resolve.
func TestBuildMCPBMultiPlatformBundleSingleDarwinArch(t *testing.T) {
	dir := t.TempDir()
	writeMultiPlatformManifest(t, dir, "demo-mcp")
	only := filepath.Join(dir, "only")
	require.NoError(t, os.WriteFile(only, []byte("binary"), 0o755))

	out := filepath.Join(dir, "one.mcpb")
	require.NoError(t, BuildMCPBMultiPlatformBundle(BundleParams{CLIDir: dir, OutputPath: out},
		[]BundleBinary{{GOOS: "darwin", GOARCH: "arm64", MCPPath: only}}))

	members, manifest := bundleMembers(t, out)
	assert.False(t, members["bin/demo-mcp-darwin"], "nothing to merge, so no universal binary should be written")
	assert.Equal(t, "${__dirname}/bin/demo-mcp-darwin-arm64",
		manifest.Server.MCPConfig.PlatformOverrides["darwin"].Command)
	require.NoError(t, ValidateMCPBBundle(out))
}

func readFull(r interface{ Read([]byte) (int, error) }, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			if total == len(buf) {
				return total, nil
			}
			return total, err
		}
	}
	return total, nil
}
