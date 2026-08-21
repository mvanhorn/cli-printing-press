## Phase 4: Prioritize

Sort survivors of Phase 3 into three buckets:

- **Do** — survived Phase 3 Step G with a clear case-for and clears P1 or P2
  below. Scorer bugs are just findings like any other — rank them by user or
  shipping impact alongside template gaps and parser issues.
- **Skip** — survived Phase 2.5 triage but didn't clear Phase 3 (Step B couldn't
  name 3 APIs with evidence, Step D recurrence-cost disqualified, or Step G's
  case-against was stronger). State the specific step that failed. These are
  listed in the retro so the maintainer can see what was considered and rejected.
- **Drop** — rejected at Phase 2.5 triage as iteration noise, printed-CLI fix,
  upstream API quirk, unproven one-off, or recurring-not-implemented. Listed as
  one-liners only — they don't need full analysis, they need a record so triage
  is auditable.

No numerical scoring formulas. State the priority reasoning in words.

### Priority rubric

- **P1 — the printed CLI is broken or unsafe.** Generate or regen ships a CLI
  that does not work, or a user of that CLI can lose data, spend money, or skip a
  safety prompt. Includes commands that fail or no-op; silent wrong or missing
  data; broken or fail-open auth; skipped confirmations; regen dropping working
  code; or generated CLIs that will not run or load config. "Produces a broken
  CLI" is P1.
- **P2 — real generalizing defect, but the printed CLI still works.**
  Scorer/dogfood/publish-skill/operator gates that mis-score or block shipping;
  noticeable non-breaking polish such as wrong `which` ranking, a dropped
  `doctor` render, or empty CSV output on zero rows. The defect must generalize
  across CLIs, reproduce on current `main`, and be something that belongs in the
  next weekday fix wave.
- **No P3.** If a survivor is not P1 or P2, move it to Skip or Drop. Do not file
  it and do not apply any low-priority label.

**Sanity check before moving to Phase 5.** Look at the bucket distribution.
Almost every retro should have *some* drops and *some* skips. A retro with
"all Do, no Skip, no Drop" is the failure mode — re-run triage and Step G on
the weakest findings. Likewise, if every Do is P1, you're probably inflating;
force yourself to identify the weakest "Do" and ask whether it is truly a
broken or unsafe printed CLI rather than a P2, Skip, or Drop.

Next: phases/06-write-the-retro.md
