# Setup Checks

Post-contract checks the skill must run after executing the bash setup contract block in `SKILL.md`. These handle three signals the contract emits to stdout: `[setup-error]`, `[upgrade-available]`, and the `min-binary-version` compatibility check.

Apply these in order. Each section is conditional — do nothing if its trigger isn't present.

## 1. Refusal: missing binary

If the setup contract output contains a line starting with `[setup-error]`, the printing-press binary is not installed and the contract has already exited non-zero.

**Stop the skill immediately.** Do not proceed to research, generation, or any other work. Surface the message the contract printed (it includes the exact `go install` command and `GOPRIVATE` guidance) verbatim to the user.

The user must install the binary in their terminal before re-running. Do not offer to auto-install — the README's two-step install is the source of truth, and silent auto-install hides failure modes (GOPRIVATE auth, network, wrong GOPATH) inside an opaque skill invocation.

## 2. Interactive upgrade prompt

If the setup contract output contains a line starting with `[upgrade-available]`, parse the two follow-up lines for the version values:

- `PRESS_UPGRADE_AVAILABLE=<latest>`
- `PRESS_UPGRADE_INSTALLED=<installed>`

Then ask the user via `AskUserQuestion` before continuing setup:

- **question:** `"printing-press v<latest> is available (you have v<installed>). Upgrade now? Takes about 10 seconds."`
- **header:** `"Update available"`
- **multiSelect:** `false`
- **options:**
  1. **Yes — upgrade now** — `"Run go install and use the latest version for this session."`
  2. **Skip — keep current version** — `"Continue with the current binary."`

If the user picks **Yes**, run:

```bash
GOPRIVATE=github.com/mvanhorn/* go install github.com/mvanhorn/cli-printing-press/v3/cmd/printing-press@latest
```

After it completes, confirm with `printing-press version --json` and tell the user `"Upgraded to v<new>."` Then continue setup.

If the upgrade command fails (network error, auth error, etc.), surface the failure to the user and continue with the current binary — do not block the run on a failed upgrade. The user can re-run later.

If no `[upgrade-available]` line was emitted, skip this section entirely.

## 3. Min-binary-version compatibility

Check binary version compatibility against the skill's declared minimum. Read the `min-binary-version` field from the skill's YAML frontmatter. Run `printing-press version --json` and parse the version from the output. Compare it to `min-binary-version` using semver rules.

If the installed binary is older than the minimum, warn the user:

> "printing-press binary vX.Y.Z is older than the minimum required vA.B.C. Run `go install github.com/mvanhorn/cli-printing-press/v3/cmd/printing-press@latest` to update."

Continue anyway but surface the warning prominently. (Note: if the user just declined the optional upgrade in section 2, they may still pass min-version compatibility here — that's fine. The two checks have different bars.)
