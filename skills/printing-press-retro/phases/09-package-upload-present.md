## Phase 6: Package, upload, and present

### Step 1: Package artifacts into staging folder

Read and apply [../references/artifact-packaging.md](../references/artifact-packaging.md)
**through Step 4 only** (create staging dir, copy, scrub, zip). Do not upload or
clean up yet — the staging folder stays alive until the end of Phase 6.

The staging folder (`$STAGING_DIR`) now contains the scrubbed copies and the zips.
This is both the review target and the upload source.

### Step 2: Compute filing plan + confirm before publishing

*This step only runs if the Phase 5.6 issue gate passed (there are Printing Press findings to act on).*

Before showing the confirm prompt, run `references/issue-template.md`
**Steps 1, 2, and 2.5** to ensure labels exist, sort the work units, and
compute the per-WU filing plan via the dedup scan against open
`source:retro` or legacy `retro` issues. Each WU ends up classified as either:

- **File new** — no matching open issue
- **Comment on #N** — Step 2.5 found a `same` match; the new evidence will be added as a comment instead of filing a duplicate
- **File new with related issues** — Step 2.5 found one or more `related-area` matches; the new issue's body will reference them via `#N` in the Related issues block

The dedup scan does not need to be bulletproof. Bias toward "file new"
when uncertain — duplicates are recoverable, miscomments on the wrong
issue are uglier.

Then show the user a summary including the filing plan and ask for
confirmation via `AskUserQuestion`.

> **Ready to submit your retro.**
>
> Here's what will happen on [mvanhorn/cli-printing-press](https://github.com/mvanhorn/cli-printing-press):
>
> **Filing plan:**
>
> | # | Title | Plan | Notes |
> |---|-------|------|-------|
> | 1 | <wu-1 title> | File new (P1, bug, comp:<slug>) | No match |
> | 2 | <wu-2 title> | Comment on #234 | Matches "<existing title>" |
> | 3 | <wu-3 title> | File new + reference #189 | Adjacent open issue |
>
> Each new issue carries `source:retro`, the mapped `bug` or `enhancement` type,
> `priority:P1` or `priority:P2`, and `comp:<slug>` labels — agents filter related work across
> retros with `gh issue list --label source:retro`, `gh issue list --label
> comp:<slug>`, or `gh issue list --label priority:P1`. During the label cutover,
> legacy `retro` issues remain discoverable and a write may use `retro` only when
> the canonical `source:retro` label is not available.
>
> Scrubbed artifact zips uploaded to catbox.moe and linked from each new issue:
>   - **Retro document** — full triage rationale, drops, skips, what went right
>   - **Manuscripts** (<size>) — research brief, shipcheck proof, build logs
>   - **CLI source** (<size>) — the generated Go code (no binary, no vendor/) *(omit if not available)*
>
> Everything is staged at `<$STAGING_DIR>` if you'd like to inspect the files first.

Options:
1. **Submit** — execute the filing plan
2. **Let me review the files first** — I'll check the staging folder, then come back
3. **Save locally only** — skip filing, keep the manuscript proof and temp copy

If the user picks "Let me review the files first," acknowledge and wait. When they
come back, re-ask with Submit / Save locally only.

If the user picks "Save locally only," skip Steps 3 and 4 — the retro is already
saved to manuscript proofs and `/tmp/printing-press/retro/`. Clean up the staging
folder, then jump to Step 6.

If the user picks **Submit**, set `SUBMISSION_CONFIRMED=true` immediately before
running Step 3. Leave it unset or set it to `false` for the other choices; the
artifact-packaging reference refuses public uploads unless this explicit consent
marker and the successful scrub marker are both present.

If the user wants to override a dedup decision before submitting (e.g.,
"file new for WU-2 instead of commenting"), accept the override: clear
`WU_DEDUP[i]` for that WU and proceed.

### Step 3: Upload artifacts

Run artifact-packaging.md Step 5 (the catbox upload) using the zips already in
`$STAGING_DIR`. This produces `$MANUSCRIPTS_URL` and `$CLI_SOURCE_URL`.

### Step 4: Execute the filing plan

Steps 1, 2, and 2.5 of [../references/issue-template.md](../references/issue-template.md)
already ran during Step 2 (filing plan + confirm), so labels exist, the safe
provenance marker is selected, WUs are sorted, and `$WU_DEDUP`, `$WU_RELATED`,
and `$WU_DEPENDENCY_EDGES` are populated. This step runs
**Step 3** of the reference: build bodies and execute the plan in parallel.

