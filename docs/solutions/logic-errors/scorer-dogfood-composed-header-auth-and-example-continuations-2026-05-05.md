---
title: "Scorecard and dogfood parsing: composed header auth and example continuations"
date: 2026-05-05
category: logic-errors
module: internal/pipeline
problem_type: logic_error
component: tooling
related_components:
  - authentication
  - testing_framework
symptoms:
  - "Auth protocol scoring under-scored or over-scored composed header authentication when multiple security schemes represented one runtime auth contract."
  - "Kalshi-style KALSHI-ACCESS-KEY, KALSHI-ACCESS-SIGNATURE, and KALSHI-ACCESS-TIMESTAMP headers were not scored as one composed protocol."
  - "Dogfood matrix parsing split shell line continuations in Cobra Example fields into incorrect tokens."
  - "Escaped backslashes that were not shell line continuations still needed to remain intact."
root_cause: logic_error
resolution_type: code_fix
severity: medium
tags:
  - auth-protocol
  - scorecard
  - securityschemes
  - dogfood
  - shellargs
  - cobra-examples
  - tokenizer
  - line-continuation
---

# Scorecard and dogfood parsing: composed header auth and example continuations

## Problem

Two independent retro findings had the same shape: a static checker was judging generated CLI behavior from a narrower representation than the runtime actually uses.

For `auth_protocol`, the scorecard looked at OpenAPI security declarations too literally. Kalshi-style auth is one runtime protocol made from multiple headers: an access key, a signature, and a timestamp. The OpenAPI security requirement may name only the key scheme, while the generated client still emits all required headers at runtime. Scoring declaration counts missed that composed contract. Session history from the Kalshi reprint also confirmed that the printed CLI's RSA-PSS header emission was correct; the misleading signal was the scorecard, not the generated auth implementation.

For dogfood matrix parsing, Cobra `Example:` blocks used shell backslash-newline continuations. The shared `internal/shellargs` tokenizer treated those physical line breaks as token separators instead of removing the continuation before tokenization.

## Symptoms

- `auth_protocol` could score a composed header-auth CLI poorly even when runtime code set every required header.
- A missing sibling header in a composed protocol could hide behind the referenced key scheme and score too generously.
- Independent same-prefix headers could be accidentally grouped if the scorer used prefix matching alone.
- Example commands using `\` followed by LF or CRLF split into the wrong argv.
- Mid-token continuations like `foo\` followed by `bar` needed to become `foobar`, while ordinary escaped backslashes needed to stay unchanged.

## What Didn't Work

1. Treating every unreferenced OpenAPI header security scheme as unused. Some specs model composed auth by referencing the key scheme while leaving signature and timestamp siblings as separate security schemes.
2. Merging every same-prefix header scheme. `X-API-KEY` and `X-API-TOKEN` may be independent headers, not one protocol.
3. Merging schemes that are already listed as separate OpenAPI security alternatives. If the spec says `X-API-KEY` OR `X-API-SIGNATURE`, the scorer must not reinterpret that as KEY AND SIGNATURE.
4. Awarding broad auth points from unrelated signals like query auth plus env config. That can mask missing runtime header emission for required schemes.
5. Normalizing line continuations by inserting whitespace. In shell syntax, backslash-newline is removed, so mid-token continuations concatenate.

## Solution

For `auth_protocol`, score the runtime behavior of the generated client:

- Start from each OpenAPI security alternative.
- Expand a referenced `apiKey` header scheme only to unreferenced sibling header schemes with the same prefix when that prefix has signing-style companions such as `SIGNATURE`, `TIMESTAMP`, `DATE`, `NONCE`, or `DIGEST`.
- Do not pull in sibling schemes that are already referenced by any OpenAPI security requirement.
- For composed header groups, inspect `internal/client/client.go` for each required header assignment through `Header.Set` or `Header.Add`.
- If a required multi-scheme alternative has any zero-score member, keep the alternative below the passing band instead of letting the average hide the failure.

For `internal/shellargs`, remove shell line continuations before tokenization:

- Delete `\\\n` and `\\\r\n`.
- Do this before quote, escape, and whitespace processing.
- Leave all other backslash sequences alone.

The fix is intentionally systemic: it changes the scorer and shared tokenizer that every future printed CLI uses, rather than patching one printed Kalshi CLI or one matrix fixture.

## Why This Works

OpenAPI `securitySchemes` describe names and carriers, but composed request-signing auth is enforced by the concrete headers emitted at runtime. The scorecard should therefore use the spec to identify the expected contract, then use generated client code to verify that contract is actually implemented.

The signing-style suffix gate keeps the expansion narrow. It recognizes common multi-header auth families without converting arbitrary same-prefix API keys into one required group. Excluding schemes already referenced in `security` preserves explicit OpenAPI OR semantics.

The shellargs change follows POSIX shell behavior for line continuations: the escaped newline is removed before lexical splitting. That handles LF, CRLF, quoted strings, and mid-token examples with one rule while preserving unrelated escaped backslashes.

## Prevention

1. **Prefer runtime evidence when the scored dimension is runtime behavior.** For auth protocol scoring, declarations identify candidates; generated header emission proves implementation.
2. **Gate heuristic grouping with semantic shape, not just shared text.** Same-prefix matching is too broad unless paired with known composed-auth suffixes and security-requirement checks.
3. **Test both false negatives and false positives.** The composed-auth scorer needs tests for full Kalshi-style success, missing sibling failure, same-prefix independent headers, and explicit OR alternatives.
4. **Keep command-example parsing centralized.** Cobra examples, dogfood matrices, and future validators should all use `internal/shellargs` so shell-compatibility fixes land once.
5. **Model shell continuations before tokenization.** Backslash-newline removal changes the byte stream the lexer sees; treating it as a post-token cleanup misses mid-token cases.
6. **Make exact-score tests when calibrating a scorer.** A loose `>= 8` assertion can pass while hiding whether the intended protocol path scored full credit.

## Related Issues

- `#596` -- Auth_protocol scorer evaluates runtime header emission, not declaration counts.
- `#601` -- Matrix tokenizer joins line-continuation in Cobra Example fields.
- `#594` -- Kalshi retro that surfaced the composed header-auth scoring gap.
- `#597` -- Dogfood retro wave that surfaced the example-tokenizer gap.

## Related Docs

- `docs/solutions/best-practices/steinberger-scorecard-scoring-architecture-2026-03-27.md` -- architecture and invariants for scorecard dimensions.
- `docs/solutions/logic-errors/scorecard-accuracy-broadened-pattern-matching-2026-03-27.md` -- earlier scorecard false-positive and false-negative fixes.
- `docs/solutions/design-patterns/dry-run-default-for-mutator-probes-in-test-harnesses-2026-05-05.md` -- adjacent dogfood guidance for safe test-harness behavior.
