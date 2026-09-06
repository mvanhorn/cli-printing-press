## 03-resolve-and-reuse (Phase 0: Resolve And Reuse)

**Receipt entry (required):**

```bash
"$PRINTING_PRESS_BIN" phase-receipt enter --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "03-resolve-and-reuse"
```

Before new research:

1. Resolve the spec source.

   **Local physical device detection.** If the user's target is a local Bluetooth/BLE-controlled physical device (for example an appliance, toy, light, sensor, exercise machine, lock, or other device controlled from a phone app over Bluetooth), do not route it through browser-sniff as the primary discovery path. Read and apply [references/device-sniff-ble.md](../references/device-sniff-ble.md). Use `device-sniff ble` for normalized BLE evidence and `bluetooth-sniff` as the discoverable alias. Community libraries, docs, Android logs, Wireshark/nRF captures, and manual action journals are evidence inputs; they are not a reason to hardcode a vendor-specific generator path.

   **BLE mapping research gate.** A BLE scan/inspect/read/subscribe pass only discovers identity, services, characteristics, and telemetry candidates; it does not by itself discover what write payloads mean. Before generating callable control commands or running any live `write`, establish a command mapping from at least one concrete source: user-provided mapping, official docs, community protocol/library code, Android/iOS/Bluetooth logs, Wireshark/nRF captures, or an operator action journal that correlates a real user action with observed writes. If no mapping source is found, generate only read/status/capability metadata or stop and ask the user for mapping evidence. Do not invent mutating payloads or brute-force probe a physical device.

   **URL Detection** — If the argument contains `://`, it's a URL. Determine whether it's a spec or a website before proceeding.

   **Step 1: Content probe.** Fetch the URL with the raw docs helper from [references/fetch-docs.md](../references/fetch-docs.md) and inspect the response status, `Content-Type`, and first few lines of the returned file:
   - Check the `Content-Type` header and the first few lines of the body.
   - If the fetch fails (timeout, 404, DNS error), record the exact status/error, then skip to Step 2 — treat it as a website.

   If the content starts with `openapi:`, `swagger:`, or is valid JSON containing an `"openapi"` or `"swagger"` key → it's a spec. Treat as `--spec` and proceed directly. No disambiguation needed.

   If the content is a HAR file (JSON with `"log"` and `"entries"` keys) → treat as `--har` and proceed directly.

   **Step 2: Disambiguation.** If the content is HTML or the probe failed, ask the user what they want. Extract the site name from the hostname (e.g., `postman.com` → "Postman", `app.linear.app` → "Linear"). Derive `<api>` from the site name using the same `cleanSpecName` normalization the generator uses.

   Use `AskUserQuestion` with:
   - **question:** `"What kind of CLI do you want for <SiteName>?"`
   - **header:** `"CLI target"`
   - **multiSelect:** `false`
   - **options:**
     1. **label:** `"<SiteName>'s official API"` — **description:** `"Build a CLI for <SiteName>'s documented API (e.g. REST endpoints, webhooks, OAuth)"`
     2. **label:** `"The <SiteName> website itself"` — **description:** `"Build from the website itself — I may open or attach to Chrome during generation to capture site traffic, then generate a lightweight CLI from replayable HTTP/HTML surfaces"`

   The user can also pick the automatic "Other" option to describe what they're after in free text.

   **Routing after disambiguation:**
   - "<SiteName>'s official API" → use `<api>` as the argument, proceed with normal discovery ([Phase 1](04-research-brief.md) research, then [Phase 1.7](06-browser-sniff-gate.md) browser-sniff gate evaluates independently as usual)
   - "The <SiteName> website itself" → use `<api>` as the argument, set `BROWSER_SNIFF_TARGET_URL=<url>`. Proceed to [Phase 1](04-research-brief.md) research. When [Phase 1.7](06-browser-sniff-gate.md) is reached, skip the browser-sniff gate decision and go directly to "If user approves browser-sniff" (the user already approved temporary browser discovery in Phase 0 — do not re-ask). Use `BROWSER_SNIFF_TARGET_URL` as the starting URL for browser capture. The printed CLI must still use a replayable runtime surface; do not ship a resident browser transport.
   - "Other" → read the user's free-form response and adapt

   **End of URL detection.** The remaining spec resolution rules apply when the argument is NOT a URL:

   - If the user passed `--har <path>`, this is a HAR-first run. Before invoking browser-sniff, parse the file as JSON and verify it contains traffic: HAR files must have `.log.entries | length > 0`; enriched capture JSON must have `.entries | length > 0`. If the file is missing, invalid JSON, or has zero entries, do not continue silently. Print `HAR/capture contains no network entries; the export likely recorded no traffic.` and ask via `AskUserQuestion` with exactly these choices: **Capture again (Recommended)** — "Re-export a HAR after recording a real user flow"; **Proceed with docs-only** — "Skip HAR/browser-sniff and continue from docs/spec discovery only"; **HOLD** — "Stop this run until a valid capture is available." Only proceed to browser-sniff when the parsed entry count is non-zero.
   - For a valid `--har <path>`, run `cli-printing-press browser-sniff --har <path> --name <api> --output "$RESEARCH_DIR/<api>-browser-sniff-spec.yaml" --analysis-output "$DISCOVERY_DIR/traffic-analysis.json"` to generate a spec and traffic analysis from captured traffic. If `$API_RUN_DIR/source-priority.json` exists with two or more sources, add `--preserve-hosts` so combo-CLI captures retain peer API hosts with per-endpoint `base_url` overrides instead of collapsing them into secondary evidence. Immediately inspect `$DISCOVERY_DIR/traffic-analysis.json`: if it contains the `empty_response_shapes` warning, or if every endpoint cluster has `size_class: "empty"` and `response_shape: {}`, use curl/direct HTTP to call each discovered endpoint and capture response structure before writing or trusting the spec. Do not proceed to `generate` with a sniffed spec that has no type information. Use the generated spec as the primary spec source for the rest of the pipeline only after this quality check passes. Skip the browser-sniff gate in [Phase 1.7](06-browser-sniff-gate.md) (browser-sniff already ran).
   - If the user passed `--spec`, use it directly (existing behavior).
   - Otherwise, proceed with normal discovery (KnownSpecs, apis-guru, web search, and researched docs).

   #### Directory spec-source guard

   If any resolved spec source is a local directory, do not pass the directory
   itself to `cli-printing-press generate` and do not silently pick the first
   file. Enumerate candidate specs first:

   ```bash
   find "$SPEC_SOURCE_DIR" -type f \( -iname '*.json' -o -iname '*.yaml' -o -iname '*.yml' \) | sort
   ```

   Keep only files whose head looks like an OpenAPI or Swagger root document
   (`openapi:`, `swagger:`, or JSON with a top-level `"openapi"` or `"swagger"`
   key). Ignore unrelated JSON/YAML config files.

   When the filtered candidate list is empty, abort with:
   `No OpenAPI/Swagger spec found under <directory>. Pass --spec <file> directly.`
   Do not continue with the raw directory as the spec source.

   When the directory contains exactly one candidate, use that file as the
   spec source and write it to `state.json` as `spec_path`.

   When the directory contains more than one candidate:
   - Print a prominent warning before generation:
     `N OpenAPI/Swagger specs found under <directory>; no single file represents the whole API surface.`
   - List every candidate when `N <= 20`; otherwise list the first 20 sorted
     paths and print `...and N-20 more`.
   - Record the directory and candidates in `$STATE_FILE` before continuing:
     `spec_path` is the directory and `spec_candidates` is the sorted list.
   - Ask the user to choose one spec, several specs, or all specs. If this
     runtime cannot ask a blocking question, stop after printing the warning
     and tell the user to re-run with explicit `--spec <file>` arguments. This
     is the minimum safe floor: never let a directory run finish while hiding
     that additional specs were ignored.
   - After the user confirms the selection, update `$STATE_FILE` with
     `selected_spec_paths` set to the list that will be generated.
   - For multiple selected specs, default to one independent printed CLI per
     spec using a derived `<api>-<spec-slug>` name and a distinct working
     directory under `$API_RUN_DIR/working/`. Do not merge all selected specs
     into one CLI unless the user explicitly asks for a combined surface and
     provides the umbrella name for `--name`.

