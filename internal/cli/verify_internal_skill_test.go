package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeInternalSkillFixture(t *testing.T, dir, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644))
}

func TestVerifyInternalSkill_HappyPath(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "printing-press-fixture")
	writeInternalSkillFixture(t, dir, `---
name: printing-press-fixture
description: A fixture skill for testing
allowed-tools:
  - Bash
  - Read
---

# /printing-press-fixture

A fixture skill body.
`)

	report, hasError, err := runVerifyInternalSkillChecks(dir)
	require.NoError(t, err)
	assert.False(t, hasError, "expected no error-level findings, got %+v", report.Findings)
	assert.Empty(t, report.Findings, "expected no findings, got %+v", report.Findings)
}

func TestVerifyInternalSkill_MissingFrontmatter(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "printing-press-fixture")
	writeInternalSkillFixture(t, dir, `# A skill without frontmatter

This skill has no YAML frontmatter at all.
`)

	report, hasError, err := runVerifyInternalSkillChecks(dir)
	require.NoError(t, err)
	assert.True(t, hasError)
	require.Len(t, report.Findings, 1)
	assert.Equal(t, "frontmatter-parse", report.Findings[0].Check)
	assert.Equal(t, "error", report.Findings[0].Severity)
}

func TestVerifyInternalSkill_MissingRequiredFields(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "printing-press-fixture")
	writeInternalSkillFixture(t, dir, `---
name: printing-press-fixture
---

# Body
`)

	report, hasError, err := runVerifyInternalSkillChecks(dir)
	require.NoError(t, err)
	assert.True(t, hasError)
	count := 0
	for _, f := range report.Findings {
		if f.Check == "frontmatter-required" {
			count++
		}
	}
	assert.Equal(t, 2, count, "expected 2 frontmatter-required findings (description, allowed-tools), got %+v", report.Findings)
}

func TestVerifyInternalSkill_NameMismatch(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "printing-press-fixture")
	writeInternalSkillFixture(t, dir, `---
name: completely-different-name
description: A fixture skill
allowed-tools:
  - Bash
---

# Body
`)

	report, hasError, err := runVerifyInternalSkillChecks(dir)
	require.NoError(t, err)
	assert.True(t, hasError)
	found := false
	for _, f := range report.Findings {
		if f.Check == "name-matches-dir" && f.Severity == "error" {
			found = true
			assert.Contains(t, f.Detail, "completely-different-name")
			assert.Contains(t, f.Detail, "printing-press-fixture")
		}
	}
	assert.True(t, found, "expected name-matches-dir error finding, got %+v", report.Findings)
}

func TestVerifyInternalSkill_EmptyAllowedToolsEntry(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "printing-press-fixture")
	writeInternalSkillFixture(t, dir, `---
name: printing-press-fixture
description: A fixture
allowed-tools:
  - Bash
  - ""
  - Read
---

# Body
`)

	report, hasError, err := runVerifyInternalSkillChecks(dir)
	require.NoError(t, err)
	assert.True(t, hasError)
	found := false
	for _, f := range report.Findings {
		if f.Check == "allowed-tools-shape" {
			found = true
			assert.Equal(t, "error", f.Severity)
		}
	}
	assert.True(t, found, "expected allowed-tools-shape finding, got %+v", report.Findings)
}

func TestVerifyInternalSkill_NoH1Heading(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "printing-press-fixture")
	writeInternalSkillFixture(t, dir, `---
name: printing-press-fixture
description: A fixture
allowed-tools:
  - Bash
---

## Just an H2

No H1 in the body.
`)

	report, hasError, err := runVerifyInternalSkillChecks(dir)
	require.NoError(t, err)
	assert.False(t, hasError, "body-has-heading is warn-level, should not fail")
	found := false
	for _, f := range report.Findings {
		if f.Check == "body-has-heading" {
			found = true
			assert.Equal(t, "warn", f.Severity)
		}
	}
	assert.True(t, found, "expected body-has-heading warning, got %+v", report.Findings)
}

func TestVerifyInternalSkill_MissingSkillFile(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "empty-dir")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	_, _, err := runVerifyInternalSkillChecks(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no SKILL.md")
}

func TestSplitFrontmatter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		input       string
		wantFM      string
		wantBodyHas string
		wantOk      bool
	}{
		{
			name:        "happy path",
			input:       "---\nname: foo\ndescription: bar\n---\n\n# Body\n",
			wantFM:      "name: foo\ndescription: bar",
			wantBodyHas: "# Body",
			wantOk:      true,
		},
		{
			name:   "no frontmatter",
			input:  "# Just a body\n\nNo frontmatter.\n",
			wantOk: false,
		},
		{
			name:   "frontmatter never closes",
			input:  "---\nname: foo\nbody never closes\n",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, body, ok := splitFrontmatter(tt.input)
			assert.Equal(t, tt.wantOk, ok)
			if tt.wantOk {
				assert.Equal(t, tt.wantFM, strings.TrimSpace(fm))
				assert.Contains(t, body, tt.wantBodyHas)
			}
		})
	}
}

func TestHasH1Heading(t *testing.T) {
	t.Parallel()
	tests := []struct {
		body string
		want bool
	}{
		{"# Heading\n\nBody", true},
		{"## Only H2\n", false},
		{"text\n# In the middle\nmore", true},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, hasH1Heading(tt.body), "body=%q", tt.body)
	}
}
