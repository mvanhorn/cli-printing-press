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


if __name__ == "__main__":
    unittest.main()
