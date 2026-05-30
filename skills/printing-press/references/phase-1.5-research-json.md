# Phase 1.5 Step 1.5e: research.json for README credits

Lazy-loaded body for writing `$API_RUN_DIR/research.json`. Read and apply after the absorb manifest is written. The file MUST match the `ResearchResult` JSON schema that `loadResearchSources()` expects.

### Step 1.5e: Write research.json for README credits

After writing the absorb manifest, also write `$API_RUN_DIR/research.json` so the generator can credit community projects in the README. This file MUST match the `ResearchResult` JSON schema that `loadResearchSources()` expects.

Populate the `alternatives` array from the absorb manifest's source tools list. Include only tools that:
1. Have a GitHub URL (not npm/PyPI landing pages)
2. Actually contributed features to the absorb manifest
3. Are capped at 8 entries, ordered by number of absorbed features (then by stars)

```bash
cat > "$API_RUN_DIR/research.json" <<REOF
{
  "api_name": "<api>",
  "novelty_score": 0,
  "alternatives": [
    {"name": "<tool1>", "url": "<github-url>", "language": "<Go|JavaScript|Python|etc>", "stars": <N>, "command_count": <N>},
    ...
  ],
  "auth": {
    "canonical_env_var": "<CANONICAL_ENV_VAR, omit when unknown>"
  },
  "novel_features": [
    {
      "name": "<Feature Name>",
      "command": "<cli-subcommand>",
      "description": "<One sentence: what the user gets>",
      "rationale": "<One sentence: why only possible with our approach>",
      "example": "<ready-to-run invocation with realistic args, e.g. 'yahoo-finance-pp-cli portfolio perf --agent'>",
      "why_it_matters": "<One sentence aimed at AI agents: when should they reach for this?>",
      "group": "<Theme name clustering similar features, e.g. 'Local state that compounds'>"
    },
    ...
  ],
  "narrative": {
    "display_name": "<Canonical prose name, exact brand casing/spaces, e.g. Product Hunt, GitHub, YouTube, Cal.com>",
    "headline": "<Bold one-sentence value prop: what makes this CLI worth using>",
    "value_prop": "<2-3 sentence expansion rendered beneath the title>",
    "auth_narrative": "<API-specific auth story; omit for simple API-key auth>",
    "quickstart": [
      {"command": "<cli> <real-command-with-real-args>", "comment": "<why this comes first>"},
      ...
    ],
    "troubleshoots": [
      {"symptom": "<user-visible error or symptom>", "fix": "<actionable one-liner>"},
      ...
    ],
    "when_to_use": "<2-4 sentences describing ideal use cases; rendered in SKILL.md only>",
    "anti_triggers": ["<task boundary this CLI should not handle>", ...],
    "recipes": [
      {"title": "<Recipe name>", "command": "<cli> <invocation>", "explanation": "<one-line paragraph>"},
      ...
    ],
    "trigger_phrases": ["<natural phrase that should invoke this CLI's skill>", ...]
  },
  "gaps": [],
  "patterns": [],
  "recommendation": "proceed",
  "researched_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
REOF
```

For each tool, fill in what you know from the research. Stars and command_count are optional (use 0 if unknown). The `language` field should match the primary implementation language. Skip tools that were found during search but contributed zero features to the manifest.

