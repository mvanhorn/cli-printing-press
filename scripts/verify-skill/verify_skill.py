#!/usr/bin/env python3
"""verify_skill.py — validate that SKILL.md and README.md match the shipped CLI source.

Five checks run in sequence:

  1. flag-names — every `--flag` used on a `<cli_binary> ...` invocation in
     a prose source (SKILL.md, plus README.md when present) is declared as
     a cobra flag somewhere in internal/cli/*.go. Flags on lines that invoke
     other tools (npx installers, gh, go, curl, etc.) are out of scope and
     ignored.
  2. flag-commands — every `--flag` used on a specific command is declared
     on that command (or as a persistent/root flag).
  3. positional-args — positional args in bash recipes match the command's
     `Use:` field signature (required + optional + variadic).
  4. shell-var-quotes — every shell variable expanded inside a generated bash
     code block is wrapped in double quotes.
  5. unknown-command — every command path referenced in a prose source (in
     bash recipes from SKILL.md and README.md, plus inline backticks under
     SKILL.md's `## Command Reference`) maps to a real cobra `Use:`
     declaration in internal/cli/*.go. Catches docs that promise commands
     the binary does not implement (e.g. SKILL.md lists `qr get-qrcode` but
     the CLI only registers a leaf `qr` after promotion).

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
    python3 verify_skill.py --dir <cli-dir> --only shell-var-quotes
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


def read_utf8(path: Path) -> str:
    return path.read_text(encoding="utf-8")


# Cobra supplies these without explicit source-level flag declarations. Other
# generated/global flags must still be discovered in source so command-scoped
# copy-paste examples cannot hide missing flags behind this whitelist.
COMMON_FLAGS = {"help", "home", "version"}

CODEBLOCK_BASH = re.compile(r"^[ \t]*```bash[^\n]*\n(.*?)\n[ \t]*```[ \t]*$", re.DOTALL | re.MULTILINE)
FENCED_CODE = re.compile(r"```.*?```|~~~.*?~~~", re.DOTALL)
INLINE_CODE = re.compile(r"`[^`\n]*`")
# Trailing punctuation that prose glues onto a token: quotes/brackets that wrap
# a quoted command span (`'cli cmd --flag'`) and sentence punctuation. Cobra
# flag names are `[a-z0-9-]`, so trimming these can never truncate a real name.
TOKEN_TRAILING_PUNCT = "'\")].,;:"
SHELL_VAR_RE = re.compile(r"\$(?:\{[^}\n]+\}|[A-Za-z_][A-Za-z0-9_]*|[0-9]+|[@*#?])")
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
FLAG_METHOD_PATTERN = (
    r'StringVar|BoolVar|IntVar|Int32Var|Int64Var|Float32Var|Float64Var|DurationVar|'
    r'StringSliceVar|StringArrayVar|IntSliceVar|Int32SliceVar|Int64SliceVar|'
    r'Float64SliceVar|BoolSliceVar|DurationSliceVar|'
    r'UintVar|Uint32Var|Uint64Var|UintSliceVar|IPVar|IPSliceVar|Float32SliceVar|'
    r'StringToStringVar|StringToIntVar|StringToInt64Var'
)
FLAG_DECL_RE = re.compile(
    r'(Persistent)?Flags\(\)\.'
    r'(' + FLAG_METHOD_PATTERN + r')P?\('
    r'&[^,]+,\s*"([a-z][a-z0-9-]*)"'
)
FLAG_ALIAS_RE = re.compile(r'\b([A-Za-z_]\w*)\s*(?::=|=)\s*[A-Za-z_][\w.]*\.(Persistent)?Flags\(\)')
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
# Parent is one identifier (cmd, root, rootCmd, accountCmd). The candidate
# constructor is the second argument, same role as AddCommand's argument.
NOVEL_ATTACH_RE = re.compile(
    r'\baddNovelCommandIfAbsent\s*\(\s*([A-Za-z_]\w*)\s*,\s*(new[A-Z]\w*Cmd)\s*\('
)
# A selector (pkg.registerNovelCommand) is not the package-level hook.
HOOK_NAMED_RE = re.compile(r'(?<![\w.])registerNovelCommand\s*\(\s*([A-Za-z_]\w*)\s*\)')
NAMED_HOOK_FUNC_RE = re.compile(
    r'^func\s+([A-Za-z_]\w*)\s*\(\s*([A-Za-z_]\w*)\s+\*cobra\.Command\b[^)]*\)\s*\{',
    re.MULTILINE,
)
# `var name = func`, `name := func`, and `var name func(...) = func`.
# The optional type sits between the name and `=` so a typed variable still
# resolves to the literal that is actually registered.
VAR_HOOK_FUNC_RE = re.compile(
    r'(?:(?<!\w)var\s+)?'
    r'([A-Za-z_]\w*)'
    r'(?:\s+func\b[^={}]*)?'
    r'\s*(?::=|=)\s*func\s*\(\s*([A-Za-z_]\w*)\s+\*cobra\.Command\b[^)]*\)\s*\{'
)
ADDCMD_CALL_RE = re.compile(
    r'\b([A-Za-z_]\w*)\.AddCommand\s*\(\s*(new[A-Z]\w*Cmd)\s*\('
)
SCAFFOLD_ANN_RE = re.compile(
    r'"pp:novel-scaffold"\s*\]?\s*(?::|=)\s*"true"'
)
FIND_SLICE_RE = re.compile(
    r'\b([A-Za-z_]\w*)\s*,\s*_\s*,\s*(?:err|_)\s*(?::=|=)\s*'
    r'([A-Za-z_]\w*)\.Find\(\s*\[\]string\{([^}]*)\}'
)
FIND_FIELDS_RE = re.compile(
    r'\b([A-Za-z_]\w*)\s*,\s*_\s*,\s*(?:err|_)\s*(?::=|=)\s*'
    r'([A-Za-z_]\w*)\.Find\(\s*strings\.Fields\(\s*"([^"]*)"\s*\)'
)


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
    scaffold: bool = False


def _mask_go_comments(text: str) -> str:
    """Blank // and /* */ comments without changing offsets or newlines.

    Generated roots document registerNovelCommand inside a comment. A raw
    search would treat that example as a live hook. Strings are left intact
    so a later string mask, and Use:/Find literals, still see them.
    """
    if not text:
        return text
    chars = list(text)
    i = 0
    n = len(text)
    while i < n:
        c = text[i]
        nxt = text[i + 1] if i + 1 < n else ""
        if c == "/" and nxt == "/":
            j = i
            while j < n and text[j] != "\n":
                chars[j] = " "
                j += 1
            i = j
            continue
        if c == "/" and nxt == "*":
            end = text.find("*/", i + 2)
            stop = n if end < 0 else end + 2
            for j in range(i, stop):
                if chars[j] != "\n":
                    chars[j] = " "
            i = stop
            continue
        if c == '"':
            i += 1
            while i < n:
                if text[i] == "\\":
                    i += 2
                    continue
                if text[i] == '"':
                    i += 1
                    break
                if text[i] == "\n":
                    break
                i += 1
            continue
        if c == "`":
            end = text.find("`", i + 1)
            i = n if end < 0 else end + 1
            continue
        if c == "'":
            i += 1
            if i < n and text[i] == "\\":
                i += 2
            elif i < n:
                i += 1
            if i < n and text[i] == "'":
                i += 1
            continue
        i += 1
    return "".join(chars)


def _mask_go_strings(text: str) -> str:
    """Blank string and rune contents without changing offsets or newlines."""
    if not text:
        return text
    chars = list(text)
    i = 0
    n = len(text)
    while i < n:
        c = text[i]
        if c == '"':
            j = i + 1
            while j < n:
                if text[j] == "\\":
                    j += 2
                    continue
                if text[j] == '"':
                    for k in range(i + 1, j):
                        if chars[k] != "\n":
                            chars[k] = " "
                    i = j + 1
                    break
                if text[j] == "\n":
                    i = j
                    break
                j += 1
            else:
                i = n
            continue
        if c == "`":
            end = text.find("`", i + 1)
            if end < 0:
                for k in range(i + 1, n):
                    if chars[k] != "\n":
                        chars[k] = " "
                i = n
                continue
            for k in range(i + 1, end):
                if chars[k] != "\n":
                    chars[k] = " "
            i = end + 1
            continue
        if c == "'":
            j = i + 1
            if j < n and text[j] == "\\":
                j += 2
            elif j < n:
                j += 1
            if j < n and text[j] == "'":
                for k in range(i + 1, j):
                    if chars[k] != "\n":
                        chars[k] = " "
                i = j + 1
            else:
                i += 1
            continue
        i += 1
    return "".join(chars)


def _read_balanced(text: str, open_index: int) -> tuple[str | None, int]:
    """text[open_index] is '('. Return (inner, index_after_close)."""
    if open_index >= len(text) or text[open_index] != "(":
        return None, open_index
    depth = 0
    i = open_index
    n = len(text)
    in_string: str | None = None
    while i < n:
        c = text[i]
        if in_string is not None:
            if c == "\\" and in_string != "`" and i + 1 < n:
                i += 2
                continue
            if c == in_string:
                in_string = None
            i += 1
            continue
        if c in ('"', "`", "'"):
            in_string = c
            i += 1
            continue
        if c == "(":
            depth += 1
        elif c == ")":
            depth -= 1
            if depth == 0:
                return text[open_index + 1:i], i + 1
        i += 1
    return None, n


def _first_param_if_cobra_command(params: str) -> str | None:
    """Return the first parameter name when its type is *cobra.Command."""
    depth = 0
    buf: list[str] = []
    for c in params:
        if c == "(":
            depth += 1
        elif c == ")":
            depth = max(0, depth - 1)
        elif c == "," and depth == 0:
            break
        else:
            buf.append(c)
    field = "".join(buf).strip()
    match = re.match(r"([A-Za-z_]\w*)\b", field)
    if not match or "*cobra.Command" not in field:
        return None
    return match.group(1)


def _append_unique(children: list[str], ctor: str) -> None:
    if ctor not in children:
        children.append(ctor)


def _find_bindings(body: str, root_names: set[str]) -> dict[str, list[str]]:
    """Map a variable assigned from root.Find to its command path segments."""
    bindings: dict[str, list[str]] = {}

    def take(var: str, recv: str, parts: list[str]) -> None:
        if recv not in root_names or not var or var == "_":
            return
        cleaned = [part for part in parts if part]
        if cleaned:
            bindings[var] = cleaned

    for match in FIND_SLICE_RE.finditer(body):
        take(match.group(1), match.group(2), re.findall(r'"([^"]*)"', match.group(3)))
    for match in FIND_FIELDS_RE.finditer(body):
        take(match.group(1), match.group(2), match.group(3).split())
    return bindings


def _funclit_hooks(string_masked: str, comment_masked: str) -> list[tuple[str, str]]:
    """Return (root param, body) for registerNovelCommand(func(...) { ... }) hooks.

    The header is scanned on string-masked text so an example inside a string
    literal is not a hook. The body is taken from comment-masked text so
    Find path literals survive.
    """
    hooks: list[tuple[str, str]] = []
    for match in re.finditer(r"(?<![\w.])registerNovelCommand\s*\(\s*func\b", string_masked):
        i = match.end()
        while i < len(string_masked) and string_masked[i].isspace():
            i += 1
        if i >= len(string_masked) or string_masked[i] != "(":
            continue
        params, after = _read_balanced(string_masked, i)
        if params is None:
            continue
        i = after
        while i < len(string_masked) and string_masked[i].isspace():
            i += 1
        if i >= len(string_masked) or string_masked[i] != "{":
            continue
        root_param = _first_param_if_cobra_command(params)
        if not root_param:
            continue
        body = _extract_function_body(comment_masked, i + 1)
        if body is None:
            continue
        hooks.append((root_param, body))
    return hooks


def _code_brace_depths(text: str) -> list[int]:
    depths = [0] * len(text)
    depth = 0
    for i, char in enumerate(text):
        depths[i] = depth
        if char == "{":
            depth += 1
        elif char == "}":
            depth = max(0, depth - 1)
    return depths


def _keep_hook(
    found: dict[str, tuple[str, str, int]],
    name: str,
    root_param: str,
    body: str,
    depth: int,
) -> None:
    """Keep a package-level callback when a local reuses its name.

    Other files can pass only a package-level function or variable to
    registerNovelCommand. A later local with the same name must not hide it.
    Call sites still prefer a local declared in the registering block; this
    map is the fallback for every other registration of that name.
    """
    prev = found.get(name)
    if prev is None or (prev[2] > 0 and depth == 0):
        found[name] = (root_param, body, depth)


@dataclass(frozen=True)
class _LocalHook:
    name: str
    root_param: str
    body: str
    decl_end: int
    scope_end: int
    depth: int


def _enclosing_block_end(text: str, depths: list[int], pos: int) -> int:
    """Index of the brace that closes the block containing pos."""
    if pos < 0 or pos >= len(text) or not depths or depths[pos] == 0:
        return len(text)
    depth = depths[pos]
    for i in range(pos, len(text)):
        if text[i] == "}" and depths[i] == depth:
            return i
    return len(text)


_K_NAME = "name"
_K_ARRAY = "array"
_K_MAP = "map"
_K_STRUCT = "struct"
_K_VALUE = "value"
_CONTROL_KEYWORDS = ("if", "for", "switch")
_BINOPS = ("||", "&&", "==", "!=", "<=", ">=", "&^", "<<", ">>")


def _ident_char(char: str) -> bool:
    return char.isalnum() or char == "_"


def _keyword_at(text: str, i: int, word: str) -> bool:
    end = i + len(word)
    if i < 0 or end > len(text) or not text.startswith(word, i):
        return False
    if i > 0 and _ident_char(text[i - 1]):
        return False
    return end == len(text) or not _ident_char(text[end])


def _skip_ws(text: str, i: int) -> int:
    n = len(text)
    while i < n and text[i].isspace():
        i += 1
    return i


def _read_ident(text: str, i: int) -> tuple[str, int]:
    if i >= len(text) or not (text[i].isalpha() or text[i] == "_"):
        return "", i
    j = i + 1
    while j < len(text) and _ident_char(text[j]):
        j += 1
    return text[i:j], j


def _skip_go_string(text: str, i: int) -> int:
    """i points at a quote. Return the index after that literal."""
    if i >= len(text):
        return i
    quote = text[i]
    n = len(text)
    if quote == "`":
        end = text.find("`", i + 1)
        return n if end < 0 else end + 1
    if quote not in "\"'":
        return i
    i += 1
    if quote == "'":
        if i < n and text[i] == "\\":
            i += 2
        elif i < n:
            i += 1
        if i < n and text[i] == "'":
            return i + 1
        return i
    while i < n:
        if text[i] == "\\":
            i += 2
            continue
        if text[i] == quote:
            return i + 1
        if text[i] == "\n":
            return i
        i += 1
    return n


def _skip_number(text: str, i: int) -> int:
    n = len(text)
    if text.startswith(("0x", "0X", "0b", "0B", "0o", "0O"), i):
        i += 2
        while i < n and (text[i].isalnum() or text[i] == "_"):
            i += 1
        return i
    while i < n and (text[i].isdigit() or text[i] == "_"):
        i += 1
    if i < n and text[i] == "." and i + 1 < n and text[i + 1].isdigit():
        i += 2
        while i < n and (text[i].isdigit() or text[i] == "_"):
            i += 1
    if i < n and text[i] in "eE":
        j = i + 1
        if j < n and text[j] in "+-":
            j += 1
        if j < n and text[j].isdigit():
            i = j + 1
            while i < n and (text[i].isdigit() or text[i] == "_"):
                i += 1
    if i < n and text[i] == "i":
        i += 1
    return i


def _matching_close_brace(text: str, depths: list[int], open_brace: int) -> int | None:
    """Index of the '}' that closes the '{' at open_brace."""
    if open_brace < 0 or open_brace >= len(text) or text[open_brace] != "{":
        return None
    target = depths[open_brace] + 1
    for i in range(open_brace + 1, len(text)):
        if text[i] == "}" and depths[i] == target:
            return i
    return None


def _skip_matched_brace(text: str, depths: list[int], i: int) -> int | None:
    close = _matching_close_brace(text, depths, i)
    if close is None:
        return None
    return close + 1


def _skip_bracket_group(text: str, i: int) -> int | None:
    """i points at '['. Return the index after the matching ']'."""
    if i >= len(text) or text[i] != "[":
        return None
    bracket = 0
    n = len(text)
    while i < n:
        char = text[i]
        if char in "\"'`":
            i = _skip_go_string(text, i)
            continue
        if char == "[":
            bracket += 1
        elif char == "]":
            bracket -= 1
            if bracket == 0:
                return i + 1
        i += 1
    return None


def _skip_paren_group(text: str, i: int) -> int | None:
    """i points at '('. Return the index after the matching ')'."""
    if i >= len(text) or text[i] != "(":
        return None
    paren = 0
    n = len(text)
    while i < n:
        char = text[i]
        if char in "\"'`":
            i = _skip_go_string(text, i)
            continue
        if char == "(":
            paren += 1
        elif char == ")":
            paren -= 1
            if paren == 0:
                return i + 1
        i += 1
    return None


def _skip_type(text: str, depths: list[int], i: int) -> int:
    """Skip a type. Does not consume a following composite literal."""
    n = len(text)
    while True:
        i = _skip_ws(text, i)
        if i >= n:
            return i
        if text.startswith("<-", i):
            i += 2
            continue
        if _keyword_at(text, i, "chan"):
            i += 4
            i = _skip_ws(text, i)
            if text.startswith("<-", i):
                i += 2
            continue
        if text[i] == "*":
            i += 1
            continue
        break
    i = _skip_ws(text, i)
    if i >= n:
        return i
    if _keyword_at(text, i, "func"):
        return _skip_func_type(text, depths, i)
    if _keyword_at(text, i, "struct") or _keyword_at(text, i, "interface"):
        word = "struct" if _keyword_at(text, i, "struct") else "interface"
        j = _skip_ws(text, i + len(word))
        if j < n and text[j] == "{":
            end = _skip_matched_brace(text, depths, j)
            return j if end is None else end
        return j
    if _keyword_at(text, i, "map"):
        return _skip_map_type(text, depths, i)
    if text[i] == "[":
        return _skip_array_type(text, depths, i)
    if text[i] == "(":
        end = _skip_paren_group(text, i)
        return i + 1 if end is None else end
    if text[i].isalpha() or text[i] == "_":
        _, i = _read_ident(text, i)
        while True:
            j = _skip_ws(text, i)
            if j < n and text[j] == ".":
                j = _skip_ws(text, j + 1)
                if j < n and (text[j].isalpha() or text[j] == "_"):
                    _, j = _read_ident(text, j)
                    i = j
                    continue
                return j
            if j < n and text[j] == "[":
                end = _skip_bracket_group(text, j)
                if end is None:
                    return j
                i = end
                continue
            return j
    return i


def _skip_array_type(text: str, depths: list[int], i: int) -> int:
    end = _skip_bracket_group(text, i)
    if end is None:
        return i + 1
    return _skip_type(text, depths, end)


def _skip_map_type(text: str, depths: list[int], i: int) -> int:
    j = _skip_ws(text, i + 3)
    if j >= len(text) or text[j] != "[":
        return j
    end = _skip_bracket_group(text, j)
    if end is None:
        return j
    return _skip_type(text, depths, end)


def _skip_func_type(text: str, depths: list[int], i: int) -> int:
    """Skip a function type starting at func, without a body."""
    j = _skip_ws(text, i + 4)
    if j >= len(text) or text[j] != "(":
        return j
    j = _skip_paren_group(text, j)
    if j is None:
        return i + 4
    j = _skip_ws(text, j)
    if j < len(text) and text[j] == "(":
        end = _skip_paren_group(text, j)
        return j if end is None else end
    if j < len(text) and text[j] != "{":
        return _skip_type(text, depths, j)
    return j


def _header_keyword(text: str, depths: list[int], pos: int) -> tuple[int, str] | None:
    """if/for/switch whose header contains pos.

    A '{' at the same brace depth means pos is in that block, not in a
    statement header before the block.
    """
    if pos <= 0 or pos >= len(depths) or depths[pos] == 0:
        return None
    depth = depths[pos]
    paren = 0
    bracket = 0
    i = pos
    while i > 0:
        i -= 1
        if depths[i] < depth:
            return None
        if depths[i] > depth:
            continue
        char = text[i]
        if char == ")":
            paren += 1
            continue
        if char == "(":
            if paren == 0:
                return None
            paren -= 1
            continue
        if char == "]":
            bracket += 1
            continue
        if char == "[":
            if bracket == 0:
                return None
            bracket -= 1
            continue
        if paren or bracket:
            continue
        if char == "{":
            return None
        for word in _CONTROL_KEYWORDS:
            if _keyword_at(text, i, word):
                return i, word
    return None


def _opens_composite(kind: str, expr_lev: int) -> bool:
    """Whether '{' after this primary is a composite literal.

    In a control clause, a name, selector, or index is the statement
    block. Array, slice, map, and struct types still take a literal.
    Nested expressions (calls, parens, literal elements) accept a name too.
    """
    if kind in (_K_ARRAY, _K_MAP, _K_STRUCT):
        return True
    return kind == _K_NAME and expr_lev > 0


def _bin_op_len(text: str, i: int) -> int:
    for op in _BINOPS:
        if text.startswith(op, i):
            return len(op)
    if text.startswith("<-", i):
        return 0
    if i < len(text) and text[i] in "+-|^*/%&<>":
        return 1
    return 0


def _skip_literal_value(
    text: str,
    depths: list[int],
    i: int,
    depth: int,
    expr_lev: int,
) -> int:
    """i points at '{'. Return the index after the composite literal."""
    open_depth = depths[i] if i < len(depths) else depth
    i += 1
    n = len(text)
    while i < n:
        i = _skip_ws(text, i)
        if i >= n:
            return i
        if text[i] in "\"'`":
            i = _skip_go_string(text, i)
            continue
        if text[i] == "}" and i < len(depths) and depths[i] == open_depth + 1:
            return i + 1
        if text[i] == ",":
            i += 1
            continue
        if text[i] == "{":
            i = _skip_literal_value(text, depths, i, depth, expr_lev + 1)
            continue
        nxt, _ = _skip_expr(text, depths, i, depth, expr_lev + 1)
        if nxt <= i:
            i += 1
            continue
        i = nxt
        j = _skip_ws(text, i)
        if j < n and text[j] == ":":
            i = j + 1
    return i


def _skip_operand(
    text: str,
    depths: list[int],
    i: int,
    depth: int,
    expr_lev: int,
) -> tuple[int, str]:
    n = len(text)
    i = _skip_ws(text, i)
    if i >= n:
        return i, _K_VALUE
    char = text[i]
    if char in "\"'`":
        return _skip_go_string(text, i), _K_VALUE
    if char.isdigit():
        return _skip_number(text, i), _K_VALUE
    if char == "{":
        if expr_lev > 0:
            return _skip_literal_value(text, depths, i, depth, expr_lev), _K_VALUE
        return i, _K_VALUE
    if char == "(":
        return _skip_paren_expr(text, depths, i, depth, expr_lev)
    if char == "[":
        return _skip_array_type(text, depths, i), _K_ARRAY
    if char.isalpha() or char == "_":
        word, j = _read_ident(text, i)
        if word == "func":
            return _skip_func_expr(text, depths, i)
        if word == "struct":
            k = _skip_ws(text, j)
            if k < n and text[k] == "{":
                end = _skip_matched_brace(text, depths, k)
                k = k if end is None else end
            return k, _K_STRUCT
        if word == "interface":
            k = _skip_ws(text, j)
            if k < n and text[k] == "{":
                end = _skip_matched_brace(text, depths, k)
                k = k if end is None else end
            return k, _K_VALUE
        if word == "map":
            return _skip_map_type(text, depths, i), _K_MAP
        if word == "chan":
            return _skip_type(text, depths, i), _K_VALUE
        return j, _K_NAME
    return i, _K_VALUE


def _skip_func_expr(
    text: str,
    depths: list[int],
    i: int,
) -> tuple[int, str]:
    j = _skip_func_type(text, depths, i)
    j = _skip_ws(text, j)
    if j < len(text) and text[j] == "{":
        end = _skip_matched_brace(text, depths, j)
        if end is None:
            return j, _K_VALUE
        return end, _K_VALUE
    return j, _K_VALUE


def _skip_paren_expr(
    text: str,
    depths: list[int],
    i: int,
    depth: int,
    expr_lev: int,
) -> tuple[int, str]:
    """Parentheses keep the inner type kind so (struct{...}){...} still composites."""
    j, kind = _skip_expr(text, depths, i + 1, depth, expr_lev + 1)
    j = _skip_ws(text, j)
    if j < len(text) and text[j] == ")":
        j += 1
    return j, kind


def _skip_primary(
    text: str,
    depths: list[int],
    i: int,
    depth: int,
    expr_lev: int,
) -> tuple[int, str]:
    i, kind = _skip_operand(text, depths, i, depth, expr_lev)
    n = len(text)
    while True:
        j = _skip_ws(text, i)
        if j >= n:
            return j, kind
        char = text[j]
        if char == ".":
            if text.startswith("...", j):
                return j, kind
            k = j + 1
            k = _skip_ws(text, k)
            if k < n and text[k] == "(":
                end = _skip_paren_group(text, k)
                if end is None:
                    return k, _K_VALUE
                i = end
                kind = _K_VALUE
                continue
            if k < n and (text[k].isalpha() or text[k] == "_"):
                _, i = _read_ident(text, k)
                kind = _K_NAME
                continue
            return j, kind
        if char == "[":
            i, sliced = _skip_bracket_suffix(text, depths, j, depth, expr_lev)
            kind = _K_VALUE if sliced else _K_NAME
            continue
        if char == "(":
            i = _skip_call_args(text, depths, j, depth, expr_lev)
            kind = _K_VALUE
            continue
        if char == "{":
            if _opens_composite(kind, expr_lev):
                i = _skip_literal_value(text, depths, j, depth, expr_lev)
                kind = _K_VALUE
                continue
            return j, kind
        return j, kind


def _skip_bracket_suffix(
    text: str,
    depths: list[int],
    i: int,
    depth: int,
    expr_lev: int,
) -> tuple[int, bool]:
    """i points at '[' of an index, slice, or instantiation. Return (after, is_slice)."""
    i += 1
    n = len(text)
    sliced = False
    while i < n and text[i] != "]":
        i = _skip_ws(text, i)
        if i >= n:
            return i, sliced
        if text[i] == "]":
            break
        if text[i] in "\"'`":
            i = _skip_go_string(text, i)
            continue
        if text[i] == ":":
            sliced = True
            i += 1
            continue
        if text[i] == ",":
            i += 1
            continue
        nxt, _ = _skip_expr(text, depths, i, depth, expr_lev + 1)
        if nxt <= i:
            i += 1
            continue
        i = nxt
    if i < n and text[i] == "]":
        i += 1
    return i, sliced


def _skip_call_args(
    text: str,
    depths: list[int],
    i: int,
    depth: int,
    expr_lev: int,
) -> int:
    i += 1
    n = len(text)
    while i < n and text[i] != ")":
        i = _skip_ws(text, i)
        if i >= n or text[i] == ")":
            break
        if text[i] == ",":
            i += 1
            continue
        if text.startswith("...", i):
            i += 3
            continue
        nxt, _ = _skip_expr(text, depths, i, depth, expr_lev + 1)
        if nxt <= i:
            i += 1
            continue
        i = nxt
    if i < n and text[i] == ")":
        i += 1
    return i


def _skip_unary(
    text: str,
    depths: list[int],
    i: int,
    depth: int,
    expr_lev: int,
) -> tuple[int, str]:
    n = len(text)
    prefixed = False
    while True:
        i = _skip_ws(text, i)
        if i >= n:
            return i, _K_VALUE
        if text.startswith("<-", i):
            i += 2
            prefixed = True
            continue
        if text[i] in "+-!^&~*":
            i += 1
            prefixed = True
            continue
        break
    i, kind = _skip_primary(text, depths, i, depth, expr_lev)
    if prefixed:
        kind = _K_VALUE
    return i, kind


def _skip_expr(
    text: str,
    depths: list[int],
    i: int,
    depth: int,
    expr_lev: int,
) -> tuple[int, str]:
    i, kind = _skip_unary(text, depths, i, depth, expr_lev)
    while True:
        j = _skip_ws(text, i)
        op = _bin_op_len(text, j)
        if op == 0:
            return j, kind
        i, _ = _skip_unary(text, depths, j + op, depth, expr_lev)
        kind = _K_VALUE


def _control_body_open(text: str, depths: list[int], i: int, depth: int) -> int | None:
    """Index of the '{' that opens an if/for/switch body, scanning from i."""
    n = len(text)
    while i < n:
        i = _skip_ws(text, i)
        if i >= n:
            return None
        if i < len(depths) and depths[i] < depth:
            return None
        char = text[i]
        if char == "{" and (i >= len(depths) or depths[i] == depth):
            return i
        if char == ";":
            i += 1
            continue
        if text.startswith((":=", "++", "--", "<-"), i):
            i += 2
            continue
        if char == "=":
            i += 1
            continue
        nxt, _ = _skip_expr(text, depths, i, depth, 0)
        if nxt <= i:
            i += 1
            continue
        i = nxt
    return None


def _extend_through_else(text: str, depths: list[int], close_brace: int, depth: int) -> int:
    """Include an if's else or else-if chain. close_brace is the then-block '}'."""
    i = _skip_ws(text, close_brace + 1)
    if not _keyword_at(text, i, "else"):
        return close_brace
    i = _skip_ws(text, i + 4)
    if _keyword_at(text, i, "if"):
        nested = _control_statement_end(text, depths, i + 2, "if", depth)
        return close_brace if nested is None else nested
    if i < len(text) and text[i] == "{":
        close = _matching_close_brace(text, depths, i)
        return close_brace if close is None else close
    return close_brace


