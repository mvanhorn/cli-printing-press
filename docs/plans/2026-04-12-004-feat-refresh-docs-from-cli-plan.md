# feat: refresh-docs — regenerate README and SKILL.md from the shipped CLI

**Date:** 2026-04-12
**Scope:** machine (new subcommand + source introspection + template wiring)
**Commit scope:** `cli`

## Problem

The current docs pipeline is **spec-driven**:

```
spec → research → absorb → templates → README.md / SKILL.md
```

This works when the shipped CLI matches the spec. It breaks as soon as the CLI diverges from the spec — which is **the normal case for any CLI worth shipping**:

- Emboss cycles layer transcendence features (watchlist, portfolio perf, digest, compare, sparkline in yahoo-finance; morning_brief, breathe, compare, watch in weather-goat; tldr, pulse, since, algolia in hackernews).
- Polish cycles remove dead code.
- Manual PRs add bug fixes, new flags, improved auth flows.

The docs describe what the spec + research **predicted** the CLI would be. They drift further from reality with every post-gen change.

Re-running `printing-press generate` regenerates the code from scratch, which **strips transcendence**. For our 10 launch CLIs that regenerate would delete 8–24 .go files per CLI — the very files that make each CLI valuable. Unacceptable.

`printing-press emboss` preserves transcendence by design but is a full skill-driven cycle (research + gap analysis + improve + re-verify) per CLI. Heavyweight. And it commits code changes too — which isn't what we need when the only thing we want to refresh is the docs.

We need a **docs-only refresh** that reads the shipped CLI as ground truth.

## The insight

Flip the source of truth:

```
shipped CLI code    (commands, flags, file organization)
  +
git history         (what was added when, temporal ordering)
  +
PR descriptions     (rationale, "why it matters", trigger phrase hints)
  +
research.json       (competitor context, positioning)
  +
manuscripts         (original brief, retro findings)
         ↓
    absorb LLM pass (synthesis, not invention)
         ↓
    template render → README.md + SKILL.md  (nothing else touched)
```

The absorb pass shifts from **inventing narrative from a spec** to **summarizing grounded facts from code + history**. Much lower hallucination risk. Lower LLM cost. Reproducible.

## Non-goals

- **Not a replacement for emboss.** Emboss adds new capabilities. refresh-docs only rewrites docs.
- **Not a spec regenerator.** Will not refresh `cmd/` or `internal/` Go source.
- **Not a provenance rewriter.** `.printing-press.json` is frozen per generation; refresh-docs doesn't touch it.
- **Not a polish replacement.** Polish still handles dead-code removal.
- **Not for greenfield CLIs.** First-time generation still goes through `generate`.

## Design decisions (locked)

| # | Decision | Rationale |
|---|---|---|
| D1 | New top-level subcommand: `printing-press refresh-docs` | Parallel to `generate` / `emboss` / `polish`; discoverable via `--help` |
| D2 | Only writes `README.md` and `SKILL.md` | Narrow blast radius; predictable; safe to run repeatedly |
| D3 | Command discovery via Go AST parsing of `internal/cli/*.go` | Richer than `--help` walking (captures file groupings, annotations, flags, inline examples) |
| D4 | PR history via `gh api` when available, git log as fallback | Best signal layered gracefully; works without GitHub auth |
| D5 | LLM absorb pass with `--no-llm` deterministic fallback | LLM richens the narrative; fallback still ships bug-fixes + SKILL.md |
| D6 | Classification of base vs. transcendence via `.printing-press.json` `generated_at` timestamp | Anything committed after generated_at is transcendence; deterministic rule |
| D7 | Never modifies anything outside `README.md` and `SKILL.md` (enforced by test) | Safety invariant for retroactive refresh |
| D8 | Integration with `emboss` — emboss calls `refresh-docs` as its final step | Keeps emboss outputs docs-accurate; no new manual step |

## Sources of truth (priority order)

**Primary (ground truth — required):**

