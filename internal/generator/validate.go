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
	"syscall"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
	"github.com/mvanhorn/cli-printing-press/v4/internal/govulncheck"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
)

type validationGate struct {
	name string
	run  func() error
}

const (
	qualityGateTimeout      = 5 * time.Minute
	binaryExecTimeout       = 15 * time.Second
	dockerValidationTimeout = 10 * time.Minute
)

// ValidationMode selects how the generated CLI's runtime checks (--help,
// version, doctor) are executed once the host build gates pass.
type ValidationMode string

const (
	// ModeBinary builds a host binary and execs it. Default; the most
	// thorough check and the one CI relies on. On an AV/WDAC-hardened Windows
	// host the freshly built, unsigned binary may be quarantined or
	// policy-blocked — that outcome is reported as ExecBlocked, never as a
	// generic generation failure.
	ModeBinary ValidationMode = "binary"
	// ModeGoRun runs `go run ./cmd/<cli>` instead of a standalone binary. No
	// `-o` target to mis-suffix, but it still compiles and executes a fresh
	// binary, so it is not guaranteed to dodge host AV/WDAC.
	ModeGoRun ValidationMode = "go-run"
	// ModeDocker builds and runs inside a Linux container, bypassing Windows
	// AV/WDAC entirely. Requires a running Docker engine.
	ModeDocker ValidationMode = "docker"
	// ModeSkipExec runs every host build gate but skips executing the CLI.
	// The skip is explicit in stderr and JSON and carries the exact commands
	// a human or CI runner needs to finish validation. AV/WDAC-safe.
	ModeSkipExec ValidationMode = "skip-exec"
)

// ExecStatus reports whether the runtime smoke (--help/version/doctor) ran.
type ExecStatus string

const (
	ExecRan     ExecStatus = "ran"     // the CLI executed and produced output
	ExecSkipped ExecStatus = "skipped" // skip-exec mode: not executed by request
	ExecBlocked ExecStatus = "blocked" // build passed but exec blocked by AV/WDAC/OS policy
	ExecFailed  ExecStatus = "failed"  // genuine runtime failure of the generated CLI
)

// ValidationReport is the machine-readable outcome of Validate(). It is
// surfaced verbatim in `generate --json` under the additive "validation" key.
type ValidationReport struct {
	Mode           ValidationMode `json:"mode"`
	ExecStatus     ExecStatus     `json:"exec_status"`
	Reason         string         `json:"reason,omitempty"`
	ManualCommands []string       `json:"manual_commands,omitempty"`
}

const blockReasonQuarantine = "the freshly built validation binary disappeared between build and exec — typically an antivirus/EDR quarantine or deletion (e.g. Trend Micro 'Verdacht bestand geblokkeerd')"

func (g *Generator) Validate() (ValidationReport, error) {
	mode := g.ValidationMode
	if mode == "" {
		mode = ModeBinary
	}
	report := ValidationReport{Mode: mode, ExecStatus: ExecFailed}

	cliName := naming.CLI(g.Spec.Name)
	binPath := filepath.Join(g.OutputDir, naming.HostExeName(naming.ValidationBinary(g.Spec.Name)))

	if err := artifacts.CleanupGeneratedCLI(g.OutputDir, artifacts.CleanupOptions{
		RemoveValidationBinaries: true,
		RemoveRecursiveCopies:    true,
		RemoveFinderMetadata:     true,
	}); err != nil {
		return report, fmt.Errorf("pre-validating cleanup: %w", err)
	}
	defer func() {
		_ = artifacts.CleanupGeneratedCLI(g.OutputDir, artifacts.CleanupOptions{
			RemoveValidationBinaries: true,
			RemoveRecursiveCopies:    true,
			RemoveFinderMetadata:     true,
		})
	}()

	// Host gates exercise the Go toolchain only (no fresh-CLI exec), so they
	// are AV/WDAC-safe and run identically for every mode.
	hostGates := []validationGate{
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
	}

	for _, gate := range hostGates {
		if err := gate.run(); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL %s\n", gate.name)
			return report, fmt.Errorf("gate %q failed: %w", gate.name, err)
		}
		fmt.Fprintf(os.Stderr, "PASS %s\n", gate.name)
	}

	var (
		status ExecStatus
		reason string
		err    error
	)
	switch mode {
	case ModeBinary:
		status, reason, err = g.runBinarySmoke(binPath, cliName)
	case ModeGoRun:
		status, reason, err = g.runGoRunSmoke(cliName)
	case ModeDocker:
		status, reason, err = g.runDockerSmoke(cliName)
	case ModeSkipExec:
		status, reason = ExecSkipped, "validation-mode=skip-exec: runtime checks were not executed"
		fmt.Fprintf(os.Stderr, "SKIP runtime validation (--validation-mode=skip-exec)\n")
	default:
		return report, fmt.Errorf("unknown validation mode %q (want binary, go-run, docker, or skip-exec)", mode)
	}

	report.ExecStatus = status
	report.Reason = reason
	if err != nil {
		report.ExecStatus = ExecFailed
		return report, err
	}
	if status == ExecBlocked || status == ExecSkipped {
		report.ManualCommands = manualValidationCommands(g.OutputDir, binPath, cliName)
		printValidationNotice(report)
	}
	return report, nil
}

