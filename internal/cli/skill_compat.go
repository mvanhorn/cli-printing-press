package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/version"
	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

// MinSkillVersion is the oldest printing-press skill frontmatter `version`
// this binary treats as current. Bump it together with the skill `version:`
// field (and the setup-contract `# skill-version:` comment) when skill shape
// changes so a stale install would follow deleted commands. Distinct from
// min-binary-version, which is the other direction (skill requires binary).
const MinSkillVersion = "3.0.0"

const (
	printingPressSkillName = "printing-press"
	skillsReinstallHint    = "curl -fsSL https://raw.githubusercontent.com/mvanhorn/cli-printing-press/main/scripts/install.sh | bash -s -- --skills-only"
	skillStatusStale       = "stale"
	skillStatusCurrent     = "current"
	skillStatusMissing     = "missing"
)

// skillCompatReport is the JSON shape for `skill-compat --json` and the
// optional skill-drift fields on `version --json`.
type skillCompatReport struct {
	Status        string `json:"skill_status"`
	Installed     string `json:"installed_skill_version,omitempty"`
	Required      string `json:"min_skill_version"`
	Path          string `json:"installed_skill_path,omitempty"`
	BinaryVersion string `json:"binary_version"`
	Reinstall     string `json:"reinstall,omitempty"`
	Detail        string `json:"detail,omitempty"`
}

func skillVersionLess(installed, minimum string) bool {
	a := normalizeSemver(installed)
	b := normalizeSemver(minimum)
	if !semver.IsValid(a) || !semver.IsValid(b) {
		// Unparseable or missing installed version cannot prove currency.
		return true
	}
	return semver.Compare(a, b) < 0
}

func parseSkillFrontmatterVersion(skillMD string) string {
	frontmatter, _, ok := splitFrontmatter(skillMD)
	if !ok {
		return ""
	}
	var fm internalSkillFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatter), &fm); err != nil {
		return ""
	}
	return strings.TrimSpace(fm.Version)
}

func readSkillFileVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return parseSkillFrontmatterVersion(string(data)), nil
}

func installedPrintingPressSkillPaths(home string) []string {
	if home == "" {
		return nil
	}
	candidates := []string{
		filepath.Join(home, ".claude", "skills", printingPressSkillName, "SKILL.md"),
		filepath.Join(home, ".agents", "skills", printingPressSkillName, "SKILL.md"),
		filepath.Join(home, ".codex", "skills", printingPressSkillName, "SKILL.md"),
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "agents", "skills", printingPressSkillName, "SKILL.md"))
	}
	return candidates
}

func evaluateSkillCompat(skillPath, installedVersion string) skillCompatReport {
	report := skillCompatReport{
		Required:      MinSkillVersion,
		BinaryVersion: version.Version,
		Path:          skillPath,
		Installed:     strings.TrimSpace(installedVersion),
		Reinstall:     skillsReinstallHint,
	}
	if strings.TrimSpace(skillPath) == "" && report.Installed == "" {
		report.Status = skillStatusMissing
		report.Detail = "no printing-press SKILL.md found in well-known install locations"
		report.Reinstall = ""
		return report
	}
	if report.Installed == "" || skillVersionLess(report.Installed, MinSkillVersion) {
		installed := report.Installed
		if installed == "" {
			installed = "none"
		}
		report.Status = skillStatusStale
		report.Detail = fmt.Sprintf("installed printing-press skill declares version %s; binary v%s expects >= %s — reinstall via: %s",
			installed, version.Version, MinSkillVersion, skillsReinstallHint)
		return report
	}
	report.Status = skillStatusCurrent
	report.Reinstall = ""
	return report
}

func checkSkillFile(path string) (skillCompatReport, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return skillCompatReport{}, err
	}
	installed, err := readSkillFileVersion(abs)
	if err != nil {
		return skillCompatReport{}, err
	}
	return evaluateSkillCompat(abs, installed), nil
}

func discoverInstalledSkillCompat(home string) skillCompatReport {
	var (
		firstCurrent skillCompatReport
		haveCurrent  bool
		worstStale   skillCompatReport
		haveStale    bool
	)
	for _, path := range installedPrintingPressSkillPaths(home) {
		installed, err := readSkillFileVersion(path)
		if err != nil {
			continue
		}
		report := evaluateSkillCompat(path, installed)
		if report.Status == skillStatusStale {
			if !haveStale || installedSkillOlder(report.Installed, worstStale.Installed) {
				worstStale = report
				haveStale = true
			}
			continue
		}
		if !haveCurrent {
			firstCurrent = report
			haveCurrent = true
		}
	}
	if haveStale {
		return worstStale
	}
	if haveCurrent {
		return firstCurrent
	}
	return evaluateSkillCompat("", "")
}

