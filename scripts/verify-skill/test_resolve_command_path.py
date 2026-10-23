"""Focused unit tests for resolve_command_path and helpers.

Run with: python3 -m pytest scripts/verify-skill/test_resolve_command_path.py
or: python3 scripts/verify-skill/test_resolve_command_path.py

These tests cover retro #301 finding F1: the shared-leaf disambiguation
that the legacy specificity heuristic got wrong.
"""
from __future__ import annotations

import sys
import tempfile
import unittest
from unittest.mock import patch
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import verify_skill  # noqa: E402
from verify_skill import (  # noqa: E402
    collect_command_constructors,
    find_root_children,
    resolve_command_path,
    find_command_source,
    run_checks,
    extract_recipes,
    _cli_invocation_from_tokens,
    _extract_function_body,
    _extract_prose_invocations,
)


def _write_cli(tmp: Path, files: dict[str, str]) -> Path:
    """Materialize a synthetic CLI under tmp/internal/cli/<name>.go and
    return tmp (the cli_dir)."""
    cli_dir = tmp / "internal" / "cli"
    cli_dir.mkdir(parents=True, exist_ok=True)
    for name, content in files.items():
        (cli_dir / name).write_text(content)
    return tmp


class TestExtractFunctionBody(unittest.TestCase):
    def test_simple_body(self):
        text = "func foo() {\n  return 1\n}\n"
        body = _extract_function_body(text, text.index("{") + 1)
        self.assertIn("return 1", body)

    def test_braces_inside_string(self):
        text = 'func foo() {\n  s := "{not a brace}"\n  return 1\n}\n'
        body = _extract_function_body(text, text.index("{") + 1)
        self.assertIn("return 1", body)

    def test_braces_inside_raw_string(self):
        text = "func foo() {\n  s := `{still not a brace}`\n  return 2\n}\n"
        body = _extract_function_body(text, text.index("{") + 1)
        self.assertIn("return 2", body)

    def test_braces_inside_line_comment(self):
        text = "func foo() {\n  // ignored brace }\n  return 3\n}\n"
        body = _extract_function_body(text, text.index("{") + 1)
        self.assertIn("return 3", body)

    def test_braces_inside_block_comment(self):
        text = "func foo() {\n  /* { ignored } */\n  return 4\n}\n"
        body = _extract_function_body(text, text.index("{") + 1)
        self.assertIn("return 4", body)

    def test_unclosed_returns_none(self):
        text = "func foo() {\n  return 5\n"  # missing closing brace
        body = _extract_function_body(text, text.index("{") + 1)
        self.assertIsNone(body)


class TestCollectAndResolve(unittest.TestCase):
    def test_resolves_top_level_when_leaf_collides_with_subcommand(self):
        """Retro #301 F1: top-level `save <url>` and `profile save <name>`
        share leaf 'save'. cmd_path=['save'] must resolve to the top-level
        save_cmd.go, not profile.go's profile-save subcommand."""
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func Execute() error {
    rootCmd := &cobra.Command{Use: "demo-pp-cli"}
    rootCmd.AddCommand(newSaveCmd())
    rootCmd.AddCommand(newProfileCmd())
    return rootCmd.Execute()
}
''',
                "save_cmd.go": '''package cli
import "github.com/spf13/cobra"
func newSaveCmd() *cobra.Command {
    cmd := &cobra.Command{Use: "save <url>"}
    return cmd
}
''',
                "profile.go": '''package cli
import "github.com/spf13/cobra"
func newProfileCmd() *cobra.Command {
    cmd := &cobra.Command{Use: "profile"}
    cmd.AddCommand(newProfileSaveCmd())
    return cmd
}
func newProfileSaveCmd() *cobra.Command {
    return &cobra.Command{Use: "save <name> [--<flag> <value> ...]"}
}
''',
            })

            files, use, _ = find_command_source(cli_dir, ["save"])
            self.assertEqual([f.name for f in files], ["save_cmd.go"])
            self.assertEqual(use, "save <url>")

            files, use, _ = find_command_source(cli_dir, ["profile", "save"])
            self.assertEqual([f.name for f in files], ["profile.go"])
            self.assertEqual(use, "save <name> [--<flag> <value> ...]")

    def test_constructor_collection(self):
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "auth.go": '''package cli
import "github.com/spf13/cobra"
func newAuthCmd() *cobra.Command {
    cmd := &cobra.Command{Use: "auth"}
    cmd.AddCommand(newAuthLoginCmd())
    cmd.AddCommand(newAuthLogoutCmd())
    return cmd
}
func newAuthLoginCmd() *cobra.Command {
    return &cobra.Command{Use: "login <token>"}
}
func newAuthLogoutCmd() *cobra.Command {
    return &cobra.Command{Use: "logout"}
}
''',
            })
            ctors = collect_command_constructors(cli_dir)
            self.assertEqual(set(ctors), {"newAuthCmd", "newAuthLoginCmd", "newAuthLogoutCmd"})
            self.assertEqual(ctors["newAuthCmd"].use, "auth")
            self.assertEqual(set(ctors["newAuthCmd"].children), {"newAuthLoginCmd", "newAuthLogoutCmd"})
            self.assertEqual(ctors["newAuthLoginCmd"].use, "login <token>")

    def test_root_children_discovery(self):
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func Execute() error {
    rootCmd := &cobra.Command{Use: "x-pp-cli"}
    rootCmd.AddCommand(newAuthCmd())
    rootCmd.AddCommand(newDoctorCmd())
    return rootCmd.Execute()
}
''',
                "auth.go": '''package cli
import "github.com/spf13/cobra"
func newAuthCmd() *cobra.Command { return &cobra.Command{Use: "auth"} }
''',
                "doctor.go": '''package cli
import "github.com/spf13/cobra"
func newDoctorCmd() *cobra.Command { return &cobra.Command{Use: "doctor"} }
''',
            })
            self.assertEqual(set(find_root_children(cli_dir)), {"newAuthCmd", "newDoctorCmd"})

    def test_unresolvable_path_returns_none(self):
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func Execute() error {
    rootCmd := &cobra.Command{Use: "x"}
    rootCmd.AddCommand(newFooCmd())
    return rootCmd.Execute()
}
''',
                "foo.go": '''package cli
import "github.com/spf13/cobra"
func newFooCmd() *cobra.Command { return &cobra.Command{Use: "foo"} }
''',
            })
            file, use, _ = resolve_command_path(cli_dir, ["nonexistent"])
            self.assertIsNone(file)
            self.assertIsNone(use)

    def test_constructor_with_func_typed_param(self):
        """Retro #303 review item #6: CONSTRUCTOR_RE must match
        constructors whose signatures include function-typed
        parameters like `func(int) error`. Without nested-paren
        handling the regex stops at the first `)` of the inner
        func type and silently drops the constructor from the
        constructor map."""
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "callback.go": '''package cli
import "github.com/spf13/cobra"
func newCallbackCmd(handler func() error, fallback func(int) string) *cobra.Command {
    return &cobra.Command{Use: "callback"}
}
''',
            })
            from verify_skill import collect_command_constructors
            ctors = collect_command_constructors(cli_dir)
            self.assertIn("newCallbackCmd", ctors,
                "regex must handle func() and func(int) parameter types")
            self.assertEqual(ctors["newCallbackCmd"].use, "callback")

    def test_collect_command_constructors_is_cached(self):
        """Retro #303 review item #5: collect_command_constructors
        is wrapped with lru_cache so verify-skill's per-recipe
        find_command_source loop doesn't re-scan internal/cli/*.go
        for every recipe. The cache key is the cli_dir Path."""
        from verify_skill import collect_command_constructors
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "foo.go": '''package cli
import "github.com/spf13/cobra"
func newFooCmd() *cobra.Command { return &cobra.Command{Use: "foo"} }
''',
            })
            first = collect_command_constructors(cli_dir)
            second = collect_command_constructors(cli_dir)
            self.assertIs(first, second,
                "second call must return the cached object, not rescan")

    def test_legacy_fallback_when_no_root_addcommand(self):
        """When the CLI doesn't follow the standard rootCmd.AddCommand
        pattern (no root.go, or different convention), the legacy
        specificity heuristic still finds something usable."""
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                # No root.go — just a single command file
                "search.go": '''package cli
