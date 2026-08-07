package pipeline

import (
	"archive/zip"
	"bytes"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/platform"
)

// refreshPromoteArtifacts makes the host-platform staged binaries and MCPB
// bundle match the printed CLI source before promotion. It returns false when
// every artifact is already current so an unchanged promote remains a no-op.
func refreshPromoteArtifacts(cliDir, cliName string) (bool, error) {
	if err := WriteMCPBManifest(cliDir); err != nil {
		return false, fmt.Errorf("refreshing MCPB manifest: %w", err)
	}
	manifest, ok, err := readPromoteMCPBManifest(cliDir)
	if err != nil || !ok {
		return false, err
	}

	cmdDir, err := findCLICommandDir(cliDir)
	if err != nil {
		return false, fmt.Errorf("finding CLI command directory: %w", err)
	}
	newestSource, found, err := newestLiveCheckSourceUnder(cliDir)
	if err != nil {
		return false, fmt.Errorf("checking source freshness: %w", err)
	}
	for _, buildDir := range []string{cmdDir, filepath.Join(cliDir, "cmd", manifest.Name)} {
		graphNewest, graphFound, err := newestLiveCheckBuildGraphModTime(buildDir)
		if err != nil {
			return false, fmt.Errorf("checking %s build graph freshness: %w", filepath.Base(buildDir), err)
		}
		if graphFound && (!found || graphNewest.After(newestSource)) {
			newestSource = graphNewest
			found = true
		}
	}
	if !found {
		return false, nil
	}

	cliArchiveName := platform.ExecutablePathForGOOS(cliName, runtime.GOOS)
	mcpArchiveName := platform.ExecutablePathForGOOS(manifest.Name, runtime.GOOS)
	cliBinary := StagedMCPBinaryPath(cliDir, cliArchiveName)
	mcpBinary := StagedMCPBinaryPath(cliDir, mcpArchiveName)
	refreshed := false
	if !fileCurrentAt(cliBinary, newestSource, runtime.GOOS, runtime.GOARCH) || !fileCurrentAt(mcpBinary, newestSource, runtime.GOOS, runtime.GOARCH) {
		if err := os.MkdirAll(filepath.Dir(cliBinary), 0o755); err != nil {
			return false, fmt.Errorf("creating staged binary directory: %w", err)
		}
		if err := rebuildPromoteBinaryPair(cliDir, manifest.Name, cliBinary, mcpBinary); err != nil {
			return false, err
		}
		refreshed = true
	}

	bundleRefreshed, err := syncPromoteBundle(cliDir, cliName)
	if err != nil {
		return false, err
	}
	return refreshed || bundleRefreshed, nil
}

// syncPromoteBundle repacks only when the host MCPB's manifest or binaries
// differ from the files that promotion is about to publish.
func syncPromoteBundle(cliDir, cliName string) (bool, error) {
	manifest, ok, err := readPromoteMCPBManifest(cliDir)
	if err != nil || !ok {
		return false, err
	}
	cliArchiveName := platform.ExecutablePathForGOOS(cliName, runtime.GOOS)
	mcpArchiveName := platform.ExecutablePathForGOOS(manifest.Name, runtime.GOOS)
	cliBinary := StagedMCPBinaryPath(cliDir, cliArchiveName)
	mcpBinary := StagedMCPBinaryPath(cliDir, mcpArchiveName)
	bundlePath := DefaultBundleOutputPath(cliDir, manifest.Name, runtime.GOOS, runtime.GOARCH)

	matches, err := promoteBundleMatches(bundlePath, cliDir, mcpArchiveName, mcpBinary, cliArchiveName, cliBinary)
	if err != nil {
		return false, err
	}
	if matches {
		return false, nil
	}
	if err := BuildMCPBBundle(BundleParams{
		CLIDir:        cliDir,
		BinaryPath:    mcpBinary,
		BinaryName:    mcpArchiveName,
		CLIBinaryName: cliArchiveName,
		CLIBinaryPath: cliBinary,
		OutputPath:    bundlePath,
	}); err != nil {
		return false, fmt.Errorf("repacking MCPB bundle: %w", err)
	}
	return true, nil
}

func readPromoteMCPBManifest(cliDir string) (MCPBManifest, bool, error) {
	data, err := os.ReadFile(filepath.Join(cliDir, MCPBManifestFilename))
	if os.IsNotExist(err) {
		return MCPBManifest{}, false, nil
	}
	if err != nil {
		return MCPBManifest{}, false, fmt.Errorf("reading MCPB manifest: %w", err)
	}
	var manifest MCPBManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return MCPBManifest{}, false, fmt.Errorf("parsing MCPB manifest: %w", err)
	}
	if manifest.Name == "" {
		return MCPBManifest{}, false, fmt.Errorf("MCPB manifest name is empty")
	}
	return manifest, true, nil
}

func fileCurrentAt(path string, threshold time.Time, goos, goarch string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 ||
		!isLiveCheckExecutableForGOOS(path, info.Mode(), goos) || info.ModTime().Before(threshold) {
		return false
	}
	build, err := buildinfo.ReadFile(path)
	if err != nil {
		return false
	}
	settings := make(map[string]string, len(build.Settings))
	for _, setting := range build.Settings {
		settings[setting.Key] = setting.Value
	}
	return settings["GOOS"] == goos && settings["GOARCH"] == goarch
}

