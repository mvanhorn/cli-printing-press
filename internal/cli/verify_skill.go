package cli

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/spf13/cobra"
)

// verifySkillScript is the full text of scripts/verify-skill/verify_skill.py
// bundled into the binary so the verification runs the same way in CI, in
// Phase 4 shipcheck, and from a developer's machine — no "is the script in
// my PATH" guesswork.
//
//go:embed verify_skill_bundled.py
var verifySkillScript string

const canonicalSectionsCheckName = "canonical-sections"

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
	// Caller distinguishes the two failure modes:
	//   * err != nil  — script could not run (python missing, fork failure)
	//   * result.ExitCode != 0 — script ran and reported findings
	// Pre-check failures (missing SKILL.md / internal/cli) above are signalled
	// via err with ExitError so the caller can propagate them as input errors
	// without confusing them with findings.
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return result, fmt.Errorf("running verifier: %w", runErr)
	}
	return result, nil
}

// canonicalFinding mirrors the Python script's finding shape so JSON merges
// stay shape-stable for downstream consumers (polish skill reads
// /tmp/polish-verify-skill.json).
type canonicalFinding struct {
	Check               string `json:"check"`
	Severity            string `json:"severity"`
	Command             string `json:"command"`
	Detail              string `json:"detail"`
	Evidence            string `json:"evidence"`
	LikelyFalsePositive bool   `json:"likely_false_positive"`
}

type pythonReport struct {
	CLIDir         string             `json:"cli_dir"`
	SkillPath      string             `json:"skill_path"`
	ChecksRun      []string           `json:"checks_run"`
	RecipesChecked int                `json:"recipes_checked"`
	Findings       []canonicalFinding `json:"findings"`
}

// goModVersionRE matches the `go X.Y` directive at the top of go.mod.
var goModVersionRE = regexp.MustCompile(`(?m)^go\s+(\d+)\.(\d+)`)

// runCanonicalSectionsCheck verifies that the install/prerequisites section
// of dir/SKILL.md matches what the generator would emit for this CLI today.
// Detects post-publish edits to a generator-owned section (the failure mode
// where an automation loop strips --cli-only or fabricates a slash command
// to silence a flag-names false positive).
//
// Skipped (skipped=true, finding zero, error nil) when the inputs needed to
// compute the canonical text are absent — minimal fixtures used by other
// verify-skill tests carry no .printing-press.json api_name and no go.mod,
// and forcing the check to fire on those would convert every fixture into
// a maintenance burden without catching a real bug.
func runCanonicalSectionsCheck(dir string) (finding canonicalFinding, hasFinding bool, skipped bool, err error) {
	manifest, mErr := pipeline.ReadCLIManifest(dir)
	if mErr != nil {
		return canonicalFinding{}, false, true, nil
	}
	name := manifest.APIName
	if name == "" {
		return canonicalFinding{}, false, true, nil
	}

	goModBytes, gErr := os.ReadFile(filepath.Join(dir, "go.mod"))
	if gErr != nil {
		return canonicalFinding{}, false, true, nil
	}
	usesBrowserHTTP := false
	if m := goModVersionRE.FindSubmatch(goModBytes); m != nil {
		// browser-HTTP transport raises the floor to 1.25; treat any
		// 1.25+ go directive as the browser-HTTP signal. CLIs not on
		// browser-HTTP stay on 1.23.
		if string(m[1]) == "1" && string(m[2]) >= "25" {
			usesBrowserHTTP = true
		}
	}

	skillBytes, sErr := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if sErr != nil {
		return canonicalFinding{}, false, false, fmt.Errorf("reading SKILL.md: %w", sErr)
	}
	skill := string(skillBytes)

	expected := generator.CanonicalSkillInstallSection(name, manifest.Category, usesBrowserHTTP)
	got, ok := ExtractInstallSectionForTest(skill)
	if !ok {
		return canonicalFinding{
			Check:    canonicalSectionsCheckName,
			Severity: "error",
			Command:  "(file: SKILL.md)",
			Detail:   "install section is missing or malformed; this section is generator-owned. Regenerate the printed CLI to restore it.",
			Evidence: fmt.Sprintf("expected canonical block:\n%s", expected),
		}, true, false, nil
	}
	if got == expected {
		return canonicalFinding{}, false, false, nil
	}
	return canonicalFinding{
		Check:    canonicalSectionsCheckName,
		Severity: "error",
		Command:  "(file: SKILL.md)",
		Detail:   "install section drift: hand-edit detected in a generator-owned section. Regenerate the printed CLI to restore the canonical text — do not edit this section by hand.",
		Evidence: fmt.Sprintf("expected (from generator):\n%s\n\ngot (from SKILL.md):\n%s", expected, got),
	}, true, false, nil
}

// ExtractInstallSectionForTest re-exports the generator's extractor so
// in-package tests can build fixtures and assert the runtime check
// behavior. Defined here to keep the cross-package surface narrow.
func ExtractInstallSectionForTest(skill string) (string, bool) {
	return generator.ExtractSkillInstallSection(skill)
}

