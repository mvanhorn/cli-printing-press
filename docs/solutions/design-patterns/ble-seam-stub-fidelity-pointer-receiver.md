---
title: Testability seams around stateful third-party backends
date: 2026-06-02
category: design-patterns
module: internal/devicesniff/ble
problem_type: design_pattern
component: testing_framework
related_components:
  - service_object
  - tooling
severity: high
applies_when:
  - "wrapping a stateful third-party Go type behind a testability seam interface"
  - "the wrapped type uses pointer receivers that mutate internal state across calls"
  - "writing an in-memory stub for a backend whose return-value contracts differ from naive assumptions"
  - "verifying stub fidelity when hardware or external I/O makes direct integration tests impractical"
tags:
  - ble
  - testability-seam
  - pointer-receiver
  - stub-fidelity
  - third-party-backend
  - go
  - goroutine-leak
  - dependency-source-verification
---

# Testability seams around stateful third-party backends

## Context

We wrap hardware and OS-level backends behind Go interfaces so lifecycle logic (scan, connect, read, notify, teardown) is testable without physical devices. The BLE seam lives in `internal/devicesniff/ble/`: `driver.go` declares the `bleDriver` / `bleDevice` / `bleService` / `bleCharacteristic` interfaces, `driver_tinygo.go` implements them against `tinygo.org/x/bluetooth`, and `adapter_live_test.go` drives the whole adapter against an in-memory stub.

The seam worked for control flow, but two tests stayed green while shipping code that **panicked or leaked goroutines on real hardware**. The interface wasn't wrong — the production wrapper and the stub each diverged silently from the real backend's contracts. A seam introduced for testability gives false confidence unless the wrapper preserves the backend's value/pointer mutation semantics *and* the stub honors the backend's real contracts.

## Guidance

### Rule 1 — Store a pointer-receiver, state-mutating backend by pointer, not by value

If a wrapped type has methods with **pointer receivers that mutate internal state**, store it by pointer in the wrapper. A value field holds a copy, so a pointer-receiver method mutates that copy and the change is invisible to the next call through the wrapper.

Confirmed in `tinygo.org/x/bluetooth@v0.15.0/gattc_linux.go`: `(*DeviceCharacteristic).EnableNotifications` (line 253) is a pointer receiver. Enable sets `c.property`; the disable branch (`EnableNotifications(nil)`) returns early when `c.property == nil`. Through a value wrapper, the deferred disable runs on a fresh copy whose `property` is still nil — so `StopNotify` is never called, the D-Bus signal channel never closes, and the notification goroutine leaks (zombie notifications surface on a later connect).

### Rule 2 — Make stubs honor the backend's real return-value semantics

Stubs drift toward "convenient"; the real backend is often more hazardous. Diverging at the return-value level hides whole classes of adapter bugs. Confirmed in `gattc_linux.go` (lines 321-329): BlueZ `Read` does `copy(data, result); return len(result), nil` — it returns the **full attribute length**, not the bytes copied. The adapter sliced `buf[:n]` with that value, so a device returning more than the buffer size panics. A stub that returns `copy(...)` (always ≤ buffer length) guarantees the overflow path never runs in tests.

### Rule 3 — Harden the production path against the real contract

Once the stub mirrors reality, the missing guard becomes obvious — clamp the reported length before slicing:

```go
n, err := found.char.Read(buf)
if err != nil {
    return Event{}, mapLiveError(err)
}
if n > len(buf) {
    // Some backends report the full attribute length rather than the bytes
    // copied; clamp so a longer-than-buffer value cannot slice out of range.
    n = len(buf)
}
```

### Rule 4 — Add a regression test at the contract edge

Exercise the exact boundary the stub used to hide (an oversized read, a double-stop, a re-entrant teardown). If the stub *can't* express the boundary, fix the stub or record the gap explicitly.

### Rule 5 — Confirm contracts from the dependency source, not by assumption

Both bugs were invisible until the backend source was read directly. The module cache makes this cheap:

