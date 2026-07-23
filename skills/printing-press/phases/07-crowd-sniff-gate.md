## Phase 1.8: Crowd-Sniff Gate

**Receipt entry (required):**

```bash
"$PRINTING_PRESS_BIN" phase-receipt enter --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "07-crowd-sniff-gate"
```

After [Phase 1.7](06-browser-sniff-gate.md) (Browser-Sniff Gate), evaluate whether mining community signals (npm SDKs and GitHub code search) would improve the spec. Skip this gate entirely if the user already passed `--spec` (spec source is already resolved and appears complete).

**Time budget:** The crowd-sniff gate should complete within 10 minutes. If `cli-printing-press crowd-sniff` fails or times out, fall back immediately:
- If a spec already exists: "Crowd-sniff failed — proceeding with existing spec."
- If no spec exists: "Crowd-sniff failed — falling back to --docs generation."

### When to offer crowd-sniff

| Spec found? | Research shows gaps? | Action |
|-------------|---------------------|--------|
| Yes | Yes — competitors or community projects reference more endpoints | **Offer crowd-sniff as enrichment** |
| Yes | No — spec appears complete | Skip silently |
| No | Community SDKs exist on npm | **Offer crowd-sniff as primary discovery** |
| No | No SDKs or code found | Skip — fall back to `--docs` |

### Crowd-sniff as enrichment (spec exists but has gaps)

Present to the user via `AskUserQuestion`:

> "Found a spec with **N endpoints**, but research shows the live API likely has more. Want me to search npm packages and GitHub code for `<api>` to discover additional endpoints? This typically takes 5-10 minutes."
>
> Options:
> 1. **Yes — crowd-sniff and merge** (search npm SDKs and GitHub code, merge discovered endpoints with the existing spec)
> 2. **No — use existing spec** (proceed with what we have)

### Crowd-sniff as primary (no spec found)

Present to the user via `AskUserQuestion`:

> "No OpenAPI spec found for `<API>`. Want me to search npm packages and GitHub code to discover the API from community usage? This typically takes 5-10 minutes."
>
> Options:
> 1. **Yes — crowd-sniff the community** (search npm SDKs and GitHub code, generate a spec from discovered endpoints)
> 2. **No — use docs instead** (attempt `--docs` generation from documentation pages)
> 3. **No — I'll provide a spec or HAR** (user will supply input manually)

### If user approves crowd-sniff

Read and follow [references/crowd-sniff.md](../references/crowd-sniff.md) for the crowd-sniff
command, provenance capture, and discovery report writing.

### If user declines crowd-sniff

Proceed with whatever spec source exists. If no spec was found, fall back to `--docs` or ask the user to provide a spec/HAR manually.

---

Before following `Next:`, record the durable handoff. If this gate was skipped
because a complete `--spec` was supplied, add `--skip --note "<allowed
reason>"`:

```bash
"$PRINTING_PRESS_BIN" phase-receipt complete --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "07-crowd-sniff-gate"
```

Next: phases/08-ecosystem-absorb-gate.md
