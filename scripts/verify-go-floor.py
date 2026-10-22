#!/usr/bin/env python3
"""Verify Go floor text and fixtures match the root go.mod directive.

librarySafeGoDirective is what printed CLIs declare. It must be <= the root
go.mod go directive. It must also be <= printing-press-library's .go-version.
That library comparison warns unless GO_FLOOR_ENFORCE_LIBRARY=1 (release.yml).
A network or parse failure warns the same way, and fails only when enforced.
"""

from __future__ import annotations

import os
import re
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

_SCRIPTS = Path(__file__).resolve().parent
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))

from go_release import parse_go_release, version_le


REPO_ROOT = Path(__file__).resolve().parents[1]

LIBRARY_GO_VERSION_URL = (
    "https://raw.githubusercontent.com/mvanhorn/printing-press-library/main/.go-version"
)
ENFORCE_LIBRARY_ENV = "GO_FLOOR_ENFORCE_LIBRARY"
DIRECTIVE_FILE = Path("internal/generator/go_version.go")
DIRECTIVE_RE = re.compile(
    r'^const\s+librarySafeGoDirective\s*=\s*"(?P<version>\d+\.\d+\.\d+)"\s*$',
    re.MULTILINE,
)


PATTERNS = [
    re.compile(r"\bGo (?P<version>\d+\.\d+\.\d+) or newer\b"),
    re.compile(r"\brequires Go (?P<version>\d+\.\d+\.\d+) or newer\b"),
    re.compile(r"\bInstall Go (?P<version>\d+\.\d+\.\d+)"),
    re.compile(r'GO_MIN_VERSION="(?P<version>\d+\.\d+\.\d+)"'),
    re.compile(r"\bPRESS_GO_REQUIRED=(?P<version>\d+\.\d+\.\d+)"),
    re.compile(r"\bPP_FAKE_GO_INSTALLED:-(?P<version>\d+\.\d+\.\d+)"),
    re.compile(r"\bPP_FAKE_GO_BINARY:-(?P<version>\d+\.\d+\.\d+)"),
    re.compile(r"\bgo version go(?P<version>\d+\.\d+\.\d+)"),
    re.compile(r'\bgoInstalled:\s+"(?P<version>\d+\.\d+\.\d+)"'),
    re.compile(r'\bgoBinary:\s+"(?P<version>\d+\.\d+\.\d+)"'),
    re.compile(r"\btoolchain=go(?P<version>\d+\.\d+\.\d+)"),
    re.compile(r"\\ngo (?P<version>\d+\.\d+\.\d+)\\n"),
    re.compile(r"\\ntoolchain go(?P<version>\d+\.\d+\.\d+)\\n"),
    re.compile(r"^go (?P<version>\d+\.\d+\.\d+)$", re.MULTILINE),
    re.compile(r"^toolchain go(?P<version>\d+\.\d+\.\d+)$", re.MULTILINE),
]


PATHS = [
    "README.md",
    "scripts/install.sh",
    "skills/printing-press/SKILL.md",
    "skills/printing-press/phases/01-preflight.md",
    "skills/printing-press-amend/SKILL.md",
    "skills/printing-press-import/SKILL.md",
    "skills/printing-press-polish/SKILL.md",
    "skills/printing-press-publish/SKILL.md",
    "skills/printing-press-score/SKILL.md",
    "internal/cli/generate_test.go",
    "internal/cli/publish_test.go",
    "internal/cli/verify_skill_test.go",
    "internal/generator/go_version_test.go",
    "internal/generator/install_section.go",
    "internal/generator/templates/readme.md.tmpl",
    "internal/generator/templates/skill.md.tmpl",
    "internal/generator/validate_test.go",
    "internal/govulncheck/govulncheck_test.go",
    "internal/pipeline/contracts_test.go",
]


ALLOWED_OLD_FIXTURES = {
    ("internal/govulncheck/govulncheck_test.go", "go 1.25.0"),
    ("internal/pipeline/contracts_test.go", 'goInstalled:     "1.25.0"'),
    ("internal/pipeline/contracts_test.go", "PRESS_GO_INSTALLED=1.25.0"),
}


def repo_go_floor() -> str:
    content = (REPO_ROOT / "go.mod").read_text()
    match = re.search(r"^go\s+(\d+\.\d+\.\d+)\s*$", content, re.MULTILINE)
    if not match:
        raise SystemExit("could not find patch-level go directive in go.mod")
    return match.group(1)


def library_safe_directive() -> str:
    content = (REPO_ROOT / DIRECTIVE_FILE).read_text()
    matches = list(DIRECTIVE_RE.finditer(content))
    if len(matches) != 1:
        raise SystemExit(
            f"expected one librarySafeGoDirective string in {DIRECTIVE_FILE}, found {len(matches)}"
        )
    return matches[0].group("version")