import "github.com/spf13/cobra"
func newSearchCmd() *cobra.Command {
    return &cobra.Command{Use: "search <query>"}
}
''',
            })
            files, use, _ = find_command_source(cli_dir, ["search"])
            # Legacy fallback returns the file; not empty
            self.assertEqual([f.name for f in files], ["search.go"])
            self.assertEqual(use, "search <query>")


class TestFlagChecks(unittest.TestCase):
    def test_alias_receiver_flags_are_recognized(self):
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func Execute() error {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    rootCmd.AddCommand(newSearchCmd())
    return rootCmd.Execute()
}
''',
                "search.go": '''package cli
import "github.com/spf13/cobra"
func newSearchCmd() *cobra.Command {
    cmd := &cobra.Command{Use: "search"}
    var level string
    f := cmd.Flags()
    f.StringVar(&level, "level", "", "filter level")
    return cmd
}
''',
            })
            cli_binary = f"{cli_dir.name}-pp-cli"
            (cli_dir / "SKILL.md").write_text(
                f"""# Fixture

```bash
{cli_binary} search --level high
```
""",
                encoding="utf-8",
            )

            report = run_checks(cli_dir, {"flag-names", "flag-commands"})

            self.assertEqual([], report.findings)

    def test_missing_limit_flag_is_reported(self):
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func Execute() error {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    rootCmd.AddCommand(newSearchCmd())
    return rootCmd.Execute()
}
''',
                "search.go": '''package cli
import "github.com/spf13/cobra"
func newSearchCmd() *cobra.Command {
    return &cobra.Command{Use: "search"}
}
''',
            })
            cli_binary = f"{cli_dir.name}-pp-cli"
            (cli_dir / "SKILL.md").write_text(
                f"""# Fixture

```bash
{cli_binary} search --limit 5
{cli_binary} search --limit 10
```
""",
                encoding="utf-8",
            )

            report = run_checks(cli_dir, {"flag-names", "flag-commands"})
            details = {(f.check, f.detail) for f in report.findings}

            self.assertIn(
                ("flag-names", "--limit is referenced in SKILL.md but not declared in any internal/cli/*.go"),
                details,
            )
            self.assertIn(
                ("flag-commands", "--limit is not declared anywhere"),
                details,
            )
            self.assertEqual(
                1,
                sum(
                    1
                    for f in report.findings
                    if f.check == "flag-commands"
                    and f.command == f"{cli_binary} search"
                    and f.detail == "--limit is not declared anywhere"
                ),
            )


class TestInvocationParsing(unittest.TestCase):
    def test_optional_positionals_are_bound_before_sibling_resolution(self):
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func Execute() error {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    rootCmd.AddCommand(newIssuancesCmd())
    rootCmd.AddCommand(newRrCmd())
    return rootCmd.Execute()
}
''',
                "issuances.go": '''package cli
import "github.com/spf13/cobra"
func newIssuancesCmd() *cobra.Command {
    cmd := &cobra.Command{Use: "issuances"}
    cmd.AddCommand(newCitedByCmd())
    return cmd
}
func newCitedByCmd() *cobra.Command {
    return &cobra.Command{Use: "cited-by [type] [number-year]"}
}
''',
                "rr.go": '''package cli
import "github.com/spf13/cobra"
func newRrCmd() *cobra.Command {
    return &cobra.Command{Use: "rr"}
}
''',
            })
            cli_binary = f"{cli_dir.name}-pp-cli"
            skill = cli_dir / "SKILL.md"
            skill.write_text(
                f"""# Fixture

```bash
{cli_binary} issuances cited-by rr 7-2003
```
""",
                encoding="utf-8",
            )

            recipes = extract_recipes(skill, cli_binary, cli_dir)

            self.assertEqual([(["issuances", "cited-by"], ["rr", "7-2003"], [])], recipes)

    def test_trailing_quote_is_stripped_from_flag_token(self):
        """Prose that quotes a full command (`'cli cmd --refresh'`) glues the
        closing quote onto the flag token; the declaration lookup must see
        `--refresh`, not `--refresh'`. A genuinely undeclared flag still keeps
        its real name after the quote is trimmed."""
        _path, _positional, flags = _cli_invocation_from_tokens(
            ["entitlements", "--refresh'", "to", "map"], None,
        )
        self.assertEqual(["--refresh"], flags)

        _path, _positional, flags = _cli_invocation_from_tokens(
            ["get", "--bogus')."], None,
        )
        self.assertEqual(["--bogus"], flags)

    def test_space_separated_long_flag_value_is_not_positional(self):
        cmd_path, positional, flags = _cli_invocation_from_tokens(
            ["search", "--filter", "status=active", "item-123"],
            None,
        )

        self.assertEqual(["search"], cmd_path)
        self.assertEqual(["item-123"], positional)
        self.assertEqual(["--filter"], flags)

    def test_boolean_long_flag_does_not_swallow_following_positional(self):
        """A value-less boolean flag (e.g. `--json`) takes no value, so the
        token after it is a positional and must not be consumed as the flag's
        value. A value-bearing flag in the same CLI still consumes its value."""
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func Execute() error {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    rootCmd.AddCommand(newGetCmd())
    return rootCmd.Execute()
}
''',
                "get.go": '''package cli
import "github.com/spf13/cobra"
func newGetCmd() *cobra.Command {
    cmd := &cobra.Command{Use: "get <id>"}
    var asJSON bool
    var filter string
    cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
    cmd.Flags().StringVar(&filter, "filter", "", "filter expression")
    return cmd
}
''',
            })

            # Boolean flag: the next token is a positional, not the flag's value.
            cmd_path, positional, flags = _cli_invocation_from_tokens(
                ["get", "--json", "item-123"], cli_dir,
            )
            self.assertEqual(["get"], cmd_path)
            self.assertEqual(
                ["item-123"], positional,
                "a positional after a boolean flag must not be swallowed as its value",
            )
            self.assertEqual(["--json"], flags)

            # Value-bearing flag still consumes its space-separated value.
            cmd_path, positional, flags = _cli_invocation_from_tokens(
                ["get", "--filter", "active", "item-123"], cli_dir,
            )
            self.assertEqual(["item-123"], positional)
            self.assertEqual(["--filter"], flags)


class UTF8ReadTest(unittest.TestCase):
    def test_read_text_uses_explicit_utf8_encoding(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "SKILL.md"
            path.write_text("# 한국어 테스트\n", encoding="utf-8")

            seen = []
            original = Path.read_text

            def spy(self, *args, **kwargs):
                seen.append(kwargs.get("encoding"))
                return original(self, *args, **kwargs)

            with patch.object(Path, "read_text", spy):
                self.assertIn("한국어", verify_skill.read_utf8(path))

            self.assertEqual(seen, ["utf-8"])


class TestExtractProseInvocations(unittest.TestCase):
    """Prose flag extraction must not leak wrapping quotes into flag tokens.

    A single-quoted prose command like `'<cli> auth login --chrome'` cannot be
    balanced by shlex, so extraction falls back to `str.split()`; without
    quote stripping the closing quote leaks as the phantom flag `--chrome'`.
    """

    AUTH_GO = (
        "package cli\n"
        'import "github.com/spf13/cobra"\n'
        "func newAuthCmd() *cobra.Command {\n"
        '  c := &cobra.Command{Use: "auth"}\n'
        "  return c\n"
        "}\n"
        "func newAuthLoginCmd() *cobra.Command {\n"
        '  c := &cobra.Command{Use: "login"}\n'
        '  c.Flags().Bool("chrome", false, "use chrome")\n'
        "  return c\n"
        "}\n"
    )

    def _cli_dir(self, tmp: str) -> Path:
        return _write_cli(Path(tmp), {"auth.go": self.AUTH_GO})

    def test_single_quoted_command_strips_trailing_quote(self):
        with tempfile.TemporaryDirectory() as tmp:
            cli_dir = self._cli_dir(tmp)
            text = "Run 'mycli auth login --chrome' to authenticate."
            results = _extract_prose_invocations(text, "mycli", cli_dir)
            flags = [f for _cmd, _pos, fl, _surface in results for f in fl]
            self.assertIn("--chrome", flags)
            self.assertNotIn("--chrome'", flags)
            self.assertFalse(
                any(f.endswith("'") or f.endswith('"') for f in flags),
                f"flag tokens leaked a wrapping quote: {flags}",
            )

    def test_double_quoted_command_strips_trailing_quote(self):
        with tempfile.TemporaryDirectory() as tmp:
            cli_dir = self._cli_dir(tmp)
            # The opening quote sits after the binary, so the extracted
            # fragment (`auth login --chrome" to authenticate.`) carries an
            # unbalanced `"` that shlex.split rejects, forcing the
            # fragment.split() fallback to yield `--chrome"`. Without the
            # trailing-quote strip the flag token would leak that quote.
            text = 'Run "mycli auth login --chrome" to authenticate.'
            results = _extract_prose_invocations(text, "mycli", cli_dir)
            flags = [f for _cmd, _pos, fl, _surface in results for f in fl]
            self.assertIn("--chrome", flags)
            self.assertFalse(
                any(f.endswith("'") or f.endswith('"') for f in flags),
                f"flag tokens leaked a wrapping quote: {flags}",
            )

    def test_undeclared_flag_in_prose_still_extracted(self):
        # The quote fix must not suppress genuinely undeclared flags: a real
        # but undeclared flag still surfaces so downstream flag-name checks
        # can fail it.
        with tempfile.TemporaryDirectory() as tmp:
            cli_dir = self._cli_dir(tmp)
            text = "Run mycli auth login --bogusflag to break things."
            results = _extract_prose_invocations(text, "mycli", cli_dir)
            flags = [f for _cmd, _pos, fl, _surface in results for f in fl]
            self.assertIn("--bogusflag", flags)


NOVEL_ROOT_GO = '''package cli
import "github.com/spf13/cobra"