2. Check for prior research in:
   - `$PRESS_MANUSCRIPTS/<api-slug>/*/research/*`
3. Reuse good prior work instead of redoing it.
4. **Library Check** — Check if a CLI for this API already exists in the library or is actively being built, and present the user with context and options.

   First, check lock status to detect active builds:

   ```bash
   LOCK_STATUS=$("$PRINTING_PRESS_BIN" lock status --cli <api>-pp-cli --json 2>/dev/null)
   LOCK_HELD=$(echo "$LOCK_STATUS" | grep -o '"held"[[:space:]]*:[[:space:]]*[a-z]*' | head -1 | sed 's/.*: *//')
   LOCK_STALE=$(echo "$LOCK_STATUS" | grep -o '"stale"[[:space:]]*:[[:space:]]*[a-z]*' | head -1 | sed 's/.*: *//')
   LOCK_PHASE=$(echo "$LOCK_STATUS" | grep -o '"phase"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"phase"[[:space:]]*:[[:space:]]*"//;s/"//')
   LOCK_AGE=$(echo "$LOCK_STATUS" | grep -o '"age_seconds"[[:space:]]*:[[:space:]]*[0-9]*' | head -1 | sed 's/.*: *//')
   ```

   Then check the library directory:

   ```bash
   CLI_DIR="$PRESS_LIBRARY/<api>"
   HAS_LIBRARY=false
   HAS_GOMOD=false
   CLI_RELEASE_VERSION=""
   PRIOR_STEINBERGER_SCORE=""
   PRIOR_SUB60_REPRINT=false
   if [ -d "$CLI_DIR" ]; then
     HAS_LIBRARY=true
     if [ -f "$CLI_DIR/go.mod" ]; then
       HAS_GOMOD=true
     fi
     # Read manifest if available
     MANIFEST="$CLI_DIR/.printing-press.json"
     if [ -f "$MANIFEST" ]; then
       PRESS_VERSION=$(cat "$MANIFEST" | grep -o '"printing_press_version"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"printing_press_version"[[:space:]]*:[[:space:]]*"//;s/"//')
       GENERATED_AT=$(cat "$MANIFEST" | grep -o '"generated_at"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"generated_at"[[:space:]]*:[[:space:]]*"//;s/"//')
       PRIOR_STEINBERGER_SCORE=$(jq -r '.scorecard.steinberger.percentage // empty' "$MANIFEST" 2>/dev/null || true)
       if [ -n "$PRIOR_STEINBERGER_SCORE" ] && awk "BEGIN { exit !($PRIOR_STEINBERGER_SCORE < 60) }"; then
         PRIOR_SUB60_REPRINT=true
       fi
     fi
     RELEASE_MANIFEST="$CLI_DIR/.printing-press-release.json"
     if [ -f "$RELEASE_MANIFEST" ]; then
       CLI_RELEASE_VERSION=$(jq -r '.version // empty' "$RELEASE_MANIFEST" 2>/dev/null || true)
     fi
     # Get directory modification time as fallback
     CLI_MTIME=$(stat -f "%Sm" -t "%Y-%m-%d" "$CLI_DIR" 2>/dev/null || stat -c "%y" "$CLI_DIR" 2>/dev/null | cut -d' ' -f1)
   fi
   ```

   **Decision matrix:**

   | Library dir? | Lock? | Stale? | Has go.mod? | Action |
   |-------------|-------|--------|-------------|--------|
   | No | No | N/A | N/A | Proceed normally |
   | No | Yes | No | N/A | Warn: "Actively being built (phase: `<phase>`, `<age>` seconds ago). Wait, use a different name, or pick a different API." |
   | No | Yes | Yes | N/A | Offer reclaim: "Interrupted build detected (stale since `<age>`s ago). Reclaim and start fresh?" |
   | Yes | No | N/A | Yes | Existing "Found existing" flow (see below) |
   | Yes | No | N/A | No | Debris: "Found `<api>` directory in library but it appears incomplete (no go.mod). Clean up and start fresh?" If user approves, `rm -rf "$CLI_DIR"` and proceed normally. |
   | Yes | Yes | No | Any | Warn: "Actively being rebuilt (phase: `<phase>`, `<age>` seconds ago). Wait, use a different name, or pick a different API." |
   | Yes | Yes | Yes | Any | Offer reclaim: "Interrupted rebuild detected (stale since `<age>`s ago). Reclaim and start fresh?" |

   **If actively locked (not stale):** Present via `AskUserQuestion` with options to wait, pick a different API, or force-reclaim (`cli-printing-press lock acquire --cli <api>-pp-cli --scope "$PRESS_SCOPE" --force`).

   **If stale lock:** Reclaiming is automatic on `lock acquire` in [Phase 2](10-generate.md). If user approves, proceed normally — the lock acquire in [Phase 2](10-generate.md) will auto-reclaim the stale lock.

   **If library exists with go.mod and no lock (completed CLI):** Display context and present options using `AskUserQuestion`:

   > Found existing `<api>` in library (last modified `<date>`).

   If `PRESS_VERSION` is available, append: `Built with printing-press v<version>.`
   If `CLI_RELEASE_VERSION` is available, append: `Published CLI release: <version>.` This is the public-library per-CLI CalVer release, not the generator version; do not use it for the generator staleness comparison below.
   If `PRIOR_SUB60_REPRINT=true`, append: `Prior Steinberger score: <score>%. Reprint will require all approved transcendence rows to ship unless you explicitly accept partial coverage.`

   If prior research was also found (step 2), include the research summary alongside the library info.

   Then ask:
   1. **"Generate a fresh CLI"** — Re-runs the Printing Press into a working directory, overwrites generated code, then rebuilds transcendence features. Prior research is reused if recent.
   2. **"Improve existing CLI"** — Keeps all current code, audits for quality gaps, implements top improvements. The Printing Press is not re-run.
   3. **"Review prior research first"** — Show the full research brief and absorb manifest before deciding.

   If the user picks option 1, proceed to [Phase 1](04-research-brief.md) (research) and then [Phase 2](10-generate.md) (generate) as normal.
   If the user picks option 2, invoke `/printing-press-polish <api>` to improve the existing CLI.
   If the user picks option 3, display the prior research, then re-present options 1 and 2.

   **MANDATORY when re-using prior research after a binary upgrade.** If the user picks "Generate a fresh CLI" (option 1) AND `PRESS_VERSION` from the manifest differs from the current binary's version (parse both via semver and compare; only fire when the leading minor or major segment changed — patch-level deltas don't trigger this), prompt the user once before kicking off [Phase 1](04-research-brief.md) research.

   Construct the prompt's "what changed" list from these category buckets — the categories are stable across versions; the specific machine deltas inside each category are not. Read `docs/CHANGELOG.md` (or run `git log --oneline v<PRESS_VERSION>..v<CURRENT> -- internal/`) and tag each notable change to one of these buckets:

   | Category | Affects prior-brief assumption about... |
   |---|---|
   | **Transport / reachability** | Which sources are reachable, what auth/clearance is needed, which clients (stdlib, Surf, browser-clearance) the brief assumed |
   | **Scoring rubrics** | What [Phase 1.5](08-ecosystem-absorb-gate.md)/scorecard dimensions the brief targets, whether prior "high-priority" features still rank as such |
   | **Auth modes** | Whether brief's auth choice (api-key, cookie, composed, oauth) is still the right pick, whether new modes unlock new endpoints |
   | **MCP surface** | Whether brief's MCP shape (endpoint-mirror vs intent vs code-orchestration) matches the latest emit defaults |
   | **Discovery** | Whether browser-sniff / crowd-sniff workflows changed, whether prior gate decisions are still valid |

   For the prompt itself, list only the buckets that have at least one notable change between the two versions. If the CHANGELOG / git log is unavailable, list all five buckets generically and let the user decide.

   > "The prior `<api>` was generated with printing-press v`<PRESS_VERSION>`. The current binary is v`<CURRENT>`. Categories where the machine has changed since then: `<applicable buckets>`. Each can invalidate prior research assumptions. Re-validate the prior brief against the current machine before reusing it?"

   Options:
   1. **Yes, re-validate the prior research** — fold the validation into [Phase 1](04-research-brief.md) (briefly re-probe reachability for previously-blocked sources, confirm scoring still classifies the prior CLI's pattern correctly, etc.) before reusing the brief.
   2. **No, reuse the prior research as-is** — proceed with the brief verbatim, even if the underlying machine assumptions are stale.

   The prompt forces the user to acknowledge the version delta and explicitly accept (or refuse) re-validation. Skip it entirely on first generation, on same-version regenerations, or when no prior manifest exists.

   If no CLI exists in the local library and no lock is active, run the **Public-library check** below before proceeding to [Phase 1](04-research-brief.md).

   #### Public-library check (registry.json + blocked-apis.json)

   The local library check above only sees CLIs this machine has already printed. A user on a fresh checkout — or one who typed a slightly different name than the published slug (`Slack` vs `slack-bot`, `Cal` vs `cal-com`), or who described what they wanted in their own words (`Hacker News reader`, `Notion clone`, `prediction market`) — will miss CLIs that already exist in the public library. Scan `mvanhorn/printing-press-library/registry.json` to catch those cases before [Phase 1](04-research-brief.md) research begins (the expensive 30-60-minute portion of the pipeline).

   The public library also carries `blocked-apis.json`, a shared journal of APIs that were attempted and put on hold for reachability or buildability reasons. Scan it in the same Phase 0 window so a user does not repeat an already-known dead-end run before the blocking issue is fixed.

   **Skip this check entirely when:**
   - The local-library check above already prompted (mutual exclusion — do not double-ask).
   - `BROWSER_SNIFF_TARGET_URL` is set (the user is building a from-website CLI; the registry indexes API CLIs and naming collisions are unlikely and intentional).
   - The user passed `--har <path>` with an explicit `--name <api>` for a private capture.

   **Fetch the registry and blocked journal.** Match the pattern `/printing-press-import` and `/printing-press-reprint` already use:

   ```bash
   REGISTRY=$(mktemp)
   if ! gh api -H "Accept: application/vnd.github.v3.raw" \
        repos/mvanhorn/printing-press-library/contents/registry.json \
        > "$REGISTRY" 2>/dev/null; then
     echo "Public-library check failed: registry.json is unreachable. Stop here instead of treating the API as unpublished; retry when GitHub/library access is available."
     rm -f "$REGISTRY"
     exit 1
   fi

   BLOCKED_APIS=$(mktemp)
   if ! gh api -H "Accept: application/vnd.github.v3.raw" \
        repos/mvanhorn/printing-press-library/contents/blocked-apis.json \
        > "$BLOCKED_APIS" 2>/dev/null; then
     echo "Blocked-API journal check skipped: blocked-apis.json unreachable or absent. Proceeding with the registry check."
     rm -f "$BLOCKED_APIS"
     BLOCKED_APIS=""
   fi
   ```

   Do not continue past a registry fetch/parse failure: it can hide an already published nested CLI and route a duplicate greenfield build. Network failure or missing `blocked-apis.json` is still non-blocking. After step 4 finishes, clean up tempfiles only if the fetch succeeded: `[ -n "$REGISTRY" ] && rm -f "$REGISTRY"` and `[ -n "$BLOCKED_APIS" ] && rm -f "$BLOCKED_APIS"`. The blocked-journal failure branch above already removed its file and set its variable to empty, so an unconditional `rm -f "$BLOCKED_APIS"` would run `rm -f ""`.

   **Read the blocked journal before reasoning about registry matches.** If `BLOCKED_APIS` is non-empty, read it directly. Expected shape:

   ```json
   [
     {
       "slug": "1001tracklists",
       "attempted_at": "2026-05-25",
       "verdict": "hold",
       "reason": "Cloudflare Turnstile clearance gate; pure-HTTP cannot mint fsuid",
       "blocking_issue": 2140,
       "permanent": false
     }
   ]
   ```

   Entries are API-slug records, not printed-CLI registry entries. Match the user's requested API against `slug` using the same slug-normalization judgment as the registry check (`<api>`, `<api>-cli`, `<api>-pp-cli`, punctuation and case variants). Do not use vague category or description matching for the blocked journal. A false positive here stops a potentially valid run; only prompt when the entry appears to be the same API under a slug or brand spelling variant.

   If a blocked entry matches, prompt before reading registry matches:

   > "`<entry.slug>` was attempted on `<entry.attempted_at>` and held — `<entry.reason>`<tracking suffix>. The shared blocked-API journal exists so users do not repeat known unreachable or unbuildable runs before the blocker changes. Proceed anyway?"

   Where `<tracking suffix>` is:
   - ` (tracking #<entry.blocking_issue>; marked permanent)` when `blocking_issue` is non-null and `permanent` is `true`.
   - ` (tracking #<entry.blocking_issue>)` when `blocking_issue` is non-null and `permanent` is `false`.
   - ` (marked permanent)` when `permanent` is `true` and `blocking_issue` is `null`.
   - empty when neither applies.

   Options:
   1. **Stop here (recommended)** — end this run. If a tracking issue is present, tell the user to re-attempt only after that issue closes or the journal entry is updated.
   2. **Proceed anyway** — continue to the registry check and then [Phase 1](04-research-brief.md). Use this only when the user has new evidence that the blocker no longer applies or wants a deliberate fresh attempt.

   If multiple blocked entries somehow match, pick the most recent `attempted_at` value and mention that additional older journal entries exist. If `blocked-apis.json` is malformed, print "Blocked-API journal check skipped: blocked-apis.json is malformed. Proceeding with the registry check." and continue; do not let a bad journal file block fresh prints.

   **Read the registry and reason about matches** — do not gate on string equality alone. The registry uses schema version 2: entries are under the `.entries` key (i.e., `jq '.entries[]'`), not at the top level. Read it directly and use judgment. Each entry has fields `name` (slug), `category`, `api` (brand display), `description`, `search_terms`, `path`, `release`, `printer`.

   The user's argument may arrive in many shapes, and only some are catchable by deterministic match:

   - **Slug or near-slug** — `Notion`, `notion-cli`, `notion-pp-cli`
   - **Brand with punctuation** — `Cal.com`, `Customer.io`, `Archive.today`, `Trigger.dev`
   - **Concept or category** — `Hacker News reader`, `Notion clone`, `prediction market`, `prediction-market CLI`
   - **Adjacent product** — `Polymarket` when the registry has `kalshi` (peer prediction market)
   - **Genuinely novel** — no useful overlap

   Classify the best match at three confidence levels and act only on the top two:

   - **High** — same product under a different name (slug variant, brand vs slug form, `-cli`/`-pp-cli` suffix variant, well-known alias). Examples: `Cal.com` ↔ `cal-com`, `Notion` ↔ `notion-cli`, `slack` ↔ `slack-bot`.
   - **Medium** — same category and overlapping function; a reasonable user would want to know before building. Examples: `prediction market` finds `kalshi`, `Hacker News reader` finds `hackernews`, `Polymarket` surfaces `kalshi` as a peer.
   - **Low** — vaguely adjacent (e.g. "payment gateway" finding every payment-related CLI). Skip silently — false-positive prompts get dismissed reflexively at this gate.

   Resist over-matching on `description` keywords. Most descriptions mention several adjacent concepts; matching liberally on description text produces noise. Use the description to *confirm* a name-or-category candidate, not to *discover* candidates from scratch.

   **Combo CLIs.** When `SOURCE_PRIORITY` is set (from the Multi-Source Priority Gate in [references/run-resolution.md](../references/run-resolution.md)), skip the single-source High/Medium/No-match branches below. Classify matches per source, then present a single combined prompt rather than asking N times. For combo runs the existing single-source CLIs are usually *informational* — the user came here to build a combo, so the recommended default is to continue with the combo rather than reprint a component standalone.

   **Cap displayed reprint options at 2 across all sources combined** so the prompt fits the 4-option `AskUserQuestion` limit (2 reprints + continue + abort). Pick the 2 best candidates by judgment in this order: (1) High over Medium, (2) primary-source over secondary-source (the first entry in `SOURCE_PRIORITY` wins ties), (3) canonical slug over variant. If additional matches exist beyond the displayed 2, append "(plus N other source matches)" to the prompt body so the user knows the list is truncated. Omit sources with no match rather than listing them as empty rows. If no source has any match at High or Medium, print nothing and proceed to [Phase 1](04-research-brief.md).

   > Found matches across the sources you listed:
   >
   > - **`<source1>`**: `<entry1.name>` (`<entry1.api>`) [High] — same product as `<source1>`
   > - **`<source2>`**: `<entry2.name>` (`<entry2.api>`) [Medium] — similar/adjacent
   >
   > This is informational — these components already exist as single-source CLIs. Continue building the combo, switch to reprinting one standalone, or abort?

   Options:
   1. **Continue with the combo as planned (recommended)** — the combo itself is the value-add; proceed to [Phase 1](04-research-brief.md) with all sources.
   2. **Reprint `<entry1.name>` standalone instead** — invoke `/printing-press-reprint <entry1.name>` (abandons the combo for now).
   3. **Reprint `<entry2.name>` standalone instead** — same, for the second candidate.
   4. **Abort** — stop here.

   The `[High]` / `[Medium]` tags surface the confidence so the user can distinguish "this is literally the thing you named" from "this is adjacent." Tag in the bullet, not the option label, to keep options scannable.

   **Single-source CLIs.** When `SOURCE_PRIORITY` is not set, use the branches below.

   **High match — prompt strongly.** Under Claude Code, use `AskUserQuestion`; under another harness, use the equivalent native prompt primitive. The option set is the same either way.

   > Found **`<entry.api>`** in the public library (printed by **@`<entry.printer>`**, path `<entry.path>`).
   >
   > `<entry.description>`
   >
   > URL: `https://github.com/mvanhorn/printing-press-library/tree/main/<entry.path>`
   >
   > This CLI already exists. What would you like to do?

   Options:
   1. **Reprint with the current Printing Press (recommended)** — end this run and invoke `/printing-press-reprint <entry.name>`. That skill pulls the existing CLI, carries prior research and post-publish patches into reconciliation, and regenerates under the current binary. Almost always the right choice when a user discovers the CLI exists.
   2. **Continue and build a fresh one anyway** — proceed with the current run from scratch. Rare; appropriate only for a deliberate fork or variant.
   3. **Abort** — stop here.

   **Multiple High matches — present each candidate, do not use the Medium-match phrasing.** Rare — typically only happens when the user's argument is ambiguous between siblings like `slack` and `slack-bot`. Cap displayed candidates at 2 to stay within the 4-option prompt limit alongside continue/abort. If 3+ High candidates somehow qualify, pick the 2 best by judgment (typically the canonical slug match plus the next-most-likely alternative) and note "(plus N other close matches)" in the prompt body so the user knows the list is truncated.

   > Found multiple matches for **`<api>`** in the public library — each appears to be the same product under a different name:
   >
   > - **`<entry1.name>`** (`<entry1.api>`) — `<entry1.description>`
   > - **`<entry2.name>`** (`<entry2.api>`) — `<entry2.description>`
   >
   > Pick one to reprint, or continue/abort.

   Options:
   1. **Reprint `<entry1.name>`** — invoke `/printing-press-reprint <entry1.name>`.
   2. **Reprint `<entry2.name>`** — same, for the second candidate.
   3. **Continue and build a fresh one anyway** — rare; appropriate only for a deliberate fork or variant.
   4. **Abort** — stop here.

   **Medium match — present alternatives.** Cap candidates at 2 to stay within the 4-option prompt limit alongside continue/abort.

   > Found similar entries in the public library that don't exactly match `<api>` but may overlap:
   >
   > - **`<entry1.name>`** (`<entry1.api>`) — `<entry1.description>`
   > - **`<entry2.name>`** (`<entry2.api>`) — `<entry2.description>`
   >
   > Continue with `<api>` as planned, or reprint one of these instead?

   Options:
   1. **Continue with `<api>` as planned** — proceed to [Phase 1](04-research-brief.md).
   2. **Reprint `<entry1.name>` instead** — invoke `/printing-press-reprint <entry1.name>`.
   3. **Reprint `<entry2.name>` instead** — same, for the second candidate.
   4. **Abort** — stop here.

   **No High or Medium match:** print nothing, proceed to [Phase 1](04-research-brief.md).

5. **API Key Gate** — Check whether this API requires authentication, then handle accordingly.

**First, determine if the API needs auth.** Use these signals:
- The spec has no `security` or `securityDefinitions` section → likely no auth needed
- The API's endpoints are accessible without authentication (e.g., ESPN's undocumented endpoints, weather APIs, public data feeds) — note: "no auth required" does NOT mean the service has an official public API
- No env var matching the API name exists AND no known token pattern applies
- Community docs or npm/PyPI wrappers describe the API as "no auth required"