1. **Shipped CLI code.** Extracted via Go AST from `internal/cli/*.go`:
   - Every `cobra.Command` construct → `Use`, `Short`, `Long`, `Example`, `Annotations`
   - Flags: `cmd.Flags().StringVar(...)` → name, default, description
   - File path per command → grouping hint (commands in same file likely cluster)
   - Resolves the full command tree, including subcommands
   - **Authoritative for what the CLI actually does.** Only commands present here appear in rendered docs.

**Primary (narrative — strongly preferred when available):**

2. **research.json** (via `--research-dir`):
   - `alternatives[]` → source credits (rendered as-is)
   - `competitor_insights` → positioning (rendered as-is)
   - `novel_features[]` — for each planned feature: `name`, `command`, `description`, `rationale`. Cross-referenced with shipped Cobra tree; matches provide narrative enrichment. Unmatched planned features are dropped with audit log.
3. **Brief markdown** (`<manuscripts>/research/*-brief.md`):
   - "Top Workflows" section → direct source for `TriggerPhrases[]`
   - "Product Thesis" paragraph → source for `Headline` + `ValueProp`
   - "Data Layer" section → context for local-state transcendence features
   - "Auth" section → source for `AuthNarrative`
   - "Table Stakes" + competitor sections → helps distinguish base from transcendence

   **For our 10 retroactive CLIs, the brief already contains the narrative content #186 wants to generate.** The LLM's job is summarize-and-restructure, not invent.

**Secondary (enrichment — graceful fallback):**

4. **Git history for the CLI path.** `git log --follow --pretty=format:'%H|%aI|%s' -- <cli-subpath>`:
   - Commit hashes, author dates, subjects
   - `AddedAt` per file → classify base-vs-transcendence via `generated_at` cutoff
   - For each commit, `gh api /repos/<owner>/<repo>/commits/<sha>/pulls` resolves PR # (if `gh` authenticated)
5. **PR descriptions.** `gh pr view <N> --json title,body`:
   - Title → feature-level framing for post-gen transcendence
   - Body → rationale, use cases, trigger phrases for features not covered in the brief
6. **Retro + shipcheck markdown** (`<manuscripts>/proofs/*`):
   - Retro findings → surface polish rationale
   - Shipcheck results → understand what the generator flagged

**Graceful degradation.** Only #1 is strictly required. Any subset of #2–6 may be absent; the LLM (or `--no-llm` fallback) adapts. Empirically for our 10: #1, #2, #3 present for all; #4, #5 available for published CLIs; #6 variable.

## Command specification

### Signature

```
printing-press refresh-docs [flags]

Flags:
  --dir string            Path to the shipped CLI directory (required)
  --research-dir string   Pipeline directory with research.json and discovery/ (optional)
  --git-repo string       Git repo containing the CLI, for commit/PR history (optional)
  --git-path string       Path to CLI within the git repo (default: derived from --dir)
  --since string          Only read commits after this timestamp (default: reads .printing-press.json generated_at)
  --github-repo string    GitHub owner/repo for PR enrichment (default: derived from git remote)
  --no-llm                Skip the absorb pass; use deterministic fallback
  --dry-run               Print proposed README/SKILL content without writing
  --json                  Machine-readable output summary
```

### Phases

**Phase 1: Discover commands from CLI.**
Parse `<dir>/internal/cli/*.go` with `go/parser`. For each `cobra.Command` composite literal, extract the fields. Build a `CommandTree` rooted at the top-level `NewRootCmd` (or equivalent).

Output:
```go
type Command struct {
    Path        string   // e.g., "portfolio perf"
    Short       string
    Long        string
    Example     string
    Flags       []Flag
    File        string   // source file; grouping hint
    AddedAt     time.Time // from git blame of file
    IsTranscendence bool  // AddedAt > generated_at
}
```

**Phase 2: Collect git + PR signal.**
For each source file, run `git log --follow --diff-filter=A --pretty=format:'%H|%aI'` to find creation commit. For each creation commit:
- `gh api /repos/<owner>/<repo>/commits/<sha>/pulls --jq '.[0].number'` resolves PR.
- If found: `gh pr view <N> --json title,body` fetches content.
- Cache to a tempfile keyed by commit SHA (stable within a run).

