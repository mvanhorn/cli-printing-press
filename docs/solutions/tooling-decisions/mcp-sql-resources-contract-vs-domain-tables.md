---
title: "MCP SQL keeps the resources query contract; domain tables stay"
date: 2026-09-23
category: tooling-decisions
module: internal/generator/templates
problem_type: tooling_decision
component: tooling
severity: medium
applies_when:
  - "Changing the generated MCP sql tool description or the unknown-table error"
  - "Changing whether the store emits one table per resource"
  - "Writing a generated test that queries the local store and expects a missing table"
related_components:
  - internal/mcp
  - store
tags:
  - mcp
  - sql
  - store
  - domain-tables
  - resources
---

# MCP SQL keeps the resources query contract; domain tables stay

## Context

The local store writes two shapes:

- `resources(resource_type, id, data)` for every synced record.
- A typed domain table, named from the resource, when `emitsDomainTable` is true (a domain upsert with more than the base `id` / `data` / `synced_at` columns, or the JSON-only fallback). Resources that fail that gate never get their own table; their rows stay in `resources` only.

The MCP `sql` tool description and the unknown-table error tell agents to query `resources` and read fields with `json_extract`. The error also says records live there, "not one SQL table per resource."

## Decision

Keep both store shapes. Do not drop domain tables to make the error sentence literal, and do not retarget the MCP `sql` contract at per-resource tables.

`resources(resource_type, id, data)` plus `json_extract` is the uniform query contract. It exists for every resource, including those with no domain table. Domain tables are a typed projection for the resources that qualify; they are queryable when present, and they are not guaranteed.

The clause "not one SQL table per resource" is steering copy toward that uniform contract. It is not a schema invariant. Rewording it, or teaching agents to prefer domain tables, is a separate change from keeping the store.

Generated tests that need a missing table must not use a resource-shaped name. `widgets` is a legal domain table. The reserved sentinel is `__pp_missing_table__`.

## Why This Matters

A test that hardcodes `SELECT * FROM widgets` passes only for specs that lack a `widgets` resource. On a spec that declares one, the store creates the table, the query returns rows, and the generated `go test` suite fails. The failure looks like a store bug. It is a test that assumed the error sentence was a schema guarantee.

## When to Apply

- When an unknown-table MCP SQL error should name `resources(resource_type, id, data)` and `json_extract`.
- When a generated SQL test needs a table that cannot be a per-resource domain table.
- When a change would delete domain tables, or would make domain tables the only documented MCP SQL shape, to "match" the error sentence.
