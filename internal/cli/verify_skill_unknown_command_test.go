// Copyright 2026 trevin-chow. Licensed under Apache-2.0. See LICENSE.

package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestVerifySkill_DetectsUnknownCommand integration-tests the new
// unknown-command check from U2: a SKILL that references an op-id-shaped
// path (`<cli> qr get-qrcode`) for a resource the cobra source actually
// registers as a leaf (`<cli> qr`) is rejected.
func TestVerifySkill_DetectsUnknownCommand(t *testing.T) {
	bin := buildPrintingPressBinary(t)
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))

	// Minimal cobra source: only `qr` exists as a leaf. SKILL claims `qr get-qrcode`.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "root.go"), []byte(`package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
	rootCmd.AddCommand(newQrCmd())
	return rootCmd
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "qr.go"), []byte(`package cli
import "github.com/spf13/cobra"
func newQrCmd() *cobra.Command {
	return &cobra.Command{Use: "qr <url>"}
}
`), 0o644))

	skill := `---
name: pp-fixture
description: "fixture"
---

# Fixture

## Command Reference

- ` + "`fixture-pp-cli qr get-qrcode <url>`" + ` — phantom op-id form
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(`{"cli_name":"fixture-pp-cli"}`), 0o644))

	out, err := exec.Command(bin, "verify-skill", "--dir", dir).CombinedOutput()
	require.Error(t, err, "verifier must exit non-zero when SKILL references an unknown command path")
	exitErr, ok := err.(*exec.ExitError)
	require.True(t, ok)
	require.Equal(t, 1, exitErr.ExitCode(), "exit 1 signals findings (not usage error)")
	require.Contains(t, string(out), "[unknown-command]",
		"output must label the finding as unknown-command")
	require.Contains(t, string(out), "qr get-qrcode",
		"diagnostic must name the phantom path so the SKILL author knows what to fix")
	require.Contains(t, string(out), "closest existing prefix is `fixture-pp-cli qr`",
		"diagnostic must name the closest valid prefix to guide the fix")
}

// TestVerifySkill_UnknownCommandPassesWhenAllPathsResolve confirms the
// negative case: a SKILL whose command-reference paths all map to real
// cobra Use: declarations passes the unknown-command check.
func TestVerifySkill_UnknownCommandPassesWhenAllPathsResolve(t *testing.T) {
	bin := buildPrintingPressBinary(t)
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "root.go"), []byte(`package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
	rootCmd.AddCommand(newQrCmd())
	return rootCmd
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "qr.go"), []byte(`package cli
import "github.com/spf13/cobra"
func newQrCmd() *cobra.Command {
	return &cobra.Command{Use: "qr <url>"}
}
`), 0o644))

	// SKILL uses the leaf form — the real, registered path.
	skill := `---
name: pp-fixture
description: "fixture"
---

# Fixture

## Command Reference

- ` + "`fixture-pp-cli qr <url>`" + ` — leaf form, resolves correctly
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(`{"cli_name":"fixture-pp-cli"}`), 0o644))

	out, err := exec.Command(bin, "verify-skill", "--dir", dir, "--only", "unknown-command").CombinedOutput()
	require.NoError(t, err, "unknown-command must NOT fire when every path resolves: %s", string(out))
	require.Contains(t, string(out), "All checks passed",
		"output must indicate clean pass on the unknown-command check")
}

// TestVerifySkill_UnknownCommandSkipsBuiltins confirms cobra's auto-registered
// built-in commands (help, completion, version) are whitelisted — references
// to `<cli> help` in SKILL.md must NOT fire unknown-command.
func TestVerifySkill_UnknownCommandSkipsBuiltins(t *testing.T) {
	bin := buildPrintingPressBinary(t)
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "root.go"), []byte(`package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	return &cobra.Command{Use: "fixture-pp-cli"}
}
`), 0o644))

	skill := `---
name: pp-fixture
description: "fixture"
---

# Fixture

## Command Reference

- ` + "`fixture-pp-cli help`" + ` — cobra auto-registered, must not flag
- ` + "`fixture-pp-cli completion`" + ` — cobra auto-registered, must not flag
- ` + "`fixture-pp-cli version`" + ` — common pattern, must not flag
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(`{"cli_name":"fixture-pp-cli"}`), 0o644))

	out, err := exec.Command(bin, "verify-skill", "--dir", dir, "--only", "unknown-command").CombinedOutput()
	require.NoError(t, err, "unknown-command must NOT fire on cobra builtins: %s", string(out))
	require.NotContains(t, string(out), "[unknown-command]",
		"no findings expected — help/completion/version are whitelisted")
}