// Device-template comment must not register newMyCmd.
//
//	func init() {
//		registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
//			root.AddCommand(newMyCmd(flags))
//		})
//	}

const sample = "registerNovelCommand(func(root *cobra.Command, flags *rootFlags) { addNovelCommandIfAbsent(root, newFakeFromStringCmd(flags)) })"

func newRootCmd(flags *rootFlags) *cobra.Command {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    rootCmd.AddCommand(newAccountCmd(flags))
    rootCmd.AddCommand(newProfileCmd(flags))
    addNovelCommandIfAbsent(rootCmd, newNovelAuditCmd(flags))
    return rootCmd
}
'''

NOVEL_ACCOUNT_GO = '''package cli
import "github.com/spf13/cobra"
func newAccountCmd(flags *rootFlags) *cobra.Command {
    cmd := &cobra.Command{Use: "account"}
    cmd.AddCommand(newAccountGetCmd(flags))
    addNovelCommandIfAbsent(cmd, newNovelAccountEstimateCmd(flags))
    return cmd
}
func newAccountGetCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "get <id>"}
}
'''

NOVEL_ESTIMATE_GO = '''package cli
import "github.com/spf13/cobra"
func newNovelAccountEstimateCmd(flags *rootFlags) *cobra.Command {
    var posts string
    cmd := &cobra.Command{Use: "estimate"}
    cmd.Flags().StringVar(&posts, "posts", "", "Post filter")
    return cmd
}
'''

NOVEL_BUDGET_GO = '''package cli
import "github.com/spf13/cobra"
func newNovelAccountBudgetCmd(flags *rootFlags) *cobra.Command {
    var limit int
    cmd := &cobra.Command{Use: "budget"}
    cmd.Flags().IntVar(&limit, "limit", 10, "Row limit")
    return cmd
}
'''

NOVEL_PROFILE_GO = '''package cli
import "github.com/spf13/cobra"
func newProfileCmd(flags *rootFlags) *cobra.Command {
    cmd := &cobra.Command{Use: "profile"}
    cmd.AddCommand(newProfileSaveCmd(flags))
    cmd.AddCommand(newProfileShowCmd(flags))
    return cmd
}
func newProfileSaveCmd(flags *rootFlags) *cobra.Command {
    var label string
    cmd := &cobra.Command{Use: "save <name>"}
    cmd.Flags().StringVar(&label, "label", "", "Profile label")
    return cmd
}
func newProfileShowCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "show <name>"}
}
'''

NOVEL_CHECKPOINT_GO = '''package cli
import "github.com/spf13/cobra"
func newCheckpointCmd(flags *rootFlags) *cobra.Command {
    cmd := &cobra.Command{Use: "checkpoint"}
    addNovelCommandIfAbsent(cmd, newCheckpointSaveCmd(flags))
    return cmd
}
func newCheckpointSaveCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "save"}
}
'''

NOVEL_WATCHLIST_GO = '''package cli
import "github.com/spf13/cobra"
func newWatchlistCmd(flags *rootFlags) *cobra.Command {
    cmd := &cobra.Command{Use: "watchlist"}
    cmd.AddCommand(newWatchlistShowCmd(flags))
    return cmd
}
func newWatchlistShowCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "show"}
}
'''

NOVEL_AUDIT_GO = '''package cli
import "github.com/spf13/cobra"
func newNovelAuditCmd(flags *rootFlags) *cobra.Command {
    var since string
    cmd := &cobra.Command{Use: "audit"}
    cmd.Flags().StringVar(&since, "since", "", "Since when")
    return cmd
}
'''

NOVEL_DEVICE_GO = '''package cli
import "github.com/spf13/cobra"
func newDeviceStyleCmd(flags *rootFlags) *cobra.Command {
    var mode string
    cmd := &cobra.Command{Use: "device-style"}
    cmd.Flags().StringVar(&mode, "mode", "", "Mode")
    return cmd
}
func newWrappedCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "wrapped"}
}
'''

NOVEL_ORPHAN_GO = '''package cli
import "github.com/spf13/cobra"
func newOrphanCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "orphan <id>"}
}
func newFakeFromStringCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "fake-from-string"}
}
func newMyCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "my"}
}
func newNopeCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "nope"}
}
'''

NOVEL_REGISTER_GO = '''package cli
import "github.com/spf13/cobra"
func init() {
    registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newCheckpointCmd(flags))
        addNovelCommandIfAbsent(root, newWatchlistCmd(flags))
        accountCmd, _, err := root.Find([]string{"account"})
        if err == nil {
            addNovelCommandIfAbsent(accountCmd, newNovelAccountBudgetCmd(flags))
        }
    })
}
func init() {
    registerNovelCommand(registerDeviceStyle)
}
func registerDeviceStyle(root *cobra.Command, flags *rootFlags) {
    root.AddCommand(newDeviceStyleCmd(flags))
}
func init() {
    registerNovelCommand(func(
        root *cobra.Command,
        flags *rootFlags,
    ) {
        addNovelCommandIfAbsent(root, newWrappedCmd(flags))
    })
}
func init() {
    registerNovelCommand(func(flags *rootFlags) {
        addNovelCommandIfAbsent(root, newNopeCmd(flags))
    })
}
'''


def _novel_cli(tmp: Path) -> Path:
    return _write_cli(Path(tmp), {
        "root.go": NOVEL_ROOT_GO,
        "account.go": NOVEL_ACCOUNT_GO,
        "estimate.go": NOVEL_ESTIMATE_GO,
        "budget.go": NOVEL_BUDGET_GO,
        "profile.go": NOVEL_PROFILE_GO,
        "checkpoint.go": NOVEL_CHECKPOINT_GO,
        "watchlist.go": NOVEL_WATCHLIST_GO,
        "audit.go": NOVEL_AUDIT_GO,
        "device.go": NOVEL_DEVICE_GO,
        "orphan.go": NOVEL_ORPHAN_GO,
        "fixture_register.go": NOVEL_REGISTER_GO,
    })


class TestNovelCommandRegistration(unittest.TestCase):
    def test_hook_and_helper_commands_resolve_past_shared_leaves(self):
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _novel_cli(Path(td))
            roots = set(find_root_children(cli_dir))
            self.assertIn("newCheckpointCmd", roots)
            self.assertIn("newWatchlistCmd", roots)
            self.assertIn("newNovelAuditCmd", roots)
            self.assertIn("newDeviceStyleCmd", roots)
            self.assertIn("newWrappedCmd", roots)
            self.assertIn("newAccountCmd", roots)
            self.assertIn("newProfileCmd", roots)
            for orphan in ("newOrphanCmd", "newFakeFromStringCmd", "newMyCmd", "newNopeCmd", "newNovelAccountBudgetCmd"):
                self.assertNotIn(orphan, roots, orphan)

            ctors = collect_command_constructors(cli_dir)
            self.assertIn("newNovelAccountEstimateCmd", ctors["newAccountCmd"].children)
            self.assertIn("newNovelAccountBudgetCmd", ctors["newAccountCmd"].children)
            self.assertIn("newCheckpointSaveCmd", ctors["newCheckpointCmd"].children)
            self.assertNotIn("newOrphanCmd", ctors["newAccountCmd"].children)

            file, use, _ = resolve_command_path(cli_dir, ["checkpoint", "save"])
            self.assertEqual(file.name, "checkpoint.go")
            self.assertEqual(use, "save")

            file, use, _ = resolve_command_path(cli_dir, ["profile", "save"])
            self.assertEqual(file.name, "profile.go")
            self.assertEqual(use, "save <name>")

            file, use, _ = resolve_command_path(cli_dir, ["watchlist", "show"])
            self.assertEqual(file.name, "watchlist.go")
            self.assertEqual(use, "show")

            file, use, _ = resolve_command_path(cli_dir, ["profile", "show"])
            self.assertEqual(use, "show <name>")

            file, use, _ = resolve_command_path(cli_dir, ["account", "estimate"])
            self.assertEqual(file.name, "estimate.go")
            self.assertEqual(use, "estimate")

            file, use, _ = resolve_command_path(cli_dir, ["account", "budget"])
            self.assertEqual(file.name, "budget.go")

            file, _, _ = resolve_command_path(cli_dir, ["orphan"])
            self.assertIsNone(file)
            file, _, _ = resolve_command_path(cli_dir, ["my"])
            self.assertIsNone(file)
            file, _, _ = resolve_command_path(cli_dir, ["nope"])
            self.assertIsNone(file)

    def test_skill_checks_pass_for_hook_and_grouped_novels(self):
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _novel_cli(Path(td))
            binary = f"{cli_dir.name}-pp-cli"
            (cli_dir / "SKILL.md").write_text(
                f"""# Fixture

```bash
{binary} account estimate --posts recent
{binary} account budget --limit 5
{binary} account get acct_123
{binary} checkpoint save
{binary} watchlist show
{binary} profile save alice
{binary} profile show bob
{binary} audit --since 7d
{binary} device-style --mode safe
{binary} wrapped
```
""",
                encoding="utf-8",
            )
            report = run_checks(cli_dir, {"flag-commands", "positional-args", "unknown-command"})
            self.assertEqual([], report.findings, [f.detail for f in report.findings])

    def test_subcommand_flags_stay_on_that_subcommand(self):
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _novel_cli(Path(td))
            binary = f"{cli_dir.name}-pp-cli"
            (cli_dir / "SKILL.md").write_text(
                f"""# Fixture

```bash
{binary} account estimate --label x
{binary} account budget --posts recent
{binary} account --posts recent
{binary} checkpoint save extra
{binary} watchlist show extra
```
""",
                encoding="utf-8",
            )
            report = run_checks(cli_dir, {"flag-commands", "positional-args"})
            details = {(f.check, f.command, f.detail) for f in report.findings}
            self.assertIn(
                ("flag-commands", f"{binary} account estimate", "--label is declared elsewhere but not on account estimate"),
                details,
            )
            self.assertIn(
                ("flag-commands", f"{binary} account budget", "--posts is declared elsewhere but not on account budget"),
                details,
            )
            self.assertIn(
                ("flag-commands", f"{binary} account", "--posts is declared elsewhere but not on account"),
                details,
            )
            positional = [f for f in report.findings if f.check == "positional-args"]
            self.assertTrue(any("checkpoint save" in f.command and "got 1 positional" in f.detail for f in positional))
            self.assertTrue(any("watchlist show" in f.command and "got 1 positional" in f.detail for f in positional))
            self.assertFalse(any(f.likely_false_positive for f in positional))

    def test_cross_file_named_hook_and_fields_find(self):
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func newRootCmd(flags *rootFlags) *cobra.Command {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    rootCmd.AddCommand(newIssuesCmd(flags))
    return rootCmd
}
''',
                "issues.go": '''package cli
import "github.com/spf13/cobra"
func newIssuesCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "issues"}
}
''',
                "fixture_register.go": '''package cli
func init() { registerNovelCommand(registerSync) }
''',
                "sync_wire.go": '''package cli
import "github.com/spf13/cobra"
func registerSync(root *cobra.Command, flags *rootFlags) {
    addNovelCommandIfAbsent(
        root,
        newSyncCmd(flags),
    )
    if parent, _, err := root.Find(strings.Fields("issues")); err == nil {
        addNovelCommandIfAbsent(parent, newIssuesStaleCmd(flags))
    }
}
''',
                "sync.go": '''package cli
import "github.com/spf13/cobra"
func newSyncCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "sync"}
}
''',
                "stale.go": '''package cli
import "github.com/spf13/cobra"
func newIssuesStaleCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "stale"}
}
''',
                "orphan.go": '''package cli
import "github.com/spf13/cobra"
func newOrphanCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "orphan"}
}
''',
            })
            self.assertIn("newSyncCmd", find_root_children(cli_dir))
            file, use, _ = resolve_command_path(cli_dir, ["issues", "stale"])
            self.assertEqual(file.name, "stale.go")
            self.assertEqual(use, "stale")
            file, _, _ = resolve_command_path(cli_dir, ["orphan"])
            self.assertIsNone(file)

    def test_unclosed_hook_does_not_hide_a_later_hook(self):
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    rootCmd.AddCommand(newLiveCmd())
    return rootCmd
}
''',
                "live.go": '''package cli
