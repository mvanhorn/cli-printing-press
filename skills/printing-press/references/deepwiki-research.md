# DeepWiki Codebase Analysis

> **When to read:** This file is referenced by Phase 1.5a.6 of the printing-press skill.
> Read it when GitHub repos have been discovered during Phase 1/1.5a research.

## What DeepWiki Provides

DeepWiki generates AI-analyzed documentation for any public GitHub repo. It explains
how a codebase works - architecture, data models, auth flows, error handling - not
just what endpoints exist. This complements crowd-sniff (which finds endpoints) and
MCP source reading (which finds auth headers) with semantic understanding.

URL pattern: `https://deepwiki.com/{owner}/{repo}`

## When to Query

Query DeepWiki when Phase 1 or Phase 1.5a discovers a GitHub repo URL for:
- The API's official SDK repo (e.g., `stripe/stripe-node`, `triggerdotdev/trigger.dev`)
- The API's server repo (if open source)
- A popular community wrapper or MCP server repo

**Repo URL sources:** npm package metadata (`repository` field), GitHub search results,
MCP server discovery (Step 1.5a searches), WebSearch results linking to GitHub repos.

**Skip when:**
- No GitHub repos were discovered
- The repo is private (DeepWiki only indexes public repos)
- The API is trivially simple (1-2 endpoints, no auth)

## How to Query

**Time budget:** 2 minutes max. If DeepWiki is slow or returns errors, skip silently.

### Step 1: Fetch the wiki structure

```
WebFetch: https://deepwiki.com/{owner}/{repo}
Prompt: "List all wiki section titles and their numeric path identifiers"
```

This returns the wiki's table of contents with section IDs like `1-overview`,
`3.1-task-definition-api`, `5.8-authentication-and-authorization`.

### Step 2: Fetch targeted sections

From the wiki structure, identify and fetch **up to 3** of these high-value sections
(in priority order):

1. **Authentication/Authorization** - look for sections with "auth", "authentication",
   "authorization", "security", "api key", "token" in the title. This reveals token
   formats, auth flows, required scopes, and session handling.

2. **Data Model/Schema** - look for sections with "model", "schema", "database",
   "data", "entity" in the title. This reveals entity relationships, primary keys,
   and data flow patterns.

3. **Architecture/Overview** - look for sections with "architecture", "overview",
   "system", "design" in the title. This reveals how components interact, what the
   main abstractions are, and how the API is structured internally.

4. **API/SDK surface** - look for sections with "api", "sdk", "client", "endpoint",
   "rest" in the title. This reveals the intended API surface from the maintainer's
   perspective.

For each section, use WebFetch with a targeted extraction prompt:

```
WebFetch: https://deepwiki.com/{owner}/{repo}/{section-path}
Prompt: "Extract: (1) authentication method and token format, (2) primary data
entities and their relationships, (3) rate limiting or throttling behavior,
(4) error handling patterns, (5) key architectural decisions. Be specific -
include field names, header names, status codes, and entity names."
```

### Step 3: Synthesize findings

Organize extracted intelligence into these categories:

- **Auth flow**: Token type (Bearer, API key, session cookie, OAuth), header name,
  env var convention, required scopes or permissions
- **Data model**: Primary entities, their relationships (1:many, many:many),
  key fields, pagination approach
- **Rate limiting**: Limits per endpoint or global, retry-after behavior,
  backoff strategy
- **Error patterns**: Error response format, common error codes, retry guidance
- **Architecture insights**: Key abstractions, service boundaries, queue/worker
  patterns, event systems

## How to Use Findings

### In the Research Brief (Phase 1)

Add findings to the `## Codebase Intelligence` section (between Data Layer and
User Vision). Only populate when DeepWiki returned useful findings:

```markdown
## Codebase Intelligence
- Source: DeepWiki analysis of {owner}/{repo}
- Auth: [token type, header, env var pattern]
- Data model: [primary entities and relationships]
- Rate limiting: [limits and behavior]
- Architecture: [key insight about how the API works internally]
```

### In the Absorb Manifest (Phase 1.5b)

When attributing features in the absorb manifest's "Best Source" column, use
`DeepWiki ({owner}/{repo})` to distinguish DeepWiki-sourced intelligence from
other sources.

### In Novel Feature Scoring (Phase 1.5c.5)

DeepWiki architectural analysis counts as 1 evidence source for the "Research
Backing" scoring dimension (0-2 points). Combined with another source (e.g.,
community issues, competitor tool analysis), it can push a feature to the
maximum 2/2 Research Backing score.

### Pre-Sniff Auth Intelligence (Phase 1.6)

DeepWiki auth findings directly feed into Phase 1.6's auth profile classification.
If DeepWiki reveals the token format (e.g., `tr_dev_` prefix for Trigger.dev,
`sk_live_` for Stripe), credential setup steps, or required scopes, use these
to ask the user more specific auth questions.

## Failure Handling

- **DeepWiki returns 404 or error:** Skip silently. Log: "DeepWiki: no wiki
  available for {owner}/{repo}"
- **WebFetch times out:** Skip silently. Log: "DeepWiki: timeout fetching
  {owner}/{repo}"
- **Wiki exists but targeted sections not found:** Extract what is available.
  Even the overview section provides useful architectural context.
- **Wiki content is thin or unhelpful:** Use only findings that are specific
  and actionable. Do not pad the brief with generic DeepWiki output.
