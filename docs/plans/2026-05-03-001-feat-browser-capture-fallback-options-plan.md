---
title: "feat: Browser-capture fallback options (chrome-MCP, computer-use) for browser-sniff"
type: feat
status: active
date: 2026-05-03
---

# feat: Browser-capture fallback options for browser-sniff

## Summary

Extend the `printing-press` browser-sniff capture workflow so that when the primary backends (`browser-use`, `agent-browser`) get blocked or fail, the skill detects and offers two additional capture options the runtime already has access to: the Claude chrome-MCP (`mcp__claude-in-chrome__*`) and computer-use (`mcp__computer-use__*`). The fix lives in three files: the shared `browser-sniff-capture.md` reference (capture playbook, recovery menu, manual-HAR refresh), `printing-press/SKILL.md` (Phase 1.7 disclosure + Banned Skip Reasons entry), and `printing-press-polish/SKILL.md` (one-paragraph routing pointer). No Go code changes, no new artifact format.

---

## Problem Frame

The `airbnb-vrbo` printing-press session on 2026-05-02 surfaced a recurring failure mode. When `agent-browser` got hard-blocked by Akamai on VRBO, the skill's recovery menu (Step 2c.5 in `skills/printing-press/references/browser-sniff-capture.md`) offered exactly three options: try cleared-browser again (which would hit the same block), provide a HAR manually from DevTools, or pivot scope. The first option was useless against a hard block. The second option failed because Chrome 147's DevTools moved the HAR-export button and the text-only instructions sent the user through three dead-end menus before they gave up. The third option meant abandoning the killer feature.

The skill never offered the Claude chrome-MCP or computer-use, even though both were loaded into the same runtime. The user had to manually inject "you have MCP chrome extension you can use my computer" to redirect the agent. Once redirected, chrome-MCP captured both Airbnb listing-detail SSR shape and VRBO SSR rendering in one session — exactly the data the failed `agent-browser` run was supposed to produce.

The gap is that the skill's tool-detection block (Step 1) and recovery menu (Step 2c.5) hard-code awareness of `browser-use` and `agent-browser` only. Adding two more capture backends as first-class options closes the loop without changing the underlying capture-artifact contract or the marker-file gate.

---

## Requirements

- R1. The skill detects whether `mcp__claude-in-chrome__*` tools are loadable in the current runtime (deferred-tool list or already-loaded), and sets a `CHROME_MCP_AVAILABLE` flag.
- R2. The skill detects whether `mcp__computer-use__*` tools are loadable, and sets a `COMPUTER_USE_AVAILABLE` flag.
- R3. When the primary capture backend (`browser-use` or `agent-browser`) fails by the criteria already documented in Step 2c.5 (challenge-only HTML, hard block, login-redirect loop, ≥3 consecutive 429s), the recovery menu surfaces chrome-MCP as an option when `CHROME_MCP_AVAILABLE=true`.
- R4. The recovery menu surfaces computer-use as a visual-aid for the manual-HAR option when `COMPUTER_USE_AVAILABLE=true`. Computer-use is NOT offered as a peer browser-driver — its tier-"read" restriction on browsers makes it unable to click or type into Chrome.
- R5. A new chrome-MCP capture playbook section in `browser-sniff-capture.md` describes how to drive the chrome-MCP for capture: tabs context → navigate → interact → read page text → read network requests, with output written to the same `$DISCOVERY_DIR/browser-sniff-capture.json` enriched-capture path the existing pipeline already consumes.
- R6. The chrome-MCP backend inherits the existing adaptive-pacing posture (start at 1s, double on 429, pause 30s after 3 consecutive 429s) so the WAF that blocked the primary backend doesn't get hammered by the fallback.
- R7. The manual-HAR instructions in Step 1d and Step 2c.5 are refreshed for Chrome 147+ DevTools layout (the download-arrow icon at the top of the Network panel, since "Save all as HAR with content" was removed from the right-click menu).
- R8. The Phase 1.7 marker file (`browser-browser-sniff-gate.json`) records which backend was actually used in its existing free-form `reason` field — no schema change.
- R9. The `browser-sniff-capture.md` cardinal rules section (rule 1) is updated to name chrome-MCP and computer-use as valid fallbacks alongside the existing `browser-use` / `agent-browser` / manual-HAR set.
- R10. SKILL.md Phase 1.7 "Banned skip reasons" gains an entry that prevents silent skip-to-pivot when chrome-MCP is available and the previous backend failed against an anti-bot gate. The gate is "ask before giving up," not "auto-pivot when blocked."
- R11. The polish skill (`skills/printing-press-polish/SKILL.md`) gains a brief pointer in its Phase 1 baseline section noting that if discovery rework requires browser capture, the chrome-MCP and computer-use are available via the shared reference. No duplication of the capture playbook.

(R12 was a non-regression invariant; moved to System-Wide Impact > Unchanged invariants for clarity.)

---

## Scope Boundaries

