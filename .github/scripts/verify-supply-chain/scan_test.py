#!/usr/bin/env python3
"""Unit tests for the supply-chain scan (cli-printing-press mirror).

Vendored from mvanhorn/printing-press-library's scan_test.py (2026-05-17)
with the test cases for omitted signals (R3 replace, R5 npm, R6 module
path) stripped out. Tests for R1, R2, R4, and integration coverage are
retained.

Run from this directory:
    python3 -m unittest scan_test
"""

from __future__ import annotations

import shutil
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path

import scan
import signals


def _fc(
    path: str,
    *,
    base: str | None = None,
    head: str | None = None,
    added: list[tuple[int, str]] | None = None,
) -> signals.FileChange:
    return signals.FileChange(
        path=path,
        base_content=base,
        head_content=head,
        added_lines=added or [],
    )


# ---------------------------------------------------------------------------
# Signal-level unit tests
# ---------------------------------------------------------------------------


class WorkflowTrustSignalTest(unittest.TestCase):
    def test_pull_request_target_with_head_sha_ref_blocks(self) -> None:
        wf = textwrap.dedent(
            """
            name: bad
            on:
              pull_request_target:
            jobs:
              x:
                runs-on: ubuntu-latest
                steps:
                  - uses: actions/checkout@v4
                    with:
                      ref: ${{ github.event.pull_request.head.sha }}
            """
        )
        findings = signals.signal_workflow_trust(_fc(".github/workflows/bad.yml", head=wf))
        self.assertEqual(len(findings), 1)
        self.assertTrue(findings[0].is_block())

    def test_pull_request_target_with_refs_pull_merge_blocks(self) -> None:
        wf = "on: pull_request_target\njobs:\n  x:\n    steps:\n      - uses: actions/checkout@v4\n        with:\n          ref: refs/pull/${{ github.event.number }}/merge\n"
        findings = signals.signal_workflow_trust(_fc(".github/workflows/bad.yml", head=wf))
        self.assertEqual(len(findings), 1)

    def test_safe_pull_request_target_no_checkout_does_not_block(self) -> None:
        """The existing conversation-resolution-check.yml posture: pull_request_target,
        no PR-head checkout, only API calls. Must NOT trigger."""
        wf = textwrap.dedent(
            """
            on:
              pull_request_target:
            permissions:
              contents: read
              pull-requests: read
            jobs:
              gate:
                runs-on: ubuntu-latest
                steps:
                  - run: gh api repos/${{ github.repository }}/pulls/${{ github.event.pull_request.number }}
            """
        )
        findings = signals.signal_workflow_trust(_fc(".github/workflows/policy.yml", head=wf))
        self.assertEqual(findings, [])

    def test_plain_pull_request_with_head_ref_does_not_block(self) -> None:
        wf = "on: pull_request\njobs:\n  x:\n    steps:\n      - uses: actions/checkout@v4\n        with:\n          ref: ${{ github.event.pull_request.head.sha }}\n"
        findings = signals.signal_workflow_trust(_fc(".github/workflows/ci.yml", head=wf))
        self.assertEqual(findings, [])

    def test_flow_sequence_trigger_blocks(self) -> None:
        """Compact YAML flow-sequence trigger form `on: [pull_request_target, push]`."""
        wf = "on: [pull_request_target, push]\njobs:\n  x:\n    steps:\n      - uses: actions/checkout@v4\n        with:\n          ref: ${{ github.event.pull_request.head.sha }}\n"
        findings = signals.signal_workflow_trust(_fc(".github/workflows/bad.yml", head=wf))
        self.assertEqual(len(findings), 1)
        self.assertTrue(findings[0].is_block())

    def test_preexisting_dangerous_ref_unchanged_does_not_fire(self) -> None:
        """Diff-awareness: pre-existing pattern on base unchanged → no findings."""
        wf = "on:\n  pull_request_target:\njobs:\n  x:\n    steps:\n      - uses: actions/checkout@v4\n        with:\n          ref: ${{ github.event.pull_request.head.sha }}\n"
        change = _fc(".github/workflows/legacy.yml", base=wf, head=wf, added=[])
        findings = signals.signal_workflow_trust(change)
        self.assertEqual(findings, [])

    def test_block_scalar_folded_ref_blocks(self) -> None:
        """Greptile-flagged bypass: YAML folded block-scalar `ref: >-` with
        the dangerous expression on the next line evades single-line regex
        but is semantically identical. Structural YAML parsing catches it."""
        wf = (
            "on:\n  pull_request_target:\n"
            "jobs:\n  x:\n    steps:\n"
            "      - uses: actions/checkout@v4\n"
            "        with:\n"
            "          ref: >-\n"
            "            ${{ github.event.pull_request.head.sha }}\n"
        )
        findings = signals.signal_workflow_trust(_fc(".github/workflows/bad.yml", head=wf))
        self.assertEqual(len(findings), 1)
        self.assertTrue(findings[0].is_block())

    def test_block_scalar_literal_ref_blocks(self) -> None:
        """Literal block-scalar form `|-` same as folded — must also block."""
        wf = (
            "on:\n  pull_request_target:\n"
            "jobs:\n  x:\n    steps:\n"
            "      - uses: actions/checkout@v4\n"
            "        with:\n"
            "          ref: |-\n"
            "            refs/pull/123/merge\n"
        )
        findings = signals.signal_workflow_trust(_fc(".github/workflows/bad.yml", head=wf))
        self.assertEqual(len(findings), 1)

    def test_github_head_ref_shorthand_blocks(self) -> None:
        """Greptile-flagged: github.head_ref is the shorthand alias for
        event.pull_request.head.ref — must block."""
        wf = (
            "on:\n  pull_request_target:\n"
            "jobs:\n  x:\n    steps:\n"
            "      - uses: actions/checkout@v4\n"
            "        with:\n"
            "          ref: ${{ github.head_ref }}\n"
        )
        findings = signals.signal_workflow_trust(_fc(".github/workflows/bad.yml", head=wf))
        self.assertEqual(len(findings), 1)
        self.assertTrue(findings[0].is_block())

    def test_merge_commit_sha_blocks(self) -> None:
        """github.event.pull_request.merge_commit_sha = GitHub's test-merge
        commit; contains PR-author code merged with base."""
        wf = (
            "on:\n  pull_request_target:\n"
            "jobs:\n  x:\n    steps:\n"
            "      - uses: actions/checkout@v4\n"
            "        with:\n"
            "          ref: ${{ github.event.pull_request.merge_commit_sha }}\n"
        )
        findings = signals.signal_workflow_trust(_fc(".github/workflows/bad.yml", head=wf))
        self.assertEqual(len(findings), 1)


