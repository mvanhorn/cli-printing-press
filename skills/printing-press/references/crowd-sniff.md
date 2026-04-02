# Crowd Sniff Implementation

> **When to read:** This file is referenced by Phase 1.8 of the printing-press skill.
> Read it when the user approves crowd sniff (mining npm SDKs and GitHub code search).

### If user approves crowd sniff

Ensure the discovery directory exists:

```bash
mkdir -p "$DISCOVERY_DIR"
```

Run the crowd-sniff command and capture both the spec and JSON provenance:

```bash
printing-press crowd-sniff --api <api> --output "$RESEARCH_DIR/<api>-crowd-spec.yaml" --json > "$DISCOVERY_DIR/crowd-sniff-provenance.json"
```

If the API has a known base URL from Phase 1 research, pass it:

```bash
printing-press crowd-sniff --api <api> --base-url <known-base-url> --output "$RESEARCH_DIR/<api>-crowd-spec.yaml" --json > "$DISCOVERY_DIR/crowd-sniff-provenance.json"
```

Report the results: "Crowd sniff discovered **N endpoints** across **M resources** (X from npm, Y from GitHub)."

**Feed into Phase 2:**
- **Enrichment mode**: Phase 2 will use `--spec <original> --spec <crowd-spec> --name <api>` to merge both
- **Primary mode**: Phase 2 will use `--spec <crowd-spec>` directly

#### Write crowd-sniff discovery report

Write a structured crowd-sniff provenance report to `$DISCOVERY_DIR/crowd-sniff-report.md`. This report preserves the discovery evidence so a future maintainer can see what community sources informed the spec.

The report must contain these sections:

1. **npm Packages Analyzed** — List each SDK package examined: name, version, download count, recency. Note which packages yielded endpoints and which were empty/irrelevant.

2. **GitHub Repos Searched** — The search queries used, repos matched, and freshness of each repo. Note the GitHub token status (authenticated with broader results, or unauthenticated with rate-limited results).

3. **Endpoints Discovered** — A markdown table with columns: Method, Path, Source Tier (official-sdk / community-sdk / code-search), Source Count (seen in N independent sources). Sorted by source tier then frequency.

4. **Base URL Resolution** — Candidates discovered and which was selected, with rationale (e.g., "Found in 3 npm packages: https://api.notion.com").

5. **Auth Patterns Detected** — Authentication patterns found in SDK code (API key headers, bearer tokens, OAuth flows). Include the header name or env variable convention when visible.

6. **Coverage Summary** — Total endpoints found, breakdown by source tier, and any gaps compared to the Phase 1 research brief (e.g., "Brief mentions webhooks but no webhook endpoints found in community code").
