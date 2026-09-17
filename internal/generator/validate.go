package generator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
	"github.com/mvanhorn/cli-printing-press/v4/internal/govulncheck"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/platform"
)

type validationGate struct {
	name string
	run  func() error
}

const qualityGateTimeout = 5 * time.Minute

// Isolated GOCACHE is shared across generated modules so parallel tests
// reuse the stdlib compile. Go's own trim is age-based (days), so unique
// generated packages accumulate until the disk fills. Bound it by size.
const isolatedBuildCacheMaxBytes int64 = 2 << 30

const isolatedBuildCacheCheckInterval = 15 * time.Second

var errCacheOverLimit = errors.New("build cache over size limit")

var (
	cacheGateMu     sync.Mutex
	cacheGateCond   = sync.NewCond(&cacheGateMu)
	cacheGateActive int
	cacheCheckMu    sync.Mutex
	cacheLastCheck  = map[string]time.Time{}
)

func (g *Generator) Validate() error {
	binPath := platform.ExecutablePath(filepath.Join(g.OutputDir, naming.ValidationBinary(g.Spec.Name)))
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
			name: "ensure safe golang.org/x/net",
			run: func() error {
				return ensureSafeXNet(g.OutputDir)
			},
		},
		{
			name: "ensure safe golang.org/x/text",
			run: func() error {
				return ensureSafeXText(g.OutputDir)
			},
		},
		{
			name: "ensure safe github.com/enetx/http",
			run: func() error {
				return ensureSafeEnetxHTTP(g.OutputDir)
			},
		},
		{
			name: "go test ./...",
			run: func() error {
				_, err := runCommand(g.OutputDir, qualityGateTimeout, "go", "test", "-count=1", "./...")
				return err
			},
		},
		{
			name: "govulncheck ./...",
			run: func() error {
				env := govulncheck.ToolchainEnv(g.OutputDir)
				_, err := runCommandWithEnv(g.OutputDir, qualityGateTimeout, env, "go", govulncheck.GoRunArgs("./...")...)
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
				_, err := runCommand(g.OutputDir, qualityGateTimeout, "go", "build", "-trimpath", "-ldflags=-buildid=", "./...")
				return err
			},
		},
		{
			name: "build runnable binary",
			run: func() error {
				_, err := runCommand(g.OutputDir, qualityGateTimeout, "go", "build", "-trimpath", "-ldflags=-buildid=", "-o", binPath, "./cmd/"+naming.CLI(g.Spec.Name))
				return err
			},
		},
		{
			name: naming.CLI(g.Spec.Name) + " --help",
			run: func() error {
				return validateCommandOutput(g.OutputDir, helpGateTimeout(runtime.GOOS), binPath, "--help")
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

func helpGateTimeout(goos string) time.Duration {
	if goos == "windows" {
		return 30 * time.Second
	}
	return 15 * time.Second
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
	return runCommandWithEnv(dir, timeout, nil, name, args...)
}

func runCommandWithEnv(dir string, timeout time.Duration, extraEnv []string, name string, args ...string) (string, error) {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := withGoBuildCache(dir, func(cacheDir string) error {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOCACHE="+cacheDir)
		cmd.Env = append(cmd.Env, extraEnv...)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		return cmd.Run()
	})
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
	cacheDir, _, err := resolveGoBuildCacheDir(dir)
	return cacheDir, err
}

func withGoBuildCache(dir string, fn func(cacheDir string) error) error {
	return withGoBuildCacheLimited(dir, isolatedBuildCacheMaxBytes, fn)
}

func withGoBuildCacheLimited(dir string, maxBytes int64, fn func(cacheDir string) error) error {
	cacheDir, isolated, err := resolveGoBuildCacheDir(dir)
	if err != nil {
		return err
	}

	cacheGateMu.Lock()
	if isolated {
		if err := waitAndMaybeWipeLocked(cacheDir, maxBytes); err != nil {
			cacheGateMu.Unlock()
			return fmt.Errorf("bounding isolated GOCACHE: %w", err)
		}
	}
	cacheGateActive++
	cacheGateMu.Unlock()
	defer func() {
		cacheGateMu.Lock()
		cacheGateActive--
		cacheGateCond.Broadcast()
		cacheGateMu.Unlock()
	}()

	return fn(cacheDir)
}

func resolveGoBuildCacheDir(dir string) (string, bool, error) {
	if cacheDir := os.Getenv("GOCACHE"); cacheDir != "" {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return "", false, fmt.Errorf("creating GOCACHE dir: %w", err)
		}
		return cacheDir, false, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		absDir, absErr := filepath.Abs(dir)
		if absErr != nil {
			return "", false, fmt.Errorf("resolving build cache path: %w", absErr)
		}
		fallback := filepath.Join(absDir, ".cache", "go-build")
		if mkErr := os.MkdirAll(fallback, 0o755); mkErr != nil {
			return "", false, fmt.Errorf("creating fallback build cache dir: %w", mkErr)
		}
		return fallback, true, nil
	}

	// Use a single shared cache for all generated CLIs.
	// Per-project caches forced each parallel test to compile the Go
	// standard library from scratch, causing CI timeouts.
	cacheDir := filepath.Join(homeDir, ".cache", "printing-press", "go-build")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", false, fmt.Errorf("creating build cache dir: %w", err)
	}
	return cacheDir, true, nil
}