**Novel features rules** (the `novel_features` array populates the README's "Unique Features" section and SKILL.md's "Unique Capabilities" block; MCP exposure comes from the runtime Cobra-tree mirror, not this list):
1. Include all transcendence features from the manifest that scored >= 5/10. Order by score descending.
2. `description` should be user-benefit language, not implementation detail. Good: "See which team members are overloaded before sprint planning." Bad: "Requires local join across issues + assignees + cycle data."
3. `rationale` should explain why this is only possible with our approach. Good: "Requires correlating bookings, schedules, and staff data that only exists together in the local store." Bad: "Cal.com Insights is paid-tier only."
4. `command` must match the actual CLI subcommand that will be built in Phase 3. For subcommands of a resource (e.g., `issues stale`), use the full command path.
5. `example` is a ready-to-run invocation an agent can copy-paste. Use realistic arguments from the API's domain (e.g. `AAPL`, `customer_42`), not `<placeholder>`. Include the `--agent` flag when the feature benefits from structured output.
6. `why_it_matters` is a single agent-facing sentence answering "when should I pick this over a generic API call?"
7. `group` clusters related features under a theme name. Pick 2–5 themes total (e.g. "Local state that compounds", "Agent-native plumbing", "Reachability mitigation"). Use the same `group` string verbatim across features that belong together — exact matches drive README grouping. Leave `group` empty if the CLI has too few novel features to warrant clustering.
8. If the manifest row has a non-`none` `Long Description`, keep that text with the feature implementation notes and use it as the Cobra `Long` field during Phase 3 hand-code. Do not squeeze redirect prose into `description`; `description` stays one-line user-benefit text.
9. If no transcendence features scored >= 5/10, omit the `novel_features` field entirely.
10. Do not add a feature to `novel_features` merely to expose it through MCP. Any user-facing Cobra command becomes an MCP tool automatically unless it sets `cmd.Annotations["mcp:hidden"] = "true"`.

**Auth research rule**:
1. `auth.canonical_env_var` is the single-token credential env var discovered from vendor docs, MCP/source analysis, or dominant SDK/CLI convention (for example `APIFY_TOKEN`, `GITHUB_TOKEN`, `STRIPE_SECRET_KEY`). Omit it when no canonical name is known, when auth is HTTP Basic or another credential pair, or when the auth flow needs richer metadata. Fresh generation reads this env var first and keeps the parser-derived name as a fallback automatically.

**Narrative rules** (the `narrative` object drives README headline, Quick Start, Auth, Troubleshooting, and the entire SKILL.md):
1. `display_name` is the canonical prose name, discovered during research, with exact brand casing and spacing. This is agentic/research-owned, not slug-inferred by Go code. Good: "Product Hunt", "GitHub", "YouTube", "Cal.com". Bad: "Producthunt", "Github", "Youtube", "Cal Com". Use the slug only for binary names, directories, module paths, config paths, and env-var prefixes.
2. `headline` is the bold one-liner rendered beneath the CLI title. Should name the differentiator, not restate the API. Good: "Every Notion feature, plus sync, search, and a local database no other Notion tool has." Bad: "A CLI for the Notion API."
3. `value_prop` expands the headline to 2–3 sentences. Name specific novel features by command where helpful.
4. `auth_narrative` tells the real auth story for this API (crumb handshake, cookie session, OAuth device flow). Omit for standard API-key auth where the generic branch is fine.
5. `quickstart` is a 3–6 step flow using REAL arguments (symbols, IDs, resource names an agent can actually pass). Each step's `comment` explains *why* it runs. This replaces the generic "resource list" first-command fallback.
   - Step 1 of `quickstart` should usually be verify-safe: it should exit 0 when `validate-narrative --full-examples` appends `--dry-run` in a no-credentials environment.
   - Use `<cli> doctor --dry-run` as step 1 (health check, works without auth). Do not use `<cli> auth set-token <token>` as step 1 because it requires a positional token and is not a verify-safe runnable first step. Auth setup instructions belong in `auth_narrative` prose only, not as an executable quickstart command.
