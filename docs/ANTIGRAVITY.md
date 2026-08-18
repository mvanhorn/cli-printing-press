# Using Printing Press Skills in Antigravity

Antigravity (the Google DeepMind Advanced Agentic Coding environment) supports running Printing Press skills natively.

## 1. Install for Antigravity

Install the generator binary and the skills into your Antigravity environment:

```bash
curl -fsSL https://raw.githubusercontent.com/mvanhorn/cli-printing-press/main/scripts/install.sh | bash -s -- --agent antigravity
```

If the binary is already current and you only want to refresh skills:

```bash
curl -fsSL https://raw.githubusercontent.com/mvanhorn/cli-printing-press/main/scripts/install.sh | bash -s -- --skills-only --agent antigravity
```

The installer populates skills directly into your global Antigravity skills directory (`~/.gemini/config/skills/`).

## 2. Verify Skill Discovery

Verify that the skills are installed:

```bash
ls -d ~/.gemini/config/skills/printing-press*
```

You should see the 9 Printing Press skills:
- `printing-press`
- `printing-press-amend`
- `printing-press-import`
- `printing-press-output-review`
- `printing-press-polish`
- `printing-press-publish`
- `printing-press-reprint`
- `printing-press-retro`
- `printing-press-score`

## 3. Running Printing Press

Start a conversation with Antigravity and invoke:

```text
/printing-press <api-slug>
```

Antigravity will load `~/.gemini/config/skills/printing-press/SKILL.md` and begin the generation pipeline.

## 4. Key Antigravity Tool Conventions

When executing under Antigravity, the skills and generated tools map to native Antigravity primitives:

- **Subagents:** Spawning uses `invoke_subagent` with `TypeName: "research"` or `"self"`. Antigravity operates on a **Reactive Wakeup** model — after dispatching subagents, the parent simply ends its turn and is woken automatically when the subagent responds.
- **Interactive Questions:** Discrete user choices use `ask_question` (`{questions: [{question, options, is_multi_select}]}`). Open-ended questions are output as regular markdown text.
- **Terminal Execution:** In Antigravity's `run_command`, directory changes must use subshells `(cd <dir> && <cmd>)` or set the tool's `Cwd` parameter, avoiding bare `cd`.
- **Code Review & Simplification:** Phase 4.95 runs code review (via reviewer subagents or an installed review skill) and executes a targeted simplification pass to tighten autofix edits.
- **Transcripts:** The `/printing-press-amend` dogfood flow reads Antigravity session transcripts from `<appDataDir>/brain/<conversation-id>/.system_generated/logs/transcript.jsonl`.

## 5. Using Generated MCP Servers in Antigravity

Every printed CLI ships a companion MCP server (`<name>-pp-mcp`). To register it with Antigravity, add it to your Antigravity `mcp_config.json`:

```json
{
  "mcpServers": {
    "<name>-pp-mcp": {
      "command": "<name>-pp-mcp"
    }
  }
}
```
