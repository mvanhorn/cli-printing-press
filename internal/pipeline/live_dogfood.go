package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

type LiveDogfoodStatus string

const (
	LiveDogfoodStatusPass LiveDogfoodStatus = "pass"
	LiveDogfoodStatusFail LiveDogfoodStatus = "fail"
	LiveDogfoodStatusSkip LiveDogfoodStatus = "skip"
)

type LiveDogfoodTestKind string

const (
	LiveDogfoodTestHelp  LiveDogfoodTestKind = "help"
	LiveDogfoodTestHappy LiveDogfoodTestKind = "happy_path"
	LiveDogfoodTestJSON  LiveDogfoodTestKind = "json_fidelity"
	LiveDogfoodTestError LiveDogfoodTestKind = "error_path"
)

type LiveDogfoodOptions struct {
	CLIDir              string
	BinaryName          string
	Level               string
	Timeout             time.Duration
	WriteAcceptancePath string
	AuthEnv             string
}

type LiveDogfoodReport struct {
	Dir        string                  `json:"dir"`
	Binary     string                  `json:"binary"`
	Level      string                  `json:"level"`
	Verdict    string                  `json:"verdict"`
	MatrixSize int                     `json:"matrix_size"`
	Passed     int                     `json:"passed"`
	Failed     int                     `json:"failed"`
	Skipped    int                     `json:"skipped"`
	Commands   []string                `json:"commands"`
	Tests      []LiveDogfoodTestResult `json:"tests"`
	RanAt      time.Time               `json:"ran_at"`
}

type LiveDogfoodTestResult struct {
	Command      string              `json:"command"`
	Kind         LiveDogfoodTestKind `json:"kind"`
	Args         []string            `json:"args"`
	Status       LiveDogfoodStatus   `json:"status"`
	ExitCode     int                 `json:"exit_code,omitempty"`
	Reason       string              `json:"reason,omitempty"`
	OutputSample string              `json:"output_sample,omitempty"`
}

type liveDogfoodCommand struct {
	Path []string
	Help string
}

type liveDogfoodRun struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
}

