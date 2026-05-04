# docs(cli): consolidate AGENTS.md via progressive disclosure

## Summary

- Extract long, situational `AGENTS.md` content into focused developer docs under `docs/`.
- Replace extracted sections with command-shaped inline rules that keep the trigger, prohibition/action, and concrete values visible at write time.
- Correct three stale facts inline to match current code/tests: version files, catalog `example` category, and the wrapper-only `spec_url` / `spec_format` carve-out.

## Paragraph Mapping

| Old lines | Old paragraph | Destination |
|---|---|---|
| 1-1 | Title | `AGENTS.md` title |
| 3-3 | `## Machine vs Printed CLI` | `AGENTS.md` -> `## Machine vs Printed CLI` |
| 5-5 | Machine vs printed CLI intro | `AGENTS.md` -> `## Machine vs Printed CLI` |
| 7-8 | Machine vs printed CLI bullets | `AGENTS.md` -> `## Machine vs Printed CLI` |
| 10-10 | `Rules:` label | `AGENTS.md` -> `## Machine vs Printed CLI` |
| 12-15 | Machine-change rules | `AGENTS.md` -> `## Machine vs Printed CLI` |
| 17-17 | Systemic vs specific finding loop | `AGENTS.md` -> `## Machine vs Printed CLI` |
| 19-19 | `### Anti-reimplementation` | `AGENTS.md` -> `### Anti-reimplementation` |
| 21-21 | Anti-reimplementation intro | `AGENTS.md` -> `### Anti-reimplementation` |
| 23-23 | `Reject:` label | `AGENTS.md` -> `### Anti-reimplementation` |
| 25-28 | Reject list | `AGENTS.md` -> `### Anti-reimplementation` |
| 30-30 | `Carve-outs:` label | `AGENTS.md` -> `### Anti-reimplementation` |
| 32-36 | Carve-out list | `AGENTS.md` -> `### Anti-reimplementation` |
| 38-38 | Enforcement note | `AGENTS.md` -> `### Anti-reimplementation` |
| 40-40 | `## Agent-Native Surface` | `AGENTS.md` -> `## Agent-Native Surface` |
| 42-42 | Agent-native surface intro | `AGENTS.md` -> `## Agent-Native Surface` |
| 44-44 | `### Default: expose; skip rules are exceptions` | `AGENTS.md` -> same heading |
| 46-46 | Cobra-tree mirror intro | `AGENTS.md` -> same heading |
| 48-50 | Three skip cases | `AGENTS.md` -> same heading |
| 52-52 | Store-population commands stay exposed | `AGENTS.md` -> same heading |
| 54-54 | When in doubt, expose | `AGENTS.md` -> same heading |
| 56-56 | `### Tool safety annotations` | `AGENTS.md` -> same heading |
| 58-58 | MCP hint intro | `AGENTS.md` -> same heading |
| 60-60 | Automatic-annotation label | `AGENTS.md` -> same heading |
| 62-64 | Annotation emission bullets | `AGENTS.md` -> same heading |
| 66-66 | Read-only template-time rule | `AGENTS.md` -> same heading |
| 68-68 | Wrong annotations are worse than missing ones | `AGENTS.md` -> same heading |
| 70-70 | `### Typed exit-code verification` | `AGENTS.md` -> same heading |
| 72-72 | Typed exit-code intro | `AGENTS.md` -> same heading |
| 74-76 | Cobra annotation code block | `AGENTS.md` -> same heading |
| 78-78 | Exit-code help-block rule | `AGENTS.md` -> same heading |
| 80-80 | `## Build, Test & Lint` | `AGENTS.md` -> same heading |
| 82-87 | Build/test/lint command block | `AGENTS.md` -> same heading |
| 89-89 | Hook and lint paragraph | `AGENTS.md` -> same heading |
| 91-91 | `go fmt` paragraph | `AGENTS.md` -> same heading |
| 93-93 | Relative build-output rule | `AGENTS.md` -> same heading |
| 95-95 | `## Golden Output Harness` | `AGENTS.md` -> `## Generator Output Stability` + `docs/GOLDEN.md` |
| 97-97 | Golden harness intro | `docs/GOLDEN.md` |
| 99-99 | Golden harness purpose | `docs/GOLDEN.md` |
| 101-101 | Golden trigger list | `AGENTS.md` -> `## Generator Output Stability` |
| 103-103 | Identical-behavior verify rule | `docs/GOLDEN.md` |
| 105-105 | Intentional-change update rule + never-update-to-mask rule | `AGENTS.md` -> `## Generator Output Stability` + `docs/GOLDEN.md` |
| 107-107 | Deterministic/offline/auth-free constraints | `docs/GOLDEN.md` |
| 109-109 | Coverage-limit paragraph | `docs/GOLDEN.md` |
| 111-111 | `Decision rubric:` label | `docs/GOLDEN.md` |
| 113-115 | Decision-rubric bullets | `docs/GOLDEN.md` |
| 117-117 | How to add a case | `docs/GOLDEN.md` |
| 119-119 | Contract-shaped artifact rule | `docs/GOLDEN.md` |
| 121-121 | `golden-api.yaml` maintenance rule | `docs/GOLDEN.md` |
| 123-123 | Verify-failure handling | `docs/GOLDEN.md` |
| 125-125 | Golden-vs-other-checks paragraph | `docs/GOLDEN.md` |
| 127-127 | `## Project Structure` | `AGENTS.md` -> `## Project Structure` |
| 129-140 | Project-structure bullet list | `AGENTS.md` -> `## Project Structure` (extended with new doc pointers) |
| 142-142 | `## Glossary` | `AGENTS.md` -> `## Naming and Disambiguation` + `docs/GLOSSARY.md` |
| 144-144 | Glossary intro | `docs/GLOSSARY.md` |
| 146-146 | Use canonical term / ask-before-acting rule | `AGENTS.md` -> `## Naming and Disambiguation` + `docs/GLOSSARY.md` |
| 148-148 | Use “the Printing Press” in skills/user-facing output | `AGENTS.md` -> `## Naming and Disambiguation` + `docs/GLOSSARY.md` |
| 150-150 | Subsystem-name guidance | `AGENTS.md` -> `## Naming and Disambiguation` + `docs/GLOSSARY.md` |
| 152-152 | Default-disambiguation label | `AGENTS.md` -> `## Naming and Disambiguation` + `docs/GLOSSARY.md` |
| 154-157 | Library/publish/manifest/catalog defaults | `AGENTS.md` -> `## Naming and Disambiguation` + `docs/GLOSSARY.md` |
| 159-198 | Full glossary term table | `docs/GLOSSARY.md` |
| 200-200 | `## Commit Style` | `AGENTS.md` -> `## Commit Style` |
| 202-202 | Commit-style format rule | `AGENTS.md` -> `## Commit Style` |
| 204-204 | Scopes label | `AGENTS.md` -> `## Commit Style` |
| 206-211 | Scope table | `AGENTS.md` -> `## Commit Style` |
| 213-213 | `main` reservation | `AGENTS.md` -> `## Commit Style` |
| 215-215 | Scope requirement / PR Title action | `AGENTS.md` -> `## Commit Style` |
| 217-217 | Breaking-change rule | `AGENTS.md` -> `## Commit Style` |
| 219-224 | Version-bump rules | `AGENTS.md` -> `## Commit Style` |
| 226-226 | PR-title enforcement paragraph | `AGENTS.md` -> `## Commit Style` |
| 228-228 | `## Versioning & Release` | `AGENTS.md` -> `## Versioning` + `docs/RELEASE.md` |
| 230-230 | Release flow intro | `docs/RELEASE.md` |
| 232-234 | Three release steps | `docs/RELEASE.md` |
| 236-239 | Version-file rule | `AGENTS.md` -> `## Versioning` (corrected to two files plus marketplace prohibition) |
| 241-241 | `TestVersionConsistencyAcrossFiles` note | `AGENTS.md` -> `## Versioning` (expanded to include `TestMarketplaceJSONHasNoPluginVersion`) |
| 243-243 | `## Adding Catalog Entries` | `AGENTS.md` -> same heading + `docs/CATALOG.md` |
| 245-249 | Catalog validation bullets | `AGENTS.md` -> same heading (corrected with wrapper-only carve-out and `example` test-only category) + `docs/CATALOG.md` |
| 251-251 | `## Testing` | `AGENTS.md` -> `## Testing` |
| 253-253 | Test-file update rule | `AGENTS.md` -> `## Testing` |
| 255-255 | Add-tests guidance | `AGENTS.md` -> `## Testing` |
| 257-257 | `go test ./...` requirement | `AGENTS.md` -> `## Testing` |
| 259-259 | `## Quality Gates` | `AGENTS.md` -> `## Quality Gates` |
| 261-261 | Seven quality gates | `AGENTS.md` -> `## Quality Gates` |
| 263-263 | `## Local Artifacts` | `AGENTS.md` -> `## Local Artifacts` + `docs/ARTIFACTS.md` |
| 265-265 | Local-artifacts intro | `docs/ARTIFACTS.md` |
| 267-269 | Library/manuscripts/runstate bullets | `AGENTS.md` -> `## Local Artifacts` + `docs/ARTIFACTS.md` |
| 271-271 | API slug derivation rule | `AGENTS.md` -> `## Local Artifacts` + `docs/ARTIFACTS.md` |
| 273-273 | `-pp-` infix explanation | `docs/ARTIFACTS.md` |
| 275-275 | `## Public Library` | `docs/ARTIFACTS.md` |
| 277-277 | Public-library intro | `docs/ARTIFACTS.md` |
| 279-279 | Local-to-public flow | `docs/ARTIFACTS.md` |
| 281-281 | Local-vs-public divergence label | `docs/ARTIFACTS.md` |
| 283-284 | Expected/unexpected divergence bullets | `docs/ARTIFACTS.md` |
| 286-286 | Durable-artifact vs working-copy rule | `docs/ARTIFACTS.md` |
| 288-288 | `## Internal Skills` | `AGENTS.md` -> `## Internal Skills` |
| 290-290 | Internal-skills intro | `AGENTS.md` -> `## Internal Skills` |
| 292-292 | Global-install lead-in | `AGENTS.md` -> `## Internal Skills` |
| 294-296 | Install command block | `AGENTS.md` -> `## Internal Skills` |
| 298-298 | Global-install result | `AGENTS.md` -> `## Internal Skills` |
| 300-300 | `## Skill Authoring` | `AGENTS.md` -> `## Skill Authoring` |
| 302-302 | Skill-update rule | `AGENTS.md` -> `## Skill Authoring` |
| 304-304 | `docs/SKILLS.md` pointer | `AGENTS.md` -> `## Skill Authoring` |
| 306-306 | `## Code & Comment Hygiene` | `AGENTS.md` -> `## Code & Comment Hygiene` |
| 308-308 | `### Write-time defaults` | `AGENTS.md` -> same heading |
| 310-317 | Write-time defaults bullet list | `AGENTS.md` -> same heading |
| 319-319 | `### Pre-commit: scan the diff` | `AGENTS.md` -> same heading |
| 321-324 | Diff-scan bullet list | `AGENTS.md` -> same heading |
| 326-326 | `## Editing AGENTS.md` | `AGENTS.md` -> `## Editing AGENTS.md` + `docs/DOCS.md` |
| 328-328 | Editing-AGENTS intro | `AGENTS.md` -> `## Editing AGENTS.md` + `docs/DOCS.md` |
| 330-333 | Editing-AGENTS rules | `docs/DOCS.md` |
| 335-335 | `## Patterns` | `AGENTS.md` -> `## Patterns` |
| 337-337 | Patterns pointer paragraph | `AGENTS.md` -> `## Patterns` |

## Scoped Non-Skill Anchor Audit

- No `AGENTS.md#...` anchor references remain in active non-skill docs/code after excluding planning and PR-draft directories.
- One prose reference remains in `internal/pipeline/reimplementation_check.go`: it says the `// pp:novel-static-reference` opt-out is documented in `AGENTS.md`. That reference is still correct because the anti-reimplementation directive stayed inline in `AGENTS.md`.
- Result: the non-skill anchor audit is clean; no broken anchor or prose references needed retargeting.

## Validation

- `AGENTS.md` line count: 155
- Required inline corrections applied:
  - versioning now names exactly two version locations and explicitly forbids per-plugin versions in `.claude-plugin/marketplace.json`
  - catalog rules now mention `example` as test-only
  - catalog rules now make `spec_url` / `spec_format` conditional on the wrapper-only carve-out
- `go test ./...` passed
- `scripts/golden.sh verify` passed
