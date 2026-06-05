# Attribution: creator + contributors

A printed CLI's attribution is a single permanent **`creator`** plus a multi-valued **`contributors[]`**, both `spec.Person{handle, name}` on `APISpec` and `CLIManifest`. This doc is the full model; keep it in sync with the resolver, validation, and legacy-field contract, and update the inline `AGENTS.md` trigger in the same PR when their applicability changes.

## The dual-key Person

Keep the `handle` / `name` split *inside* each `Person`; never conflate them into one string.

- `handle` — the slug-safe GitHub @handle. Anchors the README/NOTICE byline link (`[@handle](https://github.com/handle)`; the byline renders only when `handle` is set) and the legacy slug-form copyright-header recovery regex (`copyrightOwnerRe`).
- `name` — the prose display name. Drives the current copyright header `Copyright YYYY <name> and contributors.` and its recovery regex (`copyrightCreatorRe`), the `RewriteOwner` header-token swap, the SKILL `author:`, the NOTICE credit, and the byline's parenthetical name.

## Creator is permanent

The creator is the human who first got the CLI accepted into the library. Never reassign it on a reprint or contribution; it is top-billed everywhere. The copyright header is `Copyright YYYY <creator name> and contributors.` — the `and contributors` suffix is constant regardless of count (zero included), so it never churns golden fixtures.

## Contributors accrue only via deliberate flows

Contributors are added only by the contribution flows (`publish` / `amend` / `reprint`), via `cli-printing-press contributors add`:

- Idempotent: skips the creator and anyone already listed; `--front` prepends the reprinter.
- A plain `generate --force` / `mcp-sync` / sweep **preserves** the list and never appends the operator.
- The same-lineage guard (`api_name` + spec checksum) plus a non-nil-empty explicit-clear signal keep a cross-API `--force` from resurrecting another CLI's list.

## Manifest is the source of truth

Resolution prefers the manifest over re-derivation so regens by others don't overwrite attribution. The resolver falls back manifest-creator → legacy `printer`/`printer_name` → `owner`/`owner_name` → copyright header → git config. `New()` bridges in-memory legacy fields into the creator (printer in full; owner as **name-only**, so the vendor slug never leaks into the byline).

## Validation is layered

- `Generate()` soft-validates the creator-derived legacy fields (`OwnerName`, `Printer`): a stderr warning plus a slug-shaped fallback when they are empty, never fatal, so tests and `mcp-sync` / `regen-merge` keep working.
- The publish path strictly rejects an empty creator handle or the sentinel handles `"USER"` / `"user"`.

## Transition window: legacy fields are dual-written

`owner` / `owner_name` / `printer` / `printer_name` are still emitted (derived from `creator`) so older skills and library tooling that read them keep working — this change is additive (`feat`, not breaking). `min-binary-version` stays at the major baseline (`4.0.0`); the additive `contributors add` step degrades gracefully on a binary that predates it. A future major removes the legacy write — that removal is the breaking change.

## Never hand-edit attribution

Never hand-edit `creator` or `contributors[]` (or the NOTICE / byline blocks) in a publish PR — the command and the library's post-merge refresh own them. NOTICE credits both the per-CLI creator/contributors and the Press's own co-creators (Matt Van Horn and Trevin Chow) — distinct concerns.

The library-side sweep that migrates already-published CLIs and backfills contributors from git history lands in `printing-press-library` per the cross-repo lockstep contract.