def _control_statement_end(
    text: str,
    depths: list[int],
    scan_from: int,
    keyword: str,
    depth: int,
) -> int | None:
    body = _control_body_open(text, depths, scan_from, depth)
    if body is None:
        return None
    end = _matching_close_brace(text, depths, body)
    if end is None:
        return None
    if keyword == "if":
        return _extend_through_else(text, depths, end, depth)
    return end


def _statement_header_end(
    text: str,
    depths: list[int],
    pos: int,
    decl_end: int,
) -> int | None:
    """End of the if/for/switch whose header declares pos, else None.

    Scan starts after this declaration's func literal so that literal's
    braces are not the statement body.
    """
    found = _header_keyword(text, depths, pos)
    if found is None or pos >= len(depths):
        return None
    _, keyword = found
    return _control_statement_end(text, depths, decl_end, keyword, depths[pos])


def _local_hook_scope_end(
    text: str,
    depths: list[int],
    pos: int,
    decl_end: int,
) -> int:
    """Scope end for a local callback declared at pos.

    Brace blocks end at the enclosing '}'. An if, for, or switch header is
    its own implicit block and ends with that statement, including an if's
    else chain, so a later registration sees the outer callback.
    """
    block_end = _enclosing_block_end(text, depths, pos)
    stmt_end = _statement_header_end(text, depths, pos, decl_end)
    if stmt_end is not None and decl_end < stmt_end < block_end:
        return stmt_end
    return block_end