import "github.com/spf13/cobra"
func newLiveCmd() *cobra.Command { return &cobra.Command{Use: "live"} }
''',
                "fixture_register.go": '''package cli
func init() {
    registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newBrokenCmd(flags))
    // hook left unclosed; a later hook must still register
func init() {
    registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newStaleCmd(flags))
    })
}
''',
                "stale.go": '''package cli
import "github.com/spf13/cobra"
func newStaleCmd(flags *rootFlags) *cobra.Command { return &cobra.Command{Use: "stale"} }
''',
                "broken.go": '''package cli
import "github.com/spf13/cobra"
func newBrokenCmd(flags *rootFlags) *cobra.Command { return &cobra.Command{Use: "broken"} }
''',
            })
            roots = set(find_root_children(cli_dir))
            self.assertIn("newStaleCmd", roots)
            self.assertNotIn("newBrokenCmd", roots)
            self.assertIn("newLiveCmd", roots)

    def test_test_file_hook_is_ignored(self):
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    rootCmd.AddCommand(newLiveCmd())
    return rootCmd
}
''',
                "live.go": '''package cli
import "github.com/spf13/cobra"
func newLiveCmd() *cobra.Command { return &cobra.Command{Use: "live"} }
''',
                "from_test_register_test.go": '''package cli
func init() {
    registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newFromTestCmd(flags))
    })
}
''',
                "from_test.go": '''package cli
import "github.com/spf13/cobra"
func newFromTestCmd(flags *rootFlags) *cobra.Command { return &cobra.Command{Use: "from-test"} }
''',
            })
            roots = set(find_root_children(cli_dir))
            self.assertEqual(roots, {"newLiveCmd"})
            file, _, _ = resolve_command_path(cli_dir, ["from-test"])
            self.assertIsNone(file)

    def test_selector_call_is_not_a_hook(self):
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    rootCmd.AddCommand(newLiveCmd())
    return rootCmd
}
''',
                "live.go": '''package cli