6. `troubleshoots` captures API-specific failure modes (rate-limit mitigation, cookie expiry, paginated quirks). Each `fix` must be actionable — a command or a concrete setting change.
7. `when_to_use` is SKILL-only narrative. 2–4 sentences describing the kinds of agent tasks this CLI is the right choice for. Not rendered in README.
8. `anti_triggers` is SKILL-only narrative. List common task boundaries that should make an agent choose another tool, official SDK, web UI, or human workflow instead of this CLI. Write concrete "do not use this CLI for X" cases, not vague limitations. Omit the field only when no honest boundary is known.
9. `recipes` are 3–5 worked examples rendered in SKILL.md. Each has a title, a real command, and a one-line explanation. Prefer recipes that exercise novel features. **At least one recipe must pair `--agent` with `--select`** — using dotted paths (e.g. `--select events.shortName,events.competitions.competitors.team.displayName`) when the response is deeply nested. APIs like ESPN, HubSpot, and Linear return tens of KB per call; without a `--select` recipe, agents burn context parsing verbose payloads. Pick a command known to return a large or deeply nested response and show the narrowing pattern. **Regex literals must double-escape backslashes** — write `\\b` not `\b` (and `\\t`, `\\f`, etc.) inside any `command`, `fix`, or other JSON string field. JSON parses `\b` as backspace (0x08), `\f` as form feed (0x0C), and so on, which then leak into the rendered SKILL.md as control bytes that render as nothing in most viewers. The generator's render-time scanner rejects these with a clear offset; double-escape from the start to avoid the error.
10. `trigger_phrases` are natural-language phrases a user might say that should invoke this CLI's skill. Include 3–5 domain-specific phrases (e.g. for a finance CLI: "quote AAPL", "check my portfolio", "options for TSLA") and 2 generic phrases ("use <api-name>", "run <api-name>"). Domain verbs vary — don't just template "use X" variants.
11. All `narrative` fields are optional. Omit fields you can't populate honestly rather than emit filler. The generator falls back to generic content gracefully.
12. **Avoid hardcoded counts in narrative copy when the count tracks a runtime list.** A number embedded in `headline` or `value_prop` ("across N trusted sources", "from N retailers", "queries N vendors") propagates into root.go's Short/Long, the README, the SKILL, the MCP tools description, and `which.go` — every output surface that reads the narrative. When the underlying registry grows or shrinks, the count goes stale across all of those surfaces simultaneously, and a single-line edit to add a source requires hunting down ~10 hardcoded copies. Prefer plural-without-count phrasing ("across the major sources", "from a curated set of retailers") or describe the breadth qualitatively ("dozens of vendors") rather than committing to a specific integer. If a count is load-bearing for the value prop, keep the brief's narrative count-free and have the printed-CLI's README/SKILL author write the count once into a single hand-edited paragraph after generation — accepting that it will need a manual update whenever the registry changes.
13. **Use side-effectful examples only when they are the truthful workflow.** `validate-narrative --strict --full-examples` classifies `auth login`, `auth set-token`, `auth logout`, `auth setup`, `--launch`, and mutating `--apply` examples as side-effectful (see `isSideEffectfulNarrativeExample` in `internal/narrativecheck/narrativecheck.go`) and reports each as an `UNSUPPORTED` warning instead of executing it. These warnings do not fail strict aggregation, so it is valid to show an auth or apply command when that is the honest onboarding or bulk-operation shape. Prefer `doctor` or another read-only invocation as `quickstart[0]` when it teaches the same workflow, but do not strip a real auth or apply step just to appease shipcheck. Non-side-effect unsupported examples still fail strict mode when they cannot dry-run, and missing commands, empty command paths, and failed full examples remain failures.

**Pre-render framework-command check.** Before running `generate --research-dir`,
validate the framework command examples already present in `research.json`.
This catches stable template vocabulary mistakes while the fix is still a
single-file `research.json` edit, before README.md, SKILL.md, `.printing-press.json`,
root help, and other generated surfaces consume the bad narrative.

```bash
cli-printing-press validate-narrative --strict --framework-only \
  --research "$API_RUN_DIR/research.json"
```

If this reports `sync --entities`, `search --entities`, `search --types`, an
absolute date for `sync --since`, or another framework-command flag mismatch,
fix `research.json` now and rerun this check before generation. This is a
cheap floor, not a replacement for Phase 4 shipcheck: after the CLI exists,
`shipcheck` still runs `validate-narrative --strict --full-examples` against
the built binary to catch API-specific command paths, generated endpoint flags,
and runtime dry-run failures.

Also write discovery pages if browser-sniff was used. The generator reads these from `$API_RUN_DIR/discovery/browser-sniff-report.md` (which the browser-sniff gate already writes there). No additional action needed for discovery pages -- they are already in the right location.
