# Setup Checks

Post-contract checks the skill must run after executing the bash setup contract block in `SKILL.md`. These handle the contract output signals: `[setup-error]`, optional `[local-binary-stale]` / `[local-binary-rebuilt]` repo-mode rebuild markers, `[repo-upgrade-available]`, the always-emitted `PRINTING_PRESS_BIN=<abs-path>` and `PRESS_REPO_MODE=<true|false>` markers, the global open-agent-skills freshness check, the `min-binary-version` compatibility check, `[upgrade-required]`, `[upgrade-available]`, `[browser-tools-missing]`, and optional `[binary-shadow]` advisory.

Apply these in order. The preamble below runs unconditionally; each numbered section after it is conditional — do nothing if its trigger isn't present.

## Preamble: Capture the absolute binary path (unconditional)

Before applying any numbered section below, capture the `PRINTING_PRESS_BIN=<absolute path to the binary>` line the contract emitted to stdout. Every generator invocation referenced anywhere below — including the `version --json` calls in sections 4 and 5 — must be made using that absolute path (substitute the captured value, not the literal `$PRINTING_PRESS_BIN` token). The contract's `export PATH=...` line only affects the single Bash tool call it runs in; later Bash tool calls open fresh shells where bare `cli-printing-press` or legacy bare `printing-press` resolves against the user's default `PATH`, and a stale globally-installed binary (`$HOME/go/bin/cli-printing-press` from an earlier `go install`, a Homebrew copy, etc.) or the public catalog installer can silently shadow the local repo build the contract selected. Using the absolute path eliminates the shadow.

Also capture `PRESS_REPO_MODE=<true|false>`. When it is `true`, the current session is running from a repo checkout/plugin-dir and should use the checkout's skill files, not mutate the user's global skill install as part of this run. When it is `false`, run the global open-agent-skills freshness check in section 3, but treat a missing global open-agent-skills entry as a non-blocking signal that this skill may have been loaded from another install surface.

If `PRINTING_PRESS_BIN` was emitted as an empty value (`PRINTING_PRESS_BIN=`), the contract was unable to resolve a binary; this should have already been surfaced as `[setup-error]` (handled in section 1 below). Treat an empty value here as a setup-error fallback and stop.

This rule applies to *all* generator command references in the rest of this document. Where sections below write `cli-printing-press version --json` or similar, read that as shorthand for `<PRINTING_PRESS_BIN> version --json` with the captured value substituted.

## 1. Refusal: missing prerequisite

If the setup contract output contains a line starting with `[setup-error]`, a required prerequisite is missing (the cli-printing-press binary or the Go toolchain) and the contract has already exited non-zero.

**Stop the skill immediately.** Do not proceed to research, generation, or any other work. Surface the message the contract printed (it includes the exact install command or download URL) verbatim to the user.

The user must install the missing prerequisite in their terminal before re-running. Do not offer to auto-install — the README's install flow is the source of truth for the binary, and silent auto-install hides failure modes (network, wrong GOPATH, no Go toolchain) inside an opaque skill invocation.

If the contract output contains `[local-binary-stale]` followed by `[local-binary-rebuilt]`, continue setup. The repo-mode local binary was older than the checked-out source version and the contract rebuilt it before emitting `PRINTING_PRESS_BIN`. If `[local-binary-stale]` is followed by `[setup-error]`, this section's refusal rule applies.

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
PRESS_HOME="${PRINTING_PRESS_HOME:-$HOME/printing-press}"
mkdir -p "$PRESS_HOME"
printf "last_check=%s\nmode=repo\nskipped_repo_main=%s\n" "$(date +%s)" "$PRESS_REPO_MAIN" > "$PRESS_HOME/.version-check"
```

Prompt again only when `origin/main` advances to a different SHA.

If no `[repo-upgrade-available]` line was emitted, skip this section entirely.

## 3. Global open-agent-skills freshness check

If `PRESS_REPO_MODE=true`, skip this section entirely. The repo checkout/plugin-dir is the source of truth for skill files in that mode, and section 2 already handles updating the checkout from `origin/main`.

If `PRESS_REPO_MODE=false`, run the targeted global open-agent-skills updater before continuing:

```bash
# PRINTING_PRESS_SKILL_UPDATE_START
npx -y skills@latest update -g \
  printing-press \
  printing-press-amend \
  printing-press-import \
  printing-press-output-review \
  printing-press-polish \
  printing-press-publish \
  printing-press-reprint \
  printing-press-retro \
  printing-press-score