import "github.com/spf13/cobra"
func newLiveCmd() *cobra.Command { return &cobra.Command{Use: "live"} }
''',
                "selector.go": '''package cli
import "github.com/spf13/cobra"
func init() {
    cli.registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newSelectorCmd(flags))
    })
    cli.registerNovelCommand(registerSelector)
}
func registerSelector(root *cobra.Command, flags *rootFlags) {
    addNovelCommandIfAbsent(root, newSelectorNamedCmd(flags))
}
func newSelectorCmd(flags *rootFlags) *cobra.Command { return &cobra.Command{Use: "selector"} }
func newSelectorNamedCmd(flags *rootFlags) *cobra.Command { return &cobra.Command{Use: "selector-named"} }
''',
            })
            roots = set(find_root_children(cli_dir))
            self.assertEqual(roots, {"newLiveCmd"})
            for path in (["selector"], ["selector-named"]):
                file, _, _ = resolve_command_path(cli_dir, path)
                self.assertIsNone(file, path)

    def test_implemented_command_replaces_novel_scaffold(self):
        # root.go attaches the scaffold first. A later hook attaches the
        # implementation (and a second real command) under the same Use.
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func newRootCmd(flags *rootFlags) *cobra.Command {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    rootCmd.AddCommand(newAccountCmd(flags))
    addNovelCommandIfAbsent(rootCmd, newNovelAuditCmd(flags))
    addNovelCommandIfAbsent(rootCmd, newTodoCmd(flags))
    return rootCmd
}
''',
                "account.go": '''package cli
import "github.com/spf13/cobra"
func newAccountCmd(flags *rootFlags) *cobra.Command {
    cmd := &cobra.Command{Use: "account"}
    addNovelCommandIfAbsent(cmd, newNovelAccountEstimateCmd(flags))
    return cmd
}
''',
                "audit_scaffold.go": '''package cli
import "github.com/spf13/cobra"
func newNovelAuditCmd(flags *rootFlags) *cobra.Command {
    var since string
    cmd := &cobra.Command{
        Use: "audit",
        Annotations: map[string]string{
            "pp:novel-scaffold": "true",
        },
    }
    cmd.Flags().StringVar(&since, "since", "", "Since when")
    return cmd
}
''',
                "estimate_scaffold.go": '''package cli
import "github.com/spf13/cobra"
func newNovelAccountEstimateCmd(flags *rootFlags) *cobra.Command {
    var since string
    cmd := &cobra.Command{Use: "estimate"}
    cmd.Annotations = map[string]string{}
    cmd.Annotations["pp:novel-scaffold"] = "true"
    cmd.Flags().StringVar(&since, "since", "", "Since when")
    return cmd
}
''',
                "audit_live.go": '''package cli
import "github.com/spf13/cobra"
func newAuditCmd(flags *rootFlags) *cobra.Command {
    // "pp:novel-scaffold": "true"
    const doc = `"pp:novel-scaffold": "true"`
    _ = doc
    var window string
    cmd := &cobra.Command{
        Use: "audit <id>",
        Annotations: map[string]string{"pp:novel-scaffold": "false"},
    }
    cmd.Flags().StringVar(&window, "window", "", "Window")
    return cmd
}
''',
                "audit_extra.go": '''package cli
import "github.com/spf13/cobra"
func newAuditExtraCmd(flags *rootFlags) *cobra.Command {
    var extra string
    cmd := &cobra.Command{Use: "audit"}
    cmd.Flags().StringVar(&extra, "extra", "", "Extra")
    return cmd
}
''',
                "estimate_live.go": '''package cli
import "github.com/spf13/cobra"
func newEstimateCmd(flags *rootFlags) *cobra.Command {
    var window string
    cmd := &cobra.Command{Use: "estimate <id>"}
    cmd.Flags().StringVar(&window, "window", "", "Window")
    return cmd
}
''',
                "todo.go": '''package cli
import "github.com/spf13/cobra"
func newTodoCmd(flags *rootFlags) *cobra.Command {
    var dry string
    cmd := &cobra.Command{
        Use: "todo",
        Annotations: map[string]string{"pp:novel-scaffold": "true"},
    }
    cmd.Flags().StringVar(&dry, "dry", "", "Dry run marker")
    return cmd
}
''',
                "zz_hook.go": '''package cli
import "github.com/spf13/cobra"
func init() {
    registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newAuditCmd(flags))
        root.AddCommand(newAuditExtraCmd(flags))
        accountCmd, _, err := root.Find([]string{"account"})
        if err == nil {
            addNovelCommandIfAbsent(accountCmd, newEstimateCmd(flags))
        }
    })
}
''',
            })
            file, use, _ = resolve_command_path(cli_dir, ["audit"])
            self.assertEqual(file.name, "audit_live.go")
            self.assertEqual(use, "audit <id>")
            file, use, _ = resolve_command_path(cli_dir, ["account", "estimate"])
            self.assertEqual(file.name, "estimate_live.go")
            self.assertEqual(use, "estimate <id>")
            file, use, _ = resolve_command_path(cli_dir, ["todo"])
            self.assertEqual(file.name, "todo.go")
            self.assertEqual(use, "todo")

            roots = find_root_children(cli_dir)
            self.assertNotIn("newNovelAuditCmd", roots)
            self.assertIn("newAuditCmd", roots)
            self.assertIn("newAuditExtraCmd", roots)
            self.assertIn("newTodoCmd", roots)
            ctors = collect_command_constructors(cli_dir)
            self.assertNotIn("newNovelAccountEstimateCmd", ctors["newAccountCmd"].children)
            self.assertIn("newEstimateCmd", ctors["newAccountCmd"].children)
            self.assertTrue(ctors["newNovelAuditCmd"].scaffold)
            self.assertTrue(ctors["newNovelAccountEstimateCmd"].scaffold)
            self.assertTrue(ctors["newTodoCmd"].scaffold)
            self.assertFalse(ctors["newAuditCmd"].scaffold)
            self.assertFalse(ctors["newAuditExtraCmd"].scaffold)

            binary = f"{cli_dir.name}-pp-cli"
            (cli_dir / "SKILL.md").write_text(
                f"""# Fixture

```bash
{binary} audit --window
{binary} audit ACCT123
{binary} audit --since
{binary} audit --extra
{binary} audit ACCT123 EXTRA
{binary} account estimate --window
{binary} account estimate ACCT123
{binary} account estimate --since
{binary} account estimate ACCT123 EXTRA
{binary} todo --dry
```
""",
                encoding="utf-8",
            )
            report = run_checks(cli_dir, {"flag-commands", "positional-args", "unknown-command"})
            details = {(f.check, f.command, f.detail) for f in report.findings}
            self.assertIn(
                ("flag-commands", f"{binary} audit", "--since is declared elsewhere but not on audit"),
                details,
            )
            self.assertIn(
                ("flag-commands", f"{binary} audit", "--extra is declared elsewhere but not on audit"),
                details,
            )
            self.assertIn(
                (
                    "flag-commands",
                    f"{binary} account estimate",
                    "--since is declared elsewhere but not on account estimate",
                ),
                details,
            )
            positional = [f for f in report.findings if f.check == "positional-args"]
            self.assertTrue(any(
                f.command == f"{binary} audit" and 'Use: "audit <id>"' in f.detail and "got 2 positional" in f.detail
                and not f.likely_false_positive
                for f in positional
            ), [f.detail for f in positional])
            self.assertTrue(any(
                f.command == f"{binary} account estimate" and 'Use: "estimate <id>"' in f.detail
                and "got 2 positional" in f.detail and not f.likely_false_positive
                for f in positional
            ), [f.detail for f in positional])
            self.assertFalse(any("got 1 positional" in f.detail for f in positional), [f.detail for f in positional])
            self.assertFalse(any(f.check == "flag-commands" and "--window" in f.detail for f in report.findings))
            self.assertFalse(any(f.check == "flag-commands" and "--dry" in f.detail for f in report.findings))
            self.assertFalse(any(f.check == "unknown-command" for f in report.findings))

    def test_addcommand_on_find_parent_registers_child(self):
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func newRootCmd(flags *rootFlags) *cobra.Command {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    rootCmd.AddCommand(newAccountCmd(flags))
    rootCmd.AddCommand(newProfileCmd(flags))
    return rootCmd
}
''',
                "account.go": '''package cli
