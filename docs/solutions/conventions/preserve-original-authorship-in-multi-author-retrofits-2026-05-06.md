---
title: "Preserve original authorship in multi-author retrofits: don't trust the operator's git config when sweeping content others created"
date: 2026-05-06
category: conventions
module: cli-printing-press-generator
problem_type: convention
component: tooling
severity: high
applies_when:
  - "Writing a one-shot or recurring sweep tool that mutates content across many entries created by different people"
  - "The natural-feeling default would be to attribute every entry to whoever's running the sweep (via `git config user.name`, `whoami`, etc.)"
  - "Original authorship is recoverable from existing artifacts (copyright headers, git history, README bylines) — and getting it wrong burns trust with the actual authors"
  - "The published output ships with an `author:` field, copyright notice, or other attribution surface that downstream consumers will see"
tags:
  - retrofit
  - authorship
  - sweep-tool
  - cross-repo
  - attribution
  - cross-repo-lesson
related_components:
  - tooling
  - documentation
---

# Preserve original authorship in multi-author retrofits: don't trust the operator's git config when sweeping content others created

## Context

> **Cross-repo note.** The sweep tool that triggered this lesson lives in [`mvanhorn/printing-press-library`](https://github.com/mvanhorn/printing-press-library) at `tools/sweep-frontmatter/main.go`. The lesson itself applies anywhere `cli-printing-press` (this repo) ships generation or rewriting tooling that touches multi-author content — primarily the published library, but also any future cross-CLI sweep we add to the generator's surface.

The Hermes / OpenClaw frontmatter alignment work needed to add an `author:` field to every existing CLI's published `library/<cat>/<api>/SKILL.md`. The sweep tool's first cut resolved the field from the operator's `git config user.name` — a pragmatic default that worked for fresh prints (whoever runs `/printing-press` is the author) and that mirrored how every other field was being populated.

The result, after running the sweep across all 49 published CLIs:

```yaml
# library/commerce/shopify/SKILL.md (originally created by Cathryn)
author: "Trevin Chow"

# library/commerce/instacart/SKILL.md (originally created by Matt)
author: "Trevin Chow"

# library/payments/coingecko/SKILL.md (originally created by Hiten)
author: "Trevin Chow"

# library/marketing/scrape-creators/SKILL.md (originally created by Adrian)
author: "Trevin Chow"
```

The operator running the sweep (Trevin) had not created 29 of the 49 CLIs. The default silently flipped attribution to him for every entry. From the user's reaction:

> "for the printing-press-library you added Author as 'Trevin Chow' on all of them, but they should match whoever is in the copyright as i didn't create them all. As is, it sounds like I've taken the copyright authorship ownership over, and I don't want that to be the case."

The signal isn't subtle. Attribution belongs to the person who did the work. A retrofit tool that overwrites attribution silently is doing real damage even when the published artifacts haven't shipped to a wider audience yet.

## Guidance

For a sweep over multi-author content, build a curated per-entry authorship map sourced from primary evidence (copyright headers, git history, manifest fields), not from the operator's environment. Apply explicit corrections for cases where the primary evidence is wrong or missing. Treat the operator's git config as a last-resort fallback only for entries the map doesn't cover.

```go
// printing-press-library/tools/sweep-frontmatter/main.go (the actual fix)

// cliAuthorByAPIName is the canonical author display name for every
// existing per-CLI library entry, keyed by api_name (the directory
// basename: dominos, linear, etc.). Entries derived from each CLI's
// `// Copyright YYYY <slug>.` header where present, with per-CLI
// corrections applied for cases where the slug doesn't reflect actual
// authorship — generator-fallback "user" headers (5 CLIs originally
// generated before git config was set), missing copyright headers
// (2 legacy CLIs), and one slug-vs-actual mismatch (espn).
var cliAuthorByAPIName = map[string]string{
    "agent-capture":   "Matt Van Horn",
    "ahrefs":          "Cathryn Lavery",
    "airbnb":          "Matt Van Horn",
    // ... 46 more entries
    "yahoo-finance":   "Trevin Chow",
}

// Resolution order in the sweep:
//   1. cliAuthorByAPIName — source of truth for the existing 49
//   2. manifest's owner_name — set by future fresh prints under the
//      post-Hermes-alignment generator template; lets a future regen
//      preserve attribution
//   3. operator's git config user.name — last-resort fallback for new
//      CLIs added to the library without an entry in the map above
authorName := cliAuthorByAPIName[mf.APIName]
if authorName == "" {
    authorName = mf.OwnerName
}
if authorName == "" {
    authorName = ownerName  // operator's git config
}
```

The map's contents come from a deliberate audit, not heuristics:

1. **Extract the slug from each CLI's copyright header.** `find . -name "*.go" | xargs grep "// Copyright YYYY"` against the published library produces a `(api_name, slug)` table. For CLIs missing the header entirely (legacy generator output predating the convention), use `git log --format="%an" --reverse | head -1` against the per-CLI directory to identify the first committer.
2. **Map slugs to display names.** For most slugs the mapping is mechanical (`trevin-chow` → `Trevin Chow`). Some require GitHub-profile lookup or domain knowledge. Document the few that aren't obvious (e.g., `rderwin` had no public name set).
3. **Apply explicit corrections.** Some copyright slugs are wrong (`user` from a generator-fallback path; `trevin-chow` on a CLI that was actually Matt's work). Override with manual entries; document the override reason in a comment.
4. **Surface the table to a human reviewer before the sweep mutates anything.** A 49-row table is easy to eyeball; the audit catches misclassifications before they ship.

The sweep tool's last-resort fallback to the operator's git config is intentional — it's the right answer for a NEW CLI added to the library after this work, where the operator IS the author. The map handles the historical content; the fallback handles future content.

## Why This Matters

Three failure modes the curated-map approach prevents:

1. **Attribution flip on every regen.** Without the map, every operator running the sweep silently overwrites authorship with their own identity. Attribution is wrong AT BEST temporarily (until the next sweep with the right operator) and wrong PERMANENTLY in the worst case (operator forgets to undo, the wrong attribution ships to a public registry).
2. **Burn trust with actual authors.** The CLIs in this library represent real work by real people. A tool that silently rewrites that work's attribution to the operator implies a claim of ownership that wasn't earned. Even if no one notices for months, the eventual discovery is a credibility hit that's hard to recover from.
3. **Compounding error in derivative artifacts.** Hermes / OpenClaw skill registries cache the `author:` field. Once a wrong value ships, retracting it requires another sweep + a re-publish. If users have already cached the wrong attribution locally, retraction may not even reach them.

The "use the operator's identity" default isn't malicious — it's the path of least resistance when you're writing the tool. The lesson is to recognize when the path of least resistance produces a class of bug (silent attribution flip) that's worse than the alternative (a 30-minute audit + 49-line lookup table).

## When to Apply

- Sweeping content across many entries with multiple original authors
- Attribution will be persisted in a way that downstream consumers see (`author:` in skill metadata, copyright header rewrites, README bylines, contributor lists)
- Original authorship is recoverable from primary evidence (copyright headers, git history, an existing manifest field)
- The cost of getting it wrong is non-trivial (public registry, branded asset, user-facing field)

Don't apply when:

- The content is genuinely owned by the sweeping operator (e.g., your personal dotfiles, a single-author repo where everyone running the sweep is the author)
- The attribution surface is private / debug-only and won't be visible to the original authors or to downstream consumers
- The original authorship is genuinely unrecoverable AND the operator's identity is the most honest default available (rare; usually some primary evidence exists)

## Examples

### Anti-pattern — implicit operator identity

```go
// Naive sweep tool — what we shipped first
func sweepCLI(cliDir string) error {
    operatorName, _ := exec.Command("git", "config", "user.name").Output()
    skill := readSkill(cliDir)
    skill.Author = strings.TrimSpace(string(operatorName))  // wrong for ~60% of entries
    return writeSkill(cliDir, skill)
}
```

Result on a multi-author corpus: every `author:` field flips to the sweep operator. Discoverable only by manual inspection. Already-published artifacts may be cached downstream.

### Pattern — curated map with audit-friendly structure

```go
// Sweep tool with curated per-entry mapping
var cliAuthorByAPIName = map[string]string{
    "agent-capture":   "Matt Van Horn",      // no copyright header; from git first-commit
    "ahrefs":          "Cathryn Lavery",     // copyright slug "user"; from git first-commit
    "airbnb":          "Matt Van Horn",      // copyright slug matches
    "espn":            "Matt Van Horn",      // copyright slug "trevin-chow" — corrected per author
    // ... etc.
}

func sweepCLI(cliDir string, mf manifest, operatorName string) error {
    authorName := cliAuthorByAPIName[mf.APIName]
    if authorName == "" {
        authorName = mf.OwnerName  // future fresh prints carry this
    }
    if authorName == "" {
        authorName = operatorName  // last-resort fallback
    }
    skill := readSkill(cliDir)
    skill.Author = authorName
    return writeSkill(cliDir, skill)
}
```

Result: every `author:` field carries the actual author's name. Audit friction during the initial map construction (~30 minutes) prevents months of "is this actually Trevin's CLI?" downstream questions.

### Pattern — surface the map for human review before applying

When the table is large enough that manual review at-merge is risky, pre-build the table and surface it for review before the sweep runs:

```
| CLI            | Copyright slug   | Proposed `author:`        |
|----------------|------------------|---------------------------|
| agent-capture  | (none)           | **Matt Van Horn** *(git)* |
| ahrefs         | `user`           | **Cathryn Lavery** *(git)* |
| airbnb         | `matt-van-horn`  | Matt Van Horn             |
| espn           | `trevin-chow`    | **Matt Van Horn** *(corrected)* |
| ...            | ...              | ...                        |
```

The user reviewing this caught two slug-vs-actual mismatches that the heuristic would have shipped wrong.

## Related

- `printing-press-library/tools/sweep-frontmatter/main.go` (cross-repo) — `cliAuthorByAPIName` map declaration
- `docs/solutions/design-patterns/dual-key-identity-fields-2026-05-06.md` — the dual-key model `OwnerName` / `Owner` (slug); this learning is about how to populate `OwnerName` when retrofitting content where the operator isn't the author
- `docs/solutions/conventions/soft-validation-in-reusable-library-packages-2026-05-06.md` — the in-generator soft-fallback that fires when `OwnerName` is empty; the sweep's curated map overrides the soft-fallback for retrofitted content
