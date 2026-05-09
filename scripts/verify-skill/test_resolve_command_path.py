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
    _extract_function_body,
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


if __name__ == "__main__":
    unittest.main()