import "github.com/spf13/cobra"
func newAccountCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "account"}
}
''',
                "profile.go": '''package cli
import "github.com/spf13/cobra"
func newProfileCmd(flags *rootFlags) *cobra.Command {
    cmd := &cobra.Command{Use: "profile"}
    cmd.AddCommand(newProfileSaveCmd(flags))
    return cmd
}
func newProfileSaveCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "save <name>"}
}
''',
                "save_cmd.go": '''package cli
import "github.com/spf13/cobra"
func newCheckpointSaveCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "save"}
}
''',
                "wrapped.go": '''package cli
import "github.com/spf13/cobra"
func newWrappedCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "wrapped"}
}
''',
                "decoys.go": '''package cli
import "github.com/spf13/cobra"
func newCommentedCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "commented"}
}
func newFakeCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "fake"}
}
func newGhostCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "ghost"}
}
func newMysteryCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "mystery"}
}
''',
                "zz_hook.go": '''package cli
import "github.com/spf13/cobra"
func init() {
    registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
        // accountCmd.AddCommand(newCommentedCmd(flags))
        const sample = "accountCmd.AddCommand(newFakeCmd(flags))"
        _ = sample
        accountCmd, _, err := root.Find([]string{"account"})
        if err == nil {
            accountCmd.AddCommand(newCheckpointSaveCmd(flags))
        }
        other.AddCommand(newGhostCmd(flags))
        mystery, _, err := other.Find([]string{"account"})
        if err == nil {
            mystery.AddCommand(newMysteryCmd(flags))
        }
        root.AddCommand(newWrappedCmd(flags))
    })
}
''',
            })
            file, use, _ = resolve_command_path(cli_dir, ["account", "save"])
            self.assertEqual(file.name, "save_cmd.go")
            self.assertEqual(use, "save")
            file, use, _ = resolve_command_path(cli_dir, ["profile", "save"])
            self.assertEqual(file.name, "profile.go")
            self.assertEqual(use, "save <name>")
            file, _, _ = resolve_command_path(cli_dir, ["wrapped"])
            self.assertEqual(file.name, "wrapped.go")
            ctors = collect_command_constructors(cli_dir)
            self.assertEqual(ctors["newAccountCmd"].children, ["newCheckpointSaveCmd"])
            for path in (["commented"], ["fake"], ["ghost"], ["mystery"], ["account", "commented"]):
                file, _, _ = resolve_command_path(cli_dir, path)
                self.assertIsNone(file, path)

            binary = f"{cli_dir.name}-pp-cli"
            (cli_dir / "SKILL.md").write_text(
                f"""# Fixture

```bash
{binary} account save
{binary} account save EXTRA
{binary} profile save alice
{binary} wrapped
```
""",
                encoding="utf-8",
            )
            report = run_checks(cli_dir, {"flag-commands", "positional-args", "unknown-command"})
            positional = [f for f in report.findings if f.check == "positional-args"]
            account_save = [f for f in positional if f.command == f"{binary} account save"]
            self.assertEqual(len(account_save), 1, [f.detail for f in positional])
            self.assertIn('Use: "save"', account_save[0].detail)
            self.assertIn("got 1 positional", account_save[0].detail)
            self.assertNotIn("<name>", account_save[0].detail)
            self.assertFalse(account_save[0].likely_false_positive)
            self.assertFalse(any(f.command == f"{binary} profile save" for f in positional))
            self.assertFalse(any(f.check == "unknown-command" for f in report.findings), [f.detail for f in report.findings])

    def test_variable_hook_funcs_are_registered(self):
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func newRootCmd(flags *rootFlags) *cobra.Command {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    rootCmd.AddCommand(newProfileCmd(flags))
    return rootCmd
}
''',
                "profile.go": '''package cli
import "github.com/spf13/cobra"
func newProfileCmd(flags *rootFlags) *cobra.Command {
    cmd := &cobra.Command{Use: "profile"}
    cmd.AddCommand(newProfileSaveCmd(flags))
    return cmd
}
func newProfileSaveCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "save <name>"}
}
''',
                "same_file_hook.go": '''package cli
import "github.com/spf13/cobra"
var syncHook = func(root *cobra.Command, flags *rootFlags) {
    addNovelCommandIfAbsent(root, newSyncCmd(flags))
}
func init() { registerNovelCommand(syncHook) }
func init() {
    localHook := func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newLocalCmd(flags))
    }
    registerNovelCommand(localHook)
}
var typedHook func(root *cobra.Command, flags *rootFlags) = func(root *cobra.Command, flags *rootFlags) {
    addNovelCommandIfAbsent(root, newTypedCmd(flags))
}
func init() { registerNovelCommand(typedHook) }
var assignedHook func(root *cobra.Command, flags *rootFlags)
func init() {
    assignedHook = func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newAssignedCmd(flags))
    }
    registerNovelCommand(assignedHook)
}
var nopeHook = func(flags *rootFlags) {
    addNovelCommandIfAbsent(flags, newNopeCmd(flags))
}
func init() { registerNovelCommand(nopeHook) }
// var commentedHook = func(root *cobra.Command, flags *rootFlags) {
//     addNovelCommandIfAbsent(root, newCommentedCmd(flags))
// }
// func init() { registerNovelCommand(commentedHook) }
const sample = "var stringHook = func(root *cobra.Command, flags *rootFlags) { addNovelCommandIfAbsent(root, newStringHookCmd(flags)) }"
func init() { registerNovelCommand(stringHook) }
''',
                "fixture_register.go": '''package cli
func init() { registerNovelCommand(blockHook) }
''',
                "block_wire.go": '''package cli
import "github.com/spf13/cobra"
var (
    blockHook = func(
        root *cobra.Command,
        flags *rootFlags,
    ) {
        addNovelCommandIfAbsent(root, newCheckpointCmd(flags))
    }
)
''',
                "zz_decoy.go": '''package cli
import "github.com/spf13/cobra"
func init() {
    syncHook := func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newDecoyCmd(flags))
    }
    _ = syncHook
}
''',
                "sync.go": '''package cli
import "github.com/spf13/cobra"
func newSyncCmd(flags *rootFlags) *cobra.Command { return &cobra.Command{Use: "sync"} }
''',
                "local.go": '''package cli
import "github.com/spf13/cobra"
func newLocalCmd(flags *rootFlags) *cobra.Command { return &cobra.Command{Use: "local"} }
''',
                "typed.go": '''package cli
import "github.com/spf13/cobra"
func newTypedCmd(flags *rootFlags) *cobra.Command { return &cobra.Command{Use: "typed"} }
''',
                "assigned.go": '''package cli
import "github.com/spf13/cobra"
func newAssignedCmd(flags *rootFlags) *cobra.Command { return &cobra.Command{Use: "assigned"} }
''',
                "checkpoint.go": '''package cli
import "github.com/spf13/cobra"
func newCheckpointCmd(flags *rootFlags) *cobra.Command {
    cmd := &cobra.Command{Use: "checkpoint"}
    cmd.AddCommand(newCheckpointSaveCmd(flags))
    return cmd
}
''',
                "save_cmd.go": '''package cli
import "github.com/spf13/cobra"
func newCheckpointSaveCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "save"}
}
''',
                "decoy.go": '''package cli
import "github.com/spf13/cobra"
func newDecoyCmd(flags *rootFlags) *cobra.Command { return &cobra.Command{Use: "decoy"} }
func newNopeCmd(flags *rootFlags) *cobra.Command { return &cobra.Command{Use: "nope"} }
func newCommentedCmd(flags *rootFlags) *cobra.Command { return &cobra.Command{Use: "commented"} }
func newStringHookCmd(flags *rootFlags) *cobra.Command { return &cobra.Command{Use: "string-hook"} }
''',
            })
            roots = set(find_root_children(cli_dir))
            for name in ("newSyncCmd", "newLocalCmd", "newTypedCmd", "newAssignedCmd", "newCheckpointCmd", "newProfileCmd"):
                self.assertIn(name, roots, name)
            for name in ("newDecoyCmd", "newNopeCmd", "newCommentedCmd", "newStringHookCmd"):
                self.assertNotIn(name, roots, name)
            file, use, _ = resolve_command_path(cli_dir, ["checkpoint", "save"])
            self.assertEqual(file.name, "save_cmd.go")
            self.assertEqual(use, "save")
            file, use, _ = resolve_command_path(cli_dir, ["profile", "save"])
            self.assertEqual(use, "save <name>")
            for path in (["decoy"], ["nope"], ["commented"], ["string-hook"]):
                file, _, _ = resolve_command_path(cli_dir, path)
                self.assertIsNone(file, path)

            binary = f"{cli_dir.name}-pp-cli"
            (cli_dir / "SKILL.md").write_text(
                f"""# Fixture

```bash
{binary} checkpoint save
{binary} checkpoint save EXTRA
{binary} profile save alice
{binary} sync
{binary} local
{binary} typed
{binary} assigned
```
""",
                encoding="utf-8",
            )
            report = run_checks(cli_dir, {"positional-args", "unknown-command"})
            positional = [f for f in report.findings if f.check == "positional-args"]
            checkpoint = [f for f in positional if f.command == f"{binary} checkpoint save"]
            self.assertEqual(len(checkpoint), 1, [f.detail for f in positional])
            self.assertIn('Use: "save"', checkpoint[0].detail)
            self.assertIn("got 1 positional", checkpoint[0].detail)
            self.assertNotIn("<name>", checkpoint[0].detail)
            self.assertFalse(any(f.check == "unknown-command" for f in report.findings), [f.detail for f in report.findings])

    def test_same_named_local_hooks_keep_their_own_edges(self):
        # Two init funcs can each register a local callback named hook.
        # The name map stores one body, so the second registration must
        # still be resolved from the init that contains it.
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func newRootCmd(flags *rootFlags) *cobra.Command {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    rootCmd.AddCommand(newProfileCmd(flags))
    return rootCmd
}
''',
                "profile.go": '''package cli
