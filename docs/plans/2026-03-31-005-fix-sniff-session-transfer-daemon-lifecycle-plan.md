---
title: "fix: Sniff session transfer fails due to daemon lifecycle mismatch"
type: fix
status: completed
date: 2026-03-31
---

# Fix: Sniff Session Transfer Fails Due to Daemon Lifecycle Mismatch

## Overview

The printing-press skill's Step 1d (Session Transfer) tells Claude to run `agent-browser --auto-connect state save` followed by `agent-browser --state <file> open <url>`. This sequence fails because `--auto-connect` starts a daemon, and subsequent `--state` flags are silently ignored by the already-running daemon. The user's authenticated cookies never reach the sniff session.

## Problem Frame

When a user is logged in to a target site in Chrome and chooses "Grab session from Chrome," the skill should capture their cookies and use them for authenticated browsing during the sniff. Instead:

1. `agent-browser --auto-connect state save ./auth.json` succeeds -- starts a daemon connected to real Chrome, saves cookies to disk
2. `agent-browser --state ./auth.json open <url>` warns `--state ignored: daemon already running` and opens the URL without cookies
3. All subsequent browsing runs unauthenticated -- customer pages, order history, rewards endpoints are never triggered
4. The sniff captures only public traffic (analytics, CDN, a couple GraphQL ops)

The user was never prompted about the failure because the warning was a yellow `⚠` not a red `✗`, and the page still loaded (just without auth).

## Root Cause Analysis

Three independent bugs in SKILL.md Step 1d:

### Bug 1: Missing `agent-browser close` between state save and state load

The skill prescribes (lines 764-768):
```bash
agent-browser --auto-connect state save "$DISCOVERY_DIR/session-state.json"
# ... then ...
agent-browser --state "$DISCOVERY_DIR/session-state.json" open <url>
```

agent-browser's daemon model: `--auto-connect`, `--state`, `--profile`, and `--headed` are all **daemon launch options**. They only take effect when starting a new daemon. Once a daemon is running, these flags on subsequent commands are silently ignored (with a yellow warning).

The `--auto-connect` in the save command starts the daemon. The `--state` in the open command is ignored because the daemon is already running.

**Fix:** Insert `agent-browser close` between save and open, so the second command starts a fresh daemon with `--state`.

### Bug 2: `--headless` flag doesn't exist

Step 2b (line 896) prescribes:
```bash
agent-browser open <target-url> --headless
```

`--headless` is not a flag. agent-browser runs headless by default. The opposite flag is `--headed` (show browser window). When Claude executed this, it got `Unknown command: --headless`, which was noise that obscured the real problem.

**Fix:** Remove `--headless` from Step 2b. agent-browser is headless by default.

### Bug 3: No error checking after state load

The skill doesn't say to verify that `--state` actually took effect. The `⚠ --state ignored` warning went unnoticed. There's no instruction to check for this warning or to verify the session is authenticated before proceeding to browse.

**Fix:** After starting the daemon with `--state`, verify cookies loaded. Something like:
```bash
agent-browser cookies get --json | grep -q "dominos.com"
```
If no target-domain cookies are found, stop and tell the user the session transfer failed.

## Additional Design Issue: Auto-Connect Is Actually Better

There's an alternative approach the skill should consider: **stay in auto-connect mode for the entire sniff** instead of the save-then-restore pattern.

When `--auto-connect` is active, the daemon IS controlling the user's real Chrome. This means:
- All cookies are already present (no save/restore needed)
- Browsing happens in the user's actual browser (they can see it)
- No daemon lifecycle juggling required

The tradeoff: the user's Chrome tab changes as pages are browsed. For a 60-90 second sniff, this is probably acceptable. The skill should offer this as an option.

However, this only works if Chrome was launched with `--remote-debugging-port`. agent-browser's `--auto-connect` auto-discovers this, but if Chrome wasn't launched with it, auto-connect still somehow succeeded in our session (it may use a different discovery mechanism). This needs verification.

## Scope Boundaries

- This plan covers SKILL.md Step 1d (Session Transfer) and Step 2b line 896 (the `--headless` flag)
- Does NOT change the sniff browsing logic itself (Step 2a/2b page collection)
- Does NOT change browser-use codepaths (only agent-browser)
- Does NOT add new sniff capabilities -- just fixes the auth session actually being used

## Key Technical Decisions

- **Save-then-restore vs. stay-in-auto-connect:** The save-then-restore pattern is more correct (doesn't hijack the user's Chrome, runs headless). Fix it by adding `agent-browser close`. But also document the auto-connect alternative for users who prefer to see what's happening.
- **Error checking strategy:** Use `cookies get` to verify domain cookies are present after state load. This is the most reliable signal that auth transferred.
- **Prompting on failure:** If cookies aren't found, present an AskUserQuestion with fallback options (retry with headed login, try auto-connect mode, skip auth, provide HAR manually).

## Implementation Units

