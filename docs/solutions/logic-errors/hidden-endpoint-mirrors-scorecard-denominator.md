---
title: "Hidden endpoint mirrors must not score as visible MCP tools"
date: "2026-05-22"
category: logic-errors
module: internal/pipeline
problem_type: logic_error
component: tooling
severity: medium
symptoms:
  - "tools-audit emitted thin-mcp-description findings for endpoint mirrors hidden behind code orchestration"
  - "MCPDescriptionQuality counted hidden endpoint catalog records in its denominator"
  - "Polish could not legitimately resolve prose-poor endpoint findings because those endpoints were not registered as individual MCP tools"
root_cause: logic_error
resolution_type: code_fix
tags:
  - mcp
  - scorecard
  - tools-audit
  - tools-manifest
  - code-orchestration
---

# Hidden endpoint mirrors must not score as visible MCP tools

## Problem

`tools-manifest.json` serves two related but different consumers. In endpoint-mirror mode, its `tools` records describe agent-visible typed MCP tools. In code-orchestration mode, those same endpoint records are used as the search/execute catalog behind the thin `<api>_search` and `<api>_execute` surface.

The scorer and `tools-audit` treated every manifest tool as agent-visible. For prose-poor large specs, that made `thin-mcp-description` and `MCPDescriptionQuality` block shipping on endpoint records agents never see as standalone tools.

## Symptoms

- Code-orchestration CLIs could produce hundreds or thousands of pending `thin-mcp-description` findings for hidden endpoint catalog entries.
- `MCPDescriptionQuality` could score the hidden endpoint catalog as 0/10 even though the visible MCP surface was the generated search/execute pair plus framework and novel tools.
- Polish had no legitimate path to `ship` because fabricating endpoint descriptions is forbidden and accepting bulk thin-description findings leaves the scorecard unchanged.

## What Didn't Work

- Treating `tools-manifest.json` as only a visible-tool manifest ignored its catalog role in code-orchestration mode.
- Returning full credit for hidden endpoint records would also be wrong. Hidden endpoint records should be removed from the `MCPDescriptionQuality` denominator, not counted as richly described visible tools.
- Fixing only `tools-audit` would leave the scorecard gate stale. Both surfaces read the same manifest and must share the same visibility policy.

## Solution

Persist the MCP endpoint visibility fields needed by manifest consumers:

```go
type ManifestMCP struct {
    EndpointTools string `json:"endpoint_tools,omitempty"`
    Orchestration string `json:"orchestration,omitempty"`
}
```

Keep the visibility rule canonical on `spec.MCPConfig`:

```go
func (m MCPConfig) EndpointMirrorsVisible() bool {
    if m.IsCodeOrchestration() {
        return false
    }
    return m.EndpointTools != "hidden"
}
```

Then have manifest consumers branch on that rule:

- `tools-audit` skips manifest endpoint-description findings when endpoint mirrors are hidden.
- `MCPDescriptionQuality` returns unscored for hidden endpoint mirrors, so the scorecard denominator excludes records that are not agent-visible tools.
- Visible endpoint-mirror manifests still get `thin-mcp-description` findings and score penalties.

## Why This Works

The scoring unit is the agent-visible tool, not every endpoint catalog record. Code orchestration still needs endpoint metadata so `<api>_execute` can reach the whole API surface, but those records are not registered as one tool per endpoint.

Separating endpoint visibility from manifest presence keeps both truths intact: the catalog remains available for code orchestration, and the ship gates only judge descriptions agents can actually select as tools.

## Prevention

- When a verifier reads `tools-manifest.json`, first decide whether it is evaluating visible tools or endpoint catalog metadata.
- Optional scorecard dimensions should return unscored when their denominator has no applicable visible surface.
- Regression tests should pair a positive hidden-surface case with a negative visible-surface case so future changes do not silently disable endpoint-mirror quality gates.

## Related

- `docs/solutions/best-practices/steinberger-scorecard-scoring-architecture-2026-03-27.md`
- `docs/solutions/logic-errors/cobratree-framework-command-depth-parity.md`
