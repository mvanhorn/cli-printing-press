package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// internalSkillFrontmatter is the subset of fields verify-internal-skill checks.
// Other fields (e.g. user-invocable, context) are tolerated but not enforced —
// the goal is to catch drift on the load-bearing fields without forcing a rigid
// template across polish, retro, publish, amend, etc.
type internalSkillFrontmatter struct {
	Name             string   `yaml:"name"`
	Description      string   `yaml:"description"`
	AllowedTools     []string `yaml:"allowed-tools"`
	Version          string   `yaml:"version"`
	MinBinaryVersion string   `yaml:"min-binary-version"`
	Context          string   `yaml:"context"`
	UserInvocable    *bool    `yaml:"user-invocable"`
}

// internalSkillFinding mirrors canonicalFinding's shape so JSON output stays
// shape-stable across the verify-* family. Downstream consumers (skill polish,
// CI report aggregators) can parse one schema for both.
type internalSkillFinding struct {
	Check    string `json:"check"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
	Evidence string `json:"evidence,omitempty"`
}

type internalSkillReport struct {
	SkillDir  string                 `json:"skill_dir"`
	SkillPath string                 `json:"skill_path"`
	ChecksRun []string               `json:"checks_run"`
	Findings  []internalSkillFinding `json:"findings"`
}

// runVerifyInternalSkillChecks runs the static lint pass against an internal
// SKILL.md. Returns the report and a hasError flag — hasError is true when at
// least one finding has severity "error" (warn-level findings do not fail the
// verification).
func runVerifyInternalSkillChecks(dir string) (internalSkillReport, bool, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return internalSkillReport{}, false, fmt.Errorf("resolving --dir: %w", err)
	}
	skillPath := filepath.Join(abs, "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		return internalSkillReport{}, false, &ExitError{Code: ExitInputError, Err: fmt.Errorf("no SKILL.md in %s", abs)}
	}

	report := internalSkillReport{
		SkillDir:  abs,
		SkillPath: skillPath,
		ChecksRun: []string{"frontmatter-parse", "frontmatter-required", "name-matches-dir", "allowed-tools-shape", "body-has-heading"},
		Findings:  []internalSkillFinding{},
	}

	skillBytes, err := os.ReadFile(skillPath)
	if err != nil {
		return report, false, fmt.Errorf("reading SKILL.md: %w", err)
	}
	skill := string(skillBytes)

	frontmatter, body, ok := splitFrontmatter(skill)
	if !ok {
		report.Findings = append(report.Findings, internalSkillFinding{
			Check:    "frontmatter-parse",
			Severity: "error",
			Detail:   "SKILL.md does not start with a YAML frontmatter block (--- ... ---). Internal skills require frontmatter.",
		})
		return report, true, nil
	}

	var fm internalSkillFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatter), &fm); err != nil {
		report.Findings = append(report.Findings, internalSkillFinding{
			Check:    "frontmatter-parse",
			Severity: "error",
			Detail:   "YAML frontmatter does not parse",
			Evidence: err.Error(),
		})
		return report, true, nil
	}

	// frontmatter-required: name, description, allowed-tools
	if strings.TrimSpace(fm.Name) == "" {
		report.Findings = append(report.Findings, internalSkillFinding{
			Check:    "frontmatter-required",
			Severity: "error",
			Detail:   "frontmatter is missing required field: name",
		})
	}
	if strings.TrimSpace(fm.Description) == "" {
		report.Findings = append(report.Findings, internalSkillFinding{
			Check:    "frontmatter-required",
			Severity: "error",
			Detail:   "frontmatter is missing required field: description",
		})
	}
	if len(fm.AllowedTools) == 0 {
		report.Findings = append(report.Findings, internalSkillFinding{
			Check:    "frontmatter-required",
			Severity: "error",
			Detail:   "frontmatter is missing required field: allowed-tools (must be non-empty list)",
		})
	}

	// name-matches-dir: name should match the basename of the skill directory
	if fm.Name != "" {
		expected := filepath.Base(abs)
		if fm.Name != expected {
			report.Findings = append(report.Findings, internalSkillFinding{
				Check:    "name-matches-dir",
				Severity: "error",
				Detail:   fmt.Sprintf("frontmatter `name: %s` does not match directory basename `%s`", fm.Name, expected),
			})
		}
	}

	// allowed-tools-shape: each tool entry must be a non-empty string
	for i, tool := range fm.AllowedTools {
		if strings.TrimSpace(tool) == "" {
			report.Findings = append(report.Findings, internalSkillFinding{
				Check:    "allowed-tools-shape",
				Severity: "error",
				Detail:   fmt.Sprintf("allowed-tools entry %d is empty", i),
			})
		}
	}

	// body-has-heading: the SKILL.md body should have at least one H1 heading
	if !hasH1Heading(body) {
		report.Findings = append(report.Findings, internalSkillFinding{
			Check:    "body-has-heading",
			Severity: "warn",
			Detail:   "SKILL.md body has no H1 (`#`) heading; recommended convention is one H1 introducing the skill",
		})
	}

	hasError := false
	for _, f := range report.Findings {
		if f.Severity == "error" {
			hasError = true
			break
		}
	}
	return report, hasError, nil
}

