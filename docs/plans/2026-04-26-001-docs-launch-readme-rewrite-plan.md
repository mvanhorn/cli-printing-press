---
title: "docs: Launch-day README rewrite for cli-printing-press and printing-press-library"
type: docs
status: active
date: 2026-04-26
origin: docs/plans/2026-04-19-001-feat-printing-press-public-launch-plan.md
---

# Launch-day README rewrite for cli-printing-press and printing-press-library

## Overview

The Printing Press launches publicly on Tue Apr 28, 2026 (T-2 days from this plan). Both repo READMEs are the canonical first-touch surface for everyone arriving from the launch video, the X thread, the Show HN post, and printingpress.dev. Today they read like internal docs: the Printing Press README is feature-dense and assumes you already know what "absorb and transcend" means; the Library README is a thin catalog page that doesn't tell the story. They also drift on numbers (Library README still claims "21 CLIs, 19 MCP servers" — actual is 24 CLIs / 17 MCP servers, heading to 25 / 18 by launch).

This plan proposes the rewrite of both READMEs as one paired narrative. The Printing Press README leads with Matt's pitch voice, anchors the first 90 seconds of attention with three concrete demos (ESPN, flight-goat, Linear), then keeps the strongest sections of the existing copy. The Library README opens with the same 3-line hook so the two repos feel like one product, then becomes a real catalog page with an at-a-glance install pattern. Both link to printingpress.dev as the live home.

This is Unit 5 of the public launch plan — the README refresh. It does not change generator behavior, ship new features, or modify CLI internals. Polish only.

---

## Problem Frame

A first-time visitor arrives at one of the two repos cold. They have ~30 seconds before they bounce. The README has to:

1. Tell them what the Printing Press is in one sentence.
2. Tell them why an agent-native CLI matters in 2026 (speed, tokens, muscle memory).
3. Show them a concrete thing it can do that nothing else can (sniffed APIs, compound SQLite queries, GraphQL+REST mix).
4. Hand them an install command that works on the first paste.
5. Cross-link to the other repo and to printingpress.dev so the visitor doesn't have to hunt.

Today neither README does all five. The Printing Press README does (1)-(3) well but buries the install path under feature density and leans on insider terminology ("absorb and transcend", "NOI", "Rung 5") before earning it. The Library README skips (1)-(3) almost entirely and doesn't give the visitor a reason to install before showing the table.

The current numbers also need a refresh:

- cli-printing-press is on **v2.3.7** (not the 1.3.3 noted in the launch plan from a week ago).
- The library has **24 published CLIs** today across 17 categories, with **17** shipping an MCP server. Showcase additions Stripe / GitHub / Discord land before launch, taking the count to 25 / 18-20.
- The catalog ships **19 pre-cooked APIs** ready to generate.
- Recent shipped features that the README hasn't surfaced yet: browser-sniff with traffic analysis, mcp-audit subcommand, machine-owned freshness for store-backed CLIs, MCP production-readiness (HTTP transport, intent tools, code orchestration), composed cookie auth, super-cli run namespace, auth doctor.

---

## Requirements Trace

- R1. Both READMEs open with the same 3-line pitch so the two repos feel like one product. (Origin R6.)
- R2. Printing Press README anchors the first screen on Matt's pitch voice and three concrete demos (ESPN, flight-goat, Linear), each one resolving a different "you-can-only-do-this-here" capability.
- R3. Library README becomes a real first-touch surface: same pitch hook, then story, then catalog table with accurate counts.
- R4. Every install command (binary, plugin, per-CLI) pastes verbatim into a fresh Claude Code session and works on the first try.
- R5. Both READMEs cross-link to each other and to printingpress.dev (treated as live).
- R6. Numbers and counts in copy match the live state on launch morning: 25 published CLIs, ~18-20 MCP servers, 19 catalog APIs, current binary version. (Origin R2, R6.)
- R7. New shipped features have a home in the Printing Press README without bloating the first screen: browser-sniff traffic analysis, mcp-audit, machine-owned freshness, MCP production-readiness, auth doctor.
- R8. Tone rules respected throughout: no emdashes, no endashes, no bold. Single hyphen ok. (User memory `feedback_formatting`.)
- R9. Credits preserved and updated: Steinberger (discrawl, gogcli), Trevin Chow, Ramp.

**Origin actors:** end users (developers and AI agents), launch-day visitors, future contributors.
**Origin flows:** F1 (visitor lands -> reads pitch -> installs), F2 (returning user -> updates -> finds new features), F3 (cross-link from one repo to the other).
**Origin acceptance examples:** AE1 (cold visitor pastes install command in fresh session and `/printing-press` works), AE2 (visitor on Library README clicks any CLI install command and `go install` succeeds), AE3 (visitor on either README finds printingpress.dev within 30 seconds).

---

## Scope Boundaries

- Not changing the Printing Press generator, templates, or skills.
- Not changing the Library plugin's `registry.json`, `tools/generate-skills/`, or any per-CLI source.
- Not adding new CLIs to the library — the showcase trio (Stripe, GitHub, Discord) is Unit 2 of the parent launch plan and runs independently.
- Not touching CHANGELOG, AGENTS.md, CONTRIBUTING.md, or per-CLI READMEs.
- Not building the website. printingpress.dev is a separate repo (parent launch plan Units 3, 4).

### Deferred to Follow-Up Work

- Per-category Library filtering UI: separate post-launch PR.
- Tagged release notes / launch announcement: separate task in the launch runbook.
- A future "what's new in the press" running section: deferred until there's enough launch traffic to justify it.

---

## Context and Research

### Relevant Code and Patterns

- `README.md` (cli-printing-press): current narrative is strong on absorb/transcend, NOI, creativity ladder, archetypes. Keep the spine, replace the entry sequence and the example block, add new feature surface near the bottom, refresh counts.
- `README.md` (printing-press-library): currently a catalog page with no story. Needs a full rewrite of the top half plus a refreshed catalog table.
- `registry.json` (printing-press-library): authoritative count source. **24 CLIs as of 2026-04-26.** Catalog table copy must regenerate from this on launch morning.
- `catalog/*.yaml` (cli-printing-press): authoritative catalog count. **19 entries** (asana, digitalocean, discord, front, github, google-flights, hubspot, kayak, launchdarkly, petstore, pipedrive, plaid, postman-explore, producthunt, sentry, stripe, stytch, telegram, twilio).
- `.claude-plugin/plugin.json` (both repos): version source for the install snippet pages.
- `docs/plans/2026-04-19-001-feat-printing-press-public-launch-plan.md`: parent plan; this README rewrite is its Unit 5.
- `docs/PIPELINE.md` (cli-printing-press): keep the README's pipeline link pointing here.

### Institutional Learnings

