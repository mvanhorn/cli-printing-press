package cli

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

// verifySkillScript is the full text of scripts/verify-skill/verify_skill.py
// bundled into the binary so the verification runs the same way in CI, in
// Phase 4 shipcheck, and from a developer's machine — no "is the script in
// my PATH" guesswork.
//
//go:embed verify_skill_bundled.py
var verifySkillScript string

type verifySkillRunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func runVerifySkillScript(dir string, only []string, asJSON bool, strict bool) (verifySkillRunResult, error) {
	result := verifySkillRunResult{}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return result, fmt.Errorf("resolving --dir: %w", err)
	}
	if _, err := os.Stat(filepath.Join(abs, "SKILL.md")); err != nil {
		result.ExitCode = ExitInputError
		return result, &ExitError{Code: ExitInputError, Err: fmt.Errorf("no SKILL.md in %s", abs)}
	}
	if _, err := os.Stat(filepath.Join(abs, "internal", "cli")); err != nil {
		result.ExitCode = ExitInputError
		return result, &ExitError{Code: ExitInputError, Err: fmt.Errorf("no internal/cli/ in %s", abs)}
	}

	tmpFile, err := os.CreateTemp("", "verify-skill-*.py")
	if err != nil {
		return result, fmt.Errorf("creating temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	if _, err := tmpFile.WriteString(verifySkillScript); err != nil {
		_ = tmpFile.Close()
		return result, fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return result, fmt.Errorf("closing temp file: %w", err)
	}

	pyArgs := []string{tmpFile.Name(), "--dir", abs}
	for _, o := range only {
		pyArgs = append(pyArgs, "--only", o)
	}
	if asJSON {
		pyArgs = append(pyArgs, "--json")
	}
	if strict {
		pyArgs = append(pyArgs, "--strict")
	}

	py := exec.Command("python3", pyArgs...)
	py.Stdin = os.Stdin
	var stdout, stderr bytes.Buffer
	py.Stdout = &stdout
	py.Stderr = &stderr
	runErr := py.Run()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			return result, &ExitError{
				Code:   exitErr.ExitCode(),
				Err:    fmt.Errorf("SKILL verification failed"),
				Silent: true,
			}
		}
		return result, fmt.Errorf("running verifier: %w", runErr)
	}
	return result, nil
}

func newVerifySkillCmd() *cobra.Command {
	var (
		dir    string
		only   []string
		asJSON bool
		strict bool
	)

	cmd := &cobra.Command{
		Use:           "verify-skill",
		Short:         "Verify SKILL.md matches the shipped CLI source",
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: `Run four checks against a printed CLI's SKILL.md:

  1. flag-names — every --flag referenced in SKILL.md is declared in internal/cli/*.go
  2. flag-commands — every --flag used on a specific command is declared on that command (or persistent)
  3. positional-args — positional args in bash recipes match the command's Use: field
  4. unknown-command — every referenced command path maps to a cobra Use: declaration

Fails when the SKILL advertises commands, flags, or arguments that the binary
doesn't actually provide — which is how the recipe-goat "search --max-time"
bug shipped before this gate existed. Runs the same logic as the library-repo
CI check (scripts/verify-skill/verify_skill.py) via an embedded copy so no
external script path is needed.

Requires python3 on PATH (same dependency as the cookie-auth doctor check).`,
		Example: `  # Run all checks against a generated CLI
  printing-press verify-skill --dir ./my-api-pp-cli

  # JSON output for programmatic consumption
  printing-press verify-skill --dir ./my-api-pp-cli --json

  # Only check a specific category
  printing-press verify-skill --dir ./my-api-pp-cli --only flag-commands`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--dir is required")}
			}

			result, runErr := runVerifySkillScript(dir, only, asJSON, strict)
			// Forward verifier output to the caller regardless of exit code.
			if result.Stdout != "" {
				fmt.Fprint(os.Stdout, result.Stdout)
			}
			if result.Stderr != "" {
				fmt.Fprint(os.Stderr, result.Stderr)
			}
			if runErr != nil {
				return runErr
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Path to the printed CLI directory (contains SKILL.md + internal/cli/)")
	cmd.Flags().StringSliceVar(&only, "only", nil, "Run only the named check(s): flag-names, flag-commands, positional-args, unknown-command (repeatable)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&strict, "strict", false, "Treat likely-false-positive findings as failures")

	return cmd
}
