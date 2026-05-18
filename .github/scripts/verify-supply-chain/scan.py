#!/usr/bin/env python3
"""PR-time supply-chain scan for cli-printing-press (generator repo).

This file is vendored from mvanhorn/printing-press-library's
.github/scripts/verify-supply-chain/scan.py (source dated 2026-05-17).
The orchestration logic (diff walking, git plumbing, annotation
emission, exit codes) is general-purpose and copied verbatim. Only
is_scoped() is adapted to the smaller signal set used here — see
signals.py for the scope rationale.

Drift between the two repos' scan.py is surfaced by hand on PR review.
A parity script is deferred (see the published-library plan's "Deferred
to follow-up work" section).

Exit code semantics:
  0 — no block-severity findings.
  1 — at least one block-severity finding.
"""

from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path, PurePosixPath

import signals


REPO_ROOT = Path(__file__).resolve().parents[3]


# ---------------------------------------------------------------------------
# Git plumbing
# ---------------------------------------------------------------------------


def run_git(args: list[str]) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", *args],
        cwd=REPO_ROOT,
        check=False,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )


def git_show(ref: str, path: str) -> str | None:
    result = run_git(["show", f"{ref}:{path}"])
    return result.stdout if result.returncode == 0 else None


def changed_files(base_ref: str, head_ref: str) -> list[tuple[str, str]]:
    result = run_git(["diff", "--name-status", "-z", f"{base_ref}...{head_ref}"])
    if result.returncode != 0:
        print(result.stderr, file=sys.stderr)
        raise SystemExit(result.returncode)

    fields = result.stdout.split("\0")
    if fields and fields[-1] == "":
        fields.pop()

    entries: list[tuple[str, str]] = []
    i = 0
    while i < len(fields):
        status = fields[i]
        i += 1
        if not status:
            continue
        if status.startswith(("R", "C")):
            _ = fields[i]
            new_path = fields[i + 1]
            i += 2
            entries.append((status[0], new_path))
        else:
            path = fields[i]
            i += 1
            entries.append((status, path))
    return entries


def added_lines(base_ref: str, head_ref: str, path: str) -> list[tuple[int, str]]:
    result = run_git(["diff", "--unified=0", "--no-color", f"{base_ref}...{head_ref}", "--", path])
    if result.returncode != 0:
        return []

    added: list[tuple[int, str]] = []
    head_line = 0
    for raw in result.stdout.splitlines():
        if raw.startswith("@@"):
            try:
                plus = raw.split(" ")[2]
                head_line = int(plus[1:].split(",")[0])
            except (IndexError, ValueError):
                head_line = 0
            continue
        if raw.startswith("+++") or raw.startswith("---"):
            continue
        if raw.startswith("+"):
            added.append((head_line, raw[1:]))
            head_line += 1
        elif raw.startswith(" "):
            head_line += 1
    return added


# ---------------------------------------------------------------------------
# Path scoping — adapted for the generator-repo signal set
# ---------------------------------------------------------------------------


def is_scoped(path: str) -> bool:
    parts = PurePosixPath(path).parts
    if len(parts) >= 3 and parts[0] == ".github" and parts[1] == "workflows":
        return path.endswith((".yml", ".yaml"))
    return False


# ---------------------------------------------------------------------------
# Annotation emission
# ---------------------------------------------------------------------------


def annotation_escape(value: str) -> str:
    return value.replace("%", "%25").replace("\r", "%0D").replace("\n", "%0A")


def emit_annotation(f: signals.Finding) -> None:
    kind = "error" if f.is_block() else "notice"
    pieces = [f"file={f.path}"]
    if f.line is not None:
        pieces.append(f"line={f.line}")
    pieces.append(f"title=supply-chain:{f.signal_id}")
    head = ",".join(pieces)
    body = annotation_escape(f"{f.message} | Fix: {f.remediation}")
    print(f"::{kind} {head}::{body}")


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------


def build_change(base_ref: str, head_ref: str, status: str, path: str) -> signals.FileChange:
    base_content = None if status == "A" else git_show(base_ref, path)
    head_content = None if status == "D" else git_show(head_ref, path)
    diff_added = added_lines(base_ref, head_ref, path) if head_content is not None else []
    return signals.FileChange(
        path=path,
        base_content=base_content,
        head_content=head_content,
        added_lines=diff_added,
    )


def scan(base_ref: str, head_ref: str, strict: bool) -> list[signals.Finding]:
    findings: list[signals.Finding] = []
    for status, path in changed_files(base_ref, head_ref):
        if not is_scoped(path):
            continue
        change = build_change(base_ref, head_ref, status, path)
        for finding in signals.run_signals(change):
            if strict and finding.severity == "advise":
                finding = signals.Finding(
                    path=finding.path,
                    line=finding.line,
                    severity="block",
                    signal_id=finding.signal_id + ".strict",
                    message=finding.message + " [strict mode: promoted to block]",
                    remediation=finding.remediation,
                )
            findings.append(finding)
    return findings


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-ref", required=True, help="git ref for the diff base")
    parser.add_argument("--head-ref", default="HEAD", help="git ref for the diff head")
    parser.add_argument(
        "--strict",
        action="store_true",
        help="promote advisory findings to block-severity (off by default)",
    )
    args = parser.parse_args(argv)

    findings = scan(args.base_ref, args.head_ref, args.strict)

    block_count = 0
    for f in findings:
        emit_annotation(f)
        if f.is_block():
            block_count += 1

    if block_count:
        print(
            f"::error::supply-chain scan: {block_count} block-severity finding(s); see annotations above."
        )
        return 1

    advisory_count = sum(1 for f in findings if not f.is_block())
    if advisory_count:
        print(f"::notice::supply-chain scan: {advisory_count} advisory finding(s); see annotations.")
    else:
        print("supply-chain scan: no findings.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
