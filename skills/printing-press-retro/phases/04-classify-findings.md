## Phase 3: Classify findings

For each candidate that survived Phase 2.5 triage, answer these seven questions.
Question 5 has seven sub-steps (A through G); Step G is the adversarial check.
Findings that fail Step G do not get a priority and do not go in Do. Put them
in Skip when they survived deep analysis but failed the filing bar, or Drop when
the analysis shows they were triage noise after all.

**1. What happened?** One sentence — the symptom, not the fix.

**2. Is the scorer correct?** (mandatory for score-penalty findings)
- **Scorer correct** → fix the Printing Press (templates, binary, or skill)
- **Scorer wrong** → fix the scoring tool, not the Printing Press
- **Both** → fix both, label which is primary

**3. What category?**

| Category | Description |
|----------|-------------|
| **Bug** | Generated code is wrong |
| **Scorer bug** | Scoring tool reports a false positive |
| **Template gap** | No template for a common pattern |
| **Assumption mismatch** | Printing Press assumes X but API uses Y |
| **Recurring friction** | Happens every generation, might be inherent |
| **Missing scaffolding** | Feature class the Printing Press could emit but doesn't |
| **Default gap** | Printing Press emits a wrong or placeholder default |
| **Discovered optimization** | Improvement found during use |
| **Skill instruction gap** | Skill told Claude wrong thing or missed a step |

### Actionable issue type mapping

Every actionable work unit must carry exactly one real GitHub issue type label:
`bug` or `enhancement`. Derive it deterministically from the absorbed finding
categories:

| Finding category | Issue type |
|------------------|------------|
| Bug | `bug` |
| Scorer bug | `bug` |
| Assumption mismatch | `bug` |
| Default gap | `bug` |
| Template gap | `enhancement` |
| Recurring friction | `enhancement` |
| Missing scaffolding | `enhancement` |
| Discovered optimization | `enhancement` |
| Skill instruction gap | `enhancement` |

If a work unit absorbs multiple findings, `bug` wins when any absorbed category
maps to `bug`; otherwise use `enhancement`. Priority, component, provenance,
and routing/terminal labels are not substitutes for this type label.

**4. Where in the Printing Press does this originate?**

Pick exactly one component. The `slug` column drives the `comp:<slug>` label
applied to the issue when filed (Phase 6), which is how agents filter related
work across retros (`gh issue list --label comp:<slug>`).

| Component | Slug | Path |
|-----------|------|------|
| Generator templates | `generator` | `internal/generator/` |
| Spec parser | `spec-parser` | `internal/spec/` |
| OpenAPI parser | `openapi-parser` | `internal/openapi/` |
| Main skill | `skill` | `skills/printing-press/SKILL.md` |
| Verify/dogfood/scorecard | `scorer` | CLI commands |

If a finding genuinely spans two components, pick the one where the durable
fix lands. Don't multi-label.

**5. Blast radius and fallback cost — should the Printing Press handle this?**

**Step A: Cross-API stress test.** Test across API shapes (standard REST, proxy-envelope,
RPC-style) and input methods (OpenAPI, crowd-sniffed, HAR-sniffed, no spec).

