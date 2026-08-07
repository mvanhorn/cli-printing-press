---
title: "Verifying generator fixes at the generated-output level"
date: 2026-05-25
category: best-practices
module: generator
problem_type: best_practice
component: generator
severity: medium
applies_when:
  - "Authoring a fix in internal/generator, templates, parser, mcpdesc, naming, or SKILL.md generation skeletons"
  - "Reviewing or dispatching an agent to fix a generator or template bug"
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

The Printing Press is a two-layer system: templates emit Go source, and the real behavior is in the generated code, not the template text.

Generator/template fixes can compile and pass a scoped unit test while still emitting broken CLI code. The common gap is between "what the template says" and "what the generated code does." Fast gates such as scoped `go test -run ...` and `scripts/golden.sh verify` can under-cover fallback or conditional branches:

- Golden fixtures often carry happy shapes: defaults are present, operations have summaries, and response envelopes are simple.
- Scoped package tests may pass while another generated package fails to compile.
- Snapshotting a generated file does not prove all emitted files agree on the same condition.

## Recurring Failure Modes

1. **Half of a two-sided contract.** A fix changes an emitted helper, definition, or data flow but not the consumer under the same condition.
2. **Hand-rolled behavior where the codebase has an idiom.** Examples include interpolating spec text into Go literals without `oneline` / `printf "%q"`, or using append semantics where sibling branches overwrite.
3. **Go predicates that do not match template truthiness.** For example, `p.Default == nil` can drift from template `(not .Default)` semantics.
4. **Over-broad scope.** A CLI-level guard or unconditional branch can break a related surface such as MCP, read commands, or promoted commands.
5. **Symptom fixes that violate invariants.** A placeholder or fallback may satisfy one review comment while breaking parser, verifier, scorer, or printed-CLI rules.

## Verification Contract

Apply these whenever a change alters what the generator emits:

- Run `go test ./...`, not only a scoped `-run` test.
- Run `scripts/golden.sh verify` when output shape may change.
- Run `scripts/verify-generator-output.sh` to generate representative CLI cases and compile the emitted modules with `go build ./...`.
- Generate from a spec and assert on emitted code or compiled generated output, not only template text.
- In `internal/generator` tests, use `requireGeneratedCompiles(t, outputDir)` after generating a CLI whose emitted helpers, imports, or call sites are part of the contract under test.
- Assert statement kind, not only substrings. For example, check for `return fmt.Errorf(` and `NotContains` the old warning-only form when the behavior is supposed to become fatal.
- Cover the affected template variants and fallback shapes: endpoint and promoted templates, missing defaults, missing summaries, envelope responses, and every generated file that participates in the contract.
- When changing an emitted definition, grep for call sites and gate them identically.
- Use established idioms: `oneline` / `OneLineNormalize` / `printf "%q"` for literals, `text/template.IsTrue` or shared helpers for template truthiness, and sibling-branch semantics for request mutation.
- Check system invariants before committing to the obvious fix.

## Structural Prevention

- Keep fallback-shape golden fixtures for no-default positionals, summary-less operations, and envelope responses.
- Prefer generated-output tests that compile or execute emitted code for bug shapes that span multiple generated files.
- Make PRs name their generated-output evidence in the Output Contract and Verification sections.
- Treat independent code review as a required guard for generator fixes, not a cleanup step after local green.