- Not implementing chrome-MCP or computer-use as a runtime transport for the printed CLI. Browser-sniff is generation-time discovery only; the cardinal rule against resident-browser sidecar transport is unchanged.
- Not refactoring the existing `browser-use` or `agent-browser` Step 2a / Step 2b capture flows. Additive only.
- Not adding a new artifact format. chrome-MCP writes to the existing `$DISCOVERY_DIR/browser-sniff-capture.json` enriched-capture shape so `printing-press browser-sniff --har <path>` (which accepts both HAR and enriched-JSON) consumes it unchanged.
- Not building anti-bot prediction. The new menu fires on observed failure, not predicted failure. The existing `probe-reachability` classification stays the gatekeeper for transport-tier selection.
- Not auto-retrying a backend that just failed. If `agent-browser` got blocked, the menu offers different backends; it does not silently re-run `agent-browser`.
- Not auth-failure or network-failure handling. Those have separate paths in Step 1d and stay unchanged.
- Not extending tool detection to other speculative MCPs (`browse`, `gstack`, etc.) — only the two the user named and the runtime makes available today.

### Deferred to Follow-Up Work

- A `/ce-compound` writeup of the WAF-fallback escalation pattern + MCP-availability detection convention. The `learnings-researcher` pass confirmed no prior docs exist on either; this plan creates the precedent worth capturing back. Separate task after the plan ships.

---

## Context & Research

### Relevant Code and Patterns

- `skills/printing-press/references/browser-sniff-capture.md` — the shared capture reference. Step 1 (lines 56-80) does tool detection. Step 1b (lines 82-125) is the install-picker `AskUserQuestion`. Step 2c.5 (lines 703-723) is the failure-recovery menu the user hit. All edits land here.
- `skills/printing-press/SKILL.md` — Phase 1.7 (lines 781-1012). The marker file contract (lines 789-820) and the "Banned skip reasons" list (lines 822-834). Disclosure language about which backends the skill will offer lives in the AskUserQuestion blocks at lines 949-953 and 957-970.
- `skills/printing-press-polish/SKILL.md` — Phase 1 baseline diagnostics (line 192). One pointer line gets added; no playbook duplication.
- `internal/cli/browser_sniff.go` (lines 17-44) — the Cobra `browser-sniff` command. Inputs are `--har` or `--auth-from`. Accepts both HAR files and the enriched-capture JSON shape. The new chrome-MCP backend produces the enriched-JSON shape; this Go code is untouched.
- `internal/browsersniff/parser.go`, `fixtures.go` — HAR + enriched-capture parsers. Untouched.
- AskUserQuestion shape precedent: 3-option menus with "(Recommended)" badge on option 1, manual-HAR always last. Step 1b (lines 86-91), Step 1d Chrome-running (lines 192-196), Step 2c.5 (lines 715-719). The recovery menu may need to grow to 4 options when both chrome-MCP and computer-use are available; this is permitted by the AskUserQuestion 4-option cap.

### Institutional Learnings

- `docs/solutions/best-practices/sniff-and-crowd-sniff-complementary-discovery-2026-03-30.md` — discovery sources are complementary, not replacing. New backends should slot into the same "more sources = better coverage" mental model and produce the same artifact shape so the existing `mergeSpecs()` collision-handling works unchanged.
- `docs/solutions/best-practices/adaptive-rate-limiting-sniffed-apis.md` — sites guarded by Akamai/DataDome 429 aggressively. The new chrome-MCP backend must inherit the existing pacing posture so the WAF that blocked the primary doesn't get hit at full speed by the fallback. R6 carries this forward.
- `docs/solutions/best-practices/multi-source-api-discovery-design-2026-03-30.md` — each source is optional and independent; failure of one (missing tool, denied access) yields warn-then-empty, never aborts siblings. Maps directly to "chrome-MCP not loaded" and "computer-use access not granted" cases — both should warn and continue, not block the whole capture.
- `docs/solutions/security-issues/filepath-join-traversal-with-user-input-2026-03-29.md` — tangential. Only relevant if the new playbook accepts user-supplied paths. The existing `$DISCOVERY_DIR` convention sidesteps this; flagged only if the chrome-MCP playbook ends up taking a user path arg.

### Prior Plan Precedent

The five prior browser-sniff plans (`2026-04-02-001-feat-browser-auth-cookie-runtime`, `2026-04-03-001-fix-sniff-auth-graphql-naming`, `2026-04-11-001-fix-sniff-gate-enforcement`, `2026-04-18-002-refactor-rename-sniff-to-browser-sniff`, `2026-04-21-001-feat-browser-sniff-traffic-analysis`) all extend the capture flow by inserting new Step subsections (`Step 2a.1.5`, `Step 2a.2.5`, etc.) inside the existing structure rather than restructuring it. This plan follows the same precedent: a new `Step 2e` for chrome-MCP, an inline expansion of Step 2c.5 for the recovery menu, and a refresh of Step 1 for detection.

---

## Key Technical Decisions

