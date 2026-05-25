---
status: active
created: 2026-05-24
type: feat
plan_depth: lightweight
primary_repo: mvanhorn/cli-printing-press
secondary_repo: mvanhorn/printing-press-library
predecessor_plan: docs/plans/2026-05-23-002-feat-generator-wide-self-learning-cli-plan.md
gates_phase: 2.5 (between Phase 2 ship and Phase 3 full sweep of predecessor plan)
---

# feat: ESPN learn config — first real-seed pilot

**Primary target repo:** `mvanhorn/cli-printing-press` (this repo, where the espn spec lives).
**Secondary target repo:** `mvanhorn/printing-press-library` (where the swept espn CLI ships).

## Why this plan exists

The predecessor plan (cli-printing-press#2085, printing-press-library#826, #827) shipped the self-learning loop into 5 pilot CLIs with empty `spec.Learn` defaults — recall returns `{found: false}` because there are no taught queries and no entity-lookup seeds. The dogfood window the predecessor plan called for (1-2 weeks of recall/teach traffic) measures nothing in that state.

This plan authors a real, populated `spec.Learn` block for ESPN — the cleanest authoring target across the 5 pilots — so the pilot actually exercises the entity-substitution + pattern-extraction layer the predecessor plan built. The plan's deliverable proves the loop's value end-to-end and creates the worked example for the other 4 pilots + eventually the rest of the library.

## Scope

### In scope

- Author `spec.Learn` block for ESPN in `cli-printing-press`'s espn spec (catalog entry or internal spec, whichever holds the source of truth — check `catalog/espn.yaml` and `internal/spec/testdata/`)
- Populate three fields:
  - `ticker_patterns`: regex patterns for ESPN's stable identifiers (game IDs, athlete IDs, team IDs)
  - `stopwords`: domain-specific filler words for sports queries ("vs", "games", "schedule", "tonight", etc.)
  - `entity_lookup_seeds`: ~49 team rows per league (NFL 32, NBA 30, MLB 30, MLS 30+) covering canonical name + common aliases ("Niners" → "San Francisco 49ers")
- Regenerate espn from the local generator and verify the emitted `learn_init.go` carries the seeds
- Sweep the published espn entry in printing-press-library to update its learn_init.go with the new seeds
- Teach 3-5 real-world queries against the swept binary to validate the recall + pattern-Apply paths work end-to-end against real data
- Stack a follow-up PR on printing-press-library#827 (or land separately after #827 merges) with just the espn changes

### Deferred for later

- The other 4 pilots (contact-goat, company-goat, podcast-goat, instacart) — same authoring exercise, separate per-CLI plans
- Building a `learn:` block authoring guide / template for spec authors generally
- Auto-populating entity_lookup_seeds from a CLI's `sync` data (e.g., espn could auto-seed teams from its already-synced sport data)

### Outside this product's identity

- Manual recall traffic generation (faking dogfood data). Real usage produces meaningful threshold measurements; synthetic data doesn't.

## Implementation Units

### U1. Author spec.Learn block for ESPN

**Goal:** Write the YAML block with ticker patterns, stopwords, and team-roster seeds.

**Files:**
- `cli-printing-press`: the espn spec source file (likely `catalog/espn.yaml` or `internal/spec/testdata/espn.yaml`; verify)
- A scratch reference file documenting the data sources (ESPN public API team list, Wikipedia rosters) so future maintainers can refresh

**Approach:**

Ticker patterns to register:
- Game IDs: `^[0-9]{9}$` (ESPN's 9-digit event IDs)
- Athlete IDs: `^a-[0-9]+$` or similar (check actual format)
- Team IDs: `^t-[a-z0-9]+$` or league-prefix patterns (check)

Stopwords to add:
- "vs", "v.", "vs.", "versus", "tonight", "today", "yesterday", "tomorrow", "weekend"
- "game", "games", "match", "matches", "schedule", "scoreboard"
- "score", "scores", "result", "results", "winner"
- "stats", "standings", "lineup", "roster", "depth"

Seed kinds + entries (~120 rows total):
- `nfl_team`: 32 teams × ~3 aliases each (e.g., `canonical: "San Francisco 49ers"`, `aliases: ["49ers", "Niners", "SF 49ers", "SF"]`)
- `nba_team`: 30 teams × ~3 aliases
- `mlb_team`: 30 teams × ~3 aliases
- `mls_team`: 30+ teams × ~3 aliases

Source the canonical names from ESPN's own team metadata (each league's `/teams` endpoint). Source aliases from common usage + each team's abbreviation.

**Test scenarios:**
- Spec parses (`validateLearn` passes per U1 of predecessor plan)
- Generator emits learn_init.go carrying all the seeds verbatim
- The generated learn_init.go compiles in the printed CLI

### U2. Regenerate ESPN locally + validate

**Goal:** Confirm the spec change flows through the generator to a working binary.

**Files:**
- No source changes — uses existing cli-printing-press from PR #2085 branch
- New output goes to `~/printing-press/library/espn/` (local working library, not the published library)

**Approach:**

```bash
cd ~/cli-printing-press
git checkout feat/generator-wide-self-learning-loop  # or whatever branch PR #2085 lands on after merge
cli-printing-press generate espn
# Inspect ~/printing-press/library/espn/internal/cli/learn_init.go — should have all seeds
cd ~/printing-press/library/espn
go test ./...
HOME=/tmp/espn-test go build -o /tmp/espn-pp-cli ./cmd/espn-pp-cli
HOME=/tmp/espn-test /tmp/espn-pp-cli teach --query "Niners game tonight" --resource <some-espn-event-id> --resource-type events
HOME=/tmp/espn-test /tmp/espn-pp-cli recall "49ers game" --json  # should find the same hit via alias resolution
```

**Test scenarios:**
- Recall an alias not directly taught (teach "Niners game tonight" → recall "49ers game tonight" hits via alias)
- Pattern extraction fires after 3 structurally-similar teaches (e.g., teach Niners + Cowboys + Eagles → pattern emerges for "{nfl_team} game tonight")
- Pattern Apply substitutes correctly (recall "Patriots game tonight" returns the right event via substitution against the seeded NFL roster)

### U3. Sweep the published espn entry

**Goal:** Update printing-press-library's espn so the published CLI carries the new seeds.

**Files:**
- `printing-press-library:library/media-and-entertainment/espn/internal/cli/learn_init.go`
- Possibly `.printing-press.json` for the version stamp

**Approach:**

Build the sweep tool from PR #826's branch. Run against the printing-press-library espn entry. The sweep should regenerate `learn_init.go` with the new seeds and leave everything else untouched (the spec block is the only delta from the previous sweep state).

Commit on a new branch stacked on #827 (or land separately after #827 merges to main). Validate end-to-end:
- `go build`, `go test`
- `HOME=fresh-X ./espn-pp-cli recall "Niners game" --json` — returns the right event via alias resolution against the seeded data

### U4. Author guide for spec.Learn

**Goal:** Document the authoring process so the other 4 pilots (and future CLI authors) can replicate.

**Files:**
- `cli-printing-press:docs/SPEC-LEARN-AUTHORING.md` (new) — guide to authoring `spec.Learn` blocks: which fields, how to source seed data, how to test, common pitfalls

**Approach:**

The guide is a worked example based on ESPN. Sections:
1. When to add a `learn:` block to your spec (vs leaving the package as a benign no-op)
2. Sourcing ticker_patterns (look at the API's identifier shapes)
3. Sourcing stopwords (domain filler words your queries always include)
4. Sourcing entity_lookup_seeds (canonical + aliases, where to find them)
5. Local validation workflow (regenerate, recall smoke test, alias resolution test)
6. Library sweep workflow (when the CLI is already published)

## Verification

**Done when:**
- ESPN's spec.Learn block carries real seed data (~120+ entity rows, 5+ ticker patterns, 10+ stopwords)
- Local regeneration produces a working learn_init.go with the seeds baked in
- A teach against ESPN's published library CLI populates the cache; a recall via alias returns the same hit
- A PR is open with the espn changes (stacked on or after printing-press-library#827)
- The authoring guide exists at `docs/SPEC-LEARN-AUTHORING.md`

## Pickup prompt for fresh window

Paste this into a new Claude Code session to resume:

```
/ce-work /Users/mvanhorn/cli-printing-press/docs/plans/2026-05-24-001-feat-espn-learn-config-pilot-plan.md
```

The predecessor plan at `docs/plans/2026-05-23-002-feat-generator-wide-self-learning-cli-plan.md` carries full context (Phase 1+2 retrospective, 4 bugs found, validation gate ordering). The 3 in-flight PRs (#2085 generator, #826 tool, #827 pilot) hold the implementation work. This plan is small and self-contained — 4 units, mostly spec authoring + one validation sweep.