# PRINTING_PRESS_SKILL_UPDATE_END
```

Interpret the output as follows:

- If it reports `✓ Updated N skill(s)` with `N > 0`, stop immediately. Tell the user:

  > "Updated N Printing Press skill(s) on disk. This agent session may still have the old skill text loaded. Restart the agent session, then re-run `/printing-press` before continuing."

  Do not proceed to research, generation, scoring, publishing, or any other workflow after a skill update.
- If it reports `All global skills are up to date` or otherwise completes without an updated-skill summary, continue.
- If it reports `No installed skills found matching: ...`, continue without blocking. The current skill is already running, so absence from the global open-agent-skills registry usually means this session was loaded from another install surface, such as the Claude Code plugin/marketplace channel, a project-scoped skill install, or a local plugin directory. Do not tell the user to reinstall through `npx skills` as a prerequisite. If they explicitly ask how to move to the global open-agent-skills install path, give them the skills-only installer command so they do not have to name individual skills:

  ```bash
  curl -fsSL https://raw.githubusercontent.com/mvanhorn/cli-printing-press/main/scripts/install.sh | bash -s -- --skills-only
  ```

  Then tell them to restart the agent session.
- If it reports one or more `Failed to update ...` lines or exits non-zero, stop and surface the failure. For manual repair, tell the user to run the skills-only installer command below, then restart the agent session. Do not continue with potentially mixed skill files.

  ```bash
  curl -fsSL https://raw.githubusercontent.com/mvanhorn/cli-printing-press/main/scripts/install.sh | bash -s -- --skills-only
  ```

This command mutates global skill files, so never run it silently after user work has started. It belongs in preflight before any user-facing prompt.

## 4. Min-binary-version compatibility

Check binary version compatibility against the skill's declared minimum. Read the `min-binary-version` field from the skill's YAML frontmatter. Run `<PRINTING_PRESS_BIN> version --json` (using the absolute path captured in the preamble — not bare `cli-printing-press` or legacy bare `printing-press`, which would resolve against the user's default `PATH` and could interrogate a stale global or the public catalog installer) and parse the version from the output. Compare it to `min-binary-version` using semver rules.

If the installed binary is older than the minimum, stop the skill immediately and tell the user:

> "cli-printing-press binary vX.Y.Z is older than the minimum required vA.B.C. Run `go install github.com/mvanhorn/cli-printing-press/v4/cmd/cli-printing-press@latest` to update."

Do not proceed to research, scoring, publishing, or any other workflow when the binary is below `min-binary-version`. This is the compatibility floor, not a freshness advisory.

## 4.5. Required-minimum (currency floor) hard gate

If the setup contract output contains a line starting with `[upgrade-required]`, the installed binary is below the **currently supported** minimum — older releases generate CLIs with known, since-fixed bugs. This is distinct from section 4: section 4 is the skill's frozen compatibility floor (the skill literally cannot run below it, and it only moves on a major version); the currency floor is a freshness *requirement* that maintainers raise out-of-band (via the published `supported-versions.txt`) as bad-output bugs get fixed, with no skill or binary release. Parse the follow-up lines:

- `PRESS_REQUIRED_MIN=<minimum supported version>`
- `PRESS_REQUIRED_INSTALLED=<installed version>`
- `PRESS_REQUIRED_REASON=<one-line reason>`

This is a hard gate. Do not proceed to research, generation, scoring, publishing, or any other workflow on a binary below the floor. Offer a one-click upgrade via `AskUserQuestion` before continuing:

- **question:** `"printing-press v<installed> is below the minimum supported v<minimum>. <reason> Upgrade now? Takes about 10 seconds."`
- **header:** `"Update required"`
- **multiSelect:** `false`
- **options:**
  1. **Yes — upgrade now** — `"Run go install and continue on the latest released binary."`
  2. **Cancel** — `"Stop the run; do not generate on a binary below the supported floor."`

If the user picks **Yes**, run:

```bash
go install github.com/mvanhorn/cli-printing-press/v4/cmd/cli-printing-press@latest
```

Then **re-resolve `PRINTING_PRESS_BIN`** exactly as section 5 describes (the new binary may land at a different path than the one the contract captured), confirm with `<PRINTING_PRESS_BIN> version --json`, tell the user `"Upgraded to v<new>."`, and continue this run with the upgraded binary.

If the user picks **Cancel**, stop the skill immediately. Unlike section 5's `[upgrade-available]` advisory, there is **no skip-and-continue** — below the floor the only paths are upgrade or abort.

If the upgrade command fails (network, auth, missing Go toolchain), surface the failure and stop. Do not fall back to generating on the below-floor binary.

If no `[upgrade-required]` line was emitted, skip this section entirely.

## 5. Interactive standalone binary upgrade prompt

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
go install github.com/mvanhorn/cli-printing-press/v4/cmd/cli-printing-press@latest
```