- **Tier roles:** `browser-use` stays primary, `agent-browser` stays secondary. chrome-MCP is a third-tier fallback that enters only on failure recovery or when the user picks it explicitly from the install/initial menu. Computer-use is positioned as a visual-aid for manual HAR, not as a peer browser-driver. Rationale: chrome-MCP requires the user's existing browser session (good for auth-gated flows but tied to one tab at a time); computer-use cannot drive browsers due to its tier-"read" restriction on browser apps; the existing tools handle the unblocked-site case better.
- **Detection mechanism:** check the runtime's deferred-tool list for substring matches on `mcp__claude-in-chrome__` and `mcp__computer-use__`. Loading the schemas via `ToolSearch` is deferred until the user actually picks the option, so detection is cheap. Falls back gracefully on platforms where the deferred-tool list is not exposed (treat as "not available").
- **Artifact contract preserved:** chrome-MCP writes the enriched-capture JSON shape (`$DISCOVERY_DIR/browser-sniff-capture.json`) that `internal/browsersniff` already parses. Computer-use does not produce a primary artifact — it augments manual-HAR with screenshots saved to `$DISCOVERY_DIR/devtools-help-*.png` for the user's reference, plus on-screen verification of DevTools state.
- **Marker file unchanged:** the `browser-browser-sniff-gate.json` marker keeps its current schema. The `reason` field (already free-form) records which backend was used (`reason: "chrome-mcp captured 142 requests; SSR enriched"`), giving Phase 1.5 and downstream readers the audit trail without a schema migration.
- **Pacing posture:** chrome-MCP inherits the same 1s→0.3s ramp-down, double-on-429, 30s-pause-after-3-consecutive-429s rules already documented in Step 1b "Browser-Sniff Pacing." The playbook references the existing rules rather than restating them.
- **Menu shape on recovery:** Step 2c.5's 3-option menu grows to up to 5 options when both chrome-MCP and computer-use are available (try-cleared-browser, try chrome-MCP, manual-HAR, manual-HAR-with-computer-use-screenshots, alternate scope). Per the AskUserQuestion 4-option cap, the implementation collapses computer-use into the manual-HAR option as a sub-flow ("manual HAR — I'll guide you with screenshots") rather than a separate top-level option, keeping the menu at 4 options max.
- **Default-flow preservation:** when no failure occurs and both default backends are available and working, the new options never surface. Defaults stay defaults. The new options enter only on (a) Step 2c.5 failure recovery, or (b) Step 1b install-picker as opt-in choices when the user wants them up-front.

---

## Open Questions

### Resolved During Planning

- Should computer-use be a peer browser-driver to chrome-MCP? Resolved: no — computer-use's tier-"read" restriction on browsers blocks clicks/typing. Computer-use is positioned as a visual-aid for manual-HAR (screenshot verification, step-by-step guidance with `screenshot` tool) instead.
- Does this fix also need polish-skill duplication? Resolved: no — polish does not call browser-sniff today (`grep` confirmed zero references). One pointer line in polish's baseline section is enough so future polish runs that need capture know where the playbook lives.
- Does the marker file schema need migration? Resolved: no — the existing `reason` free-form field absorbs the new content.

### Deferred to Implementation

- Exact wording and option labels for the expanded Step 2c.5 menu. The implementer should follow the existing label conventions (3-5 word bold labels, "Recommended" badge on option 1 only when one is unambiguously preferred, manual-HAR last) and pick wording at edit time.
- Whether the chrome-MCP playbook should explicitly list common interaction patterns (`tabs_context_mcp` first, `navigate`, `read_page`, `read_network_requests`, `javascript_tool` for `__APOLLO_STATE__`-style introspection) or stay at a higher level pointing readers at the MCP tool index. Implementer to pick during the edit based on how dense the existing Step 2a playbook is.
- Whether DevTools-instruction screenshots (computer-use augmentation) should be saved as a static set in `assets/` or generated per-session. Static is simpler; per-session reflects the user's actual Chrome version. Defer to implementation.

### Deferred from doc review (2026-05-03)

These items surfaced in document review but require their own design discussion or scope decision before this plan ships. None are blockers for U1-U6 as currently scoped, but each warrants resolution before implementation begins.

