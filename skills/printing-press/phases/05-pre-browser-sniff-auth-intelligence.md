## Phase 1.6: Pre-Browser-Sniff Auth Intelligence

**Receipt entry (required):**

```bash
"$PRINTING_PRESS_BIN" phase-receipt enter --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "05-pre-browser-sniff-auth-intelligence"
```

After [Phase 1](04-research-brief.md) research completes, analyze findings to proactively assess what auth context the user could provide. This step uses research intelligence to ask the right question before browser-sniffing starts, rather than waiting for the user to volunteer "I logged in."

**Skip this step if:** The briefing ([Orientation & Briefing](../SKILL.md#orientation--briefing) in the router) already captured auth context (`AUTH_CONTEXT` is set from the user selecting "I have an API key or I'm logged in").

**Classify the API's auth profile from research findings:**

| Signal from research | Auth profile | What to ask |
|---------------------|-------------|-------------|
| Community wrappers use API keys (e.g., `STRIPE_SECRET_KEY`), MCP source shows `Authorization: Bearer` headers, spec has `security` section | **API key auth** | "Do you have an API key for `<API>`?" |
| Site has user accounts, research found auth-only features (order history, saved items, rewards, account settings), login pages exist | **Browser session auth** | "This API has authenticated endpoints ([list specific features from research, e.g., order history, saved addresses, rewards]). Are you logged in to `<site>` in your browser? The browser-sniff will find more endpoints if you are." |
| Endpoints accessible without auth, no login-gated features found, community wrappers describe API as "no auth required" | **No auth needed** | Skip this step silently |
| Both API key AND browser session features found | **Dual auth** | Ask about both: API key for smoke testing, browser session for browser-sniff |

**Name the specific features the user would unlock.** Do not say "auth would help." Say "This API has order history, saved addresses, and rewards that require a logged-in session."

**Where signals come from:**
- [Phase 1](04-research-brief.md) brief's "Data profile" and "Top Workflows" sections
- [Phase 1.5a](08-ecosystem-absorb-gate.md) MCP source code analysis (auth patterns, token formats)
- Community wrapper README "auth" or "authentication" sections
- The API Key Gate's token detection ([Phase 0.5](03-resolve-and-reuse.md)) — if it already found a key, don't re-ask

**For API key auth:** Present via `AskUserQuestion`:
> "Do you have an API key for `<API>`? It will be used for read-only live smoke testing in Phase 5."
>
> 1. **Yes** — user provides the key or confirms it's in the environment
> 2. **No, continue without it** — skip live smoke testing

If the user provides a key, set it in `AUTH_CONTEXT` so the API Key Gate ([Phase 0.5](03-resolve-and-reuse.md)) does not re-ask.

**For browser session auth:** Present via `AskUserQuestion`:
> "`<API>` has authenticated endpoints ([list features]). Are you logged in to `<site>` in your browser? If so, the generated CLI will support `auth login --chrome` — you'll be able to authenticate just by being logged into the site in Chrome. No API key needed."
>
> 1. **Yes, I'm logged in** — I'll use your session during browser-sniff and enable browser auth in the CLI
> 2. **No, but I can log in** — I'll help you log in before browser-sniffing
> 3. **No, skip authenticated endpoints** — browser-sniff only public endpoints

Set `AUTH_SESSION_AVAILABLE=true` if the user selects option 1 or 2. The Browser-Sniff Gate ([Phase 1.7](06-browser-sniff-gate.md)) will use this flag. After traffic capture, Step 2d in [references/browser-sniff-capture.md](../references/browser-sniff-capture.md) validates that cookie replay works before enabling browser auth in the generated CLI.

**For dual auth:** Ask about both in sequence — API key first (simple env var check), then browser session.

---

Before following `Next:`, record the durable handoff. If this phase used its
allowed skip, add `--skip --note "<allowed reason>"`; never put auth values in
the note:

```bash
"$PRINTING_PRESS_BIN" phase-receipt complete --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "05-pre-browser-sniff-auth-intelligence"
```

Next: phases/06-browser-sniff-gate.md