```bash
$(go env GOMODCACHE)/<module>@<version>/<file>.go
```

Before wrapping a backend, check each method for: pointer vs value receiver (and whether it mutates state), whether a returned count means bytes-copied or bytes-available, and whether teardown is idempotent and what precondition it checks.

## Why This Matters

The failure mode is silent at the test layer. The value-wrapper teardown leak only manifests on Linux hardware where the live D-Bus connection keeps the goroutine alive; the `buf[:n]` panic only fires when a real device returns an oversized value. In both cases the bug sat one character (value vs pointer) or one line (return semantics) off the production path, and the suite reported green. The delta between "stub passes" and "hardware crashes" was exactly the unread backend contract — so the seam, meant to *raise* confidence, was quietly lowering it.

In Go specifically, value vs pointer receiver is invisible at the call site and invisible through an interface: dispatch always succeeds. The mutation contract is only visible by reading the method definition.

## When to Apply

- **New wrapper type** around a stateful/IO backend — audit every method's receiver kind; store by pointer if any pointer-receiver method mutates state.
- **New hand-written stub/fake** — match the backend's return semantics, error preconditions, and teardown idempotency exactly, even when the simpler version is tempting.
- **New test on an existing stub** — add a case that violates the boundary the stub models; if it can't, fix the stub or note the gap.
- **Any dependency upgrade** — re-read the methods whose contracts you relied on; receiver kind and return semantics can change between versions.

## Examples

**A — value vs pointer storage (the teardown no-op):**

```go
// WRONG: value field; the pointer-receiver enable mutates a copy, disable no-ops.
type tinybleCharacteristic struct{ characteristic tinyble.DeviceCharacteristic }
for _, char := range chars {
    out = append(out, tinybleCharacteristic{characteristic: char})
}

// CORRECT: pointer field; enable and disable mutate the same instance.
type tinybleCharacteristic struct{ characteristic *tinyble.DeviceCharacteristic }
for i := range chars {
    out = append(out, tinybleCharacteristic{characteristic: &chars[i]}) // index the slice; don't take &loopvar
}
```

**B — stub return semantics (bytes-copied vs attribute length):**

```go
// WRONG: copy() is always <= len(buf), so the adapter's overflow clamp never runs.
func (c *stubCharacteristic) Read(buf []byte) (int, error) {
    if c.readErr != nil { return 0, c.readErr }
    return copy(buf, c.readData), nil
}

// CORRECT: mirror the real BlueZ contract (len of the value, not bytes copied).
func (c *stubCharacteristic) Read(buf []byte) (int, error) {
    if c.readErr != nil { return 0, c.readErr }
    // Backends may report the attribute length rather than the bytes copied into
    // the caller's buffer; honoring that contract exercises the adapter's clamp.
    copy(buf, c.readData)
    return len(c.readData), nil
}
```

With the honest stub, a `readData` longer than `bleCharacteristicMaxValueBytes` drives `n > len(buf)` and the Rule 3 clamp executes — the regression test then asserts no panic.

## Related

- [`docs/solutions/design-patterns/dry-run-default-for-mutator-probes-in-test-harnesses-2026-05-05.md`](dry-run-default-for-mutator-probes-in-test-harnesses-2026-05-05.md) — closest peer: the test path must stay contract-faithful to the real invocation path, or a green test hides a real-path failure. Same root cause, different surface (HTTP method routing vs receiver/return semantics).
- [`docs/solutions/best-practices/multi-source-api-discovery-design-2026-03-30.md`](../best-practices/multi-source-api-discovery-design-2026-03-30.md) — the HTTP analogue of this seam: a constructor-injectable interface backed by `httptest` in tests.
- `docs/PATTERNS.md` "Device-Native Discovery" names the BLE adapter seam architecturally but says nothing about keeping the seam contract-faithful; this doc is the "how to keep it honest" elaboration and is a good cross-link target.
- GitHub issue #2587 (`feat: add BLE device-sniff foundation`) — the live-adapter work this learning came out of.