- **U6 polish pointer is too thin to fix the observed improvisation failure** (scope-guardian + adversarial). The pointer is found only by an agent that already concluded re-discovery is needed — not the failure mode the user experienced. Decide: keep the lightweight pointer (current), add a Phase 1 trigger in polish that fires the browser-capture playbook when sparse-capture or extraction-broken signals appear, or some hybrid. May warrant a follow-up plan rather than scope expansion of U6.
- **Banned Skip Reasons is enforcement by prose only** (adversarial). The existing 5 banned reasons already failed to prevent the silent pivot in the user's session — adding a 6th treats the symptom, not the failure mode. Decide: keep U5 as prose enforcement (current), extend the marker-file gate with a `recovery_menu_shown` field that Phase 1.5 enforces, or some hybrid. The marker-file option also needs a small Go change to Phase 1.5.
- **Menu re-fire state machine for failed backends unspecified** (design-lens). U3 edge case 4 introduces a new pattern (re-fire with chrome-MCP option removed) but doesn't say where the tried-backends state lives, whether the re-fired menu is the same Step 2c.5 or a distinct state, or how it interacts with the existing two-consecutive-failure logic. Decide: session-scoped (agent reasons about what it just tried) vs run-scoped (marker file tracks tried-and-failed across runs).
- **Flag-11 silently drops plain manual-HAR option** (design-lens). When both flags are true, manual-HAR-without-screenshots is replaced by manual-HAR-with-screenshots. A user who doesn't want computer-use watching their screen has no escape. Decide: forced substitution (current implication), opt-in within the manual-HAR option (sub-question or "screenshots: yes/no" suffix), or two top-level options collapsed elsewhere.
- **chrome-MCP consent surface omitted from Phase 1.7 disclosure analysis** (design-lens). chrome-MCP accesses the user's authenticated Chrome session — meaningfully different consent than browser-use's profile load. Decide: Phase 1.7 pre-approval with rich disclosure, per-invocation re-consent at Step 2c.5 menu, or explicit enumeration in disclosure language.
- **chrome-MCP-also-blocked recovery has no real exit** (adversarial). Plan acknowledges the WAF could block chrome-MCP but doesn't specify failure-detection criteria for chrome-MCP — `read_network_requests` returning 142 challenge-page entries would be misrecorded as success. Decide: HTTP status pattern matching, response body markers (`cf_chl_opt`, `Just a moment`, Akamai sensors), Apollo/SSR introspection failures, or some combination; and at what threshold to re-fire the recovery menu vs accept partial capture.
- **Question stem teaching for first-time chrome-MCP mechanic unresolved** (design-lens). Step 2c.5 fires when the user is mid-failure and has never seen chrome-MCP-as-fallback. Decide: stem includes a one-line explainer when chrome-MCP is in the menu, stem stays generic and option body carries the explainer, or agent decides at edit time based on session context.
- **Computer-use only path (flag 01) complexity unjustified** (scope-guardian). Conditional menu composition for the 01 state delivers thin uplift (screenshot verification only). Decide: keep the 01 specialization (current), drop it (treat as flag 00 when chrome-MCP unavailable, reducing 4 conditional states to 3), or keep with stronger justification.

---

## Implementation Units

**Recommended implementation order:** U1 → U2 → U3 → U4 → U5 → U6. The unit dependencies encode the partial order, but U3 + U4 both edit Step 2c.5 of `browser-sniff-capture.md` and U5 references the new Step 2e from U2 — sequential traversal in the listed order avoids edit conflicts on shared sections.

- U1. **Detect chrome-MCP and computer-use availability**

**Goal:** Extend the tool-detection block in `browser-sniff-capture.md` Step 1 so the skill knows whether the new backends are reachable, and sets flags the downstream menus consume.

**Requirements:** R1, R2

**Dependencies:** None

**Files:**
- Modify: `skills/printing-press/references/browser-sniff-capture.md` (Step 1: Detect capture tools, lines 56-80)