func waitAndMaybeWipeLocked(cacheDir string, maxBytes int64) error {
	if maxBytes <= 0 || !shouldCheckBuildCache(cacheDir) {
		return nil
	}
	cacheGateMu.Unlock()
	over, err := buildCacheExceeds(cacheDir, maxBytes)
	cacheGateMu.Lock()
	if !scanRequiresTrim(over, err) {
		markBuildCacheChecked(cacheDir)
		return nil
	}
	for cacheGateActive > 0 {
		cacheGateCond.Wait()
	}
	if wipeErr := boundBuildCache(cacheDir, maxBytes); wipeErr != nil {
		return fmt.Errorf("wiping oversized build cache: %w", wipeErr)
	}
	markBuildCacheChecked(cacheDir)
	return nil
}

func shouldCheckBuildCache(cacheDir string) bool {
	key := filepath.Clean(cacheDir)
	cacheCheckMu.Lock()
	defer cacheCheckMu.Unlock()
	return time.Since(cacheLastCheck[key]) >= isolatedBuildCacheCheckInterval
}

func markBuildCacheChecked(cacheDir string) {
	key := filepath.Clean(cacheDir)
	cacheCheckMu.Lock()
	defer cacheCheckMu.Unlock()
	cacheLastCheck[key] = time.Now()
}

func boundBuildCache(dir string, maxBytes int64) error {
	if maxBytes <= 0 {
		return nil
	}
	over, err := buildCacheExceeds(dir, maxBytes)
	if !scanRequiresTrim(over, err) {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(dir, 0o755)
		}
		return err
	}
	var removeErr error
	for _, e := range entries {
		if rmErr := os.RemoveAll(filepath.Join(dir, e.Name())); rmErr != nil && !os.IsNotExist(rmErr) {
			removeErr = rmErr
		}
	}
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return mkErr
	}
	return removeErr
}

func scanRequiresTrim(over bool, err error) bool {
	return over || err != nil
}

func cacheEntrySize(d os.DirEntry) (int64, error) {
	info, err := d.Info()
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return info.Size(), nil
}

func buildCacheExceeds(dir string, maxBytes int64) (bool, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		size, statErr := cacheEntrySize(d)
		if statErr != nil {
			return statErr
		}
		total += size
		if total > maxBytes {
			return errCacheOverLimit
		}
		return nil
	})
	if errors.Is(err, errCacheOverLimit) {
		return true, nil
	}
	return false, err
}
