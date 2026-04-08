# CLI Printing Press Onboarding Guide

You give the printing press an API spec. It gives you back a Go CLI, an MCP server, and the few artifacts needed to keep the next step grounded. It handles REST (OpenAPI) and GraphQL. One command, a lean fast path, two binaries.

The key idea: most API CLI generators stop at wrapping endpoints. The printing press goes further -- it profiles each API, detects its domain archetype (communication, project management, payments, etc.), and generates domain-specific "power user" commands like `sync`, `search`, `stale`, `health`, and `similar` on top of the standard CRUD wrappers.

This is built as a Claude Code skill. You run `/printing-press Discord` inside Claude Code, and it now uses a lean brief -> generate -> build -> shipcheck loop for the normal fast path. The older 9-phase pipeline still exists behind `printing-press print` when you explicitly want resumable phase plans.

---

## How It's Used

The primary entry point for users is the **`/printing-press` Claude Code skill** (defined in `skills/printing-press/`). A user types `/printing-press <API name>` inside Claude Code and the skill drives the fast path: one research brief, generation, focused build work, then a shipcheck block using `dogfood`, `verify`, and `scorecard`. Everything else in this repo -- the Go binary, the parsers, the templates, the profiler -- exists to serve that skill.

Developers working on this codebase build and test the Go binary directly (`go build`, `go test`), but the thing you're ultimately shipping is the skill-driven experience.

---

## How Is It Organized?

```
cli-printing-press/
  cmd/printing-press/        # CLI binary entry point
  internal/
    cli/                     # Cobra command definitions
    spec/                    # Internal YAML spec format
    openapi/                 # OpenAPI 3.0+ parser
    graphql/                 # GraphQL SDL parser
    docspec/                 # Docs-to-spec generator
    generator/               # Go template engine
      templates/             # 30+ Go templates
    profiler/                # API domain profiler
    pipeline/                # Multi-phase orchestrator
    vision/                  # Feature scoring model
    llm/                     # Claude/Codex integration
    llmpolish/               # Post-gen help text polish
    catalog/                 # Catalog schema validator
  catalog/                   # 17 API catalog entries
  skills/                    # Claude Code skill defs
    printing-press/          # Main generation skill
    printing-press-catalog/  # Catalog browsing skill
  testdata/                  # Test fixtures
  docs/plans/                # Project planning docs for this repo itself
```

| Module | Responsibility |
|--------|---------------|
| `internal/spec/` | Defines `APISpec` -- the canonical intermediate format all parsers produce |
| `internal/openapi/` | Parses OpenAPI 3.0+ JSON/YAML into `APISpec` |
| `internal/graphql/` | Parses GraphQL SDL into `APISpec` |
| `internal/docspec/` | Scrapes API documentation URLs and generates `APISpec` (regex or LLM) |
| `internal/generator/` | Renders Go templates against `APISpec` to produce a CLI project |
| `internal/profiler/` | Analyzes an `APISpec` to detect domain archetype and recommend features |
| `internal/pipeline/` | Orchestrates the optional resumable plan pipeline plus shipcheck helpers |
| `internal/vision/` | Defines the feature scoring model used by the profiler |
| `internal/cli/` | Wires all Cobra commands: `generate`, `print`, `scorecard`, `dogfood`, `vision` |
| `catalog/` | YAML entries for known APIs (Discord, Stripe, Linear, etc.) with spec URLs |

Data flows through the system like this: a spec file (OpenAPI, GraphQL SDL, or internal YAML) gets parsed into an `APISpec` struct. The profiler analyzes that struct to detect domain signals and recommend features. The generator takes both the spec and the profile, selects the right templates, and renders a full Go project to disk.

The pipeline module adds a higher-level orchestration layer on top. When you run `printing-press print Discord`, it creates a 9-phase managed run under `~/printing-press/.runstate/<scope>/runs/<run-id>/` with seed documents and `state.json` for resumability. The normal skill flow does not require all 9 phases; it uses the faster direct loop unless you explicitly ask for resumable phase plans.

This project has no external service dependencies. It's a pure Go binary that reads spec files and writes generated code.

---

## Key Concepts and Abstractions

| Concept | What it means in this codebase |
|---------|-------------------------------|
| `APISpec` | The canonical intermediate representation. Every parser (OpenAPI, GraphQL, docs) converts to this before generation. Defined in `internal/spec/spec.go`. |
| Non-Obvious Insight (NOI) | A one-sentence reframe of what an API is really for. "Discord isn't a chat app -- it's a searchable knowledge base." Drives the power-user commands. |
| Domain archetype | A classification (communication, project-management, payments, etc.) auto-detected from resource names and field patterns. Determines which workflow/insight templates get rendered. |
| Profiler | Walks an `APISpec` to detect pagination patterns, CRUD ratios, domain signals, and syncable resources. Output drives template selection. |
| Vision template set | The set of optional templates selected based on the API profile: `sync`, `search`, `store`, `tail`, `analytics`, `export`, `import`. |
| Quality gates | 7 mechanical checks every generated CLI must pass: `go mod tidy`, `go vet`, `go build`, binary build, `--help`, `version`, `doctor`. |
| Two-tier scoring | Infrastructure scoring (50 pts: output modes, auth, errors, agent-native flags) + Domain correctness scoring (50 pts: path validity, auth protocol, data pipeline, dead code). |
| Dogfood validator | Catches dead flags, dead functions, invalid API paths, and auth mismatches by cross-referencing generated code against the source spec. |
| Pipeline phases | Optional 9-phase resumable pipeline: preflight, research, scaffold, enrich, regenerate, review, agent-readiness, comparative, ship. |
| Catalog entry | A YAML file in `catalog/` that maps an API name to its spec URL, format, category, and tier. Used by `DiscoverSpec()` to auto-resolve API names. |
| Creativity ladder | Rung 1-2: API wrappers + output formatting (always generated). Rung 3: local persistence. Rung 4: domain analytics. Rung 5: behavioral insights. |