def _preceded_by_var(text: str, open_paren: int) -> bool:
    j = open_paren - 1
    while j >= 0 and text[j].isspace():
        j -= 1
    if j < 2 or text[j - 2:j + 1] != "var":
        return False
    if j == 2:
        return True
    prev = text[j - 3]
    return not (prev.isalnum() or prev == "_")


def _in_var_group(text: str, pos: int, depths: list[int]) -> bool:
    """True when pos sits in a `var (` group in the same brace block."""
    if pos >= len(depths) or depths[pos] == 0:
        return False
    depth = depths[pos]
    balance = 0
    i = pos
    while i > 0:
        i -= 1
        if depths[i] < depth:
            return False
        if depths[i] > depth:
            continue
        char = text[i]
        if char == ")":
            balance += 1
            continue
        if char != "(":
            continue
        if balance > 0:
            balance -= 1
            continue
        return _preceded_by_var(text, i)
    return False


def _var_hook_is_local_decl(text: str, match: re.Match[str], depths: list[int]) -> bool:
    """True for a function-local `:=`, `var name =`, or `var (` hook spec.

    `name = func` assigns to a name that already exists, so it stays on the
    package-level callback instead of opening a new scope.
    """
    if match.start() >= len(depths) or depths[match.start()] == 0:
        return False
    head = text[match.start():match.end(1)]
    if re.match(r"var\s+", head):
        return True
    between = text[match.end(1):match.end()]
    if re.match(r"(?:\s+func\b[^={}]*)?\s*:=", between):
        return True
    return _in_var_group(text, match.start(), depths)