The "Execution principles" block at the top of `issue-template.md` is
mandatory: build issue bodies inline (heredocs into shell variables, not
the Write tool), run the whole step in one Bash invocation, and parallelize
the per-WU `gh issue create` / `gh issue comment` calls. Skipping these
costs real wall-clock latency — an N WU retro should finish in a single
round trip's worth of network time, not a serialized stack of them.

Each WU is independent: WUs marked `comment:#N` get a comment on the
existing issue; WUs marked file-new create a new flat top-level issue. No
parent or sub-issue hierarchy — every new issue stands alone in GitHub's issue
list with its own open/close lifecycle. Explicit prerequisites are applied as
native `blocked-by`/`blocking` relationships after issue numbers are known;
ordinary related-area references remain prose.

Each new issue carries its own provenance marker (`source:retro`, or legacy
`retro` only when the canonical label is unavailable), exactly one mapped
`bug`/`enhancement` type, `priority:P1` or `priority:P2`, and `comp:<slug>` labels.
This is what enables `gh issue list --label comp:openapi-parser` to surface
every retro WU in that area across every retro — labels are the cross-retro
discovery surface, not auto-cross-links inside issue bodies.

Each new issue body's **Related issues** block combines:

- Prior-retro references from Phase 3 Step D (alignments, contradictions, extensions across retros)
- `related-area` issue references from Step 2.5 (open issues in adjacent territory)

Both reach across separate filed work where the `#N` auto-cross-link is
real signal. The body does *not* auto-cross-link to sibling WUs in the
same retro; that linkage is noise unless one is genuinely a prerequisite,
which is captured in `Dependencies:` and applied natively rather than left as
prose alone.

If `gh` is not authenticated or every per-WU action fails, follow the
graceful degradation path in the issue-template reference: save locally and
print manual filing instructions. Per-WU partial failures (some succeed,
some don't) are surfaced through `$FAILED_ISSUES` in Step 6.

### Step 5: Local scratch copy

Ensure the temp scratch copy exists. This is the human-friendly local path for
reviewing or manually filing the retro when upload or issue creation fails.

```bash
if [ -f "$RETRO_PROOF_PATH" ]; then
  mkdir -p "$RETRO_SCRATCH_DIR"
  cp "$RETRO_PROOF_PATH" "$RETRO_SCRATCH_PATH"
fi
```

### Step 6: Present results

After issues are created and comments posted, show the user a summary in
priority order. Group `created` and `commented` outcomes — both are real
filed work, but the shape differs.

> **Retro submitted!**
>
> Filed <C> new issue<s>, added <E> comment<s> on existing issues (P1 → P2 order):
>
> *New issues:*
>   - [P1] <title> — <full $OUTCOME_URL[i]>
>   - [P2] <title> — <full $OUTCOME_URL[i]>
>   - ...
>
> *Comments on existing issues:*
>   - [P1] <title> → comment on #234 — <comment URL>
>   - ...
>
> <N> findings across <M> work units. New issues are tagged with `source:retro`,
> their mapped `bug` or `enhancement` type, `comp:<slug>`, and `priority:P1`/`priority:P2`
> labels — agents can filter related work across retros with `gh issue list
> --label source:retro`, `gh issue list --label comp:<slug>`, or `gh issue list
> --label priority:P1`.
> *(if artifacts uploaded)* Artifacts: [retro doc](<URL>) · [manuscripts](<URL>) · [CLI source](<URL>)
> Local copy: <$RETRO_SCRATCH_PATH>

The `[P<n>]` annotation here is presentation-only — the issue titles
themselves do not carry a priority prefix (priority lives on the label).
Showing it in the user-facing summary helps the user scan filed work in
priority order without opening each issue.

Omit either subsection (`New issues:` or `Comments on existing issues:`)
when empty. A retro that produced only comments (every WU matched an
existing open issue) is a good outcome — it means the issue tracker
already covered the findings and the new evidence reinforces them.

If `$FAILED_ISSUES` is non-empty (set by `references/issue-template.md`
Step 3), append a warning block before the closing line:

> ⚠️ Some actions need attention:
>   - <title> — issue creation failed
>   - <title> — comment on #234 failed
>   - ...
>
> File the missing issue(s) or comment(s) manually using the retro doc at <$RETRO_SCRATCH_PATH>.

If filing wasn't completed (user chose local-only, or gh failed entirely),
show the local save paths and the manual filing instructions printed by
the issue-template fallback path.

### Step 7: Clean up staging folder

Run artifact-packaging.md Step 7 to delete `$STAGING_DIR`.

Next: return to the router
