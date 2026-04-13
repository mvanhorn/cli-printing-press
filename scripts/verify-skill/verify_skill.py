#!/usr/bin/env python3
"""verify_skill.py — validate that SKILL.md matches the shipped CLI source.

Three checks run in sequence:

  1. flag-names — every `--flag` in SKILL.md is declared as a cobra flag
     somewhere in internal/cli/*.go.
  2. flag-commands — every `--flag` used on a specific command is declared
     on that command (or as a persistent/root flag).
  3. positional-args — positional args in bash recipes match the command's
     `Use:` field signature (required + optional + variadic).

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
FLAG_TOKEN_RE = re.compile(r"(?:^|\s)(--[a-z][a-z0-9-]*)")


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


def find_command_source(cli_dir: Path, cmd_path: list[str]):
    """Locate source file(s) whose cobra.Command matches this path.

    Returns (go_files, use_str, args_info) where go_files is a list.
    Why a list: CLIs sometimes declare the same Use in two files (historical
    artifact or generator + transcendence both shipping a version of the
    same command). Cobra only registers one at runtime, but we don't know
    which without parsing root.go's AddCommand calls. Returning all matching
    files lets callers union their flags when checking declarations.
    """
    if not cmd_path:
        return [], None, None
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

    # Multi-token paths: prefer filename-match (e.g., contacts_search.go for
    # cmd_path ["contacts", "search"]).
    if len(cmd_path) >= 2:
        expected_basename = "_".join(cmd_path).replace("-", "_") + ".go"
        for spec, go_file, use_str, args_info in candidates:
            if go_file.name == expected_basename:
                return [go_file], use_str, args_info

    # Single-token paths (or no filename match): take all files at the
    # highest specificity tier. For flag verification, any one of them
    # declaring the flag counts. For Use string, pick the representative.
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


def extract_all_flags(skill: Path) -> set[str]:
    """Return every `--flag-name` token (without `--`) used anywhere in SKILL.md."""
    text = skill.read_text()
    return {t.lstrip("-") for t in FLAG_TOKEN_RE.findall(text)}


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


def check_flag_names(cli_dir: Path, skill: Path, report: Report) -> None:
    all_files = list((cli_dir / "internal/cli").glob("*.go"))
    flags = extract_all_flags(skill) - COMMON_FLAGS
    for flag in sorted(flags):
        if not flag_declared_in(all_files, flag):
            report.findings.append(
                Finding(
                    check="flag-names",
                    severity="error",
                    command="(any)",
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
        # the parser may have under-counted cmd_path.
        if len(cmd_path) == 1 and positional and re.match(r"^[a-z][a-z0-9-]+$", positional[0]):
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

    checks = only or {"flag-names", "flag-commands", "positional-args"}
    if "flag-names" in checks:
        report.checks_run.append("flag-names")
        check_flag_names(cli_dir, skill, report)
    if "flag-commands" in checks:
        report.checks_run.append("flag-commands")
        check_flag_commands(cli_dir, skill, cli_binary, report)
    if "positional-args" in checks:
        report.checks_run.append("positional-args")
        check_positional_args(cli_dir, skill, cli_binary, report)
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
        choices=["flag-names", "flag-commands", "positional-args"],
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