func rebuildPromoteBinaryPair(cliDir, mcpName, cliOutputPath, mcpOutputPath string) error {
	cliTmpPath, err := promoteRebuildTempPath(cliOutputPath)
	if err != nil {
		return fmt.Errorf("creating staged CLI rebuild path: %w", err)
	}
	defer func() { _ = os.Remove(cliTmpPath) }()
	mcpTmpPath, err := promoteRebuildTempPath(mcpOutputPath)
	if err != nil {
		return fmt.Errorf("creating staged MCP rebuild path: %w", err)
	}
	defer func() { _ = os.Remove(mcpTmpPath) }()

	if err := buildCLITo(cliDir, cliTmpPath); err != nil {
		return fmt.Errorf("rebuilding staged CLI binary: %w", err)
	}
	if err := BuildMCPBBinary(cliDir, mcpName, mcpTmpPath, runtime.GOOS, runtime.GOARCH); err != nil {
		return fmt.Errorf("rebuilding staged MCP binary: %w", err)
	}
	if err := replacePromoteBinaryPair(cliTmpPath, cliOutputPath, mcpTmpPath, mcpOutputPath, replaceLiveCheckBinary); err != nil {
		return err
	}
	return nil
}

type promoteOriginalBackup struct {
	destination string
	backupPath  string
	existed     bool
}

func replacePromoteBinaryPair(cliTmpPath, cliOutputPath, mcpTmpPath, mcpOutputPath string, replace func(string, string) error) error {
	cliBackup, err := backupPromoteOriginal(cliOutputPath)
	if err != nil {
		return fmt.Errorf("backing up staged CLI binary: %w", err)
	}
	defer func() { _ = os.Remove(cliBackup.backupPath) }()
	mcpBackup, err := backupPromoteOriginal(mcpOutputPath)
	if err != nil {
		return errors.Join(fmt.Errorf("backing up staged MCP binary: %w", err), restorePromoteOriginal(cliBackup))
	}
	defer func() { _ = os.Remove(mcpBackup.backupPath) }()

	if err := replace(cliTmpPath, cliOutputPath); err != nil {
		return errors.Join(fmt.Errorf("replacing staged CLI binary: %w", err), restorePromoteOriginal(cliBackup), restorePromoteOriginal(mcpBackup))
	}
	if err := replace(mcpTmpPath, mcpOutputPath); err != nil {
		return errors.Join(fmt.Errorf("replacing staged MCP binary: %w", err), restorePromoteOriginal(cliBackup), restorePromoteOriginal(mcpBackup))
	}
	return nil
}

func backupPromoteOriginal(destination string) (promoteOriginalBackup, error) {
	backup := promoteOriginalBackup{destination: destination}
	if _, err := os.Stat(destination); os.IsNotExist(err) {
		return backup, nil
	} else if err != nil {
		return backup, err
	}
	backupPath, err := promoteRebuildTempPath(destination + ".old")
	if err != nil {
		return backup, err
	}
	if err := os.Rename(destination, backupPath); err != nil {
		return backup, err
	}
	backup.backupPath = backupPath
	backup.existed = true
	return backup, nil
}

func restorePromoteOriginal(backup promoteOriginalBackup) error {
	if err := os.Remove(backup.destination); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing partial staged binary %s: %w", backup.destination, err)
	}
	if !backup.existed {
		return nil
	}
	if err := os.Rename(backup.backupPath, backup.destination); err != nil {
		return fmt.Errorf("restoring staged binary %s: %w", backup.destination, err)
	}
	return nil
}

func promoteRebuildTempPath(outputPath string) (string, error) {
	tmp, err := os.CreateTemp(filepath.Dir(outputPath), "."+filepath.Base(outputPath)+".rebuild-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := os.Remove(tmpPath); err != nil {
		return "", err
	}
	return tmpPath, nil
}

func promoteBundleMatches(bundlePath, cliDir, mcpArchiveName, mcpBinary, cliArchiveName, cliBinary string) (bool, error) {
	expectedManifest, err := os.ReadFile(filepath.Join(cliDir, MCPBManifestFilename))
	if err != nil {
		return false, err
	}
	var manifest MCPBManifest
	if err := json.Unmarshal(expectedManifest, &manifest); err != nil {
		return false, err
	}
	expectedEntryPoint := "bin/" + mcpArchiveName
	if manifest.Server.EntryPoint != expectedEntryPoint {
		expectedManifest, err = rewriteMCPBManifestLaunch(expectedManifest, expectedEntryPoint)
		if err != nil {
			return false, err
		}
	}
	expected := map[string][]byte{MCPBManifestFilename: expectedManifest}
	for name, path := range map[string]string{
		"bin/" + mcpArchiveName: mcpBinary,
		"bin/" + cliArchiveName: cliBinary,
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			return false, fmt.Errorf("reading staged artifact %s: %w", path, err)
		}
		expected[name] = data
	}

	zr, err := zip.OpenReader(bundlePath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, nil
	}
	defer func() { _ = zr.Close() }()
	for _, entry := range zr.File {
		want, ok := expected[entry.Name]
		if !ok {
			return false, nil
		}
		r, err := entry.Open()
		if err != nil {
			return false, nil
		}
		got, readErr := io.ReadAll(r)
		_ = r.Close()
		if readErr != nil || !bytes.Equal(got, want) {
			return false, nil
		}
		delete(expected, entry.Name)
	}
	return len(expected) == 0, nil
}
