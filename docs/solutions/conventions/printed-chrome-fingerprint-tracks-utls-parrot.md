---
title: "Printed chrome fingerprint tracks the uTLS parrot, not live Chrome"
date: 2026-09-16
category: conventions
module: cli-printing-press-generator
problem_type: convention
component: tooling
severity: medium
applies_when:
  - "Tempted to bump chromeMajor, sec-ch-ua, or the Chrome User-Agent because live Chrome is newer"
  - "Changing HelloChrome_Auto, the printed utls require, or chrome_profile.go.tmpl"
  - "A chrome-family print starts getting bot-wall 403s and the first idea is a newer User-Agent"
related_components:
  - generator
  - chrome-transport
  - utls
tags:
  - chrome
  - utls
  - fingerprint
  - browser-chrome
  - http-transport
---

# Printed chrome fingerprint tracks the uTLS parrot, not live Chrome

## Context

Printed `browser-chrome*` CLIs no longer import Surf. They emit a frozen HTTP identity in `internal/generator/templates/chrome_profile.go.tmpl` and a TLS ClientHello via `utls.HelloChrome_Auto`. Those are two different version clocks.

- **HTTP headers** (`chromeMajor`, User-Agent, `sec-ch-ua` including the GREASE brand) are generator literals. They were copied from Surf's `Impersonate().Chrome()` / `JA().Chrome145()` snapshot, which is why `chromeMajor` is 145.
- **TLS** is whatever `HelloChrome_Auto` aliases in the printed `utls` version (`go.mod.tmpl`). On `utls v1.8.2` (and the later `v1.8.3-0.20260301010127` pseudo-version the machine itself uses) that alias is `HelloChrome_133`. There is no `HelloChrome_145` in tagged uTLS.
- **Live Chrome** moves on a four-week stable cadence (153 on 2026-09-15). It is not a pin the generator can emit.

A reader who sees `chromeMajor = "145"` next to current Chrome will want to raise it. That is the wrong move with the current parrot.

## Guidance

The advertised Chrome major must not exceed the ClientHello we actually send. Bump identity only as a set:

1. Read the printed `utls` require in `go.mod.tmpl`.
2. Resolve `HelloChrome_Auto` in that module (`u_common.go`: `HelloChrome_Auto = HelloChrome_<N>`).
3. Set `chromeMajor`, User-Agent, and the GREASE `sec-ch-ua` brand together to that same major (or leave the Surf-era 145 snapshot if you are not doing a coordinated bump).
4. If you need a major newer than Auto, wait for a uTLS parrot that emits it, then bump the require and the literals in one change.

Do not raise headers to 148, 152, or 153 while Auto is still 133. Chrome rotates the GREASE brand every major. A 152 UA with a 133 ClientHello and a 145 GREASE string is a sharper mismatch than a coherent stale 145 profile.

If a WAF starts correlating UA major with ClientHello and 145 headers fail where a matched pair would pass, the coherent direction is **down to 133**, not up.

The impersonation-off path in `client.go.tmpl` / `auth_browser.go.tmpl` still hardcodes a browser-shaped `Chrome/148` User-Agent. That string is independent of `chromeMajor`. Do not "align" it to 145 or to live Chrome as a drive-by.

## Why This Matters

Bot management scores TLS (JA3/JA4) harder than a slightly old UA. Cloudflare-style gates that already required `browser-chrome` mostly care that the ClientHello looks like Chrome. A WAF that also correlates `sec-ch-ua` major, GREASE brand, and ClientHello (Akamai, DataDome, some bot-fight rules) can score a split identity. Raising only the UA makes that split worse.

uTLS issue [refraction-networking/utls#397](https://github.com/refraction-networking/utls/issues/397) records that Chrome 141+ *can* send the `trust_anchors` (`0xca34`) extension, which `HelloChrome_133` does not. Production Windows/macOS enablement is finch-gated and not a reason to invent a 141+ parrot in this repo. It is a reason not to claim those majors in headers until uTLS does.

## When to Apply

- Editing `chrome_profile.go.tmpl`, the printed `utls` pin, or chrome header tests that assert `Chrome/<major>`.
- A live chrome-family 403 that "Surf did not get" and the proposed fix is a newer User-Agent.
- Dependabot or a go.mod bump of `refraction-networking/utls` that might move `HelloChrome_Auto`. After such a bump, re-read the Auto alias and decide whether `chromeMajor` should follow.

Do not apply this to the machine reachability probe (`internal/reachability/probe.go`), which still uses Surf.

## Related

- `internal/generator/templates/chrome_profile.go.tmpl`, `chrome.go.tmpl`, `go.mod.tmpl`
- `internal/generator/templates/client.go.tmpl` impersonation-off `Chrome/148` default
- Printed `utls v1.8.2` `HelloChrome_Auto` = `HelloChrome_133`
- Originating Surf-removal work: cli-printing-press#3982, PR #4716