Degrade gracefully:
- No `--git-repo` → skip this phase entirely.
- `gh` not authed → use commit messages as fallback rationale.
- PR not found → use commit message.

**Phase 3: Classify, group, and validate.**
For each command:
- `IsTranscendence = AddedAt > ProvenanceGeneratedAt` (from `.printing-press.json`)
- Group = {file-based → LLM refines} OR {PR-based → all commands added in same PR share a group}

For each planned novel feature (from `research.json.novel_features[]`):
- Attempt to match against a shipped command via **path-aware matching** (not leaf-only — see [issue #191](https://github.com/mvanhorn/cli-printing-press/issues/191) for why the existing dogfood leaf-matcher under-reports). Full command paths are matched with hyphen-prefix tolerance.
- **Matched:** the shipped command picks up the planned feature's `Name`, `Description`, and `Rationale` as narrative enrichment.
- **Unmatched planned:** DROPPED. Logged to stderr as `planned-but-not-built: <feature>`. Never rendered.
- **Unmatched shipped (no planned counterpart):** still rendered. Description falls back to cobra `Short`; rationale falls back to PR body first line. Log as `shipped-without-plan: <command>` for audit.

This is the ground-truth validation invariant. The shipped CLI is authoritative for what appears in docs; the manuscripts provide enrichment only.

**Phase 4: Synthesize narrative (absorb-style pass).**
Feed the LLM a structured JSON payload:
```json
{
  "cli": "yahoo-finance-pp-cli",
  "commands": [
    {
      "path": "portfolio perf",
      "short": "Compute unrealized P&L...",
      "added_at": "2026-04-08T...",
      "is_transcendence": true,
      "file": "watchlist.go",
      "pr": {
        "number": 42,
        "title": "Add portfolio tracking with SQLite local state",
        "body": "Users wanted to track multiple lots..."
      }
    }
  ],
  "competitors": [...from research.json...],
  "brief_summary": "...from manuscripts/brief.md..."
}
```
Prompt: "Emit a `ReadmeNarrative` JSON. Ground every claim in the provided data. Do not invent commands or features that aren't present."

Parse the response, validate schema, retry once on parse failure.

**Phase 5: Render templates.**
Build a `generator.Generator` populated with:
- `NovelFeatures` = transcendence-classified commands with narrative fields from phase 4
- `Sources` = research.json alternatives
- `Narrative` = phase 4 output
- `Auth` / `Config` / `Resources` = derived from the command tree (structural only)

Execute `readme.md.tmpl` and `skill.md.tmpl`. Write atomically (`os.WriteFile` to a temp, then rename).

### Deterministic fallback (`--no-llm`)

If `--no-llm` is set, phase 4 is replaced by:
- `Narrative = nil`
- `NovelFeatures[].WhyItMatters = <first line of PR body, if any>`
- `NovelFeatures[].Example = <Example field from cobra command, if set>`
- Templates render in their existing fallback mode (no bold headline, no value-prop paragraph)

Still delivers: 4 bug fixes, SKILL.md emission, correct command list from actual code, source credits. Narrative enrichment is just absent.

### Safety invariants (enforced by tests)

1. `refresh-docs` MUST NOT modify any file outside `README.md` and `SKILL.md`. Test: run against a pristine CLI copy, assert `git status` shows only those two files modified.
2. `refresh-docs` MUST NOT break `go build`. Test: run → `go build ./cmd/...` → assert success.
3. `refresh-docs --dry-run` MUST NOT write any file. Test: run against a scratch dir, assert no mtime changes.
4. `refresh-docs` MUST be idempotent (modulo clock-sensitive fields). Test: run twice, diff should be empty.
5. LLM output MUST pass schema validation. Test: feed a fixture payload, assert parses + required fields present.
6. **Every rendered command MUST exist in the shipped Cobra tree.** Test: parse the rendered README + SKILL, extract every `command` reference, assert each is present in the AST-discovered command paths. Planned-but-not-built features are dropped; shipped commands without a planned rationale are kept with a fallback description. This is the hard validation contract — a feature the CLI doesn't actually ship MUST NOT appear in docs.
7. **Planned-but-not-built features MUST be logged to stderr.** Operators need to audit which planned features didn't make it into the shipped code so they can decide: follow up with an emboss cycle to add them, or delete them from research.json as abandoned. Silent dropping hides regressions.

## Implementation plan

### Subtask breakdown

| # | Task | File(s) | Est. size |
|---|---|---|---|
| 1 | Plan doc (this file) | `docs/plans/2026-04-12-004-...md` | S |
| 2 | Cobra AST command-tree parser | `internal/docparse/commands.go` + test | M |
| 3 | Git-log + gh PR fetcher | `internal/docparse/history.go` + test | M |
| 4 | Transcendence classifier | `internal/docparse/classify.go` + test | S |
| 5 | Absorb prompt + schema validator | `internal/docrefresh/absorb.go` + test | M |
| 6 | `refresh-docs` CLI subcommand wiring | `internal/cli/refresh_docs.go` + test | S |
| 7 | Safety-invariant integration test | `internal/cli/refresh_docs_test.go` | M |
| 8 | Idempotency test | `internal/cli/refresh_docs_test.go` | S |
| 9 | Docs: update AGENTS.md glossary with the new subcommand | `AGENTS.md` | S |
| 10 | `emboss` integration: call refresh-docs as Step 5.5 | `internal/cli/emboss.go` | S |

### Dependencies

```
1 (plan) → 2, 3, 4    (can parallelize)
2, 3, 4 → 5           (absorb needs all three inputs)
5 → 6                 (CLI wires absorb into command)
6 → 7, 8              (tests require the subcommand)
6 → 9, 10             (docs + emboss integration after core ships)
```

### Testing strategy

- **Unit.** Each parser + classifier tested against golden fixtures committed to `testdata/refresh-docs/`.
- **Integration.** A fixture CLI at `testdata/refresh-docs/yahoo-like/` with a cobra tree, synthetic git history, a fixture research.json, and a golden expected README + SKILL. Run refresh-docs in `--no-llm` mode; diff against golden. Re-run with LLM stubbed; diff against second golden.
- **Safety invariants.** Enforced via the 5 tests above.
- **Real CLI smoke test.** Run against a staged copy of `~/Code/printing-press-library/library/commerce/yahoo-finance/`, inspect output manually. Not asserted automatically (output depends on LLM) but documented as a manual acceptance test.

## Rollout plan for the 10 launch CLIs

Once `refresh-docs` ships:

1. **Per-CLI dry-run + diff review.** For each of the 10, run `refresh-docs --dry-run` and inspect the proposed README + SKILL diff. Flag any narrative hallucinations or missing commands.
2. **Commit per-CLI PRs** against the library repo. One PR per CLI, reviewable individually. Title format: `docs(<api>): refresh README and SKILL from shipped CLI`. Keeps the launch-blast-radius small and allows rollback per CLI.
3. **Publish-time hook (optional later).** Add `--refresh-docs` flag to `publish package` that runs refresh-docs before staging. Keeps future publishes self-healing.
4. **instacart, hubspot, steam-web.** Separate workstreams. instacart needs a full generate (no manuscripts). hubspot and steam-web need generator bug fixes before either `generate` or `refresh-docs` will succeed on them.

## Alternatives considered

### Alternative: `--help`-walker instead of Go AST

**Pros:** No source parsing; works on compiled binary alone.
**Cons:** Only surfaces `Short` descriptions (`Long` isn't in the standard `--help` output). Can't see flag declarations' inline comments. Can't see file groupings. Can't see `Annotations`.
**Decision:** Rejected. Go AST gives strictly more signal for similar complexity.

### Alternative: Full emboss on every CLI

**Pros:** Preserves transcendence by design (that's what emboss does). Produces docs in sync with code naturally.
**Cons:** Much more expensive — full research + gap analysis + improve + re-verify per CLI. Also modifies code, not just docs, which isn't what we need here.
**Decision:** Rejected for docs-only refresh. Emboss stays the tool for capability additions.

### Alternative: Manual README edits per CLI

**Pros:** Highest fidelity; a human authors the final copy.
**Cons:** Doesn't compound. Doesn't produce SKILL.md at the structural level. Scales poorly beyond 10 CLIs.
**Decision:** Rejected as the primary path. May remain a post-refresh polish step for ones that need extra care.

### Alternative: Accept thin regen (lose transcendence)

**Pros:** Zero new code; use existing `generate`.
**Cons:** Strips valuable functionality from every CLI. Blocked by user feedback.
**Decision:** Rejected outright.

### Alternative: SKILL.md-only ship, skip README refresh entirely

**Pros:** Safest short-term move. Delivers #61 value. Ships today.
**Cons:** Leaves the 4 README bugs in place indefinitely. Doesn't close the drift between docs and reality.
**Decision:** Worth doing as a parallel track. SKILL-only ship unblocks the downstream copy path; refresh-docs is the proper long-term fix. Both compound; neither blocks the other.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| LLM invents features not in the CLI | Structured prompt constrains output to provided commands; schema validation rejects unknown command paths; post-processing validates every rendered command is in the cobra tree |
| `gh` auth missing on user machine | Graceful fallback to commit messages; warning printed once; no hard failure |
| AST parser breaks on unusual cobra patterns | Start with common patterns (composite literals, `Use:` / `Short:` / `Long:` / `Example:`); emit warnings for unparseable files; ship with fixture coverage for existing 10 CLIs |
| Output writes mid-run on interrupt leave partial files | Atomic write via temp-file + rename; no partial state possible |
| Regressions in existing generate flow | refresh-docs lives in a new package; no changes to generator.go |
| User runs refresh-docs on a CLI with uncommitted changes | No-op guard: if the repo is dirty and `--git-repo` is set, warn and require `--force` |

## Open questions

- **Should refresh-docs write to the library repo directly, or only to the local CLI dir?** Default to the local CLI dir. Publishing to library is a separate step.
- **Should the absorb prompt be versioned?** Yes — include a prompt version in `.printing-press.json` updates? Actually no; `.printing-press.json` stays frozen. But cache absorb responses keyed by input hash so reruns are idempotent.
- **Should refresh-docs emit a change-log entry when narrative changes substantially?** Out of scope for v1. Could be a v2 feature.

## Related work (not blocking)

- [Issue #188](https://github.com/mvanhorn/cli-printing-press/issues/188) — hubspot multi-spec duplicate-method generator bug. Blocks `generate`-based refresh on hubspot. refresh-docs sidesteps this by not calling generate.
- [Issue #189](https://github.com/mvanhorn/cli-printing-press/issues/189) — steam-web promoted-endpoint int/string type mismatch. Blocks `generate`-based refresh on steam-web. refresh-docs sidesteps this.
- [Issue #190](https://github.com/mvanhorn/cli-printing-press/issues/190) — instacart has no local manuscripts. Will use `refresh-docs --no-llm` as its path once this plan ships.
- [Issue #191](https://github.com/mvanhorn/cli-printing-press/issues/191) — dogfood's novel-feature matcher undercounts built features (6 of 8 missed on yahoo-finance) because it uses leaf-only flat-map matching. refresh-docs uses path-aware matching to avoid the same class of bug. Fixing #191 improves the spec-driven flow's README accuracy for every new CLI; not blocking this plan but valuable parallel work.

## Success criteria

- `refresh-docs` on all 10 launch CLIs produces README + SKILL that:
  - Pass all 5 safety invariants
  - List every cobra command actually in the CLI (no missing commands)
  - List no commands that aren't in the CLI (no hallucinated commands)
  - Include the 4 template bug fixes
  - Emit a SKILL.md that the downstream `generate-skills` (library repo PR #61) will copy verbatim
- LLM-enriched narrative is qualitatively better than deterministic fallback when spot-checked on yahoo-finance (the canonical case)
- Integration into emboss leaves emboss green on CI

## Out of scope

- Refreshing `.manuscripts/` artifacts (frozen snapshots)
- Refreshing `.printing-press.json` provenance (frozen per generation)
- Refreshing generated Go code (use emboss or polish)
- Automatic publishing to library repo (manual step post-refresh)
- Schema changes to research.json (beyond what #186 already added)