func RunLiveDogfood(opts LiveDogfoodOptions) (*LiveDogfoodReport, error) {
	if strings.TrimSpace(opts.CLIDir) == "" {
		return nil, fmt.Errorf("CLIDir is required")
	}
	level, err := normalizeLiveDogfoodLevel(opts.Level)
	if err != nil {
		return nil, err
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	binaryPath, err := liveDogfoodBinaryPath(opts.CLIDir, opts.BinaryName)
	if err != nil {
		return nil, err
	}

	commands, err := discoverLiveDogfoodCommands(binaryPath)
	if err != nil {
		return nil, err
	}
	if level == "quick" {
		commands = liveDogfoodQuickCommands(commands)
	}
	if len(commands) == 0 {
		return nil, fmt.Errorf("no live dogfood command leaves discovered")
	}

	report := &LiveDogfoodReport{
		Dir:     opts.CLIDir,
		Binary:  binaryPath,
		Level:   level,
		Verdict: "PASS",
		RanAt:   time.Now().UTC(),
	}

	for _, command := range commands {
		commandName := strings.Join(command.Path, " ")
		report.Commands = append(report.Commands, commandName)
		report.Tests = append(report.Tests, runLiveDogfoodCommand(binaryPath, opts.CLIDir, command, timeout)...)
	}

	finalizeLiveDogfoodReport(report)
	if report.Verdict == "PASS" && opts.WriteAcceptancePath != "" {
		if err := writeLiveDogfoodAcceptance(opts, report); err != nil {
			return nil, err
		}
	}
	return report, nil
}

func liveDogfoodBinaryPath(dir, name string) (string, error) {
	if path, err := resolveBinaryPath(dir, name); err == nil {
		return path, nil
	} else if strings.TrimSpace(name) != "" {
		return "", err
	}

	cliName := findCLIName(dir)
	if cliName == "" {
		return "", fmt.Errorf("no runnable binary found in %q and no cmd/<cli-name> package to build", dir)
	}
	return buildDogfoodBinary(dir, cliName)
}

func discoverLiveDogfoodCommands(binaryPath string) ([]liveDogfoodCommand, error) {
	out, err := runStdoutOnly(binaryPath, 15*time.Second, "agent-context")
	if err != nil {
		return nil, fmt.Errorf("agent-context failed: %w", err)
	}

	var ctx dogfoodAgentContext
	if err := json.Unmarshal(out, &ctx); err != nil {
		return nil, fmt.Errorf("parsing agent-context: %w", err)
	}

	var paths [][]string
	for _, command := range ctx.Commands {
		collectLiveDogfoodCommandPaths(nil, command, &paths)
	}
	sort.Slice(paths, func(i, j int) bool {
		return strings.Join(paths[i], " ") < strings.Join(paths[j], " ")
	})

	commands := make([]liveDogfoodCommand, 0, len(paths))
	for _, path := range paths {
		commands = append(commands, liveDogfoodCommand{Path: path})
	}
	return commands, nil
}

var liveDogfoodFrameworkSkip = map[string]bool{
	"agent-context": true,
	"completion":    true,
	"help":          true,
	"version":       true,
}

func collectLiveDogfoodCommandPaths(prefix []string, command dogfoodAgentCommand, paths *[][]string) {
	if command.Name == "" || liveDogfoodFrameworkSkip[command.Name] {
		return
	}

	next := append(append([]string{}, prefix...), command.Name)
	if len(command.Subcommands) == 0 {
		*paths = append(*paths, next)
		return
	}
	for _, sub := range command.Subcommands {
		collectLiveDogfoodCommandPaths(next, sub, paths)
	}
}

func runLiveDogfoodCommand(binaryPath, cliDir string, command liveDogfoodCommand, timeout time.Duration) []LiveDogfoodTestResult {
	commandName := strings.Join(command.Path, " ")

	helpArgs := append(append([]string{}, command.Path...), "--help")
	helpRun := runLiveDogfoodProcess(binaryPath, cliDir, helpArgs, timeout)
	helpResult := liveDogfoodResult(commandName, LiveDogfoodTestHelp, helpArgs, helpRun)
	helpPassed := helpRun.exitCode == 0
	help := helpRun.stdout + helpRun.stderr
	if helpPassed && extractExamplesSection(help) == "" {
		helpPassed = false
		helpResult.Status = LiveDogfoodStatusFail
		helpResult.Reason = "missing Examples section"
	}
	if helpPassed {
		helpResult.Status = LiveDogfoodStatusPass
		helpResult.Reason = ""
	}

	results := []LiveDogfoodTestResult{helpResult}
	if !helpPassed {
		results = append(results,
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestHappy, "help check failed"),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, "help check failed"),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestError, "help check failed"),
		)
		return results
	}

	command.Help = help
	happyArgs, ok := liveDogfoodHappyArgs(command)
	if !ok {
		results = append(results,
			failedLiveDogfoodResult(commandName, LiveDogfoodTestHappy, command.Path, "missing runnable example"),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, "missing runnable example"),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestError, "missing runnable example"),
		)
		return results
	}

	happyRun := runLiveDogfoodProcess(binaryPath, cliDir, happyArgs, timeout)
	happyResult := liveDogfoodResult(commandName, LiveDogfoodTestHappy, happyArgs, happyRun)
	if happyRun.exitCode == 0 {
		happyResult.Status = LiveDogfoodStatusPass
		happyResult.Reason = ""
	}
	results = append(results, happyResult)

	if commandSupportsJSON(command.Help) {
		jsonArgs := appendJSONArg(happyArgs)
		jsonRun := runLiveDogfoodProcess(binaryPath, cliDir, jsonArgs, timeout)
		jsonResult := liveDogfoodResult(commandName, LiveDogfoodTestJSON, jsonArgs, jsonRun)
		if jsonRun.exitCode == 0 {
			if !json.Valid([]byte(jsonRun.stdout)) {
				jsonResult.Status = LiveDogfoodStatusFail
				jsonResult.Reason = "invalid JSON"
			} else {
				jsonResult.Status = LiveDogfoodStatusPass
				jsonResult.Reason = ""
			}
		}
		results = append(results, jsonResult)
	} else {
		results = append(results, skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, "--json not supported"))
	}

	if liveDogfoodCommandTakesArg(command.Help) {
		errorArgs := append(append([]string{}, command.Path...), "__printing_press_invalid__")
		errorRun := runLiveDogfoodProcess(binaryPath, cliDir, errorArgs, timeout)
		errorResult := liveDogfoodResult(commandName, LiveDogfoodTestError, errorArgs, errorRun)
		if errorRun.exitCode != 0 {
			errorResult.Status = LiveDogfoodStatusPass
			errorResult.Reason = ""
		} else {
			errorResult.Status = LiveDogfoodStatusFail
			errorResult.Reason = "expected non-zero exit for invalid argument"
		}
		results = append(results, errorResult)
	} else {
		results = append(results, skippedLiveDogfoodResult(commandName, LiveDogfoodTestError, "no positional argument"))
	}

	return results
}

func runLiveDogfoodProcess(binaryPath, cliDir string, args []string, timeout time.Duration) liveDogfoodRun {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = cliDir
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = &limitedWriter{w: stdout, remaining: MaxOutputBytes}
	cmd.Stderr = &limitedWriter{w: stderr, remaining: MaxOutputBytes}

	err := cmd.Run()
	result := liveDogfoodRun{
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		exitCode: 0,
		err:      err,
	}
	if ctx.Err() == context.DeadlineExceeded {
		result.exitCode = -1
		result.err = fmt.Errorf("timed out after %s", timeout)
		return result
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.exitCode = exitErr.ExitCode()
		} else {
			result.exitCode = -1
		}
	}
	return result
}

