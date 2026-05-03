# Novel Features Subagent

> **When to read:** This file is referenced by Phase 1.5 Step 1.5c.5 of the
> printing-press skill. It owns the input bundle contract, the Task subagent
> invocation, the subagent prompt template, and the output parsing rules for
> novel-feature brainstorming.

## Invariants

The subagent spawns once per absorb step, in every printing-press run. The
only legal non-spawn outcome is the pre-flight HALT defined below.

- A missing prior `research.json` is a first print, not a skip signal — the
  subagent still spawns; only Pass 2(d) (reprint reconciliation) is omitted.
- A complete-looking prior manifest is not a skip signal either — Pass 2(d)
  re-scores prior features against the current personas, which is the
  entire reason the reprint case exists. Strong priors make the spawn more
  valuable, not less.
- Hand-curating from a prior manifest, falling back to inline brainstorming
  in SKILL.md, or disclosing a skip in the gate showcase are violations of
  this contract, not exits from it.
- Run the prior-research discovery snippet below as written. Do not
  substitute a hand-eyeballed `ls` of the manuscripts directory; the
  snippet's path (`$PRESS_MANUSCRIPTS/<api-slug>/*/research.json`) is at the
  run-id level, not inside the `research/` subdirectory.

## Pre-flight check

Before spawning, confirm the Phase 1 brief has both:
- a populated `Users` section with concrete user types, AND
- a populated `Top Workflows` section with named rituals.

If either is missing or pro-forma ("developers", "general users", "anyone using
the API"), do NOT spawn the subagent. The brainstorm will fabricate plausible
personas instead of grounding in research. Loop back to Phase 1 user research
and return here.

## Input bundle

Assemble these values before spawning. Substitute into the prompt template
below.

| Variable | Value |
|----------|-------|
| `${BRIEF_PATH}` | `$RESEARCH_DIR/<stamp>-feat-<api>-pp-cli-brief.md` — also embeds DeepWiki findings (`## Codebase Intelligence`) and user briefing (`## User Vision`) when present |
| `${ABSORB_MANIFEST_CONTENT}` | Inline markdown: the absorbed-features table built in Step 1.5b. The on-disk manifest at `<stamp>-feat-<api>-pp-cli-absorb-manifest.md` is not written until Step 1.5d, so paste the table content directly into the prompt. |
| `${RUBRIC_PATH}` | Absolute path to `references/absorb-scoring.md` (the scoring rubric) |
| `${API_SPEC_SUMMARY}` | Inline 1-paragraph summary: API surface (endpoint families), auth shape, NOI domain |
| `${PRIOR_RESEARCH_PATH}` | Result of the prior-research discovery snippet below, or the literal string `none` |

**Prior-research discovery.** Run before assembling the bundle to set
`${PRIOR_RESEARCH_PATH}`:

```bash
PRIOR_RESEARCH=none
if [ -f "$PRESS_LIBRARY/<api>/research.json" ]; then
  PRIOR_RESEARCH="$PRESS_LIBRARY/<api>/research.json"
elif [ -d "$PRESS_MANUSCRIPTS/<api-slug>" ]; then
  CANDIDATE=$(ls -1t "$PRESS_MANUSCRIPTS/<api-slug>"/*/research.json 2>/dev/null | head -1)
  [ -n "$CANDIDATE" ] && PRIOR_RESEARCH="$CANDIDATE"
fi
```

User vision and DeepWiki findings are not separate variables — they live
inside the brief. The subagent detects their presence by checking for the
`## User Vision` and `## Codebase Intelligence` sections in
`${BRIEF_PATH}`.

## Subagent invocation

One Task tool call. Do not split passes across multiple invocations — the cut
pass must see the candidates it generated.

```
Agent({
  description: "Novel-features brainstorm + adversarial cut",
  subagent_type: "general-purpose",
  prompt: <Subagent prompt template below, with ${...} placeholders substituted>
})
```

## Subagent prompt template

````
You are a novel-features brainstorming agent for the Printing Press, a system
that generates CLIs for APIs. Your job is to propose the differentiating
features for ONE printed CLI and then ruthlessly cut the weak ones.

You will run THREE PASSES IN ORDER. The third pass is not optional and is not
a polish pass — it is the cut.

## Inputs (read these before Pass 1)

- Brief (read from disk):  ${BRIEF_PATH}
- Scoring rubric (read):   ${RUBRIC_PATH}            (kill/keep checks, score dimensions, table format, reprint verdicts)
- Prior research.json:     ${PRIOR_RESEARCH_PATH}    (read from disk if real path; literal `none` means first print)
- API spec summary:        ${API_SPEC_SUMMARY}       (inline)

Inline absorb manifest (features ALREADY covered — do not re-propose):

${ABSORB_MANIFEST_CONTENT}

The brief may contain `## User Vision` and `## Codebase Intelligence`
sections. Detect their presence after reading the brief; they gate Pass 2
sources (e) and (f).

If `${PRIOR_RESEARCH_PATH}` is a real path, this is a REPRINT and Pass 2 step
(d) is mandatory.

## Pass 1: Customer pass

Write down WHO the user of this CLI is BEFORE proposing any features. Output
under heading `## Customer model`.

- 2-4 named personas. NOT "developers" or "users" — concrete people with
  concrete habits drawn from the brief's Users + Top Workflows sections.
  Good: "Someone who checks HN every morning before standup."
  Good: "A hiring manager scanning Who's Hiring threads monthly."
  Bad:  "API users."
- For each persona, write three short paragraphs:
  - **Today (without this CLI):** What do they do today to accomplish their
    weekly ritual with this API? What tabs do they have open? What scripts do
    they re-run? What questions can they NOT answer?
  - **Weekly ritual:** The repeated workflow that defines their relationship
    with this service.
  - **Frustration:** The single most tedious or impossible part of the ritual.

If you cannot do this from the brief alone — if you find yourself inventing
personas to keep going — STOP and return ONLY:

```
## Failure: brief lacks user research

The Phase 1 brief does not contain enough Users / Top Workflows detail to
ground a customer model. Re-run Phase 1 user research before spawning this
subagent again.
```

Do not proceed to Pass 2 with fabricated personas.

## Pass 2: Generate pass

Output under heading `## Candidates (pre-cut)`. Generate ~2× the features you
expect to ship — aim for 8-16 candidates total.

Sources for candidates (label each candidate with its source):

(a) **Persona-driven:** For each persona's frustration from Pass 1, propose
    the one-command feature that resolves it.

(b) **Service-specific content patterns:** What unique content types or
    workflows define this service's identity (e.g., HN's Show HN / Ask HN /
    Who's Hiring; Spotify's Discover Weekly / Wrapped; GitHub's
    Discussions; TMDb's Collections / Watch Providers)? Propose features
    that exploit each.

(c) **Cross-entity local queries:** What joins across synced tables produce
    insights no single API call can?

(d) **Reprint reconciliation** (only if `${PRIOR_RESEARCH_PATH}` is a real
    path): Load the prior `novel_features` and `novel_features_built`
    arrays. For each prior feature, score it against the current personas
    using the rubric. Add to candidates with verdict tag `prior-keep`,
    `prior-reframe`, or `prior-drop`. The prior list is candidate input,
    not gospel and not noise.

(e) **User briefing** (only if the brief contains a `## User Vision`
    section): Propose features that directly serve the user's stated vision
    but are not already covered by the absorb manifest.

(f) **DeepWiki** (only if the brief contains a `## Codebase Intelligence`
    section): Propose features that exploit internal data relationships,
    queue/worker patterns, or event systems that the public API docs don't
    suggest.

For each candidate capture: name, command, one-line description, persona
served, source label from the list above.

Apply the rubric's kill/keep checks (LLM dependency, external service, auth
gap, scope creep, verifiability, reimplementation) inline. Reframe or cut
obvious failures NOW so they don't waste Pass 3 attention.

