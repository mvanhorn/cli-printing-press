package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writePrintingPressSkill(t *testing.T, dir, skillVersion string) string {
	t.Helper()
	skillDir := filepath.Join(dir, "printing-press")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	path := filepath.Join(skillDir, "SKILL.md")
	content := "---\nname: printing-press\ndescription: fixture\nversion: " + skillVersion + "\nallowed-tools:\n  - Bash\n---\n\n# /printing-press\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestSkillVersionLess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		installed string
		minimum   string
		want      bool
	}{
		{installed: "2.0.0", minimum: "3.0.0", want: true},
		{installed: "3.0.0", minimum: "3.0.0", want: false},
		{installed: "3.1.0", minimum: "3.0.0", want: false},
		{installed: "", minimum: "3.0.0", want: true},
		{installed: "not-a-version", minimum: "3.0.0", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.installed+"<"+tt.minimum, func(t *testing.T) {
			assert.Equal(t, tt.want, skillVersionLess(tt.installed, tt.minimum))
		})
	}
}

func TestParseSkillFrontmatterVersion(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "2.0.0", parseSkillFrontmatterVersion("---\nname: printing-press\nversion: 2.0.0\n---\n\n# Body\n"))
	assert.Equal(t, "3.0.0", parseSkillFrontmatterVersion("---\nname: printing-press\nversion: \"3.0.0\"\n---\n\n# Body\n"))
	assert.Equal(t, "", parseSkillFrontmatterVersion("# no frontmatter\n"))
	assert.Equal(t, "", parseSkillFrontmatterVersion("---\nname: printing-press\n---\n\n# Body\n"))
}

func TestCheckSkillFileStalePreRewriteVersion(t *testing.T) {
	t.Parallel()
	path := writePrintingPressSkill(t, t.TempDir(), "2.0.0")

	report, err := checkSkillFile(path)
	require.NoError(t, err)
	assert.Equal(t, skillStatusStale, report.Status)
	assert.Equal(t, "2.0.0", report.Installed)
	assert.Equal(t, MinSkillVersion, report.Required)
	assert.Equal(t, version.Version, report.BinaryVersion)
	assert.Contains(t, report.Detail, "2.0.0")
	assert.Contains(t, report.Detail, MinSkillVersion)
	assert.Contains(t, report.Reinstall, "--skills-only")
	assert.Contains(t, formatSkillCompatText(report), "[skill-stale]")
}

func TestCheckSkillFileCurrentVersion(t *testing.T) {
	t.Parallel()
	path := writePrintingPressSkill(t, t.TempDir(), MinSkillVersion)

	report, err := checkSkillFile(path)
	require.NoError(t, err)
	assert.Equal(t, skillStatusCurrent, report.Status)
	assert.Equal(t, MinSkillVersion, report.Installed)
	assert.Empty(t, report.Reinstall)
	assert.NotContains(t, formatSkillCompatText(report), "[skill-stale]")
}

func TestCheckSkillFileMissingVersionIsStale(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "printing-press")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "SKILL.md")
	require.NoError(t, os.WriteFile(path, []byte("---\nname: printing-press\ndescription: fixture\nallowed-tools:\n  - Bash\n---\n\n# Body\n"), 0o644))

	report, err := checkSkillFile(path)
	require.NoError(t, err)
	assert.Equal(t, skillStatusStale, report.Status)
	assert.Empty(t, report.Installed)
}

func TestDiscoverInstalledSkillCompat(t *testing.T) {
	home := t.TempDir()
	writePrintingPressSkill(t, filepath.Join(home, ".claude", "skills"), "2.0.0")

	report := discoverInstalledSkillCompat(home)
	assert.Equal(t, skillStatusStale, report.Status)
	assert.Equal(t, "2.0.0", report.Installed)
	assert.True(t, strings.HasSuffix(report.Path, filepath.Join(".claude", "skills", "printing-press", "SKILL.md")))
}

func TestDiscoverInstalledSkillCompatMissingIsNotStale(t *testing.T) {
	t.Parallel()
	report := discoverInstalledSkillCompat(t.TempDir())
	assert.Equal(t, skillStatusMissing, report.Status)
	assert.NotContains(t, formatSkillCompatText(report), "[skill-stale]")
}

func TestDiscoverInstalledSkillCompatCurrentDoesNotMaskStaleSibling(t *testing.T) {
	home := t.TempDir()
	writePrintingPressSkill(t, filepath.Join(home, ".claude", "skills"), MinSkillVersion)
	writePrintingPressSkill(t, filepath.Join(home, ".codex", "skills"), "2.0.0")

	report := discoverInstalledSkillCompat(home)
	assert.Equal(t, skillStatusStale, report.Status)
	assert.Equal(t, "2.0.0", report.Installed)
	assert.True(t, strings.HasSuffix(report.Path, filepath.Join(".codex", "skills", "printing-press", "SKILL.md")))

	payload := versionJSONPayload(home)
	assert.Equal(t, skillStatusStale, payload.SkillStatus)
	assert.Equal(t, "2.0.0", payload.InstalledSkillVersion)
	assert.Contains(t, payload.InstalledSkillPath, filepath.Join(".codex", "skills", "printing-press", "SKILL.md"))
}

