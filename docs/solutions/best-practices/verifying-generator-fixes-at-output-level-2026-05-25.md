---
title: "Verifying generator fixes at the generated-output level"
date: 2026-05-25
category: best-practices
module: generator
problem_type: best_practice
component: generator
severity: medium
applies_when:
  - "Authoring a fix in internal/generator (templates, parser, mcpdesc, naming) or a SKILL.md generation skeleton"
  - "Reviewing or dispatching an agent/codex to fix a generator or template bug"
tags:
  - generator
  - templates
  - testing
  - golden
  - code-review
  - agent-authoring
related_components:
  - generator
  - templates
  - testing
---

# Verifying generator fixes at the generated-output level

## Context

The Printing Press is a two-layer system: templates emit Go source, and the
real behavior is in the *generated code*, not the template text. Across one
batch of generator/template fixes, roughly half of the agent-authored changes
contained a real defect that nonetheless **compiled** and **passed its own
scoped unit test** — caught only by full-suite runs, independent code review,
or CI/Greptile. The common thread: defects live in the gap between "what the
template says" and "what the generated code does," and the fast quality gates
(`go test -run <X>`, `scripts/golden.sh verify`) systematically under-cover the
conditional/fallback branches that bug-fixes most often touch.

Two structural reasons the cheap gates give false confidence:

- **Golden fixtures all carry "happy" shapes** — every positional has a
  default, every operation has a summary, no response is a `{status,data}`
  envelope. Fixes to *fallback* branches (placeholders, synthesized
  descriptions, envelope unwrap) therefore produce **zero golden diff** and
  look safe. (Tracked: add a fallback-shape fixture — see Related issues.)
- **A scoped `-run` test passes while a different package's compile test
  fails.** An emit/call gate mismatch only surfaces under the full
  `go test ./...` matrix.

## Defect taxonomy (recurring failure modes)

1. **Half of a two-sided contract.** A fix changes a definition/emission but not
   its consumer under the *same* condition. Examples: a helper's emission gate
   was narrowed but its call site stayed on a broader condition →
   `undefined: <helper>` (emit/call mismatch); a data-flow change (keep-logic)
   left the dependent log message asserting a now-false premise; one collector
   slice was fixed but a second identical site was missed.
2. **Hand-rolled what the codebase already solves.** Spec-controlled text was
   interpolated into a Go string literal without the established
   `oneline` / `printf "%q"` escaping idiom → non-compiling output; an
   append-semantics API (`req.AddCookie`) was used where sibling branches used
   overwrite-semantics (`req.Header.Set`) → a duplicated header.
3. **A Go-side predicate that does not *exactly* mirror the template gate.**
   `p.Default == nil` instead of the template's `(not .Default)` truthiness;
   mirror it with `text/template.IsTrue`, or reuse the same helper the template
   calls.
4. **Over-broad scope.** An unconditional guard that broke an unrelated surface
   (e.g. help-on-empty that also short-circuited read commands / the MCP tool);
   a CLI-level global flag where per-command gating was required.
5. **Symptom fixed against a system invariant.** Replacing a value with a
   `<placeholder>` that another subsystem is *designed to reject*
   (verify-skill flags `<...>` positionals); implementing an issue's literal
   prescription that contradicts the parser's canonical output, so the fix
   diverges from `generate`. When the "obvious" fix trips an invariant, the
   real fix is usually larger (or it is a design decision to escalate, not a
   band-aid to ship).

## The fix-authoring verification contract

Apply these whenever a change alters what the generator emits:

- **Run the full `go test ./...`, not just the scoped `-run` test.** Scoped
  green + `golden.sh verify` green is **not** proof for conditional/fallback
  code. The full matrix runs the `TestGenerate*Compile` cases that catch
  emit/call breaks.
- **Verify at the generated-output + compile level.** Generate from a spec,
  then `go build ./...` the output and assert on the *emitted code*, not
  template text. Compiling is the cheapest guard against emit/call mismatches.
- **Assert statement *kind*, not a substring.** Check for `return fmt.Errorf(`
  (not just the message), and add a `NotContains` for the old form, so a
  regression that keeps the message but reverts the behavior (warn vs return)
  still fails.
- **Cover every template variant and the fallback shape.** Endpoint *and*
  promoted templates; the no-default / summary-less / envelope shape, not just
  the happy fixture; all N generated files when a spec produces several.
- **When you change an emission/definition, grep for and gate its call sites
  identically**; when you change a data flow, find the dependent
  reporting/consumers it invalidates.
- **Use the established idiom** (`oneline`/`OneLineNormalize`/`printf "%q"` for
  literals; `IsTrue` to mirror `(not .Default)`); match sibling-branch
  semantics rather than introducing a new API shape.
- **Scope the change to the reported surface, and check the system invariant**
  the fix might break before committing to it.
- **Independent code review per fix.** Review caught the inert/half/regressive
  changes that compiled and passed unit tests; it is the highest-value step,
  not an optional one.

## Related issues (structural prevention)

These propose making the system catch the above for everyone, not just careful
authors:

- [#2279](https://github.com/mvanhorn/cli-printing-press/issues/2279) — compile
  generated output in generator tests (emit/call parity).
- [#2280](https://github.com/mvanhorn/cli-printing-press/issues/2280) — add a
  fallback-shape golden fixture (no-default positional + summary-less op +
  envelope response) so fallback-branch fixes are visible at `golden.sh verify`.
- [#2281](https://github.com/mvanhorn/cli-printing-press/issues/2281) — guard
  against spec-controlled strings emitted into Go literals without escaping.
