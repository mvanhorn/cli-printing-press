#!/usr/bin/env python3
"""Unit tests for scripts/verify-go-floor.py."""

from __future__ import annotations

import importlib.util
import os
import unittest
from pathlib import Path


def load_checker():
    path = Path(__file__).resolve().parent / "verify-go-floor.py"
    spec = importlib.util.spec_from_file_location("verify_go_floor", path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"could not load {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class GoReleaseTests(unittest.TestCase):
    def test_parse_and_compare(self) -> None:
        checker = load_checker()
        self.assertEqual(checker.parse_go_release("1.26.10"), "1.26.10")
        self.assertEqual(checker.parse_go_release("go1.26.10\n"), "1.26.10")
        self.assertEqual(checker.parse_go_release("v1.26.10"), "1.26.10")
        self.assertIsNone(checker.parse_go_release("1.26"))
        self.assertTrue(checker.version_le("1.26.9", "1.26.10"))
        self.assertTrue(checker.version_le("1.26.10", "1.26.10"))
        self.assertTrue(checker.version_le("1.26.10", "1.27.0"))
        self.assertFalse(checker.version_le("1.26.10", "1.26.9"))


class DirectiveTests(unittest.TestCase):
    def test_real_directive_does_not_exceed_go_mod(self) -> None:
        checker = load_checker()
        directive = checker.library_safe_directive()
        floor = checker.repo_go_floor()
        self.assertIsNone(checker.directive_floor_failure(directive, floor))
        self.assertRegex(directive, r"^\d+\.\d+\.\d+$")

    def test_directive_above_go_mod_fails(self) -> None:
        checker = load_checker()
        failure = checker.directive_floor_failure("1.27.0", "1.26.10")
        self.assertIsNotNone(failure)
        self.assertIn("1.27.0", failure or "")
        self.assertIn("1.26.10", failure or "")


class LibraryFloorTests(unittest.TestCase):
    def test_older_library_warns_unless_enforced(self) -> None:
        checker = load_checker()
        failures, notes = checker.library_floor_status("1.26.10", False, lambda _url: "1.26.9\n")
        self.assertEqual(failures, [])
        self.assertTrue(any(note.startswith("warning:") and "1.26.9" in note for note in notes))

        failures, notes = checker.library_floor_status("1.26.10", True, lambda _url: "1.26.9")
        self.assertEqual(len(failures), 1)
        self.assertIn("1.26.10", failures[0])
        self.assertEqual(notes, [])

    def test_matching_library_is_a_note(self) -> None:
        checker = load_checker()
        failures, notes = checker.library_floor_status("1.26.10", False, lambda _url: "go1.26.10")
        self.assertEqual(failures, [])
        self.assertEqual(
            notes,
            ["printing-press-library .go-version 1.26.10 >= librarySafeGoDirective 1.26.10."],
        )

    def test_network_and_parse_errors_warn_unless_enforced(self) -> None:
        checker = load_checker()

        def boom(_url: str) -> str:
            raise OSError("timed out")

        failures, notes = checker.library_floor_status("1.26.10", False, boom)
        self.assertEqual(failures, [])
        self.assertTrue(any("timed out" in note for note in notes))

        failures, notes = checker.library_floor_status("1.26.10", True, boom)
        self.assertEqual(len(failures), 1)
        self.assertIn("timed out", failures[0])
        self.assertEqual(notes, [])

        failures, notes = checker.library_floor_status("1.26.10", False, lambda _url: "latest")
        self.assertEqual(failures, [])
        self.assertTrue(any("could not parse" in note for note in notes))

        failures, _notes = checker.library_floor_status("1.26.10", True, lambda _url: "latest")
        self.assertEqual(len(failures), 1)


class MainTests(unittest.TestCase):
    def setUp(self) -> None:
        self.checker = load_checker()
        self.previous = os.environ.get(self.checker.ENFORCE_LIBRARY_ENV)

    def tearDown(self) -> None:
        if self.previous is None:
            os.environ.pop(self.checker.ENFORCE_LIBRARY_ENV, None)
        else:
            os.environ[self.checker.ENFORCE_LIBRARY_ENV] = self.previous

    def test_main_passes_when_library_matches_directive(self) -> None:
        os.environ.pop(self.checker.ENFORCE_LIBRARY_ENV, None)
        directive = self.checker.library_safe_directive()
        self.checker.fetch_library_body = lambda _url=None, **_kwargs: directive
        self.assertEqual(self.checker.main(), 0)

    def test_main_warns_when_library_is_older(self) -> None:
        os.environ.pop(self.checker.ENFORCE_LIBRARY_ENV, None)
        self.checker.fetch_library_body = lambda _url=None, **_kwargs: "1.0.0"
        self.assertEqual(self.checker.main(), 0)

    def test_main_fails_when_enforced_and_library_is_older(self) -> None:
        os.environ[self.checker.ENFORCE_LIBRARY_ENV] = "1"
        self.checker.fetch_library_body = lambda _url=None, **_kwargs: "1.0.0"
        self.assertEqual(self.checker.main(), 1)

    def test_main_fails_when_enforced_and_fetch_fails(self) -> None:
        os.environ[self.checker.ENFORCE_LIBRARY_ENV] = "1"

        def boom(_url=None, **_kwargs):
            raise OSError("offline")

        self.checker.fetch_library_body = boom
        self.assertEqual(self.checker.main(), 1)


if __name__ == "__main__":
    unittest.main()