- [x] **Unit 1: Fix the daemon lifecycle in Step 1d**

  **Goal:** Insert `agent-browser close` between state save and state load so the second command starts a fresh daemon.

  **Files:**
  - Modify: `skills/printing-press/SKILL.md` (lines 762-769)

  **Approach:**
  Replace the current option 1 block:
  ```bash
  agent-browser --auto-connect state save "$DISCOVERY_DIR/session-state.json" 2>&1
  # ... use state file ...
  agent-browser --state "$DISCOVERY_DIR/session-state.json" open <url>
  ```
  With:
  ```bash
  # Grab cookies from running Chrome
  agent-browser --auto-connect state save "$DISCOVERY_DIR/session-state.json" 2>&1

  # Close the auto-connect daemon so --state can start a fresh one
  agent-browser close 2>&1

  # Start a new headless daemon with the saved auth state
  agent-browser --state "$DISCOVERY_DIR/session-state.json" open <url>
  ```

  Add a verification step after the open:
  ```bash
  # Verify cookies transferred
  if ! agent-browser cookies get --json 2>/dev/null | grep -q "<target-domain>"; then
    echo "WARNING: No <target-domain> cookies found. Session transfer may have failed."
    # Present fallback options via AskUserQuestion
  fi
  ```

  **Patterns to follow:** The headed login path (lines 798-810) already does close-then-restart correctly: "Close the headed browser and restart headless with the saved state."

  **Verification:** Run the full sequence against a site the user is logged into. Verify that `cookies get` shows target-domain cookies after the state load.

- [x] **Unit 2: Remove `--headless` flag from Step 2b**

  **Goal:** Fix the invalid flag that produces `Unknown command: --headless`.

  **Files:**
  - Modify: `skills/printing-press/SKILL.md` (line 896)

  **Approach:**
  Change:
  ```bash
  agent-browser open <target-url> --headless
  ```
  To:
  ```bash
  agent-browser open <target-url>
  ```
  Add a comment: `# agent-browser is headless by default; use --headed to show the browser window`

  **Verification:** `agent-browser open <url>` succeeds without error.

- [x] **Unit 3: Add auto-connect alternative for authenticated sniff**

  **Goal:** Give users the option to stay in auto-connect mode (browse in their real Chrome) instead of the save-then-restore pattern.

  **Files:**
  - Modify: `skills/printing-press/SKILL.md` (Step 1d, around line 755)

  **Approach:**
  Update the AskUserQuestion options when Chrome IS running:
  1. "Grab session from your Chrome" (save cookies, sniff in headless) -- the current option, now fixed
  2. "Sniff in your Chrome directly" (stay in auto-connect, browse in your real Chrome -- you'll see pages changing)
  3. "Log in within a new browser window"
  4. "I'll export a HAR file"

  For option 2, the flow is:
  ```bash
  agent-browser --auto-connect open <url>
  agent-browser network har start
  # ... browse pages ...
  agent-browser network har stop <path>
  # No close/restart needed -- daemon stays connected to real Chrome
  ```

  This is simpler and avoids the daemon lifecycle entirely. The tradeoff (user sees pages changing) should be stated explicitly.

  **Verification:** Auto-connect sniff captures authenticated API traffic that the save-then-restore pattern misses.

- [x] **Unit 4: Add cookie verification gate after any session transfer method**

  **Goal:** Detect and surface session transfer failures before wasting 60+ seconds on an unauthenticated sniff.

  **Files:**
  - Modify: `skills/printing-press/SKILL.md` (after Step 1d, before Step 2a/2b)

  **Approach:**
  After any session transfer method completes, add a verification gate:
  ```bash
  # Verify auth cookies are present
  COOKIES=$(agent-browser cookies get --json 2>/dev/null)
  if echo "$COOKIES" | grep -q "<target-domain>"; then
    echo "Session transfer verified — found <target-domain> cookies."
  else
    echo "WARNING: No <target-domain> cookies found."
    # AskUserQuestion with options:
    # 1. Try auto-connect mode instead
    # 2. Log in manually (headed browser)
    # 3. Continue without auth (public endpoints only)
    # 4. Provide HAR manually
  fi
  ```

  This catches not just the daemon lifecycle bug but any future session transfer failure mode.

  **Verification:** Intentionally break session transfer (e.g., wrong state file) and verify the gate catches it and prompts the user.

## System-Wide Impact

- **Interaction graph:** Step 1d feeds into Steps 2a/2b (capture flow). If auth cookies aren't present, every subsequent page browse runs unauthenticated. The cookie verification gate (Unit 4) is the circuit breaker.
- **Error propagation:** Currently, auth failures are silent (yellow warning, pages still load). After this fix, auth failures are surfaced with fallback options.
- **Other sniff backends:** browser-use has its own `--connect` and `--profile` flags for auth. Those codepaths are not affected by this fix, but could benefit from a similar cookie verification gate in the future.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| `agent-browser cookies get` may not support `--json` flag in all versions | Check version compatibility; fall back to parsing text output |
| Auto-connect mode may not work without `--remote-debugging-port` | Test auto-connect discovery; document the requirement if needed |
| Closing daemon between save/load may lose the CDP connection to Chrome | This is expected -- we're intentionally switching from auto-connect to headless mode |

## Sources & References

- agent-browser v0.23.0 `--help` output (full CLI reference)
- SKILL.md Step 1d (lines 741-814)
- SKILL.md Step 2b (lines 890-919)
- Runtime failure trace from the Domino's Pizza sniff session (2026-03-31)