**If no auth is required**, skip the key gate entirely. Proceed with: "No authentication required for `<API>` — skipping API key gate." Do NOT call it "a public API" unless the service officially publishes one. Many services (ESPN, etc.) have unauthenticated endpoints without having an official API. Live smoke testing in [Phase 5](18-dogfood-testing.md) will work without a key.

**If the API DOES require auth**, run the key gate:

Token detection order:
- GitHub: `GITHUB_TOKEN`, `GH_TOKEN`, or `gh auth token`
- Discord: `DISCORD_TOKEN`, `DISCORD_BOT_TOKEN`
- Linear: `LINEAR_API_KEY`
- Notion: `NOTION_TOKEN`
- Stripe: `STRIPE_SECRET_KEY`
- Generic: `API_KEY`, `API_TOKEN`

**If a token IS found**, stop and explain:
> Found `<ENV_VAR>` in your environment. This key will be used **only** for read-only live smoke testing in Phase 5 — listing, fetching, and health checks. It will never be used for write operations (create, update, delete). OK to use it?

- If the user approves → proceed with the key available for [Phase 5](18-dogfood-testing.md).
- If the user declines → proceed without the key and display: "Live smoke testing ([Phase 5](18-dogfood-testing.md)) will be skipped. The CLI will still be generated and verified against mock responses."

**If no token is found**, stop and ask:
> No API key detected for `<API>`. You can provide one now for read-only live smoke testing in Phase 5, or continue without it.
>
> Set it with `export <ENV_VAR>=<your-key>` or paste the key here.

- If the user provides a key → proceed with the key available for [Phase 5](18-dogfood-testing.md).
- If the user declines → proceed without the key and display: "Live smoke testing ([Phase 5](18-dogfood-testing.md)) will be skipped. The CLI will still be generated and verified against mock responses."

Resolve the API key gate (or skip it for public APIs) before moving to [Phase 1](04-research-brief.md).

Before following `Next:`, record the durable handoff. Add `--evidence
<existing-path>` for the resolved spec or state artifact and one short `--note`
when a decision is not otherwise evident:

```bash
"$PRINTING_PRESS_BIN" phase-receipt complete --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "03-resolve-and-reuse"
```

Next: phases/04-research-brief.md
