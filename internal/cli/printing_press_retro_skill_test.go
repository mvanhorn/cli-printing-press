package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrintingPressRetroSkillPhaseChainIntegrity(t *testing.T) {
	t.Parallel()

	const skillDir = "../../skills/printing-press-retro"
	expected := []string{
		"01-gather-evidence.md",
		"02-mine-the-session.md",
		"03-triage-candidates.md",
		"04-classify-findings.md",
		"05-prioritize.md",
		"06-write-the-retro.md",
		"07-plannable-work-units.md",
		"08-issue-gate.md",
		"09-package-upload-present.md",
	}

	paths, err := filepath.Glob(filepath.Join(skillDir, "phases", "*.md"))
	require.NoError(t, err)
	var found []string
	for _, path := range paths {
		found = append(found, filepath.Base(path))
	}
	require.Equal(t, expected, found, "phases/ must contain exactly the expected files in order")

	router, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	require.NoError(t, err)
	require.Contains(t, string(router), "**Never execute a phase from memory. When you enter a phase, Read its file from phases/ first.**")

	rowPattern := regexp.MustCompile(`(?m)^\| ([0-9]+) \| \[phases/([^\]]+)\]\(phases/([^)]+)\) \|`)
	var indexRows []string
	for i, match := range rowPattern.FindAllStringSubmatch(string(router), -1) {
		require.Equal(t, strconv.Itoa(i+1), match[1], "phase index step must match its execution position")
		require.Equal(t, match[2], match[3], "phase index link text and target must agree")
		indexRows = append(indexRows, match[2])
	}
	require.Equal(t, expected, indexRows, "router phase index rows must list every phase file in execution order")

	routerText := string(router)
	require.Less(t, len(strings.Split(routerText, "\n")), 100, "SKILL.md must stay a thin always-loaded spine")
	require.Contains(t, routerText, "**Result:**")
	require.Contains(t, routerText, "**Next consumer:**")
	require.Contains(t, routerText, "**Done:**")
	require.Contains(t, routerText, "**Intent:**")
	require.Contains(t, routerText, `"Raised the floor" is analysis language, not a filing bar`)
	require.Contains(t, routerText, "**No P3.** File only P1/P2")
	require.Contains(t, routerText, "The issue gate files P1/P2 Do work units only")
	require.Contains(t, routerText, "references/run-resolution.md")
	require.NotContains(t, routerText, "Read every run artifact")
	require.NotContains(t, routerText, "Scan the session for candidate signals")
	require.Contains(t, routerText, "Use when the user asks to retro")

	var bundle strings.Builder
	bundle.WriteString(routerText)
	for _, name := range expected {
		data, err := os.ReadFile(filepath.Join(skillDir, "phases", name))
		require.NoError(t, err)
		bundle.WriteByte('\n')
		bundle.Write(data)
	}
	bundleText := bundle.String()
	require.Contains(t, bundleText, "Would weekday maintainer triage close this?")
	require.Contains(t, bundleText, "Put them\nin Skip when they survived deep analysis but failed the filing bar, or Drop when")
	require.Contains(t, bundleText, "print session's on-disk transcript")
	require.Contains(t, bundleText, "returns only the candidate list")

	for i, name := range expected {
		data, err := os.ReadFile(filepath.Join(skillDir, "phases", name))
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(string(data), "## "), "%s must start with an h2 section header", name)
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		last := lines[len(lines)-1]
		if i == len(expected)-1 {
			require.Equal(t, "Next: return to the router", last, "%s must close the chain", name)
		} else {
			require.Equal(t, "Next: phases/"+expected[i+1], last, "%s must point at the next phase", name)
		}
	}
}