func liveDogfoodResult(command string, kind LiveDogfoodTestKind, args []string, run liveDogfoodRun) LiveDogfoodTestResult {
	result := LiveDogfoodTestResult{
		Command:      command,
		Kind:         kind,
		Args:         append([]string{}, args...),
		Status:       LiveDogfoodStatusFail,
		ExitCode:     run.exitCode,
		OutputSample: sampleOutput(run.stdout + run.stderr),
	}
	if run.exitCode != 0 {
		result.Reason = fmt.Sprintf("exit %d", run.exitCode)
	}
	if run.err != nil && result.Reason == "" {
		result.Reason = run.err.Error()
	}
	return result
}

func failedLiveDogfoodResult(command string, kind LiveDogfoodTestKind, args []string, reason string) LiveDogfoodTestResult {
	return LiveDogfoodTestResult{
		Command: command,
		Kind:    kind,
		Args:    append([]string{}, args...),
		Status:  LiveDogfoodStatusFail,
		Reason:  reason,
	}
}

func skippedLiveDogfoodResult(command string, kind LiveDogfoodTestKind, reason string) LiveDogfoodTestResult {
	return LiveDogfoodTestResult{
		Command: command,
		Kind:    kind,
		Status:  LiveDogfoodStatusSkip,
		Reason:  reason,
	}
}

func liveDogfoodHappyArgs(command liveDogfoodCommand) ([]string, bool) {
	examples := extractExamplesSection(command.Help)
	for line := range strings.SplitSeq(examples, "\n") {
		candidate := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "$"))
		if candidate == "" || strings.HasPrefix(candidate, "#") {
			continue
		}
		args, err := parseExampleArgs(candidate)
		if err == nil && len(args) > 0 && slices.Equal(args[:min(len(command.Path), len(args))], command.Path) {
			return args, true
		}
	}
	return nil, false
}

func commandSupportsJSON(help string) bool {
	return slices.Contains(extractFlagNames(help), "json")
}

func appendJSONArg(args []string) []string {
	out := append([]string{}, args...)
	for _, arg := range out {
		if arg == "--json" || strings.HasPrefix(arg, "--json=") {
			return out
		}
	}
	return append(out, "--json")
}

func liveDogfoodCommandTakesArg(help string) bool {
	usage := liveDogfoodUsageSuffix(help)
	return len(extractPositionalPlaceholders(usage)) > 0
}

func liveDogfoodUsageSuffix(help string) string {
	lines := strings.Split(help, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "Usage:" {
			continue
		}
		if i+1 < len(lines) {
			return lines[i+1]
		}
	}
	return ""
}

func finalizeLiveDogfoodReport(report *LiveDogfoodReport) {
	for _, result := range report.Tests {
		switch result.Status {
		case LiveDogfoodStatusPass:
			report.Passed++
			report.MatrixSize++
		case LiveDogfoodStatusFail:
			report.Failed++
			report.MatrixSize++
		default:
			report.Skipped++
		}
	}
	switch {
	case report.Level == "quick" && report.MatrixSize == 6 && report.Passed >= 5:
		report.Verdict = "PASS"
	case report.Failed > 0 || report.MatrixSize == 0:
		report.Verdict = "FAIL"
	case report.Level == "quick" && report.MatrixSize != 6:
		report.Verdict = "FAIL"
	}
}

func writeLiveDogfoodAcceptance(opts LiveDogfoodOptions, report *LiveDogfoodReport) error {
	manifest, err := ReadCLIManifest(opts.CLIDir)
	if err != nil {
		return fmt.Errorf("reading CLI manifest for phase5 acceptance: %w", err)
	}
	if manifest.APIName == "" {
		return fmt.Errorf("CLI manifest missing api_name; cannot write phase5 acceptance")
	}
	if manifest.RunID == "" {
		return fmt.Errorf("CLI manifest missing run_id; cannot write phase5 acceptance")
	}
	authType := manifest.AuthType
	if authType == "" {
		authType = "none"
	}

	marker := Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       manifest.APIName,
		RunID:         manifest.RunID,
		Status:        "pass",
		Level:         report.Level,
		MatrixSize:    report.MatrixSize,
		TestsPassed:   report.Passed,
		TestsFailed:   report.Failed,
		AuthContext: Phase5AuthContext{
			Type:            authType,
			APIKeyAvailable: opts.AuthEnv != "" && os.Getenv(opts.AuthEnv) != "",
		},
	}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling phase5 acceptance marker: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(opts.WriteAcceptancePath), 0o755); err != nil {
		return fmt.Errorf("creating phase5 acceptance directory: %w", err)
	}
	if err := os.WriteFile(opts.WriteAcceptancePath, data, 0o644); err != nil {
		return fmt.Errorf("writing phase5 acceptance marker: %w", err)
	}
	return nil
}

func liveDogfoodQuickCommands(commands []liveDogfoodCommand) []liveDogfoodCommand {
	if len(commands) <= 2 {
		return commands
	}
	return commands[:2]
}

func normalizeLiveDogfoodLevel(level string) (string, error) {
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "" {
		return "full", nil
	}
	switch level {
	case "quick", "full":
		return level, nil
	default:
		return "", fmt.Errorf("invalid live dogfood level %q (expected quick or full)", level)
	}
}