// splitFrontmatter parses leading `--- ... ---` YAML frontmatter. Returns
// (frontmatter, body, true) when frontmatter is present, otherwise
// ("", original, false). Only `\n---\n` is recognized as the closing marker.
func splitFrontmatter(s string) (string, string, bool) {
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return "", s, false
	}
	rest := strings.TrimPrefix(s, "---\n")
	rest = strings.TrimPrefix(rest, "---\r\n")
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		end = strings.Index(rest, "\r\n---\r\n")
		if end < 0 {
			return "", s, false
		}
	}
	frontmatter := rest[:end]
	body := rest[end:]
	body = strings.TrimPrefix(body, "\n---\n")
	body = strings.TrimPrefix(body, "\r\n---\r\n")
	return frontmatter, body, true
}

// hasH1Heading reports whether body contains at least one line starting with `# `.
func hasH1Heading(body string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(line, "# ") {
			return true
		}
	}
	return false
}

func newVerifyInternalSkillCmd() *cobra.Command {
	var (
		dir    string
		asJSON bool
	)

	cmd := &cobra.Command{
		Use:           "verify-internal-skill",
		Short:         "Lint an internal SKILL.md (frontmatter + canonical sections)",
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: `Run static lint checks against an internal-skill SKILL.md (e.g. printing-press-polish, printing-press-retro, printing-press-amend):

  1. frontmatter-parse — leading --- ... --- block exists and parses as YAML
  2. frontmatter-required — name, description, and allowed-tools are present
  3. name-matches-dir — frontmatter name field matches the skill directory basename
  4. allowed-tools-shape — every allowed-tools entry is a non-empty string
  5. body-has-heading (warn) — body contains at least one H1 heading

Distinct from verify-skill, which validates a printed CLI's SKILL.md against the CLI's Go source. verify-internal-skill is for skills that are NOT tied to a printed CLI — the polish/retro/publish/amend family lives in skills/ and has no internal/cli/ source to verify against.`,
		Example: `  cli-printing-press verify-internal-skill --dir skills/printing-press-polish
  cli-printing-press verify-internal-skill --dir skills/printing-press-amend --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, hasError, err := runVerifyInternalSkillChecks(dir)
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(report); err != nil {
					return fmt.Errorf("encoding JSON: %w", err)
				}
			} else {
				fmt.Fprintf(os.Stdout, "verify-internal-skill: %s\n", report.SkillPath)
				if len(report.Findings) == 0 {
					fmt.Fprintln(os.Stdout, "  ✓ all checks passed")
				} else {
					for _, f := range report.Findings {
						symbol := "✘"
						if f.Severity == "warn" {
							symbol = "⚠"
						}
						fmt.Fprintf(os.Stdout, "  %s [%s] %s: %s\n", symbol, f.Severity, f.Check, f.Detail)
						if f.Evidence != "" {
							fmt.Fprintf(os.Stdout, "      evidence: %s\n", f.Evidence)
						}
					}
				}
			}

			if hasError {
				return &ExitError{
					Code:   1,
					Err:    fmt.Errorf("internal SKILL verification failed"),
					Silent: true,
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Path to the internal-skill directory (contains SKILL.md)")
	_ = cmd.MarkFlagRequired("dir")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}