After `go install` completes, **re-resolve `PRINTING_PRESS_BIN`** before confirming. `go install` writes to `$(go env GOBIN)` if set, otherwise `$(go env GOPATH)/bin` — which may not be the same path the contract originally captured (e.g. when the pre-upgrade binary was the legacy `/opt/homebrew/bin/printing-press` or `/usr/local/bin/printing-press`, the new binary lives at `$GOPATH/bin/cli-printing-press` and the old one is unchanged). Run this re-resolution in a single Bash call so its stdout becomes the new captured value:

```bash
_gobin="$(go env GOBIN 2>/dev/null)"
[ -z "$_gobin" ] && _gobin="$(go env GOPATH 2>/dev/null)/bin"
if [ -x "$_gobin/cli-printing-press" ]; then
  echo "PRINTING_PRESS_BIN=$_gobin/cli-printing-press"
elif command -v cli-printing-press >/dev/null 2>&1; then
  echo "PRINTING_PRESS_BIN=$(command -v cli-printing-press)"
elif [ -x "$_gobin/printing-press" ] && "$_gobin/printing-press" version --json >/dev/null 2>&1; then
  echo "PRINTING_PRESS_BIN=$_gobin/printing-press"
else
  echo "PRINTING_PRESS_BIN=$(command -v cli-printing-press 2>/dev/null || true)"
fi
```

Capture the new `PRINTING_PRESS_BIN=<abs-path>` value and use it for every subsequent generator invocation in the rest of this run, overriding the value captured in the preamble. Then confirm with `<PRINTING_PRESS_BIN> version --json` and tell the user `"Upgraded to v<new>."` **Continue this current setup run with the freshly installed binary on disk — do not stop, do not reload the session, do not skip the remaining checks (min-binary-version compatibility, etc.).** Skill freshness was already handled by section 3, so this binary-only update does not require a session restart.

If the upgrade command fails (network error, auth error, etc.), surface the failure to the user and continue with the current binary — do not block the run on a failed upgrade. The user can re-run later.

If no `[upgrade-available]` line was emitted, skip this section entirely.

## 6. Interactive browser-sniff backend install prompt

If the setup contract output contains a line starting with `[browser-tools-missing]`, parse the follow-up lines:

- `PRESS_BROWSER_USE_MISSING=<true|false>`
- `PRESS_AGENT_BROWSER_MISSING=<true|false>`

Then ask the user via `AskUserQuestion` before continuing setup. The prompt fires every run when either tool is missing — there is no decline cache. Re-prompting is intentional: browser-use and agent-browser are the preferred Phase 1.7 backends, and mid-flight install gates during generation are more disruptive than one short preflight prompt.

