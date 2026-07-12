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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

func TestVerifySkill_UnknownCommandIgnoresNaturalLanguageCliProse(t *testing.T) {
	t.Parallel()

	bin := buildPrintingPressBinary(t)
	dir := t.TempDir()

	skill := `---
name: pp-fixture
description: "fixture"
---

# Fixture

fixture-pp-cli wraps the official Fixture API with typed commands for every endpoint.
`
	writeVerifySkillFixture(t, dir, map[string]string{
		"root.go": `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	return &cobra.Command{Use: "fixture-pp-cli"}
}
`,
	}, skill)

	out, err := exec.Command(bin, "verify-skill", "--dir", dir, "--only", "unknown-command").CombinedOutput()
	require.NoError(t, err, "natural-language prose must not be treated as a command reference: %s", string(out))
	require.NotContains(t, string(out), "[unknown-command]")
}

func TestVerifySkill_ProseInvocationWithFlagIsChecked(t *testing.T) {
	t.Parallel()

	bin := buildPrintingPressBinary(t)
	dir := t.TempDir()

	skill := `---
name: pp-fixture
description: "fixture"
---

# Fixture

Use fixture-pp-cli sync --base USD when you need a specific base currency.
`
	writeVerifySkillFixture(t, dir, map[string]string{
		"root.go": `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
	rootCmd.AddCommand(newSyncCmd())
	return rootCmd
}
`,
		"sync.go": `package cli
import "github.com/spf13/cobra"
func newSyncCmd() *cobra.Command {
	return &cobra.Command{Use: "sync"}
}
`,
	}, skill)

	out, err := exec.Command(bin, "verify-skill", "--dir", dir, "--only", "flag-commands").CombinedOutput()
	require.Error(t, err, "prose that contains an invocation-shaped flag must still be verified")
	require.Contains(t, string(out), "--base is not declared anywhere")
	require.Contains(t, string(out), "SKILL.md")
}

func TestVerifySkill_ProseInvocationFlagNameIsChecked(t *testing.T) {
	t.Parallel()

	bin := buildPrintingPressBinary(t)
	dir := t.TempDir()

	skill := `---
name: pp-fixture
description: "fixture"
---

# Fixture

Use fixture-pp-cli sync --base USD when you need a specific base currency.
`
	writeVerifySkillFixture(t, dir, map[string]string{
		"root.go": `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
	rootCmd.AddCommand(newSyncCmd())
	return rootCmd
}
`,
		"sync.go": `package cli
import "github.com/spf13/cobra"
func newSyncCmd() *cobra.Command {
	return &cobra.Command{Use: "sync"}
}
`,
	}, skill)

	out, err := exec.Command(bin, "verify-skill", "--dir", dir, "--only", "flag-names").CombinedOutput()
	require.Error(t, err, "prose invocation flags must still be checked by flag-names")
	require.Contains(t, string(out), "--base is referenced")
	require.Contains(t, string(out), "SKILL.md")
}

func TestVerifySkill_ProseInvocationDoesNotAffectPositionalArgs(t *testing.T) {
	t.Parallel()

	bin := buildPrintingPressBinary(t)
	dir := t.TempDir()

	skill := `---
name: pp-fixture
description: "fixture"
---

# Fixture

Use fixture-pp-cli search pizza --limit 5 when you need a filtered search.
`
	writeVerifySkillFixture(t, dir, map[string]string{
		"root.go": `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
	rootCmd.AddCommand(newSearchCmd())
	return rootCmd
}
`,
		"search.go": `package cli
import "github.com/spf13/cobra"
func newSearchCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{Use: "search <query>"}
	cmd.Flags().IntVar(&limit, "limit", 10, "Limit")
	return cmd
}
`,
	}, skill)

	out, err := exec.Command(bin, "verify-skill", "--dir", dir, "--only", "positional-args").CombinedOutput()
	require.NoError(t, err, "plain-prose invocations must not be counted as bash recipes for positional args: %s", string(out))
}

func TestVerifySkill_ProseInvocationSplitsMultipleCliMentions(t *testing.T) {
	t.Parallel()

	bin := buildPrintingPressBinary(t)
	dir := t.TempDir()

	skill := `---
name: pp-fixture
description: "fixture"
---

# Fixture

Use fixture-pp-cli sync --base USD or fixture-pp-cli export --format csv for local handoffs.
`
	writeVerifySkillFixture(t, dir, map[string]string{
		"root.go": `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
	rootCmd.AddCommand(newSyncCmd())
	rootCmd.AddCommand(newExportCmd())
	return rootCmd
}
`,
		"sync.go": `package cli
import "github.com/spf13/cobra"
func newSyncCmd() *cobra.Command {
	return &cobra.Command{Use: "sync"}
}
`,
		"export.go": `package cli
import "github.com/spf13/cobra"
func newExportCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{Use: "export"}
	cmd.Flags().StringVar(&format, "format", "", "Format")
	return cmd
}
`,
	}, skill)

	out, err := exec.Command(bin, "verify-skill", "--dir", dir, "--only", "flag-commands").CombinedOutput()
	require.Error(t, err, "bad sync flag should still be reported")
	require.Contains(t, string(out), "--base is not declared anywhere")
	require.NotContains(t, string(out), "--format is declared elsewhere but not on sync")
}

func TestVerifySkill_ProseInvocationIgnoresBareSeparatorBetweenMentions(t *testing.T) {
	t.Parallel()

	bin := buildPrintingPressBinary(t)
	dir := t.TempDir()

	skill := `---
name: pp-fixture
description: "fixture"
---

# Fixture

Use fixture-pp-cli sync --base USD -- fixture-pp-cli export --format csv for local handoffs.
`
	writeVerifySkillFixture(t, dir, map[string]string{
		"root.go": `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
	rootCmd.AddCommand(newSyncCmd())
	rootCmd.AddCommand(newExportCmd())
	return rootCmd
}
`,
		"sync.go": `package cli
import "github.com/spf13/cobra"
func newSyncCmd() *cobra.Command {
	var base string
	cmd := &cobra.Command{Use: "sync"}
	cmd.Flags().StringVar(&base, "base", "", "Base")
	return cmd
}
`,
		"export.go": `package cli
import "github.com/spf13/cobra"
func newExportCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{Use: "export"}
	cmd.Flags().StringVar(&format, "format", "", "Format")
	return cmd
}
`,
	}, skill)

	out, err := exec.Command(bin, "verify-skill", "--dir", dir, "--only", "flag-names").CombinedOutput()
	require.NoError(t, err, "bare -- separator between prose mentions must not produce an empty-flag finding: %s", string(out))
	require.NotContains(t, string(out), "[flag-names]")
}
