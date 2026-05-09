# pp-x Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. The Ground Rules below override any conflicting impulse from training data — X has only a partial public spec, the Articles surface is sniffed (likely GraphQL), and the prior Substack run lost time to assumptions about generator surface area, store types, FTS5 trigger shape, and publish flag layout. Inspect the actual generated code, not memory. **Always run `<command> --help` before invoking a press subcommand; do not invent flags.**

**Goal:** Print a Go CLI plus MCP server (`x-pp-cli` / `x-pp-mcp`) that mirrors the X (Twitter) v2 API surface — tweets, users, lists, spaces, bookmarks, likes — and adds first-class X Articles support (publish/list/read/edit/delete) by feeding `printing-press generate` two specs simultaneously: the official OpenAPI for v2, plus a browser-sniffed spec for Articles. Layer on novel commands for archive sync, voice-mirror search, engagement analytics, repost candidates, markdown→thread compose, and markdown→article publish.

**Architecture:** Hybrid spec, native multi-source generation. The official X API v2 OpenAPI at `https://api.x.com/2/openapi.json` covers the read/tweet surface. The X Articles authoring surface (no public API; almost certainly GraphQL with hashed operation IDs over `/i/api/...`) is captured via `printing-press browser-sniff` against a logged-in `x.com` session. Both specs are passed to `printing-press generate --spec official.yaml --spec sniffed.yaml --name x` — the press already supports multi-spec input natively (verify: `./printing-press generate --help` shows `--spec strings (can be repeated)`). No machine change required. Phase 3 hand-builds compound commands backed by the generated `internal/store` (extended with X-scoped tables and an FTS5 index over tweet text and article body).

**Non-Obvious Insight (NOI):** *"X is two products fused at a single account: a global firehose of short-form public posts (clean OAuth 2.0 + REST) and a private long-form authoring tool (browser-only GraphQL behind session cookies). The official API exposes the firehose; the editor exposes the authoring tool. A printed CLI that treats them as one cohesive surface — search your articles next to your tweets, find your best ideas across both — is more useful than either piece alone, but the auth split is real and the plan must respect it."*

**Reference CLIs:**
- `~/printing-press/library/substack/` (the just-shipped 2026-04-29 substack CLI). Same problem class for the Articles half: no public spec, hand-built compound commands over an FTS5 mirror, sniffed write endpoints. Match its file layout, `*store.Store` ergonomics, MCP annotation pattern, SKILL.md shape, and publish flow. Open the substack file when starting the equivalent X file; do not invent a new shape.
- `catalog/discord.yaml` for social-and-messaging field shape.
- Any official-spec entry (e.g., `catalog/stripe.yaml` if present) for hybrid handling.