class IdTokenSignalTest(unittest.TestCase):
    def test_id_token_in_any_workflow_blocks(self) -> None:
        """In the generator repo the allowlist is empty — id-token: write
        anywhere should block."""
        wf = "permissions:\n  id-token: write\n  contents: read\n"
        findings = signals.signal_id_token_outside_allowlist(
            _fc(".github/workflows/release.yml", head=wf)
        )
        self.assertEqual(len(findings), 1)
        self.assertTrue(findings[0].is_block())

    def test_no_id_token_does_not_block(self) -> None:
        wf = "permissions:\n  contents: read\n"
        findings = signals.signal_id_token_outside_allowlist(
            _fc(".github/workflows/anything.yml", head=wf)
        )
        self.assertEqual(findings, [])

    def test_preexisting_id_token_unchanged_does_not_fire(self) -> None:
        """Diff-aware: a pre-existing id-token grant shouldn't be re-flagged
        when the diff doesn't touch it."""
        wf = "permissions:\n  id-token: write\n  contents: read\n"
        change = _fc(".github/workflows/legacy.yml", base=wf, head=wf, added=[])
        findings = signals.signal_id_token_outside_allowlist(change)
        self.assertEqual(findings, [])

    def test_id_token_with_trailing_comment_blocks(self) -> None:
        """Trailing comment must not evade the match."""
        wf = "permissions:\n  id-token: write  # justification\n"
        findings = signals.signal_id_token_outside_allowlist(
            _fc(".github/workflows/sneaky.yml", head=wf)
        )
        self.assertEqual(len(findings), 1)