**Step B: Name three concrete APIs from the library with direct evidence.** Not "every
API with multi-word resources" or "any browser-sniffed CLI." Name three specific APIs
already in `$PRESS_LIBRARY/` or the public Printing Press Library where you
can point to evidence the pattern exists: a path in their spec, a known endpoint shape,
a header the vendor documents, an output you can reproduce. "Stripe, Notion, GitHub
probably have this" is hand-waving; "Stripe (Stripe-Version header in spec line N),
GitHub (X-GitHub-Api-Version on the issues endpoints), Linear (api-version on /v2/*)"
is evidence. If you can name only two with evidence — or three with hand-waving — the
finding cannot be filed. Move it to Skip if it otherwise survived deep analysis,
or Drop if the evidence is only a one-off.

**Step C: Counter-check question.** Ask explicitly: "If I implemented this fix, would it
actively hurt any API that doesn't have this pattern?" If yes, the fix needs a guard or
condition before being P1/P2 — not a default change. Example: turning on client-side
`?limit=N` truncation by default would hurt APIs that need server-side pagination for
correctness; it stays P2 only because it's gated on profiler-detected absence of a
paginator. Without that guard the same finding is unsafe to land.

**Step D: Recurrence-cost check.** Search prior retros under
`$PRESS_MANUSCRIPTS/*/proofs/*-retro-*.md` for the same finding. If the same
finding has been raised in 2+ prior retros without being implemented, the prior cost-
benefit math has been "no" twice. Don't re-raise it — either drop it with a
"raised N times, still not justified" note, or reframe the finding into a smaller
incremental fix that now clears the P1/P2 bar. Recurrence without a sharper,
current-main defect is a triage failure, not stronger evidence.

**Capture matched prior retros.** When the search returns hits, record each as a
structured tuple — retro CLI name, retro file path (or GitHub issue number if the
retro file's frontmatter contains one), and a one-word classification:

- `aligned` — the prior retro proposed the same fix direction. Strengthens the case;
  reference it in Step F.
- `contradicts` — the prior retro proposed an *opposing* fix or chose a different
  default. Surface this explicitly: a maintainer reading the new finding must see
  the disagreement. State in one sentence why this retro reaches a different
  conclusion (e.g., "prior retro saw single-paginator APIs; this one saw an
  always-paginated API where the prior default would break").
- `extends` — the prior retro raised an adjacent finding in the same component
  area but a different specific fix. Useful context, doesn't change the case.

These tuples flow forward into the per-finding template ("Related prior retros")
in the retro doc and merge into the issue body's "Related issues" block alongside
the Step 2.5 dedup scan's `related-area` outputs. GitHub auto-cross-links any `#N`
issue number you write, so contradictions and alignments show up in both retro
timelines without further action.

**Step E: Assess fallback cost.** How reliably will Claude catch and fix this across every
future API? A "simple" edit Claude forgets 30% of the time means 30% ship with the defect.

**Step F: Make the tradeoff.** Default is **don't change the machine.** The burden of
proof is on the finding to justify a machine change. Continue to Step G only when all
three of these are true:

(a) Step B named three concrete APIs *with evidence* (not speculation).
(b) Step D's recurrence-cost check didn't disqualify the finding.
(c) Step C's counter-check didn't surface a hurts-other-APIs concern that lacks a guard.

If a finding can't clear all three, it doesn't get a priority — it goes to Drop with
the specific reason ("only named 2 APIs with evidence" / "raised 3 times, still not
justified" / "fix would hurt single-paginator APIs without a guard").

When the finding applies to an API subclass, include: Condition (when to activate),
Guard (when to skip), Frequency estimate.

**Step G: Construct the case against filing.** Before recording the finding, write
1-2 sentences arguing the *opposite* — what makes this look like a printed-CLI
fix, an iteration artifact, or a wishlist item. Why might a maintainer close this
as "works as designed" or "too narrow for a machine fix"? What's the strongest
version of "this shouldn't be filed"?

If the case-against is stronger than the case-for, drop the finding. If they're
roughly even, drop the finding (default direction is don't-file). Only when the
case-for is clearly stronger does the finding survive to Phase 4.

This step is not a formality. It is the explicit place where weak findings die.
A finding that survives Step G should be able to state, in one sentence, why
the case-against fails — and that sentence is worth quoting in the retro entry.

**6. Is this inherent or fixable?** Push hard on whether smarter templates, a
post-processing step, or better spec analysis could eliminate the friction. If inherent,
propose the cheapest mitigation.

**7. What is the durable fix?** Prefer: template fix > binary post-processing > skill instruction.

**Mark uncertainty explicitly.** If you can't confidently isolate one root cause
or one fix, say so — list the candidate causes (or candidate fixes) and how an
implementer could disambiguate before committing. The issue body surfaces this
uncertainty so the agent picking up the work doesn't lock in a wrong-but-plausible
diagnosis. Confidence isn't a virtue when it's manufactured; an honest "either A
or B; verify by X" is more useful than a wrong prescription.

**Strip API-specific details from the proposed fix.** The durable fix must work across
APIs, not just the one that surfaced the finding. If the fix includes hardcoded param
names (e.g., `--sport`, `--league`), date formats (e.g., `YYYYMMDD`), chunking
strategies (e.g., monthly), or domain-specific logic, those are printed-CLI details
leaking into the machine recommendation. The machine fix should be parameterized —
driven by what the profiler detects in the spec, not by what one API happens to need.

Example of the anti-pattern:
- Finding: "ESPN sync needs `--dates` for historical data"
- Bad fix: "Add `--dates` with `YYYYMMDD-YYYYMMDD` format, `--sport`/`--league` flags, and monthly chunking to the sync template"
- Good fix: "When the profiler detects a date-range query param, emit a `--dates` flag that passes the value through to the API"

The bad fix bakes ESPN's date format, scope params, and chunking strategy into the
machine. The good fix lets the profiler drive behavior from the spec.

Next: phases/05-prioritize.md
