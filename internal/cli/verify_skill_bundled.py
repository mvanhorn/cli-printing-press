#!/usr/bin/env python3
"""verify_skill.py — validate that SKILL.md matches the shipped CLI source.

Four checks run in sequence:

  1. flag-names — every `--flag` used on a `<cli_binary> ...` invocation in
     SKILL.md is declared as a cobra flag somewhere in internal/cli/*.go.
     Flags on lines that invoke other tools (npx installers, gh, go,
     curl, etc.) are out of scope and ignored.
  2. flag-commands — every `--flag` used on a specific command is declared
     on that command (or as a persistent/root flag).
  3. positional-args — positional args in bash recipes match the command's
     `Use:` field signature (required + optional + variadic).
  4. unknown-command — every command path referenced in SKILL.md (in bash
     recipes and inline backticks under `## Command Reference`) maps to a
     real cobra `Use:` declaration in internal/cli/*.go. Catches docs that
     promise commands the binary does not implement (e.g. SKILL.md lists
     `qr get-qrcode` but the CLI only registers a leaf `qr` after promotion).

The checks are pattern-matching heuristics against Go AST-adjacent text.
False positives are possible for edge cases:
  - Shell command substitution (`$(...)`) inside a recipe can be
    misinterpreted as extending the outer command path.
  - Commands where the first positional arg is a valid subcommand name
    (e.g., `hubspot associations companies <id>` where `companies` is an
    object type passed as arg, not a subcommand).

Known false-positives are reported with a `[likely false positive]` tag.

USAGE

    python3 verify_skill.py --dir <cli-dir>
    python3 verify_skill.py --dir <cli-dir> --json
    python3 verify_skill.py --dir <cli-dir> --only flag-names
    python3 verify_skill.py --dir <cli-dir> --only unknown-command
    python3 verify_skill.py --dir <cli-dir> --strict  # treat known-FPs as failures

Exit codes:
    0 — all checks passed
    1 — one or more checks found issues (excluding known false positives
        unless --strict is set)
    2 — usage error (missing --dir, SKILL.md not found, etc.)

The CLI dir must contain both `SKILL.md` and `internal/cli/*.go`.
"""
from __future__ import annotations

import argparse
import json
import re
import shlex
import sys
from dataclasses import dataclass, field
from functools import lru_cache
from pathlib import Path
from typing import Iterable


COMMON_FLAGS = {
    "help", "version", "json", "csv", "plain", "quiet", "agent",
    "select", "compact", "dry-run", "no-cache", "yes", "no-input",
    "no-color", "human-friendly", "config", "base-url", "rate-limit",
    "timeout", "data-source", "stdin", "limit", "format", "output",
    "no-prompt", "days",
}

CODEBLOCK_BASH = re.compile(r"```bash\n(.*?)\n```", re.DOTALL)
COMMAND_REFERENCE_SECTION_RE = re.compile(
    r"^##\s+Command\s+Reference\s*$(.*?)(?=^##\s+|\Z)",
    re.DOTALL | re.MULTILINE | re.IGNORECASE,
)
# Cobra registers help/completion automatically; treat as always-present.
# Other CLIs may surface version as a real cobra command, but it is also a
# common --version flag pattern; we conservatively whitelist it too so a
# `<binary> version` reference never fires this check.
BUILTIN_COMMANDS = {"help", "completion", "version"}
USE_RE = re.compile(r'Use:\s*"([^"]+)"')
ARGS_RE = re.compile(
    r'Args:\s*cobra\.(ExactArgs|MinimumNArgs|MaximumNArgs|RangeArgs|NoArgs|OnlyValidArgs|ExactValidArgs)\s*\(([^)]*)\)'
)
FLAG_DECL_RE = re.compile(
    r'(Persistent)?Flags\(\)\.'
    r'(StringVar|BoolVar|IntVar|Int64Var|Float64Var|DurationVar|'
    r'StringSliceVar|StringArrayVar|UintVar|Uint64Var)P?\('
    r'&[^,]+,\s*"([a-z][a-z0-9-]*)"'
)
@dataclass
class Finding:
    check: str
    severity: str  # "error" or "warning"
    command: str
    detail: str
    evidence: str = ""
    likely_false_positive: bool = False


@dataclass
class Report:
    cli_dir: str
    skill_path: str
    findings: list[Finding] = field(default_factory=list)
    checks_run: list[str] = field(default_factory=list)
    recipes_checked: int = 0

    def has_real_failures(self) -> bool:
        return any(not f.likely_false_positive for f in self.findings)


