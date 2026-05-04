# Local Artifacts and Public Library

Generated artifacts live under the user's home directory, not in this repo.

## Local artifacts

- `~/printing-press/library/<api-slug>/` — local library: printed CLIs the generator has produced. Directory names are keyed by API slug, not CLI name. The binary inside is still `<api-slug>-pp-cli`.
- `~/printing-press/manuscripts/<api-slug>/` — archived research and verification proofs, keyed by API slug. One API can have multiple runs.
- `~/printing-press/.runstate/<scope>/` — mutable per-workspace state such as current run and sync cursors.

The API slug is derived by the generator from the spec title (`cleanSpecName`), not manually chosen. The CLI binary name is `<api-slug>-pp-cli`. Never hardcode an API slug when the generator can derive it; names with periods normalize differently than you'd guess.

The `-pp-` infix exists to avoid colliding with official CLIs. The binary `notion-pp-cli` can coexist with whatever `notion-cli` the vendor ships. The library directory is just `notion/`; the `-pp-cli` suffix appears on binary names, not directory names.

## Public library

The public library is the GitHub repo [`mvanhorn/printing-press-library`](https://github.com/mvanhorn/printing-press-library) — a curated, category-organized catalog of finished printed CLIs. Users install printed CLIs from there.

Local-to-public flow: a successfully generated printed CLI lives in the local library first. The `/printing-press-publish` skill packages a local CLI and opens a PR against the public library repo. Merging that PR is what moves the CLI from "works on this machine" to "users can install it."

The local library and public library can diverge in two ways:

- **Expected divergence.** Some files are intentionally rewritten by the publish step, most notably `go.mod`'s module path. The polish skill's divergence check exempts these.
- **Unexpected divergence.** Local edits since the last publish, such as polish in progress, manual fixes, or `mcp-sync` regen, that have not been pushed. The polish skill's divergence check surfaces these so you can decide whether to republish or discard the local changes.

Treat the public library as the durable artifact and the local library as the working copy. When users hit a bug, they are hitting the public library's version, not whatever is currently in `~/printing-press/library/`.
