"""Signal catalog for the supply-chain scan (cli-printing-press mirror).

This file is vendored from mvanhorn/printing-press-library's
.github/scripts/verify-supply-chain/signals.py with scope adaptations for
the generator repo:

  - R1 (pull_request_target + PR-head checkout) — applied unchanged.
  - R2 (id-token: write outside allowlist) — applied with an empty
    allowlist. The generator repo does not currently use OIDC anywhere;
    any addition trips the rule. If release.yml migrates to keyless
    cosign with OIDC, allowlist that specific workflow at that time.
  - R3 (replace directives in library/**/go.mod) — omitted.
  - R4 (GOPROXY/GOFLAGS/GONOSUMCHECK in workflows) — applied unchanged.
  - R5 (hard-coded actions/setup-go versions) — adapted to require
    go-version-file: go.mod.
  - R6 (npm lifecycle scripts) — omitted.
  - R7 (module-path drift) — omitted.

Detection strategy for R1/R2/R4: parse the YAML structurally with pyyaml
(pre-installed on ubuntu-latest runners) and walk the parsed dict tree.
The earlier regex approach missed valid YAML forms (block-scalar values
`ref: >-` with the value on the next line, etc.) and required a separate
patch for each YAML quirk encountered. Structural parsing eliminates the
whole class.
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import PurePosixPath
from typing import Any

import yaml


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
# YAML parsing helpers (shared by R1, R2, R4)
# ---------------------------------------------------------------------------


def _parse_workflow(content: str | None) -> Any:
    """Parse a workflow YAML safely. Returns the loaded structure (typically
    a dict) or None if the content is absent or unparseable. A malformed
    workflow would also fail to load in GitHub Actions itself, so silently
    skipping it is safe."""
    if content is None:
        return None
    try:
        return yaml.safe_load(content)
    except (yaml.YAMLError, TypeError, ValueError):
        return None


def _workflow_on(parsed: Any) -> Any:
    """Extract the workflow's `on:` section.

    YAML 1.1 (pyyaml's default) interprets unquoted `on` as the boolean
    literal True — so `on:` at the top of a workflow becomes the key True
    after parsing. Check both forms; GitHub Actions accepts either."""
    if not isinstance(parsed, dict):
        return None
    if "on" in parsed:
        return parsed["on"]
    if True in parsed:
        return parsed[True]
    return None


def _has_pr_target_trigger(on_node: Any) -> bool:
    """Detect pull_request_target in any of YAML's trigger declaration forms:
    string (`on: pull_request_target`), list (`on: [...]` or
    `on:\\n  - pull_request_target`), or mapping (`on:\\n  pull_request_target:`)."""
    if on_node is None:
        return False
    if isinstance(on_node, str):
        return on_node.strip() == "pull_request_target"
    if isinstance(on_node, list):
        return any(
            isinstance(item, str) and item.strip() == "pull_request_target"
            for item in on_node
        )
    if isinstance(on_node, dict):
        return "pull_request_target" in on_node
    return False


_DANGEROUS_REF_VALUE = re.compile(
    # All forms that resolve to PR-author-controlled content under
    # pull_request_target. github.head_ref is the shorthand alias for
    # event.pull_request.head.ref (Greptile-flagged); merge_commit_sha
    # points at GitHub's synthesised merge commit which contains PR code.
    r"github\.event\.pull_request\.head\.(sha|ref)"
    r"|github\.event\.pull_request\.merge_commit_sha"
    r"|github\.head_ref"
    # [^\n] (not [^\s]) so the match survives spaces inside `${{ ... }}`
    # expressions, e.g., refs/pull/${{ github.event.number }}/merge.
    r"|refs/pull/[^\n]*?/(merge|head)"
)


def _is_dangerous_ref_value(value: Any) -> bool:
    """Return True if a checkout step's `ref:` value references the PR head."""
    if not isinstance(value, str):
        return False
    return bool(_DANGEROUS_REF_VALUE.search(value))


def _walk_checkout_refs(parsed: Any) -> list[str]:
    """Walk parsed workflow for actions/checkout steps with dangerous ref
    values. Returns the list of dangerous ref values found (one per offending
    step). Inspects every job and every step recursively but only flags the
    `with.ref` field of `uses: actions/checkout*` steps."""
    if not isinstance(parsed, dict):
        return []
    jobs = parsed.get("jobs")
    if not isinstance(jobs, dict):
        return []
    findings: list[str] = []
    for job in jobs.values():
        if not isinstance(job, dict):
            continue
        steps = job.get("steps")
        if not isinstance(steps, list):
            continue
        for step in steps:
            if not isinstance(step, dict):
                continue
            uses = step.get("uses")
            if not isinstance(uses, str) or not uses.startswith("actions/checkout"):
                continue
            with_block = step.get("with")
            if not isinstance(with_block, dict):
                continue
            ref = with_block.get("ref")
            if _is_dangerous_ref_value(ref):
                findings.append(ref.strip())
    return findings


def _walk_id_token_grants(parsed: Any) -> bool:
    """Return True if the parsed workflow grants `id-token: write` at workflow
    or job level. (GitHub Actions doesn't honor step-level permissions, so we
    skip those.)"""
    if not isinstance(parsed, dict):
        return False
    if _permissions_grant_id_token(parsed.get("permissions")):
        return True
    jobs = parsed.get("jobs")
    if isinstance(jobs, dict):
        for job in jobs.values():
            if isinstance(job, dict) and _permissions_grant_id_token(job.get("permissions")):
                return True
    return False


def _permissions_grant_id_token(perm: Any) -> bool:
    if isinstance(perm, dict):
        value = perm.get("id-token")
        return isinstance(value, str) and value.strip() == "write"
    # A string value of "write-all" grants every permission, including id-token.
    if isinstance(perm, str) and perm.strip() == "write-all":
        return True
    return False


_GO_ENV_KEYS = ("GOPROXY", "GOFLAGS", "GONOSUMCHECK", "GOSUMDB", "GONOSUMDB")


def _walk_go_env_overrides(parsed: Any) -> list[str]:
    """Return the list of GOPROXY/GOFLAGS/etc env-variable names set anywhere
    in the workflow's env blocks (workflow-level, job-level, or step-level)."""
    found: list[str] = []
    if not isinstance(parsed, dict):
        return found
    _collect_go_env_from(parsed.get("env"), found)
    jobs = parsed.get("jobs")
    if isinstance(jobs, dict):
        for job in jobs.values():
            if not isinstance(job, dict):
                continue
            _collect_go_env_from(job.get("env"), found)
            steps = job.get("steps")
            if isinstance(steps, list):
                for step in steps:
                    if isinstance(step, dict):
                        _collect_go_env_from(step.get("env"), found)
    return found


def _walk_setup_go_version_literals(parsed: Any) -> list[str]:
    """Return literal `go-version` values from actions/setup-go steps."""
    found: list[str] = []
    if not isinstance(parsed, dict):
        return found
    jobs = parsed.get("jobs")
    if not isinstance(jobs, dict):
        return found
    for job in jobs.values():
        if not isinstance(job, dict):
            continue
        steps = job.get("steps")
        if not isinstance(steps, list):
            continue
        for step in steps:
            if not isinstance(step, dict):
                continue
            uses = step.get("uses")
            if not isinstance(uses, str) or not uses.startswith("actions/setup-go"):
                continue
            with_block = step.get("with")
            if not isinstance(with_block, dict):
                continue
            version = with_block.get("go-version")
            if version is not None:
                found.append(str(version).strip())
    return found


def _collect_go_env_from(env_block: Any, out: list[str]) -> None:
    if not isinstance(env_block, dict):
        return
    for key in env_block:
        if isinstance(key, str) and key in _GO_ENV_KEYS:
            out.append(key)


def _find_line_in(content: str | None, needle: str) -> int | None:
    """Best-effort line number lookup. Returns the 1-indexed line where the
    substring first appears, or None. Used to annotate findings with a line
    pointer even when the parsed YAML loses position info."""
    if not content or not needle:
        return None
    for idx, line in enumerate(content.splitlines(), start=1):
        if needle in line:
            return idx
    return None


# ---------------------------------------------------------------------------
# R1: pull_request_target + PR-head checkout (TanStack OIDC theft)
# ---------------------------------------------------------------------------


def signal_workflow_trust(change: FileChange) -> list[Finding]:
    """R1. A workflow that combines pull_request_target with a checkout of
    the PR head ref is the TanStack mini-Shai-Hulud attack shape — head
    code runs with the elevated permissions of the base context, including
    secrets and OIDC.

    Structural diff: fires when head has the bad combo AND base didn't have
    the same dangerous ref value. (Reformatting the same dangerous YAML in
    a different style is not a new attack — only newly-introduced danger
    fires.)"""
    if not is_workflow(change.path) or change.head_content is None:
        return []

    head = _parse_workflow(change.head_content)
    if not _has_pr_target_trigger(_workflow_on(head)):
        return []

    head_refs = _walk_checkout_refs(head)
    if not head_refs:
        return []

    # Diff-aware: any dangerous ref present on base is pre-existing, not new.
    base = _parse_workflow(change.base_content)
    base_refs: set[str] = set()
    if base is not None and _has_pr_target_trigger(_workflow_on(base)):
        base_refs = set(_walk_checkout_refs(base))

    new_dangerous = [r for r in head_refs if r not in base_refs]
    if not new_dangerous:
        return []

    danger_text = new_dangerous[0]
    line = _find_line_in(change.head_content, danger_text)
    return [
        Finding(
            path=change.path,
            line=line,
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


def signal_id_token_outside_allowlist(change: FileChange) -> list[Finding]:
    """R2. id-token: write mints OIDC tokens. It must appear only in the
    workflow(s) that actually publish. Generator repo's allowlist is empty.

    Structural diff: fires only if the head workflow grants id-token: write
    and the base didn't (or didn't exist)."""
    if not is_workflow(change.path) or change.head_content is None:
        return []
    if change.path in ID_TOKEN_ALLOWLIST:
        return []

    head = _parse_workflow(change.head_content)
    if not _walk_id_token_grants(head):
        return []

    base = _parse_workflow(change.base_content)
    if base is not None and _walk_id_token_grants(base):
        # Pre-existing grant; not introduced by this PR.
        return []

    line = _find_line_in(change.head_content, "id-token")
    allowlist_label = ", ".join(sorted(ID_TOKEN_ALLOWLIST)) or "(none — generator repo has no OIDC workflows on origin/main)"
    return [
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
    ]


# ---------------------------------------------------------------------------
# R4: GOPROXY / GOFLAGS / GONOSUMCHECK overrides in workflows
# ---------------------------------------------------------------------------


def signal_go_env_override(change: FileChange) -> list[Finding]:
    """R4. Setting GOPROXY / GOFLAGS / GONOSUMCHECK / GOSUMDB inside a
    workflow env block lets an attacker redirect module resolution or
    suppress checksum verification (BufferZoneCorp).

    Structural diff: fires for each go-env key newly set in head that wasn't
    set in base."""
    if not is_workflow(change.path) or change.head_content is None:
        return []

    head_vars = set(_walk_go_env_overrides(_parse_workflow(change.head_content)))
    if not head_vars:
        return []

    base_vars: set[str] = set()
    if change.base_content is not None:
        base_vars = set(_walk_go_env_overrides(_parse_workflow(change.base_content)))

    new_vars = head_vars - base_vars
    if not new_vars:
        return []

    findings: list[Finding] = []
    for var in sorted(new_vars):
        findings.append(
            Finding(
                path=change.path,
                line=_find_line_in(change.head_content, var),
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
# R5: setup-go must read the repo-wide go.mod floor
# ---------------------------------------------------------------------------


def signal_setup_go_uses_go_version_file(change: FileChange) -> list[Finding]:
    """R5. Go workflow versions must come from go.mod. Hard-coded setup-go
    pins make emergency vulnerability bumps a multi-file hunt, and stale pins
    can keep CI on a vulnerable standard library after go.mod moves forward.

    Structural diff: fires only for literal pins newly introduced by the PR.
    """
    if not is_workflow(change.path) or change.head_content is None:
        return []

    head_literals = set(_walk_setup_go_version_literals(_parse_workflow(change.head_content)))
    if not head_literals:
        return []

    base_literals: set[str] = set()
    if change.base_content is not None:
        base_literals = set(_walk_setup_go_version_literals(_parse_workflow(change.base_content)))

    new_literals = sorted(head_literals - base_literals)
    if not new_literals:
        return []

    literal = new_literals[0]
    return [
        Finding(
            path=change.path,
            line=_find_line_in(change.head_content, "go-version:"),
            severity="block",
            signal_id="setup_go_hardcoded_version",
            message=(
                "Workflow pins actions/setup-go with `go-version: %s`. Go "
                "toolchain floor bumps must be single-source so vulnerability "
                "fixes do not leave stale workflow pins behind." % literal
            ),
            remediation=(
                "Use `go-version-file: go.mod` in actions/setup-go and bump "
                "the root go.mod directive when the repo-wide Go floor changes."
            ),
        )
    ]


# ---------------------------------------------------------------------------
# Signal dispatch
# ---------------------------------------------------------------------------


ALL_SIGNALS = (
    signal_workflow_trust,
    signal_id_token_outside_allowlist,
    signal_go_env_override,
    signal_setup_go_uses_go_version_file,
)


def run_signals(change: FileChange) -> list[Finding]:
    findings: list[Finding] = []
    for sig in ALL_SIGNALS:
        findings.extend(sig(change))
    return findings
