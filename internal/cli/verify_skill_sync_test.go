// Copyright 2026 trevin-chow. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestVerifySkillScriptInSync ensures the script embedded into the binary
// (internal/cli/verify_skill_bundled.py) matches the canonical script that
// the library repo's CI runs (scripts/verify-skill/verify_skill.py).
//
// The two used to drift: a bundled vendor copy, hand-maintained, eventually
// missed checks the canonical added (most recently the unknown-command
// check, ported in U2). The fix is mechanical: a lefthook pre-commit hook
// copies canonical → bundled on every commit that touches the canonical,
// and this test catches any commit that bypasses the hook (--no-verify).
//
// To regenerate the bundled copy after editing the canonical:
//
//	cp scripts/verify-skill/verify_skill.py internal/cli/verify_skill_bundled.py
//
// Or just run lefthook:
//
//	lefthook run pre-commit
func TestVerifySkillScriptInSync(t *testing.T) {
	t.Parallel()

	repoRoot := findRepoRoot(t)
	canonical := filepath.Join(repoRoot, "scripts", "verify-skill", "verify_skill.py")
	bundled := filepath.Join(repoRoot, "internal", "cli", "verify_skill_bundled.py")

	canonicalBytes, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatalf("read canonical script %s: %v", canonical, err)
	}
	bundledBytes, err := os.ReadFile(bundled)
	if err != nil {
		t.Fatalf("read bundled script %s: %v", bundled, err)
	}

	canonicalHash := sha256.Sum256(canonicalBytes)
	bundledHash := sha256.Sum256(bundledBytes)

	if canonicalHash != bundledHash {
		t.Fatalf(
			"verify-skill scripts have diverged. "+
				"\n  scripts/verify-skill/verify_skill.py    sha256=%s (%d bytes)"+
				"\n  internal/cli/verify_skill_bundled.py    sha256=%s (%d bytes)"+
				"\n\n"+
				"The canonical script (scripts/verify-skill/verify_skill.py) is the source of truth. "+
				"To resync, run:\n\n"+
				"  cp scripts/verify-skill/verify_skill.py internal/cli/verify_skill_bundled.py\n\n"+
				"Or run lefthook:\n\n"+
				"  lefthook run pre-commit",
			hex.EncodeToString(canonicalHash[:]), len(canonicalBytes),
			hex.EncodeToString(bundledHash[:]), len(bundledBytes),
		)
	}
}

func TestVerifySkillDriftWorkflowGuardsLibraryCopy(t *testing.T) {
	t.Parallel()

	repoRoot := findRepoRoot(t)
	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "verify-skill-drift-check.yml")
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read verify-skill drift workflow %s: %v", workflowPath, err)
	}

	var workflow map[string]any
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("parse verify-skill drift workflow YAML: %v", err)
	}

	content := string(data)
	required := []string{
		"name: Verify Skill Drift",
		"schedule:",
		"cron:",
		"push:",
		"branches: [main]",
		"scripts/verify-skill/verify_skill.py",
		".github/workflows/verify-skill-drift-check.yml",
		"issues: write",
		"https://raw.githubusercontent.com/mvanhorn/printing-press-library/main/.github/scripts/verify-skill/verify_skill.py",
		"GH_TOKEN: ${{ github.token }}",
		"sha256sum",
		"cmp -s",
		"gh issue list",
		"gh issue create",
		"exit 1",
		"cp scripts/verify-skill/verify_skill.py ../printing-press-library/.github/scripts/verify-skill/verify_skill.py",
	}
	for _, want := range required {
		if !strings.Contains(content, want) {
			t.Fatalf("verify-skill drift workflow should contain %q", want)
		}
	}
}

// findRepoRoot walks up from the test file's location until it finds go.mod.
// This is more robust than relying on PWD or runtime.Caller alone, because
// `go test ./...` runs each package from its own directory.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine test file location")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root (no go.mod) starting from %s", filepath.Dir(thisFile))
		}
		dir = parent
	}
}
