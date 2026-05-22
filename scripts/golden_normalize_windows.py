#!/usr/bin/env python3
"""Post-pass for scripts/golden.sh normalize_text on Windows.

The sed-based normalize_text in golden.sh only handles POSIX-form paths
(forward slashes). When the generator runs on Windows it emits absolute
paths with backslashes, and the same path appears two ways in captured
output:

  - stderr.txt: `C:\\Users\\name\\repo\\.gotmp\\golden\\actual\\...`
    (single backslashes between segments; this is the literal byte
    sequence in the file)
  - JSON files: `C:\\\\Users\\\\name\\\\repo\\\\.gotmp\\\\golden\\\\actual\\\\...`
    (json.dumps doubles each backslash, so the literal bytes contain
    two backslashes per separator)

Both forms slip past the sed substitution and leave Windows absolute
paths in expected/ goldens, which then fail byte-for-byte comparison
on Linux CI.

This script reads stdin, takes four path arguments (actual_abs,
actual_root, repo_root, HOME) as the script does in bash, and emits
stdout with the Windows-form variants of those paths replaced by the
same <ARTIFACT_DIR> / <REPO> / <HOME> tokens. It also normalizes
backslash separators inside tokenized path segments to forward slash
so goldens are portable, and strips the .exe suffix on tokenized
binary paths so Linux CI (which builds ELF binaries with no extension)
matches.

POSIX-form substitution still happens in the sed pipeline upstream;
this is purely additive.
"""
import os
import re
import sys


def windows_variants(p: str) -> list[str]:
    """Backslash-form variants of a POSIX-style path."""
    if not p:
        return []
    win = p.replace("/", "\\")
    win_json = win.replace("\\", "\\\\")
    return [win_json, win]  # JSON-escaped first (longer prefix wins)


def main() -> int:
    if len(sys.argv) != 5:
        sys.stderr.write(
            "usage: golden_normalize_windows.py <actual_abs> <actual_root> <repo_root> <home>\n"
        )
        return 2

    actual_abs, actual_root, repo_root, home = sys.argv[1:]
    text = sys.stdin.read()

    for variant in windows_variants(actual_abs):
        text = text.replace(variant, "<ARTIFACT_DIR>")
    for variant in windows_variants(actual_root):
        text = text.replace(variant, "<ARTIFACT_DIR>")
    for variant in windows_variants(repo_root):
        text = text.replace(variant, "<REPO>")
    for variant in windows_variants(home):
        text = text.replace(variant, "<HOME>")

    # Collapse any run of backslashes to a single forward slash. JSON-string
    # literals encode each path separator as two backslash chars (`\\`); a
    # naive char-by-char replace would emit `//` instead of `/` (Greptile P1
    # on PR #1398 — Linux CI compares against single-slash tokens).
    text = re.sub(
        r"(<(?:ARTIFACT_DIR|REPO|HOME)>)((?:\\+[A-Za-z0-9._\-]+)+)",
        lambda m: m.group(1) + re.sub(r"\\+", "/", m.group(2)),
        text,
    )
    text = re.sub(r"(<(?:ARTIFACT_DIR|REPO)>/[A-Za-z0-9._\-/]+)\.exe", r"\1", text)

    sys.stdout.write(text)
    return 0


if __name__ == "__main__":
    sys.exit(main())