## Pass 3: Adversarial cut pass

This is the pass that exists because brainstorms without it produce flabby
lists. Output under heading `## Survivors and kills`.

For EVERY surviving candidate, force-answer these in writing:

1. **Weekly use:** Would the named persona actually run this command at
   least weekly? "Monthly", "occasionally", or "depends" is a soft kill.
2. **Wrapper vs leverage:** Is this a thin renaming of one API endpoint
   the user could call directly with a generic client? If yes, kill it —
   wrappers do not justify transcendence rows.
3. **Transcendence proof:** Does this feature get its power from local
   SQLite, a cross-source join, agent-shaped output, or a service-specific
   content pattern? If the answer is "none of the above", it is not
   transcendent.
4. **Sibling kill:** Name the closest candidate you killed and why. If
   you cannot, you didn't generate enough candidates — return to Pass 2
   and add more.

Drop ~half. Target output: 4-8 survivors. Score survivors with the rubric's
4-dimension score; only keep features scoring >= 5/10.

For every killed candidate, record a one-sentence kill reason. Killed
candidates are PART of the output, not silent.

## Output contract

Return a single markdown document with these top-level sections, in this
order:

1. `## Customer model` — personas from Pass 1.
2. `## Candidates (pre-cut)` — full Pass 2 list with source labels and
   inline kill/keep verdicts from the rubric.
3. `## Survivors and kills`
   - `### Survivors` — features scoring >= 5/10, formatted as a
     transcendence table per the rubric's "Transcendence Table Format"
     section. Include score, persona-served, and the one-sentence
     buildability proof per the rubric.
   - `### Killed candidates` — table with columns: feature, kill reason,
     closest-surviving-sibling.
4. `## Reprint verdicts` (REPRINT ONLY) — per-prior-feature: keep / reframe
   / drop, with one-line justification, per the rubric's reprint verdict
   rules.

Do not include any other sections. Do not summarize. Do not editorialize.
Do not propose follow-up work.
````

## Output handling in SKILL.md

After the subagent returns:

1. **Parse `### Survivors`** — these become the transcendence rows in the
   absorb manifest (Step 1.5d). The score and buildability proof go into the
   transcendence table; the persona-served column is the audit trail.
2. **Parse `## Reprint verdicts`** (if present) — record dropped prior features
   under the transcendence table per the rubric's reprint surface rule, so the
   user can override drops at the Phase 1.5 gate review.
3. **Audit trail** — save the full subagent response (including `## Customer
   model`, `## Candidates (pre-cut)`, `### Killed candidates`) to
   `$RESEARCH_DIR/<stamp>-novel-features-brainstorm.md`. The Customer model and
   Killed candidates do NOT go into the manifest, but they MUST be persisted
   for retro/dogfood debugging.
4. **Failure handling** — if the subagent returned the `## Failure: brief lacks
   user research` envelope, HALT Phase 1.5 immediately. Do not fall back to
   inline brainstorming. Surface the failure to the user and re-run Phase 1
   user research.
