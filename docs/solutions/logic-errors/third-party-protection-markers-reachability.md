---
title: Attribute protection markers before deriving reachability
date: 2026-05-24
category: logic-errors
module: browsersniff
problem_type: logic_error
component: tooling
symptoms:
  - "`traffic-analysis.json` set reachability.mode to browser_required when only third-party scripts contained CAPTCHA or WAF markers"
  - "Generation refused replayable browser-sniffed CLIs with requires_page_context and requires_protected_client hints"
root_cause: logic_error
resolution_type: code_fix
severity: medium
tags: [browser-sniff, traffic-analysis, reachability, waf]
---

# Attribute protection markers before deriving reachability

## Problem

Browser-sniff traffic analysis classified protection markers from every captured HAR entry, including third-party script and telemetry hosts. Modern pages commonly load Stripe, reCAPTCHA, DataDome, ad-tech, and WAF helper scripts, so unrelated assets could make a replayable first-party API capture look like it required live browser page context.

## Symptoms

- `protections[].label` included `captcha`, `aws_waf`, `datadome`, or similar labels whose evidence pointed at third-party static assets.
- `classifyReachability` promoted the whole capture to `browser_required`.
- `deriveGenerationHints` emitted `requires_page_context` and `requires_protected_client`, causing `generate --traffic-analysis` to reject a CLI even when first-party API endpoints returned 2xx JSON.

## What Didn't Work

- Filtering only by endpoint classification would drop real challenge pages from API hosts, because a 403 HTML challenge response often scores as noise even when the same host also served JSON API traffic.
- Filtering only by the capture target host would miss split-host APIs where the product page is on one registered domain and the usable API is on another.
- Counting every protection marker was too broad because third-party fingerprinting scripts are common page noise, not evidence that the target API itself needs a resident browser.

## Solution

Build an allowed protection surface before scanning marker bodies:

- Include the capture target's registered domain only when the target URL has a real HTTP(S) hostname.
- Include registered domains and exact hosts from entries already classified as API traffic.
- Analyze protection markers only when the entry is on an allowed registered domain or exact API host.

This keeps first-party challenge scripts and cross-domain API-host challenges load-bearing while dropping third-party script and tracker markers.

## Why This Works

Reachability is a property of the target surface, not the whole browser page. Endpoint classification already identifies which hosts are part of the API surface, but challenge responses from those hosts may themselves be HTML and therefore noise. Carrying both the target registered domain and discovered API hosts into protection detection gives the reachability gate the right attribution boundary.

The hostless-target guard matters for HARs whose first request is `data:` or `about:blank`: do not anchor filtering to parser fallback pseudo-hosts. Infer from real HTTP(S) target/API hosts instead.

## Prevention

- Protection labels that drive `browser_required` or `browser_clearance_http` need attribution tests, not just marker-detection tests.
- Add paired regressions for third-party marker false positives and first-party/API-host marker false negatives.
- When optimizing large-HAR scans, precompute target/API registered domains once instead of recomputing public-suffix lookups per entry.

## Related Issues

- GitHub issue #1628
- GitHub issue #1420
- `docs/solutions/logic-errors/captcha-precheck-challenge-classification-2026-05-21.md`