import "github.com/spf13/cobra"
func newProfileCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "profile"}
}
''',
                "hooks.go": '''package cli
import "github.com/spf13/cobra"
func init() {
    hook := func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newAlphaCmd(flags))
    }
    registerNovelCommand(hook)
}
func init() {
    hook := func(root *cobra.Command, flags *rootFlags) {
        if parent, _, err := root.Find(strings.Fields("profile")); err == nil {
            addNovelCommandIfAbsent(parent, newBetaCmd(flags))
        }
    }
    registerNovelCommand(hook)
}
func init() {
    hook := func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newGammaCmd(flags))
    }
    _ = hook
}
''',
                "alpha.go": '''package cli
import "github.com/spf13/cobra"
func newAlphaCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "alpha"}
}
''',
                "beta.go": '''package cli
import "github.com/spf13/cobra"
func newBetaCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "beta <id>"}
}
''',
                "gamma.go": '''package cli
import "github.com/spf13/cobra"
func newGammaCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "gamma"}
}
''',
            })
            roots = set(find_root_children(cli_dir))
            self.assertIn("newAlphaCmd", roots)
            self.assertIn("newProfileCmd", roots)
            self.assertNotIn("newBetaCmd", roots)
            self.assertNotIn("newGammaCmd", roots)
            file, use, _ = resolve_command_path(cli_dir, ["alpha"])
            self.assertEqual(file.name, "alpha.go")
            self.assertEqual(use, "alpha")
            file, use, _ = resolve_command_path(cli_dir, ["profile", "beta"])
            self.assertEqual(file.name, "beta.go")
            self.assertEqual(use, "beta <id>")
            file, _, _ = resolve_command_path(cli_dir, ["gamma"])
            self.assertIsNone(file)

            binary = f"{cli_dir.name}-pp-cli"
            (cli_dir / "SKILL.md").write_text(
                f"""# Fixture

```bash
{binary} alpha EXTRA
{binary} profile beta
```
""",
                encoding="utf-8",
            )
            report = run_checks(cli_dir, {"positional-args", "unknown-command"})
            positional = [f for f in report.findings if f.check == "positional-args"]
            alpha = [f for f in positional if f.command == f"{binary} alpha"]
            beta = [f for f in positional if f.command == f"{binary} profile beta"]
            self.assertEqual(len(alpha), 1, [f.detail for f in report.findings])
            self.assertIn('Use: "alpha"', alpha[0].detail)
            self.assertIn("got 1 positional", alpha[0].detail)
            self.assertEqual(len(beta), 1, [f.detail for f in report.findings])
            self.assertIn('Use: "beta <id>"', beta[0].detail)
            self.assertIn("got 0 positional", beta[0].detail)
            self.assertFalse(any(f.check == "unknown-command" for f in report.findings), [f.detail for f in report.findings])

    def test_local_hook_shadows_package_callback(self):
        # registerNovelCommand(hook) inside the local's block is the local
        # callback. A registration outside that block is the package func.
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func newRootCmd(flags *rootFlags) *cobra.Command {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    return rootCmd
}
''',
                "hooks.go": '''package cli
import "github.com/spf13/cobra"
func hook(root *cobra.Command, flags *rootFlags) {
    addNovelCommandIfAbsent(root, newPackageCmd(flags))
}
func init() {
    registerNovelCommand(hook)
}
func init() {
    hook := func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newShadowCmd(flags))
    }
    registerNovelCommand(hook)
}
''',
                "package_cmd.go": '''package cli
import "github.com/spf13/cobra"
func newPackageCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "package-cmd"}
}
''',
                "shadow.go": '''package cli
import "github.com/spf13/cobra"
func newShadowCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "shadow"}
}
''',
            })
            roots = set(find_root_children(cli_dir))
            self.assertIn("newPackageCmd", roots)
            self.assertIn("newShadowCmd", roots)
            file, use, _ = resolve_command_path(cli_dir, ["package-cmd"])
            self.assertEqual(file.name, "package_cmd.go")
            self.assertEqual(use, "package-cmd")
            file, use, _ = resolve_command_path(cli_dir, ["shadow"])
            self.assertEqual(file.name, "shadow.go")
            self.assertEqual(use, "shadow")

    def test_var_group_local_hook_shadows_package_callback(self):
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func newRootCmd(flags *rootFlags) *cobra.Command {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    return rootCmd
}
''',
                "hooks.go": '''package cli
import "github.com/spf13/cobra"
func hook(root *cobra.Command, flags *rootFlags) {
    addNovelCommandIfAbsent(root, newPackageCmd(flags))
}
func init() {
    var (
        hook = func(root *cobra.Command, flags *rootFlags) {
            addNovelCommandIfAbsent(root, newShadowCmd(flags))
        }
    )
    registerNovelCommand(hook)
}
func init() { registerNovelCommand(hook) }
''',
                "package_cmd.go": '''package cli
import "github.com/spf13/cobra"
func newPackageCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "package-cmd"}
}
''',
                "shadow.go": '''package cli