# ---------------------------------------------------------------------------
# Source inspection
# ---------------------------------------------------------------------------


def parse_use(use_str: str) -> tuple[str, int, int, bool]:
    """Return (name, required_count, optional_count, has_variadic)."""
    tokens = use_str.split()
    if not tokens:
        return "", 0, 0, False
    name = tokens[0]
    required = optional = 0
    variadic = False
    for t in tokens[1:]:
        if t.startswith("<") and t.endswith(">"):
            required += 1
        elif t.startswith("[") and t.endswith("]"):
            if "..." in t:
                variadic = True
            else:
                optional += 1
        elif "..." in t:
            variadic = True
    return name, required, optional, variadic


# CONSTRUCTOR_RE matches `func newXxxCmd(...) *cobra.Command {`. The
# parameter list pattern `\([^()]*(?:\([^()]*\)[^()]*)*\)` handles one
# level of nested parens — the case that matters in practice is a
# function-typed parameter like `func newFooCmd(handler func() error)
# *cobra.Command`. Without the nested-paren handling the regex stops
# at the first `)` (the closer of `func()`) and the constructor is
# silently dropped from the constructor map. Two-level nesting (e.g.
# `func()` inside another `func()`) would still fail; flag for the
# legacy fallback when that pathological shape appears.
CONSTRUCTOR_RE = re.compile(
    r'^func\s+(new[A-Z]\w*Cmd)\s*'
    r'\([^()]*(?:\([^()]*\)[^()]*)*\)'
    r'\s*\*cobra\.Command\s*\{',
    re.MULTILINE,
)
ADDCMD_CHILD_RE = re.compile(r'\.AddCommand\s*\(\s*(new[A-Z]\w*Cmd)\s*\(')
ROOT_ADDCMD_RE = re.compile(r'rootCmd\.AddCommand\s*\(\s*(new[A-Z]\w*Cmd)\s*\(')


def _extract_function_body(text: str, start_offset: int) -> str | None:
    """Given the offset just after the opening `{` of a function body,
    return the body text (excluding the closing `}`). Tracks string and
    comment state so braces inside string literals or comments don't
    confuse the depth counter. Returns None if the body is unclosed.
    """
    depth = 1
    i = start_offset
    n = len(text)
    in_string: str | None = None  # holds the active string opener: '"', '`', or "'"
    in_line_comment = False
    in_block_comment = False
    while i < n and depth > 0:
        c = text[i]
        if in_line_comment:
            if c == '\n':
                in_line_comment = False
            i += 1
            continue
        if in_block_comment:
            if c == '*' and i + 1 < n and text[i + 1] == '/':
                in_block_comment = False
                i += 2
                continue
            i += 1
            continue
        if in_string is not None:
            if c == '\\' and in_string != '`' and i + 1 < n:
                # Skip the escaped char (still inside the string)
                i += 2
                continue
            if c == in_string:
                in_string = None
            i += 1
            continue
        if c == '/' and i + 1 < n:
            if text[i + 1] == '/':
                in_line_comment = True
                i += 2
                continue
            if text[i + 1] == '*':
                in_block_comment = True
                i += 2
                continue
        if c in ('"', '`', "'"):
            in_string = c
            i += 1
            continue
        if c == '{':
            depth += 1
        elif c == '}':
            depth -= 1
        i += 1
    if depth != 0:
        return None
    return text[start_offset:i - 1]


@dataclass
class CommandConstructor:
    name: str
    file: Path
    use: str
    args_info: tuple | None
    children: list[str] = field(default_factory=list)


@lru_cache(maxsize=None)
def collect_command_constructors(cli_dir: Path) -> dict[str, CommandConstructor]:
    """Scan internal/cli/*.go for `func newXxxCmd(...) *cobra.Command`
    declarations. For each, capture the Use string, the cobra.Args
    validator, and the list of child constructor names called via
    `<var>.AddCommand(newYyyCmd(...))` within the function body.

    Result maps constructor name → CommandConstructor.

    Cached per cli_dir for the lifetime of the process. verify-skill
    invokes find_command_source once per recipe in SKILL.md (typically
    5-15) and the source tree doesn't change mid-run, so the
    file-system scan only needs to happen once. Callers must not
    mutate the returned dict (it's the cache's storage).
    """
    src = cli_dir / "internal/cli"
    if not src.exists():
        return {}
    constructors: dict[str, CommandConstructor] = {}
    for go_file in src.glob("*.go"):
        if go_file.name.endswith("_test.go"):
            continue
        try:
            text = go_file.read_text()
        except Exception:
            continue
        for m in CONSTRUCTOR_RE.finditer(text):
            fn_name = m.group(1)
            body = _extract_function_body(text, m.end())
            if body is None:
                continue
            use_match = USE_RE.search(body)
            if not use_match:
                continue
            args_match = ARGS_RE.search(body)
            args_info = (args_match.group(1), args_match.group(2)) if args_match else None
            children = list(dict.fromkeys(
                child.group(1) for child in ADDCMD_CHILD_RE.finditer(body)
            ))
            constructors[fn_name] = CommandConstructor(
                name=fn_name,
                file=go_file,
                use=use_match.group(1),
                args_info=args_info,
                children=children,
            )
    return constructors