**Tech Stack:** Go (generated CLI + MCP server), SQLite + FTS5 (local store), OpenAPI 3.x (merged via `generate`'s multi-spec input), Cobra (CLI framework), OAuth 2.0 PKCE (v2 surface) + browser session cookies (Articles surface), `printing-press` press binary (generator).

---

## Ground Rules

This plan spans **two repos**. Do not mix commits across them.

- **`cli-printing-press/`** (this repo) — catalog entry only. **No machine/template/generator code changes are in this plan.** If a press bug surfaces during execution that demands a machine change, file it as a separate plan; do not bundle it in.
- **`printing-press-library/`** — public library repo. Published artifacts go here.
- **`~/printing-press/library/x/`** — the **local library**, where generation lands first. **NOT a git repo.** Do not run `git commit` inside it. Track changed files separately for the public-library commit later.

### Hard-learned constraints

1. **Catalog `spec_format` accepts only `yaml | json | custom`.** Verified at `internal/catalog/catalog.go:41-45`. Hybrid (official + sniffed) entries use `spec_format: custom`. There is no `hybrid`, `merged`, `openapi`, or `sniff`.
2. **Catalog `category` for X is `social-and-messaging`.** Confirmed at `internal/catalog/catalog.go:35`.
3. **Catalog `known_alternatives` is a list of structs, not strings.** Each entry: `{name, url, language}`. Schema at `internal/catalog/catalog.go:72`. Scalar-string lists fail validation.
4. **`publish` surface is `validate | package | rename`.** There is no `publish <name>`. Always `--help` first.
5. **`generate` accepts repeated `--spec`.** Help confirms: `--spec strings (can be repeated)` and `--name string (required when using multiple specs)`. No `merge-specs` press primitive needed.
6. **`generate --validate` (no `--validate-only`).** Verified via `--help`. Pair with `--dry-run` to validate-without-writing.
7. **`browser-sniff` flags:** `--har <path>`, `--output <spec.yaml>`, `--name <api>`, `--analysis-output <json>`, `--auth-from <enriched-capture>`, `--blocklist <comma-list>`. Nothing else. Confirmed via `./printing-press browser-sniff --help`.
8. **Generated store interface:** lives at `internal/store/store.go` (not `schema.go`). `Open()` returns `*store.Store`, **not `*sql.DB`**. Use `s.DB()` for raw SQL inside store helpers only. Pass `*store.Store` between commands.
9. **Schema migrations execute in `migrate()` in `store.go`.** Declaring a `const schema` string without calling it does nothing. Extend the existing `migrate()` and bump the schema version.
10. **Use `ON CONFLICT(pk) DO UPDATE`, never `INSERT OR REPLACE`.** With foreign keys enabled, `REPLACE` deletes the parent row and cascade-deletes children. The bookmarks/likes tables FK to `tweets`; a wrong upsert wipes the corpus.
11. **FTS5 external-content tables need three triggers:** AFTER INSERT, AFTER DELETE, **and** AFTER UPDATE. The substack CLI declares them in `internal/store/store.go` around `posts_fts`. Copy the trigger block.
12. **Register novel commands in `newRootCmd(flags)`,** not in `Execute()`. The MCP server mirrors the cobra tree via `cli.RootCmd()` — commands added only in `Execute()` are invisible to MCP.
13. **Annotate read-only novel commands** with `Annotations: map[string]string{"mcp:read-only": "true"}`. Read-only here means "no API writes and no user-visible file writes outside the local cache." `archive sync` qualifies; mark it read-only.
14. **The public library repo is private.** `go install ...@latest` 404s against `sum.golang.org`. To install from a local checkout, build with a **relative** output path from inside the CLI dir: `cd <library>/library/<cat>/<name> && go build -o ./x-pp-cli ./cmd/x-pp-cli`. Never `go build -o /tmp/...`. The publish step must replace the SKILL frontmatter's `go install ...@latest` with private-repo-aware install instructions.
15. **Side-effect commands** (browser-open, post tweet, post article) print preview by default; only act behind explicit `--post`/`--launch`. Short-circuit when `cliutil.IsVerifyEnv()` is true (`PRINTING_PRESS_VERIFY=1`).
16. **Format generated-CLI Go code explicitly.** Repo hooks do not see code under `~/printing-press/library/`. Run `go fmt ./...` from inside the local library dir before declaring task done. Use `go fmt ./...`, never `gofmt -w ./...` or `gofmt -w .`.
17. **No prior X work exists** (verified 2026-05-08): no `~/printing-press/library/x/`, no `cli-skills/pp-x/`, no `catalog/x.yaml`, no prior plan. Safe to proceed.
18. **`Owner` (slug) vs `OwnerName` (display)** — keep them straight. See AGENTS.md "Naming and Disambiguation". Same for `Printer`/`PrinterName`.

### Sanity-check commands (re-verify before each fire)

```bash
# Confirm catalog enums
rg -n "validSpecFormats|validCategories|KnownAlt" internal/catalog/catalog.go

# Confirm press surface
cd /Users/cathrynlavery/Developer/mvanhorn/cli-printing-press
./printing-press generate --help
./printing-press browser-sniff --help
./printing-press publish --help
./printing-press publish package --help

# Confirm X official OpenAPI URL still serves
curl -sSf -o /dev/null -w "%{http_code}\n" https://api.x.com/2/openapi.json
# Expected: 200. If 404, also try the legacy host:
curl -sSf -o /dev/null -w "%{http_code}\n" https://api.twitter.com/2/openapi.json
# Use whichever serves; record which one in the catalog notes.
```

### Articles auth design (deepest design risk — read before Tasks 3, 6, 10)

The X Articles editor is browser-only. It runs over `/i/api/graphql/<opHash>/<OperationName>` with two auth artifacts that OAuth 2.0 PKCE does **not** provide:

- `auth_token` cookie — long-lived session token.
- `ct0` cookie — CSRF token mirrored as the `x-csrf-token` request header.
- `x-twitter-active-user: yes`, `x-twitter-auth-type: OAuth2Session`, and a hardcoded `Bearer` for the web app (NOT a user OAuth bearer).

Implication: the printed CLI must support **two parallel auth sources**:

- **Source A — OAuth 2.0 PKCE.** Standard CLI auth flow, used for every endpoint from the official v2 spec. Stored at `~/.config/x-pp-cli/oauth2.json`.
- **Source B — Browser session cookies.** Used only for the sniffed Articles GraphQL endpoints. The cookie capture is performed by re-running `browser-sniff` with `--auth-from <enriched-capture>` against a logged-in browser, OR by a manual one-time extraction (documented in the SKILL). Stored at `~/.config/x-pp-cli/cookies.json` with `0600` perms. Refreshed manually when the session expires.

**Generator hint:** when generation runs, the v2 paths use OAuth 2.0; the sniffed paths use the cookie source. The generator may emit a single client; the plan expects to **post-edit the generated client** in Task 6 to add a per-host auth selector (host `api.x.com` → OAuth 2.0; host `x.com` → cookies). If the generator already supports per-host auth selection, prefer that.

### GraphQL risk acknowledgment

X's web app uses GraphQL with rotating operation hashes. Sniffed paths look like `/i/api/graphql/aBcD123XyZ/CreateArticle`. The hash changes when X redeploys the web app. Three implications:

1. The sniffed spec captures a **point-in-time** hash; expect drift.
2. Treat captured hashes as runtime config, not compile-time constants. Store them in `~/.config/x-pp-cli/article-ops.json`; let users re-sniff and update.
3. If `browser-sniff` produces an unusable spec (no operationIds, no schemas, hashes baked into paths), escalate before continuing — the sniff may need pre-processing or a hand-written GraphQL operation map.

This is the single largest feasibility risk in the plan. Task 3 must verify the sniff produced something usable before Task 5 (generation) proceeds. If it didn't, stop and re-plan the Articles surface.

---

## Task 1: Catalog Entry

**File:** `catalog/x.yaml`

- [ ] **Step 1: Re-confirm valid enums and `known_alternatives` shape**

```bash
rg -n "validSpecFormats|validCategories" internal/catalog/catalog.go
rg -n "type KnownAlt" internal/catalog/catalog.go
```
Expected: `KnownAlt` is a struct with `Name`, `URL`, `Language` fields. Use struct list shape, not scalar strings.

- [ ] **Step 2: Create `catalog/x.yaml`**

```yaml
name: x
display_name: X (Twitter)
description: X v2 API mirror with first-class Articles support — tweets, users, lists, spaces, bookmarks, likes via the official OpenAPI; long-form Articles authoring via browser-sniffed editor endpoints (GraphQL).
category: social-and-messaging
spec_url: https://api.x.com/2/openapi.json
spec_format: custom
tier: community
verified_date: "2026-05-08"
homepage: https://docs.x.com/x-api
notes: |
  Hybrid spec. The catalog points at the official v2 OpenAPI for provenance, but
  generation feeds `printing-press generate` two specs simultaneously
  (`--spec official.yaml --spec sniffed.yaml --name x`); see Task 5.

  Auth is split: OAuth 2.0 PKCE for v2 endpoints (Source A), browser session
  cookies for the sniffed Articles GraphQL endpoints (Source B). The CLI
  selects auth per-host. See "Articles auth design" in the plan.

  X Articles is GraphQL with rotating operation hashes — captured at sniff
  time, refreshed when X redeploys. Treat hashes as runtime config, not
  compile-time constants.

  Pro-tier-only endpoints (filtered stream, full-archive search) are excluded
  from v1. Free/Basic tier limits apply.
known_alternatives:
  - name: twurl
    url: https://github.com/twitterdev/twurl
    language: ruby
  - name: rettiwt-api
    url: https://github.com/Rishikant181/Rettiwt-API
    language: typescript
  - name: twscrape
    url: https://github.com/vladkens/twscrape
    language: python
```

- [ ] **Step 3: Validate**

```bash
go build -o ./printing-press ./cmd/printing-press
./printing-press catalog list | grep '^x '
./printing-press catalog show x
go test ./internal/catalog/...
```
Expected: list includes `x`; show prints without errors; tests pass.

- [ ] **Step 4: Commit**

```bash
git add catalog/x.yaml
git commit -m "feat(cli): add x catalog entry"
```

---

## Task 2: Capture Official OpenAPI

**Files:**
- Create: `~/printing-press/specs/x/x-official.yaml` (working dir, not committed)

- [ ] **Step 1: Confirm host**

```bash
curl -sSf -o /dev/null -w "%{http_code}\n" https://api.x.com/2/openapi.json
```
If 200, use `api.x.com`. If 404, fall back to `https://api.twitter.com/2/openapi.json`. Record which one served in a comment in the spec file.

- [ ] **Step 2: Pull and validate**

```bash
mkdir -p ~/printing-press/specs/x
curl -sSf https://api.x.com/2/openapi.json -o ~/printing-press/specs/x/x-official.json

# Convert JSON → YAML for downstream uniformity (optional; --spec accepts both)
yq -P eval '.' ~/printing-press/specs/x/x-official.json > ~/printing-press/specs/x/x-official.yaml

# Press parser dry-run + validate
./printing-press generate --spec ~/printing-press/specs/x/x-official.yaml --name x-tmp --dry-run --validate
```
Expected: parser accepts the spec without fatal errors. Warnings are fine.

- [ ] **Step 3: Strip Pro-only endpoints**

The official spec includes filtered-stream and full-archive endpoints that 4xx on Free/Basic tier. Keep them out of the printed surface.

```bash
yq eval 'del(.paths."/2/tweets/search/stream", .paths."/2/tweets/search/all", .paths."/2/tweets/counts/all")' \
  ~/printing-press/specs/x/x-official.yaml > ~/printing-press/specs/x/x-official.tmp.yaml
mv ~/printing-press/specs/x/x-official.tmp.yaml ~/printing-press/specs/x/x-official.yaml

yq eval '.paths | keys | .[] | select(test("stream|search/all|counts/all"))' ~/printing-press/specs/x/x-official.yaml
```
Expected: empty output.

- [ ] **Step 4: Sanity-check size**

```bash
wc -l ~/printing-press/specs/x/x-official.yaml
```
Expected: 5k–15k lines order of magnitude. <1k or >50k means investigate.

(No commit — working artifact.)

---

## Task 3: Browser-Sniff Articles (highest-risk task — verify output before proceeding)

**Files:**
- Create: `~/printing-press/specs/x/x-articles-sniffed.yaml`
- Create: `~/printing-press/specs/x/x-articles-capture.har` (working artifact, not committed)
- Create: `~/printing-press/specs/x/x-articles-cookies.json` (working artifact, **never committed**)

- [ ] **Step 1: Confirm flag set**

```bash
./printing-press browser-sniff --help
```
Expected flags: `--har`, `--output`, `--name`, `--analysis-output`, `--auth-from`, `--blocklist`. If any of those is missing, stop — the press surface has changed and the rest of this task needs re-derivation.

- [ ] **Step 2: Capture an Articles session**

In a logged-in browser tab on `x.com`, open DevTools → Network and start a HAR recording. Then:
1. Click "Articles" in the compose surface.
2. Create a new article: title, body, cover image upload, save draft.
3. Edit the draft.
4. Publish.
5. List your articles (profile → Articles tab).
6. Open the published article (read).
7. Delete it.

Save the HAR to `~/printing-press/specs/x/x-articles-capture.har`.

- [ ] **Step 3: Run browser-sniff**

```bash
./printing-press browser-sniff \
  --har ~/printing-press/specs/x/x-articles-capture.har \
  --output ~/printing-press/specs/x/x-articles-sniffed.yaml \
  --name x-articles \
  --analysis-output ~/printing-press/specs/x/x-articles-analysis.json
```
Expected: a YAML OpenAPI fragment with paths under `/i/api/graphql/...` for create, update, list, get, delete article, plus media upload.

- [ ] **Step 4: GO/NO-GO check on sniff usability**

Open `~/printing-press/specs/x/x-articles-sniffed.yaml` and `x-articles-analysis.json`. Confirm at minimum:
- Each operation has an `operationId`. (Sniff usually generates one; rename collisions: `articlesCreate`, `articlesUpdate`, `articlesList`, `articlesGet`, `articlesDelete`, `articlesUploadMedia`.)
- Path keys are stable and don't collide with the official spec's `/2/...` paths.
- Request/response examples are present (sniff captures real bodies; keep them — they drive better client codegen).

If any of these are missing, **STOP and escalate**: the GraphQL hash structure may have made the spec unusable. Re-plan: either hand-write a small OpenAPI fragment using the captured analysis JSON as a guide, or treat Articles as a `client_pattern: graphql` with a separate operation-map file. Do not proceed to Task 5 with a broken spec.

- [ ] **Step 5: Capture session cookies for Source B auth**

The HAR contains `Cookie:` headers on every Articles request. Extract `auth_token` and `ct0` and persist them for runtime use. **This file is local-only and is NEVER committed** — it contains a live session token. The repo's `.gitignore` already excludes `~/.config/`; the working dir under `~/printing-press/specs/x/` is also outside the repo.

```bash
# One-time extraction. Substitute the actual cookie values from the HAR.
mkdir -p ~/.config/x-pp-cli
cat > ~/.config/x-pp-cli/cookies.json <<'EOF'
{
  "auth_token": "<from HAR>",
  "ct0": "<from HAR>",
  "captured_at": "2026-05-08T00:00:00Z"
}
EOF
chmod 600 ~/.config/x-pp-cli/cookies.json
```

Add a SKILL.md note (Task 12) explaining session expiry and re-capture flow.

- [ ] **Step 6: Commit a redacted sniffed spec to testdata**

This step IS the only Task 3 artifact that lands in `cli-printing-press/`. For reproducibility (and golden-suite grounding if added later), copy a redacted version into the repo. Strip any header/cookie values, redact bearer tokens; keep paths and JSON schemas.

```bash
cp ~/printing-press/specs/x/x-articles-sniffed.yaml testdata/specs/x-articles-sniffed.yaml
# Hand-redact any captured tokens, then:
git add testdata/specs/x-articles-sniffed.yaml
git commit -m "test(cli): add redacted X Articles sniffed spec fixture"
```

---

## Task 4: Initial Generation

- [ ] **Step 1: Generate via multi-spec input**

`generate` accepts repeated `--spec`; no separate merge step needed.

```bash
./printing-press generate \
  --spec ~/printing-press/specs/x/x-official.yaml \
  --spec ~/printing-press/specs/x/x-articles-sniffed.yaml \
  --name x \
  --output ~/printing-press/library/x \
  --auth oauth2-pkce \
  --spec-source sniffed
```
Expected: directory tree at `~/printing-press/library/x/` with `cmd/x-pp-cli/`, `internal/cli/`, `internal/store/`, `internal/client/`, `cmd/x-pp-mcp/`, `cli-skills/pp-x/`.

- [ ] **Step 2: Build**

```bash
cd ~/printing-press/library/x
go mod tidy
go build -o ./x-pp-cli ./cmd/x-pp-cli
go build -o ./x-pp-mcp ./cmd/x-pp-mcp
```

- [ ] **Step 3: Smoke**

```bash
./x-pp-cli --help
./x-pp-cli version
./x-pp-cli doctor
```

- [ ] **Step 4: Run generated tests**

```bash
go test ./...
```

- [ ] **Step 5: Format**

```bash
go fmt ./...
```

(No git commit — local library is not a repo.)

---

## Task 5: Per-Host Auth Selector in the Generated Client

The generated client likely uses one auth chain (OAuth 2.0). The Articles surface needs cookie auth. This task adds a per-host selector before novel commands depend on it.

**Files:**
- Modify: `~/printing-press/library/x/internal/client/client.go` (post-edit generated code)
- Create: `~/printing-press/library/x/internal/client/cookies.go`
- Create: `~/printing-press/library/x/internal/client/cookies_test.go`

- [ ] **Step 1: Inspect the generated transport**

```bash
sed -n '/func .*RoundTrip\|http.Client\|setAuth/,/^}/p' ~/printing-press/library/x/internal/client/client.go | head -120
```
Note the existing auth-injection point.

- [ ] **Step 2: Implement cookie loader + per-host selector**

```go
// cookies.go
package client

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "time"
)

type CookieAuth struct {
    AuthToken   string    `json:"auth_token"`
    CT0         string    `json:"ct0"`
    CapturedAt  time.Time `json:"captured_at"`
}

func LoadCookies() (*CookieAuth, error) {
    home, err := os.UserHomeDir()
    if err != nil { return nil, err }
    path := filepath.Join(home, ".config", "x-pp-cli", "cookies.json")
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("read %s: %w (run browser-sniff to capture)", path, err)
    }
    var c CookieAuth
    if err := json.Unmarshal(data, &c); err != nil {
        return nil, fmt.Errorf("parse cookies: %w", err)
    }
    if c.AuthToken == "" || c.CT0 == "" {
        return nil, fmt.Errorf("cookies file missing auth_token or ct0")
    }
    return &c, nil
}

// ApplyCookieAuth attaches Source B auth to a request bound for x.com.
func (c *CookieAuth) Apply(req *http.Request) {
    req.AddCookie(&http.Cookie{Name: "auth_token", Value: c.AuthToken})
    req.AddCookie(&http.Cookie{Name: "ct0", Value: c.CT0})
    req.Header.Set("x-csrf-token", c.CT0)
    req.Header.Set("x-twitter-active-user", "yes")
    req.Header.Set("x-twitter-auth-type", "OAuth2Session")
    // The web app uses a hardcoded public Bearer for /i/api; capture from HAR and inline.
    req.Header.Set("Authorization", "Bearer "+webAppBearer)
}
```

- [ ] **Step 3: Wire into the transport**

In the generated transport's RoundTrip (or equivalent injection point), branch on host:

```go
func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
    switch req.URL.Host {
    case "api.x.com", "api.twitter.com":
        // Source A: OAuth 2.0 (existing behavior)
        return t.oauthRoundTrip(req)
    case "x.com", "twitter.com":
        // Source B: cookies
        if t.cookies == nil {
            c, err := LoadCookies()
            if err != nil { return nil, err }
            t.cookies = c
        }
        t.cookies.Apply(req)
        return t.base.RoundTrip(req)
    default:
        return t.base.RoundTrip(req)
    }
}
```

- [ ] **Step 4: Unit test the loader**

```go
func TestLoadCookies_RoundTrip(t *testing.T) {
    dir := t.TempDir()
    t.Setenv("HOME", dir)
    cfg := filepath.Join(dir, ".config", "x-pp-cli")
    require.NoError(t, os.MkdirAll(cfg, 0o700))
    require.NoError(t, os.WriteFile(filepath.Join(cfg, "cookies.json"),
        []byte(`{"auth_token":"a","ct0":"c","captured_at":"2026-05-08T00:00:00Z"}`), 0o600))
    c, err := LoadCookies()
    require.NoError(t, err)
    assert.Equal(t, "a", c.AuthToken)
    assert.Equal(t, "c", c.CT0)
}

func TestLoadCookies_Missing(t *testing.T) {
    dir := t.TempDir()
    t.Setenv("HOME", dir)
    _, err := LoadCookies()
    require.Error(t, err)
    assert.Contains(t, err.Error(), "browser-sniff")
}
```

- [ ] **Step 5: Test, format**

```bash
go test ./internal/client/ -run Cookies -v
go fmt ./...
```

---

## Task 6: Store API and Schema

Two-part task. **Step group A** adds Go-level types and helpers (`Tweet`, `Article`, `UpsertTweet`, `UpsertArticle`, `GetCursor`, `SetCursor`); **Step group B** runs the SQL migration. Both groups land before any novel command consumes them.

**Files:**
- Modify: `~/printing-press/library/x/internal/store/store.go`
- Create: `~/printing-press/library/x/internal/store/x_types.go`
- Create: `~/printing-press/library/x/internal/store/x_helpers.go`
- Create: `~/printing-press/library/x/internal/store/x_schema_test.go`

### 6A — Types and helpers

- [ ] **Step 1: Define Tweet/Article/Cursor types**

```go
// x_types.go
package store

import "time"

type Tweet struct {
    ID                 string
    AuthorID           string
    Text               string
    CreatedAt          time.Time
    ConversationID     string
    InReplyToUserID    string
    ReferencedTweetID  string
    ReferencedTweetType string
    Lang               string
    Source             string
    PossiblySensitive  bool
    RetweetCount       int
    ReplyCount         int
    LikeCount          int
    QuoteCount         int
    ImpressionCount    int
    BookmarkCount      int
    RawJSON            string
}

type Article struct {
    ID             string
    AuthorID       string
    Title          string
    Body           string
    BodyFormat     string // html | markdown
    CoverMediaID   string
    CoverURL       string
    Status         string // draft | published
    PublishedAt    *time.Time
    UpdatedAt      *time.Time
    ViewCount      int
    ReadCount      int
    BookmarkCount  int
    RawJSON        string
}

type Cursor struct {
    Resource         string
    SinceID          string
    PaginationToken  string
    LastSyncedAt     time.Time
}
```

- [ ] **Step 2: Implement helpers**

```go
// x_helpers.go
package store

import (
    "context"
    "database/sql"
    "errors"
    "time"
)

func (s *Store) UpsertTweet(ctx context.Context, t Tweet) error {
    _, err := s.DB().ExecContext(ctx, `
INSERT INTO tweets (
  id, author_id, text, created_at, conversation_id, in_reply_to_user_id,
  referenced_tweet_id, referenced_tweet_type, lang, source, possibly_sensitive,
  retweet_count, reply_count, like_count, quote_count, impression_count,
  bookmark_count, raw_json, fetched_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)
ON CONFLICT(id) DO UPDATE SET
  text = excluded.text,
  retweet_count = excluded.retweet_count,
  reply_count = excluded.reply_count,
  like_count = excluded.like_count,
  quote_count = excluded.quote_count,
  impression_count = excluded.impression_count,
  bookmark_count = excluded.bookmark_count,
  raw_json = excluded.raw_json,
  fetched_at = CURRENT_TIMESTAMP
`, t.ID, t.AuthorID, t.Text, t.CreatedAt.Format(time.RFC3339), t.ConversationID,
        t.InReplyToUserID, t.ReferencedTweetID, t.ReferencedTweetType, t.Lang, t.Source,
        boolToInt(t.PossiblySensitive), t.RetweetCount, t.ReplyCount, t.LikeCount,
        t.QuoteCount, t.ImpressionCount, t.BookmarkCount, t.RawJSON)
    return err
}

func (s *Store) UpsertArticle(ctx context.Context, a Article) error {
    // Same shape as UpsertTweet; ON CONFLICT(id) DO UPDATE on view/read/bookmark counts + body/title.
    // Implementation omitted for brevity.
}

func (s *Store) GetCursor(ctx context.Context, resource string) (Cursor, error) {
    var c Cursor
    var ts sql.NullString
    err := s.DB().QueryRowContext(ctx,
        `SELECT resource, since_id, pagination_token, last_synced_at FROM sync_cursors WHERE resource = ?`,
        resource,
    ).Scan(&c.Resource, &c.SinceID, &c.PaginationToken, &ts)
    if errors.Is(err, sql.ErrNoRows) {
        return Cursor{Resource: resource}, nil
    }
    if err != nil { return c, err }
    if ts.Valid { c.LastSyncedAt, _ = time.Parse(time.RFC3339, ts.String) }
    return c, nil
}

func (s *Store) SetCursor(ctx context.Context, c Cursor) error {
    _, err := s.DB().ExecContext(ctx, `
INSERT INTO sync_cursors (resource, since_id, pagination_token, last_synced_at)
VALUES (?,?,?,?)
ON CONFLICT(resource) DO UPDATE SET
  since_id = excluded.since_id,
  pagination_token = excluded.pagination_token,
  last_synced_at = excluded.last_synced_at
`, c.Resource, c.SinceID, c.PaginationToken, time.Now().UTC().Format(time.RFC3339))
    return err
}

func boolToInt(b bool) int { if b { return 1 }; return 0 }
```

### 6B — Schema migration

- [ ] **Step 3: Read existing migrate()**

```bash
sed -n '/^func .*migrate/,/^}/p' ~/printing-press/library/x/internal/store/store.go | head -80
```
Note the current `schema_version` constant.

- [ ] **Step 4: Extend migrate() with X tables**

Append a new version branch in `migrate()` that runs this DDL. Bump the version number (e.g., 1 → 2).

```sql
-- tweets
CREATE TABLE IF NOT EXISTS tweets (
  id TEXT PRIMARY KEY,
  author_id TEXT NOT NULL,
  text TEXT NOT NULL,
  created_at TEXT NOT NULL,
  conversation_id TEXT,
  in_reply_to_user_id TEXT,
  referenced_tweet_id TEXT,
  referenced_tweet_type TEXT,
  lang TEXT,
  source TEXT,
  possibly_sensitive INTEGER,
  retweet_count INTEGER DEFAULT 0,
  reply_count INTEGER DEFAULT 0,
  like_count INTEGER DEFAULT 0,
  quote_count INTEGER DEFAULT 0,
  impression_count INTEGER DEFAULT 0,
  bookmark_count INTEGER DEFAULT 0,
  raw_json TEXT NOT NULL,
  fetched_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_tweets_author_created ON tweets(author_id, created_at DESC);

-- articles
CREATE TABLE IF NOT EXISTS articles (
  id TEXT PRIMARY KEY,
  author_id TEXT NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  body_format TEXT NOT NULL DEFAULT 'html',
  cover_media_id TEXT,
  cover_url TEXT,
  status TEXT NOT NULL DEFAULT 'published',
  published_at TEXT,
  updated_at TEXT,
  view_count INTEGER DEFAULT 0,
  read_count INTEGER DEFAULT 0,
  bookmark_count INTEGER DEFAULT 0,
  raw_json TEXT NOT NULL,
  fetched_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_articles_author_published ON articles(author_id, published_at DESC);

-- bookmarks (FK to tweets so a sync can reuse tweet rows)
CREATE TABLE IF NOT EXISTS bookmarks (
  tweet_id TEXT PRIMARY KEY REFERENCES tweets(id) ON DELETE CASCADE,
  bookmarked_at TEXT NOT NULL
);

-- likes
CREATE TABLE IF NOT EXISTS likes (
  tweet_id TEXT PRIMARY KEY REFERENCES tweets(id) ON DELETE CASCADE,
  liked_at TEXT NOT NULL
);

-- sync cursors
CREATE TABLE IF NOT EXISTS sync_cursors (
  resource TEXT PRIMARY KEY,
  since_id TEXT,
  pagination_token TEXT,
  last_synced_at TEXT
);

-- FTS5
CREATE VIRTUAL TABLE IF NOT EXISTS tweets_fts USING fts5(
  text, content='tweets', content_rowid='rowid'
);
CREATE VIRTUAL TABLE IF NOT EXISTS articles_fts USING fts5(
  title, body, content='articles', content_rowid='rowid'
);

-- Triggers (Ground Rule 11: insert/delete/update — all three required)
CREATE TRIGGER IF NOT EXISTS tweets_ai AFTER INSERT ON tweets BEGIN
  INSERT INTO tweets_fts(rowid, text) VALUES (new.rowid, new.text);
END;
CREATE TRIGGER IF NOT EXISTS tweets_ad AFTER DELETE ON tweets BEGIN
  INSERT INTO tweets_fts(tweets_fts, rowid, text) VALUES('delete', old.rowid, old.text);
END;
CREATE TRIGGER IF NOT EXISTS tweets_au AFTER UPDATE ON tweets BEGIN
  INSERT INTO tweets_fts(tweets_fts, rowid, text) VALUES('delete', old.rowid, old.text);
  INSERT INTO tweets_fts(rowid, text) VALUES (new.rowid, new.text);
END;

CREATE TRIGGER IF NOT EXISTS articles_ai AFTER INSERT ON articles BEGIN
  INSERT INTO articles_fts(rowid, title, body) VALUES (new.rowid, new.title, new.body);
END;
CREATE TRIGGER IF NOT EXISTS articles_ad AFTER DELETE ON articles BEGIN
  INSERT INTO articles_fts(articles_fts, rowid, title, body) VALUES('delete', old.rowid, old.title, old.body);
END;
CREATE TRIGGER IF NOT EXISTS articles_au AFTER UPDATE ON articles BEGIN
  INSERT INTO articles_fts(articles_fts, rowid, title, body) VALUES('delete', old.rowid, old.title, old.body);
  INSERT INTO articles_fts(rowid, title, body) VALUES (new.rowid, new.title, new.body);
END;
```

- [ ] **Step 5: Tests**

```go
// x_schema_test.go
func TestXSchemaCreated(t *testing.T) { /* asserts every table/index/trigger exists */ }
func TestTweetsFTSRoundtrip(t *testing.T) { /* insert, search, update, search again */ }
func TestUpsertTweet_Idempotent(t *testing.T) { /* upsert same ID twice, count == 1 */ }
func TestCursor_RoundTrip(t *testing.T) { /* SetCursor, GetCursor returns same values */ }
```

```bash
go test ./internal/store/ -v
go fmt ./...
```

---

## Task 7: Novel Command — `archive sync`

**Files:**
- Create: `~/printing-press/library/x/internal/cli/archive.go`
- Create: `~/printing-press/library/x/internal/cli/archive_test.go`
- Modify: `~/printing-press/library/x/internal/cli/root.go` (register in `newRootCmd(flags)` — Ground Rule 12)

**Error policy (pick one and make code match):** Per-resource isolation. If `sync tweets` fails, log the error and continue with `sync articles`. Final exit code is non-zero if any resource failed. Document this with `Annotations["pp:typed-exit-codes"] = "0,2"`.

- [ ] **Step 1: Test the per-resource isolation**

```go
func TestArchiveSync_ContinuesPastResourceError(t *testing.T) {
    s := openTestStore(t)
    client := &fakeClient{
        tweetsErr: errors.New("rate limited"),
        articles:  []store.Article{{ID: "a1", AuthorID: "u1", Title: "x", Body: "y", RawJSON: "{}"}},
    }
    err := RunArchiveSync(ctx, s, client, "u1", "all")
    require.Error(t, err) // overall failure recorded
    var n int
    s.DB().QueryRow("SELECT count(*) FROM articles").Scan(&n)
    assert.Equal(t, 1, n) // articles still synced
}
```

- [ ] **Step 2: Implement**

```go
// archive.go
func RunArchiveSync(ctx context.Context, s *store.Store, c Client, userID, what string) error {
    steps := []struct {
        name string
        run  func(context.Context, *store.Store, Client, string) error
    }{
        {"tweets",    SyncTweets},
        {"articles",  SyncArticles},
        {"bookmarks", SyncBookmarks},
        {"likes",     SyncLikes},
    }
    var firstErr error
    for _, step := range steps {
        if what != "all" && what != step.name { continue }
        if err := step.run(ctx, s, c, userID); err != nil {
            fmt.Fprintf(os.Stderr, "sync %s: %v\n", step.name, err)
            if firstErr == nil { firstErr = err }
            continue // per-resource isolation
        }
        fmt.Fprintf(os.Stdout, "synced %s\n", step.name)
    }
    return firstErr
}

func SyncTweets(ctx context.Context, s *store.Store, c Client, userID string) error {
    cursor, _ := s.GetCursor(ctx, "tweets")
    for {
        page, next, err := c.UserTweets(ctx, userID, cursor.SinceID, cursor.PaginationToken)
        if err != nil {
            // No retry-with-backoff in v1. Surface the error; resume on next run via cursor.
            return err
        }
        for _, t := range page {
            if err := s.UpsertTweet(ctx, t); err != nil { return err }
        }
        if next == "" { break }
        cursor.PaginationToken = next
    }
    cursor.LastSyncedAt = time.Now().UTC()
    return s.SetCursor(ctx, cursor)
}
```

(SyncArticles, SyncBookmarks, SyncLikes follow the same shape. Articles uses Source B auth via the per-host transport.)

- [ ] **Step 3: Cobra wrapper with annotations**

```go
func newArchiveSyncCmd() *cobra.Command {
    var what string
    cmd := &cobra.Command{
        Use:   "sync",
        Short: "Pull tweets, articles, bookmarks, and likes into the local store",
        Annotations: map[string]string{
            "mcp:read-only":      "true",
            "pp:typed-exit-codes": "0,2",
        },
        RunE: func(cmd *cobra.Command, args []string) error { /* glue */ },
    }
    cmd.Flags().StringVar(&what, "what", "all", "Which resource(s) to sync: all|tweets|articles|bookmarks|likes")
    return cmd
}
```

- [ ] **Step 4: Register, test, format, smoke MCP**

```bash
go test ./internal/cli/ -run TestArchiveSync -v
go fmt ./...
./x-pp-cli --help | grep -A1 archive
./x-pp-mcp --list-tools 2>&1 | grep archive
```

---

## Task 8: Novel Command — `voice-mirror find`

**Files:**
- Create: `~/printing-press/library/x/internal/cli/voice.go`
- Create: `~/printing-press/library/x/internal/cli/voice_test.go`

`voice-mirror find <topic>` returns top-engagement posts (tweets + articles) matching a topic, ranked by `like_count + 2*reply_count + 5*bookmark_count`.

- [ ] **Step 1: Test query semantics** — seed varying engagement, query, assert order.

- [ ] **Step 2: Implement**

```go
const voiceQuery = `
SELECT 'tweet' AS kind, t.id, t.text AS body,
       t.like_count, t.reply_count, t.bookmark_count
FROM tweets t
JOIN tweets_fts f ON f.rowid = t.rowid
WHERE tweets_fts MATCH ?
UNION ALL
SELECT 'article' AS kind, a.id, a.title || ' — ' || substr(a.body, 1, 200),
       0, 0, a.bookmark_count
FROM articles a
JOIN articles_fts af ON af.rowid = a.rowid
WHERE articles_fts MATCH ?
ORDER BY (like_count + 2*reply_count + 5*bookmark_count) DESC
LIMIT ?;`
```

Annotate `mcp:read-only: true`. Register, test, format, smoke.

---

## Task 9: Novel Commands — `analytics summary` + `repost-candidates`

**Files:**
- Create: `~/printing-press/library/x/internal/cli/analytics.go`
- Create: `~/printing-press/library/x/internal/cli/analytics_test.go`

`analytics summary` produces a per-week roll-up. `repost-candidates` returns evergreen-eligible tweets (>30 days old, top 20% by engagement).

- [ ] **Step 1: Test summary aggregation; implement.**

```sql
SELECT strftime('%Y-W%W', created_at) AS week,
       count(*) AS tweet_count,
       sum(impression_count) AS impressions,
       sum(like_count) AS likes
FROM tweets
WHERE created_at > date('now', ?)
GROUP BY week ORDER BY week DESC;
```

- [ ] **Step 2: Test repost-candidates; implement.**

```sql
WITH ranked AS (
  SELECT id, text, like_count + 2*reply_count + 5*bookmark_count AS score
  FROM tweets WHERE created_at < date('now', '-30 days')
)
SELECT id, text, score FROM ranked
WHERE score >= (
  SELECT score FROM ranked
  ORDER BY score DESC
  LIMIT 1 OFFSET (SELECT cast(count(*)*0.2 AS int) FROM ranked)
)
ORDER BY score DESC LIMIT 20;
```

- [ ] **Step 3: Register, annotate `mcp:read-only`, format, smoke.**

---

## Task 10: Novel Command — `article publish <md>`

**Files:**
- Create: `~/printing-press/library/x/internal/cli/article.go`
- Create: `~/printing-press/library/x/internal/cli/article_test.go`
- Create: `~/printing-press/library/x/testdata/sample.md`

Default behavior: dry-run (print payload). `--post` triggers real call. Short-circuit on `cliutil.IsVerifyEnv()`. Uses Source B auth via the per-host transport (Task 5). For `--post`, the operation hash captured during sniff is loaded from `~/.config/x-pp-cli/article-ops.json` (which Task 12's SKILL documents how to refresh).

- [ ] **Step 1: Frontmatter shape**

```yaml
---
title: How I Stopped Writing Threads and Started Writing Articles
cover: ./covers/2026-05-08-articles.png
tags: [writing, x, articles]
summary: A short note on long-form X.
---
```

- [ ] **Step 2: Test parser**

```go
func TestParseArticleMarkdown(t *testing.T) {
    md := []byte("---\ntitle: Test\ncover: ./img.png\n---\n\nBody.")
    art, err := parseArticleMarkdown(md, "/some/dir")
    require.NoError(t, err)
    assert.Equal(t, "Test", art.Title)
    assert.Equal(t, "/some/dir/img.png", art.CoverPath)
}
```

- [ ] **Step 3: Implement parser** (frontmatter via `gopkg.in/yaml.v3`).

- [ ] **Step 4: Test dry-run** — default prints payload, doesn't call API.

- [ ] **Step 5: Implement publish**

```go
func newArticlePublishCmd() *cobra.Command {
    var post bool
    cmd := &cobra.Command{
        Use:   "publish <markdown-file>",
        Short: "Publish an X Article from a markdown file",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            if cliutil.IsVerifyEnv() {
                fmt.Fprintln(cmd.OutOrStdout(), "verify-env: skipping article publish")
                return nil
            }
            art, err := parseArticleMarkdown(mustRead(args[0]), filepath.Dir(args[0]))
            if err != nil { return err }
            payload := buildPublishPayload(art)
            if !post {
                fmt.Fprintln(cmd.OutOrStdout(), "DRY-RUN — pass --post to actually publish")
                return printJSON(cmd.OutOrStdout(), payload)
            }
            client, err := newClientFromConfig()
            if err != nil { return err }
            if art.CoverPath != "" {
                mediaID, err := client.UploadArticleCover(cmd.Context(), art.CoverPath)
                if err != nil { return fmt.Errorf("upload cover: %w", err) }
                payload.CoverMediaID = mediaID
            }
            resp, err := client.CreateArticle(cmd.Context(), payload)
            if err != nil {
                // Print response body so the agent can see the actual error from x.com
                fmt.Fprintf(cmd.ErrOrStderr(), "request: %+v\n", payload)
                return fmt.Errorf("create article: %w", err)
            }
            // Mirror locally
            s, err := openStore()
            if err == nil {
                defer s.Close()
                if err := s.UpsertArticle(cmd.Context(), resp.ToStoreArticle()); err != nil {
                    fmt.Fprintf(cmd.ErrOrStderr(), "warn: local mirror failed: %v\n", err)
                }
            }
            fmt.Fprintf(cmd.OutOrStdout(), "Published: %s\n", resp.URL)
            return nil
        },
    }
    cmd.Flags().BoolVar(&post, "post", false, "Actually publish (default: dry-run)")
    return cmd
}
```

- [ ] **Step 6: `article list`, `article get`, `article delete`** — `list`/`get` are `mcp:read-only`; `delete` is destructive (no annotation).

- [ ] **Step 7: Test, format, smoke**

```bash
go test ./internal/cli/ -run Article -v
go fmt ./...
./x-pp-cli article publish testdata/sample.md   # prints DRY-RUN
./x-pp-cli article list                          # requires Source B auth
```

---

## Task 11: Novel Command — `thread compose <md>`

**Files:**
- Create: `~/printing-press/library/x/internal/cli/thread.go`
- Create: `~/printing-press/library/x/internal/cli/thread_test.go`

Splits a markdown doc into a numbered tweet thread. **Important:** budget for the `(N/M)` numbering suffix BEFORE splitting, not after. Use rune count, not byte count, and apply X's weighted character counting (URLs count as 23, CJK/emoji weighted heavier — for v1, approximate by counting runes and leaving a 20% safety margin under 280).

- [ ] **Step 1: Test the splitter (numbering-aware)**

```go
func TestSplitForThread_BudgetsForNumbering(t *testing.T) {
    md := strings.Repeat("word ", 200)
    parts, err := splitForThread(md, 280)
    require.NoError(t, err)
    n := len(parts)
    suffixLen := len(fmt.Sprintf(" (%d/%d)", n, n)) // worst case
    for _, p := range parts {
        assert.LessOrEqual(t, utf8.RuneCountInString(p)+suffixLen, 280)
    }
}

func TestSplitter_PreservesCodeFences(t *testing.T) {
    md := "Intro.\n\n```go\nfunc main() {}\n```\n\nOutro."
    parts, err := splitForThread(md, 280)
    require.NoError(t, err)
    foundFence := 0
    for _, p := range parts {
        if strings.Contains(p, "```go") && strings.Contains(p, "```") { foundFence++ }
    }
    assert.Equal(t, 1, foundFence)
}
```

- [ ] **Step 2: Implement splitter** — two-pass: (1) coarse split by atom (paragraph, code fence, list item), (2) compute final numbering width by iterating until stable, then re-pack with budget = `limit - max-suffix-len`.

- [ ] **Step 3: Test atomic post (dry-run)**

- [ ] **Step 4: Implement post-thread**

```go
func postThread(ctx context.Context, c Client, parts []string) error {
    var prevID string
    n := len(parts)
    for i, p := range parts {
        body := p
        if n > 1 { body = fmt.Sprintf("%s (%d/%d)", p, i+1, n) }
        opts := CreateTweetOpts{Text: body}
        if prevID != "" { opts.InReplyToTweetID = prevID }
        tw, err := c.CreateTweet(ctx, opts)
        if err != nil {
            fmt.Fprintf(os.Stderr, "thread broke at part %d/%d: %v\n", i+1, n, err)
            if prevID != "" {
                fmt.Fprintf(os.Stderr, "last successful tweet ID: %s\n", prevID)
            }
            fmt.Fprintln(os.Stderr, "unposted remainder:")
            for _, rem := range parts[i:] { fmt.Fprintln(os.Stderr, "  -", rem) }
            return err
        }
        prevID = tw.ID
    }
    return nil
}
```

- [ ] **Step 5: Default dry-run, `--post` flag, `cliutil.IsVerifyEnv()` short-circuit, register, format, smoke.**

---

## Task 12: SKILL.md and skill verification

**Files:**
- Modify: `~/printing-press/library/x/cli-skills/pp-x/SKILL.md`

- [ ] **Step 1: Read the generated skeleton** and read substack's SKILL.md as the reference.

- [ ] **Step 2: Add a "Novel commands" section** documenting all six with example invocations and trigger phrases.

- [ ] **Step 3: Add an "Auth setup" section** explaining:
  - OAuth 2.0 PKCE for v2 endpoints (one-time `x-pp-cli auth login`).
  - Browser session cookie capture for Articles (one-time HAR + extraction; documented step-by-step with the `browser-sniff --auth-from` flow). Refresh when X invalidates the session.
  - Articles operation-hash refresh (when `article publish` returns 404 from a stale hash, re-sniff and update `~/.config/x-pp-cli/article-ops.json`).

- [ ] **Step 4: Replace the install header** (Ground Rule 14) with private-repo-aware instructions.

- [ ] **Step 5: Verify skill matches the shipped CLI**

```bash
cd /Users/cathrynlavery/Developer/mvanhorn/cli-printing-press
./printing-press verify-skill --dir ~/printing-press/library/x
```

---

## Task 13: Validate, Package, Publish

- [ ] **Step 1: Shipcheck (5-leg sweep)**

```bash
./printing-press shipcheck --dir ~/printing-press/library/x
```

- [ ] **Step 2: Tools-audit**

```bash
./printing-press tools-audit --dir ~/printing-press/library/x
```

- [ ] **Step 3: Validate publish readiness**

```bash
./printing-press publish validate --dir ~/printing-press/library/x --json
```

- [ ] **Step 4: Package**

```bash
./printing-press publish package \
  --dir ~/printing-press/library/x \
  --category social-and-messaging \
  --target ~/printing-press/.publish-staging/x \
  --json
```

- [ ] **Step 5: Copy into public-library and PR**

```bash
PUB=~/Developer/mvanhorn/printing-press-library
cp -R ~/printing-press/.publish-staging/x/. "$PUB/library/social-and-messaging/x/"
cd "$PUB"
git checkout -b feat/pp-x
git add library/social-and-messaging/x/
# Use whichever scope the public-library repo's CONTRIBUTING / pr-title workflow expects.
# This repo (cli-printing-press) requires `type(scope):` per AGENTS.md; the library repo
# may use a different convention — confirm by reading its CONTRIBUTING.md or pr-title workflow.
git commit -m "feat(library): add pp-x printed CLI for X (Twitter) v2 + Articles"
git push -u origin feat/pp-x
gh pr create --title "feat(library): add pp-x printed CLI for X (Twitter) v2 + Articles" \
  --body "$(cat <<'EOF'
## Summary
- pp-x CLI: official X v2 surface (tweets/users/lists/spaces/bookmarks/likes) plus browser-sniffed Articles editor (publish/list/get/edit/delete).
- Multi-spec input via `printing-press generate --spec official --spec sniffed --name x`. No machine change required.
- Two-source auth: OAuth 2.0 PKCE for v2; browser session cookies for Articles GraphQL.
- Novel commands: archive sync, voice-mirror find, analytics summary, repost-candidates, article publish (markdown), thread compose (markdown).

## Test plan
- [ ] shipcheck passes locally
- [ ] tools-audit clean
- [ ] verify-skill clean
- [ ] manual: archive sync against a real account
- [ ] manual: article publish dry-run + --post against a test account
EOF
)"
```

- [ ] **Step 6: Add `ready-to-merge` label** when CI passes. Mergify handles the merge.

(No companion PR in `cli-printing-press/` — this plan ships only a catalog entry plus a redacted spec fixture, both already committed in Tasks 1 and 3. They can land via the catalog PR or a separate `feat(cli): add x catalog entry` PR.)

---

## Error handling & edge cases (consolidated for review)

- **Auth failures (Source A):** generated client surfaces the X v2 error envelope; novel commands wrap with `fmt.Errorf("<step>: %w", err)`.
- **Auth failures (Source B):** if the cookie file is missing or the session is expired (HTTP 401/403 from `/i/api/...`), the CLI prints the documented re-capture flow and exits non-zero. No silent fallback.
- **Operation-hash drift:** if `article publish --post` returns 404 with a "no such operation" body, the error message instructs the user to re-sniff and update `article-ops.json`.
- **Rate limits:** generated client honors `x-rate-limit-*` headers (passes through 429 as an error). `archive sync` does NOT retry in v1 — failures surface, the cursor preserves position, the next run resumes. Exponential backoff is explicitly out of v1 scope.
- **Sniff drift (paths/schemas):** Articles requests can change shape without notice. `article publish` prints both the request payload and the response body on 4xx/5xx so an agent can see exactly what changed and re-sniff.
- **Mid-thread failure:** `thread compose --post` prints partial chain and unposted remainder; no auto-retry.
- **Cover image upload failure:** aborts before article create — no coverless article left behind.
- **Pro-tier endpoints:** stripped from the official spec at Task 2 step 3; never appear as commands.
- **Free-tier exhaustion:** `archive sync` writes `last_synced_at` to the cursor; resume continues from there.
- **`cliutil.IsVerifyEnv()`:** every write command (`article publish`, `article delete`, `thread compose --post`) short-circuits when `PRINTING_PRESS_VERIFY=1`.

## Testing strategy

- **Unit:** every novel command has `_test.go` next to it. Use `*store.Store` against `t.TempDir()` and a fake `Client` interface.
- **Schema:** `internal/store/x_schema_test.go` asserts every table/index/trigger and exercises FTS5 update path (catches missing `_au` triggers).
- **Cookies:** `internal/client/cookies_test.go` covers the loader (round-trip, missing file, malformed JSON).
- **Integration:** gated by `FULL_RUN=1`. With a real X account: `archive sync --what tweets` → ≥1 row in `tweets`; `article list` → no error.
- **Press validation:** `shipcheck` runs all five legs (dogfood, verify, workflow-verify, verify-skill, scorecard) before publish. `tools-audit` enforces MCP surface contract.
- **Manual smoke (one-time, before publish):**
  - `archive sync --what all` → rows in each table.
  - `voice-mirror find <topic>` → ranking sane.
  - `analytics summary --since 60d` → weekly buckets.
  - `article publish testdata/sample.md` (dry-run) → payload looks right.
  - `article publish testdata/sample.md --post` against a test account → article appears on x.com.
  - `thread compose testdata/long.md` (dry-run) → split readable.

---

## Acceptance (binary, end-of-plan)

- [ ] `printing-press-library` PR for `library/social-and-messaging/x/` is merged (or queued).
- [ ] `cli-printing-press` PR for `catalog/x.yaml` (+ redacted Articles spec fixture) is merged (or queued).
- [ ] All shipcheck legs green.
- [ ] All six novel commands work end-to-end against a real account.
- [ ] X v2 read endpoints exposed and visible in MCP.
- [ ] X Articles read+write endpoints exposed and visible in MCP.
- [ ] SKILL.md describes novel commands, two-source auth, and operation-hash refresh.
- [ ] `tools-audit` clean.
- [ ] No machine changes shipped (no `cli-printing-press/internal/...` Go edits).

No phased ship — done means everything above, in one cut.
