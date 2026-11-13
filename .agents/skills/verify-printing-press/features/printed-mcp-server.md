# Printed MCP server

The printed `<api>-pp-mcp` binary serves the CLI's capabilities to agents over MCP stdio. It offers typed endpoint tools, framework tools (`context`, `sql`, `search`), and a runtime mirror of the Cobra tree. Each tool carries `readOnlyHint` / `destructiveHint` / `openWorldHint` annotations, and hosts use those to decide when to ask permission. `<PREFIX>_MCP_READ_ONLY=1` keeps only tools annotated `readOnlyHint: true`.

## Sub-features

- `mcp-catalog` `tools/list` returns every user-facing command once: typed endpoints plus mirrored novel commands, without framework skips like `auth`, `doctor`, or `version`.
- `mcp-annotations` GET-backed and read-only-annotated tools say `readOnlyHint: true, destructiveHint: false`, and write tools say `readOnlyHint: false, destructiveHint: true`.
- `mcp-call-read` `tools/call` on a read tool hits the API and returns the body as text content.
- `mcp-read-only` with `<PREFIX>_MCP_READ_ONLY=1`, every non-read-only tool is gone from `tools/list`, and calling one fails with `tool not found`.

## How to get to it (user POV)

- An MCP host (Claude Desktop, Claude Code, Cursor) launches `printing-press-golden-pp-mcp` over stdio, often from the `.mcpb` bundle in `build/`.
- The agent calls `tools/list`, then `tools/call`.

## Driving it with pp-verify

Preconditions:

- Healthy doctor.

- **Catalog.** `$V mcp "$RUN" list`. Exit 0, a JSON array of 36 tools on the golden print. It includes `projects_list`, `projects_create`, `context`, `sql`, `search`, `sync`, and `teach`, and has no `auth`, `doctor`, or `version`.
- **Annotations.** In the same output, `projects_list`, `currencies_list`, and `recall` have `readOnlyHint: true, destructiveHint: false`. `projects_create`, `projects_tasks_update-project`, `sync`, `teach`, and `learnings_forget` have `readOnlyHint: false, destructiveHint: true`.
- **Call a read.** `$V mcp "$RUN" call projects_get '{"projectId":"proj-1"}'`. `content[0].text` contains `"id": "proj-1"`. The drive's `.requests.jsonl` has exactly `GET /v1/projects/proj-1`.
- **Read-only switch.** `$V mcp "$RUN" -e PRINTING_PRESS_GOLDEN_MCP_READ_ONLY=1 list`. 21 tools, every one `readOnlyHint: true`.
- **Read-only refuses writes.** `$V mcp "$RUN" -e PRINTING_PRESS_GOLDEN_MCP_READ_ONLY=1 call projects_create '{"name":"X","visibility":"private"}'`. Exit 1, with `{"code": -32602, "message": "tool 'projects_create' not found: tool not found"}`, and the drive's `.requests.jsonl` is empty.

## Gotchas

- Tool counts are for the golden fixture at the commit this map was written on. A different count after a change is a finding to explain, not automatically a bug. Diff the names.
- `context`, `sql`, and `search` currently report `openWorldHint: true`, while AGENTS.md calls them "read-only and local-only". `mcp_tools.go.tmpl` sets only the read-only and destructive hints, and mcp-go's `NewTool` defaults `openWorldHint` to true. Treat a change here as intended only if the template sets it explicitly.
- `mcp_call.py` exits non-zero on a JSON-RPC error. A tool-level failure comes back as a result with `isError: true` and exit 0, so read the content.
- `sql` exists only as an MCP tool. The printed CLI has no `sql` command.