@lru_cache(maxsize=None)
def find_root_children(cli_dir: Path) -> list[str]:
    """Return the constructor names called from `rootCmd.AddCommand(...)`
    anywhere in internal/cli/*.go. The ordering follows source order, but
    the result is deduplicated.

    Cached per cli_dir; callers must not mutate the returned list.
    See collect_command_constructors for rationale."""
    src = cli_dir / "internal/cli"
    if not src.exists():
        return []
    seen: dict[str, None] = {}
    for go_file in src.glob("*.go"):
        if go_file.name.endswith("_test.go"):
            continue
        try:
            text = go_file.read_text()
        except Exception:
            continue
        for m in ROOT_ADDCMD_RE.finditer(text):
            seen.setdefault(m.group(1), None)
    return list(seen)


def resolve_command_path(
    cli_dir: Path,
    cmd_path: list[str],
    constructors: dict[str, CommandConstructor] | None = None,
    root_children: list[str] | None = None,
):
    """Walk the AddCommand graph to find the canonical declaring file for
    cmd_path. Returns (file, use_str, args_info) or (None, None, None) if
    the path can't be resolved (unknown command, unconventional CLI).

    This is the durable replacement for the old specificity-based
    disambiguation in find_command_source. It picks the file based on
    actual command-tree structure rather than guessing from `Use:` token
    counts. See retro #301 finding F1.
    """
    if not cmd_path:
        return None, None, None
    if constructors is None:
        constructors = collect_command_constructors(cli_dir)
    if root_children is None:
        root_children = find_root_children(cli_dir)
    if not constructors or not root_children:
        return None, None, None

    current = None
    for fn_name in root_children:
        info = constructors.get(fn_name)
        if info is None:
            continue
        leaf, _, _, _ = parse_use(info.use)
        if leaf == cmd_path[0]:
            current = info
            break
    if current is None:
        return None, None, None

    for token in cmd_path[1:]:
        next_info = None
        for child_fn in current.children:
            child = constructors.get(child_fn)
            if child is None:
                continue
            leaf, _, _, _ = parse_use(child.use)
            if leaf == token:
                next_info = child
                break
        if next_info is None:
            return None, None, None
        current = next_info

    return current.file, current.use, current.args_info


def find_command_source(cli_dir: Path, cmd_path: list[str]):
    """Locate the source file whose cobra.Command matches this path.

    Returns (go_files, use_str, args_info) where go_files is a list (kept
    list-shaped for backwards compatibility — most callers iterate it for
    `flag_declared_in` lookups).

    Resolution strategy:

      1. Walk the rootCmd.AddCommand graph (the durable approach added in
         retro #301 F1). When the CLI follows the standard `func newXxxCmd`
         + `<parent>.AddCommand(newXxxCmd(...))` convention, this returns
         exactly one file per command path with no false positives, even
         when two different commands share a leaf name (e.g.,
         `recipe-goat-pp-cli save` vs `recipe-goat-pp-cli profile save`).

      2. If the graph walk fails (unconventional CLI, missing rootCmd,
         constructor functions not named `newXxxCmd`), fall back to a
         legacy specificity heuristic that scans every Go file for any
         `Use:` whose first token matches the leaf. The legacy path is
         imperfect (it can pick the wrong file when leaves collide) but
         keeps the tool useful on CLIs that don't follow the standard
         convention.
    """
    if not cmd_path:
        return [], None, None

    file, use_str, args_info = resolve_command_path(cli_dir, cmd_path)
    if file is not None:
        return [file], use_str, args_info

    # Legacy fallback — kept to preserve behavior for unconventional CLIs.
    return _legacy_find_command_source(cli_dir, cmd_path)