def _local_hook_decls(string_masked: str, comment_masked: str) -> list[_LocalHook]:
    depths = _code_brace_depths(string_masked)
    found: list[_LocalHook] = []
    for match in VAR_HOOK_FUNC_RE.finditer(string_masked):
        if not _var_hook_is_local_decl(string_masked, match, depths):
            continue
        body = _extract_function_body(comment_masked, match.end())
        if body is None:
            continue
        # The short-var or var spec ends at the func literal. A call inside
        # that literal still sees the outer name.
        decl_end = match.end() + len(body) + 1
        found.append(
            _LocalHook(
                name=match.group(1),
                root_param=match.group(2),
                body=body,
                decl_end=decl_end,
                scope_end=_local_hook_scope_end(
                    string_masked, depths, match.start(), decl_end
                ),
                depth=depths[match.start()],
            )
        )
    return found


def _local_hook_at(hooks: list[_LocalHook], name: str, pos: int) -> _LocalHook | None:
    chosen: _LocalHook | None = None
    for hook in hooks:
        if hook.name != name or pos < hook.decl_end or pos >= hook.scope_end:
            continue
        if chosen is None or (hook.depth, hook.decl_end) > (chosen.depth, chosen.decl_end):
            chosen = hook
    return chosen


def _resolve_registered_hook(
    name: str,
    pos: int,
    locals_: list[_LocalHook],
    package: dict[str, tuple[str, str, int]],
) -> tuple[str, str] | None:
    """Resolve registerNovelCommand(name) from the registering block.

    Two functions can register different locals that share a name, and a
    local can shadow a package callback. The package map has one body per
    name, so the call has to pick the local in scope before falling back.
    """
    local = _local_hook_at(locals_, name, pos)
    if local is not None:
        return local.root_param, local.body
    hook = package.get(name)
    if hook is None:
        return None
    return hook[0], hook[1]


