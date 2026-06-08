#!/usr/bin/env python3
"""Summarize Greptile PR review state.

This helper intentionally uses only `gh` and the Python standard library so it
can be run by contributor agents without local Codex plugin scripts.
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from typing import Any


DEFAULT_REPO = "mvanhorn/cli-printing-press"
ACTIONABLE_MARKERS = (
    "Issue 1 of",
    "Fix the following",
    "Comments Outside Diff",
    "remaining open item",
    "Safe to merge after fixing",
    "Safe to merge after reviewing",
)


def run(cmd: list[str], *, check: bool = True) -> subprocess.CompletedProcess[str]:
    proc = subprocess.run(cmd, text=True, capture_output=True)
    if check and proc.returncode != 0:
        sys.stderr.write(proc.stderr)
        raise SystemExit(proc.returncode)
    return proc


def split_repo(repo: str) -> tuple[str, str]:
    parts = repo.split("/")
    if len(parts) != 2 or not all(parts):
        raise SystemExit(f"--repo must be OWNER/REPO, got {repo!r}")
    return parts[0], parts[1]


def parse_graphql_response(stdout: str) -> dict[str, Any]:
    data = json.loads(stdout)
    errors = data.get("errors") or []
    if errors:
        messages = "; ".join(error.get("message", str(error)) for error in errors)
        raise SystemExit(f"GitHub GraphQL error: {messages}")
    return data


def pull_request_from_response(data: dict[str, Any], repo: str, pr_number: int) -> dict[str, Any]:
    repository = (data.get("data") or {}).get("repository")
    if repository is None:
        raise SystemExit(f"Repository {repo} not found or inaccessible")
    pr = repository.get("pullRequest")
    if pr is None:
        raise SystemExit(f"PR #{pr_number} not found in {repo}")
    return pr


def fetch_review_threads(repo: str, pr_number: int) -> tuple[str, str, list[dict[str, Any]]]:
    owner, name = split_repo(repo)
    query = """
query($owner: String!, $repo: String!, $number: Int!, $cursor: String) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      url
      headRefOid
      reviewThreads(first: 100, after: $cursor) {
        nodes {
          id
          isResolved
          isOutdated
          path
          line
          comments(first: 50) {
            nodes {
              body
              createdAt
              author { login }
              url
            }
          }
        }
        pageInfo {
          hasNextPage
          endCursor
        }
      }
    }
  }
}
"""
    cursor: str | None = None
    threads: list[dict[str, Any]] = []
    pr_url = ""
    head_sha = ""
    while True:
        cmd = [
            "gh",
            "api",
            "graphql",
            "-f",
            f"query={query}",
            "-f",
            f"owner={owner}",
            "-f",
            f"repo={name}",
            "-F",
            f"number={pr_number}",
        ]
        if cursor:
            cmd.extend(["-f", f"cursor={cursor}"])
        proc = run(cmd)
        pr = pull_request_from_response(parse_graphql_response(proc.stdout), repo, pr_number)
        pr_url = pr["url"]
        head_sha = pr["headRefOid"]
        page = pr["reviewThreads"]
        threads.extend(page["nodes"])
        page_info = page["pageInfo"]
        if not page_info["hasNextPage"]:
            return pr_url, head_sha, threads
        cursor = page_info["endCursor"]


def fetch_comments(repo: str, pr_number: int) -> list[dict[str, Any]]:
    owner, name = split_repo(repo)
    query = """