import "github.com/spf13/cobra"
func newShadowCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "shadow"}
}
''',
            })
            roots = set(find_root_children(cli_dir))
            self.assertIn("newPackageCmd", roots)
            self.assertIn("newShadowCmd", roots)

    def test_nested_local_hook_stays_in_its_block(self):
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func newRootCmd(flags *rootFlags) *cobra.Command {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    return rootCmd
}
''',
                "hooks.go": '''package cli
import "github.com/spf13/cobra"
func init() {
    hook := func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newOuterCmd(flags))
    }
    {
        hook := func(root *cobra.Command, flags *rootFlags) {
            addNovelCommandIfAbsent(root, newInnerCmd(flags))
        }
        registerNovelCommand(hook)
    }
    registerNovelCommand(hook)
}
''',
                "outer.go": '''package cli
import "github.com/spf13/cobra"
func newOuterCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "outer"}
}
''',
                "inner.go": '''package cli
import "github.com/spf13/cobra"
func newInnerCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "inner"}
}
''',
            })
            roots = set(find_root_children(cli_dir))
            self.assertIn("newOuterCmd", roots)
            self.assertIn("newInnerCmd", roots)

    def test_statement_initializer_callback_does_not_outlive_statement(self):
        # A short variable in an if, for, or switch header lives until that
        # statement ends, including an if's else. The call after the statement
        # registers the outer callback.
        head = '''package cli
import "github.com/spf13/cobra"

func hook(root *cobra.Command, flags *rootFlags) {
    addNovelCommandIfAbsent(root, newPackageCmd(flags))
}
'''
        root = '''package cli
import "github.com/spf13/cobra"
func newRootCmd(flags *rootFlags) *cobra.Command {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    return rootCmd
}
'''

        def check(label: str, extra: str, present: set[str], absent: set[str]) -> None:
            with tempfile.TemporaryDirectory() as td:
                cli_dir = _write_cli(Path(td), {
                    "root.go": root,
                    "hooks.go": head + extra,
                })
                got = set(find_root_children(cli_dir))
            missing = present - got
            unexpected = absent & got
            self.assertFalse(missing, f"{label}: missing {missing} from {got}")
            self.assertFalse(unexpected, f"{label}: unexpected {unexpected} in {got}")

        check(
            "if",
            '''
func init() {
    if hook := func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newLocalCmd(flags))
    }; []struct{ X int }{{X: 1}}[0].X == 1 && (struct{ Y int }{Y: 1}).Y == 1 && func() bool { return true }() {
        registerNovelCommand(hook)
    }
    registerNovelCommand(hook)
}
''',
            {"newPackageCmd", "newLocalCmd"},
            set(),
        )
        check(
            "else",
            '''
func init() {
    if hook := func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newElseCmd(flags))
    }; false {
    } /* keep */ else {
        registerNovelCommand(hook)
    }
    registerNovelCommand(hook)
}
''',
            {"newPackageCmd", "newElseCmd"},
            set(),
        )
        check(
            "else-if",
            '''
func init() {
    if hook := func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newOuterCmd(flags))
    }; false {
    } else if hook := func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newInnerCmd(flags))
    }; false {
    } else {
        registerNovelCommand(hook)
    }
    registerNovelCommand(hook)
}
''',
            {"newPackageCmd", "newInnerCmd"},
            {"newOuterCmd"},
        )
        check(
            "for",
            '''
func init() {
    for hook := func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newForCmd(flags))
    }; []int{1, 2}[0] == 1; hook = nil {
        registerNovelCommand(hook)
    }
    registerNovelCommand(hook)
}
''',
            {"newPackageCmd", "newForCmd"},
            set(),
        )
        check(
            "switch",
            '''
func init() {
    switch hook := func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newSwitchCmd(flags))
    }; map[string]int{"a": 1}["a"] {
    default:
        registerNovelCommand(hook)
    }
    registerNovelCommand(hook)
}
''',
            {"newPackageCmd", "newSwitchCmd"},
            set(),
        )
        check(
            "body",
            '''
func init() {
    if true {
        hook := func(root *cobra.Command, flags *rootFlags) {
            addNovelCommandIfAbsent(root, newBodyCmd(flags))
        }
    } else {
        registerNovelCommand(hook)
    }
}
''',
            {"newPackageCmd"},
            {"newBodyCmd"},
        )
        check(
            "nested",
            '''
func init() {
    for hook := func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newForCmd(flags))
    }; false; {
        if hook := func(root *cobra.Command, flags *rootFlags) {
            addNovelCommandIfAbsent(root, newIfCmd(flags))
        }; true {
            registerNovelCommand(hook)
        }
        registerNovelCommand(hook)
    }
    registerNovelCommand(hook)
}
''',
            {"newPackageCmd", "newForCmd", "newIfCmd"},
            set(),
        )
        check(
            "shadow",
            '''
func init() {
    hook := func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newOuterCmd(flags))
    }
    if hook := func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newIfCmd(flags))
    }; true {
        registerNovelCommand(hook)
    }
    registerNovelCommand(hook)
}
''',
            {"newOuterCmd", "newIfCmd"},
            {"newPackageCmd"},
        )

    def test_if_initializer_package_callback_keeps_its_command(self):
        # The call inside the if registers the local callback. The call after
        # it registers the package callback, so an example is checked against
        # that command's Use line.
        with tempfile.TemporaryDirectory() as td:
            cli_dir = _write_cli(Path(td), {
                "root.go": '''package cli
import "github.com/spf13/cobra"
func newRootCmd(flags *rootFlags) *cobra.Command {
    rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
    return rootCmd
}
''',
                "hooks.go": '''package cli
import "github.com/spf13/cobra"
func hook(root *cobra.Command, flags *rootFlags) {
    addNovelCommandIfAbsent(root, newPackageCmd(flags))
}
func init() {
    if hook := func(root *cobra.Command, flags *rootFlags) {
        addNovelCommandIfAbsent(root, newLocalCmd(flags))
    }; true {
        registerNovelCommand(hook)
    }
    registerNovelCommand(hook)
}
''',
                "package_cmd.go": '''package cli
import "github.com/spf13/cobra"
func newPackageCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "package-cmd"}
}
''',
                "local.go": '''package cli
import "github.com/spf13/cobra"
func newLocalCmd(flags *rootFlags) *cobra.Command {
    return &cobra.Command{Use: "local-cmd <id>"}
}
''',
            })
            roots = set(find_root_children(cli_dir))
            self.assertIn("newPackageCmd", roots)
            self.assertIn("newLocalCmd", roots)
            file, use, _ = resolve_command_path(cli_dir, ["package-cmd"])
            self.assertEqual(file.name, "package_cmd.go")
            self.assertEqual(use, "package-cmd")
            file, use, _ = resolve_command_path(cli_dir, ["local-cmd"])
            self.assertEqual(file.name, "local.go")
            self.assertEqual(use, "local-cmd <id>")

            binary = f"{cli_dir.name}-pp-cli"
            (cli_dir / "SKILL.md").write_text(
                f"""# Fixture

```bash
{binary} package-cmd EXTRA
{binary} local-cmd
```
""",
                encoding="utf-8",
            )
            report = run_checks(cli_dir, {"positional-args", "unknown-command"})
            positional = [f for f in report.findings if f.check == "positional-args"]
            package_hit = [f for f in positional if f.command == f"{binary} package-cmd"]
            local_hit = [f for f in positional if f.command == f"{binary} local-cmd"]
            self.assertEqual(len(package_hit), 1, [f.detail for f in report.findings])
            self.assertIn('Use: "package-cmd"', package_hit[0].detail)
            self.assertIn("got 1 positional", package_hit[0].detail)
            self.assertEqual(len(local_hit), 1, [f.detail for f in report.findings])
            self.assertIn('Use: "local-cmd <id>"', local_hit[0].detail)
            self.assertIn("got 0 positional", local_hit[0].detail)
            self.assertFalse(
                any(f.check == "unknown-command" for f in report.findings),
                [f.detail for f in report.findings],
            )


if __name__ == "__main__":
    unittest.main()
