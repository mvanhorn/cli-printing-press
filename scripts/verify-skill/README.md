# verify-skill

Static verifier for `SKILL.md` files that ship alongside printed CLIs. Checks
that every command, flag, and positional-arg signature referenced in a
SKILL.md actually exists in the shipped CLI's `internal/cli/*.go` source.

## Why

SKILL.md files are authored prose. The authoring process (human or LLM)
can confidently reference a flag like `--moneyness` or a command like
`portfolio perf` that doesn't exist in the shipped code. Without
verification, agents following the SKILL will hit "unknown flag" errors
in production.

This verifier was built after hand-authoring SKILLs for the 11 launch
CLIs and discovering **23 invented / wrong-command / wrong-arg-count
errors** across 7 of them. Three tiers of checks catch different error
classes; the earlier tier is strictest and the later tiers cover what
the earlier ones miss.

## What it checks

**1. flag-names** — every `--flag` token in SKILL.md is declared as
`.Flags().XVar(&var, "name", ...)` somewhere in `internal/cli/*.go`. Catches
pure inventions.

**2. flag-commands** — every `--flag` used on a specific bash recipe is
declared on that command's source file (or as a persistent root flag).
Catches cases where a flag exists somewhere but I used it on the wrong
command (e.g., `--interval` on `payouts` when it's actually on
`commissions`).

**3. positional-args** — each bash recipe's positional-arg count is
compatible with the command's `Use:` field (`<required>` + `[optional]` +
`variadic`) and its `Args:` validator (`cobra.ExactArgs(N)`,
`MinimumNArgs(N)`, etc.). Catches wrong-arity invocations.

## Usage

```bash
# Run all three checks against a CLI directory
python3 verify_skill.py --dir /path/to/my-pp-cli

# Run only the flag-command check
python3 verify_skill.py --dir /path/to/my-pp-cli --only flag-commands

# JSON output for CI
python3 verify_skill.py --dir /path/to/my-pp-cli --json

# Fail on findings that the script flags as likely false positives
python3 verify_skill.py --dir /path/to/my-pp-cli --strict
```

Exit code `0` = clean (or only likely-false-positive findings), `1` =
real issues found, `2` = usage error.

## Known false positives

The verifier uses pattern matching against Go source text — it doesn't
build a full Cobra command tree. Two known FP patterns:

**Parent command's first positional arg is a valid subcommand name.**
Example: `hubspot-pp-cli associations companies <id> contacts` — `companies`
is an object-type argument, not a subcommand. The parser reads `companies`
as extending the command path and reports a bogus arity mismatch on a
non-existent `associations.companies` command.

**Shell command substitution inside a recipe.** Example:
`tool --flag "$(tool other-subcommand ...)"`. The parser strips `$(...)`
and replaces with a sentinel before tokenizing, but edge cases can slip
through.

Findings in these patterns are tagged `[likely false positive]` and
don't trigger non-zero exit unless `--strict` is set.

## When to run it

**Per CLI, during SKILL authoring.** Any time you edit a SKILL.md,
run the verifier against its CLI directory to catch invented flags
immediately.

**In CI for the library repo.** A GitHub Actions workflow can run this
against every CLI directory on every PR, blocking merges that introduce
unverified commands or flags. See `.github/workflows/verify-skills.yml`
in `mvanhorn/printing-press-library` for the reference setup.

## Future: Go port

This is an interim Python implementation. The eventual home is a
`printing-press verify-skill` subcommand in the CLI Printing Press,
alongside `dogfood`, `verify`, and `scorecard` — integrated into
`shipcheck` so SKILL validity is gated at publish time. See
`docs/plans/` for the Go-port plan when it lands.
