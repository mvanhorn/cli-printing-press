---
title: "Windows MCPB companion CLIs need target executable names"
date: 2026-05-23
category: logic-errors
module: internal/generator/templates/cobratree
problem_type: logic_error
component: tooling
related_components:
  - internal/cli
  - internal/pipeline
symptoms:
  - "Windows MCPB-bundled cobratree shell-out tools reported companion CLI binary not found."
  - "Generated SiblingCLIPath searched for the unsuffixed CLI name even though Windows bundle binaries use .exe."
  - "A template-only fix could still miss the bundled sibling when bundle staging archived the companion under the raw logical name."
root_cause: logic_error
resolution_type: code_fix
severity: high
tags:
  - mcpb
  - windows
  - cobratree
  - executable-names
  - companion-cli
---

# Windows MCPB companion CLIs need target executable names

## Problem

Printed CLI MCP servers shell out to their companion CLI for cobratree-mirrored tools. On Windows MCPB installs, the companion binary must be found as a sibling executable with a `.exe` suffix, or every runtime-mirrored tool fails before it can call the CLI.

## Symptoms

- MCPB-bundled Windows installs failed cobratree shell-out tools with `companion CLI binary not found`.
- The generated `internal/mcp/cobratree/cli_path.go` built the sibling candidate from the logical CLI name without the Windows suffix.
- A partial fix in the template was insufficient unless `cli-printing-press bundle` also staged and zipped the companion CLI under the same target executable name.

## What Didn't Work

- Patching a generated printed CLI was not durable because `internal/mcp/cobratree/` is generator-owned and overwritten on regen.
- Updating only `SiblingCLIPath` left a second mismatch: cross-compilation uses the exact `go build -o` path, so the bundle code also has to choose a target-GOOS output and archive name for the companion CLI.
- Storing the companion CLI path in `manifest.json` is not the right escape hatch. MCPB v0.3 hosts reject unknown manifest keys, so the manifest stays schema-clean and the runtime keeps sibling, env-var, and PATH resolution.

## Solution

Keep logical binary names platform-agnostic, and resolve filesystem/archive executable names at the target boundary:

- Generated `SiblingCLIPath` calls a small `cliExecutableName(runtime.GOOS)` helper so Windows searches for `<api>-pp-cli.exe`, while Linux and macOS keep `<api>-pp-cli`.
- `cli-printing-press bundle` uses `internal/platform.ExecutablePathForGOOS` when choosing the companion CLI staging path and zip entry for the bundle target platform.
- The bundle still passes the raw logical CLI name to `go build` as the package name, and keeps MCPB `manifest.json` free of companion-CLI metadata.

## Why This Works

The runtime resolver and the bundle archive now agree on the same target-specific filename. A Windows MCPB bundle contains `bin/<api>-pp-cli.exe`, and the generated MCP server looks for the same sibling before falling back to `<API>_CLI_PATH` or `PATH`.

Separating logical names from executable paths also avoids leaking platform suffixes into package names, manifest identity, or generated command names.

## Prevention

- For Windows target builds, route filesystem executable paths through `internal/platform.ExecutablePathForGOOS` or an emitted equivalent when generated code cannot import the generator package.
- When changing cobratree runtime resolution, check bundle staging and archive names in the same pass. The generated resolver and MCPB package layout are one contract.
- Add tests at both layers: generated cobratree helpers should simulate Windows and non-Windows GOOS values, and bundle tests should assert the companion CLI zip entry uses the target executable name.

## Related

- `docs/solutions/logic-errors/cobratree-framework-command-depth-parity.md`
