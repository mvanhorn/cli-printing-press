# Setup Checks

Post-contract checks the skill must run after executing the bash setup contract block in `SKILL.md`. These handle five signals the contract emits to stdout: `[setup-error]`, `[repo-upgrade-available]`, `[upgrade-available]`, `[browser-tools-missing]`, and the `min-binary-version` compatibility check.

Apply these in order. Each section is conditional — do nothing if its trigger isn't present.

## 1. Refusal: missing prerequisite

If the setup contract output contains a line starting with `[setup-error]`, a required prerequisite is missing (the printing-press binary or the Go toolchain) and the contract has already exited non-zero.

**Stop the skill immediately.** Do not proceed to research, generation, or any other work. Surface the message the contract printed (it includes the exact install command or download URL) verbatim to the user.

The user must install the missing prerequisite in their terminal before re-running. Do not offer to auto-install — the README's two-step install is the source of truth for the binary, and silent auto-install hides failure modes (network, wrong GOPATH, no Go toolchain) inside an opaque skill invocation.

## 2. Interactive repo upgrade prompt

If the setup contract output contains a line starting with `[repo-upgrade-available]`, parse the follow-up lines:

- `PRESS_REPO_DIR=<absolute repo path>`
- `PRESS_REPO_HEAD=<current HEAD sha>`
- `PRESS_REPO_MAIN=<origin/main sha>`

Then ask the user via `AskUserQuestion` before continuing setup:

- **question:** `"origin/main has newer Printing Press changes. Pull the latest main now? After this, reload the skill with /reload-plugin."`
- **header:** `"Update repo"`
- **multiSelect:** `false`
- **options:**
  1. **Yes — pull main** — `"Run git pull --ff-only origin main in the Printing Press repo, then stop so you can reload the skill."`
  2. **Skip — keep current checkout** — `"Continue with the current checkout."`

If the user picks **Yes**, run:

```bash
git -C "$PRESS_REPO_DIR" pull --ff-only origin main
```

After it completes, tell the user:

> "Updated the Printing Press checkout. Run `/reload-plugin`, then re-run `/printing-press` so the refreshed skill and rebuilt local binary are used."

Then stop the skill immediately. Do not continue the current run, because the skill text that is executing may now be stale.

If the pull fails, surface the failure to the user and continue with the current checkout. Do not attempt a non-fast-forward merge, rebase, reset, stash, or branch switch from the skill preflight.

If the user picks **Skip**, record the skipped target SHA so the same update is not prompted again:

```bash
mkdir -p "$HOME/printing-press"
printf "last_check=%s\nmode=repo\nskipped_repo_main=%s\n" "$(date +%s)" "$PRESS_REPO_MAIN" > "$HOME/printing-press/.version-check"
```

Prompt again only when `origin/main` advances to a different SHA.

If no `[repo-upgrade-available]` line was emitted, skip this section entirely.

## 3. Min-binary-version compatibility

Check binary version compatibility against the skill's declared minimum. Read the `min-binary-version` field from the skill's YAML frontmatter. Run `printing-press version --json` and parse the version from the output. Compare it to `min-binary-version` using semver rules.

If the installed binary is older than the minimum, stop the skill immediately and tell the user:

> "printing-press binary vX.Y.Z is older than the minimum required vA.B.C. Run `go install github.com/mvanhorn/cli-printing-press/v4/cmd/printing-press@latest` to update."

Do not proceed to research, scoring, publishing, or any other workflow when the binary is below `min-binary-version`. This is the compatibility floor, not a freshness advisory.

## 4. Interactive standalone binary upgrade prompt

If the setup contract output contains a line starting with `[upgrade-available]`, parse the two follow-up lines for the version values:

- `PRESS_UPGRADE_AVAILABLE=<latest>`
- `PRESS_UPGRADE_INSTALLED=<installed>`

Then ask the user via `AskUserQuestion` before continuing setup:

- **question:** `"printing-press v<latest> is available (you have v<installed>). Upgrade now? Takes about 10 seconds."`
- **header:** `"Update available"`
- **multiSelect:** `false`
- **options:**
  1. **Yes — upgrade now** — `"Run go install and use the latest released binary for this session."`
  2. **Skip — keep current version** — `"Continue with the current binary."`

If the user picks **Yes**, run:

```bash
go install github.com/mvanhorn/cli-printing-press/v4/cmd/printing-press@latest
```

After it completes, confirm with `printing-press version --json` and tell the user `"Upgraded to v<new>."` **Continue this current setup run with the freshly installed binary on disk — do not stop, do not reload the session, do not skip the remaining checks (min-binary-version compatibility, etc.).**

Separately, as out-of-band advice for the user's *next* session (not a stop signal for this run), tell them they can also refresh their installed skill files outside the repo checkout by running one of:

```bash
gh skill update
```

or:

```bash
npx skills update
```

These two commands update skill files that live outside the repo; they only take effect after the user reloads or restarts the agent session, which they should do *after* the current run finishes. Frame this clearly to the user as "for next time" guidance, then continue setup with the newly installed binary.

If the upgrade command fails (network error, auth error, etc.), surface the failure to the user and continue with the current binary — do not block the run on a failed upgrade. The user can re-run later.

If no `[upgrade-available]` line was emitted, skip this section entirely.

## 5. Interactive browser-sniff backend install prompt

If the setup contract output contains a line starting with `[browser-tools-missing]`, parse the follow-up lines:

- `PRESS_BROWSER_USE_MISSING=<true|false>`
- `PRESS_AGENT_BROWSER_MISSING=<true|false>`

Then ask the user via `AskUserQuestion` before continuing setup. The prompt fires every run when either tool is missing — there is no decline cache. Re-prompting is intentional: browser-use and agent-browser are the preferred Phase 1.7 backends, and mid-flight install gates during generation are more disruptive than one short preflight prompt.

- **question:** `"browser-use and/or agent-browser are not installed. These are the preferred Phase 1.7 browser-sniff backends — broadly useful for future runs and avoids mid-flight install prompts. (chrome-MCP is a narrow-case fallback, not a substitute.) Install now?"`
- **header:** `"Browser-sniff backends"`
- **multiSelect:** `false`
- **options:** compose based on which are missing — see below.

**Option composition.**

- Both missing → (1) **Install both** (Recommended), (2) **Install browser-use only**, (3) **Install agent-browser only**, (4) **Skip for this run**.
- Only `browser-use` missing → (1) **Install browser-use** (Recommended), (2) **Skip for this run**.
- Only `agent-browser` missing → (1) **Install agent-browser** (Recommended), (2) **Skip for this run**.

**Install commands.**

For `browser-use`:

```bash
if command -v uv >/dev/null 2>&1; then
  uv pip install browser-use
elif command -v pip >/dev/null 2>&1; then
  pip install browser-use
else
  echo "Neither uv nor pip found. Install Python first: https://www.python.org/downloads/"
fi
```

For `agent-browser`:

```bash
if command -v brew >/dev/null 2>&1; then
  brew install agent-browser
elif command -v npm >/dev/null 2>&1; then
  npm install -g agent-browser
else
  echo "Neither brew nor npm found. Install Node.js first: https://nodejs.org/"
fi
```

After install, verify with `command -v <tool>` and confirm to the user: `"Installed <tool>."` **Continue this current setup run with the newly available tools — do not stop, do not skip the remaining checks (min-binary-version compatibility, etc.).**

If an install command fails (no Python, no Node.js, network error), surface the failure to the user and continue without the missing backend. Do not block the run on a failed install — runs using vendor specs, `--spec`, or `--har` do not need these tools, and the lazy Step 1b prompt in `browser-sniff-capture.md` remains as a fallback if browser-sniff is later invoked.

If the user picks **Skip for this run**, continue without prompting further this run. The decision is not cached — the prompt re-fires on the next run if the tool is still missing.

If no `[browser-tools-missing]` line was emitted, skip this section entirely.
