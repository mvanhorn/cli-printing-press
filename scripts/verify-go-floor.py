#!/usr/bin/env python3
"""Verify Go floor text and fixtures match the root go.mod directive."""

from __future__ import annotations

import re
import sys
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[1]


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


def main() -> int:
    floor = repo_go_floor()
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
                if version != floor:
                    failures.append(
                        f"{rel}:{line_number(content, match.start())}: expected {floor}, found {version}"
                    )

    if failures:
        print("Go floor drift detected:")
        for failure in failures:
            print(f"  {failure}")
        print("\nUpdate go.mod once, then update the reported surfaces to the same version.")
        return 1

    print(f"Go floor check passed: all tracked surfaces match {floor}.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
