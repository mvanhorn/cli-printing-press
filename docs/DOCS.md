# Doc Authoring Rules

These rules cover `AGENTS.md` and the extracted developer docs it points to.

## Editing `AGENTS.md`

The "Code & Comment Hygiene" rules apply to docs too:

- No dates, incidents, or ticket numbers in rules. Justification belongs in the PR introducing the rule, not embedded in it.
- Do not defend the doc's structure inside the doc. Write the rule and the pointer; skip narration about why the file is arranged that way.
- Make rules applicable at the moment they fire. Keep the inline `AGENTS.md` rule command-shaped: trigger, required action or prohibition, concrete values, then the pointer to the longer doc.
- Examples should be generic or anti-pattern-shaped, not lifted from the specific incident that prompted the rule.

## Pointer-rot rule

When an extracted doc's applicability changes, update the inline trigger sentence in `AGENTS.md` in the same PR. Applicability changes include a new fire condition, a removed fire condition, or a changed prohibition, enum, file path, test name, or required value.

## Developer-doc surface

The extracted developer docs are:

- `docs/GOLDEN.md` — golden harness rubric and fixture conventions
- `docs/GLOSSARY.md` — naming conventions, disambiguation defaults, and the implementation reference behind the concepts in `CONCEPTS.md`
- `docs/RELEASE.md` — release-please / goreleaser flow
- `docs/ATTRIBUTION.md` — creator + contributors attribution model
- `docs/CATALOG.md` — catalog validator rationale and wrapper-only shape
- `docs/ARTIFACTS.md` — local library, manuscripts, and public-library flow
- `docs/CODEX.md` — installing and using Printing Press skills in Codex
- `docs/CURSOR.md` — using printed CLIs and skills in Cursor
- `docs/DOCS.md` — this doc-authoring guidance
- `docs/PLUGIN-DEV.md` — persistent local plugin development setup
