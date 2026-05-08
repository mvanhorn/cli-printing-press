# Printing Press Golden Printed CLI Agent Guide

This directory is a generated `printing-press-golden-pp-cli` printed CLI. It was produced by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press), so treat systemic fixes as upstream Printing Press fixes first. Keep local edits narrow and document why a generated-tree patch belongs here.

## Local Operating Contract

Start by asking the generated CLI for current runtime truth:

```bash
printing-press-golden-pp-cli doctor --json
printing-press-golden-pp-cli agent-context --pretty
```

Use runtime discovery instead of relying on a copied command list:

```bash
printing-press-golden-pp-cli which "<capability>" --json
printing-press-golden-pp-cli <command> --help
```

Add `--agent` to command invocations for JSON, compact output, non-interactive defaults, no color, and confirmation-safe scripting:

```bash
printing-press-golden-pp-cli <command> --agent
```

Before running an unfamiliar command that may mutate remote state, inspect its help and prefer a dry run:

```bash
printing-press-golden-pp-cli <command> --help
printing-press-golden-pp-cli <command> --dry-run --agent
```

Use `--yes --no-input` only after the target, arguments, and side effects are clear.

For install, auth, examples, and longer product guidance, read `README.md` and `SKILL.md`. This file intentionally stays small so repo-local agents get invariant local guidance without duplicating the generated docs.

## Local Customizations

If you modify this CLI beyond what the generator produced, record each customization so it isn't lost on the next regen and is visible to the next reader.

1. **Mark every changed site** in source with a comment summarizing the deviation:

    ```
    // PATCH: <one-line summary>
    ```

    Include an upstream reference inline when there is one (e.g. `// PATCH(upstream cli-printing-press#<issue>): ...`). `grep -rn 'PATCH' .` from this directory then surfaces every customization.

2. **Catalog the change** in a `.printing-press-patches.json` at this CLI's root (parallel to `.printing-press.json`). Minimum shape:

    ```json
    {
      "schema_version": 1,
      "applied_at": "YYYY-MM-DD",
      "base_run_id": "<copy from .printing-press.json>",
      "base_printing_press_version": "<copy from .printing-press.json>",
      "patches": [
        {
          "id": "short-identifier",
          "summary": "What changed (one sentence).",
          "reason": "Why this customization was needed (one or two sentences).",
          "files": ["internal/cli/foo.go"],
          "validated_outcome": "Optional: non-obvious test result that confirms the fix.",
          "upstream_issue": "Optional: https://github.com/mvanhorn/cli-printing-press/issues/<n>"
        }
      ]
    }
    ```

This file is an **index of customizations**, not a second copy of the diff. Diffs live in `git`; code lives in the source files; the inline `// PATCH:` comment carries the local semantics. Keep `summary` and `reason` short -- if you find yourself writing tables of field renames or code transformations, that detail belongs in the source comment or commit message, not here.