def _legacy_find_command_source(cli_dir: Path, cmd_path: list[str]):
    """Pre-retro-#301 specificity-based heuristic. Retained as a fallback
    for CLIs whose command structure doesn't match the standard
    `rootCmd.AddCommand(newXxxCmd(...))` pattern resolve_command_path
    expects (e.g., commands constructed via local helpers, or files that
    declare cobra.Commands without going through a `newXxxCmd` factory)."""
    leaf = cmd_path[-1]
    src = cli_dir / "internal/cli"
    if not src.exists():
        return [], None, None

    candidates = []
    for go_file in src.glob("*.go"):
        if go_file.name.endswith("_test.go"):
            continue
        try:
            text = go_file.read_text()
        except Exception:
            continue
        for m in USE_RE.finditer(text):
            use_str = m.group(1)
            name, req, opt, var_ = parse_use(use_str)
            if name != leaf:
                continue
            end = m.end()
            window = text[end : end + 500]
            args_match = ARGS_RE.search(window)
            args_info = (args_match.group(1), args_match.group(2)) if args_match else None
            specificity = req + opt + (1 if var_ else 0)
            candidates.append((specificity, go_file, use_str, args_info))

    if not candidates:
        return [], None, None

    if len(cmd_path) >= 2:
        expected_basename = "_".join(cmd_path).replace("-", "_") + ".go"
        for spec, go_file, use_str, args_info in candidates:
            if go_file.name == expected_basename:
                return [go_file], use_str, args_info

    candidates.sort(key=lambda c: -c[0])
    top_spec = candidates[0][0]
    top_files = [c[1] for c in candidates if c[0] == top_spec]
    return top_files, candidates[0][2], candidates[0][3]


def flag_declared_in(files: Iterable[Path], flag_name: str) -> bool:
    for f in files:
        try:
            text = f.read_text()
        except Exception:
            continue
        for m in FLAG_DECL_RE.finditer(text):
            if m.group(3) == flag_name:
                return True
    return False


# Matches a function call whose first positional arg is `cmd` (or `cmd.Flags()`).
# Captures the function name so the verifier can look up its body.
HELPER_CALL_RE = re.compile(
    r"\b([a-zA-Z_]\w*)\s*\(\s*cmd(?:\b|\.Flags\(\))"
)

# Cobra/stdlib methods that take cmd as first arg but never declare flags.
_HELPER_CALL_IGNORE = frozenset({
    "AddCommand", "Run", "Execute", "Help", "Usage",
    "Print", "Printf", "Println",
})


def go_block_body(text: str, open_brace: int) -> str:
    """Return the body for the Go block whose `{` starts at open_brace.

    This is intentionally a small scanner rather than a Go parser. It handles
    nested braces and skips comments/strings so adjacent helper functions do
    not leak into the matched helper body.
    """
    if open_brace < 0 or open_brace >= len(text) or text[open_brace] != "{":
        return ""

    depth = 0
    i = open_brace
    state = "code"
    while i < len(text):
        ch = text[i]
        nxt = text[i + 1] if i + 1 < len(text) else ""

        if state == "line_comment":
            if ch == "\n":
                state = "code"
            i += 1
            continue
        if state == "block_comment":
            if ch == "*" and nxt == "/":
                state = "code"
                i += 2
            else:
                i += 1
            continue
        if state == "double_string":
            if ch == "\\":
                i += 2
                continue
            if ch == '"':
                state = "code"
            i += 1
            continue
        if state == "raw_string":
            if ch == "`":
                state = "code"
            i += 1
            continue
        if state == "rune":
            if ch == "\\":
                i += 2
                continue
            if ch == "'":
                state = "code"
            i += 1
            continue

        if ch == "/" and nxt == "/":
            state = "line_comment"
            i += 2
            continue
        if ch == "/" and nxt == "*":
            state = "block_comment"
            i += 2
            continue
        if ch == '"':
            state = "double_string"
            i += 1
            continue
        if ch == "`":
            state = "raw_string"
            i += 1
            continue
        if ch == "'":
            state = "rune"
            i += 1
            continue

        if ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0:
                return text[open_brace + 1:i]
        i += 1

    return ""