def _constructor_is_scaffold(body: str) -> bool:
    """body is comment-masked. A copy of the annotation inside a string is
    not the command's own pp:novel-scaffold marker.
    """
    masked = _mask_go_strings(body)
    for match in SCAFFOLD_ANN_RE.finditer(body):
        if masked[match.start()] == body[match.start()]:
            return True
    return False


def _named_hook_funcs(string_masked: str, comment_masked: str) -> dict[str, tuple[str, str, int]]:
    found: dict[str, tuple[str, str, int]] = {}
    for match in NAMED_HOOK_FUNC_RE.finditer(string_masked):
        body = _extract_function_body(comment_masked, match.end())
        if body is None:
            continue
        _keep_hook(found, match.group(1), match.group(2), body, 0)
    depths = _code_brace_depths(string_masked)
    for match in VAR_HOOK_FUNC_RE.finditer(string_masked):
        body = _extract_function_body(comment_masked, match.end())
        if body is None:
            continue
        depth = depths[match.start()] if match.start() < len(depths) else 0
        _keep_hook(found, match.group(1), match.group(2), body, depth)
    return found


def _record_hook_child(
    parent: str,
    ctor: str,
    root_param: str,
    bindings: dict[str, list[str]],
    root_children: dict[str, None],
    deferred: list[tuple[list[str], str]],
) -> None:
    if parent == root_param or parent == "rootCmd":
        root_children.setdefault(ctor, None)
        return
    path = bindings.get(parent)
    if path:
        deferred.append((path, ctor))


def _consume_hook_body(
    body: str,
    root_param: str,
    root_children: dict[str, None],
    deferred: list[tuple[list[str], str]],
) -> None:
    scan = _mask_go_strings(body)
    bindings = _find_bindings(body, {root_param, "rootCmd"})
    for parent, ctor in NOVEL_ATTACH_RE.findall(scan):
        _record_hook_child(parent, ctor, root_param, bindings, root_children, deferred)
    for parent, ctor in ADDCMD_CALL_RE.findall(scan):
        _record_hook_child(parent, ctor, root_param, bindings, root_children, deferred)


def _command_at_token(
    constructors: dict[str, CommandConstructor],
    names: list[str],
    token: str,
) -> CommandConstructor | None:
    """A pp:novel-scaffold command is removed when a real command with the
    same name is registered. The scaffold is often the earlier registration,
    so the first Use match is the command the running CLI has already dropped.
    """
    matches: list[CommandConstructor] = []
    for fn_name in names:
        info = constructors.get(fn_name)
        if info is None:
            continue
        leaf, _, _, _ = parse_use(info.use)
        if leaf == token:
            matches.append(info)
    for info in matches:
        if not info.scaffold:
            return info
    if matches:
        return matches[0]
    return None


def _constructor_at_path(
    constructors: dict[str, CommandConstructor],
    root_children: list[str],
    cmd_path: list[str],
) -> CommandConstructor | None:
    if not cmd_path:
        return None
    current = _command_at_token(constructors, root_children, cmd_path[0])
    if current is None:
        return None
    for token in cmd_path[1:]:
        current = _command_at_token(constructors, current.children, token)
        if current is None:
            return None
    return current


def _drop_replaced_scaffolds(
    names: list[str],
    constructors: dict[str, CommandConstructor],
) -> list[str]:
    by_leaf: dict[str, list[CommandConstructor]] = {}
    for name in names:
        info = constructors.get(name)
        if info is None:
            continue
        leaf, _, _, _ = parse_use(info.use)
        by_leaf.setdefault(leaf, []).append(info)
    replaced: set[str] = set()
    for group in by_leaf.values():
        if any(not info.scaffold for info in group):
            for info in group:
                if info.scaffold:
                    replaced.add(info.name)
    if not replaced:
        return names
    return [name for name in names if name not in replaced]


def _apply_scaffold_replacement(
    constructors: dict[str, CommandConstructor],
    root_children: list[str],
) -> list[str]:
    root_children = _drop_replaced_scaffolds(root_children, constructors)
    pending = list(root_children)
    seen: set[str] = set()
    while pending:
        name = pending.pop()
        if name in seen:
            continue
        seen.add(name)
        info = constructors.get(name)
        if info is None:
            continue
        info.children = _drop_replaced_scaffolds(info.children, constructors)
        pending.extend(info.children)
    return root_children


def _attach_deferred(
    constructors: dict[str, CommandConstructor],
    root_children: list[str],
    deferred: list[tuple[list[str], str]],
) -> None:
    pending = list(deferred)
    for _ in range(len(pending)):
        next_pending: list[tuple[list[str], str]] = []
        progressed = False
        for path, ctor in pending:
            parent = _constructor_at_path(constructors, root_children, path)
            if parent is None or parent.name == ctor:
                if parent is None:
                    next_pending.append((path, ctor))
                continue
            if ctor not in parent.children:
                parent.children.append(ctor)
                progressed = True
        pending = next_pending
        if not progressed:
            break


def _read_cli_go_files(cli_dir: Path) -> list[tuple[Path, str]]:
    src = cli_dir / "internal/cli"
    if not src.exists():
        return []
    files: list[tuple[Path, str]] = []
    for go_file in sorted(src.glob("*.go")):
        if go_file.name.endswith("_test.go"):
            continue
        try:
            files.append((go_file, read_utf8(go_file)))
        except Exception:
            continue
    return files


