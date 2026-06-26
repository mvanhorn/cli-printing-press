package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/platform"
)

func buildCLI(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving CLI directory: %w", err)
	}
	binaryPath := platform.ExecutablePath(filepath.Join(dir, filepath.Base(dir)))
	if err := buildCLITo(dir, binaryPath); err != nil {
		return "", err
	}
	return binaryPath, nil
}

func buildCLITo(dir, binaryPath string) error {
	cmdDir, err := findCLICommandDir(dir)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", buildCLIArgs(binaryPath, "./"+filepath.Base(cmdDir))...)
	cmd.Dir = filepath.Dir(cmdDir)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build: %s\n%s", err, string(out))
	}
	return nil
}

func buildCLIArgs(binaryPath, pkg string) []string {
	return []string{"build", "-trimpath", "-ldflags=-buildid=", "-o", binaryPath, pkg}
}

func findCLICommandDir(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving CLI directory: %w", err)
	}
	name := filepath.Base(dir)
	apiName := naming.TrimCLISuffix(name)
	candidates := []string{
		filepath.Join(dir, "cmd", name),
		filepath.Join(dir, "cmd", naming.CLI(apiName)),
		filepath.Join(dir, "cmd", naming.LegacyCLI(apiName)),
		filepath.Join(dir, "cmd", apiName),
	}

	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("stat %s: %w", candidate, err)
		}
	}

	entries, err := os.ReadDir(filepath.Join(dir, "cmd"))
	if err != nil {
		return "", fmt.Errorf("reading cmd directory: %w", err)
	}

	var cliEntries []string
	var dirEntries []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirEntries = append(dirEntries, entry.Name())
		if naming.IsCLIDirName(entry.Name()) {
			cliEntries = append(cliEntries, entry.Name())
		}
	}

	sort.Strings(cliEntries)
	if len(cliEntries) == 1 {
		return filepath.Join(dir, "cmd", cliEntries[0]), nil
	}

	if len(dirEntries) == 1 {
		return filepath.Join(dir, "cmd", dirEntries[0]), nil
	}

	return "", fmt.Errorf("cannot find CLI cmd entry point in %s", dir)
}

func runCLI(binary string, args []string, env []string, timeout time.Duration) error {
	_, err := runCLIWithOutput(binary, args, env, timeout)
	return err
}

func runCLIWithOutput(binary string, args []string, env []string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("exit %w: %s", err, string(out))
	}
	return out, nil
}