// runBinarySmoke builds the validation binary and execs its --help/version/
// doctor commands. A build that passes followed by an exec the OS or an AV/EDR
// refuses is reported as ExecBlocked (recoverable, exit 0), distinct from a
// genuine CLI failure (ExecFailed, non-nil error).
func (g *Generator) runBinarySmoke(binPath, cliName string) (ExecStatus, string, error) {
	if _, err := runCommand(g.OutputDir, qualityGateTimeout, "go", "build", "-o", binPath, "./cmd/"+cliName); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL build runnable binary\n")
		return ExecFailed, "", fmt.Errorf("gate %q failed: %w", "build runnable binary", err)
	}
	fmt.Fprintf(os.Stderr, "PASS build runnable binary\n")

	// A binary that vanishes immediately after a clean build was quarantined
	// by an AV/EDR (the Trend Micro "Bestand verwijderen" case).
	if _, statErr := os.Stat(binPath); statErr != nil {
		fmt.Fprintf(os.Stderr, "BLOCKED runtime validation: %s\n", blockReasonQuarantine)
		return ExecBlocked, blockReasonQuarantine, nil
	}

	for _, args := range [][]string{{"--help"}, {"version"}, {"doctor"}} {
		label := cliName + " " + strings.Join(args, " ")
		if err := validateCommandOutput(g.OutputDir, binaryExecTimeout, binPath, args...); err != nil {
			_, statErr := os.Stat(binPath)
			if blocked, reason := classifyExecError(err, statErr == nil); blocked {
				fmt.Fprintf(os.Stderr, "BLOCKED %s: %s\n", label, reason)
				return ExecBlocked, reason, nil
			}
			fmt.Fprintf(os.Stderr, "FAIL %s\n", label)
			return ExecFailed, "", fmt.Errorf("gate %q failed: %w", label, err)
		}
		fmt.Fprintf(os.Stderr, "PASS %s\n", label)
	}
	return ExecRan, "", nil
}

// runGoRunSmoke validates via `go run ./cmd/<cli>`, avoiding the `-o` suffix
// path. It still compiles and runs a fresh binary, so an AV/WDAC block is
// classified the same way as binary mode.
func (g *Generator) runGoRunSmoke(cliName string) (ExecStatus, string, error) {
	pkg := "./cmd/" + cliName
	for _, args := range [][]string{{"--help"}, {"version"}, {"doctor"}} {
		label := "go run " + pkg + " " + strings.Join(args, " ")
		runArgs := append([]string{"run", pkg}, args...)
		if err := validateCommandOutput(g.OutputDir, qualityGateTimeout, "go", runArgs...); err != nil {
			if blocked, reason := classifyExecError(err, true); blocked {
				fmt.Fprintf(os.Stderr, "BLOCKED %s: %s\n", label, reason)
				return ExecBlocked, reason, nil
			}
			fmt.Fprintf(os.Stderr, "FAIL %s\n", label)
			return ExecFailed, "", fmt.Errorf("gate %q failed: %w", label, err)
		}
		fmt.Fprintf(os.Stderr, "PASS %s\n", label)
	}
	return ExecRan, "", nil
}

// runDockerSmoke builds and runs the CLI inside a Linux container. Linux
// containers are not subject to Windows AV/WDAC, so this is the robust escape
// hatch on a hardened Windows host. Requires a running Docker engine.
func (g *Generator) runDockerSmoke(cliName string) (ExecStatus, string, error) {
	if _, err := runCommand(g.OutputDir, 30*time.Second, "docker", "info", "--format", "{{.ServerVersion}}"); err != nil {
		return ExecFailed, "", fmt.Errorf("validation-mode=docker requires a running Docker engine: %w", err)
	}
	absDir, err := filepath.Abs(g.OutputDir)
	if err != nil {
		return ExecFailed, "", fmt.Errorf("resolving output dir for docker mount: %w", err)
	}
	image := dockerGoImage()
	script := fmt.Sprintf("set -e; go build -o /tmp/cli ./cmd/%s; /tmp/cli --help; /tmp/cli version; /tmp/cli doctor", cliName)
	if _, err := runCommand(g.OutputDir, dockerValidationTimeout, "docker", "run", "--rm",
		"-v", absDir+":/src", "-w", "/src", image, "sh", "-c", script); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL docker validation (%s)\n", image)
		return ExecFailed, "", fmt.Errorf("gate %q failed: %w", "docker validation ("+image+")", err)
	}
	fmt.Fprintf(os.Stderr, "PASS docker validation (%s)\n", image)
	return ExecRan, "", nil
}

