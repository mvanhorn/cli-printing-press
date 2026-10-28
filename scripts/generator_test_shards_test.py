#!/usr/bin/env python3
"""Unit tests for scripts/generator_test_shards.py."""

from __future__ import annotations

import importlib.util
import json
import os
import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "generator_test_shards.py"
WORKFLOW = Path(__file__).resolve().parents[1] / ".github" / "workflows" / "main-ci.yml"


def load_planner():
    spec = importlib.util.spec_from_file_location("generator_test_shards", SCRIPT)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"could not load {SCRIPT}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def event(action: str, test: str | None, elapsed: float | None = None) -> str:
    record: dict = {"Time": "2026-10-28T16:08:05Z", "Action": action, "Package": "example/internal/generator"}
    if test is not None:
        record["Test"] = test
    if elapsed is not None:
        record["Elapsed"] = elapsed
    return json.dumps(record)


class PlanTests(unittest.TestCase):
    def setUp(self) -> None:
        self.planner = load_planner()

    def test_every_test_lands_in_exactly_one_bin(self) -> None:
        names = [f"TestCase{i:02d}" for i in range(30)]
        weights = {name: float(i % 7 + 1) for i, name in enumerate(names)}
        bins = self.planner.plan(names, weights, 4)
        flat = [name for bin_names in bins for name in bin_names]
        self.assertEqual(sorted(flat), sorted(names))
        self.assertEqual(len(flat), len(set(flat)))

    def test_bins_balance_within_largest_weight(self) -> None:
        weights = {"TestA": 10.0, "TestB": 9.0, "TestC": 8.0, "TestD": 1.0, "TestE": 1.0, "TestF": 1.0}
        bins = self.planner.plan(sorted(weights), weights, 2)
        loads = [sum(weights[name] for name in bin_names) for bin_names in bins]
        self.assertLessEqual(max(loads) - min(loads), max(weights.values()))

    def test_unknown_test_gets_median_weight(self) -> None:
        weights = {"TestA": 1.0, "TestB": 3.0, "TestC": 100.0}
        self.assertEqual(self.planner.weight_for("TestNew", weights), 3.0)
        bins = self.planner.plan(["TestA", "TestB", "TestC", "TestNew"], weights, 2)
        self.assertEqual(sum(bin_names.count("TestNew") for bin_names in bins), 1)

    def test_parse_list_keeps_test_example_fuzz(self) -> None:
        listing = "TestOne\nExampleTwo\nFuzzThree\n\nok  \texample/internal/generator\t0.4s\n"
        self.assertEqual(self.planner.parse_list(listing), ["TestOne", "ExampleTwo", "FuzzThree"])

    def test_parse_list_rejects_duplicates_and_empty(self) -> None:
        with self.assertRaises(self.planner.PlanError):
            self.planner.parse_list("TestOne\nTestOne\n")
        with self.assertRaises(self.planner.PlanError):
            self.planner.parse_list("ok  \texample\t0.1s\n")

    def test_plan_rejects_bad_total(self) -> None:
        with self.assertRaises(self.planner.PlanError):
            self.planner.plan(["TestA"], {}, 0)


class SelectCommandTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.weights = Path(self.tmp.name) / "weights.json"
        self.names = [f"TestCase{i:02d}" for i in range(23)]
        self.weights.write_text(json.dumps({name: float(i + 1) for i, name in enumerate(self.names[:20])}))

    def run_select(self, index: int, total: int, listing: str, hash_seed: str = "0") -> subprocess.CompletedProcess:
        env = dict(os.environ, PYTHONHASHSEED=hash_seed)
        return subprocess.run(
            [sys.executable, str(SCRIPT), "select", "--weights", str(self.weights), "--total", str(total), "--index", str(index)],
            input=listing,
            capture_output=True,
            text=True,
            env=env,
            check=False,
        )

    def test_shards_partition_the_list_across_processes(self) -> None:
        listing = "\n".join(self.names) + "\nok  \texample\t1.0s\n"
        selected: list[str] = []
        for index in range(4):
            result = self.run_select(index, 4, listing)
            self.assertEqual(result.returncode, 0, result.stderr)
            selected.extend(result.stdout.split())
        self.assertEqual(sorted(selected), sorted(self.names))
        self.assertEqual(len(selected), len(set(selected)))

    def test_plan_is_independent_of_hash_seed(self) -> None:
        listing = "\n".join(reversed(self.names)) + "\n"
        for index in range(3):
            outputs = {self.run_select(index, 3, listing, seed).stdout for seed in ("0", "1", "2")}
            self.assertEqual(len(outputs), 1)

    def test_summary_reports_unknown_and_stale(self) -> None:
        listing = "\n".join(self.names[2:]) + "\n"
        result = self.run_select(0, 2, listing)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("unknown=3", result.stderr)
        self.assertIn("stale=2", result.stderr)

    def test_index_out_of_range_fails(self) -> None:
        result = self.run_select(4, 4, "\n".join(self.names))
        self.assertNotEqual(result.returncode, 0)

    def test_empty_input_fails(self) -> None:
        result = self.run_select(0, 2, "")
        self.assertNotEqual(result.returncode, 0)

    def test_empty_bin_fails(self) -> None:
        result = self.run_select(2, 3, "TestCase00\nTestCase01\n")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no tests", result.stderr)


class VerifyCommandTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.selected = Path(self.tmp.name) / "selected.txt"
        self.events = Path(self.tmp.name) / "events.json"
        self.selected.write_text("TestPass\nTestFail\nTestSkip\n")

    def run_verify(self) -> subprocess.CompletedProcess:
        return subprocess.run(
            [sys.executable, str(SCRIPT), "verify", "--selected", str(self.selected), "--events", str(self.events)],
            capture_output=True,
            text=True,
            check=False,
        )

    def test_all_reported_passes(self) -> None:
        self.events.write_text(
            "\n".join(
                [
                    "=== build output that is not json",
                    event("run", "TestPass"),
                    event("pass", "TestPass/sub", 1.0),
                    event("pass", "TestPass", 2.0),
                    event("fail", "TestFail", 1.0),
                    event("skip", "TestSkip", 0.0),
                    event("pass", None, 3.0),
                ]
            )
        )
        result = self.run_verify()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_missing_result_fails_and_names_test(self) -> None:
        self.events.write_text(
            "\n".join([event("pass", "TestPass", 2.0), event("run", "TestFail"), event("pass", "TestFail/sub", 1.0), event("skip", "TestSkip", 0.0)])
        )
        result = self.run_verify()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("TestFail", result.stderr)


class RefreshTests(unittest.TestCase):
    def setUp(self) -> None:
        self.planner = load_planner()

    def test_parses_github_log_prefix_and_discounts_parallel_tests(self) -> None:
        log_line = "full-test (generator-0)\tTest\t2026-10-28T16:10:00.1234567Z " + event("pass", "TestParallel", 9.0)
        lines = [
            event("run", "TestParallel"),
            event("pause", "TestParallel"),
            log_line,
            event("pass", "TestSerial", 4.0),
            event("pass", "TestSerial/sub", 99.0),
            event("fail", "TestFailed", 50.0),
        ]
        weights = self.planner.weights_from_events([lines])
        self.assertEqual(weights, {"TestParallel": 3.0, "TestSerial": 4.0})

    def test_median_across_inputs(self) -> None:
        runs = [[event("pass", "TestSerial", seconds)] for seconds in (1.0, 5.0, 2.0)]
        self.assertEqual(self.planner.weights_from_events(runs), {"TestSerial": 2.0})


class WorkflowMatrixTests(unittest.TestCase):
    def matrix_problem(self, text: str) -> str | None:
        shards = re.findall(r"^\s*-\s*shard:\s*generator-(\d+)\s*$", text, re.MULTILINE)
        totals = re.findall(r"^\s*shard_total=(\d+)\s*$", text, re.MULTILINE)
        if len(totals) != 1:
            return f"expected one shard_total= assignment, found {totals}"
        total = int(totals[0])
        if sorted(int(s) for s in shards) != list(range(total)):
            return f"matrix generator shards {shards} do not match shard_total={total}"
        return None

    def test_main_ci_matrix_matches_shard_total(self) -> None:
        self.assertIsNone(self.matrix_problem(WORKFLOW.read_text()))

    def test_matrix_drift_is_detected(self) -> None:
        drifted = "\n".join(f"          - shard: generator-{i}" for i in range(9)) + "\n              shard_total=10\n"
        self.assertIsNotNone(self.matrix_problem(drifted))


if __name__ == "__main__":
    unittest.main()