---

## Primary Flows

### Flow 1: Skill Fast Path (`/printing-press` skill)

This is the normal user journey:

1. The skill resolves the API name to a spec path or URL, reuses any prior research, and writes one concise brief
2. The skill runs `printing-press generate` with the resolved spec
3. The agent or user makes only the highest-value implementation changes needed for ship-readiness
4. The skill runs one shipcheck block:
   - `printing-press dogfood`
   - `printing-press verify --fix`
   - `printing-press scorecard`
5. If a token is available and the user opted in, the skill runs a small read-only live smoke test

The important part: the default path does not require creating a 9-phase resumable pipeline.

### Flow 2: Direct Generation (`printing-press generate`)

Lower-level escape hatch for one-shot generation from a spec file:

```
printing-press generate --spec ./openapi.yaml
  |
  v
internal/cli/root.go (newGenerateCmd)
  reads spec file, detects format
  |
  +-- OpenAPI? -> internal/openapi/parser.go
  +-- GraphQL? -> internal/graphql/parser.go
  +-- YAML?    -> internal/spec/spec.go
  |
  v
spec.APISpec (canonical format)
  |
  v
internal/generator/generator.go (New + Generate)
  profiles API via internal/profiler/
  selects vision templates
  renders 30+ .tmpl files to output dir
  |
  v
Generated CLI project published at ~/printing-press/library/<name>/
  cmd/<name>-pp-cli/main.go
  cmd/<name>-mcp/main.go
  internal/cli/   (per-resource commands)
  internal/client/ (HTTP client)
  internal/store/  (SQLite, if profiled)
  |
  v
Quality gates (if --validate)
  go mod tidy -> go vet -> go build
  -> binary --help -> version -> doctor
```

### Flow 3: Optional Resumable Pipeline (`printing-press print`)

Use this only when you explicitly want on-disk phase seeds and resumable state:

1. `internal/cli/root.go` (`newPrintCmd`) calls `pipeline.Init()` with the API name
2. `pipeline.Init()` calls `DiscoverSpec()` which looks up the API in `catalog/` entries
3. A managed run is created under `~/printing-press/.runstate/<scope>/runs/<run-id>/`
4. Seeds are written into `pipeline/`, research artifacts into `research/`, and scorecard/dogfood evidence into `proofs/`
5. `state.json` tracks progress across sessions, and completed runs archive to `~/printing-press/manuscripts/<api>/<run-id>/`

### Flow 4: Docs-to-Spec (`--docs`)

When no spec file exists, `--docs https://api.example.com/docs` scrapes the docs page via `internal/docspec/`, extracts endpoints using regex (or LLM if available), and produces an `APISpec` that feeds into the standard generation path.

---

## Where Do I Start?

**Build the binary:**

```
go build -o ./printing-press ./cmd/printing-press
```

**Run tests:**

```
go test ./...
```

**Generate a CLI from a spec:**

```
./printing-press generate --spec testdata/petstore.yaml
```

**Common changes:**

- To add a new Go template for generated CLIs, create a `.tmpl` file in `internal/generator/templates/` and wire it into `generator.go`'s `Generate()` method.
- To add a new API to the catalog, create a YAML file in `catalog/` following the schema in `CLAUDE.md` (name, display_name, description, category, spec_url, spec_format, tier). It must pass `internal/catalog` validation.
- To add a new CLI subcommand to the printing press itself, add a `newXxxCmd()` function in `internal/cli/` and register it with `rootCmd.AddCommand()` in `root.go`.
- To support a new spec format, implement a parser that returns `*spec.APISpec` and add format detection in `newGenerateCmd()`.
- To add a new domain archetype, add the constant in `internal/profiler/profiler.go`, add resource keywords to `detectDomainSignals()`, and create workflow/insight templates in `internal/generator/templates/`.

**Key files to start with:**

| Area | File | Why |
|------|------|-----|
| CLI commands | `internal/cli/root.go` | All subcommands are wired here |
| Spec format | `internal/spec/spec.go` | The `APISpec` struct everything revolves around |
| Code generation | `internal/generator/generator.go` | Template rendering and output structure |
| API profiling | `internal/profiler/profiler.go` | Domain detection and feature recommendation |
| Pipeline orchestration | `internal/pipeline/state.go` | Phase tracking and state machine |
| OpenAPI parsing | `internal/openapi/parser.go` | The largest parser (52KB), converts OpenAPI to `APISpec` |

**Commit style:** Conventional commits -- `feat(scope):`, `fix(scope):`, `docs:`, `refactor:`.