def flag_declared_via_helper(cli_dir: Path, cmd_files: Iterable[Path], flag_name: str) -> bool:
    """Return True if any helper called from cmd_files (with cmd as first arg)
    declares flag_name in its body. One level of indirection only — no recursive
    resolution."""
    helper_names: set[str] = set()
    for f in cmd_files:
        try:
            text = f.read_text()
        except Exception:
            continue
        for m in HELPER_CALL_RE.finditer(text):
            name = m.group(1)
            if name not in _HELPER_CALL_IGNORE:
                helper_names.add(name)
    if not helper_names:
        return False

    src = cli_dir / "internal/cli"
    if not src.exists():
        return False

    func_re = re.compile(
        r"func\s+("
        + "|".join(re.escape(n) for n in helper_names)
        + r")\s*\([^)]*\)\s*(?:\w+\s*)?\{"
    )
    for go_file in src.glob("*.go"):
        if go_file.name.endswith("_test.go"):
            continue
        try:
            text = go_file.read_text()
        except Exception:
            continue
        for m in func_re.finditer(text):
            body = go_block_body(text, m.end() - 1)
            for fm in FLAG_DECL_RE.finditer(body):
                if fm.group(3) == flag_name:
                    return True
    return False


def persistent_flag_declared(cli_dir: Path, flag_name: str) -> bool:
    src = cli_dir / "internal/cli"
    if not src.exists():
        return False
    for go_file in src.glob("*.go"):
        try:
            text = go_file.read_text()
        except Exception:
            continue
        for m in FLAG_DECL_RE.finditer(text):
            persistent, _, name = m.groups()
            if name == flag_name and persistent == "Persistent":
                return True
    return False


# ---------------------------------------------------------------------------
# SKILL.md extraction
# ---------------------------------------------------------------------------


def extract_recipes(skill: Path, cli_binary: str, cli_dir: Path | None = None) -> list[tuple[list[str], list[str], list[str]]]:
    """Return list of (cmd_path, positional_args, flags) tuples from bash blocks.

    cmd_path: leading lowercase-hyphenated tokens (up to 3)
    positional_args: non-flag tokens after cmd_path (shell-quoted strings preserved)
    flags: --flag tokens (with their -- prefix)
    """
    text = skill.read_text()
    blocks = CODEBLOCK_BASH.findall(text)
    results = []
    for block in blocks:
        # Merge line continuations
        merged = []
        buf = []
        for raw in block.splitlines():
            stripped = raw.rstrip()
            if stripped.endswith("\\"):
                buf.append(stripped[:-1].strip())
            else:
                buf.append(stripped)
                merged.append(" ".join(buf))
                buf = []
        if buf:
            merged.append(" ".join(buf))

        for line in merged:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            # Strip trailing comment
            cmt = line.find(" #")
            if cmt != -1:
                line = line[:cmt].strip()
            if not line.startswith(cli_binary + " "):
                continue
            # Strip shell command substitutions $(...) and backtick forms
            # FIRST — their contents are separate commands. Do this before
            # splitting on pipes so we don't mistakenly cut inside a $(...).
            line = re.sub(r"\$\([^)]*\)", "__SUBST__", line)
            line = re.sub(r"`[^`]*`", "__SUBST__", line)
            # Stop at outer shell operators so we don't parse pipes/redirects
            for op in [" | ", " && ", " || ", " > ", " >> ", " < "]:
                if op in line:
                    line = line.split(op)[0]
                    break
            after = line[len(cli_binary) + 1 :].strip()
            try:
                tokens = shlex.split(after, posix=True)
            except ValueError:
                tokens = after.split()
            if not tokens:
                continue
            cmd_path: list[str] = [tokens[0].lower()]
            i = 1
            while i < len(tokens):
                t = tokens[i]
                if t.startswith("-"):
                    break
                if (
                    t.startswith("<") or t.startswith("[")
                    or t.startswith('"') or t.startswith("'")
                    or t.startswith("$") or t.startswith("http")
                    or "/" in t or "=" in t
                    or re.match(r"^[A-Z]", t)
                    or re.match(r"^\d", t)
                ):
                    break
                if len(cmd_path) < 3 and re.match(r"^[a-z][a-z0-9-]*$", t):
                    # Verify adding this token still maps to a valid command.
                    # If the extended path has no source match (e.g. the
                    # parent command's Use documents <positional> and this
                    # token is just the arg), treat it as positional.
                    if cli_dir is not None:
                        trial = cmd_path + [t]
                        files, _, _ = find_command_source(cli_dir, trial)
                        if not files:
                            break
                    cmd_path.append(t)
                    i += 1
                    continue
                break
            positional: list[str] = []
            flags: list[str] = []
            while i < len(tokens):
                t = tokens[i]
                if t.startswith("--"):
                    flags.append(t)
                    # Skip value if present and not another flag
                    if i + 1 < len(tokens) and not tokens[i + 1].startswith("-"):
                        i += 2
                        continue
                elif t.startswith("-"):
                    # Short flag, skip its value heuristically
                    if i + 1 < len(tokens) and not tokens[i + 1].startswith("-"):
                        i += 2
                        continue
                else:
                    positional.append(t)
                i += 1
            results.append((cmd_path, positional, flags))
    return results


