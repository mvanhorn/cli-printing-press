# Multi-source aggregator pattern

Use this path when the CLI's value comes from combining multiple upstream
sources, but one source can still seed a normal Printing Press generation run.
This is the third path between "browser-sniff a spec" and "hand-write the whole
module": generate from the dominant source, then add secondary source clients
by hand.

Good fits:

- One source has an OpenAPI/spec/HAR capture that can produce the baseline CLI.
- Additional sources are small, wrapper-only, HTML/RSS/JSON APIs, or otherwise
  better implemented as focused clients.
- The user-facing model is unified across sources, such as a single `works`,
  `places`, `events`, or `roasters` store.
- The extra sources add coverage, ranking, enrichment, or cross-source compare
  commands rather than replacing the generated baseline.

Bad fits:

- No source can seed generation. Hand-write the module instead.
- Each source needs a completely different resource model. Build separate CLIs
  or reduce scope.
- The secondary sources are only examples in docs and are not reachable enough
  to smoke test.

## Reference layout

Keep the hand-authored source layer outside generator-owned packages:

```text
internal/source/source.go
internal/source/registry.go
internal/source/<slug>/client.go
internal/source/<slug>/sync.go
internal/cli/sources.go
```

`internal/source/source.go` defines the shared contract:

```go
type Work struct {
    Source string
    ID     string
    Title  string
    URL    string
}

type SyncOptions struct {
    Limit int
}

type Source interface {
    Name() string
    Description() string
    AuthRequired() bool
    Sync(context.Context, SyncOptions) ([]Work, error)
}
```

`internal/source/registry.go` owns registration and lookup:

```go
var (
    registryMu sync.RWMutex
    registry   = map[string]Source{}
)

func Register(source Source) {
    registryMu.Lock()
    defer registryMu.Unlock()
    registry[source.Name()] = source
}

func All() []Source {
    registryMu.RLock()
    out := make([]Source, 0, len(registry))
    for _, source := range registry {
        out = append(out, source)
    }
    registryMu.RUnlock()
    sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
    return out
}

func Lookup(name string) (Source, bool) {
    registryMu.RLock()
    defer registryMu.RUnlock()
    source, ok := registry[name]
    return source, ok
}
```

Each `internal/source/<slug>/` package registers itself from `init()` and keeps
HTTP/API details private to that source. The mutex keeps copied test helpers and
late registrations race-safe; production registration should still happen from
`init()`.

## Runtime rules

- Every source client must make real external calls. Mark intentional outbound
  calls with the existing `// pp:client-call` annotation when dogfood cannot
  infer them.
- Read [per-source-rate-limiting.md](per-source-rate-limiting.md) before
  writing clients. Each source needs its own limiter and typed 429 behavior.
- Do not edit generated `internal/client` code for secondary sources. Add a
  sibling package instead so regeneration keeps the generated client clean.
- Keep generated endpoint commands intact; add aggregator commands beside them.

## Unified store

Create one lossy cross-source table for the domain entity, such as `works`.
Include source identity in the key so IDs do not collide:

```sql
CREATE TABLE works (
    source TEXT NOT NULL,
    source_id TEXT NOT NULL,
    title TEXT NOT NULL,
    url TEXT,
    raw_json TEXT,
    PRIMARY KEY (source, source_id)
);
```

If adding FTS5 manually, either use a contentless table with explicit inserts
or copy the generator's contentful table plus INSERT/UPDATE/DELETE trigger
shape from `store.go.tmpl`. Do not create a contentful FTS5 table without the
matching triggers.

## Command surface

Add a `sources` command tree:

- `sources list` prints registered source names, descriptions, and auth needs.
- `sources sync [--source <name>] [--limit N]` runs one source or all sources.
- Domain commands (`browse`, `similar`, `compare`, `search`) query the unified
  store, not individual source packages directly.

Annotate read-only commands, especially `sources list`, with
`cmd.Annotations["mcp:read-only"] = "true"` so MCP hosts do not request write
permission for registry inspection.

The command tree should make source boundaries visible without requiring the
user to know implementation package names.

## Conformance check

Before shipping an aggregator CLI, verify:

- `internal/source/source.go` defines the shared entity and `Source` contract.
- `internal/source/registry.go` exposes register/list/lookup behavior.
- Each source lives under `internal/source/<slug>/`.
- Each source client has a real outbound call and per-source rate limiting.
- `sources list` and `sources sync` exist.
- `sources list` is annotated `mcp:read-only`.
- Cross-source commands read from the unified store.
- The README and SKILL explain which sources are included and which command
  syncs them.

Known instances that motivated this pattern:

- `coffee-goat`: generated baseline plus hand-authored coffee review, Shopify,
  and YouTube source clients.
- `art-goat`: generated baseline plus hand-authored Art Institute of Chicago
  and APOD source clients.
