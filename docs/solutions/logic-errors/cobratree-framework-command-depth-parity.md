---
title: "Cobratree framework command skips are depth-sensitive"
date: "2026-05-22"
category: logic-errors
module: internal/generator/templates/cobratree
problem_type: logic_error
component: tooling
severity: medium
symptoms:
  - "Nested domain commands named search, sql, doctor, or another framework command were absent from MCP tools/list"
  - "The runtime walker treated framework command names as globally reserved instead of top-level-only"
  - "Downstream scorer and audit code could drift when they mirrored the old leaf-name-only skip rule"
root_cause: logic_error
resolution_type: code_fix
tags:
  - mcp
  - cobratree
  - framework-commands
  - scorecard
  - tools-audit
---

# Cobratree framework command skips are depth-sensitive

## Problem

The cobratree runtime walker used the leaf command name to classify framework commands. That was correct for top-level commands such as `search` and `sql`, where typed MCP tools or non-agent-friendly framework behavior already cover the surface. It was wrong for nested domain commands such as `items search`: those commands are user-facing domain actions and should be mirrored as shell-out MCP tools.

The same assumption had also leaked into dependent verifiers. The MCP token estimator counted framework names as globally skipped, and tools-audit suppressed framework-named commands instead of suppressing only true framework command files.

## Symptoms

- A printed CLI could expose `<cli> items search` to humans while MCP `tools/list` omitted `items_search`.
- Static MCP token estimates undercounted nested framework-name tools.
- `tools-audit` missed thin descriptions or missing `mcp:read-only` annotations on nested domain commands named `search` or `sql`.

## What Didn't Work

- Keeping `frameworkCommands` as a flat name set without depth semantics made the comment contract lie. The list was documented as top-level framework commands, but the code applied it to every command with the same leaf name.
- Updating only the runtime walker was incomplete. The scorer and audit code mirror cobratree behavior because they cannot import generated printed-CLI code, so they need the same depth rule and tests.
- Dedupe by constructor function name in the estimator stopped matching runtime behavior once tool names became path-aware. Reusing the same `newSearchCmd` constructor under two parents should count two tools, not one.

## Solution

Make the framework skip top-level-only everywhere that models the cobratree surface:

- `internal/generator/templates/cobratree/classify.go.tmpl` classifies a framework name as `commandFramework` only when the command's immediate parent is the root command.
- `internal/pipeline/mcp_size.go` carries command path depth while walking the generated Cobra constructor graph, uses path-aware tool names, and dedupes by command path instead of constructor function.
- `internal/cli/tools_audit.go` suppresses findings for true top-level framework command files while auditing nested domain commands named `search`, `sql`, or other framework leaves.
- The generated golden `classify.go` is refreshed so the emitted printed-CLI contract matches the template.

## Why This Works

Cobra depth is the contract boundary. Top-level framework commands are generator-owned affordances with typed MCP equivalents or human-only behavior. Nested commands are domain commands, even when their leaf name happens to collide with a framework word.

Keeping scorer and audit code aligned with that same boundary prevents two classes of silent drift:

- False negatives, where generated CLIs expose new MCP tools but verifiers still assume they are skipped.
- False accounting, where static estimates dedupe a shared constructor even though the runtime tool names differ by path.

## Prevention

- When changing cobratree classification, update every mirror in the same change: runtime template, generated golden, MCP token estimator, and tools-audit.
- Regression tests should cover both positive and negative depth cases: top-level `search` remains skipped, nested `items search` is mirrored, and reused nested constructors under different parents produce distinct tool names.
- For verifiers that cannot import generated code, model the same semantic inputs the runtime uses. In this case that means command depth and full command path, not only the `Use` leaf.

## Related

- `docs/solutions/logic-errors/reachable-scorer-surfaces-non-rest-clis-2026-05-21.md`
- `docs/solutions/logic-errors/mcp-template-literal-type-match-missed-openapi-shapes-2026-05-14.md`
- `docs/solutions/security-issues/mcp-sql-search-readonly-bypass-2026-05-08.md`
- `docs/solutions/logic-errors/ship-plan-agent-readiness-gate-2026-05-18.md`