# ---------------------------------------------------------------------------
# Checks
# ---------------------------------------------------------------------------


def check_flag_names(cli_dir: Path, skill: Path, cli_binary: str, report: Report) -> None:
    # Scoped to recipes so flags belonging to other tools invoked from
    # SKILL.md (npx installers, gh, go, curl, ...) don't get reported as
    # missing declarations on the printed CLI. extract_recipes already
    # filters to lines starting with `cli_binary + " "`.
    all_files = list((cli_dir / "internal/cli").glob("*.go"))
    recipes = extract_recipes(skill, cli_binary, cli_dir)
    seen: set[str] = set()
    for cmd_path, _positional, flags in recipes:
        for raw_flag in flags:
            flag = raw_flag.lstrip("-")
            if flag in COMMON_FLAGS or flag in seen:
                continue
            if flag_declared_in(all_files, flag):
                continue
            seen.add(flag)
            path_str = " ".join(cmd_path)
            report.findings.append(
                Finding(
                    check="flag-names",
                    severity="error",
                    command=f"{cli_binary} {path_str}",
                    detail=f"--{flag} is referenced in SKILL.md but not declared in any internal/cli/*.go",
                )
            )


def check_flag_commands(cli_dir: Path, skill: Path, cli_binary: str, report: Report) -> None:
    all_files = list((cli_dir / "internal/cli").glob("*.go"))
    recipes = extract_recipes(skill, cli_binary, cli_dir)
    for cmd_path, _positional, flags in recipes:
        for raw_flag in flags:
            flag = raw_flag.lstrip("-")
            if flag in COMMON_FLAGS:
                continue
            cmd_files, _, _ = find_command_source(cli_dir, cmd_path)
            if cmd_files and flag_declared_in(cmd_files, flag):
                continue
            if persistent_flag_declared(cli_dir, flag):
                continue
            if cmd_files and flag_declared_via_helper(cli_dir, cmd_files, flag):
                continue
            path_str = " ".join(cmd_path)
            if flag_declared_in(all_files, flag):
                report.findings.append(
                    Finding(
                        check="flag-commands",
                        severity="error",
                        command=f"{cli_binary} {path_str}",
                        detail=f"--{flag} is declared elsewhere but not on {path_str}",
                    )
                )
            else:
                report.findings.append(
                    Finding(
                        check="flag-commands",
                        severity="error",
                        command=f"{cli_binary} {path_str}",
                        detail=f"--{flag} is not declared anywhere",
                    )
                )


def check_positional_args(cli_dir: Path, skill: Path, cli_binary: str, report: Report) -> None:
    recipes = extract_recipes(skill, cli_binary, cli_dir)
    report.recipes_checked = len(recipes)
    for cmd_path, positional, _flags in recipes:
        _files, use_str, args_info = find_command_source(cli_dir, cmd_path)
        if not use_str:
            continue  # command not found — not our job to flag here
        _, required, optional, variadic = parse_use(use_str)
        min_ok = required
        max_ok = float("inf") if variadic else required + optional
        if args_info:
            validator, arg = args_info
            try:
                n = int(arg) if arg else 0
            except ValueError:
                n = 0
            if validator == "ExactArgs":
                min_ok = max_ok = n
            elif validator == "MinimumNArgs":
                min_ok = n
                max_ok = float("inf")
            elif validator == "MaximumNArgs":
                min_ok = 0
                max_ok = n
            elif validator == "NoArgs":
                min_ok = max_ok = 0
        actual = len(positional)
        if min_ok <= actual <= max_ok:
            continue

        path_str = " ".join(cmd_path)
        # Classify common false-positive patterns.
        # FP-1: shell command-substitution residue inside an --arg value
        # (parser may have kept `$(dub-pp-cli links stale ...)` contents).
        # FP-2: parent command whose first positional arg happens to be a
        # valid cobra subcommand name (e.g., `associations companies`).
        fp = False
        if any(p.startswith("$") for p in positional):
            fp = True
        # For single-token cmd_path where positional[0] is lowercase+alpha,
        # the parser may have under-counted cmd_path. Accept hyphens AND
        # underscores so snake_case subcommands (e.g. category_page_query
        # from a GraphQL BFF expansion) classify as false positives.
        if len(cmd_path) == 1 and positional and re.match(r"^[a-z][a-z0-9_-]+$", positional[0]):
            fp = True

        max_display = "∞" if max_ok == float("inf") else int(max_ok)
        report.findings.append(
            Finding(
                check="positional-args",
                severity="error" if not fp else "warning",
                command=f"{cli_binary} {path_str}",
                detail=f'got {actual} positional args; Use: "{use_str}" expects {min_ok}–{max_display}',
                evidence=" ".join(positional) or "(none)",
                likely_false_positive=fp,
            )
        )