@lru_cache(maxsize=None)
def _command_tree(cli_dir: Path) -> tuple[dict[str, CommandConstructor], tuple[str, ...]]:
    """Build constructors and root children, including novel-command hooks.

    addNovelCommandIfAbsent(parent, newXxxCmd) is the same edge as
    parent.AddCommand(newXxxCmd). registerNovelCommand hooks are roots
    because init runs them from a file that often defines no constructor.
    A pp:novel-scaffold command is dropped when a real command with the
    same name is registered on that parent, matching the running CLI.
    """
    constructors: dict[str, CommandConstructor] = {}
    root_children: dict[str, None] = {}
    deferred: list[tuple[list[str], str]] = []
    named: dict[str, tuple[str, str, int]] = {}
    sources = _read_cli_go_files(cli_dir)

    parsed: list[tuple[Path, str, str, list[_LocalHook]]] = []
    for go_file, text in sources:
        comment_masked = _mask_go_comments(text)
        string_masked = _mask_go_strings(comment_masked)
        file_locals = _local_hook_decls(string_masked, comment_masked)
        parsed.append((go_file, comment_masked, string_masked, file_locals))
        for hook_name, hook in _named_hook_funcs(string_masked, comment_masked).items():
            _keep_hook(named, hook_name, hook[0], hook[1], hook[2])

    for go_file, comment_masked, string_masked, file_locals in parsed:
        for match in CONSTRUCTOR_RE.finditer(comment_masked):
            fn_name = match.group(1)
            body = _extract_function_body(comment_masked, match.end())
            if body is None:
                continue
            use_match = USE_RE.search(body)
            if not use_match:
                continue
            args_match = ARGS_RE.search(body)
            args_info = (args_match.group(1), args_match.group(2)) if args_match else None
            scan = _mask_go_strings(body)
            children: list[str] = []
            for child in ADDCMD_CHILD_RE.finditer(scan):
                _append_unique(children, child.group(1))
            bindings = _find_bindings(body, {"root", "rootCmd"})
            for parent, ctor in NOVEL_ATTACH_RE.findall(scan):
                if parent == "rootCmd":
                    continue
                if parent in bindings:
                    deferred.append((bindings[parent], ctor))
                else:
                    _append_unique(children, ctor)
            constructors[fn_name] = CommandConstructor(
                name=fn_name,
                file=go_file,
                use=use_match.group(1),
                args_info=args_info,
                children=children,
                scaffold=_constructor_is_scaffold(body),
            )

        for match in ROOT_ADDCMD_RE.finditer(string_masked):
            root_children.setdefault(match.group(1), None)
        for parent, ctor in NOVEL_ATTACH_RE.findall(string_masked):
            if parent == "rootCmd":
                root_children.setdefault(ctor, None)
        for root_param, body in _funclit_hooks(string_masked, comment_masked):
            _consume_hook_body(body, root_param, root_children, deferred)
        for match in HOOK_NAMED_RE.finditer(string_masked):
            resolved = _resolve_registered_hook(match.group(1), match.start(), file_locals, named)
            if resolved is None:
                continue
            root_param, body = resolved
            _consume_hook_body(body, root_param, root_children, deferred)

    root_list = list(root_children)
    _attach_deferred(constructors, root_list, deferred)
    root_list = _apply_scaffold_replacement(constructors, root_list)
    return constructors, tuple(root_list)


@lru_cache(maxsize=None)
def collect_command_constructors(cli_dir: Path) -> dict[str, CommandConstructor]:
    """Scan internal/cli/*.go for `func newXxxCmd(...) *cobra.Command`
    declarations. For each, capture the Use string, the cobra.Args
    validator, and child constructors registered with AddCommand or
    addNovelCommandIfAbsent in the function body.

    Result maps constructor name → CommandConstructor.

    Cached per cli_dir for the lifetime of the process. verify-skill
    invokes find_command_source once per recipe in SKILL.md (typically
    5-15) and the source tree doesn't change mid-run, so the
    file-system scan only needs to happen once. Callers must not
    mutate the returned dict (it's the cache's storage).
    """
    constructors, _ = _command_tree(cli_dir)
    return constructors


@lru_cache(maxsize=None)
def find_root_children(cli_dir: Path) -> list[str]:
    """Return constructors registered on the root command.

    That is rootCmd.AddCommand, addNovelCommandIfAbsent(rootCmd, ...),
    and registerNovelCommand hooks that attach to the hook's root
    parameter (addNovelCommandIfAbsent(root, ...) or root.AddCommand).
    Ordering follows source order across sorted filenames. Deduplicated.

    Cached per cli_dir; callers must not mutate the returned list.
    See collect_command_constructors for rationale."""
    _, root_children = _command_tree(cli_dir)
    return list(root_children)


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

    current = _constructor_at_path(constructors, root_children, cmd_path)
    if current is None:
        return None, None, None
    return current.file, current.use, current.args_info