- **question** (compose based on which are missing — pick the matching row):
  - Both missing: `"browser-use and agent-browser are not installed. These are the preferred Phase 1.7 browser-sniff backends — broadly useful for future runs and avoids mid-flight install prompts. (chrome-MCP is a narrow-case fallback, not a substitute.) Install now?"`
  - Only `browser-use` missing: `"browser-use is not installed. It is the preferred Phase 1.7 browser-sniff primary backend — broadly useful for future runs and avoids mid-flight install prompts. Install now?"`
  - Only `agent-browser` missing: `"agent-browser is not installed. It is the secondary browser-sniff backend (used for cookie capture from running Chrome) — broadly useful for future runs and avoids mid-flight install prompts. Install now?"`
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
# Use `uv tool install` (not `uv pip install`). `uv pip install` targets the
# active venv and won't put the binary in PATH outside it; `uv tool install`
# creates an isolated env and symlinks the entry-point into `~/.local/bin`.
if command -v uv >/dev/null 2>&1; then
  uv tool install browser-use
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

After installing `agent-browser` through brew or npm, complete its browser-binary setup as a user-run step:

```text
! agent-browser install
```

The leading `!` is intentional: surface the command for the user to run manually instead of invoking it through the agent's shell tool. Some harness classifiers block installer subcommands from a newly installed tool when the agent runs them directly. Do not treat `command -v agent-browser` alone as a complete install after the package-manager step; the `agent-browser install` step must complete before browser-sniff flows rely on it. Only do this post-install step when this section just installed `agent-browser`; if `PRESS_AGENT_BROWSER_MISSING=false`, skip redundant setup for the already-present binary. This preflight does not launch agent-browser to prove browser-cache readiness for pre-existing installs; if a later browser-sniff step reports missing browser binaries, surface `! agent-browser install` then and use a fallback backend until the user confirms it completed.

After install, verify `browser-use` with `command -v browser-use`. If this section just installed `agent-browser`, verify it with both `command -v agent-browser` and confirmation that the user-run `agent-browser install` step completed. If the user declines the manual step, it fails, or completion is unclear, do not run it through the agent shell; surface the incomplete setup and continue without treating `agent-browser` as available for browser-sniff. If `PRESS_AGENT_BROWSER_MISSING=false`, do not require post-install confirmation for the already-installed binary. Then confirm successful installs to the user: `"Installed <tool>."` **Continue this current setup run with the newly available tools — do not stop, do not skip the remaining checks (min-binary-version compatibility, etc.).**

If an install command fails (no Python, no Node.js, network error), surface the failure to the user and continue without the missing backend. Do not block the run on a failed install — runs using vendor specs, `--spec`, or `--har` do not need these tools, and the lazy Step 1b prompt in `browser-sniff-capture.md` remains as a fallback if browser-sniff is later invoked.

If the user picks **Skip for this run**, continue without prompting further this run. The decision is not cached — the prompt re-fires on the next run if the tool is still missing.

If no `[browser-tools-missing]` line was emitted, skip this section entirely.

## 7. Optional shadow advisory

If the setup contract output contains a line starting with `[binary-shadow]`, parse the follow-up lines:

- `PRESS_BIN_LOCAL_VERSION=<version reported by the local build>`
- `PRESS_BIN_GLOBAL_VERSION=<version reported by the differing global on PATH>`
- `PRESS_BIN_GLOBAL_PATH=<absolute path to the differing global>`

Surface a single-line note to the user before continuing — informational only, do not prompt:

> "Note: a global printing-press v`<global>` is installed at `<path>` but the local repo build is v`<local>`. This run will use the local build (the absolute path the preflight selected); the global is unchanged."

Then continue. Do not modify or remove the global. The note exists so the user can reconcile the divergence on their own time (typically with `go install ...@latest` once they want the new version everywhere).

If no `[binary-shadow]` line was emitted, skip the advisory and continue.