class GoEnvOverrideSignalTest(unittest.TestCase):
    def test_goproxy_blocks(self) -> None:
        wf = "jobs:\n  x:\n    env:\n      GOPROXY: https://mirror.attacker.example\n"
        findings = signals.signal_go_env_override(_fc(".github/workflows/bad.yml", head=wf))
        self.assertEqual(len(findings), 1)
        self.assertTrue(findings[0].is_block())

    def test_goflags_blocks(self) -> None:
        wf = "env:\n  GOFLAGS: -insecure\n"
        findings = signals.signal_go_env_override(_fc(".github/workflows/bad.yml", head=wf))
        self.assertEqual(len(findings), 1)

    def test_gonosumcheck_blocks(self) -> None:
        wf = "env:\n  GONOSUMCHECK: '*'\n"
        findings = signals.signal_go_env_override(_fc(".github/workflows/bad.yml", head=wf))
        self.assertEqual(len(findings), 1)

    def test_unrelated_env_does_not_fire(self) -> None:
        wf = "env:\n  GO_VERSION: 1.22\n  CGO_ENABLED: 0\n"
        findings = signals.signal_go_env_override(_fc(".github/workflows/ok.yml", head=wf))
        self.assertEqual(findings, [])

    def test_preexisting_goproxy_unchanged_does_not_fire(self) -> None:
        """Diff-aware: pre-existing GOPROXY on base shouldn't re-fire."""
        wf = "env:\n  GOPROXY: https://corp.example/\n"
        change = _fc(".github/workflows/legacy.yml", base=wf, head=wf, added=[])
        findings = signals.signal_go_env_override(change)
        self.assertEqual(findings, [])


class SetupGoVersionSignalTest(unittest.TestCase):
    def test_new_hardcoded_setup_go_version_blocks(self) -> None:
        base = "jobs:\n  x:\n    steps:\n      - uses: actions/checkout@v6\n"
        head = textwrap.dedent(
            """
            jobs:
              x:
                steps:
                  - uses: actions/setup-go@v6
                    with:
                      go-version: '1.26.5'
            """
        )
        findings = signals.signal_setup_go_uses_go_version_file(
            _fc(".github/workflows/build.yml", base=base, head=head)
        )
        self.assertEqual(len(findings), 1)
        self.assertTrue(findings[0].is_block())
        self.assertEqual(findings[0].signal_id, "setup_go_hardcoded_version")

    def test_unquoted_two_segment_setup_go_version_blocks(self) -> None:
        head = textwrap.dedent(
            """
            jobs:
              x:
                steps:
                  - uses: actions/setup-go@v6
                    with:
                      go-version: 1.22
            """
        )
        findings = signals.signal_setup_go_uses_go_version_file(
            _fc(".github/workflows/build.yml", head=head)
        )
        self.assertEqual(len(findings), 1)
        self.assertTrue(findings[0].is_block())

    def test_go_version_file_does_not_block(self) -> None:
        head = textwrap.dedent(
            """
            jobs:
              x:
                steps:
                  - uses: actions/setup-go@v6
                    with:
                      go-version-file: go.mod
            """
        )
        findings = signals.signal_setup_go_uses_go_version_file(
            _fc(".github/workflows/build.yml", head=head)
        )
        self.assertEqual(findings, [])

    def test_preexisting_literal_unchanged_does_not_re_fire(self) -> None:
        wf = textwrap.dedent(
            """
            jobs:
              x:
                steps:
                  - uses: actions/setup-go@v6
                    with:
                      go-version: '1.26.5'
            """
        )
        findings = signals.signal_setup_go_uses_go_version_file(
            _fc(".github/workflows/legacy.yml", base=wf, head=wf)
        )
        self.assertEqual(findings, [])


# ---------------------------------------------------------------------------
# Integration test
# ---------------------------------------------------------------------------


class ScanIntegrationTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp(prefix="verify-supply-chain-"))
        self.addCleanup(lambda: shutil.rmtree(self.tmp))
        self.old_root = scan.REPO_ROOT
        scan.REPO_ROOT = self.tmp
        self._git("init", "-q", "-b", "main")
        self._git("config", "user.email", "test@example.com")
        self._git("config", "user.name", "Test")
        self._git("commit", "--allow-empty", "-q", "-m", "init")

    def tearDown(self) -> None:
        scan.REPO_ROOT = self.old_root

    def _git(self, *args: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["git", *args],
            cwd=self.tmp,
            check=True,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )

    def _write(self, rel: str, content: str) -> None:
        p = self.tmp / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(content)

    def _commit(self, message: str) -> None:
        self._git("add", "-A")
        self._git("commit", "-q", "--allow-empty", "-m", message)

    def _run_scan(self, base: str = "main") -> int:
        return scan.main(["--base-ref", base])

    def test_clean_pr_no_findings(self) -> None:
        self._write("internal/generator/foo.go", "package generator\n")
        self._commit("baseline")
        self._git("checkout", "-q", "-b", "feat/x")
        self._write("internal/generator/foo.go", "package generator\n// edit\n")
        self._commit("tweak")
        self.assertEqual(self._run_scan(), 0)

    def test_pr_target_with_head_checkout_fails(self) -> None:
        self._write(".github/workflows/existing.yml", "on: push\n")
        self._commit("baseline")
        self._git("checkout", "-q", "-b", "feat/x")
        self._write(
            ".github/workflows/new.yml",
            "on:\n  pull_request_target:\njobs:\n  x:\n    steps:\n      - uses: actions/checkout@v4\n        with:\n          ref: ${{ github.event.pull_request.head.sha }}\n",
        )
        self._commit("add bad workflow")
        self.assertEqual(self._run_scan(), 1)

    def test_id_token_in_any_workflow_fails(self) -> None:
        """Generator repo allowlist is empty — id-token: write in ANY workflow blocks."""
        self._write(".github/workflows/release.yml", "on: push\n")
        self._commit("baseline")
        self._git("checkout", "-q", "-b", "feat/x")
        self._write(
            ".github/workflows/release.yml",
            "on: push\npermissions:\n  id-token: write\n",
        )
        self._commit("grant id-token in release")
        self.assertEqual(self._run_scan(), 1)

    def test_goproxy_in_workflow_env_fails(self) -> None:
        self._write(".github/workflows/baseline.yml", "on: push\n")
        self._commit("baseline")
        self._git("checkout", "-q", "-b", "feat/x")
        self._write(
            ".github/workflows/baseline.yml",
            "on: push\njobs:\n  x:\n    env:\n      GOPROXY: https://mirror.attacker.example\n",
        )
        self._commit("redirect GOPROXY")
        self.assertEqual(self._run_scan(), 1)

    def test_hardcoded_setup_go_version_fails(self) -> None:
        self._write(".github/workflows/baseline.yml", "on: push\n")
        self._commit("baseline")
        self._git("checkout", "-q", "-b", "feat/x")
        self._write(
            ".github/workflows/baseline.yml",
            "on: push\njobs:\n  x:\n    steps:\n      - uses: actions/setup-go@v6\n        with:\n          go-version: '1.26.5'\n",
        )
        self._commit("hardcode setup-go version")
        self.assertEqual(self._run_scan(), 1)

    def test_go_version_file_setup_go_passes(self) -> None:
        self._write(".github/workflows/baseline.yml", "on: push\n")
        self._commit("baseline")
        self._git("checkout", "-q", "-b", "feat/x")
        self._write(
            ".github/workflows/baseline.yml",
            "on: push\njobs:\n  x:\n    steps:\n      - uses: actions/setup-go@v6\n        with:\n          go-version-file: go.mod\n",
        )
        self._commit("use shared setup-go version")
        self.assertEqual(self._run_scan(), 0)

    def test_unrelated_changes_pass(self) -> None:
        """Touching Go source under internal/ but not any workflow → no findings."""
        self._write("internal/cli/foo.go", "package cli\n")
        self._commit("baseline")
        self._git("checkout", "-q", "-b", "feat/x")
        self._write("internal/cli/foo.go", "package cli\nfunc Bar() {}\n")
        self._commit("add func")
        self.assertEqual(self._run_scan(), 0)


if __name__ == "__main__":
    unittest.main()
