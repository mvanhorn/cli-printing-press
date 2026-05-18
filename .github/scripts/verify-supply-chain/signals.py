"""Signal catalog for the supply-chain scan (cli-printing-press mirror).

This file is vendored from mvanhorn/printing-press-library's
.github/scripts/verify-supply-chain/signals.py (source dated 2026-05-17),
with scope adaptations for the generator repo:

  - R1 (pull_request_target + PR-head checkout) — applied unchanged.
  - R2 (id-token: write outside allowlist) — applied with an empty
    allowlist. The generator repo does not currently use OIDC anywhere;
    any addition trips the rule. If release.yml migrates to keyless
    cosign with OIDC, allowlist that specific workflow at that time.
  - R3 (replace directives in library/**/go.mod) — omitted. The
    generator's testdata fixtures may legitimately use unusual replace
    shapes for golden tests. R3 already protects published-CLI go.mod
    files in the published-library repo where it matters.
  - R4 (GOPROXY/GOFLAGS/GONOSUMCHECK in workflows) — applied unchanged.
  - R5 (npm lifecycle scripts) — omitted. No npm wrapper here.
  - R6 (module-path drift) — omitted. No published-CLI module paths here.

Keep this file structurally aligned with the source so future signal
additions can be cherry-picked across both repos with minimal friction.
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import PurePosixPath


# ---------------------------------------------------------------------------
# Types
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class Finding:
    path: str
    line: int | None
    severity: str  # "block" | "advise"
    signal_id: str
    message: str
    remediation: str

    def is_block(self) -> bool:
        return self.severity == "block"


@dataclass(frozen=True)
class FileChange:
    path: str
    base_content: str | None
    head_content: str | None
    added_lines: list[tuple[int, str]]


# ---------------------------------------------------------------------------
# Path-scope helpers
# ---------------------------------------------------------------------------


def is_workflow(path: str) -> bool:
    parts = PurePosixPath(path).parts
    return (
        len(parts) >= 3
        and parts[0] == ".github"
        and parts[1] == "workflows"
        and (path.endswith(".yml") or path.endswith(".yaml"))
    )


# Empty allowlist in the generator repo — id-token: write should not appear
# in any current workflow.
ID_TOKEN_ALLOWLIST: set[str] = set()


# ---------------------------------------------------------------------------
# R1: pull_request_target + non-default checkout ref (TanStack OIDC theft)
# ---------------------------------------------------------------------------


# Detects pull_request_target as a YAML trigger declaration in any of YAML's
# valid forms — block, inline, list, and flow-sequence (`on: [..., ...]`).
# Anchored so prose mentions inside comments or string values don't false-fire.
_PR_TARGET_TRIGGER_LINE = re.compile(
    r"^\s*(?:-\s+|on\s*:\s*)?pull_request_target(?:\s*:|\s*$)",
    re.MULTILINE,
)
_PR_TARGET_TRIGGER_FLOW = re.compile(
    r"^\s*on\s*:\s*\[[^\]\n]*\bpull_request_target\b",
    re.MULTILINE,
)


def _has_pr_target_trigger(content: str) -> bool:
    return bool(
        _PR_TARGET_TRIGGER_LINE.search(content) or _PR_TARGET_TRIGGER_FLOW.search(content)
    )


_CHECKOUT_USES = re.compile(r"^\s*-\s*uses\s*:\s*actions/checkout", re.MULTILINE)
_DANGEROUS_REF = re.compile(
    r"ref\s*:\s*[^\n#]*"
    r"(github\.event\.pull_request\.head\.(sha|ref)|refs/pull/[^\n]*?/(merge|head))",
)


def _lines_to_scan(change: FileChange) -> list[tuple[int, str]]:
    """Yield lines that should be scanned for new additions. Diff-aware:
    new files yield every head line; existing files yield only added_lines.
    Prevents R1/R2/R4 from re-flagging pre-existing patterns on PRs that
    don't touch them.
    """
    if change.head_content is None:
        return []
    if change.base_content is None:
        return list(enumerate(change.head_content.splitlines(), start=1))
    return list(change.added_lines)


def signal_workflow_trust(change: FileChange) -> list[Finding]:
    if not is_workflow(change.path) or change.head_content is None:
        return []

    content = change.head_content
    if not _has_pr_target_trigger(content):
        return []
    if not _CHECKOUT_USES.search(content):
        return []

    danger_match = _DANGEROUS_REF.search(content)
    if not danger_match:
        return []

    relevant_lines = _lines_to_scan(change)
    introduced_here = False
    danger_line: int | None = None
    danger_text: str = danger_match.group(0).strip()
    for line_no, line in relevant_lines:
        if _DANGEROUS_REF.search(line):
            introduced_here = True
            danger_line = line_no
            danger_text = _DANGEROUS_REF.search(line).group(0).strip()
            break
        if _has_pr_target_trigger(line) or _CHECKOUT_USES.search(line):
            introduced_here = True

    if not introduced_here:
        return []

    if danger_line is None:
        danger_line = content.count("\n", 0, danger_match.start()) + 1

    return [
        Finding(
            path=change.path,
            line=danger_line,
            severity="block",
            signal_id="workflow_trust_pr_head_checkout",
            message=(
                "pull_request_target workflow checks out PR head code "
                "(matched: %r). This is the TanStack mini-Shai-Hulud attack "
                "shape — head code runs with base-context secrets and OIDC." % danger_text
            ),
            remediation=(
                "Use `pull_request` instead, or omit the `ref:` override on "
                "actions/checkout so it stays on the base commit. Never run "
                "PR head code under pull_request_target."
            ),
        )
    ]


# ---------------------------------------------------------------------------
# R2: id-token: write outside the publishing allowlist
# ---------------------------------------------------------------------------


_ID_TOKEN_WRITE = re.compile(r"^\s*id-token\s*:\s*write\s*(?:#.*)?$")


def signal_id_token_outside_allowlist(change: FileChange) -> list[Finding]:
    if not is_workflow(change.path) or change.head_content is None:
        return []
    if change.path in ID_TOKEN_ALLOWLIST:
        return []

    findings: list[Finding] = []
    for line_no, line_content in _lines_to_scan(change):
        if not _ID_TOKEN_WRITE.match(line_content):
            continue
        allowlist_label = ", ".join(sorted(ID_TOKEN_ALLOWLIST)) or "(none — generator repo has no OIDC workflows on origin/main)"
        findings.append(
            Finding(
                path=change.path,
                line=line_no,
                severity="block",
                signal_id="id_token_outside_allowlist",
                message=(
                    "id-token: write is granted in a workflow outside the "
                    "publishing allowlist (%s)." % allowlist_label
                ),
                remediation=(
                    "Remove the id-token permission. If a publishing workflow "
                    "with OIDC is being introduced, add it to ID_TOKEN_ALLOWLIST "
                    "in signals.py in the same PR with reviewer sign-off."
                ),
            )
        )
    return findings


# ---------------------------------------------------------------------------
# R4: GOPROXY / GOFLAGS / GONOSUMCHECK overrides in workflows
# ---------------------------------------------------------------------------


_GO_ENV_OVERRIDE = re.compile(
    r"^\s*(GOPROXY|GOFLAGS|GONOSUMCHECK|GOSUMDB|GONOSUMDB)\s*:\s*\S"
)


def signal_go_env_override(change: FileChange) -> list[Finding]:
    if not is_workflow(change.path) or change.head_content is None:
        return []

    findings: list[Finding] = []
    for line_no, line_content in _lines_to_scan(change):
        match = _GO_ENV_OVERRIDE.match(line_content)
        if not match:
            continue
        var = match.group(1)
        findings.append(
            Finding(
                path=change.path,
                line=line_no,
                severity="block",
                signal_id="go_env_override_in_workflow",
                message=(
                    "Workflow sets %s in an env block. This can redirect Go "
                    "module resolution to an attacker proxy or suppress "
                    "checksum verification (BufferZoneCorp attack shape)." % var
                ),
                remediation=(
                    "Remove the env override. If a private GOPROXY is required, "
                    "configure it at the org or runner level under operator review, "
                    "not in a workflow file that PRs can modify."
                ),
            )
        )
    return findings


# ---------------------------------------------------------------------------
# Signal dispatch
# ---------------------------------------------------------------------------


ALL_SIGNALS = (
    signal_workflow_trust,
    signal_id_token_outside_allowlist,
    signal_go_env_override,
)


def run_signals(change: FileChange) -> list[Finding]:
    findings: list[Finding] = []
    for sig in ALL_SIGNALS:
        findings.extend(sig(change))
    return findings
