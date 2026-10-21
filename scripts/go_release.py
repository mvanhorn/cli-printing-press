"""Patch-level Go release versions (major.minor.patch)."""

from __future__ import annotations

import re

_RELEASE = re.compile(r"(?:go)?v?(\d+\.\d+\.\d+)\Z")


def parse_go_release(text: str) -> str | None:
    match = _RELEASE.fullmatch(text.strip())
    if not match:
        return None
    return match.group(1)


def version_key(version: str) -> tuple[int, int, int]:
    major, minor, patch = (int(part) for part in version.split("."))
    return major, minor, patch


def version_le(left: str, right: str) -> bool:
    return version_key(left) <= version_key(right)