def _extract_inline_commands(skill_text: str, cli_binary: str) -> list[list[str]]:
    """Pull `<binary> <cmd> [more]` snippets from inline backticks under the
    `## Command Reference` section. Returns command paths only, no flags or
    positional args (those are surfaced through the bash-recipe checks).

    Why scoped to ## Command Reference: SKILL.md narrative prose mentions
    binary names in flowing text where false positives would be high. The
    Command Reference section is the canonical promise to the reader.
    """
    sec = COMMAND_REFERENCE_SECTION_RE.search(skill_text)
    if not sec:
        return []
    section_body = sec.group(1)
    binary_token = re.escape(cli_binary)
    inline_re = re.compile(rf"`({binary_token}(?:\s+[^`]+)?)`")
    paths: list[list[str]] = []
    for m in inline_re.finditer(section_body):
        snippet = m.group(1).strip()
        after = snippet[len(cli_binary):].strip()
        if not after:
            continue
        tokens = after.split()
        cmd_path: list[str] = []
        for t in tokens:
            if t.startswith("-") or t.startswith("<") or t.startswith("[") \
               or t.startswith("$") or t.startswith("\"") or t.startswith("'") \
               or t.startswith("`") or "/" in t or "=" in t:
                break
            if not re.match(r"^[a-z][a-z0-9-]*$", t):
                break
            cmd_path.append(t)
            if len(cmd_path) >= 3:
                break
        if cmd_path:
            paths.append(cmd_path)
    return paths


def check_unknown_commands(cli_dir: Path, skill: Path, cli_binary: str, report: Report) -> None:
    """Report command paths in SKILL.md that have no matching cobra Use:
    declaration in internal/cli/*.go. Source paths come from two surfaces:

      - Bash recipes (extract_recipes), which the other checks already walk
        but skip silently when the command is missing
      - Inline backtick references inside the `## Command Reference` section

    Each unique cmd_path is reported at most once per SKILL.md.

    Uses the in-repo find_command_source which walks the rootCmd.AddCommand
    graph and resolves multi-level command paths (e.g., `links stale` vs
    `profile save`) without false-positive collisions on shared leaf names.
    """
    skill_text = skill.read_text()
    seen: set[tuple[str, ...]] = set()
    sources: list[tuple[list[str], str]] = []

    for cmd_path, _pos, _flags in extract_recipes(skill, cli_binary, cli_dir):
        if cmd_path:
            sources.append((cmd_path, "bash recipe"))
    for cmd_path in _extract_inline_commands(skill_text, cli_binary):
        sources.append((cmd_path, "Command Reference inline"))

    for cmd_path, surface in sources:
        if not cmd_path:
            continue
        head = cmd_path[0]
        # Skip non-command tokens that the recipe parser may have promoted
        # into cmd_path[0]: flags, placeholders, env vars, etc. These belong
        # to other checks or are documentation conventions, not commands.
        if head in BUILTIN_COMMANDS:
            continue
        if head.startswith(("-", "<", "[", "$")) or "=" in head:
            continue
        if not re.match(r"^[a-z][a-z0-9-]*$", head):
            continue
        key = tuple(cmd_path)
        if key in seen:
            continue
        seen.add(key)
        files, _use, _args = find_command_source(cli_dir, cmd_path)
        if files:
            continue
        # Walk back to the longest existing prefix for a clearer error.
        detail = "command path not found in internal/cli/*.go (no matching Use: declaration)"
        for k in range(len(cmd_path) - 1, 0, -1):
            prefix_files, _, _ = find_command_source(cli_dir, cmd_path[:k])
            if prefix_files:
                detail = (
                    f"command path not found in internal/cli/*.go; "
                    f"closest existing prefix is `{cli_binary} {' '.join(cmd_path[:k])}`"
                )
                break
        report.findings.append(
            Finding(
                check="unknown-command",
                severity="error",
                command=f"{cli_binary} {' '.join(cmd_path)}",
                detail=detail,
                evidence=surface,
            )
        )


