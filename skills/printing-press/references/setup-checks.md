# Setup Checks

Post-contract checks the skill must run after executing the bash setup contract block in `SKILL.md`. These handle four signals the contract emits to stdout: `[setup-error]`, `[repo-upgrade-available]`, `[upgrade-available]`, and the `min-binary-version` compatibility check.

Apply these in order. Each section is conditional — do nothing if its trigger isn't present.

## 1. Refusal: missing binary

If the setup contract output contains a line starting with `[setup-error]`, the printing-press binary is not installed and the contract has already exited non-zero.

**Stop the skill immediately.** Do not proceed to research, generation, or any other work. Surface the message the contract printed (it includes the exact `go install` command) verbatim to the user.

The user must install the binary in their terminal before re-running. Do not offer to auto-install — the README's two-step install is the source of truth, and silent auto-install hides failure modes (network, wrong GOPATH) inside an opaque skill invocation.

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
