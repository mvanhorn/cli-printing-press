---
title: "Avoid classification when the failure mode is asymmetric: prefer prose over metadata for credentials and similar high-stakes routing decisions"
date: 2026-05-06
category: design-patterns
module: cli-printing-press-generator
problem_type: design_pattern
component: authentication
severity: high
applies_when:
  - "Designing a metadata field that an external tool (agent host, package manager, install loader) reads to decide what to ask the user for"
  - "The data being classified contains both 'user must supply' and 'system fills in automatically' values, and the spec authoring data alone doesn't reliably distinguish them"
  - "Mis-classification has asymmetric cost: a false positive (telling the user to enter a value the system overwrites) burns user trust; a false negative (omitting a real prompt) just produces an informative runtime error the user can recover from"
  - "The same information could instead be surfaced in human-readable prose (README, setup instructions, agent-readable docstrings) where the agent / user judges in context rather than mechanically"
tags:
  - classification
  - asymmetric-failure
  - env-vars
  - hermes
  - openclaw
  - frontmatter-design
related_components:
  - authentication
  - templates
  - generator
---

# Avoid classification when the failure mode is asymmetric: prefer prose over metadata for credentials and similar high-stakes routing decisions

## Context

Agent hosts (Hermes, OpenClaw, future hosts) read a SKILL.md frontmatter field that lists "env vars the user must set" and prompt the user for each at install time. The natural impulse when adding support for a new host is to populate the field from the existing auth metadata: walk the spec's `auth.EnvVarSpecs` (which carry `Kind` ∈ `{per_call, auth_flow_input, harvested}` and `Required` flags), filter to "user-set required," emit the survivors.

Two rounds of plan review surfaced that this is a trap. The asymmetry of the failure mode makes it not worth shipping:

- **False positive** — emit `DOMINOS_TOKEN` (a session token harvested from a Chrome login flow) as `required_environment_variables`. The user sees the install-time prompt, types in a value, the agent host writes it to its config, the CLI's `auth login` flow later overwrites it with the real harvested cookie. The user wonders why their typed value doesn't work, can't possibly produce a value the harvested-flow would accept, and concludes the install is broken. **Trust burned, recovery is non-obvious.**
- **False negative** — omit `SHOPIFY_ACCESS_TOKEN` from the metadata. The user installs, runs the first command, gets a clear `error: SHOPIFY_ACCESS_TOKEN is required` message with the canonical name. They set the env var and re-run. **Recovery is obvious and idempotent.**

The data needed to classify reliably (PerCall vs AuthFlowInput vs Harvested + Required + Sensitive) was added to the spec model in a prior iteration. But the data isn't *enough* — many legacy specs only have a flat `auth.env_vars: [...]` list with no kind annotation, and even on rich specs the `Sensitive` flag has two semantically distinct meanings (redact-in-logs vs don't-publish-in-public-metadata) that collide when you try to use it as a public-emission gate.

After two rewrites trying to make the classifier safe, we shipped v1 with **no env-var declarations in either Hermes or OpenClaw frontmatter**. The same information lives in the README's `## Use with Claude Code` and similar sections, branched by `auth.Type`, in human-readable prose. Agent hosts that drive credential setup do so via the README, not via a structured field.

## Guidance

When you're about to design a metadata field for an external consumer to act on programmatically, ask three questions:

1. **What's the failure mode if the data is wrong?** If false positives and false negatives have asymmetric cost, the symmetric "best-effort classifier" approach loses most of its value — your filter has to be tuned to the dominant cost, and the other failure mode (now untreated) leaks through.
2. **Can the spec authoring data reliably distinguish the cases?** If your classifier requires fields that aren't always populated (or that have ambiguous semantics across legacy specs), the classifier silently produces wrong output for the unannotated half of the corpus. Annotation backfill is its own multi-step project.
3. **Is there a prose surface that already handles this correctly?** Often the README, the SKILL body, or the help text already branches on the same conditions you're trying to encode in metadata, but in human-readable form. Routing the consumer to the prose is more honest than mechanizing a partial classifier.

If the answers are "asymmetric," "no," and "yes," skip the structured field. Document the credentials in prose. Re-add structured emission later if and when (a) real consumers signal demand and (b) the classification problem has been independently solved.

```yaml
# What v1 ships (after stripping the env-var hoist):
metadata:
  openclaw:
    requires:
      bins:
        - mercury-pp-cli
    install:
      - kind: go
        bins: [mercury-pp-cli]
        module: github.com/mvanhorn/printing-press-library/library/payments/mercury/cmd/mercury-pp-cli
  # No requires.env, no envVars, no required_environment_variables
```

The README carries the credential UX, branched by `auth.Type`:

```markdown
{{- if eq .Auth.Type "api_key"}}
Set the API key:
```bash
export {{$canonicalEnvVar.Name}}="<your-key>"
```
{{- else if or (eq .Auth.Type "cookie") (eq .Auth.Type "composed")}}
This CLI uses a browser session. Log in to {{.Auth.CookieDomain}} in Chrome, then:
```bash
{{.Name}}-pp-cli auth login --chrome
```
{{- else if eq .Auth.Type "oauth2"}}
Authenticate via the browser:
```bash
{{.Name}}-pp-cli auth login
```
{{- end}}
```

The `auth.Type` branching IS the conservative classification — applied where the failure mode is benign (a user reading the README) instead of where it's hostile (an install loader prompting for a value the user can't supply).

## Why This Matters

The asymmetric-failure-mode framing is the load-bearing concept. Both round-1 and round-2 plan reviews kept producing new edge cases at the boundary of the classifier:

- The `Sensitive` flag's redact-in-logs vs don't-publish-in-public-metadata semantics collided. The intuitive guard `!Sensitive` excluded legitimate user-set API keys (which spec authors correctly mark `Sensitive: true` so they're redacted in `--debug` output). Removing the guard let OAuth `CLIENT_SECRET` leak.
- The `Kind`-based filter (`per_call` only) excluded `auth_flow_input` env vars that users genuinely supply once during initial setup (OAuth `CLIENT_ID`).
- Legacy specs without `EnvVarSpecs` populated would have to be classified by `auth.type` heuristic alone, with no signal for whether a flat `env_vars` list is "user-set keys" vs "harvested cookie names."
- Even with a perfect classifier, the consumer (Hermes / OpenClaw) might interpret the field differently than we expect. We had no real-loader test until very late.

Each fix introduced a new edge case. The fundamental issue: classification has to be *more* reliable than the failure-cost ratio justifies. With ~1000:1 cost asymmetry, even a 99%-accurate classifier ships an unacceptable false-positive rate.

The prose-instead-of-metadata answer trades a worse install UX (user reads README, sets env vars manually, runs first command, possibly hits a friendly missing-creds error) for a much smaller failure surface. That trade is correct when the structured-emission risk is "ship 49 CLIs to a public registry with one wrong env var prompt" and the prose risk is "user reads slightly more text."

## When to Apply

- Designing public-facing metadata that drives external-tool behavior (agent host install prompts, package manager dependency declarations, IDE auto-config)
- The data the metadata is computed from has ambiguity at boundaries (legacy data, multi-mode fields, semantic overlaps between flags)
- A wrong emission is hard to retract once distributed (published to a registry, baked into install scripts, cached by intermediaries)
- The same information has a well-trodden human-readable form somewhere in your existing artifacts

Don't apply this pattern when:

- The classifier is reliable enough — every case has unambiguous source data, the failure mode is symmetric, the consumer has a clear contract for what to do with each value
- The prose alternative doesn't exist or would be inconsistent with the metadata (in which case fix the prose first, then optionally add metadata)
- The cost of mechanizing the data is justified by automation savings (e.g., CI dependency declarations where humans don't read the field)

## Examples

### Bad-instinct progression (why this took two plan rewrites to land)

**Round 1 plan**: emit `required_environment_variables` filtered by `Kind+Required`, reuse the existing `IsRequestCredential()` predicate.

  - Found in review: `IsRequestCredential()` returns true only for `PerCall`. Excludes `AuthFlowInput` env vars users genuinely set once.

**Round 2 plan**: introduce new `IsHermesRequiredEnv()` predicate: `(PerCall || AuthFlowInput) && Required && !Sensitive`.

  - Found in review: the `!Sensitive` guard collides with the legitimate use of `Sensitive: true` on user-set API keys (for log redaction). Removing the guard reintroduces the `CLIENT_SECRET` leak. There's no consistent semantics for `Sensitive` that satisfies both the redaction and the publish-to-public-metadata use cases.

**Final plan**: don't emit `required_environment_variables` at all. Strip the equivalent OpenClaw fields too for symmetry. Lean on the existing `auth.Type`-branched README content for credential UX.

The lesson isn't "we picked the wrong predicate twice" — it's "the data structure isn't fit for the public-metadata use case, and forcing fit produces increasingly subtle bugs." Recognizing that earlier saves multiple plan-review rounds.

### Healthy alternative — prose carries the same routing logic

```markdown
# README.md

## Use with Claude Code

Install the focused skill:
```bash
npx skills add mvanhorn/printing-press-library/cli-skills/pp-shopify -g
```

Set up authentication (api_key auth):
```bash
export SHOPIFY_ACCESS_TOKEN="<your-token>"
```
```

vs.

```markdown
## Use with Claude Code (cookie auth example — pp-allrecipes)

Install the focused skill:
```bash
npx skills add mvanhorn/printing-press-library/cli-skills/pp-allrecipes -g
```

This CLI uses a browser session. Log in to allrecipes.com in Chrome, then:
```bash
allrecipes-pp-cli auth login --chrome
```
```

The same conditional logic that would have driven a metadata classifier (api_key → "set env var", cookie → "browser session + auth login --chrome") lives in the template's auth.Type branching. The user reads it, the agent reads it, neither mis-prompts for a value that can't be supplied.

## When to Re-evaluate

Re-add structured env-var emission when at least two of these are true:

- Real users (not just plan reviewers) report that the install-time prompt UX is missing
- Spec authoring annotates `auth.env_var_specs` reliably across the corpus (no legacy-flat-list cases left)
- The consumer agent host has a documented contract for the field (not just "Hermes docs say X" — verified against the actual loader's behavior)
- A reversible publish path exists: if the classifier is wrong, you can update or unpublish without 49 manual PRs

Until then, prose UX is the honest answer.

## Related

- `docs/solutions/design-patterns/auth-envvar-rich-model-2026-05-05.md` — the data model that motivated the classifier attempts; this learning is about *not* using that model for public emission, not about the model itself
- `internal/generator/templates/skill.md.tmpl` — frontmatter shape post-strip (no `requires.env`, no `envVars`, no `primaryEnv`)
- `internal/generator/templates/readme.md.tmpl` — the prose alternative, branched by `auth.Type`
- `docs/plans/2026-05-06-002-feat-hermes-openclaw-frontmatter-alignment-plan.md` — full design rationale + cross-references to the round-1 / round-2 review findings that surfaced the asymmetric-failure problem
