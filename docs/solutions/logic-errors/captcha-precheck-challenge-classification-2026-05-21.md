---
title: Distinguish CAPTCHA precheck decisions from challenge evidence
date: 2026-05-21
category: logic-errors
module: browsersniff
problem_type: logic_error
component: tooling
symptoms:
  - "Small 2xx JSON CAPTCHA precheck responses were classified as full CAPTCHA challenges"
  - "`traffic-analysis.json` set reachability.mode to browser_required for replayable API captures"
root_cause: logic_error
resolution_type: code_fix
severity: medium
tags: [browser-sniff, traffic-analysis, captcha, reachability]
---

# Distinguish CAPTCHA precheck decisions from challenge evidence

## Problem

Browser-sniff traffic analysis treated CAPTCHA-related substrings in any response body as challenge evidence. That collapsed benign preflight endpoints such as `/api/c/check` into the same `protections[].label: "captcha"` bucket as real challenge pages, which made `reachability.mode` become `browser_required` and blocked generation for otherwise replayable APIs.

## Symptoms

- A `POST /api/c/check` response with `200 application/json` and a tiny body such as `{"present":false}` produced a `captcha` protection.
- `deriveGenerationHints` emitted `requires_page_context` because the false protection drove `browser_required`.
- Agents were pushed toward abandoning generation even though the capture contained normal authenticated API traffic.

## What Didn't Work

- Checking only response paths was too broad because many SaaS APIs expose CAPTCHA eligibility endpoints before sensitive operations.
- Checking only provider tokens such as `turnstile`, `hcaptcha`, or `recaptcha` was still too broad because negative precheck JSON can mention those provider names.
- Suppressing all 2xx JSON CAPTCHA text was too narrow because some APIs return real positive challenge decisions as JSON.

## Solution

Make the classifier decision-shaped:

- Known preflight paths with successful JSON responses are recorded as `auth.captcha_preflight`.
- The same preflight response is classified as a `captcha` protection only when parsed JSON contains an explicit positive decision such as `captcha_required: true` or a message saying a CAPTCHA is required.
- Non-preflight successful JSON responses are parsed for explicit positive CAPTCHA decisions before falling back to text heuristics.
- Provider-name-only mentions in successful JSON, such as `turnstile_enabled: false`, remain informational.

The regression coverage should include both directions:

- Negative preflight decisions stay `standard_http` and emit `auth_supports_captcha_preflight`.
- Positive preflight decisions and non-preflight JSON challenge decisions still emit `captcha` and promote reachability.

## Why This Works

CAPTCHA preflight endpoints answer "is a challenge required?" They are auth context, not challenge evidence by themselves. The classifier must preserve that distinction before the reachability gate consumes `protections`, because `captcha` is load-bearing and intentionally promotes to `browser_required`.

Parsing small JSON decisions keeps the machine general across reCAPTCHA, hCaptcha, Turnstile, and custom eligibility endpoints without hardcoding one API's path. It also prevents the inverse regression where a real JSON challenge decision is hidden just because the response status is 200.

## Prevention

- Treat protection labels as challenge evidence only after response status, content type, and body semantics agree.
- When adding a new traffic-analysis field, update `schema traffic-analysis`, golden output, and loader/unmarshal tests in the same change.
- For browser-sniff reachability fixes, write paired regressions for false positives and false negatives.

## Related Issues

- GitHub issue #1420
- `docs/plans/2026-04-21-001-feat-browser-sniff-traffic-analysis-plan.md`
- `docs/plans/2026-04-27-001-feat-printing-press-food52-retro-fixes-plan.md`
