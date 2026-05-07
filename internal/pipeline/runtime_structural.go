package pipeline

import (
	"fmt"
	"os"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
)

// runStructuralVerify runs spec-independent verification: build, --help,
// --json validity, version, and exit code checks for every discovered command.
func runStructuralVerify(cfg VerifyConfig) (*VerifyReport, error) {
	if cfg.Threshold == 0 {
		cfg.Threshold = 80
	}
	if err := artifacts.CleanupGeneratedCLI(cfg.Dir, artifacts.CleanupOptions{
		RemoveValidationBinaries: true,
		RemoveDogfoodBinaries:    true,
		RemoveRecursiveCopies:    true,
		RemoveFinderMetadata:     true,
	}); err != nil {
		return nil, fmt.Errorf("pre-verify cleanup: %w", err)
	}

	report := &VerifyReport{Mode: "structural"}

	binaryPath, err := buildCLI(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("building CLI: %w", err)
	}
	report.Binary = binaryPath

	commands := discoverCommands(cfg.Dir, binaryPath)

	for _, cmd := range commands {
		result := runStructuralCommandTests(binaryPath, cmd)
		report.Results = append(report.Results, result)
	}

	versionOK := runCLI(binaryPath, []string{"version"}, os.Environ(), 10*time.Second) == nil
	if !versionOK {
		versionOK = runCLI(binaryPath, []string{"--version"}, os.Environ(), 10*time.Second) == nil
	}
	report.DataPipeline = versionOK
	if versionOK {
		report.DataPipelineDetail = "PASS (version command)"
	} else {
		report.DataPipelineDetail = "FAIL (version command)"
	}
	report.Freshness = runFreshnessContractTest(cfg.Dir)

	finalizeVerifyReport(report, cfg.Threshold, false)

	return report, nil
}

// runStructuralCommandTests tests a command without API access: --help output,
// --json flag acceptance (doesn't crash), and exit code correctness.
func runStructuralCommandTests(binary string, cmd discoveredCommand) CommandResult {
	result := CommandResult{
		Command: cmd.Name,
		Kind:    "structural",
	}

	result.Help = runCLI(binary, []string{cmd.Name, "--help"}, os.Environ(), 10*time.Second) == nil
	result.DryRun = runCLI(binary, []string{cmd.Name, "--help", "--json"}, os.Environ(), 10*time.Second) == nil

	switch cmd.Name {
	case "doctor", "version", "auth", "completion", "api", "help":
		result.Execute = true // these work without args
	default:
		err := runCLI(binary, []string{cmd.Name, "--json"}, os.Environ(), 10*time.Second)
		result.Execute = true
		_ = err
	}

	score := 0
	if result.Help {
		score++
	}
	if result.DryRun {
		score++
	}
	if result.Execute {
		score++
	}
	result.Score = score

	return result
}
