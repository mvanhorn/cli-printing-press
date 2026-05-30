# Phase 6: Next Steps — menus & prompt templates

Lazy-loaded body for Phase 6. The dispatcher keeps the not-optional rule, the verdict-precedence Gate, the existing-PR check, and the load-bearing invariants (do-not-improvise-publish; the polish-to-retry invocation form; promote-before-ship-menu on a hold→ship transition). Read this file in full when presenting the Phase 6 menu.

### Ship-path menu

Read the polish result block emitted by Phase 5.5. The menu and recommendation are data-driven so the user is never asked to weigh "Polish again" against "Publish" when polish itself just decided another pass would not help.

**Pick the recommended action:**

| Polish state | Recommendation |
|---|---|
| `ship` + `remaining_issues` empty | **Publish** |
| `ship` + `remaining_issues` non-empty + `further_polish_recommended: yes` | **Polish again** |
| `ship` + `remaining_issues` non-empty + `further_polish_recommended: no` | **Publish** if the remaining issues do not touch the CLI's headline commands; surface the trade-off and let the user pick between **Publish** (as-is; README is not auto-updated) and **Done** otherwise |
| `ship-with-gaps` + `further_polish_recommended: yes` | **Polish again** |
| `ship-with-gaps` + `further_polish_recommended: no` | **Publish** (gaps already in README's `## Known Gaps`; polish's ship logic enforces this for `ship-with-gaps`) or **Done** — agent judgment from the actual gap content |

**Suppress "Polish again" entirely when `further_polish_recommended: no`.** Polish has already decided another pass would not help; offering the option anyway is friction. Surface `further_polish_reasoning` as context so the user sees the call.

**Always available:** Publish, Done. Retro is not on this menu — it is offered as a post-publish tail (see "If Publish now" below).

Present via `AskUserQuestion`. The recommended option leads, carries the `(recommended)` label, and a leading `Recommendation:` line states the call explicitly. Three reinforcing channels so the user does not have to infer from ordering.

**Substitute placeholders before showing the prompt.** The example prompts below use `<api>`, `<N>`, `<score>`, `<pass-rate>`, `<PR_URL>`, `<further_polish_reasoning>`, `$CLI_WORK_DIR`, and `$PRESS_LIBRARY/<api>` as fill-ins. Replace each with the concrete value (the API slug, the actual count, the parsed polish-result string, the expanded shell-variable path, etc.) so the user sees real names and paths, not literal placeholder text. The same rule applies to the hold-path menu below.

**If an existing open PR was found:**

The existing-PR branch still honors polish's `further_polish_recommended` signal. When polish thinks another pass would help, offer it as the recommended action ahead of the PR update — pushing a CLI with closable issues into an existing PR is no better than into a new one.

When `further_polish_recommended: yes`:

> "<api> passed shipcheck. There's an open publish PR (#<PR-number>). Polish flagged <type-qualified summary of remaining issues> as closable — '<further_polish_reasoning>'.
>
> Recommendation: Polish again before updating the PR.
>
> 1. **Polish again** (recommended) — close the remaining issues, then update the PR
> 2. **Update PR #<PR-number>** — push this version to the existing PR as-is
> 3. **No — I'm done**"

When `further_polish_recommended: no` (or `remaining_issues` empty):

> "<api> passed shipcheck. There's an open publish PR (#<PR-number>). Want to update it with this version?"
>
> 1. **Yes — update PR #<PR-number>** (recommended) — re-validate, re-package, and push to the existing PR
> 2. **No — I'm done**"

**Polish converged clean** (`remaining_issues` empty, `further_polish_recommended: no`):

> "<api> passed shipcheck (<score>/100, verify <pass-rate>%). Polish ran cleanly — nothing more to fix.
>
> Recommendation: Publish.
>
> 1. **Publish now** (recommended) — validate, package, and open a PR
> 2. **Done for now**

**Polish thinks another pass would help** (`further_polish_recommended: yes`):

When summarizing remaining issues to the user, type-qualify the count instead of just printing `<N> issues`. Pull the categories from the polish result block (`remaining_issues` entries usually carry their type — verify failure, README gap, MCP description, etc.). For example: "2 verify failures and 1 README gap remain" rather than "3 issues remain."

> "<api> passed shipcheck (<score>/100, verify <pass-rate>%). <type-qualified summary of remaining issues>.
>
> Polish notes: '<further_polish_reasoning>'
>
> Recommendation: Polish again before publishing.
>
> 1. **Polish again** (recommended) — close the remaining issues
> 2. **Publish now** — ship as-is
> 3. **Done for now**

**Polish opted out of recommending more polish** (`remaining_issues` non-empty, `further_polish_recommended: no`):

> "<api> passed shipcheck (<score>/100, verify <pass-rate>%). <type-qualified summary of remaining issues> — polish could not auto-resolve these.
>
> Polish notes: '<further_polish_reasoning>'
>
> Recommendation: <Publish | Done — your call from the gap content>.
>
> 1. **Publish now** — ship as-is. The remaining issues are not auto-added to README; if any are user-facing, you'll need to update the README's `## Known Gaps` section in the publish PR before merging
> 2. **Done for now** — leave the CLI in <expanded $PRESS_LIBRARY/<api> path> and address remaining issues manually before any later publish

This prompt fires only for `ship` verdicts (not `ship-with-gaps`), so polish has not auto-written `## Known Gaps`. Polish auto-writes the section only when the verdict is `ship-with-gaps` (see polish SKILL.md "Ship logic"). On a `ship` verdict with non-empty `remaining_issues`, polish has judged those issues acceptable to publish without a verdict bump — but the user may still want to surface user-facing items in README before merging the PR.