def directive_floor_failure(directive: str, floor: str) -> str | None:
    if version_le(directive, floor):
        return None
    return f"librarySafeGoDirective {directive} exceeds go.mod go {floor}"


def fetch_library_body(url: str = LIBRARY_GO_VERSION_URL, attempts: int = 3, timeout: float = 15.0) -> str:
    last_error = "unknown error"
    for attempt in range(attempts):
        try:
            request = urllib.request.Request(url, headers={"User-Agent": "cli-printing-press-go-floor"})
            with urllib.request.urlopen(request, timeout=timeout) as response:
                return response.read(4096).decode("utf-8", errors="replace")
        except (OSError, urllib.error.URLError) as exc:
            last_error = str(exc)
            if attempt + 1 < attempts:
                time.sleep(1)
    raise OSError(last_error)


def library_floor_status(directive: str, enforce: bool, fetch) -> tuple[list[str], list[str]]:
    """Return (failures, notes) for the printing-press-library .go-version check."""
    try:
        body = fetch(LIBRARY_GO_VERSION_URL)
    except OSError as exc:
        message = f"could not read printing-press-library .go-version ({LIBRARY_GO_VERSION_URL}): {exc}"
        if enforce:
            return [message], []
        return [], [f"warning: {message}"]

    version = parse_go_release(body)
    if version is None:
        message = f"could not parse printing-press-library .go-version from {body!r}"
        if enforce:
            return [message], []
        return [], [f"warning: {message}"]

    if not version_le(directive, version):
        message = (
            f"librarySafeGoDirective {directive} exceeds printing-press-library "
            f".go-version {version} ({LIBRARY_GO_VERSION_URL})"
        )
        if enforce:
            return [message], []
        return [], [f"warning: {message}"]

    return [], [
        f"printing-press-library .go-version {version} >= librarySafeGoDirective {directive}."
    ]


def iter_files() -> list[Path]:
    files = [REPO_ROOT / path for path in PATHS]
    files.extend((REPO_ROOT / "testdata/golden/expected").glob("**/go.mod"))
    return sorted(files)


def line_number(content: str, offset: int) -> int:
    return content.count("\n", 0, offset) + 1


def line_text(content: str, offset: int) -> str:
    start = content.rfind("\n", 0, offset) + 1
    end = content.find("\n", offset)
    if end == -1:
        end = len(content)
    return content[start:end]


def is_allowed_old_fixture(rel: Path, text: str) -> bool:
    rel_text = rel.as_posix()
    return any(rel_text == path and needle in text for path, needle in ALLOWED_OLD_FIXTURES)


def surface_failures(floor: str) -> list[str]:
    failures: list[str] = []
    for path in iter_files():
        if not path.exists():
            failures.append(f"{path.relative_to(REPO_ROOT)}: missing expected Go-floor surface")
            continue
        content = path.read_text()
        rel = path.relative_to(REPO_ROOT)
        for pattern in PATTERNS:
            for match in pattern.finditer(content):
                version = match.group("version")
                current_line = line_text(content, match.start())
                if is_allowed_old_fixture(rel, current_line):
                    continue
                # NotContains names a version that must not appear; it is not a floor pin.
                if "NotContains" in current_line:
                    continue
                if version != floor:
                    failures.append(
                        f"{rel}:{line_number(content, match.start())}: expected {floor}, found {version}"
                    )
    return failures


def emit_note(note: str) -> None:
    print(note)
    if note.startswith("warning:") and os.environ.get("GITHUB_ACTIONS") == "true":
        flat = " ".join(note.split())
        print(f"::warning::{flat}")


def enforced() -> bool:
    return os.environ.get(ENFORCE_LIBRARY_ENV) == "1"


def main() -> int:
    floor = repo_go_floor()
    directive = library_safe_directive()
    failures = surface_failures(floor)
    mismatch = directive_floor_failure(directive, floor)
    if mismatch:
        failures.append(mismatch)

    library_failures, notes = library_floor_status(directive, enforced(), fetch_library_body)
    failures.extend(library_failures)

    summary = f"go.mod go {floor}; librarySafeGoDirective {directive}."
    if failures:
        print("Go floor drift detected:")
        for failure in failures:
            print(f"  {failure}")
        print()
        print(summary)
        for note in notes:
            emit_note(note)
        print("\nUpdate go.mod once, then update the reported surfaces to the same version.")
        print("librarySafeGoDirective must be <= go.mod go, and <= printing-press-library .go-version.")
        return 1

    print(f"Go floor check passed: tracked surfaces match {floor}. {summary}")
    for note in notes:
        emit_note(note)
    return 0


if __name__ == "__main__":
    sys.exit(main())