query($owner: String!, $repo: String!, $number: Int!, $cursor: String) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      comments(first: 100, after: $cursor) {
        nodes {
          id
          body
          createdAt
          updatedAt
          author { login }
          url
        }
        pageInfo {
          hasNextPage
          endCursor
        }
      }
    }
  }
}
"""
    cursor: str | None = None
    comments: list[dict[str, Any]] = []
    while True:
        cmd = [
            "gh",
            "api",
            "graphql",
            "-f",
            f"query={query}",
            "-f",
            f"owner={owner}",
            "-f",
            f"repo={name}",
            "-F",
            f"number={pr_number}",
        ]
        if cursor:
            cmd.extend(["-f", f"cursor={cursor}"])
        proc = run(cmd)
        pr = pull_request_from_response(parse_graphql_response(proc.stdout), repo, pr_number)
        page = pr["comments"]
        comments.extend(page["nodes"])
        page_info = page["pageInfo"]
        if not page_info["hasNextPage"]:
            return comments
        cursor = page_info["endCursor"]


def fetch_pr(repo: str, pr_number: int) -> dict[str, Any]:
    pr_url, head_sha, threads = fetch_review_threads(repo, pr_number)
    comments = fetch_comments(repo, pr_number)
    return {
        "url": pr_url,
        "headRefOid": head_sha,
        "reviewThreads": {"nodes": threads},
        "comments": {"nodes": comments},
    }


def fetch_checks(repo: str, pr_number: int) -> list[dict[str, Any]]:
    proc = run(
        [
            "gh",
            "pr",
            "checks",
            str(pr_number),
            "-R",
            repo,
            "--json",
            "name,bucket,state,link,description,workflow",
        ],
        check=False,
    )
    if not proc.stdout.strip():
        if proc.returncode != 0:
            sys.stderr.write(proc.stderr)
            raise SystemExit(proc.returncode)
        return []
    return json.loads(proc.stdout)


def latest_greptile_comment(comments: list[dict[str, Any]]) -> dict[str, Any] | None:
    greptile_comments = [
        c for c in comments if (c.get("author") or {}).get("login") == "greptile-apps"
    ]
    if not greptile_comments:
        return None
    return max(greptile_comments, key=lambda c: c.get("updatedAt") or c.get("createdAt") or "")


def reviewed_commit(body: str) -> str | None:
    match = re.search(r"Last reviewed commit: \[\"[^\"]+\"\]\([^)]+/commit/([0-9a-f]{7,40})\)", body)
    if match:
        return match.group(1)
    return None


def check_bucket(checks: list[dict[str, Any]], name: str) -> str:
    for check in checks:
        if check.get("name") == name:
            return check.get("bucket") or check.get("state") or "unknown"
    return "missing"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("pr_number", type=int, help="GitHub pull request number, e.g. 2492")
    parser.add_argument("--repo", default=DEFAULT_REPO, help=f"GitHub repo, default: {DEFAULT_REPO}")
    args = parser.parse_args()

    pr = fetch_pr(args.repo, args.pr_number)
    checks = fetch_checks(args.repo, args.pr_number)
    active_threads = [
        t
        for t in pr["reviewThreads"]["nodes"]
        if not t.get("isResolved") and not t.get("isOutdated")
    ]
    latest = latest_greptile_comment(pr["comments"]["nodes"])
    latest_body = latest.get("body", "") if latest else ""
    latest_reviewed = reviewed_commit(latest_body) if latest else None
    head = pr["headRefOid"]
    latest_reviewed_matches_head = (
        latest_reviewed is not None and head.startswith(latest_reviewed)
    )
    markers = [marker for marker in ACTIONABLE_MARKERS if marker in latest_body]
    greptile_review = check_bucket(checks, "Greptile Review")
    conversations_resolved = check_bucket(checks, "All conversations resolved")

    print(f"PR: {pr['url']}")
    print(f"Head SHA: {head}")
    print(f"Greptile reviewed SHA: {latest_reviewed or 'none'}")
    print(f"Greptile Review check: {greptile_review}")
    print(f"All conversations resolved check: {conversations_resolved}")
    print(f"Active unresolved review threads: {len(active_threads)}")
    print(f"Latest Greptile actionable markers: {', '.join(markers) if markers else 'none'}")

    ready = True
    if not latest_reviewed_matches_head:
        ready = False
        print("NOT READY: latest Greptile comment does not match PR head")
    if greptile_review != "pass":
        ready = False
        print("NOT READY: Greptile Review check is not passing")
    if conversations_resolved != "pass":
        ready = False
        print("NOT READY: All conversations resolved check is not passing")
    if active_threads:
        ready = False
        print("NOT READY: unresolved non-outdated review threads remain")
        for thread in active_threads:
            print(f"  - {thread.get('path')}:{thread.get('line')} {thread.get('id')}")
    if markers:
        ready = False
        print("NOT READY: latest Greptile top-level comment contains actionable markers")

    if ready:
        print("READY: Greptile feedback is resolved for the current PR head")
        return 0
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
