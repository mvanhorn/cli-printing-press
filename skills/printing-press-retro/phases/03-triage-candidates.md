## Phase 2.5: Triage candidates

Before Phase 3 spends deep analysis on each candidate, run a fast triage to drop
candidates that don't justify the deeper look. **Most candidates should die here.**
The retro is a filter, not a funnel — if everything from Phase 2 makes it to Phase
3 unchanged, triage isn't doing its job.

For each candidate, ask in order:

1. **Was this iteration noise?** Normal trial-and-error during generation —
   one-off retry, typo recovery, agent forgetting a flag, transient network blip. Drop.
2. **Is this a printed-CLI fix?** The fix lives in `$PRESS_LIBRARY/<api>/`
   and helps only this one CLI. If the proposed change is "edit this command in
   this CLI" or "regenerate after fixing the spec," it's not a retro finding — it's
   a polish pass on that CLI. Drop.
3. **Is this an upstream API quirk?** The vendor returns null instead of 404, or
   ignores a query param the docs claim to honor, or has rate limits the spec
   doesn't declare. The Printing Press doesn't fix vendors. If the only fix is
   "work around this in the generator for every CLI," that's almost always wrong;
   if it's "let one CLI work around it," that's a printed-CLI fix. Drop.
4. **Is the only evidence "I noticed this once"?** A one-time observation that you
   can't connect to a recurring pattern across other CLIs is a candidate for Drop,
   not a filed issue. "I want to record this somewhere" belongs in the local
   retro doc, not in GitHub.
5. **Does the same finding appear in 2+ prior retros without being implemented?**
   Don't re-raise at the same priority. Either drop it (the cost-benefit math has
   been "no" twice and the retro is becoming a wishlist), or reframe as a smaller
   incremental fix that addresses part of the friction. Search:
   `grep -l "<finding keywords>" "$PRESS_MANUSCRIPTS"/*/proofs/*-retro-*.md`
6. **Would weekday maintainer triage close this?** Drop wishlists, skill/docs
   polish, catalog polish, lint/gosec hygiene, one-vendor quirks, one-CLI
   grab-bags, already-fixed behavior, theoretical collisions, and conveniences
   that do not affect printed-CLI users or shipping gates.

Survivors of these six questions go to Phase 3. Dropped candidates are recorded
as one-line entries in the retro's "Dropped at triage" section — they exist for
your own discipline check and for the maintainer to see triage actually ran.

**Anti-pattern to avoid.** A recent Pagliacci retro produced *"Skip: None. Every
finding warrants action."* That sentence is the failure mode this triage exists
to prevent. Two of those findings (snake_case in `Use:`, root.go `Short:` rewrite
that the SKILL already documents as a manual step) were classic per-CLI / instructional
candidates that should have been dropped here. If you find yourself writing
"every finding warrants action," stop and re-run triage.

Next: phases/04-classify-findings.md
