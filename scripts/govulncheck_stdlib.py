#!/usr/bin/env python3
"""Collect reachable stdlib findings from govulncheck -json output.

Default-mode govulncheck fails only on symbol-level (called) findings. Module
and package hits for the same advisory do not fail that gate, and non-stdlib
modules are not a Go floor bump.
"""

from __future__ import annotations

import argparse
import json
import sys
from dataclasses import dataclass
from pathlib import Path

_SCRIPTS = Path(__file__).resolve().parent
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))

from go_release import parse_go_release, version_key


@dataclass(frozen=True)
class Finding:
    scan: str
    osv: str
    package: str
    symbol: str
    found: str
    fixed: str

    def key(self) -> tuple[str, str, str, str]:
        return (self.osv, self.package, self.symbol, self.fixed)


@dataclass(frozen=True)
class Report:
    findings: tuple[Finding, ...]
    fixed_version: str

    @property
    def actionable(self) -> bool:
        return bool(self.findings)

    @property
    def title(self) -> str:
        if not self.actionable:
            return ""
        target = self.fixed_version or "unresolved"
        return f"Go floor: bump to {target}"


def _symbol(frame: dict) -> str:
    function = str(frame.get("function") or "")
    receiver = str(frame.get("receiver") or "")
    if receiver and function:
        return f"{receiver}.{function}"
    return function


def _normalize_version(value: object) -> str:
    if not isinstance(value, str):
        return ""
    parsed = parse_go_release(value)
    return parsed or ""


def parse_stream(text: str, scan: str) -> list[Finding]:
    """Parse a govulncheck -json stream.

    The pinned govulncheck JSON handler pretty-prints each message and
    concatenates them. A line-oriented parser drops those findings.
    """
    findings: list[Finding] = []
    decoder = json.JSONDecoder()
    idx = 0
    length = len(text)
    while idx < length:
        while idx < length and text[idx].isspace():
            idx += 1
        if idx >= length:
            break
        try:
            message, end = decoder.raw_decode(text, idx)
        except json.JSONDecodeError as exc:
            raise ValueError(f"{scan}: invalid govulncheck JSON at byte {idx}: {exc}") from exc
        idx = end
        if not isinstance(message, dict):
            continue
        raw = message.get("finding")
        if not isinstance(raw, dict):
            continue
        trace = raw.get("trace")
        if not isinstance(trace, list) or not trace or not isinstance(trace[0], dict):
            continue
        frame = trace[0]
        if frame.get("module") != "stdlib":
            continue
        symbol = _symbol(frame)
        if not symbol:
            continue
        findings.append(
            Finding(
                scan=scan,
                osv=str(raw.get("osv") or ""),
                package=str(frame.get("package") or ""),
                symbol=symbol,
                found=_normalize_version(frame.get("version")),
                fixed=_normalize_version(raw.get("fixed_version")),
            )
        )
    return findings


def build_report(scans: list[tuple[str, str]]) -> Report:
    grouped: dict[tuple[str, str, str, str], dict[str, list[str]]] = {}
    order: list[tuple[str, str, str, str]] = []
    for scan, text in scans:
        for finding in parse_stream(text, scan):
            slot = grouped.get(finding.key())
            if slot is None:
                slot = {"scans": [], "found": []}
                grouped[finding.key()] = slot
                order.append(finding.key())
            if finding.scan not in slot["scans"]:
                slot["scans"].append(finding.scan)
            if finding.found and finding.found not in slot["found"]:
                slot["found"].append(finding.found)

    findings: list[Finding] = []
    for key in order:
        osv, package, symbol, fixed = key
        slot = grouped[key]
        findings.append(
            Finding(
                scan=", ".join(slot["scans"]),
                osv=osv,
                package=package,
                symbol=symbol,
                found=", ".join(slot["found"]),
                fixed=fixed,
            )
        )
    return Report(findings=tuple(findings), fixed_version=_max_fixed(findings))


