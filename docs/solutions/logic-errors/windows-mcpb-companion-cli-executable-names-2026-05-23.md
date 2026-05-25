---
title: "Windows MCPB bundled executables need target executable names"
date: 2026-05-23
last_updated: 2026-05-24
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
  - "Windows MCPB server bundles could zip and launch the MCP server under an unsuffixed path even when the target platform was Windows."
root_cause: logic_error
resolution_type: code_fix
severity: high
tags:
  - mcpb
  - windows
  - cobratree
  - executable-names
  - companion-cli
  - manifest
---

# Windows MCPB bundled executables need target executable names

## Problem

Printed CLI MCPB bundles contain executable binaries that the host launches directly. On Windows MCPB installs, both the MCP server and any companion CLI must be archived and referenced with a `.exe` suffix, or the installed bundle cannot launch the server or mirrored CLI tools.

## Symptoms

- MCPB-bundled Windows installs failed cobratree shell-out tools with `companion CLI binary not found`.
- The generated `internal/mcp/cobratree/cli_path.go` built the sibling candidate from the logical CLI name without the Windows suffix.
- A partial fix in the template was insufficient unless `cli-printing-press bundle` also staged and zipped the companion CLI under the same target executable name.
- `cli-printing-press bundle --platform windows/<arch>` could package the MCP server as `bin/<api>-pp-mcp` and leave `manifest.json` pointing at the unsuffixed path even when prebuilt inputs were `.exe` files.

## What Didn't Work

- Patching a generated printed CLI was not durable because `internal/mcp/cobratree/` is generator-owned and overwritten on regen.
- Updating only `SiblingCLIPath` left a second mismatch: cross-compilation uses the exact `go build -o` path, so the bundle code also has to choose a target-GOOS output and archive name for the companion CLI.
- Storing the companion CLI path in `manifest.json` is not the right escape hatch. MCPB v0.3 hosts reject unknown manifest keys, so the manifest stays schema-clean and the runtime keeps sibling, env-var, and PATH resolution.
- Updating the MCP server path by unmarshaling into the typed manifest struct and marshaling it back would silently drop hand-edited MCPB fields that the generator does not model. Bundle-time manifest patching must preserve unknown JSON fields.

## Solution

Keep logical binary names platform-agnostic, and resolve filesystem/archive executable names at the target boundary:

- Generated `SiblingCLIPath` calls a small `cliExecutableName(runtime.GOOS)` helper so Windows searches for `<api>-pp-cli.exe`, while Linux and macOS keep `<api>-pp-cli`.
- `cli-printing-press bundle` uses `internal/platform.ExecutablePathForGOOS` when choosing MCP server and companion CLI staging paths and zip entries for the bundle target platform.
- The bundle ZIP's `manifest.json` is patched so `server.entry_point` and `server.mcp_config.command` point at the target executable name when it differs from the source manifest.
- The bundle still passes the raw logical CLI name to `go build` as the package name, and keeps MCPB `manifest.json` free of companion-CLI metadata.
- Bundle-time manifest patching operates on generic JSON and only changes the launch fields, so unknown top-level, server-level, and launch-level MCPB fields survive.

## Why This Works

The MCPB manifest, ZIP entries, and runtime resolver now agree on the same target-specific filenames. A Windows MCPB bundle contains `bin/<api>-pp-mcp.exe` and, when a companion CLI is present, `bin/<api>-pp-cli.exe`; the manifest launches the suffixed MCP server, and the generated MCP server looks for the suffixed companion before falling back to `<API>_CLI_PATH` or `PATH`.

Separating logical names from executable paths also avoids leaking platform suffixes into package names, manifest identity, generated command names, or non-Windows bundle layouts.

## Prevention

- For Windows target builds, route filesystem executable paths through `internal/platform.ExecutablePathForGOOS` or an emitted equivalent when generated code cannot import the generator package.
- When changing MCPB runtime resolution, check bundle staging, archive names, and manifest launch fields in the same pass. The generated resolver, bundle manifest, and ZIP package layout are one contract.
- Add tests at both layers: generated cobratree helpers should simulate Windows and non-Windows GOOS values, and bundle tests should assert MCP server and companion CLI zip entries plus manifest launch fields use the target executable name.
- When patching MCPB manifests at bundle time, preserve unknown JSON fields so hand-edited upstream MCPB schema extensions are not lost.

## Related

- `docs/solutions/logic-errors/cobratree-framework-command-depth-parity.md`