// installedSkillOlder reports whether a is older than b for drift ranking.
// Missing or unparseable versions rank older than any valid semver.
func installedSkillOlder(a, b string) bool {
	aN := normalizeSemver(a)
	bN := normalizeSemver(b)
	aOK := semver.IsValid(aN)
	bOK := semver.IsValid(bN)
	switch {
	case !aOK && !bOK:
		return false
	case !aOK:
		return true
	case !bOK:
		return false
	default:
		return semver.Compare(aN, bN) < 0
	}
}

func writeSkillStaleWarning(w io.Writer, report skillCompatReport) {
	if report.Status != skillStatusStale || strings.TrimSpace(report.Detail) == "" {
		return
	}
	fmt.Fprintf(w, "warning: %s\n", report.Detail)
}

func formatSkillCompatText(report skillCompatReport) string {
	switch report.Status {
	case skillStatusStale:
		var b strings.Builder
		fmt.Fprintf(&b, "[skill-stale] printing-press skill v%s is older than binary v%s expects (>= %s)\n",
			valueOrNone(report.Installed), report.BinaryVersion, report.Required)
		fmt.Fprintf(&b, "PRESS_SKILL_INSTALLED=%s\n", valueOrNone(report.Installed))
		fmt.Fprintf(&b, "PRESS_SKILL_REQUIRED=%s\n", report.Required)
		if report.Path != "" {
			fmt.Fprintf(&b, "PRESS_SKILL_PATH=%s\n", report.Path)
		}
		fmt.Fprintf(&b, "PRESS_SKILL_REINSTALL=%s\n", report.Reinstall)
		return b.String()
	case skillStatusMissing:
		return "skill-compat: no installed printing-press SKILL.md found in well-known locations\n"
	default:
		return fmt.Sprintf("skill-compat: printing-press skill v%s meets binary floor v%s\n",
			report.Installed, report.Required)
	}
}

func valueOrNone(v string) string {
	if strings.TrimSpace(v) == "" {
		return "none"
	}
	return v
}

type versionJSON struct {
	Version               string `json:"version"`
	Go                    string `json:"go"`
	MinSkillVersion       string `json:"min_skill_version"`
	SkillStatus           string `json:"skill_status,omitempty"`
	InstalledSkillVersion string `json:"installed_skill_version,omitempty"`
	InstalledSkillPath    string `json:"installed_skill_path,omitempty"`
}

func versionJSONPayload(home string) versionJSON {
	payload := versionJSON{
		Version:         version.Version,
		Go:              runtime.Version(),
		MinSkillVersion: MinSkillVersion,
	}
	report := discoverInstalledSkillCompat(home)
	if report.Status == skillStatusStale {
		payload.SkillStatus = report.Status
		payload.InstalledSkillVersion = report.Installed
		payload.InstalledSkillPath = report.Path
	}
	return payload
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.Getenv("HOME")
	}
	return home
}

func newSkillCompatCmd() *cobra.Command {
	var (
		skillPath string
		asJSON    bool
	)

	cmd := &cobra.Command{
		Use:           "skill-compat",
		Short:         "Check installed printing-press skill version against this binary",
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: `Compare a printing-press SKILL.md frontmatter version against the floor this binary expects.

Without --skill, well-known install locations under the user home are scanned.
A missing install is not an error (the skill may have loaded from a plugin).
A skill older than the floor prints [skill-stale] and exits 1.`,
		Example: `  cli-printing-press skill-compat
  cli-printing-press skill-compat --skill ~/.claude/skills/printing-press/SKILL.md
  cli-printing-press skill-compat --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				report skillCompatReport
				err    error
			)
			if strings.TrimSpace(skillPath) != "" {
				report, err = checkSkillFile(skillPath)
				if err != nil {
					return &ExitError{Code: ExitInputError, Err: fmt.Errorf("reading --skill: %w", err)}
				}
			} else {
				report = discoverInstalledSkillCompat(userHomeDir())
			}

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(report); err != nil {
					return fmt.Errorf("encoding JSON: %w", err)
				}
			} else {
				fmt.Fprint(cmd.OutOrStdout(), formatSkillCompatText(report))
			}

			if report.Status == skillStatusStale {
				writeSkillStaleWarning(cmd.ErrOrStderr(), report)
				return &ExitError{
					Code:   ExitInputError,
					Err:    fmt.Errorf("installed skill is older than this binary expects"),
					Silent: true,
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&skillPath, "skill", "", "Path to a printing-press SKILL.md (skips install-location scan)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}
