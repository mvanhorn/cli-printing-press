package generator

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
	"github.com/mvanhorn/cli-printing-press/v4/internal/govulncheck"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
)

type validationGate struct {
	name string
	run  func() error
}

const qualityGateTimeout = 5 * time.Minute

func (g *Generator) Validate() error {
	binPath := filepath.Join(g.OutputDir, naming.ValidationBinary(g.Spec.Name))
	if err := artifacts.CleanupGeneratedCLI(g.OutputDir, artifacts.CleanupOptions{
		RemoveValidationBinaries: true,
		RemoveRecursiveCopies:    true,
		RemoveFinderMetadata:     true,
	}); err != nil {
		return fmt.Errorf("pre-validating cleanup: %w", err)
	}
	defer func() {
		_ = artifacts.CleanupGeneratedCLI(g.OutputDir, artifacts.CleanupOptions{
			RemoveValidationBinaries: true,
			RemoveRecursiveCopies:    true,
			RemoveFinderMetadata:     true,
		})
	}()

	gates := []validationGate{
		{
			name: "go mod tidy",
			run: func() error {
				_, err := runCommand(g.OutputDir, qualityGateTimeout, "go", "mod", "tidy")
				return err
			},
		},
		{
			name: "govulncheck ./...",
			run: func() error {
				_, err := runCommand(g.OutputDir, qualityGateTimeout, "go", govulncheck.GoRunArgs("./...")...)
				return err
			},
		},
		{
			name: "go vet ./...",
			run: func() error {
				_, err := runCommand(g.OutputDir, qualityGateTimeout, "go", "vet", "./...")
				return err
			},
		},
		{
			name: "go build ./...",
			run: func() error {
				_, err := runCommand(g.OutputDir, qualityGateTimeout, "go", "build", "./...")
				return err
			},
		},
		{
			name: "build runnable binary",
			run: func() error {
				_, err := runCommand(g.OutputDir, qualityGateTimeout, "go", "build", "-o", binPath, "./cmd/"+naming.CLI(g.Spec.Name))
				return err
			},
		},
		{
			name: naming.CLI(g.Spec.Name) + " --help",
			run: func() error {
				return validateCommandOutput(g.OutputDir, 15*time.Second, binPath, "--help")
			},
		},
		{
			name: naming.CLI(g.Spec.Name) + " version",
			run: func() error {
				return validateCommandOutput(g.OutputDir, 15*time.Second, binPath, "version")
			},
		},
		{
			name: naming.CLI(g.Spec.Name) + " doctor",
			run: func() error {
				return validateCommandOutput(g.OutputDir, 15*time.Second, binPath, "doctor")
			},
		},
	}

	for _, gate := range gates {
		if err := gate.run(); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL %s\n", gate.name)
			return fmt.Errorf("gate %q failed: %w", gate.name, err)
		}
		fmt.Fprintf(os.Stderr, "PASS %s\n", gate.name)
	}

	return nil
}

func validateCommandOutput(dir string, timeout time.Duration, name string, args ...string) error {
	output, err := runCommand(dir, timeout, name, args...)
	if err != nil {
		return err
	}
	if strings.TrimSpace(output) == "" {
		return fmt.Errorf("%s produced no output", strings.Join(append([]string{name}, args...), " "))
	}
	return nil
}

func runCommand(dir string, timeout time.Duration, name string, args ...string) (string, error) {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cacheDir, err := goBuildCacheDir(dir)
	if err != nil {
		return "", err
	}
	cmd.Env = append(os.Environ(), "GOCACHE="+cacheDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	output := strings.TrimSpace(strings.Join([]string{stdout.String(), stderr.String()}, "\n"))
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("timed out after %s", timeout)
		}
		if output == "" {
			return "", err
		}
		return output, fmt.Errorf("%w\n%s", err, output)
	}

	return output, nil
}

func goBuildCacheDir(dir string) (string, error) {
	if cacheDir := os.Getenv("GOCACHE"); cacheDir != "" {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return "", fmt.Errorf("creating GOCACHE dir: %w", err)
		}
		return cacheDir, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		absDir, absErr := filepath.Abs(dir)
		if absErr != nil {
			return "", fmt.Errorf("resolving build cache path: %w", absErr)
		}
		fallback := filepath.Join(absDir, ".cache", "go-build")
		if mkErr := os.MkdirAll(fallback, 0o755); mkErr != nil {
			return "", fmt.Errorf("creating fallback build cache dir: %w", mkErr)
		}
		return fallback, nil
	}

	// Use a single shared cache for all generated CLIs.
	// Per-project caches forced each parallel test to compile the Go
	// standard library from scratch, causing CI timeouts.
	cacheDir := filepath.Join(homeDir, ".cache", "printing-press", "go-build")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("creating build cache dir: %w", err)
	}
	return cacheDir, nil
}
