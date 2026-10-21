#!/usr/bin/env python3
"""Unit tests for scripts/govulncheck_stdlib.py."""

from __future__ import annotations

import importlib.util
import json
import sys
import tempfile
import unittest
from pathlib import Path


def load_parser():
    path = Path(__file__).resolve().parent / "govulncheck_stdlib.py"
    spec = importlib.util.spec_from_file_location("govulncheck_stdlib", path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"could not load {path}")
    module = importlib.util.module_from_spec(spec)
    # dataclasses consult sys.modules while the class body executes.
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def message(payload: dict) -> str:
    return json.dumps(payload)


CALLED_STDLIB = message(
    {
        "finding": {
            "osv": "GO-2026-6809",
            "fixed_version": "v1.26.10",
            "trace": [
                {
                    "module": "stdlib",
                    "version": "v1.26.9",
                    "package": "crypto/x509",
                    "receiver": "Certificate",
                    "function": "Verify",
                },
                {
                    "module": "example.com/cli",
                    "package": "example.com/cli/internal/client",
                    "function": "Read",
                },
            ],
        }
    }
)

PACKAGE_ONLY_STDLIB = message(
    {
        "finding": {
            "osv": "GO-2026-6809",
            "fixed_version": "v1.26.10",
            "trace": [{"module": "stdlib", "version": "v1.26.9", "package": "crypto/x509"}],
        }
    }
)

CALLED_NET = message(
    {
        "finding": {
            "osv": "GO-2024-0001",
            "fixed_version": "v0.40.0",
            "trace": [
                {
                    "module": "golang.org/x/net",
                    "version": "v0.39.0",
                    "package": "golang.org/x/net/http2",
                    "function": "Foo",
                }
            ],
        }
    }
)

NEWER_STDLIB = message(
    {
        "finding": {
            "osv": "GO-2099-1",
            "fixed_version": "go1.26.12",
            "trace": [
                {
                    "module": "stdlib",
                    "version": "go1.26.10",
                    "package": "crypto/tls",
                    "function": "Handshake",
                }
            ],
        }
    }
)


class ParseTests(unittest.TestCase):
    def test_keeps_called_stdlib_and_drops_other_hits(self) -> None:
        parser = load_parser()
        text = "\n".join(
            [
                message({"config": {"protocol_version": "v1.0.0", "scan_level": "symbol"}}),
                PACKAGE_ONLY_STDLIB,
                CALLED_NET,
                CALLED_STDLIB,
                "",
            ]
        )
        findings = parser.parse_stream(text, "press")
        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0].osv, "GO-2026-6809")
        self.assertEqual(findings[0].symbol, "Certificate.Verify")
        self.assertEqual(findings[0].found, "1.26.9")
        self.assertEqual(findings[0].fixed, "1.26.10")

    def test_title_uses_the_highest_fixed_release_across_scans(self) -> None:
        parser = load_parser()
        report = parser.build_report(
            [
                ("press", CALLED_STDLIB + "\n"),
                ("golden", "\n".join([CALLED_STDLIB, NEWER_STDLIB])),
            ]
        )
        self.assertTrue(report.actionable)
        self.assertEqual(report.fixed_version, "1.26.12")
        self.assertEqual(report.title, "Go floor: bump to 1.26.12")
        merged = next(finding for finding in report.findings if finding.osv == "GO-2026-6809")
        self.assertEqual(merged.scan, "press, golden")
        body = parser.render_body(report)
        self.assertIn("Bump the Printing Press Go floor to `1.26.12`.", body)
        self.assertIn("Certificate.Verify", body)
        self.assertNotIn("golang.org/x/net", body)

    def test_unfixed_called_stdlib_uses_unresolved_title(self) -> None:
        parser = load_parser()
        raw = message(
            {
                "finding": {
                    "osv": "GO-2099-2",
                    "fixed_version": "",
                    "trace": [
                        {
                            "module": "stdlib",
                            "version": "v1.26.10",
                            "package": "crypto/x509",
                            "function": "Verify",
                        }
                    ],
                }
            }
        )
        report = parser.build_report([("press", raw)])
        self.assertEqual(report.title, "Go floor: bump to unresolved")
        self.assertIn("did not report a fixed Go version", parser.render_body(report))

    def test_invalid_json_is_an_error(self) -> None:
        parser = load_parser()
        with self.assertRaises(ValueError):
            parser.parse_stream("{not json}\n", "press")

    def test_parses_pretty_printed_stream(self) -> None:
        parser = load_parser()
        stream = "".join(
            json.dumps(obj, indent=2) + "\n"
            for obj in (
                {"config": {"protocol_version": "v1.1.0", "scan_level": "symbol"}},
                {"progress": {"message": "Scanning your code"}},
                json.loads(CALLED_STDLIB),
                json.loads(PACKAGE_ONLY_STDLIB),
                json.loads(CALLED_NET),
            )
        )
        findings = parser.parse_stream(stream, "golden")
        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0].symbol, "Certificate.Verify")
        self.assertEqual(findings[0].scan, "golden")

    def test_config_only_stream_is_not_actionable(self) -> None:
        parser = load_parser()
        report = parser.build_report(
            [("press", json.dumps({"config": {"scan_level": "symbol"}}, indent=2) + "\n")]
        )
        self.assertFalse(report.actionable)
        self.assertEqual(report.title, "")
        self.assertEqual(parser.render_body(report), "")

    def test_cli_writes_payload(self) -> None:
        parser = load_parser()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            log = root / "press.json"
            log.write_text(CALLED_STDLIB + "\n")
            payload = root / "out.json"
            body = root / "out.md"
            code = parser.main(
                [
                    "--scan",
                    f"press={log}",
                    "--json-out",
                    str(payload),
                    "--body-out",
                    str(body),
                ]
            )
            self.assertEqual(code, 0)
            decoded = json.loads(payload.read_text())
            self.assertTrue(decoded["actionable"])
            self.assertEqual(decoded["title"], "Go floor: bump to 1.26.10")
            self.assertIn("librarySafeGoDirective", body.read_text())


if __name__ == "__main__":
    unittest.main()