// dockerGoImage maps the host Go version to a matching golang image tag, e.g.
// go1.26.3 -> "golang:1.26".
func dockerGoImage() string {
	v := strings.TrimPrefix(runtime.Version(), "go")
	parts := strings.Split(v, ".")
	if len(parts) >= 2 {
		return "golang:" + parts[0] + "." + parts[1]
	}
	return "golang:1"
}

// classifyExecError reports whether a failed CLI exec was blocked by host
// security controls (antivirus quarantine, WDAC/AppLocker, OS access policy)
// rather than a genuine fault in the generated CLI.
//
// The discriminator is locale-independent and does not parse the OS message
// (which is localized — e.g. Dutch "geblokkeerd door een beleid voor
// toepassingsbeheer" for a WDAC block). After a clean `go build`, the binary
// is a valid executable, so:
//   - a process that STARTED and exited non-zero (*exec.ExitError), or ran
//     cleanly but printed nothing, is a genuine CLI fault — not a block;
//   - any other failure to run a still-present, freshly built binary means it
//     could not be started at all, which is the signature of an AV/EDR or
//     application-control (WDAC/AppLocker) block or an OS execution-policy
//     denial.
//
// binStillExists is the result of stat-ing the binary after the failed exec; a
// binary that vanished after a clean build was quarantined by an AV/EDR.
func classifyExecError(err error, binStillExists bool) (blocked bool, reason string) {
	if err == nil {
		return false, ""
	}
	if !binStillExists {
		return true, blockReasonQuarantine
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, "" // ran and exited non-zero -> genuine CLI fault
	}
	if strings.Contains(err.Error(), "produced no output") {
		return false, "" // ran cleanly, empty output -> genuine issue, not a block
	}

	reason = "the validation binary built cleanly but could not be started on this host — the signature of an antivirus/EDR or application-control (WDAC/AppLocker) block, or an OS execution-policy denial"
	// Sharpen the reason when a specific Windows block code is available. These
	// codes sit well above the POSIX errno range, so the check is inert on
	// Linux/macOS.
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch uintptr(errno) {
		case 1260: // ERROR_ACCESS_DISABLED_BY_POLICY (WDAC/AppLocker)
			reason = "exec was blocked by Windows application-control policy (WDAC/AppLocker)"
		case 225, 226: // ERROR_VIRUS_INFECTED / ERROR_VIRUS_DELETED
			reason = "exec was blocked by antivirus (the binary was flagged as suspicious or infected)"
		}
	}
	return true, reason
}

// manualValidationCommands lists the exact commands a human or CI runner can
// execute (in g.OutputDir) to finish runtime validation that was blocked or
// skipped on this host.
func manualValidationCommands(outputDir, binPath, cliName string) []string {
	bin := filepath.Base(binPath)
	run := "." + string(filepath.Separator) + bin
	return []string{
		"cd " + outputDir,
		fmt.Sprintf("go build -o %s ./cmd/%s", bin, cliName),
		run + " --help",
		run + " version",
		run + " doctor",
	}
}

func printValidationNotice(report ValidationReport) {
	fmt.Fprintf(os.Stderr, "\nRuntime validation did not execute (status: %s, mode: %s).\n", report.ExecStatus, report.Mode)
	if report.Reason != "" {
		fmt.Fprintf(os.Stderr, "Reason: %s\n", report.Reason)
	}
	fmt.Fprintln(os.Stderr, "The generated CLI is complete and its code compiled cleanly (host build gates passed).")
	fmt.Fprintln(os.Stderr, "To finish runtime validation, run:")
	for _, cmd := range report.ManualCommands {
		fmt.Fprintf(os.Stderr, "  %s\n", cmd)
	}
	fmt.Fprintln(os.Stderr, "Or re-run generation with --validation-mode=docker (validates inside a Linux container, bypassing Windows AV/WDAC) or --validation-mode=skip-exec.")
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