If the shipcheck report contains a `## Known Gaps` block, prepend: "Note: shipcheck documented known gaps (see the shipcheck report above)."

### If "Publish now"

Invoke `/printing-press-publish <api>`. The publish skill handles everything from there — fork, branch, manifest checks, `cli-skills/pp-<api-slug>/SKILL.md` regen, push, and PR creation.

**Do not improvise the publish flow.** Even though the publish skill itself runs `gh repo fork`, `git push`, and `gh pr create --repo mvanhorn/printing-press-library …` internally, running those commands by hand from this phase skips the preflight checks (printer sentinel validation, manifest shape, vendor-spec PII scope, govulncheck on the changed module) and the public library's own `AGENTS.md` requirements that the skill mirrors. The CWD here is `cli-printing-press`, so the public library's `AGENTS.md` is not loaded — the skill is the only entry point that brings those rules into context. If the publish skill fails, fix the underlying issue (or report it as a machine bug); do not bypass it. See [`AGENTS.md`](AGENTS.md) "Publishing to the Public Library" for the full rule.

**After publish returns success**, offer retro as a soft tail — **unless a retro proof already exists for this run** (`ls "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/proofs/"*-retro-*.md` matches, the on-disk artifact `/printing-press-retro` writes when it runs), in which case skip the offer and end normally. Anchor the skip on that file rather than memory — it survives a context-window roll or a mid-session resume, so the decision is the same whether or not the earlier retro is still in context. The publish skill drives its PR to stable green and hands back without offering anything itself; this tail is the only place the ship-path offers retro. It has no business being a peer of publish on the headline menu, but a post-publish optional offer lets users compound learnings without forcing the choice up front. Retro at this point sees the publish step as part of the session it analyzes.

Present via `AskUserQuestion`:

> "PR opened: <PR_URL>. Run a retro? It surfaces systemic gaps from this session (generator misses, scorer bugs, skill-doc drift) as a GitHub issue for the Printing Press maintainers. Every retro filed raises the floor for the next CLI — and your session context is freshest right now."
>
> 1. **No — I'm done** (default)
> 2. **Yes — run retro now**

If the user picks yes, invoke `/printing-press-retro`. The retro skill analyzes the session for generator improvements.

### If "Polish again"

Invoke `/printing-press-polish <api>`. The polish skill runs another diagnostic-fix-rediagnose pass, reports the delta, and offers its own publish at the end with the same data-driven shape used here.

### If "Done for now"

End normally. The CLI is in `$PRESS_LIBRARY/<api>` and the user can run `/printing-press-publish`, `/printing-press-polish`, or `/printing-press-retro` later.

### Hold-path menu

The CLI did not promote to library. The working copy is at `$CLI_WORK_DIR`; manuscripts and proofs are archived. Hold runs are the highest-value retro signal — something blocked the machine from reaching ship, and that signal is most valuable while session context is fresh.

Present via `AskUserQuestion`:

> "<api> couldn't pass shipcheck — <one-line reason from the shipcheck report or polish result>. The working copy is at <expanded $CLI_WORK_DIR path> and was not added to the library. What do you want to do?"
>
> 1. **Run retro** (recommended) — capture what blocked ship so the Printing Press maintainers can fix it for the next CLI you generate
> 2. **Polish to retry** — run another polish pass and try again to reach ship
> 3. **Done for now**

Default the recommendation to **Run retro**. Override to **Polish to retry** when the polish result block specifically says another pass is likely to close the gap (`further_polish_recommended: yes`) — that signal means the CLI is on hold not because the machine is structurally short, but because the last polish pass ran out of time on issues it can plausibly close.

#### If "Run retro"

Invoke `/printing-press-retro`. The retro skill analyzes the session for generator improvements.

#### If "Polish to retry"

**Invoke polish via the Skill tool with `$CLI_WORK_DIR` as the arg:**

```
Skill(
  skill: "cli-printing-press:printing-press-polish",
  args: "$CLI_WORK_DIR"
)
```

Three reasons for this exact form, all mirroring Phase 5.5:

1. **Pass `$CLI_WORK_DIR` (absolute path), not the slug.** Hold runs leave the CLI in the working directory because Phase 5.6 did not promote — `$PRESS_LIBRARY/<slug>/` either does not exist or holds a stale prior run, and a slug-form invocation would polish that stale copy.
2. **Use the Skill tool (forked context), not the `/printing-press-polish` slash command.** This matches Phase 5.5's invocation pattern — same shape, same expectations. Slash-command invocations auto-enable polish's standalone mode (Publish Offer fires); the Skill tool form defers to the parent unless `--standalone` is passed explicitly. Main SKILL owns the menu on this path.
3. **Do not include `--standalone` in `args`.** The flag is what polish gates its Publish Offer on (see polish SKILL.md "Publish Offer"). On the hold path the CLI has not been promoted; firing the offer would open a public PR for an un-promoted, un-shipped working copy.

After polish returns, parse the result block and act on the new `ship_recommendation`:

- **Polish landed on `ship` or `ship-with-gaps`** — the verdict transitioned out of hold. The working copy is still un-promoted; the library is stale. Run promote, then route to the ship-path menu (above):

  ```bash
  cli-printing-press lock promote --cli <api>-pp-cli --dir "$CLI_WORK_DIR"
  ```

  Then re-enter the ship-path menu using polish's new result block. Skip the Phase 5.6 acceptance-gate JSON check — that gate was already satisfied when this run originally reached Phase 5.6, and polish does not regenerate it.

- **Polish still on `hold`** — re-show this hold-path menu so the user can pick again. Do not loop polish automatically; the user may want retro or to give up after a failed retry.

#### If "Done for now"

End normally. The working copy stays in `$CLI_WORK_DIR` for potential future retry.
