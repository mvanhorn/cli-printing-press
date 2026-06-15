# Using Printing Press Skills in Codex

Claude Code remains the default and best-tested Printing Press skill host. Codex can also load the same skills through Vercel's `skills` CLI, which is useful when you want the `/printing-press` workflow available in a Codex session.

## 1. Install for Codex

Install the generator binary and the skills, targeting Codex:

```bash
curl -fsSL https://raw.githubusercontent.com/mvanhorn/cli-printing-press/main/scripts/install.sh | bash -s -- --agent codex
```

If the binary is already current and you only want to refresh skills:

```bash
curl -fsSL https://raw.githubusercontent.com/mvanhorn/cli-printing-press/main/scripts/install.sh | bash -s -- --skills-only --agent codex
```

The installer delegates the on-disk link/copy layout to `skills@latest`; it only selects the agent target and does not force copy mode.

The installer still defaults to Claude Code when `--agent` is omitted:

```bash
curl -fsSL https://raw.githubusercontent.com/mvanhorn/cli-printing-press/main/scripts/install.sh | bash
```

## 2. Verify the Skill Install

Check that `skills` sees the global Codex install:

```bash
npx -y skills@latest list -g -a codex --json
```

You should see the Printing Press skills in the JSON output, including `printing-press`.

## 3. Restart or Reload Codex

After installing or refreshing skills, start a new Codex session or reload the current one so Codex reads the updated skill text. If `/printing-press` does not appear, rerun the verification command above and then restart the session again.

## 4. `/printing-press <api>` vs `/printing-press <api> codex`

These are different choices:

- `/printing-press <api>` runs the Printing Press workflow in the current agent host. If you installed the skills with `--agent codex`, that means Codex is the host running the skill.
- `/printing-press <api> codex` enables the skill's Codex delegation mode. In the Claude Code path, Claude keeps orchestration and verification while Phase 3 code-writing tasks are delegated to the Codex CLI.

When the skill detects that it is already inside a Codex sandbox, it disables nested Codex delegation and continues in standard mode. In practice, Codex users should usually run:

```text
/printing-press <api>
```

Use the `codex` suffix primarily from Claude Code when you want Claude-hosted orchestration with Codex CLI code-writing delegation.