**Approach:**
- Detection is agent-prose, not shell-probe. The existing `command -v browser-use` / `command -v agent-browser` blocks remain (those are real binaries on PATH). For MCP tools, append a prose block to Step 1 instructing the agent to inspect its own tools list (visible in system reminders and the deferred-tool block) and assert flags inline in its reasoning: "If `mcp__claude-in-chrome__*` tools appear in your available or deferred tool list, set `CHROME_MCP_AVAILABLE=true`. If `mcp__computer-use__*` tools appear, set `COMPUTER_USE_AVAILABLE=true`. Otherwise both default to false." The flags live in the agent's reasoning across the same conversation turn, not as shell env vars — downstream menus read them from the same context.
- Extend the "Using **<tool>**..." status report to additionally mention which fallback MCPs the agent detected (e.g., "Fallbacks available: chrome-MCP, computer-use")
- The probe is intentionally cheap — actual schema loading via `ToolSearch` is deferred until the user picks the option
- Falls back gracefully on platforms where the deferred-tool list is not exposed (the agent observes neither MCP, both flags default to false, downstream behavior unchanged from today's)

**Patterns to follow:**
- Existing detection block at Step 1 lines 56-80 (BROWSER_USE_HAS_LLM env-var probe is the closest analogue)
- AGENTS.md "Default to machine changes" — the detection helper is conditional and additive

**Test scenarios:**
- Happy path: both MCPs available -> `CHROME_MCP_AVAILABLE=true`, `COMPUTER_USE_AVAILABLE=true`, status line names both
- Edge case: chrome-MCP available, computer-use absent -> only chrome-MCP flag is true
- Edge case: neither MCP available -> both flags false, status line unchanged from today
- Edge case: runtime does not expose deferred-tool list (non-Claude-Code platforms) -> both flags default to false; existing flow unaffected
- Integration: detection runs even when user has not yet approved any backend, so Step 2c.5 can render the right menu later

**Verification:**
- Re-running the user's `airbnb-vrbo` session shows the status line listing chrome-MCP as a detected fallback before any backend is chosen

---

- U2. **Add chrome-MCP capture playbook (new Step 2e)**

**Goal:** Document how to drive the chrome-MCP for capture, with output written to the existing enriched-capture JSON path so the downstream pipeline consumes it unchanged.

**Requirements:** R5, R6

**Dependencies:** U1

**Files:**
- Modify: `skills/printing-press/references/browser-sniff-capture.md` (insert new section after Step 2b, before Step 2c)

**Approach:**
- New `#### Step 2e: Claude chrome-MCP capture (failure-recovery fallback)` section
- **Tab scope rule (mandatory):** always create a fresh tab via `tabs_create_mcp` for capture, even if a tab matching the target site appears in `tabs_context_mcp`. Close the capture tab when done. Never `navigate` an existing tab — that could redirect work the user has open. The `tabs_context_mcp` call is for awareness only, not for tab selection.
- **Body-capture step (mandatory before navigation):** chrome-MCP's `read_network_requests` returns request metadata only — no response bodies. Before navigation, install a `fetch`/`XHR` interceptor via `javascript_tool` so response bodies are captured at original-request time. Mirrors the agent-browser enrichment pattern at lines 672-682 of the same file. The interceptor approach (vs re-fetching after the fact) avoids re-firing requests against a WAF that's already wary of the session.
- Order of MCP calls: `tabs_context_mcp` (awareness) → `tabs_create_mcp` (fresh capture tab) → `javascript_tool` (install fetch/XHR interceptor) → `navigate` (open target) → interaction loop using `find` / `left_click` / `read_page` / `javascript_tool` → `read_network_requests` (request metadata) → merge with interceptor-captured response bodies → optional `get_page_text` for SSR shape → `tabs_close_mcp` (capture tab cleanup)
- **Write-time credential strip (mandatory):** before writing to `$DISCOVERY_DIR/browser-sniff-capture.json`, scrub `Authorization`, `Cookie`, `Set-Cookie`, and any header matching `/^x-.*-(token|key|auth|session)$/i` from each entry's `request_headers` and `response_headers`. Cross-reference `secret-protection.md` for the canonical scrub list so the rules stay in one place.
- **Artifact shape:** `EnrichedCapture` in `internal/browsersniff/types.go` (top-level: `target_url`, `captured_at`, `interaction_rounds`, `auth`, `entries[]`; per-entry: `method`, `url`, `request_headers`, `response_headers`, `request_body`, `response_body`, `response_status`, `response_content_type`, `classification`, `is_noise`). Write to `$DISCOVERY_DIR/browser-sniff-capture.json`.
- **Pacing scope:** when the playbook drives navigation loops or fires `javascript_tool`-initiated fetch/XHR requests, those operations inherit the existing adaptive-pacing posture (start at 1s, double on 429, 30s pause after 3 consecutive 429s) at lines 36-46 of the same file. `read_network_requests` is observational and does not pace.
- Cross-reference the existing replayability check at the cardinal-rules section
- Note the constraint that chrome-MCP requires a visible Chrome window with the extension installed; user-side setup link or `/pair-agent` reference if applicable
- Note the auth advantage: chrome-MCP uses the user's already-logged-in Chrome session by default — no cookie transfer step needed, unlike Step 1d

**Patterns to follow:**
- Existing Step 2a (browser-use) and Step 2b (agent-browser) section structure: capture commands → enrichment → output path → replayability check
- Adaptive-pacing reference, not restatement
- AGENTS.md prose-placeholder rule: use `<api>`, `<site>`, "the target site" — concrete names only in `Example:` paragraphs

**Test scenarios:**
- Happy path: agent invokes chrome-MCP capture against a non-blocked site, produces `browser-sniff-capture.json` with at least 5 unique endpoints, and `printing-press browser-sniff --har <path>` (which accepts the enriched-JSON shape) generates a spec without error
- Edge case: chrome-MCP returns empty network requests (page didn't fire XHRs) -> playbook instructs scroll/click before re-reading network requests
- Edge case: chrome-MCP captures a page behind a CAPTCHA -> the user's Chrome session has the cleared cookies; capture proceeds; replayability check still applies to the captured surface
- Error path: `tabs_context_mcp` returns no tabs -> playbook instructs creating one via `tabs_create_mcp`
- Error path: chrome-MCP extension not connected -> playbook instructs the connection prompt and falls through to the next menu option without aborting capture
- Integration: the captured artifact at `$DISCOVERY_DIR/browser-sniff-capture.json` flows through `internal/browsersniff/parser.go` ParseEnriched without modification

**Verification:**
- Manual test: drive the chrome-MCP playbook against `vrbo.com` (the site that hard-blocked `agent-browser` in the user's session), verify capture produces a usable enriched-JSON. The `vrbo.com` reference appears only in this plan's verification step, not in the resulting SKILL.md edit — Step 2e prose MUST use placeholders (`<api>`, `<site>`, "the target site") per AGENTS.md.

---

- U3. **Expand Step 2c.5 failure-recovery menu**

**Goal:** Replace the current 3-option recovery menu with an availability-aware menu that includes chrome-MCP when detected, and threads computer-use into the manual-HAR option as a visual-aid sub-flow.

**Requirements:** R3, R4

**Dependencies:** U1, U2 (the new Step 2e the menu links to)

**Files:**
- Modify: `skills/printing-press/references/browser-sniff-capture.md` (Step 2c.5: Challenge-only capture safety check, lines 703-723)

**Approach:**
- Replace the static 3-option `AskUserQuestion` block with an availability-aware variant per the composition table below.

**Menu composition table** (one row per flag combination):

| Chrome-MCP | Computer-use | Menu options (in order) |
|---|---|---|
| no | no | Try cleared-browser, Manual HAR, Alternate scope (3 options, current behavior unchanged) |
| no | yes | Try cleared-browser, Manual HAR with screenshot guidance, Alternate scope (3 options) |
| yes | no | Try cleared-browser, Try chrome-MCP (Recommended on anti-bot trigger), Manual HAR, Alternate scope (4 options) |
| yes | yes | Try cleared-browser, Try chrome-MCP (Recommended on anti-bot trigger), Manual HAR with screenshot guidance, Alternate scope (4 options) |

**Recommended badge rule (explicit):** When an anti-bot block triggered the recovery menu, chrome-MCP carries the Recommended badge whenever it is present in the menu, regardless of whether computer-use is also detected. In other failure modes (thin results, time-budget bailout), no option carries the Recommended badge — the user picks based on context.

**Patterns to follow:**
- Existing Step 2c.5 menu (lines 715-719) for label/body shape
- Existing AskUserQuestion 4-option cap convention
- "Recommended" badge convention (only when one option is unambiguously preferred for the situation)

**Test scenarios:**
- Happy path: anti-bot block detected, both flags true -> menu shows 4 options with chrome-MCP recommended; user picks chrome-MCP and Step 2e runs
- Happy path: anti-bot block detected, only chrome-MCP available -> menu shows 4 options (computer-use sub-flow absent), chrome-MCP recommended
- Happy path: anti-bot block detected, only computer-use available -> menu shows the existing 3 options with manual-HAR body augmented
- Edge case: anti-bot block detected, neither flag true -> menu identical to today's (zero behavior change, no new entries)
- Edge case: user picks chrome-MCP and chrome-MCP also fails -> menu re-fires with chrome-MCP option removed (cannot pick a backend that just failed); fall through to manual-HAR
- Integration: the chosen backend's reason ("chrome-mcp", "manual-har-with-computer-use-screenshots", etc.) is recorded in the marker file's `reason` field

**Verification:**
- Replaying the `airbnb-vrbo` session against the updated reference: the menu offers chrome-MCP as the recommended option after `agent-browser` is blocked, and the user can pick it without manual intervention

---

- U4. **Refresh manual-HAR instructions with optional computer-use visual aid**

**Goal:** Bring the DevTools HAR-export instructions up to Chrome 147+ layout (where "Save all as HAR with content" was removed from the right-click menu and the download-arrow icon is now the export path), and document the optional computer-use augmentation for visual verification.

**Requirements:** R4, R7

**Dependencies:** U1, U3 (U3 expands Step 2c.5; U4's manual-HAR body augmentation must layer on top of U3's expanded menu structure)

**Files:**
- Modify: `skills/printing-press/references/browser-sniff-capture.md` (Step 1d manual-HAR expansion at line 262, and the manual-HAR body in Step 2c.5 at line 717)

**Approach:**
- Update the DevTools click path: Network panel → top-left download-arrow icon (between the upload arrow and the record button) → Save dialog → save as `.har`
- Note the legacy right-click "Save all as HAR with content" path no longer exists in Chrome 147+; the download-arrow icon is the only stable path
- Add troubleshooting for common stuck states the user hit: wrong tab open, Recorder tab confused for Network tab, ">> overflow arrow" hiding the Network tab when DevTools is narrow
- When `COMPUTER_USE_AVAILABLE=true`, add a visual-feedback-loop flow (NOT silent verification — the agent must close the loop with the user):
  - **Before screenshotting:** instruct the user to collapse the Network panel detail pane (only the request list visible, not header/body detail). This reduces credential exposure surface in the captured PNG.
  - Use computer-use `screenshot` to capture the user's Chrome window at each instruction checkpoint
  - **After each screenshot:** the agent MUST display the image inline by Read'ing the PNG path AND describe what it sees in 1-2 sentences (e.g., "I see your DevTools is on the Recorder tab, not Network — click >> in the tab strip to switch"). The screenshot is part of the agent's reasoning + user-facing feedback, not a silent storage artifact.
  - Save screenshots to `$DISCOVERY_DIR/devtools-help-*.png` for the agent's display loop and the user's reference
  - **Phase 5.5 cleanup:** explicitly delete `$DISCOVERY_DIR/devtools-help-*.png` at archive time (PNGs are not archived). Computer-use augmentation produces ephemeral debug aids, not durable artifacts.
- Cardinal-rule call-out: computer-use cannot click or type into Chrome (tier-"read" on browsers); it is visual-feedback-only — the agent describes what it sees back to the user, who clicks themselves

**Patterns to follow:**
- Existing Step 1d Chrome-running and Chrome-not-running branches (lines 184-262) for sub-path structure
- Existing manual-HAR body in Step 2c.5 (line 717) for option-body wording

**Test scenarios:**
- Happy path: user follows refreshed instructions on Chrome 147, reaches the download-arrow icon on first attempt, exports HAR, returns the path
- Edge case: user is on Chrome 146 or older where the right-click "Save all as HAR with content" still exists -> instructions still work because the download-arrow icon predates the right-click removal
- Edge case: user is in dark-mode Chrome where the download-arrow icon contrast is reduced -> instructions name the icon position relative to the upload-arrow and record button
- Edge case: computer-use available but user is on a non-macOS platform -> screenshot fallback degrades gracefully (instructions remain text-only, no error)
- Integration: a HAR file produced via the refreshed flow flows through `internal/browsersniff/parser.go` LoadCapture without modification

**Verification:**
- Manual test on the user's Chrome 147 setup: user follows the refreshed text-only instructions, then re-runs with computer-use augmentation, verifies the export step succeeds in both modes

---

- U5. **Update SKILL.md Phase 1.7 disclosure language and Banned Skip Reasons**

**Goal:** Mention the new backends in the Phase 1.7 AskUserQuestion disclosure language so users know what they are agreeing to, and add a Banned Skip Reasons entry preventing silent pivot when chrome-MCP is available.

**Requirements:** R8, R9, R10

**Dependencies:** U1, U2, U3

**Files:**
- Modify: `skills/printing-press/SKILL.md` (Phase 1.7 section at lines 781-1012; specifically the AskUserQuestion disclosure block around lines 949-970 and the Banned Skip Reasons list at lines 822-834)
- Modify: `skills/printing-press/references/browser-sniff-capture.md` (cardinal rule 1 at line 12)

**Approach:**
- Cardinal rule 1 in `browser-sniff-capture.md`: extend the valid-fallbacks list to name chrome-MCP and computer-use alongside `browser-use` / `agent-browser` / manual-HAR
- Phase 1.7 AskUserQuestion disclosure (SKILL.md ~line 949-970): when listing what the skill might do during browser-sniff, add a sentence noting that the chrome-MCP and computer-use may be offered as fallbacks if available
- Add a new entry to the Banned Skip Reasons list (SKILL.md ~line 822-834): "**'The site is blocked by a WAF and our default browser-use/agent-browser hit a hard block'** — this is exactly when chrome-MCP and computer-use options enter. Do NOT pivot scope or fall back to RSS/docs without first asking the user via the recovery menu in Step 2c.5."
- Note the marker-file `reason` field convention: when a backend other than `browser-use` or `agent-browser` is chosen, the reason should name it explicitly (e.g., `reason: "chrome-mcp captured 142 requests; SSR enriched after Akamai block"`)

**Patterns to follow:**
- Existing Banned Skip Reasons format (SKILL.md lines 824-830): bold quote of the rationale, dash, explanation
- Existing disclosure-language convention in Phase 1.7 AskUserQuestion blocks
- AGENTS.md "Default to machine changes" — the disclosure update is additive, doesn't break existing menu shapes

**Test scenarios:**
- Happy path: anti-bot block fires, agent reads Banned Skip Reasons, sees the new entry, fires Step 2c.5 menu instead of pivoting
- Edge case: chrome-MCP unavailable -> Banned Skip Reasons new entry still applies (the menu still fires; it just has fewer options); pivot is still gated through the menu, not silent
- Integration: marker file written for a chrome-MCP-recovered run names "chrome-mcp" in the reason field; downstream readers (Phase 1.5 pre-flight, future audit) can identify which backend was actually used

**Verification:**
- Replay the `airbnb-vrbo` session: at the moment `agent-browser` is hard-blocked, the agent does NOT pivot to manual-HAR-only; instead it fires the expanded Step 2c.5 menu and the marker file's `reason` field names the chosen backend

---

- U6. **Add browser-capture pointer to polish skill**

**Goal:** When polish discovers it needs browser capture (rare but real, as in the user's airbnb-vrbo session), point the implementer at the shared playbook so the chrome-MCP and computer-use options are visible.

**Requirements:** R11

**Dependencies:** U2, U3 (the playbook and menu the pointer references)

**Files:**
- Modify: `skills/printing-press-polish/SKILL.md` (Phase 1 baseline diagnostics, around line 192)

**Approach:**
- One short paragraph or bullet noting that polish does not normally do browser capture, but if Phase 1 baseline reveals the underlying CLI needs re-discovery (broken extraction, sparse capture, etc.), the shared playbook at `skills/printing-press/references/browser-sniff-capture.md` covers all available capture backends including the chrome-MCP and computer-use
- No duplication of the playbook itself
- No new menus added to polish; this is a routing pointer only

**Patterns to follow:**
- Existing polish SKILL.md routing-pointer convention (e.g., references to `tools-polish.md`, `printing-press-output-review`)
- AGENTS.md skill-authoring note: cross-skill shared content stays in one reference, with pointers from consumers

**Test scenarios:**
- Happy path: polish run hits a discovery-rework need, reads the new pointer, navigates to the shared playbook, finds chrome-MCP option
- Edge case: polish run never needs browser capture -> pointer is invisible noise but not actively harmful
- Test expectation: none for this unit -- pure documentation pointer with no behavioral logic

**Verification:**
- Read the polish SKILL.md after the edit and confirm the pointer is discoverable from Phase 1 baseline without disrupting the existing baseline-diagnostics flow

---

## System-Wide Impact

- **Interaction graph:** the new chrome-MCP backend integrates with the runtime via the platform's deferred-tool loading mechanism (`ToolSearch`), shared by every Claude Code skill. No new global state.
- **Error propagation:** a missing or unavailable MCP yields warn-then-empty per the multi-source-discovery doctrine; never aborts the surrounding capture flow. Failure inside a chosen backend (e.g., chrome-MCP also gets blocked) re-fires the recovery menu with the failed option removed.
- **State lifecycle risks:** none. The new backend writes to the same `$DISCOVERY_DIR` path the existing pipeline already cleans up at run end. No new persistent state.
- **API surface parity:** the marker-file schema is unchanged. Phase 1.5's pre-flight read of `browser-browser-sniff-gate.json` works without modification. Downstream audit tooling that parses `reason` strings continues to work because `reason` was already free-form.
- **Integration coverage:** unit tests of `internal/browsersniff/parser.go` already cover the enriched-capture JSON shape that chrome-MCP will produce. No new Go tests required unless the playbook reveals a new edge case in the artifact shape.
- **Unchanged invariants:** browser-sniff remains generation-time only; resident-browser sidecar transport stays banned (cardinal rule 5). Phase 1.7 marker file remains a hard gate. Default backend ordering (`browser-use` primary, `agent-browser` secondary) is preserved. Existing `browser-use` and `agent-browser` flows are unchanged when no failure occurs — defaults stay defaults; the new options enter only on failure-recovery (Step 2c.5) or as opt-in choices in the install/initial menus (Step 1b). [formerly R12, moved here as a non-regression invariant]

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| chrome-MCP capture produces a different shape than `internal/browsersniff` parses, breaking Phase 2 silently | U2 verification step: drive the playbook end-to-end and confirm `printing-press browser-sniff --har <path>` accepts the artifact unchanged. If parsing fails, the playbook instructions get tightened to match the existing shape, NOT the parser changed (avoids touching Go) |
| Detection probe for `mcp__claude-in-chrome__*` returns false negatives on platforms where the deferred-tool list is not exposed (non-Claude-Code targets running the skill via plugin install) | Detection defaults to false on detection failure; behavior degrades to today's flow (no new options offered). Documented in U1 edge-case test |
| The 4-option AskUserQuestion cap is hit when both new flags are true and we want all options visible | Computer-use folds into the manual-HAR option as a sub-flow rather than a separate top-level option. U3 approach already accounts for this |
| User picks chrome-MCP and the Chrome extension is installed but disconnected | Playbook instructs the connection prompt; capture proceeds after connection. If user declines, the menu re-fires with chrome-MCP removed |
| WAF that hard-blocked agent-browser also blocks chrome-MCP (Akamai may detect Chrome-extension automation) | Adaptive pacing carries forward (R6); replayability check still applies; user can fall through to manual-HAR. The new option isn't a guaranteed fix; it just exposes another path the runtime supports today |
| Future Chrome version moves the DevTools download-arrow icon again | U4 troubleshooting section names common stuck states; the icon's position is described relative to fixed elements (record button, upload arrow). Computer-use augmentation provides visual verification when text instructions go stale |

---

## Documentation / Operational Notes

- README.md and ONBOARDING.md make no claims about which capture backends are supported. No update needed.
- CHANGELOG.md will get an entry on release per the standard `feat(skills):` line. release-please owns the version bump per `AGENTS.md` rules — do not manually bump.
- After this plan ships, run `/ce-compound` on the WAF-fallback escalation pattern + MCP-availability detection convention. The learnings-researcher pass confirmed no prior docs exist on either; this plan creates the precedent worth capturing back into `docs/solutions/`.
- No golden-output regeneration needed — pure SKILL.md and reference-file prose changes. `scripts/golden.sh verify` is for Go-side parser changes.

---

## Sources & References

- User session transcript (2026-05-02 to 2026-05-03): `airbnb-vrbo` printing-press run that surfaced the gap, including manual chrome-MCP intervention
- `skills/printing-press/references/browser-sniff-capture.md`
- `skills/printing-press/SKILL.md` (Phase 1.7)
- `skills/printing-press-polish/SKILL.md`
- `internal/cli/browser_sniff.go`, `internal/browsersniff/parser.go`
- `docs/solutions/best-practices/sniff-and-crowd-sniff-complementary-discovery-2026-03-30.md`
- `docs/solutions/best-practices/adaptive-rate-limiting-sniffed-apis.md`
- `docs/solutions/best-practices/multi-source-api-discovery-design-2026-03-30.md`
- Prior plan precedent: `docs/plans/2026-04-11-001-fix-sniff-gate-enforcement-plan.md`, `docs/plans/2026-04-03-001-fix-sniff-auth-graphql-naming-plan.md`, `docs/plans/2026-04-21-001-feat-browser-sniff-traffic-analysis-plan.md`
