package cli_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSkillsInheritCodexModelFromConfig is the regression guard for the
// 2026-04-26 codex-model auto-default fix.
//
// Codex CLI delegations in printing-press skill files must inherit the
// user's ~/.codex/config.toml default model and reasoning effort.
// Pinning -m "gpt-...", --model "gpt-...", model_reasoning_effort=...,
// or invoking the non-existent `codex config get model` subcommand
// causes every codex run to drift from the user's actual configured
// model and freezes the skill at whatever literal string is hardcoded.
//
// The test walks every markdown file under skills/ and fails on any of
// those patterns. Vendored content under */vendor/* is excluded.
func TestSkillsInheritCodexModelFromConfig(t *testing.T) {
	skillsRoot := "../../skills"
	info, err := os.Stat(skillsRoot)
	require.NoError(t, err, "skills/ directory must exist")
	require.True(t, info.IsDir(), "skills/ must be a directory")

	// Forbidden patterns. Any match in a skill markdown file is a
	// violation.
	forbidden := []struct {
		pattern *regexp.Regexp
		reason  string
	}{
		{
			regexp.MustCompile(`-m\s+"gpt-`),
			`hardcoded -m "gpt-..." (use codex's config.toml default instead)`,
		},
		{
			regexp.MustCompile(`--model\s+"gpt-`),
			`hardcoded --model "gpt-..." (use codex's config.toml default instead)`,
		},
		{
			regexp.MustCompile(`model_reasoning_effort\s*=\s*`),
			`hardcoded model_reasoning_effort= (inherits from config.toml)`,
		},
		{
			regexp.MustCompile(`codex\s+config\s+get\s+model`),
			"calls non-existent `codex config get model` subcommand " +
				"(grep ~/.codex/config.toml directly instead)",
		},
	}

	// Files allowed to mention forbidden patterns. The test file itself
	// quotes the patterns; release notes and changelogs may reference
	// the historical hardcoded values.
	allowlist := map[string]bool{
		filepath.Clean("../../internal/cli/codex_model_invariant_test.go"): true,
	}

	var violations []string

	walkErr := filepath.WalkDir(skillsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip vendored content.
			if strings.Contains(path, "/vendor/") || strings.HasSuffix(path, "/vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		if allowlist[filepath.Clean(path)] {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		for lineNum, line := range strings.Split(string(data), "\n") {
			for _, f := range forbidden {
				if f.pattern.MatchString(line) {
					rel := strings.TrimPrefix(path, "../../")
					violations = append(violations,
						"  "+rel+":"+itoa(lineNum+1)+": "+f.reason+
							"\n    > "+strings.TrimSpace(line))
				}
			}
		}
		return nil
	})
	require.NoError(t, walkErr)

	if len(violations) > 0 {
		t.Fatalf("Skill files must inherit Codex model from "+
			"~/.codex/config.toml. Found pinned models or invalid "+
			"config calls:\n%s\n\n"+
			"Fix: remove `-m \"gpt-...\"` and "+
			"`-c 'model_reasoning_effort=...'` from `codex exec` calls. "+
			"For display, replace `codex config get model` with a "+
			"`grep ^model ~/.codex/config.toml` pattern.",
			strings.Join(violations, "\n"))
	}
}

// itoa is a small wrapper to avoid pulling strconv into this file's
// import list when the tests already use require/strings/regexp.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
