---
title: Paid Submissions Must Not Replay; Media Uploads Should Retry With a Size-Scaled Deadline
date: 2026-09-30
category: docs/solutions/runtime-errors
module: internal/generator/templates
problem_type: runtime_error
component: tooling
symptoms:
  - "A generation POST that hit a 5xx or dropped connection was sent four times (one attempt plus three retries)"
  - "A multi-megabyte multipart upload failed with: read tcp ...: read: operation timed out, while a smaller file of the same image worked"
  - "A prediction completed and was billed, but the command exited non-zero with only a poll or download error, so the result URL was lost"
root_cause: logic_error
resolution_type: code_fix
severity: high
tags: [retry, idempotency, billing, multipart, upload, timeout, polling, novel-commands]
---

# Paid Submissions Must Not Replay; Media Uploads Should Retry With a Size-Scaled Deadline

## Problem

Printed CLIs for generation APIs (image/video models, LLM jobs) mix three
kinds of request with opposite retry needs:

| Request | Cost | Safe to replay after an ambiguous failure? |
|---|---|---|
| Submit a prediction (POST) | billed per call | No. The server may have accepted it. |
| Upload an input file (multipart POST) | free | Yes. Each attempt yields an independent URL. |
| Poll status / fetch result (GET) | free | Yes. |

Clients printed before v4.26.0 (#3615) retried every verb on transport errors
and 5xx, so a gateway timeout on a paid submission could start and bill up to
four generations. The current `client.go.tmpl` fixed that with
`canRetryAmbiguousFailure`, but that same rule now also refuses to retry
free uploads unless the operator sets an idempotency key, and uploads still
use the JSON-sized `--timeout` default.

Hand-written novel commands in these CLIs also tend to wrap submit, poll, and
download in one helper that returns the first error. When polling or the CDN
download fails after the prediction is billed, the prediction ID and output
URLs are dropped.

## Solution (applied in the WaveSpeed printed CLI)

1. Generic client: replay only non-mutating verbs or `readOnlyIntent` reads
   after a transport error or 5xx. 429 stays retryable for every verb.
2. Upload helper: build the multipart body once, retry transport errors,
   429, and 5xx with backoff, and give each attempt a deadline of
   `--timeout + size / 128 KiB/s` on a copied `http.Client`. The shared
   client's timeout must not be mutated.
3. Polling: treat network errors, 429, and 5xx on the status GET as
   transient until the wait deadline, with a cap on consecutive failures.
4. After submission succeeds, wrap later failures in an error type that
   carries the prediction ID and the recovery command. Print the last known
   payload before exiting non-zero.
5. Download after completion is a warning. Return it as data
   (`DownloadErr`) so every caller can report the output URLs.

## Generator follow-up

- Classify multipart file uploads as replay-safe only when the spec or a
  `x-pp-idempotent-upload: true` extension says so. A general "multipart is
  safe" rule is wrong for APIs where upload creates a billed resource.
- Scale the per-request deadline for requests that carry a file body by
  payload size, as `StreamingHTTPClient` already does for binary responses.
- Keep the submit/poll/download split in novel-command scaffolding so
  post-billing failures never discard the result.

## Diagnostic note

`read: operation timed out` comes from the socket (ETIMEDOUT), not from
`http.Client.Timeout` (which says `Client.Timeout exceeded`). It means the
connection stalled. A longer client timeout alone would not have saved the
request. The retry is what recovers it.