def find_command_source(cli_dir: Path, cmd_path: list[str]):
    """Locate the source file whose cobra.Command matches this path.

    Returns (go_files, use_str, args_info) where go_files is a list (kept
    list-shaped for backwards compatibility — most callers iterate it for
    `flag_declared_in` lookups).

    Resolution strategy:

      1. Walk the command graph (the durable approach added in
         retro #301 F1). Edges are `<parent>.AddCommand(newXxxCmd(...))`
         and `addNovelCommandIfAbsent(<parent>, newXxxCmd(...))`, including
         commands registered from a `registerNovelCommand` hook. When the
         CLI follows that convention, this returns exactly one file per
         command path with no false positives, even when two different
         commands share a leaf name (e.g., `checkpoint save` vs
         `profile save`).

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
            text = read_utf8(go_file)
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


def iter_flag_declarations(text: str) -> Iterable[tuple[bool, str]]:
    for m in FLAG_DECL_RE.finditer(text):
        persistent, _, name = m.groups()
        yield persistent == "Persistent", name

    aliases = {
        m.group(1): m.group(2) == "Persistent"
        for m in FLAG_ALIAS_RE.finditer(text)
    }
    if not aliases:
        return

    alias_decl_re = re.compile(
        r'\b(' + "|".join(re.escape(alias) for alias in aliases) + r')\.'
        r'(' + FLAG_METHOD_PATTERN + r')P?\('
        r'&[^,]+,\s*"([a-z][a-z0-9-]*)"'
    )
    for m in alias_decl_re.finditer(text):
        yield aliases[m.group(1)], m.group(3)


def _iter_bool_flag_names(text: str) -> Iterable[str]:
    """Long-names of boolean flags declared in text (BoolVar/BoolVarP and the
    alias-receiver form). Mirrors iter_flag_declarations' direct + alias scan but
    keeps only the boolean methods, so the recipe tokenizer can tell a value-less
    boolean flag (`--json <positional>`) from a value-bearing one
    (`--filter <value>`)."""
    for m in FLAG_DECL_RE.finditer(text):
        _persistent, method, name = m.groups()
        if method in ("BoolVar", "BoolSliceVar"):
            yield name

    aliases = {
        m.group(1): m.group(2) == "Persistent"
        for m in FLAG_ALIAS_RE.finditer(text)
    }
    if not aliases:
        return

    alias_decl_re = re.compile(
        r'\b(' + "|".join(re.escape(alias) for alias in aliases) + r')\.'
        r'(' + FLAG_METHOD_PATTERN + r')P?\('
        r'&[^,]+,\s*"([a-z][a-z0-9-]*)"'
    )
    for m in alias_decl_re.finditer(text):
        if m.group(2) in ("BoolVar", "BoolSliceVar"):
            yield m.group(3)


@lru_cache(maxsize=None)
def _boolean_flag_names(cli_dir: Path) -> frozenset[str]:
    """Long-names of every boolean flag declared in the CLI's internal/cli/*.go
    (cached per cli_dir). The recipe tokenizer consults this so it never consumes
    the token after a value-less boolean flag as that flag's value — doing so
    would silently drop a real positional. A CLI-wide scan (rather than
    per-command) deliberately includes persistent/root booleans like --json or
    --verbose, which are the common case a recipe writes before a positional."""
    cli_pkg = cli_dir / "internal" / "cli"
    if not cli_pkg.is_dir():
        return frozenset()
    names: set[str] = set()
    for path in sorted(cli_pkg.glob("*.go")):
        try:
            text = read_utf8(path)
        except Exception:
            continue
        names.update(_iter_bool_flag_names(text))
    return frozenset(names)


def flag_declared_in(files: Iterable[Path], flag_name: str) -> bool:
    for f in files:
        try:
            text = read_utf8(f)
        except Exception:
            continue
        for _, name in iter_flag_declarations(text):
            if name == flag_name:
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
            text = read_utf8(f)
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
            text = read_utf8(go_file)
        except Exception:
            continue
        for m in func_re.finditer(text):
            body = go_block_body(text, m.end() - 1)
            for _, name in iter_flag_declarations(body):
                if name == flag_name:
                    return True
    return False


def persistent_flag_declared(cli_dir: Path, flag_name: str) -> bool:
    src = cli_dir / "internal/cli"
    if not src.exists():
        return False
    for go_file in src.glob("*.go"):
        try:
            text = read_utf8(go_file)
        except Exception:
            continue
        for persistent, name in iter_flag_declarations(text):
            if name == flag_name and persistent:
                return True
    return False


# ---------------------------------------------------------------------------
# Prose source extraction
# ---------------------------------------------------------------------------


def _cli_invocation_from_tokens(
    tokens: list[str],
    cli_dir: Path | None,
) -> tuple[list[str], list[str], list[str]]:
    if not tokens:
        return [], [], []

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
            if cli_dir is not None:
                _files, use_str, _args_info = find_command_source(cli_dir, cmd_path)
                if use_str:
                    _, _, optional, variadic = parse_use(use_str)
                    if optional > 0 or variadic:
                        break
            # Verify adding this token still maps to a valid command. If the
            # extended path has no source match, this token is an argument.
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
        if t == "--":
            i += 1
            continue
        if t.startswith("--"):
            flag_name = t.split("=", 1)[0].rstrip(TOKEN_TRAILING_PUNCT)
            flags.append(flag_name)
            # Skip a space-separated value (`--flag value`), but NOT when:
            #  - the value is inline (`--flag=value`) — the next token is a
            #    positional, not this flag's value; or
            #  - the flag is a known boolean flag, which takes no value, so the
            #    next token is a positional (consuming it would under-count the
            #    recipe's positional args).
            is_bool = (
                cli_dir is not None
                and flag_name.lstrip("-") in _boolean_flag_names(cli_dir)
            )
            if (
                "=" not in t
                and not is_bool
                and i + 1 < len(tokens)
                and not tokens[i + 1].startswith("-")
            ):
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

    return cmd_path, positional, flags


def _split_before_shell_operator(line: str) -> str:
    quote: str | None = None
    escaped = False
    i = 0
    while i < len(line):
        ch = line[i]
        if quote == "'":
            if ch == "'":
                quote = None
            i += 1
            continue
        if quote == '"':
            if escaped:
                escaped = False
                i += 1
                continue
            if ch == "\\":
                escaped = True
                i += 1
                continue
            if ch == quote:
                quote = None
            i += 1
            continue
        if escaped:
            escaped = False
            i += 1
            continue
        if ch == "\\":
            escaped = True
            i += 1
            continue
        if ch in ("'", '"'):
            quote = ch
            i += 1
            continue
        if ch == "<":
            placeholder = re.match(r"<[A-Za-z][A-Za-z0-9_-]*>", line[i:])
            if placeholder:
                i += placeholder.end()
                continue
            return line[:_shell_operator_cut_index(line, i)].rstrip()
        if ch in "|;&>":
            return line[:_shell_operator_cut_index(line, i)].rstrip()
        i += 1
    return line


def _strip_trailing_shell_comment(line: str) -> str:
    quote: str | None = None
    escaped = False
    i = 0
    while i < len(line):
        ch = line[i]
        if quote == "'":
            if ch == "'":
                quote = None
            i += 1
            continue
        if quote == '"':
            if escaped:
                escaped = False
                i += 1
                continue
            if ch == "\\":
                escaped = True
                i += 1
                continue
            if ch == quote:
                quote = None
            i += 1
            continue
        if escaped:
            escaped = False
            i += 1
            continue
        if ch == "\\":
            escaped = True
            i += 1
            continue
        if ch in ("'", '"'):
            quote = ch
            i += 1
            continue
        if ch == "#" and (i == 0 or line[i - 1].isspace()):
            return line[:i].rstrip()
        i += 1
    return line


def _shell_operator_cut_index(line: str, operator_index: int) -> int:
    # Redirections may be prefixed by a file descriptor, e.g. `2>err`.
    if line[operator_index] in "><":
        j = operator_index - 1
        while j >= 0 and line[j].isdigit():
            j -= 1
        if j < operator_index - 1 and (j < 0 or line[j].isspace()):
            return j + 1
    return operator_index


def _extract_prose_invocations(
    text: str,
    cli_binary: str,
    cli_dir: Path | None = None,
) -> list[tuple[list[str], list[str], list[str], str]]:
    """Return invocation-shaped plain-prose mentions of the CLI.

    Plain `<cli> <word>` mentions are common narrative prose, so this only
    treats a prose mention as command-shaped when a long flag appears after a
    plausible command token. Markdown code spans and fenced blocks are stripped
    first; those are handled by bash recipe and inline-reference scanners.
    """
    plain = INLINE_CODE.sub("", FENCED_CODE.sub("", text))
    binary = re.escape(cli_binary)
    mention_re = re.compile(rf"(?<![\w.-]){binary}\s+")
    results: list[tuple[list[str], list[str], list[str], str]] = []

    for raw_line in plain.splitlines():
        if cli_binary not in raw_line or "--" not in raw_line:
            continue
        mentions = list(mention_re.finditer(raw_line))
        for idx, m in enumerate(mentions):
            end = mentions[idx + 1].start() if idx + 1 < len(mentions) else len(raw_line)
            fragment = raw_line[m.end():end]
            try:
                tokens = shlex.split(fragment, posix=True)
            except ValueError:
                tokens = fragment.split()
            # Strip wrapping/trailing punctuation, including quotes: a
            # single-quoted prose command like `'<cli> auth login --chrome'`
            # whose closing quote shlex.split cannot balance falls back to
            # `fragment.split()` and would otherwise leak `--chrome'`.
            tokens = [
                t.strip(TOKEN_TRAILING_PUNCT)
                for t in tokens
                if t.strip(TOKEN_TRAILING_PUNCT)
            ]
            if len(tokens) < 2:
                continue

            first = tokens[0].lower()
            if not re.match(r"^[a-z][a-z0-9-]*$", first):
                continue
            first_files, _, _ = find_command_source(cli_dir, [first]) if cli_dir is not None else ([], None, None)
            # Unknown first tokens are warning-worthy only for tight
            # invocation shapes like `<cli> fake --flag`, not for narrative
            # prose such as `<cli> wraps the API ... --flag`.
            if not first_files and not tokens[1].startswith("-"):
                continue

            cmd_path, _positional, flags = _cli_invocation_from_tokens(tokens, cli_dir)
            if not flags:
                continue
            results.append((cmd_path, [], flags, "prose invocation"))

    return results


@lru_cache(maxsize=None)
def extract_cli_invocations(skill: Path, cli_binary: str, cli_dir: Path | None = None) -> tuple[tuple[tuple[str, ...], tuple[str, ...], tuple[str, ...], str], ...]:
    """Return cached (cmd_path, positional_args, flags, surface) tuples.

    Surfaces include fenced bash recipes and plain prose that is shaped like a
    real CLI invocation because it includes a long flag.

    cmd_path: leading lowercase-hyphenated tokens (up to 3)
    positional_args: non-flag tokens after cmd_path (shell-quoted strings preserved)
    flags: --flag tokens (with their -- prefix)
    """
    text = read_utf8(skill)
    blocks = CODEBLOCK_BASH.findall(text)
    results: list[tuple[list[str], list[str], list[str], str]] = []
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
            line = _strip_trailing_shell_comment(line)
            if not line.startswith(cli_binary + " "):
                continue
            # Strip shell command substitutions $(...) and backtick forms
            # FIRST — their contents are separate commands. Do this before
            # splitting on pipes so we don't mistakenly cut inside a $(...).
            line = re.sub(r"\$\([^)]*\)", "__SUBST__", line)
            line = re.sub(r"`[^`]*`", "__SUBST__", line)
            line = _split_before_shell_operator(line)
            after = line[len(cli_binary) + 1 :].strip()
            try:
                tokens = shlex.split(after, posix=True)
            except ValueError:
                tokens = after.split()
            cmd_path, positional, flags = _cli_invocation_from_tokens(tokens, cli_dir)
            if cmd_path:
                results.append((cmd_path, positional, flags, "bash recipe"))
    results.extend(_extract_prose_invocations(text, cli_binary, cli_dir))
    return tuple(
        (tuple(cmd_path), tuple(positional), tuple(flags), surface)
        for cmd_path, positional, flags, surface in results
    )


def extract_recipes(skill: Path, cli_binary: str, cli_dir: Path | None = None) -> list[tuple[list[str], list[str], list[str]]]:
    return [
        (list(cmd_path), list(positional), list(flags))
        for cmd_path, positional, flags, _surface in extract_cli_invocations(skill, cli_binary, cli_dir)
        if _surface == "bash recipe"
    ]


def _bash_blocks_with_line_numbers(text: str) -> Iterable[tuple[int, str]]:
    for match in CODEBLOCK_BASH.finditer(text):
        first_line = text[:match.start(1)].count("\n") + 1
        yield first_line, match.group(1)


def _unquoted_shell_variables(line: str) -> list[str]:
    vars_found: list[str] = []
    quote: str | None = None
    i = 0
    while i < len(line):
        ch = line[i]
        if quote == "'":
            if ch == "'":
                quote = None
            i += 1
            continue
        if quote == '"':
            if ch == "\\" and i + 1 < len(line):
                i += 2
                continue
            if ch == '"':
                quote = None
            i += 1
            continue
        if ch == "\\" and i + 1 < len(line):
            i += 2
            continue
        if ch in ("'", '"'):
            quote = ch
            i += 1
            continue
        if ch == "#" and (i == 0 or line[i - 1].isspace()):
            break
        if ch == "$":
            match = SHELL_VAR_RE.match(line, i)
            if match:
                vars_found.append(match.group(0))
                i = match.end()
                continue
        i += 1
    return vars_found


# ---------------------------------------------------------------------------
# Checks
# ---------------------------------------------------------------------------


def check_shell_var_quotes(sources: list[Path], report: Report) -> None:
    for src in sources:
        text = read_utf8(src)
        seen: set[tuple[int, str]] = set()
        for first_line, block in _bash_blocks_with_line_numbers(text):
            for offset, raw_line in enumerate(block.splitlines()):
                line_no = first_line + offset
                for var in _unquoted_shell_variables(raw_line):
                    key = (line_no, var)
                    if key in seen:
                        continue
                    seen.add(key)
                    report.findings.append(
                        Finding(
                            check="shell-var-quotes",
                            severity="error",
                            command=f"(file: {src.name})",
                            detail=f"{var} is expanded in a bash code block without double quotes",
                            evidence=f"{src.name}:{line_no}: {raw_line.strip()}",
                        )
                    )


def check_flag_names(cli_dir: Path, sources: list[Path], cli_binary: str, report: Report) -> None:
    # Scoped to printed-CLI invocations so flags belonging to other tools
    # invoked from prose (npx installers, gh, go, curl, ...) don't get
    # reported as missing declarations on the printed CLI.
    #
    # The `seen` set is scoped per source: a flag undeclared in SKILL.md
    # is reported separately from the same flag undeclared in README.md
    # so users see both surfaces and don't get a false "fixed" signal
    # after editing only the first source. Matches check_flag_commands's
    # per-source emission policy.
    all_files = list((cli_dir / "internal/cli").glob("*.go"))
    for src in sources:
        seen: set[str] = set()
        for raw_cmd_path, _positional, flags, _surface in extract_cli_invocations(src, cli_binary, cli_dir):
            cmd_path = list(raw_cmd_path)
            for raw_flag in flags:
                flag = raw_flag.lstrip("-")
                if not flag or flag in COMMON_FLAGS or flag in seen:
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
                        detail=f"--{flag} is referenced in {src.name} but not declared in any internal/cli/*.go",
                        evidence=src.name,
                    )
                )


def check_flag_commands(cli_dir: Path, sources: list[Path], cli_binary: str, report: Report) -> None:
    all_files = list((cli_dir / "internal/cli").glob("*.go"))
    for src in sources:
        seen: set[tuple[str, str]] = set()
        for raw_cmd_path, _positional, flags, _surface in extract_cli_invocations(src, cli_binary, cli_dir):
            cmd_path = list(raw_cmd_path)
            path_str = " ".join(cmd_path)
            for raw_flag in flags:
                flag = raw_flag.lstrip("-")
                key = (path_str, flag)
                if not flag or flag in COMMON_FLAGS or key in seen:
                    continue
                cmd_files, _, _ = find_command_source(cli_dir, cmd_path)
                if cmd_files and flag_declared_in(cmd_files, flag):
                    continue
                if persistent_flag_declared(cli_dir, flag):
                    continue
                if cmd_files and flag_declared_via_helper(cli_dir, cmd_files, flag):
                    continue
                seen.add(key)
                if flag_declared_in(all_files, flag):
                    report.findings.append(
                        Finding(
                            check="flag-commands",
                            severity="error",
                            command=f"{cli_binary} {path_str}",
                            detail=f"--{flag} is declared elsewhere but not on {path_str}",
                            evidence=src.name,
                        )
                    )
                else:
                    report.findings.append(
                        Finding(
                            check="flag-commands",
                            severity="error",
                            command=f"{cli_binary} {path_str}",
                            detail=f"--{flag} is not declared anywhere",
                            evidence=src.name,
                        )
                    )


def check_positional_args(cli_dir: Path, sources: list[Path], cli_binary: str, report: Report) -> None:
    for src in sources:
        for cmd_path, positional, _flags in extract_recipes(src, cli_binary, cli_dir):
            report.recipes_checked += 1
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
            evidence_args = " ".join(positional) or "(none)"
            report.findings.append(
                Finding(
                    check="positional-args",
                    severity="error" if not fp else "warning",
                    command=f"{cli_binary} {path_str}",
                    detail=f'got {actual} positional args; Use: "{use_str}" expects {min_ok}–{max_display}',
                    evidence=f"{src.name}: {evidence_args}",
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


def check_unknown_commands(cli_dir: Path, sources: list[Path], cli_binary: str, report: Report) -> None:
    """Report command paths in prose sources that have no matching cobra
    Use: declaration in internal/cli/*.go. Surfaces walked:

      - Bash recipes (extract_recipes) from every prose source, which the
        other checks already walk but skip silently when the command is
        missing
      - Inline backtick references inside SKILL.md's `## Command Reference`
        section (SKILL.md-specific structural surface; README.md has no
        equivalent canonical section)

    Each unique cmd_path is reported at most once across all sources.

    Uses the in-repo find_command_source which walks the rootCmd.AddCommand
    graph and resolves multi-level command paths (e.g., `links stale` vs
    `profile save`) without false-positive collisions on shared leaf names.
    """
    seen: set[tuple[str, ...]] = set()
    refs: list[tuple[list[str], str]] = []

    for src in sources:
        for raw_cmd_path, _pos, _flags, surface in extract_cli_invocations(src, cli_binary, cli_dir):
            cmd_path = list(raw_cmd_path)
            if cmd_path:
                refs.append((cmd_path, f"{surface} ({src.name})"))
        if src.name == "SKILL.md":
            skill_text = read_utf8(src)
            for cmd_path in _extract_inline_commands(skill_text, cli_binary):
                refs.append((cmd_path, "Command Reference inline (SKILL.md)"))

    for cmd_path, surface in refs:
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
            data = json.loads(read_utf8(manifest))
            if data.get("cli_name"):
                return data["cli_name"]
        except Exception:
            pass
    # Fallback — assume <dirname>-pp-cli
    return cli_dir.name + "-pp-cli"


def prose_sources(cli_dir: Path) -> list[Path]:
    """Return the ordered prose files to scan for bash recipes referencing
    the CLI binary. SKILL.md is required (validated by run_checks);
    README.md is scanned when present because Quick Start blocks and other
    user-facing examples there can drift the same way SKILL.md does, and
    catching them at shipcheck time prevents broken copy-paste examples
    from reaching the published library.
    """
    sources = [cli_dir / "SKILL.md"]
    readme = cli_dir / "README.md"
    if readme.exists():
        sources.append(readme)
    return sources


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
    sources = prose_sources(cli_dir)

    checks = only or {"flag-names", "flag-commands", "positional-args", "shell-var-quotes", "unknown-command"}
    if "flag-names" in checks:
        report.checks_run.append("flag-names")
        check_flag_names(cli_dir, sources, cli_binary, report)
    if "flag-commands" in checks:
        report.checks_run.append("flag-commands")
        check_flag_commands(cli_dir, sources, cli_binary, report)
    if "positional-args" in checks:
        report.checks_run.append("positional-args")
        check_positional_args(cli_dir, sources, cli_binary, report)
    if "shell-var-quotes" in checks:
        report.checks_run.append("shell-var-quotes")
        check_shell_var_quotes(sources, report)
    if "unknown-command" in checks:
        report.checks_run.append("unknown-command")
        check_unknown_commands(cli_dir, sources, cli_binary, report)
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


def _force_utf8_stdio() -> None:
    # Windows consoles default to cp1252, which cannot encode the ✓/✘ glyphs
    # this script prints. Reconfigure stdout/stderr to UTF-8 so the human
    # output renders cleanly instead of raising UnicodeEncodeError mid-print.
    # The Go wrapper also sets PYTHONIOENCODING/PYTHONUTF8 as belt-and-suspenders;
    # this call covers direct `python3 verify_skill.py ...` invocations.
    for stream in (sys.stdout, sys.stderr):
        try:
            stream.reconfigure(encoding="utf-8")  # type: ignore[attr-defined]
        except (AttributeError, OSError):
            pass


def main():
    _force_utf8_stdio()
    p = argparse.ArgumentParser(
        description="Verify SKILL.md matches shipped CLI source."
    )
    p.add_argument("--dir", required=True, help="CLI directory (contains SKILL.md + internal/cli/)")
    p.add_argument(
        "--only",
        choices=["flag-names", "flag-commands", "positional-args", "shell-var-quotes", "unknown-command"],
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
