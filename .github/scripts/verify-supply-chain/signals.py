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


# Detects pull_request_target as a YAML trigger declaration:
#   on:
#     pull_request_target:        ← block form
#   on: pull_request_target       ← inline form
#   on:
#     - pull_request_target       ← list form
# Anchored to line-start (after indent) so prose mentions inside comments
# or string values don't false-positive.
_PR_TARGET_TRIGGER = re.compile(
    r"^\s*(?:-\s+|on\s*:\s*)?pull_request_target(?:\s*:|\s*$)",
    re.MULTILINE,
)
_CHECKOUT_USES = re.compile(r"^\s*-\s*uses\s*:\s*actions/checkout", re.MULTILINE)
_DANGEROUS_REF = re.compile(
    r"ref\s*:\s*[^\n#]*"
    r"(github\.event\.pull_request\.head\.(sha|ref)|refs/pull/[^\n]*?/(merge|head))",
)


def signal_workflow_trust(change: FileChange) -> list[Finding]:
    if not is_workflow(change.path) or change.head_content is None:
        return []

    content = change.head_content
    if not _PR_TARGET_TRIGGER.search(content):
        return []
    if not _CHECKOUT_USES.search(content):
        return []
    match = _DANGEROUS_REF.search(content)
    if not match:
        return []

    line = content.count("\n", 0, match.start()) + 1
    return [
        Finding(
            path=change.path,
            line=line,
            severity="block",
            signal_id="workflow_trust_pr_head_checkout",
            message=(
                "pull_request_target workflow checks out PR head code "
                "(matched: %r). This is the TanStack mini-Shai-Hulud attack "
                "shape — head code runs with base-context secrets and OIDC." % match.group(0).strip()
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


_ID_TOKEN_WRITE = re.compile(r"^\s*id-token\s*:\s*write\s*$", re.MULTILINE)


def signal_id_token_outside_allowlist(change: FileChange) -> list[Finding]:
    if not is_workflow(change.path) or change.head_content is None:
        return []
    if change.path in ID_TOKEN_ALLOWLIST:
        return []

    findings: list[Finding] = []
    for match in _ID_TOKEN_WRITE.finditer(change.head_content):
        line = change.head_content.count("\n", 0, match.start()) + 1
        allowlist_label = ", ".join(sorted(ID_TOKEN_ALLOWLIST)) or "(none — generator repo has no OIDC workflows on origin/main)"
        findings.append(
            Finding(
                path=change.path,
                line=line,
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
    r"^\s*(GOPROXY|GOFLAGS|GONOSUMCHECK|GOSUMDB|GONOSUMDB)\s*:\s*\S",
    re.MULTILINE,
)


def signal_go_env_override(change: FileChange) -> list[Finding]:
    if not is_workflow(change.path) or change.head_content is None:
        return []

    findings: list[Finding] = []
    for match in _GO_ENV_OVERRIDE.finditer(change.head_content):
        line = change.head_content.count("\n", 0, match.start()) + 1
        var = match.group(1)
        findings.append(
            Finding(
                path=change.path,
                line=line,
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