func TestDiscoverInstalledSkillCompatReportsOldestAmongStaleSiblings(t *testing.T) {
	home := t.TempDir()
	writePrintingPressSkill(t, filepath.Join(home, ".claude", "skills"), "2.1.0")
	writePrintingPressSkill(t, filepath.Join(home, ".agents", "skills"), "2.0.0")

	report := discoverInstalledSkillCompat(home)
	assert.Equal(t, skillStatusStale, report.Status)
	assert.Equal(t, "2.0.0", report.Installed)
	assert.True(t, strings.HasSuffix(report.Path, filepath.Join(".agents", "skills", "printing-press", "SKILL.md")))
}

func TestVersionJSONIncludesMinSkillVersionWithoutDriftFieldsWhenCurrent(t *testing.T) {
	home := t.TempDir()
	writePrintingPressSkill(t, filepath.Join(home, ".claude", "skills"), MinSkillVersion)

	payload := versionJSONPayload(home)
	assert.Equal(t, version.Version, payload.Version)
	assert.Equal(t, MinSkillVersion, payload.MinSkillVersion)
	assert.Empty(t, payload.SkillStatus)
	assert.Empty(t, payload.InstalledSkillVersion)
	assert.NotEmpty(t, payload.Go)
}

func TestVersionJSONReportsStaleInstalledSkill(t *testing.T) {
	home := t.TempDir()
	writePrintingPressSkill(t, filepath.Join(home, ".claude", "skills"), "2.0.0")

	payload := versionJSONPayload(home)
	assert.Equal(t, skillStatusStale, payload.SkillStatus)
	assert.Equal(t, "2.0.0", payload.InstalledSkillVersion)
	assert.Equal(t, MinSkillVersion, payload.MinSkillVersion)
}

func TestVersionCommandJSONDoesNotTouchHome(t *testing.T) {
	decoy := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(decoy, ".config"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(decoy, ".local", "share"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(decoy, ".local", "state"), 0o755))
	sentinel := filepath.Join(decoy, ".config", "sentinel")
	require.NoError(t, os.WriteFile(sentinel, []byte("sentinel\n"), 0o644))

	t.Setenv("HOME", decoy)
	t.Setenv("USERPROFILE", decoy)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(decoy, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(decoy, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(decoy, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(decoy, ".cache"))

	writePrintingPressSkill(t, filepath.Join(decoy, ".claude", "skills"), "2.0.0")

	cmd := NewRootCommand(CanonicalBinaryName)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"version", "--json"})
	require.NoError(t, cmd.Execute())

	var payload versionJSON
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &payload))
	assert.Equal(t, skillStatusStale, payload.SkillStatus)
	assert.Contains(t, stderr.String(), "warning:")
	assert.Contains(t, stderr.String(), "2.0.0")

	got, err := os.ReadFile(sentinel)
	require.NoError(t, err)
	assert.Equal(t, "sentinel\n", string(got))
}

func TestSkillCompatCommandFailsClosedOnStaleSkill(t *testing.T) {
	path := writePrintingPressSkill(t, t.TempDir(), "2.0.0")

	cmd := NewRootCommand(CanonicalBinaryName)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"skill-compat", "--skill", path})
	err := cmd.Execute()
	require.Error(t, err)
	exitErr := asExitError(err)
	require.NotNil(t, exitErr)
	assert.Equal(t, ExitInputError, exitErr.Code)
	assert.Contains(t, stdout.String(), "[skill-stale]")
	assert.Contains(t, stderr.String(), "warning:")
}

func TestSkillCompatCommandOKOnCurrentSkill(t *testing.T) {
	path := writePrintingPressSkill(t, t.TempDir(), MinSkillVersion)

	cmd := NewRootCommand(CanonicalBinaryName)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"skill-compat", "--skill", path, "--json"})
	require.NoError(t, cmd.Execute())
	assert.Empty(t, stderr.String())

	var report skillCompatReport
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &report))
	assert.Equal(t, skillStatusCurrent, report.Status)
}

func TestPrintingPressSkillFrontmatterMeetsBinaryFloor(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "skills", "printing-press", "SKILL.md"))
	require.NoError(t, err)
	got := parseSkillFrontmatterVersion(string(data))
	require.NotEmpty(t, got, "printing-press SKILL.md must declare version:")
	assert.False(t, skillVersionLess(got, MinSkillVersion),
		"printing-press SKILL.md version %s is below binary MinSkillVersion %s", got, MinSkillVersion)
}
