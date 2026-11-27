package appsniff

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var validPackageIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z0-9_-]+)+$`)

// CommandRunner executes an external acquisition CLI command. Overridable in tests.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func defaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	bin, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("%s not found on PATH: %w", name, err)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return out.Bytes(), fmt.Errorf("%s %s failed: %w (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(RedactString(out.String())))
	}
	return out.Bytes(), nil
}

// ValidatePackageID enforces strict reverse-DNS package or bundle identifier
// syntax so untrusted input cannot inject flags or path traversal into
// external acquisition tools (apkeep, adb, xcrun).
func ValidatePackageID(id string) error {
	id = strings.TrimSpace(id)
	if !validPackageIDPattern.MatchString(id) || len(id) > 180 || strings.Contains(id, "..") {
		return fmt.Errorf("invalid package or bundle identifier %q (expected reverse-DNS like com.example.app)", id)
	}
	return nil
}

// AcquireApkeep downloads an Android .xapk or .apk package via apkeep into
// destDir and returns the downloaded archive path.
func AcquireApkeep(ctx context.Context, pkgID, destDir string, runner CommandRunner) (string, error) {
	if err := ValidatePackageID(pkgID); err != nil {
		return "", err
	}
	if runner == nil {
		runner = defaultCommandRunner
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("creating apkeep download dir: %w", err)
	}

	if _, err := runner(ctx, "apkeep", "-a", pkgID, destDir); err != nil {
		return "", fmt.Errorf("apkeep acquisition failed for %s: %w", pkgID, err)
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		return "", fmt.Errorf("reading apkeep download dir: %w", err)
	}
	// Prefer .xapk over .apk if both exist.
	var apkFallback string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		lower := strings.ToLower(e.Name())
		if strings.HasSuffix(lower, ".xapk") {
			return filepath.Join(destDir, e.Name()), nil
		}
		if strings.HasSuffix(lower, ".apk") && apkFallback == "" {
			apkFallback = filepath.Join(destDir, e.Name())
		}
	}
	if apkFallback != "" {
		return apkFallback, nil
	}
	return "", fmt.Errorf("apkeep finished for %s but no .xapk or .apk was found in %s", pkgID, destDir)
}

// AcquireADB pulls an installed Android package's APK splits from a connected
// ADB device or emulator into destDir.
func AcquireADB(ctx context.Context, pkgID, destDir string, runner CommandRunner) (string, error) {
	if err := ValidatePackageID(pkgID); err != nil {
		return "", err
	}
	if runner == nil {
		runner = defaultCommandRunner
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("creating adb pull dir: %w", err)
	}

	out, err := runner(ctx, "adb", "shell", "pm", "path", pkgID)
	if err != nil {
		return "", fmt.Errorf("adb pm path %s: %w", pkgID, err)
	}

	var remotePaths []string
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if p, ok := strings.CutPrefix(line, "package:"); ok {
			p = strings.TrimSpace(p)
			if p != "" && strings.HasPrefix(p, "/") {
				remotePaths = append(remotePaths, p)
			}
		}
	}
	if len(remotePaths) == 0 {
		return "", fmt.Errorf("package %s not found on connected ADB device", pkgID)
	}

	pulled := 0
	for i, rp := range remotePaths {
		base := filepath.Base(rp)
		if base == "" || base == "." || base == "/" {
			base = fmt.Sprintf("split-%d.apk", i)
		}
		localPath := filepath.Join(destDir, fmt.Sprintf("%02d-%s", i, base))
		if _, err := runner(ctx, "adb", "pull", rp, localPath); err != nil {
			return "", fmt.Errorf("adb pull %s: %w", rp, err)
		}
		pulled++
	}
	if pulled == 0 {
		return "", fmt.Errorf("no APK files pulled for %s", pkgID)
	}
	return destDir, nil
}

// AcquireIOSSimulator locates the unencrypted .app bundle for bundleID on the
// currently booted iOS Simulator using xcrun simctl.
func AcquireIOSSimulator(ctx context.Context, bundleID string, runner CommandRunner) (string, error) {
	if err := ValidatePackageID(bundleID); err != nil {
		return "", err
	}
	if runner == nil {
		runner = defaultCommandRunner
	}
	out, err := runner(ctx, "xcrun", "simctl", "get_app_container", "booted", bundleID, "app")
	if err != nil {
		return "", fmt.Errorf("xcrun simctl get_app_container %s: %w", bundleID, err)
	}
	appPath := strings.TrimSpace(string(out))
	if appPath == "" {
		return "", fmt.Errorf("xcrun simctl returned empty path for %s", bundleID)
	}
	if _, err := os.Stat(appPath); err != nil {
		return "", fmt.Errorf("simulator app container %s not accessible: %w", appPath, err)
	}
	return appPath, nil
}
