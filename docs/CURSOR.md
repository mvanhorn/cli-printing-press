# Using Printing Press CLIs in Cursor

This repo ships **skills** (Markdown playbooks) and generated **`<api>-pp-cli`** binaries. Cursor agents invoke the CLI from the terminal (or you can wire the matching **`<api>-pp-mcp`** server in MCP settings). Use this page when your project does not already document a Cursor-specific flow.

## 1. Install the CLI

Pick one:

- **Installer (skills + binary):** `npx -y @mvanhorn/printing-press install pp-<slug>`  
  Replace `<slug>` with the catalog slug (for example the skill name without the `pp-` prefix when it matches the published package).

- **Go toolchain:** from the [Printing Press Library](https://github.com/mvanhorn/printing-press-library), each CLI documents `go install …` on its README.

Confirm the binary is on `PATH` (`which <cli>` or `<cli> --version`). Many CLIs also support `<cli> doctor` for a quick health check.

## 2. Add the skill to Cursor

Published mirrors live under [`cli-skills/`](https://github.com/mvanhorn/printing-press-library/tree/main/cli-skills) in the library repo (`pp-<slug>/SKILL.md`). Copy the `SKILL.md` you need into your workspace where Cursor loads skills (for example `.cursor/skills/<skill-name>/SKILL.md`), or install skills globally the way your team already does for other plugins.

The skill describes **when** to use the CLI, **which flags** matter (`--agent`, `--json`, `--data-source`, etc.), and **auth** expectations.

## 3. Authentication

Auth is **per API**. Open the matching library README:

`https://github.com/mvanhorn/printing-press-library/tree/main/library/<category>/<slug>/README.md`

Typical patterns:

- Environment variables (for example `*_API_KEY`)
- `auth` subcommands on the CLI, if generated
- Optional **MCP** wiring with the same secrets documented on the README

Never commit secrets; use env vars or your OS secret store.

## 4. Running commands from the agent

- Prefer following the **skill** step-by-step.
- For ad-hoc use, run the CLI in Cursor’s terminal. Agent-friendly defaults are usually behind `--agent` (often implies `--json --no-input --no-color --yes`).
- Rely on **typed exit codes** (`4` = auth, `5` = API, `7` = rate limit) so the agent can retry or fix configuration.

## 5. CLI vs MCP in Cursor

The press generates **both** a Cobra CLI and an MCP server from the same spec. Shell-based flows (skills + terminal) usually spend **fewer context tokens** than loading a large MCP tool surface; MCP is convenient when you want IDE-native tool discovery. See [Why CLIs plus MCP](../README.md#why-clis-plus-mcp) in the main README.

## 6. Per-CLI extras

Some library packages add a **`CURSOR.md`** or extra notes next to `README.md`. If present, it lives beside the CLI under `library/<category>/<slug>/` in the Printing Press Library.

## 7. Commit and PR style in *your* repo (not the Press)

Skills and CLIs from this ecosystem do **not** define how you commit in **your** application monorepo. Always follow the **`AGENTS.md`**, **`CONTRIBUTING.md`**, and Cursor rules **in the repository where you are committing**.

Examples:

- Some organizations use imperative subjects **without** Conventional Commits (`feat:`, `fix:` prefixes are disallowed). Their root **`AGENTS.md`** states that explicitly.
- **This** repository (`mvanhorn/cli-printing-press`) uses **required** Conventional Commits with a fixed scope list; see [AGENTS.md](../AGENTS.md#commit-style) here before contributing to the generator.

If your workspace has no `AGENTS.md` yet, add one that matches your team’s policy so agents and humans do not assume the wrong commit format.