- Memory `feedback_formatting`: no emdashes, endashes (`--`), or bold (`**`) in user-facing prose. Single hyphen `-` is ok. Single asterisk italics `_` are ok. The rewrite's lint check greps both repos for `--` and `**` and fails if either is found in a non-code-block region.
- Memory `feedback_pp_update_before_run`: install snippets in the README will be the first thing newcomers paste. They must include the up-to-date `@latest` version, and the binary install must reflect the `/v2/` module path that landed in v2.3.7 (PR #298) so `--version` reports correctly.
- Memory `user_profile`: pitch voice is direct, period-heavy, short sentences. Mirror that in the hero, not the rest. The middle of the README can keep its current voice.
- Memory `feedback_no_process_in_pr_body` is README-adjacent: don't narrate the rewrite ("we recently refactored this README"). The README is about the product, not the work.
- Memory `feedback_modal_visibility`: doesn't apply here directly, but the analogous principle does — assume nobody scrolls. Everything load-bearing has to be in the first screen.

### External References

- Printing Press launch arc on X (Apr 20-27 build-in-public): the README has to land the same hook the thread builds toward, so launch-morning visitors don't hit a tone mismatch.
- printingpress.dev hero copy: the site and the README converge on the same 3-line pitch (the site is shorter; the README uses the site copy verbatim as its top block so cross-channel branding is consistent).
- Steinberger discrawl / gogcli posts: keep the credit and the framing ("we built on giants' shoulders and we say so") that the current README already does well.
- Anthropic 2026-04-22 "production agent" MCP guidance: the Printing Press README's MCP section already references this for the `mcp:` spec surface. Keep.

### Organizational Context (Slack)

Not gathered for this plan. The launch decisions (date, channels, showcase trio) were resolved in the parent launch plan and are not re-litigated here.

---

## Key Technical Decisions

- **Anchor demos: ESPN, flight-goat, Linear.** User-confirmed in planning. Mirrors the live pitch text. ESPN proves "no official API, no problem" (browser-sniff). Flight-goat proves "two sources stitched into one query" (Kayak nonstop search + sniffed Google Flights). Linear proves "compound queries the API can't do" (50ms SQLite mirror). Stripe / GitHub / Discord still appear, but as launch-day showcase callouts further down, not the hero.
- **Live site posture for printingpress.dev.** User-confirmed: treat as live and link prominently. Hero badge in both READMEs links to printingpress.dev. If the site is somehow not live at 9am PT Apr 28, the README still reads fine since the link is one element among many; the runbook's pre-flight check (Unit 13 in the parent plan) catches this.
- **Pitch trio in the Printing Press README hero; same trio echoed in the Library hero.** The Library hero says "the catalog of CLIs the Printing Press has already printed" and reuses ESPN / flight-goat / Linear as one-liner examples, then drops into the catalog table.
- **Keep the Printing Press README's strongest sections.** Absorb and Transcend, the Non-Obvious Insight table, How I Knew This Was Real, the Creativity Ladder, CLIs + MCP, Domain Archetypes, Quality Scoring. Trim where dense, refresh counts, drop the development bottom matter into a CONTRIBUTING reference.
- **Library README pivots from catalog page to story-then-catalog.** Top half: pitch + 3 concrete demos + how-it-fits-with-Printing-Press. Bottom half: install paths, regenerated table sourced from `registry.json`, repo structure overview.
- **Counts are sourced from a single check on launch morning.** A 6-line shell snippet in the launch runbook regenerates the counts (`jq '.entries | length' registry.json`, `ls catalog/*.yaml | wc -l`, `find library -name "*-pp-mcp" -type d | wc -l`, `cat .claude-plugin/plugin.json | jq -r .version`). Every "X CLIs / Y MCP servers / Z catalog APIs" claim in either README pulls from that snippet's output, not from a static guess.
- **No emdashes, endashes, bold.** Enforced by a final grep over both files in Unit 3.
- **Cross-links use absolute GitHub URLs** so they keep working on printingpress.dev when the README copy is mirrored to the site, on Show HN previews, on plugin-marketplace renders, and inside Claude Code's plugin UI.
- **Install commands are byte-identical across README, video, site, and X thread.** A single source-of-truth file `launch-assets/install-commands.txt` (parent launch plan Unit 5/8/13) holds the canonical strings; Unit 3 of this plan grep-asserts the README copies match.

---

## Open Questions

### Resolved During Planning

- Anchor demos: ESPN / flight-goat / Linear (user-confirmed).
- Site link posture: live and prominent (user-confirmed).
- Showcase callouts: Stripe / GitHub / Discord get one paragraph and table rows; not hero examples.
- Tone: pitch voice in the hero, current README voice elsewhere.
- Credits: Steinberger, Trevin Chow, Ramp preserved. Add a thank-you to community filers / contributors only if there are concrete handles by Sat Apr 26 EOD; otherwise defer to the X thread (parent plan Unit 8).

### Deferred to Implementation

- The exact final word count of each README. Target the Printing Press README at ~450 lines (current is 489) and the Library README at ~180 lines (current is 179). Stay close.
- Whether the pipeline diagram block in the Printing Press README's "How It Works" stays inline or moves to `docs/PIPELINE.md`. Default: keep inline, since it's load-bearing for the "fast path" claim, but trim the per-phase prose by ~40%.
- Whether the per-CLI install commands in the Library catalog table stay as full `go install ...` strings or collapse to `/ppl install <name> cli`. Default: show the slash-command form first, full `go install` second, in two adjacent table columns.
- Whether the showcase trio gets its own subsection in the Library README. Default: yes, one paragraph titled "New on launch day" calling out Stripe / GitHub / Discord with a one-liner each.

---

## High-Level Technical Design

> This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat the appendices as source-of-truth prose, not exact specification, and adjust word-by-word edits as needed.

The two READMEs share a hero. Below the hero they diverge into two complementary surfaces.

```
                            +-----------------------------+
                            |    SHARED HERO BLOCK        |
                            |  (3 lines pitch, 1 demo,    |
                            |   2 install commands,       |
                            |   1 link to dev site)       |
                            +-------------+---------------+
                                          |
                +-------------------------+--------------------------+
                |                                                    |
                v                                                    v
   +--------------------------------+              +-------------------------------+
   | PRINTING PRESS README          |              | LIBRARY README                |
   |--------------------------------|              |-------------------------------|
   | Story: how a CLI gets printed  |              | Story: catalog of printed CLIs|
   | Why these CLIs win             |              | At-a-glance examples          |
   | Absorb and Transcend           |              | Two install paths             |
   | The Non-Obvious Insight        |              | Catalog table (25, 17 cats)   |
   | Creativity Ladder              |              | New on launch day (showcase)  |
   | CLIs + MCP                     |              | Repo structure                |
   | Domain Archetypes              |              | What 'endorsed' means         |
   | How It Works (pipeline)        |              | Contributing                  |
   | What Gets Generated            |              |                               |
   | Quality Scoring                |              |                               |
   | Auth doctor + mcp-audit        |              |                               |
   | Library cross-link             |              |                               |
   | Quick Start                    |              |                               |
   | Verification                   |              |                               |
   | Credits                        |              |                               |
   +--------------------------------+              +-------------------------------+
                |                                                    |
                +------------------ cross-links ---------------------+
                                          |
                                          v
                                  printingpress.dev
```

The shared hero is the only block that must be byte-identical (modulo install command ordering) between the two READMEs. Everything below the hero is repo-specific.

---

## Output Structure

Two existing files modified, two appendix files left as a reference for the implementer. No new directories.

    cli-printing-press/
      README.md                                                  modified
      docs/plans/2026-04-26-001-docs-launch-readme-rewrite-plan.md   this plan

    printing-press-library/
      README.md                                                  modified

    launch-assets/install-commands.txt                           consulted (already produced by parent launch plan)

---

## Implementation Units

- [ ] U1. **Rewrite cli-printing-press README**

**Goal:** Replace the current Printing Press README with a launch-day cut that opens in pitch voice, anchors on ESPN / flight-goat / Linear, surfaces the new shipped features (browser-sniff traffic analysis, mcp-audit, machine-owned freshness, auth doctor, MCP production-readiness), refreshes every count and version, and links to printingpress.dev.

**Requirements:** R1, R2, R4, R5, R6, R7, R8, R9

**Dependencies:** None within this plan. Parent launch plan Unit 1 (launch date, domain, showcase trio confirmed) is the only external prerequisite.

**Files:**
- Modify: `README.md` (root of `cli-printing-press`)

**Approach:**
- Replace the top of the file (lines 1-37 in the current copy) with the new shared hero block from Appendix A.
- Keep the strongest existing sections: "Why These CLIs Win", "Every Endpoint. Every Insight. One Command.", "Absorb & Transcend", "The Non-Obvious Insight", "How I Knew This Was Real", "The Creativity Ladder", "Why Not Just CLIs - CLIs + MCP", "Domain Archetypes", "How It Works", "Quality Scoring - Three Benchmarks". Trim each by ~10-20% where prose runs long.
- Replace the example block in the hero with the ESPN / flight-goat / Linear three-question demo.
- Add a new short section "What's new since 1.0" with a 5-bullet rundown of recently shipped capabilities (browser-sniff traffic analysis, mcp-audit, machine-owned freshness, MCP production-readiness with HTTP transport + intents + code orchestration, auth doctor). Each bullet is one sentence + one command example. Place between "Domain Archetypes" and "How It Works".
- Refresh the "Library" section to show the current count (25 by launch) and a one-line cross-link to printing-press-library and printingpress.dev. Drop the per-CLI install table from this section; that lives in the Library README.
- Refresh the "Quick Start" install snippets so they match `launch-assets/install-commands.txt` byte-for-byte.
- Move "Development" and "Lint setup" content to the bottom of the file under a single "Development" heading; trim by 50% and add a pointer to AGENTS.md for full conventions.
- Apply the no-emdashes / no-endashes / no-bold rule throughout. Single-hyphen ok.
- Verify all numeric claims against the launch-morning counts snippet (Decision: counts are sourced from a single check on launch morning).

**Patterns to follow:**
- Current README's section voice for the middle (Why These CLIs Win through Quality Scoring).
- User pitch voice for the hero. See Appendix A.
- Existing credits block at the bottom — keep, refresh names if any new contributors land before Apr 27 EOD.

**Test scenarios:**
- Happy path: render the new README on github.com/mvanhorn/cli-printing-press at 1200px desktop width; the hero plus first demo plus install snippet fits above the fold on a 1080p viewport. Covers AE1.
- Happy path: paste the four install commands from the Quick Start section into a fresh Claude Code session in order; `/printing-press` runs and `printing-press --version` prints `2.3.7` (or current). Covers F1, AE1.
- Edge case: render the README in dark mode and light mode in GitHub's preview; no broken images, no clipped tables, no overflow.
- Edge case: the README's "What's new since 1.0" bullets each cite a feature that exists in the current binary (verify by `printing-press --help` and subcommand `--help` output).
- Error path: `grep -nP "(—|–|--[^\-]|\*\*)" README.md` returns zero matches in non-code-block regions. (Tone rule.)
- Integration: every internal link (anchors, relative file paths) resolves on github.com.
- Integration: every external link (printing-press-library repo, printingpress.dev, Steinberger repos, Anthropic blog) returns HTTP 200 from `curl -sIL`.

**Verification:**
- The hero plus pitch plus first install command fits the first screen on github.com at 1080p.
- All install commands are byte-identical to `launch-assets/install-commands.txt`.
- All counts match the launch-morning counts snippet.
- Tone-rule grep is clean.
- All links resolve.

---

- [ ] U2. **Rewrite printing-press-library README**

**Goal:** Pivot the Library README from a thin catalog page to a story-then-catalog page that opens with the same hero as the Printing Press README, demonstrates the catalog with three live one-liners (ESPN, flight-goat, Linear), shows a launch-day "new on launch day" call-out for Stripe / GitHub / Discord, then drops into a refreshed install + catalog table sourced from `registry.json`.

**Requirements:** R1, R3, R4, R5, R6, R8

**Dependencies:** None within this plan. Parent launch plan Unit 2 (showcase trio published) needs to land before launch morning so the table renders 25 entries; this plan's Unit 3 grep-asserts the count.

**Files:**
- Modify: `README.md` (root of `printing-press-library`)

**Approach:**
- Replace the top of the file with the same shared hero block from Appendix A. Re-order the install commands so the Library plugin install is first (this is the Library repo's home turf), then the Printing Press install second.
- Add a new section "What's in the Library" right after the hero: three one-liner examples (ESPN, flight-goat, Linear) showing what `/ppl` calls look like in practice. Each is a single line of code plus a single-sentence outcome description.
- Add a section "Two ways in" that contrasts `/ppl` (the mega-skill router) vs. `/pp-<name>` (focused per-CLI skills). Three sentences each.
- Replace the catalog table with a regenerated table that includes all 25 entries (24 today + showcase trio). Two install columns: slash-command first, `go install` second. Keep the auth and MCP columns. See Appendix B.
- Add a "New on launch day" section right before the catalog table calling out Stripe / GitHub / Discord with a one-line description each, only if Unit 2 of the parent plan has actually landed those CLIs by Sat Apr 26 EOD. If any of the three didn't land, the section is omitted; the plan accepts 24 instead of 25 (parent plan risk row).
- Keep the "Repo Structure" tree, refresh it slightly to clarify `skills/pp-*` is a generated mirror of `library/<…>/SKILL.md`.
- Keep the "What 'Endorsed' Means" section verbatim.
- Apply the tone rules.
- Add a footer that cross-links back to cli-printing-press and to printingpress.dev.

**Patterns to follow:**
- The current Library README's tone for the structural sections (Repo Structure, What 'Endorsed' Means, Contributing). Preserve.
- The Printing Press README's hero and example-driven voice for the new top half. See Appendix A.
- `obra/superpowers` and `anthropics/skills` for the bare-string source plugin install snippet pattern (already used today, keep).

**Test scenarios:**
- Happy path: paste the two plugin install commands plus one per-CLI `go install` example in a fresh Claude Code session; the library plugin loads, `/ppl` lists CLIs, `/pp-espn` is reachable, the per-CLI binary builds. Covers F1, AE1, AE2.
- Happy path: catalog table renders 25 rows on github.com (or 24 if Stripe/GitHub/Discord didn't all land — risk-row fallback).
- Edge case: registry.json has more entries than the table copy (or vice versa); a CI job greps the count and fails the docs check. Add this guard to Unit 3, not to U2 itself.
- Edge case: a CLI listed in the table has no `cmd/<name>-pp-mcp` directory but is marked `mcp: full`; the table copy is wrong. Verify before commit.
- Integration: clicking each CLI's link in the table lands on a working `library/<category>/<slug>/` directory in the repo.
- Integration: cross-link to cli-printing-press and to printingpress.dev resolves; trailing slash and casing match what the Printing Press README uses.
- Error path: tone-rule grep is clean.

**Verification:**
- Hero is byte-identical (modulo install order) to the Printing Press hero.
- Catalog table count matches `jq '.entries | length' registry.json`.
- Tone-rule grep is clean.
- All install commands are byte-identical to `launch-assets/install-commands.txt`.
- All 25 catalog rows resolve to a real `library/<category>/<slug>/` directory.

---

- [ ] U3. **Cross-repo consistency check + dry-run install verification**

**Goal:** Catch every silent drift between the two READMEs and the live state before launch morning. This unit is mechanical, fast, and runs three times: at the end of each rewrite, and once more at 8:55am PT Apr 28 as part of the launch-day pre-flight.

**Requirements:** R4, R5, R6

**Dependencies:** U1, U2

**Files:**
- No file modifications; this unit is verification only.
- Optional: `scripts/check-readme-consistency.sh` if a small shell script saves time on repeat runs. If added, place in `cli-printing-press/scripts/`.

**Approach:**
- Run a six-step check:
  1. `grep -nP "(—|–|--[^\-]|\*\*)" README.md` in both repos, excluding fenced code blocks. Must return zero non-code matches.
  2. `diff <(awk '/^---$/{exit} 1' cli-printing-press/README.md | head -40) <(awk '/^---$/{exit} 1' printing-press-library/README.md | head -40)` — the shared hero block (the first ~40 lines, modulo install command order) must produce only the expected install-order swap.
  3. `jq '.entries | length' printing-press-library/registry.json` — must equal the number quoted in both READMEs (25 on launch day, 24 today as a fallback).
  4. `find printing-press-library/library -type d -name "*-pp-mcp" | wc -l` — must equal the MCP-server count quoted in the Library README.
  5. `ls cli-printing-press/catalog/*.yaml | wc -l` — must equal the catalog count quoted in the Printing Press README (19 today; verify on launch morning in case showcase YAMLs are added).
  6. For each install command in `launch-assets/install-commands.txt`, grep both READMEs for the exact string. Must appear in at least one of the two; the binary install command must appear in the Printing Press README; the two plugin marketplace commands must appear in both.
- Run the check three times: after U1 commit, after U2 commit, and at 8:55am PT Apr 28 as part of the launch-day pre-flight runbook (parent plan Unit 13). The third run is the load-bearing one — it catches any drift introduced by other PRs landing in the 48 hours between this rewrite and launch.
- If any check fails, fix the offending file in a follow-up commit; don't try to merge back into the original commit and rewrite history. Treat the check as a CI-style gate, not a refactor target.

**Patterns to follow:**
- Existing pre-push lint patterns (`lefthook.yml` in cli-printing-press) for the shape of small mechanical checks.
- The launch-day runbook's existing pre-flight pattern.

**Test scenarios:**
- Happy path: all six checks pass on a fresh clone of both repos.
- Edge case: a CLI is added to the library between this plan landing and Apr 28; check 3 catches the count drift.
- Edge case: a maintainer adds a new catalog YAML between this plan landing and Apr 28; check 5 catches the count drift.
- Edge case: a downstream PR introduces an emdash in the README; check 1 catches it.
- Error path: `launch-assets/install-commands.txt` is missing or empty; check 6 fails loudly with the path it expected, not a silent skip.
- Integration: the shell script exits non-zero on any failure so it can be wired into a future GitHub Action without rewrite.

**Verification:**
- All six checks pass on launch morning Apr 28 at 8:55am PT.
- Any failure produces a clear, single-line diagnostic naming the file and the failed check.

---

## System-Wide Impact

- **Interaction graph:** The two READMEs are linked from printingpress.dev (parent plan Unit 4), the launch video (Unit 6), the X thread (Unit 8), the HN post (Unit 9), and the PH listing (Unit 10). Any rename of a section anchor (e.g., `#install`) will silently break those external links. **Mitigation:** keep the existing anchor IDs (`#get-it`, `#how-it-works`, `#library`, `#quick-start`, `#credits` on the Printing Press side; `#start-here`, `#catalog`, `#installation-paths` on the Library side) wherever possible. Where renames are necessary, update all five external surfaces in the same PR.
- **Error propagation:** Stale counts in either README cascade into the launch video's "25 CLIs" claim and the X thread's tweet 5. The launch-morning pre-flight check (U3) is the canonical gate; if it fails, the runbook calls it out before posting goes live.
- **State lifecycle risks:** Plugin marketplaces cache README content. After merging U1 / U2, force a marketplace refresh by bumping `.claude-plugin/plugin.json` patch version on the Library side (the cli-printing-press side already auto-bumps on the next release). This is in scope for U2 but called out here so it's not forgotten.
- **API surface parity:** Install commands appear in five surfaces (READMEs x2, video, site, X thread). The single-source-of-truth file `launch-assets/install-commands.txt` is the contract; U3 verifies READMEs match it.
- **Integration coverage:** A clean-clone install flow on a scratch machine (parent plan Unit 13 Mon Apr 27) is the only end-to-end check that proves the README install path works without any cached state. That run substitutes for the integration coverage U1 and U2 cannot prove on their own.
- **Unchanged invariants:** No CLI binaries, no spec formats, no plugin manifest version layouts, no CHANGELOG conventions change in this plan. The launch is marketing and polish, not new capability.

---

## Risks and Dependencies

| Risk | Mitigation |
|------|------------|
| Showcase trio (Stripe / GitHub / Discord) doesn't all land by Apr 26 EOD; the Library README claim of 25 CLIs is wrong on launch morning | U3's count check on launch morning catches the drift; runbook fallback is to swap "25" for the actual count and drop the showcase callout if any of the three is missing. Parent plan accepts 24. |
| printingpress.dev is not actually live at 9am PT despite the user-confirmed posture | The README link still renders fine — it just 404s for a few minutes; the runbook's 8:55 pre-flight check verifies site liveness and, if it fails, swaps the link to the placeholder URL. |
| A maintainer lands an unrelated README change between U1/U2 merge and launch morning that re-introduces an emdash or breaks a count | U3's third run on launch morning catches it; the fix is a one-line follow-up commit, not a rewrite. |
| Tone rule fights existing prose: a few bullets in the current README use `--` as a fake emdash (especially in the Quality Scoring tables) | Replace with `-` (single hyphen) or restructure the sentence. Tested manually before commit; U3 grep is the backstop. |
| The "What's new since 1.0" section drifts the moment a new feature ships | Mark the section header with the binary version that the bullets reflect (`What's new since v1.0 (current: v2.3.7)`); feature entries reference the subcommand they install rather than implementation details, so they survive minor refactors. |
| The catalog table in the Library README hand-edits drift from `registry.json` | Treat the table as a generated artifact; future iteration adds a `tools/generate-readme-catalog/` mirror to `tools/generate-skills/`. Out of scope for this plan, called out in Documentation / Operational Notes. |
| Cross-link to Steinberger / Trevin / Ramp could go stale (account renamed, repo archived) | All three are stable today; no mitigation needed beyond the U3 link-resolution check. |

---

## Documentation and Operational Notes

- After U1 and U2 merge, post a "READMEs are launch-ready" note in the launch-day runbook so the runbook owner knows Unit 5 of the parent launch plan is done.
- The launch-morning pre-flight check (U3 third run) is added as an item to the runbook's 8:55 PT block.
- Follow-up plan: a small generator that emits the Library catalog table from `registry.json` (similar to how `tools/generate-skills/main.go` emits `skills/pp-*/`). Out of scope for this plan; opened as an issue against `printing-press-library` after launch.
- Follow-up plan: an automated GitHub Action that runs U3's six checks on every PR to either repo. Out of scope for this plan; opened as an issue against both repos after launch.
- The `What's new since v1.0` section in the Printing Press README is the natural home for future shipped-feature entries; new PRs that ship a user-visible capability should add a one-line entry there as part of the PR.

---

## Sources and References

- **Origin document:** [docs/plans/2026-04-19-001-feat-printing-press-public-launch-plan.md](docs/plans/2026-04-19-001-feat-printing-press-public-launch-plan.md)
- Related code: `README.md` (cli-printing-press), `README.md` (printing-press-library), `registry.json` (printing-press-library), `catalog/*.yaml` (cli-printing-press), `.claude-plugin/plugin.json` (both repos).
- Related plans: `2026-04-12-003-feat-readme-skill-narrative-enrichment-plan.md`, `2026-03-26-docs-readme-noi-storytelling-plan.md`, `2026-03-27-docs-readme-narrative-refresh-plan.md`. These previous README iterations established the absorb-and-transcend, NOI, and creativity-ladder voice that this plan preserves.
- External docs: Steinberger discrawl (https://github.com/steipete/discrawl), gogcli (https://github.com/steipete/gogcli), Anthropic 2026-04-22 production-agent MCP guidance (https://www.anthropic.com/news/building-agents-that-reach-production-systems-with-mcp), Trevin Chow agent-friendly CLI principles thread (https://x.com/trevin/status/2037250000821059933).
- Memory references: `feedback_formatting`, `feedback_pp_update_before_run`, `user_profile`, `feedback_modal_visibility`, `project_v3_community_contributors`.

---

## Appendix A: Proposed README — cli-printing-press

> Copy below from `# CLI Printing Press` to the bottom of the License section into `README.md` at the root of `cli-printing-press`. The surface preserves the strongest existing sections (Why These CLIs Win, Absorb and Transcend, NOI table, Creativity Ladder, etc.) and replaces the hero plus install plus example block with a launch-day cut. All counts assume launch-morning state (25 CLIs / 17 MCP servers / 19 catalog APIs / binary v2.3.7); confirm via U3 before commit.

```markdown
# CLI Printing Press

Nothing is more valuable than time and money. In a world of AI agents, that's speed and token spend. A well-designed CLI is muscle memory for an agent: no hunting through docs, no wrong turns, no wasted tokens. We built the Printing Press to print the best CLIs in the world for agents.

It reads the official API docs, studies every popular community CLI and MCP server, sniffs the web for the APIs nobody published (think Google Flights or Dominos), and applies the power-user playbook Peter Steinberger proved with [discrawl](https://github.com/steipete/discrawl) and [gogcli](https://github.com/steipete/gogcli) - local SQLite, compound commands, agent-native flags. It fuses all of that and prints a token-efficient Go CLI plus a Claude Code skill plus an MCP server for any API or any website.

Three CLIs printed by the press, installable today:

- **ESPN** (sniffed, no official API). _"Tonight's NBA playoff games with live score, series state, each team's leading scorer's stat line, and any injury or lineup news from the last 24 hours."_ Returns everything in one call.
- **flight-goat** (Kayak nonstop search plus sniffed Google Flights). _"Non-stop flights over 8 hours from Seattle for 4 people, Dec 24 to Jan 1, cheapest first."_ Two sources stitched into one query.
- **linear-pp-cli** (50ms against a local SQLite mirror). _"Every blocked issue whose blocker has been stuck for a week."_ Compound queries the API can't answer.

Browse the full catalog of printed CLIs at [printingpress.dev](https://printingpress.dev) or in the [Printing Press Library](https://github.com/mvanhorn/printing-press-library). 25 CLIs across 17 categories, 17 with full MCP servers.

## Get it

Install the binary, then add the Claude Code plugin. Both fit in one paste.

```bash
go install github.com/mvanhorn/cli-printing-press/v3/cmd/printing-press@latest
```

```text
/plugin marketplace add mvanhorn/cli-printing-press
/plugin install cli-printing-press@cli-printing-press
```

Want pre-built CLIs to use right now? Add the [Printing Press Library](https://github.com/mvanhorn/printing-press-library) plugin too:

```text
/plugin marketplace add mvanhorn/printing-press-library
/plugin install printing-press-library@printing-press-library
```

## Print a CLI

```bash
/printing-press HubSpot                              # From the catalog (19 APIs ready)
/printing-press --spec ./openapi.yaml                # From a local spec
/printing-press --har ./capture.har --name ESPN      # From captured browser traffic
/printing-press https://postman.com/explore          # From a URL (auto-detects intent)
/printing-press HubSpot codex                        # Codex mode - 60% fewer Opus tokens
/printing-press emboss notion                        # Second pass: improve an existing CLI
```

One command. Lean loop. Produces a Go CLI plus an MCP server that absorbs every feature from every competing tool, then transcends with compound use cases only possible with local data. REST, GraphQL, or browser-sniffed traffic. No OpenAPI spec required.

## Why These CLIs Win

Most generators wrap endpoints and stop. Printing Press generates CLIs that **understand the domain**.

[Keep existing copy from current README, lines 40-55, unchanged.]

## Every Endpoint. Every Insight. One Command.

[Keep existing copy from current README, lines 56-63, unchanged.]

## Absorb and Transcend

[Keep existing copy from current README, lines 64-72, unchanged. Replace the section title's emdash if any. Verify no `**` bold remains.]

## The Non-Obvious Insight

[Keep existing copy from current README, lines 74-99, unchanged. The NOI table is load-bearing for the launch story.]

## How I Knew This Was Real

[Keep existing copy from current README, lines 100-106, unchanged. Steinberger gogcli vs Google Workspace CLI.]

## The Creativity Ladder

[Keep existing copy from current README, lines 108-122, unchanged.]

## Why Not Just CLIs - CLIs plus MCP

[Keep existing copy from current README, lines 124-170, unchanged. The MCP spec surface block is current as of v2.3.7.]

## Domain Archetypes

[Keep existing copy from current README, lines 172-184, unchanged.]

## What's new since v1.0 (current: v2.3.7)

The press has shipped continuously since 1.0. Five capabilities you can use today:

- **Browser-sniff with traffic analysis.** `/printing-press --har ./capture.har` analyzes the capture and produces an OpenAPI-compatible spec plus a `discovery/` manuscript with protocol, auth, and rate-limiting signals. Use when no official spec exists.
- **MCP production-readiness.** Specs can opt into HTTP transport (`transport: [stdio, http]`), declarative multi-step intent tools (`intents:`), or a Cloudflare-style thin code-orchestration surface (`orchestration: code`). Run `printing-press mcp-audit` to see which library CLIs would benefit. Aligned with Anthropic's [2026-04-22 production-agents guidance](https://www.anthropic.com/news/building-agents-that-reach-production-systems-with-mcp).
- **Machine-owned freshness.** Store-backed CLIs with `cache.enabled` opt into a bounded pre-read refresh in `--data-source auto` so local SQLite stays current without a manual `sync`. `--data-source local` and `--data-source live` give you full control.
- **Auth doctor.** `printing-press auth doctor` scans every installed printed CLI and reports whether its declared env vars are set, suspicious, or missing. Fingerprints show only the first four characters. Useful when an agent hits a 401 and you need to know whether the token is missing or stale before reading shell config.
- **Codex mode.** `/printing-press <api> codex` offloads Phase 3 code generation to Codex CLI for ~60% fewer Opus tokens. Claude stays the brain (research, planning, scoring, review); Codex does the hands. Falls back to local generation after 3 failures, no manual intervention.

## How It Works

[Keep existing copy from current README, lines 186-225, unchanged. Pipeline diagram and three entry paths.]

## What Gets Generated

[Keep existing copy from current README, lines 228-265, trim by ~25%. Specifically: collapse the long single-paragraph "Designed for AI agents" block and the agent-first flag list into one tighter section. Drop the "Tests" and "Distribution scaffold" bullets into a smaller "Production-ready output" subsection.]

## Quality Scoring - Three Benchmarks

[Keep existing copy from current README, lines 266-315, unchanged.]

## Diagnosing Auth

[Keep existing "Diagnosing Auth" copy from current README, lines 318-326. Already matches the new "What's new" auth doctor bullet; no rewrite needed.]

## Library

Published CLIs live in the [Printing Press Library](https://github.com/mvanhorn/printing-press-library), organized by category. 25 CLIs across 17 categories, 17 with full MCP servers. Browse at [printingpress.dev](https://printingpress.dev) or run `/ppl` after installing the Library plugin.

A small sample - see the [full catalog](https://github.com/mvanhorn/printing-press-library#catalog) for all 25:

| CLI | Category | What it does |
|-----|----------|--------------|
| `espn-pp-cli` | Media and Entertainment | ESPN sports data: scores, stats, standings across 17 sports. |
| `flightgoat-pp-cli` | Travel | Kayak nonstop search plus sniffed Google Flights, in one call. |
| `linear-pp-cli` | Project Management | 50ms compound queries against a local Linear mirror. |
| `kalshi-pp-cli` | Payments | Trade prediction markets from the terminal. |
| `recipe-goat-pp-cli` | Food and Dining | Trust-aware ranking across 37 recipe sites. |

Each published CLI ships a research manuscript, verification proofs, and a `.printing-press.json` provenance manifest.

## Quick Start

### Install

[Keep existing "Install" copy from current README, lines 343-364, unchanged. Both plugin install commands and `go install` snippet.]

### Run It

[Keep existing "Run It" copy from current README, lines 366-385, unchanged.]

### Publish

[Keep existing "Publish" copy from current README, lines 387-393, unchanged.]

## Verification Tools

[Keep existing copy from current README, lines 394-440, unchanged.]

## Development

[Trim existing "Development" copy from current README, lines 441-480, by ~50%. Move detailed lint setup to AGENTS.md; keep a one-paragraph pointer in the README.]

## Credits

- **Peter Steinberger** ([@steipete](https://github.com/steipete)) - [discrawl](https://github.com/steipete/discrawl) and [gogcli](https://github.com/steipete/gogcli) set the bar. The quality scoring system is inspired by his work; discrawl's sync architecture directly influenced the printing press templates.
- **Trevin Chow** ([@trevin](https://x.com/trevin)) - [7 Principles for Agent-Friendly CLIs](https://x.com/trevin/status/2037250000821059933) shaped the agent-first template design. Co-builder shipping PRs daily.
- **Ramp** ([@tryramp](https://github.com/ramp-public/ramp-cli)) - Their agent-first CLI inspired auto-JSON piping, --no-input, and --compact output.
- The community filers and contributors whose issues and PRs nudged the catalog forward.

## License

MIT
```

---

## Appendix B: Proposed README — printing-press-library

> Copy below from `# Printing Press Library` to the bottom of the License section into `README.md` at the root of `printing-press-library`. The catalog table assumes launch-morning state (25 CLIs); confirm via `jq '.entries | length' registry.json` and U3 check #3 before commit.

```markdown
# Printing Press Library

Nothing is more valuable than time and money. In a world of AI agents, that's speed and token spend. A well-designed CLI is muscle memory for an agent: no hunting through docs, no wrong turns, no wasted tokens. The [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press) prints those CLIs. This repo is the catalog of CLIs already printed and ready to install.

25 CLIs across 17 categories. 17 ship a full MCP server. Browse them all at [printingpress.dev](https://printingpress.dev).

Three to try first:

- **ESPN** (sniffed, no official API). _"Tonight's NBA playoff games with live score, series state, each team's leading scorer's stat line, and any injury or lineup news from the last 24 hours."_ One call.
- **flight-goat** (Kayak nonstop search plus sniffed Google Flights). _"Non-stop flights over 8 hours from Seattle for 4 people, Dec 24 to Jan 1, cheapest first."_ Two sources, one query.
- **linear-pp-cli** (50ms against a local SQLite mirror). _"Every blocked issue whose blocker has been stuck for a week."_ Compound queries the Linear API can't answer.

## Start here

This repo is itself a Claude Code plugin marketplace. Add it, install the plugin, and you have every CLI in the catalog one slash-command away.

```text
/plugin marketplace add mvanhorn/printing-press-library
/plugin install printing-press-library@printing-press-library
```

After install, the main router skill is:

```text
/ppl
```

Want to print new CLIs from API specs? Install the Printing Press itself too:

```text
/plugin marketplace add mvanhorn/cli-printing-press
/plugin install cli-printing-press@cli-printing-press
```

## Two ways in

The repository and plugin are named `printing-press-library`. The mega-skill you actually use is `/ppl`. That naming split is intentional.

Use `/ppl` when you want **discovery, routing, or installation**. Examples:

```text
/ppl
/ppl sports scores
/ppl install espn cli
/ppl install espn mcp
/ppl espn lakers score
/ppl linear my open issues
```

Use a focused `/pp-<name>` skill when you already know the tool you want. Examples:

```text
/pp-espn lakers score
/pp-flightgoat sea to lax dec 24 to jan 1 nonstop
/pp-weather-goat phoenix forecast
```

`/ppl` is the catalog plus the librarian. Each `/pp-<name>` is a single shelf you reach directly when you don't need help finding it.

## New on launch day

Three CLIs added for launch:

- `stripe-pp-cli` (Payments). Charges, customers, subscriptions, with the same agent-native flags every other Press CLI ships.
- `github-pp-cli` (Developer Tools). Issues, PRs, releases, plus a local SQLite mirror for compound queries the GitHub API can't answer in one call.
- `discord-pp-cli` (Media and Entertainment). The full Discord API surface, with the discrawl-inspired sync layer that started this whole project.

## Catalog

Tools grouped by category, sourced from [`registry.json`](registry.json). Each row links to the tool source and its focused plugin skill.

| Name | Skill | Auth | MCP | Slash install | What it does |
|------|-------|------|-----|---------------|--------------|
| [`agent-capture`](library/developer-tools/agent-capture/) | [`/pp-agent-capture`](skills/pp-agent-capture/SKILL.md) | local only | no | `/ppl install agent-capture cli` | Record, screenshot, and convert macOS windows and screens for agent evidence. |
| [`archive-is`](library/media-and-entertainment/archive-is/) | [`/pp-archive-is`](skills/pp-archive-is/SKILL.md) | none | full | `/ppl install archive-is cli` | Find and create Archive.today snapshots for URLs. |
| [`cal-com`](library/productivity/cal-com/) | [`/pp-cal-com`](skills/pp-cal-com/SKILL.md) | API key | full | `/ppl install cal-com cli` | Manage bookings, schedules, event types, and availability. |
| [`contact-goat`](library/sales-and-crm/contact-goat/) | [`/pp-contact-goat`](skills/pp-contact-goat/SKILL.md) | mixed | full | `/ppl install contact-goat cli` | Cross-source warm-intro graph across LinkedIn, Happenstance, and Deepline with a unified local store. |
| [`discord-pp-cli`](library/media-and-entertainment/discord/) | [`/pp-discord`](skills/pp-discord/SKILL.md) | bot token | full | `/ppl install discord cli` | Discord API plus discrawl-inspired sync of channels and messages into local SQLite. |
| [`dominos-pp-cli`](library/commerce/dominos-pp-cli/) | [`/pp-dominos`](skills/pp-dominos/SKILL.md) | browser login | full | `/ppl install dominos cli` | Order Domino's, browse menus, and track deliveries. |
| [`dub`](library/marketing/dub/) | [`/pp-dub`](skills/pp-dub/SKILL.md) | API key | full | `/ppl install dub cli` | Create short links, track analytics, and manage domains. |
| [`espn`](library/media-and-entertainment/espn/) | [`/pp-espn`](skills/pp-espn/SKILL.md) | none | full | `/ppl install espn cli` | Live scores, standings, schedules, and sports news. |
| [`flightgoat`](library/travel/flightgoat/) | [`/pp-flightgoat`](skills/pp-flightgoat/SKILL.md) | API key optional | full | `/ppl install flightgoat cli` | Search flights, explore routes, and track flights. |
| [`github-pp-cli`](library/developer-tools/github/) | [`/pp-github`](skills/pp-github/SKILL.md) | token | full | `/ppl install github cli` | Issues, PRs, releases plus a local SQLite mirror for compound queries. |
| [`hackernews`](library/media-and-entertainment/hackernews/) | [`/pp-hackernews`](skills/pp-hackernews/SKILL.md) | none | full | `/ppl install hackernews cli` | Browse stories, comments, jobs, and topic slices from Hacker News. |
| [`hubspot-pp-cli`](library/sales-and-crm/hubspot/) | [`/pp-hubspot`](skills/pp-hubspot/SKILL.md) | API key | full | `/ppl install hubspot cli` | Work with contacts, companies, deals, tickets, and pipelines. |
| [`instacart`](library/commerce/instacart/) | [`/pp-instacart`](skills/pp-instacart/SKILL.md) | browser session | no | `/ppl install instacart cli` | Search products, manage carts, and shop Instacart from the terminal. |
| [`kalshi`](library/payments/kalshi/) | [`/pp-kalshi`](skills/pp-kalshi/SKILL.md) | API key | full | `/ppl install kalshi cli` | Trade markets, inspect portfolios, and analyze odds. |
| [`linear`](library/project-management/linear/) | [`/pp-linear`](skills/pp-linear/SKILL.md) | API key | full | `/ppl install linear cli` | Manage issues, cycles, teams, and projects with local sync. |
| [`movie-goat`](library/media-and-entertainment/movie-goat/) | [`/pp-movie-goat`](skills/pp-movie-goat/SKILL.md) | bearer token | full | `/ppl install movie-goat cli` | Compare movie ratings, streaming availability, and recommendations. |
| [`pagliacci-pizza`](library/food-and-dining/pagliacci-pizza/) | [`/pp-pagliacci-pizza`](skills/pp-pagliacci-pizza/SKILL.md) | browser login | partial | `/ppl install pagliacci-pizza cli` | Order Pagliacci and browse public menu and store data without login. |
| [`pokeapi`](library/media-and-entertainment/pokeapi/) | [`/pp-pokeapi`](skills/pp-pokeapi/SKILL.md) | none | full | `/ppl install pokeapi cli` | PokeAPI as an agent-ready knowledge graph plus matchup and team-coverage workflows. |
| [`postman-explore`](library/developer-tools/postman-explore/) | [`/pp-postman-explore`](skills/pp-postman-explore/SKILL.md) | none | full | `/ppl install postman-explore cli` | Search and browse the Postman API Network. |
| [`producthunt`](library/marketing/producthunt/) | [`/pp-producthunt`](skills/pp-producthunt/SKILL.md) | none | full | `/ppl install producthunt cli` | Token-free Product Hunt CLI with local sync and views the website doesn't expose. |
| [`recipe-goat`](library/food-and-dining/recipe-goat/) | [`/pp-recipe-goat`](skills/pp-recipe-goat/SKILL.md) | API key | full | `/ppl install recipe-goat cli` | Find recipes across 37 trusted sites with trust-aware ranking and local cookbook. |
| [`slack`](library/productivity/slack/) | [`/pp-slack`](skills/pp-slack/SKILL.md) | API key | full | `/ppl install slack cli` | Send messages, search conversations, and monitor channels. |
| [`steam-web`](library/media-and-entertainment/steam-web/) | [`/pp-steam-web`](skills/pp-steam-web/SKILL.md) | API key | full | `/ppl install steam-web cli` | Look up Steam players, games, achievements, and stats. |
| [`stripe-pp-cli`](library/payments/stripe/) | [`/pp-stripe`](skills/pp-stripe/SKILL.md) | API key | full | `/ppl install stripe cli` | Charges, customers, subscriptions with agent-native flags throughout. |
| [`trigger-dev`](library/developer-tools/trigger-dev/) | [`/pp-trigger-dev`](skills/pp-trigger-dev/SKILL.md) | API key | full | `/ppl install trigger-dev cli` | Monitor runs, trigger tasks, and inspect schedules and failures. |
| [`weather-goat`](library/other/weather-goat/) | [`/pp-weather-goat`](skills/pp-weather-goat/SKILL.md) | none | full | `/ppl install weather-goat cli` | Forecasts, alerts, air quality, and activity verdicts. |
| [`yahoo-finance`](library/commerce/yahoo-finance/) | [`/pp-yahoo-finance`](skills/pp-yahoo-finance/SKILL.md) | none | full | `/ppl install yahoo-finance cli` | Quotes, charts, fundamentals, options, and watchlists. |

> Showcase trio (Stripe, GitHub, Discord) lands by Sat Apr 26 EOD. If any of the three slips, the table prints 24 rows and the "New on launch day" section drops the missing entry.

## Direct install

You need [Go 1.23+](https://go.dev/dl/).

```bash
go install github.com/mvanhorn/printing-press-library/<path>/cmd/<binary>@latest
```

A few worked examples:

```bash
go install github.com/mvanhorn/printing-press-library/library/media-and-entertainment/espn/cmd/espn-pp-cli@latest
go install github.com/mvanhorn/printing-press-library/library/project-management/linear/cmd/linear-pp-cli@latest
go install github.com/mvanhorn/printing-press-library/library/travel/flightgoat/cmd/flightgoat-pp-cli@latest
```

For the MCP server companion:

```bash
go install github.com/mvanhorn/printing-press-library/library/media-and-entertainment/espn/cmd/espn-pp-mcp@latest
claude mcp add espn-pp-mcp -- espn-pp-mcp
```

If a CLI needs credentials, the focused skill and the per-CLI README document the required environment variables.

## Repo structure

```text
library/
  <category>/
    <tool>/
      cmd/
        <cli-binary>/
        <mcp-binary>/        # when available
      internal/
      README.md
      go.mod
      .printing-press.json
      .manuscripts/

.claude-plugin/
  marketplace.json
  plugin.json

skills/
  ppl/
    SKILL.md
  pp-*/
    SKILL.md                 # generated mirror of library/<.>/SKILL.md

registry.json
```

Each published tool is self-contained: source code, a local README, a `.printing-press.json` provenance manifest, and the manuscripts from the printing run. `skills/pp-*` is a generated mirror of each library `SKILL.md`, produced by `tools/generate-skills/main.go`.

## What endorsed means

Every published tool in this repo has passed:

1. Generation from an API spec or captured interface through the [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
2. Validation checks: build, vet, help, version, plus the structural dogfood and runtime verify gates
3. Provenance capture through `.printing-press.json` and `.manuscripts/`

Some tools are refined after generation. The generated artifacts remain in the tool directory so the provenance stays inspectable.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). For deeper architecture, see [AGENTS.md](AGENTS.md).

## License

MIT
```

---

## Appendix C: Cross-Repo Consistency Checklist (paste into runbook)

Used three times: after U1 commit, after U2 commit, and at 8:55am PT Apr 28 in the launch-day pre-flight.

```bash
set -e

REPO_PP=$HOME/cli-printing-press
REPO_LIB=$HOME/printing-press-library

# 1. Tone rule
for f in $REPO_PP/README.md $REPO_LIB/README.md; do
  if grep -nP "(\xe2\x80\x94|\xe2\x80\x93|--[^\-]|\*\*)" "$f" | grep -v '^[0-9]*:```'; then
    echo "FAIL tone: $f has emdash, endash, or bold outside code"; exit 1
  fi
done

# 2. Counts
LIB_COUNT=$(jq '.entries | length' $REPO_LIB/registry.json)
MCP_COUNT=$(find $REPO_LIB/library -type d -name "*-pp-mcp" | wc -l | tr -d ' ')
CAT_COUNT=$(ls $REPO_PP/catalog/*.yaml | wc -l | tr -d ' ')

grep -q "$LIB_COUNT CLIs" $REPO_PP/README.md  || { echo "FAIL: PP README CLI count != $LIB_COUNT"; exit 1; }
grep -q "$LIB_COUNT CLIs" $REPO_LIB/README.md || { echo "FAIL: Library README CLI count != $LIB_COUNT"; exit 1; }
grep -q "$MCP_COUNT" $REPO_LIB/README.md       || echo "WARN: MCP count $MCP_COUNT not found verbatim in Library README"
grep -q "$CAT_COUNT APIs" $REPO_PP/README.md   || echo "WARN: Catalog count $CAT_COUNT not found verbatim in PP README"

# 3. Install commands match
INSTALL=$HOME/launch-assets/install-commands.txt
while IFS= read -r line; do
  [ -z "$line" ] && continue
  if ! grep -qF "$line" $REPO_PP/README.md $REPO_LIB/README.md; then
    echo "FAIL install: '$line' not found in either README"; exit 1
  fi
done < "$INSTALL"

# 4. Cross-links resolve
for url in https://printingpress.dev https://github.com/mvanhorn/cli-printing-press https://github.com/mvanhorn/printing-press-library; do
  curl -sIL "$url" | head -1 | grep -q "200" || { echo "FAIL link: $url"; exit 1; }
done

echo "PASS: README consistency check"
```

---
