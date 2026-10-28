#!/usr/bin/env python3
"""Plan, verify, and re-weight Main CI's internal/generator test shards.

select   reads `go test -list` output on stdin and prints this shard's tests.
verify   fails when a selected top-level test reported no pass/fail/skip.
refresh  rebuilds the weights file from `go test -json` output or job logs.

Every shard process recomputes the whole plan, so planning must not depend
on set or dict iteration order: all decisions iterate sorted lists.
"""

from __future__ import annotations

import argparse
import json
import statistics
import sys
from collections import Counter
from pathlib import Path

DEFAULT_WEIGHTS = Path(__file__).resolve().parent / "generator-test-weights.json"
LISTED_PREFIXES = ("Test", "Example", "Fuzz")
TERMINAL_ACTIONS = {"pass", "fail", "skip"}
# Parallel tests share the 4-vCPU runner, so their elapsed overstates the
# wall time they cost a shard by about the measured elapsed/wall ratio.
PARALLEL_DISCOUNT = 3.0
# Rounding sub-50ms tests to zero would stack them all on one shard.
MIN_WEIGHT = 0.1


class PlanError(Exception):
    pass


def parse_list(text: str) -> list[str]:
    names = [line.strip() for line in text.splitlines() if line.strip().startswith(LISTED_PREFIXES)]
    if not names:
        raise PlanError("no Test/Example/Fuzz names in go test -list output")
    duplicates = sorted(name for name, count in Counter(names).items() if count > 1)
    if duplicates:
        raise PlanError(f"duplicate test names: {', '.join(duplicates)}")
    return names


def weight_for(name: str, weights: dict[str, float]) -> float:
    if name in weights:
        return weights[name]
    return statistics.median(weights.values()) if weights else 1.0


def plan(names: list[str], weights: dict[str, float], total: int) -> list[list[str]]:
    """Longest-processing-time greedy: heaviest first into the lightest bin."""
    if total < 1:
        raise PlanError(f"shard total must be >= 1, got {total}")
    ordered = sorted(names, key=lambda name: (-weight_for(name, weights), name))
    bins: list[list[str]] = [[] for _ in range(total)]
    loads = [0.0] * total
    for name in ordered:
        target = min(range(total), key=lambda i: (loads[i], i))
        bins[target].append(name)
        loads[target] += weight_for(name, weights)

    flat = [name for bin_names in bins for name in bin_names]
    if sorted(flat) != sorted(names) or len(flat) != len(set(flat)):
        raise PlanError("shard plan is not an exact partition of the test list")
    return bins


def load_weights(path: Path) -> dict[str, float]:
    return {name: float(value) for name, value in json.loads(path.read_text()).items()}


def json_events(lines):
    for line in lines:
        start = line.find('{"Time"')
        if start < 0:
            continue
        try:
            yield json.loads(line[start:])
        except json.JSONDecodeError:
            continue


def is_top_level(test: str | None) -> bool:
    return bool(test) and "/" not in test


def weights_from_events(runs) -> dict[str, float]:
    samples: dict[str, list[float]] = {}
    parallel: set[str] = set()
    for lines in runs:
        for event in json_events(lines):
            test = event.get("Test")
            if not is_top_level(test):
                continue
            if event.get("Action") == "pause":
                parallel.add(test)
            elif event.get("Action") == "pass":
                samples.setdefault(test, []).append(float(event.get("Elapsed", 0.0)))
    weights = {}
    for test in sorted(samples):
        seconds = statistics.median(samples[test])
        if test in parallel:
            seconds /= PARALLEL_DISCOUNT
        weights[test] = max(MIN_WEIGHT, round(seconds, 1))
    return weights


def cmd_select(args: argparse.Namespace) -> int:
    if not 0 <= args.index < args.total:
        raise PlanError(f"--index {args.index} is outside 0..{args.total - 1}")
    names = parse_list(sys.stdin.read())
    weights = load_weights(args.weights)
    bins = plan(names, weights, args.total)
    mine = bins[args.index]
    if not mine:
        raise PlanError(f"shard {args.index} of {args.total} has no tests")

    listed = set(names)
    unknown = sum(1 for name in names if name not in weights)
    stale = sum(1 for name in weights if name not in listed)
    loads = [round(sum(weight_for(name, weights) for name in bin_names)) for bin_names in bins]
    print(
        f"shard {args.index}/{args.total}: {len(mine)} of {len(names)} tests, "
        f"predicted load {loads[args.index]}s (all shards: {loads}); unknown={unknown} stale={stale}",
        file=sys.stderr,
    )
    if unknown or stale:
        print("weights are out of date; run `scripts/generator_test_shards.py refresh` on recent CI logs", file=sys.stderr)
    print("\n".join(mine))
    return 0


def cmd_verify(args: argparse.Namespace) -> int:
    selected = [line.strip() for line in args.selected.read_text().splitlines() if line.strip()]
    with args.events.open(errors="replace") as events:
        reported = {
            event["Test"]
            for event in json_events(events)
            if is_top_level(event.get("Test")) and event.get("Action") in TERMINAL_ACTIONS
        }
    missing = [name for name in selected if name not in reported]
    if missing:
        print(f"{len(missing)} selected tests reported no result:", file=sys.stderr)
        for name in missing:
            print(f"  {name}", file=sys.stderr)
        return 1
    print(f"all {len(selected)} selected tests reported a result", file=sys.stderr)
    return 0


def cmd_refresh(args: argparse.Namespace) -> int:
    runs = []
    for path in args.inputs:
        with path.open(errors="replace") as handle:
            runs.append(handle.readlines())
    weights = weights_from_events(runs)
    if not weights:
        raise PlanError("no passing top-level tests found in inputs")
    args.weights.write_text(json.dumps(weights, indent=2, sort_keys=True) + "\n")
    print(f"wrote {len(weights)} weights to {args.weights}", file=sys.stderr)
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    commands = parser.add_subparsers(dest="command", required=True)

    select = commands.add_parser("select", help="print this shard's top-level tests")
    select.add_argument("--weights", type=Path, default=DEFAULT_WEIGHTS)
    select.add_argument("--total", type=int, required=True)
    select.add_argument("--index", type=int, required=True)
    select.set_defaults(func=cmd_select)

    verify = commands.add_parser("verify", help="check every selected test reported a result")
    verify.add_argument("--selected", type=Path, required=True)
    verify.add_argument("--events", type=Path, required=True)
    verify.set_defaults(func=cmd_verify)

    refresh = commands.add_parser("refresh", help="rebuild weights from go test -json output or job logs")
    refresh.add_argument("--weights", type=Path, default=DEFAULT_WEIGHTS)
    refresh.add_argument("inputs", type=Path, nargs="+")
    refresh.set_defaults(func=cmd_refresh)

    args = parser.parse_args(argv)
    try:
        return args.func(args)
    except PlanError as err:
        print(f"error: {err}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
