## Phase 5: Write the retro

The retro document is the durable audit trail — keep all fields below. The
GitHub issue body in Phase 6 will use a slim subset (action-shaped fields
only); the full triage rationale lives here, in the doc that gets uploaded as
an artifact and linked from the issue. See
`../references/issue-template.md` for the issue-body shape.

Write the full retro document using this template:

```markdown
# Printing Press Retro: <API name>

## Session Stats
- API: <name>
- Spec source: <public-library/browser-sniffed/docs/HAR>
- Scorecard: <score>/100 (<grade>)
- Verify pass rate: <X>%
- Fix loops: <N>
- Manual code edits: <N>
- Features built from scratch: <N>

## Findings

### 1. <Title> (<category>)
- **What happened:** ...
- **Scorer correct?** Yes / No / Partially. [details]
- **Root cause:** Component + what's specifically wrong
- **Cross-API check:** Would this recur?
- **Frequency:** every API / most / subclass:<name> / this API only
- **Fallback if the Printing Press doesn't fix it:** ...
- **Worth a Printing Press fix?** ...
- **Inherent or fixable:** ...
- **Durable fix:** ...
- **Test:** How to verify (positive + negative)
- **Evidence:** Session moment that surfaced this
- **Related prior retros:** *(from Phase 3 Step D; "None" if no matches)*
  - `<api-slug>` retro #<issue-num-if-known> — `aligned` / `contradicts` / `extends`. <one-sentence note on what changed or what's shared>
  - ...

## Prioritized Improvements

### P1 — High priority
| Finding | Title | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---------|-------|-----------|-----------|---------------------|------------|--------|

### P2 — Medium priority
| Finding | Title | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---------|-------|-----------|-----------|---------------------|------------|--------|

*Omit empty priority sections.*

### Skip
| Finding | Title | Why it didn't make it (Step B / Step D / Step G) |
|---------|-------|--------------------------------------------------|

*Findings that survived Phase 2.5 triage but failed Phase 3 — name the specific
step that failed (e.g., "Step B: only 2 APIs with evidence" / "Step G: case-against
stronger; mostly per-CLI"). Empty if every Phase 3 candidate filed.*

### Dropped at triage
| Candidate | One-liner | Drop reason |
|-----------|-----------|-------------|

*Candidates rejected at Phase 2.5. One line each. Reasons: `iteration-noise` /
`printed-CLI` / `API-quirk` / `unproven-one-off` / `raised-N-times`. If this
section is empty, re-check Phase 2.5 — almost every retro has some.*

## Work Units
(see Phase 5.5)

## Anti-patterns
- ...

## What the Printing Press Got Right
- ...
```

Save the retro to manuscript proofs (always) and to the temp retro scratch
directory (always). Do not save retro documents under the source repo's
`docs/retros/` directory; the skill must work the same way for users who do not
have the repo checked out, and retro documents are issue artifacts rather than
durable repo docs.

```bash
RETRO_STAMP="$(date +%Y%m%d-%H%M%S)"
RETRO_PROOF_PATH="$PRESS_MANUSCRIPTS/$API_NAME/$RUN_ID/proofs/$RETRO_STAMP-retro-$CLI_NAME.md"
RETRO_SCRATCH_DIR="/tmp/printing-press/retro"
RETRO_SCRATCH_PATH="$RETRO_SCRATCH_DIR/$RETRO_STAMP-$API_NAME-retro.md"
mkdir -p "$(dirname "$RETRO_PROOF_PATH")" "$RETRO_SCRATCH_DIR"
```

Write the full retro document to `$RETRO_PROOF_PATH`, then copy that file to
`$RETRO_SCRATCH_PATH`. This must complete before Phase 6 Step 1 copies the
manuscripts directory to staging.

### Scrub the retro doc immediately after writing

The retro doc is preserved in `manuscripts/<api>/<run>/proofs/` (durable),
copied to `/tmp/printing-press/retro/` (scratch), and read by future runs'
Phase 3 Step D dedup scan. If a finding's "What we observed" block pasted
unredacted scanner output, dogfood payloads, or Greptile review comments, the
secret/PII propagates into all three locations. Run the Layer 0 body scrub
from `references/secret-scrubbing.md` immediately after writing the doc, so
the scrubbed version becomes canonical:

```bash
# Define scrub_body once at the top of the Phase 5/6 bash blocks (full source
# in references/secret-scrubbing.md Layer 0). Then:
RETRO_PROOF_PATH_SCRUBBED="${RETRO_PROOF_PATH}.scrubbed.md"
if ! scrub_body "$RETRO_PROOF_PATH" "$RETRO_PROOF_PATH_SCRUBBED"; then
  echo "" >&2
  echo "ERROR: retro doc contains an unredacted vendor-prefix secret." >&2
  echo "Open $RETRO_PROOF_PATH, redact each match reported above using" >&2
  echo "  <REDACTED:<vendor>-<kind>:<first4>...<last4>:<len>ch>" >&2
  echo "per references/secret-scrubbing.md Layer 0, then re-run /printing-press-retro." >&2
  exit 1
fi
mv "$RETRO_PROOF_PATH_SCRUBBED" "$RETRO_PROOF_PATH"
cp "$RETRO_PROOF_PATH" "$RETRO_SCRATCH_PATH"
```

Hard-fail behavior is intentional: vendor-prefix secrets are unrecoverable
leaks once a retro doc gets archived or uploaded. The agent must hand-redact
and re-run rather than silently shipping the leak. PII patterns (real emails,
phones, account inboxes) auto-redact in place because the substitution is
lossless for the retro's purpose.

Next: phases/07-plannable-work-units.md