// planVerifyChecks splits the user's --only selection into the Go-side
// canonical-sections check and the Python-side checks, and reports whether
// each layer should run. An empty --only means "run all checks".
func planVerifyChecks(only []string) (runCanonical bool, pyOnly []string) {
	if len(only) == 0 {
		return true, nil
	}
	for _, c := range only {
		if c == canonicalSectionsCheckName {
			runCanonical = true
			continue
		}
		pyOnly = append(pyOnly, c)
	}
	return
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
		Long: `Run five checks against a printed CLI's SKILL.md:

  1. flag-names — every --flag used on a <cli> ... invocation in SKILL.md is declared in internal/cli/*.go
  2. flag-commands — every --flag used on a specific command is declared on that command (or persistent)
  3. positional-args — positional args in bash recipes match the command's Use: field
  4. unknown-command — every referenced command path maps to a cobra Use: declaration
  5. canonical-sections — the Prerequisites: Install the CLI section matches what the generator would emit (defends against post-publish edits to generator-owned text)

Fails when the SKILL advertises commands, flags, or arguments that the binary
doesn't actually provide — which is how the recipe-goat "search --max-time"
bug shipped before this gate existed. Also fails when the install section
has been hand-edited away from the canonical generator output, which is the
failure mode that produced fabricated /ppl install slash commands and
mangled fallback blocks during polish loops.

Checks 1-4 run via the bundled scripts/verify-skill/verify_skill.py; check 5
runs in Go using the CLI manifest (.printing-press.json) and go.mod.
Requires python3 on PATH for checks 1-4.`,
		Example: `  # Run all checks against a generated CLI
  printing-press verify-skill --dir ./my-api-pp-cli

  # JSON output for programmatic consumption
  printing-press verify-skill --dir ./my-api-pp-cli --json

  # Only check a specific category
  printing-press verify-skill --dir ./my-api-pp-cli --only flag-commands
  printing-press verify-skill --dir ./my-api-pp-cli --only canonical-sections`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--dir is required")}
			}

			runCanonical, pyOnly := planVerifyChecks(only)
			runPython := len(only) == 0 || len(pyOnly) > 0

			var (
				canonicalRan     bool
				canonicalSkipped bool
				canonicalFind    canonicalFinding
				canonicalHasFind bool
			)
			if runCanonical {
				var cErr error
				canonicalFind, canonicalHasFind, canonicalSkipped, cErr = runCanonicalSectionsCheck(dir)
				if cErr != nil {
					return &ExitError{Code: ExitInputError, Err: cErr}
				}
				canonicalRan = !canonicalSkipped
			}

			var pyResult verifySkillRunResult
			if runPython {
				var runErr error
				pyResult, runErr = runVerifySkillScript(dir, pyOnly, asJSON, strict)
				if runErr != nil {
					return runErr
				}
			}

			pyHasFinding := pyResult.ExitCode != 0

			if asJSON {
				if err := emitMergedJSON(pyResult, canonicalRan, canonicalHasFind, canonicalFind); err != nil {
					return err
				}
			} else {
				if pyResult.Stdout != "" {
					fmt.Fprint(os.Stdout, pyResult.Stdout)
				}
				if pyResult.Stderr != "" {
					fmt.Fprint(os.Stderr, pyResult.Stderr)
				}
				if canonicalRan {
					if canonicalHasFind {
						fmt.Fprintln(os.Stdout, "  ✘ canonical-sections")
						fmt.Fprintf(os.Stdout, "    [%s] %s: %s\n", canonicalFind.Check, canonicalFind.Command, canonicalFind.Detail)
						if canonicalFind.Evidence != "" {
							fmt.Fprintf(os.Stdout, "      evidence:\n%s\n", indentLines(canonicalFind.Evidence, "        "))
						}
					} else {
						fmt.Fprintln(os.Stdout, "  ✓ canonical-sections passed")
					}
				}
			}

			if pyHasFinding || canonicalHasFind {
				return &ExitError{
					Code:   1,
					Err:    fmt.Errorf("SKILL verification failed"),
					Silent: true,
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Path to the printed CLI directory (contains SKILL.md + internal/cli/)")
	cmd.Flags().StringSliceVar(&only, "only", nil, "Run only the named check(s): flag-names, flag-commands, positional-args, unknown-command, canonical-sections (repeatable)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&strict, "strict", false, "Treat likely-false-positive findings as failures")

	return cmd
}

// emitMergedJSON merges Python and canonical-sections findings into one
// JSON document on stdout. When the Python script ran, parse and append;
// otherwise emit a Go-only document that mirrors the Python schema.
func emitMergedJSON(pyResult verifySkillRunResult, runCanonical, hasCanonicalFinding bool, finding canonicalFinding) error {
	var report pythonReport
	if pyResult.Stdout != "" {
		if err := json.Unmarshal([]byte(pyResult.Stdout), &report); err != nil {
			return fmt.Errorf("parsing verify-skill JSON: %w", err)
		}
	}
	if runCanonical {
		report.ChecksRun = append(report.ChecksRun, canonicalSectionsCheckName)
		if hasCanonicalFinding {
			report.Findings = append(report.Findings, finding)
		}
	}
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding merged report: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

// indentLines prefixes every line of s with prefix. Used for human-readable
// evidence blocks that need indentation matching the rest of the output.
func indentLines(s, prefix string) string {
	var b bytes.Buffer
	for line := range strings.SplitSeq(s, "\n") {
		b.WriteString(prefix)
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