def _max_fixed(findings: list[Finding]) -> str:
    versions = [finding.fixed for finding in findings if finding.fixed]
    if not versions:
        return ""
    return max(versions, key=version_key)


def render_body(report: Report) -> str:
    if not report.actionable:
        return ""
    if report.fixed_version:
        intro = (
            "Reachable Go standard-library vulnerabilities were found. "
            f"Bump the Printing Press Go floor to `{report.fixed_version}`."
        )
        confirm = (
            f"1. Confirm `printing-press-library` `.go-version` is already >= `{report.fixed_version}` "
            "(https://raw.githubusercontent.com/mvanhorn/printing-press-library/main/.go-version). "
            "The library bumps first."
        )
        bump = (
            "2. Bump the root `go.mod` `go` directive (and the root `toolchain` line when present) "
            f"and `librarySafeGoDirective` in `internal/generator/go_version.go` to `{report.fixed_version}`."
        )
    else:
        intro = (
            "Reachable Go standard-library vulnerabilities were found, "
            "but govulncheck did not report a fixed Go version."
        )
        confirm = (
            "1. Read the advisories below and confirm `printing-press-library` `.go-version` "
            "(https://raw.githubusercontent.com/mvanhorn/printing-press-library/main/.go-version) "
            "is already >= the Go release that fixes them. The library bumps first."
        )
        bump = (
            "2. Bump the root `go.mod` `go` directive (and the root `toolchain` line when present) "
            "and `librarySafeGoDirective` in `internal/generator/go_version.go` to that release."
        )
    lines = [
        intro,
        "",
        "These are symbol-level findings. The default `govulncheck` gate fails on them.",
        "",
        confirm,
        bump,
        "3. Run `python3 scripts/verify-go-floor.py` and update every surface it reports.",
        "",
        "Renovate may already have opened the grouped patch PR. Finish the generator text, fixtures, and goldens on that PR.",
        "",
        "## Findings",
        "",
        "| Scan | Advisory | Package | Symbol | Found in | Fixed in |",
        "| --- | --- | --- | --- | --- | --- |",
    ]
    for finding in report.findings:
        lines.append(
            "| {scan} | {osv} | {package} | {symbol} | {found} | {fixed} |".format(
                scan=finding.scan or "-",
                osv=finding.osv or "-",
                package=finding.package or "-",
                symbol=finding.symbol or "-",
                found=finding.found or "-",
                fixed=finding.fixed or "-",
            )
        )
    lines.append("")
    return "\n".join(lines)


def report_payload(report: Report) -> dict:
    return {
        "actionable": report.actionable,
        "fixed_version": report.fixed_version,
        "title": report.title,
        "findings": [
            {
                "scan": finding.scan,
                "osv": finding.osv,
                "package": finding.package,
                "symbol": finding.symbol,
                "found": finding.found,
                "fixed": finding.fixed,
            }
            for finding in report.findings
        ],
    }


def _read_scan(spec: str) -> tuple[str, str]:
    name, sep, path = spec.partition("=")
    if not sep or not name or not path:
        raise SystemExit(f"--scan must be name=path, got {spec!r}")
    return name, Path(path).read_text()


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--scan", action="append", required=True, help="name=path to a govulncheck -json log")
    parser.add_argument("--json-out", required=True)
    parser.add_argument("--body-out", required=True)
    args = parser.parse_args(argv)

    try:
        report = build_report([_read_scan(spec) for spec in args.scan])
    except (OSError, ValueError) as exc:
        print(f"govulncheck stdlib parse failed: {exc}", file=sys.stderr)
        return 1

    Path(args.json_out).write_text(json.dumps(report_payload(report), indent=2) + "\n")
    Path(args.body_out).write_text(render_body(report))
    if report.actionable:
        print(report.title)
    else:
        print("No reachable stdlib findings.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