# ---------------------------------------------------------------------------
# Runner
# ---------------------------------------------------------------------------


def derive_cli_binary(cli_dir: Path) -> str:
    """Derive the CLI binary name from .printing-press.json, go.mod, or dir name."""
    manifest = cli_dir / ".printing-press.json"
    if manifest.exists():
        try:
            data = json.loads(manifest.read_text())
            if data.get("cli_name"):
                return data["cli_name"]
        except Exception:
            pass
    # Fallback — assume <dirname>-pp-cli
    return cli_dir.name + "-pp-cli"


def run_checks(cli_dir: Path, only: set[str] | None) -> Report:
    skill = cli_dir / "SKILL.md"
    if not skill.exists():
        print(f"error: no SKILL.md in {cli_dir}", file=sys.stderr)
        sys.exit(2)
    if not (cli_dir / "internal/cli").exists():
        print(f"error: no internal/cli/ in {cli_dir}", file=sys.stderr)
        sys.exit(2)

    cli_binary = derive_cli_binary(cli_dir)
    report = Report(cli_dir=str(cli_dir), skill_path=str(skill))

    checks = only or {"flag-names", "flag-commands", "positional-args", "unknown-command"}
    if "flag-names" in checks:
        report.checks_run.append("flag-names")
        check_flag_names(cli_dir, skill, cli_binary, report)
    if "flag-commands" in checks:
        report.checks_run.append("flag-commands")
        check_flag_commands(cli_dir, skill, cli_binary, report)
    if "positional-args" in checks:
        report.checks_run.append("positional-args")
        check_positional_args(cli_dir, skill, cli_binary, report)
    if "unknown-command" in checks:
        report.checks_run.append("unknown-command")
        check_unknown_commands(cli_dir, skill, cli_binary, report)
    return report


def format_human(report: Report) -> str:
    lines = [f"=== {Path(report.cli_dir).name} ==="]
    errors = [f for f in report.findings if not f.likely_false_positive]
    warnings = [f for f in report.findings if f.likely_false_positive]
    if not report.findings:
        lines.append(f"  ✓ All checks passed ({', '.join(report.checks_run)})")
        return "\n".join(lines)
    lines.append(f"  ✘ {len(errors)} error(s), {len(warnings)} likely false-positive(s)")
    for f in errors:
        lines.append(f"    [{f.check}] {f.command}: {f.detail}")
        if f.evidence:
            lines.append(f"      evidence: {f.evidence}")
    for f in warnings:
        lines.append(f"    [{f.check}] {f.command}: {f.detail}  [likely false positive]")
        if f.evidence:
            lines.append(f"      evidence: {f.evidence}")
    return "\n".join(lines)


def format_json(report: Report) -> str:
    out = {
        "cli_dir": report.cli_dir,
        "skill_path": report.skill_path,
        "checks_run": report.checks_run,
        "recipes_checked": report.recipes_checked,
        "findings": [
            {
                "check": f.check,
                "severity": f.severity,
                "command": f.command,
                "detail": f.detail,
                "evidence": f.evidence,
                "likely_false_positive": f.likely_false_positive,
            }
            for f in report.findings
        ],
    }
    return json.dumps(out, indent=2)


def main():
    p = argparse.ArgumentParser(
        description="Verify SKILL.md matches shipped CLI source."
    )
    p.add_argument("--dir", required=True, help="CLI directory (contains SKILL.md + internal/cli/)")
    p.add_argument(
        "--only",
        choices=["flag-names", "flag-commands", "positional-args", "unknown-command"],
        action="append",
        help="Run only the named check(s). Pass multiple times to include multiple.",
    )
    p.add_argument("--json", action="store_true", help="Emit JSON output")
    p.add_argument(
        "--strict",
        action="store_true",
        help="Exit non-zero even for findings classified as likely false positives.",
    )
    args = p.parse_args()
    only = set(args.only) if args.only else None
    report = run_checks(Path(args.dir).resolve(), only)

    if args.json:
        print(format_json(report))
    else:
        print(format_human(report))

    if args.strict:
        sys.exit(1 if report.findings else 0)
    sys.exit(1 if report.has_real_failures() else 0)


if __name__ == "__main__":
    main()
