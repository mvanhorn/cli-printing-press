---
title: "External-tool flags in agent-readable docs need an allowlist on the doc validator"
date: 2026-05-06
category: conventions
module: cli-printing-press-generator
problem_type: convention
component: tooling
severity: medium
applies_when:
  - "Generating SKILL.md, README.md, or other agent-readable docs that embed install / usage commands from external tools (npm, npx, hermes, claude mcp, go install, brew, pip, etc.)"
  - "A downstream validator checks that every `--flag` mentioned in the doc maps to a flag declared in the doc's owning CLI's source code"
  - "The external tools' flags (`--cli-only`, `--force`, `--registry-url`, etc.) are NOT flags of the doc's owning CLI and would never appear in its source"
  - "Adding a new install instruction to the template introduces a new external flag that wasn't previously in any printed doc"
tags:
  - skill-md
  - install-commands
  - validator
  - cross-repo
  - allowlist
  - cross-repo-lesson
related_components:
  - templates
  - tooling
  - documentation
---

# External-tool flags in agent-readable docs need an allowlist on the doc validator

## Context

> **Cross-repo note.** The validator script lives in [`mvanhorn/printing-press-library`](https://github.com/mvanhorn/printing-press-library) at `.github/scripts/verify-skill/verify_skill.py`. The flag allowlist (`COMMON_FLAGS`) is in that file. This learning is filed in `cli-printing-press` (this repo) because it's *our* template changes — adding install commands to `skill.md.tmpl` and `readme.md.tmpl` — that introduce new flags the validator needs to know about. The coupling is silent; CI only catches it when someone runs the validator after a regen.

When the Hermes / OpenClaw frontmatter alignment work added `npx -y @mvanhorn/printing-press install <api> --cli-only` to every printed CLI's Prerequisites section in SKILL.md, every existing CLI's verify check started failing:

```
❯ python3 .github/scripts/verify-skill/verify_skill.py --dir library/commerce/shopify
=== shopify ===
  ✘ 1 error(s), 0 likely false-positive(s)
    [flag-names] (any): --cli-only is referenced in SKILL.md but not declared in any internal/cli/*.go
```

49 of 49 CLIs failed with the same error. The validator's logic:

```python
# extract_all_flags returns every `--flag` token from the SKILL.md.
# check_flag_names then asserts each flag is declared in internal/cli/*.go.
def extract_all_flags(skill: Path) -> set[str]:
    text = skill.read_text()
    return {t.lstrip("-") for t in FLAG_TOKEN_RE.findall(text)}
```

The validator can't tell that `--cli-only` is a flag of `@mvanhorn/printing-press` (the npm installer) rather than a flag of the printed CLI itself. The flag *will* never appear in `internal/cli/*.go` because it's not the printed CLI's flag — it's the installer's. But the broad-extraction approach catches every `--flag` token, regardless of which command it belongs to.

The fix is a small allowlist:

```python
COMMON_FLAGS = {
    "help", "version", "json", "csv", "plain", "quiet", "agent",
    "select", "compact", "dry-run", "no-cache", "yes", "no-input",
    # ... existing entries ...

    # Flags from external tools whose install/usage commands are
    # embedded in SKILL.md prose. These are NOT flags of the printed
    # CLI being verified — they belong to the npm installer, hermes
    # CLI, claude mcp, etc. — and the verifier shouldn't expect them
    # in internal/cli/*.go.
    "cli-only",     # @mvanhorn/printing-press install ... --cli-only
    "skill-only",   # @mvanhorn/printing-press install ... --skill-only
    "registry-url", # @mvanhorn/printing-press --registry-url
    "force",        # hermes skills install ... --force
}
```

The substantive lesson isn't "the validator's wrong" — the validator's broad-extraction approach is intentionally broad so it catches typos in real CLI flag mentions in prose. The lesson is that adding new install instructions to a template has a downstream-coupling cost that's invisible until CI fires.

## Guidance

When you add an install or usage instruction to an agent-readable doc template (`skill.md.tmpl`, `readme.md.tmpl`, etc.) that embeds a command from a tool other than the doc's owning CLI:

1. **Identify every `--flag` in the new instruction.** Both the explicit ones (`--cli-only`, `--force`) and any default ones (some tools document `-y` shorthand, or required positional + flag combinations).
2. **Verify the validator on the consuming side handles them.** For published CLIs in `printing-press-library`, the validator is `.github/scripts/verify-skill/verify_skill.py`'s `COMMON_FLAGS` set. If the new flag isn't in there, add it in the same change.
3. **Document the cross-repo coupling.** The downstream side's `AGENTS.md` should have a "Watch out for: external-tool flags" pointer so a future contributor hitting the validator failure can find the fix in seconds. (We added this to `printing-press-library/AGENTS.md` as part of the Hermes-alignment work.)
4. **Add the new flag to the upstream's commit message** so the cross-repo dependency is searchable. Future PRs that touch templates can grep `git log` for "external flag" and see the precedent.

The downstream `AGENTS.md` note (in printing-press-library) reads, in part:

> When you add a new install/usage instruction to SKILL.md that introduces a new --flag, add it to COMMON_FLAGS in the same change. Otherwise the verifier will fail across every CLI on the next regen with `[flag-names] (any): --your-flag is referenced in SKILL.md but not declared in any internal/cli/*.go`. Currently allowlisted external flags: `cli-only`, `skill-only`, `registry-url` (npm installer), `force` (hermes).

## Why This Matters

Two compounding factors make this a high-friction failure mode without explicit awareness:

1. **The validator runs against published library content, not against the template.** Changing the template doesn't trigger the validator. The validator fires when `printing-press-library`'s next regen workflow runs against the new template's output. By then, the failure is across every CLI in the library, not visible during the template-change author's own test run.

2. **The error message names a CLI source-tree path that the template author isn't looking at.** A template author who sees "—cli-only is referenced in SKILL.md but not declared in any internal/cli/*.go" naturally checks whether `--cli-only` should be a flag of the printed CLI. It shouldn't. The validator doesn't say "did you mean an external tool's flag?" — there's no signal that this is a known-allowable case that just needs the allowlist update.

The fix is cheap once you know it. Costing the lesson into `AGENTS.md` on both sides is what makes it cheap for the next person.

## When to Apply

- Adding ANY new bash code block, fenced command, or copy-pasteable instruction to a generator template that includes a `--flag`
- The flag belongs to a tool other than the printed CLI (npm/npx/yarn/pnpm; brew/apt/pacman; go install; hermes/claude mcp/openclaw; gh/git; system tools)
- The downstream consumer of the template runs a doc validator that checks flag-vs-source-code correspondence (verify-skill, similar lint tools, custom CI scripts)

The pattern doesn't only apply to SKILL.md. Any agent-readable file that mixes commands from multiple tools faces the same risk: validators that scope to the file's "owning" CLI's source can't distinguish first-party flags from third-party flags. The allowlist is the standard fix.

## Examples

### What to do alongside a template change

Adding the npx install line to `internal/generator/templates/skill.md.tmpl`:

```diff
+1. Install via the Printing Press installer:
+   ```bash
+   npx -y @mvanhorn/printing-press install {{.Name}} --cli-only
+   ```
```

Same change should also include (in `printing-press-library`):

```diff
 # .github/scripts/verify-skill/verify_skill.py
 COMMON_FLAGS = {
     "help", "version", "json", ...
+    "cli-only",   # @mvanhorn/printing-press install ... --cli-only
 }
```

If the two repos can't be touched in one PR, sequence: validator-allowlist update lands first, template change lands second. Otherwise the gap window has every library regen failing.

### What goes in AGENTS.md (downstream side)

```markdown
## SKILL.md verification

[existing content...]

**Watch out for: external-tool flags embedded in SKILL.md install instructions.**
Every `--flag` token anywhere in SKILL.md is checked against the printed CLI's
source. SKILL.md sometimes embeds install commands from *other* tools (npm,
hermes, claude mcp, go install, etc.) whose flags don't exist in the printed
CLI's `internal/cli/*.go`. The verifier's `COMMON_FLAGS` set in
`.github/scripts/verify-skill/verify_skill.py` is the allowlist of flags that
don't need a CLI-source declaration.

When you add a new install instruction to SKILL.md that introduces a new
`--flag`, add it to `COMMON_FLAGS` in the same change.
```

### Anti-pattern — silent CI gap

Template change merges without the validator update. Library regen workflow runs on next push to main. CI fails for every CLI. Investigation traces back to the template change. Fix takes hours instead of minutes because the original author isn't around and the error message doesn't point at the real problem.

## Related

- `printing-press-library/.github/scripts/verify-skill/verify_skill.py` (cross-repo) — `COMMON_FLAGS` allowlist
- `printing-press-library/AGENTS.md` (cross-repo) — "Watch out for" note in the SKILL.md verification section
- `internal/generator/templates/skill.md.tmpl` — the template that introduced `--cli-only` via the Prerequisites section
- `internal/generator/templates/readme.md.tmpl` — uses `--force` (hermes), already in the allowlist
