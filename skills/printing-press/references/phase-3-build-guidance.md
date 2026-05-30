# Phase 3: Build-guidance body — starter templates, RunE skeletons, NULL-safe scans, hand-edit durability

Lazy-loaded body for hand-building Priority 2 (transcendence) novel commands after the Phase 3 Completion Gate. Read and apply when writing novel Go commands. Covers the generator-emitted helpers, the Cobra wrapper + three RunE skeletons (API-call, parallel-fetch aggregation, store-query), NULL-safe SQL scanning, and regen-mergeable hand-edits.

**Starter templates for novel commands.** Cobra wiring is mechanical and consistent across novel features; the actual feature work lives in the RunE body. Copy the wrapper below and one of the RunE skeletons that follows, fill in the placeholders from the absorb manifest's transcendence row (`Name`, `Command`, `Description`, `Example`, `WhyItMatters`), and replace the body comments with your implementation. Dogfood, verify, and scorecard still apply to the result — the templates raise the floor without changing what shipcheck checks.

**Helpers already emitted by the generator.** Do not reinvent these helpers in novel command files. They live in `internal/cli/helpers.go` after generation and are available to every hand-written command in package `cli`:

- `printJSONFiltered(w io.Writer, v any, flags *rootFlags) error` - apply `--select`, `--compact`, `--csv`, and `--quiet` while writing JSON from a Go value.
- `printAutoTable(w io.Writer, items []map[string]any) error` - render JSON-like rows as the generated human table format.
- `defaultDBPath(name string) string` - resolve the local SQLite database path for `<name>`.
- `dryRunOK(flags *rootFlags) bool` - detect verify-friendly `--dry-run` short-circuits before network, store, or filesystem work.
- `filterFields(data json.RawMessage, fields string) json.RawMessage` - apply `--select` to a JSON blob.
- `compactFields(data json.RawMessage) json.RawMessage` - apply `--compact` to a JSON blob.
- `isTerminal(w io.Writer) bool` - detect terminal output versus pipes.
- `wantsHumanTable(w io.Writer, flags *rootFlags) bool` - detect when output should use the generated human table instead of machine JSON.

```go
// internal/cli/<command>.go — replace <command> with the kebab leaf
// of NovelFeature.Command (e.g., "issues stale" → "issues_stale.go").
package cli

import (
	"github.com/spf13/cobra"
	// add: "encoding/json", "fmt", "<module>/internal/store", etc. as needed
)

func newXxxCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "<leaf-of-Command>",                    // e.g. "stale" for "issues stale"
		Short:   "<NovelFeature.Description, one line>", // truncate to ~70 chars
		Long:    "<optional: manifest Long Description, or Description + WhyItMatters>", // omit if Short is enough
		Example: "  <cli>-pp-cli <Command> --json",       // from NovelFeature.Example
		Annotations: map[string]string{
			// Set "mcp:read-only": "true" only when the command does NOT mutate
			// external state (lookups, comparisons, aggregations, render views).
			// Omit the whole map for commands that mutate (post, delete, write file).
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Pick the matching RunE skeleton below (API-call or store-query),
			// then implement the feature-specific path/query/parsing/formatting.
			return nil
		},
	}
	// cmd.Flags().StringVar(...) — add flags from the planned --flag list, if any
	return cmd
}

// Multi-word Commands like "issues stale": this constructor is registered as
// a child of the matching spec-resource parent (newIssuesCmd) — wire the
// AddCommand call inside root.go via local-variable capture:
//   issuesCmd := newIssuesCmd(flags)
//   issuesCmd.AddCommand(newIssuesStaleCmd(flags))
//   rootCmd.AddCommand(issuesCmd)
// Leaf commands must declare every non-root flag used in their examples.
// Use kebab-case flag names, such as --max-age instead of --maxAge, so the
// generated CLI convention and verify-skill flag scanner stay aligned.
// Do not rely on parent-local flags like --org or --project being accepted by
// child commands unless the parent registered them with PersistentFlags().
// Single-word Commands register directly: rootCmd.AddCommand(newXxxCmd(flags)).
```

**RunE skeleton — API-call shape** (live data via the generated client):

```go
RunE: func(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && cmd.Flags().NFlag() == 0 {
		return cmd.Help()
	}
	if dryRunOK(flags) {
		fmt.Fprintln(cmd.OutOrStdout(), "would fetch <resource>")
		return nil
	}
	if <required input missing> {
		_ = cmd.Usage()
		return usageErr(fmt.Errorf("<flag-or-arg> is required"))
	}
	c, err := flags.newClient()
	if err != nil {
		return err
	}
	// Replace path with the absorbed endpoint or hand-rolled URL. Use
	// cliutil.FanoutRun for any --site/--source/--region CSV fan-out;
	// re-implementing fanout inline is the recipe-goat silent-drop bug.
	data, err := c.Get("/api/v1/path", nil)
	if err != nil {
		return fmt.Errorf("fetching <resource>: %w", err)
	}
	// If the API returns CSV (`response_format: csv` in any spec endpoint),
	// wrap raw client data with cliutil.ParseCSV(data) before embedding it in a JSON envelope.
	// Parse data into your feature's view. Use cliutil.CleanText for any
	// text extracted from HTML or schema.org JSON-LD; re-implementing
	// HTML-entity unescape inline is the &#39; bug class.
	var view yourViewType // = parse(data)
	if flags.asJSON || (!isTerminal(cmd.OutOrStdout()) && !humanFriendly) {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	}
	// Human/terminal output (table or pretty print).
	return nil
},
```

**RunE skeleton — parallel-fetch aggregation shape** (live fan-out with partial-failure accounting):

Use this shape when a novel command fetches multiple items concurrently and computes a rollup, average, comparison, digest, or cross-source merge. The key invariant is that `err` travels with each result until aggregation, and error-tagged entries are excluded from all totals and denominators.

```go
RunE: func(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && cmd.Flags().NFlag() == 0 {
		return cmd.Help()
	}
	if dryRunOK(flags) {
		fmt.Fprintln(cmd.OutOrStdout(), "would fetch <resource> details")
		return nil
	}
	if <required input missing> {
		_ = cmd.Usage()
		return usageErr(fmt.Errorf("<flag-or-arg> is required"))
	}
	c, err := flags.newClient()
	if err != nil {
		return err
	}
	type fetchResult struct {
		idx   int
		id    string
		entry yourEntryType
		err   error
	}
	ids := []string{} // derive from args, flags, or an initial list endpoint
	results := make(chan fetchResult, len(ids))
	var wg sync.WaitGroup
	for idx, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := c.Get("/api/v1/resource/"+url.PathEscape(id), nil)
			if err != nil {
				results <- fetchResult{idx: idx, id: id, err: err}
				return
			}
			entry, err := parseEntry(data)
			results <- fetchResult{idx: idx, id: id, entry: entry, err: err}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	ordered := make([]yourEntryType, len(ids))
	fetchErrors := make([]error, len(ids))
	for r := range results {
		ordered[r.idx] = r.entry
		if r.err != nil {
			fetchErrors[r.idx] = r.err
		}
	}
	failures := make([]fetchFailure, 0)        // empty marshals as [] not null
	successfulItems := make([]yourEntryType, 0) // empty marshals as [] not null
	var total float64
	var denominator int
	for idx, entry := range ordered {
		if fetchErrors[idx] != nil {
			failures = append(failures, fetchFailure{
				ID:    ids[idx],
				Error: fetchErrors[idx].Error(),
			})
			continue
		}
		successfulItems = append(successfulItems, entry)
		total += entry.Metric
		denominator++
	}
	if len(failures) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d of %d fetches failed; averages computed over the remaining %d items\n", len(failures), len(ids), denominator)
	}
	view := yourAggregateView{
		Items:         successfulItems,
		AverageMetric: safeAverage(total, denominator),
		FetchFailures: failures, // json tag: `json:"fetch_failures,omitempty"`
	}
	if flags.asJSON || (!isTerminal(cmd.OutOrStdout()) && !humanFriendly) {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	}
	// Human/terminal output, including a visible partial-failure note.
	for _, entry := range view.Items {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%.2f\n", entry.Name, entry.Metric)
	}
	if len(failures) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "\npartial results: %d of %d fetches failed; average computed over %d items\n", len(failures), len(ids), denominator)
	}
	return nil
},
```

**RunE skeleton — store-query shape** (offline data via the local SQLite):

The generic `resources` table is keyed by `resource_type`. Flat resources synced from `/<resource>` land as `resource_type='<resource>'`. **Hierarchical resources** synced from `/<parents>/{id}/<resource>` land as `resource_type='<parent>_<resource>'` — e.g., `projects_tasks` (Asana), `repos_issues` / `repos_pulls` (GitHub) — *not* the bare `<resource>` name. A novel feature that filters by the bare name returns zero rows against a real DB. Use `IN (...)` to catch both shapes so the same code works whether the API exposes the resource flat or only parent-scoped.

```go
// Declare these alongside the cmd literal, before return cmd:
//   var dbPath string
//   cmd.Flags().StringVar(&dbPath, "db", "", "Database path")

RunE: func(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && cmd.Flags().NFlag() == 0 {
		return cmd.Help()
	}
	if dryRunOK(flags) {
		fmt.Fprintln(cmd.OutOrStdout(), "would query local store")
		return nil
	}
	if <required input missing> {
		_ = cmd.Usage()
		return usageErr(fmt.Errorf("<flag-or-arg> is required"))
	}
	if dbPath == "" {
		dbPath = defaultDBPath("<cli>-pp-cli") // replace <cli> with the API slug
	}
	db, err := store.OpenWithContext(cmd.Context(), dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()
	// Filter resources by both the flat and hierarchical naming so the
	// query catches rows synced via /<resource> AND rows synced via
	// /<parents>/{id}/<children>. Drop the parent-scoped entry if the
	// API only exposes the resource flat; add a <resource_singular>
	// entry for APIs that toggle plural/singular casing. SQL must be
	// SELECT-only; the search/sql gates reject mutating statements.
	rows, err := db.DB().QueryContext(cmd.Context(), `
		SELECT id, data FROM resources
		WHERE resource_type IN ('<resource>', '<parent>_<resource>')
		  AND ...`)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer rows.Close()
	// Scan each row. id/data on the resources table are NOT NULL so bare
	// strings are safe; ANY optional field selected via json_extract or
	// pulled from a typed FTS/upsert table can be NULL — use sql.Null*
	// scan targets (or COALESCE in the SQL) for those, see the NULL-safe
	// scans paragraph below.
	results := make([]yourRowType, 0) // scan rows into this slice; make([]T, 0) keeps empty JSON as [] not null
	// (loop over rows here: results = append(results, scannedRow))
	if flags.asJSON || (!isTerminal(cmd.OutOrStdout()) && !humanFriendly) {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}
	// Human/terminal output.
	return nil
},
```

For flat-only resources, the typed FTS/upsert tables the generator emits (e.g., `tasks_fts`, `projects`) work too — `SELECT id, data FROM <typed-table>` is the fast path. The `IN (...)` pattern above is the safe default whenever the resource may be hierarchical; `cli-printing-press dogfood --json` shows the actual `resource_type` distribution so you can confirm without running raw SQL.

For features that combine both (cache an API response in the store, or fall through to live when the local store is stale), nest one skeleton inside the other and use the `--data-source auto/local/live` flag pattern from the generated `sync` command.

**Shared helpers available to novel code:** The generator emits `internal/cliutil/` in every CLI. When authoring novel commands, prefer `cliutil.FanoutRun` for any aggregation command (any `--site`/`--source`/`--region` CSV fan-out) and `cliutil.CleanText` for any text extracted from HTML or schema.org JSON-LD. Re-implementing these inline is how recipe-goat's trending silent-drop and `&#39;` entity bugs shipped.

**Hand-coded duration flags MUST use `cliutil.ParseDurationLoose` with a `StringVar` flag (not `DurationVar`).** Go's `time.ParseDuration` rejects the `7d`/`30d`/`1w`/`4w` day/week shorthand that the framework's `sync --since` already accepts, so a `DurationVar` flag fails at runtime on input agents and users reasonably expect. Declare the flag as a `StringVar`, then post-parse with `cliutil.ParseDurationLoose`, which adds `d`/`w` suffix support and otherwise defers to `time.ParseDuration`.

**OData v3 datetime fields MUST be decoded with `cliutil.ParseODataDate`.** OData v3 APIs (Exact Online, Microsoft Dynamics 365 Business Central, Dynamics NAV) return dates as `/Date(1715731200000)/` string literals that no standard parser accepts, so the raw value passes straight through to JSON output and agents cannot parse `created_at`/`due_date`. `cliutil.ParseODataDate(s) (time.Time, bool)` decodes the literal to a UTC `time.Time` and falls back to RFC3339, so callers need not dispatch on format. Re-implementing this inline per command is how the same regex ships inconsistently across OData CLIs.

**Streaming frame normalizers MUST use `cliutil.ExtractNumber` / `cliutil.ExtractInt` rather than raw `float64`/`int64` struct fields.** Real-world WebSocket and streaming JSON feeds (Binance, Coinbase, Kraken, Stripe `*_decimal`, vendor-specific market-data feeds) commonly encode numeric values as JSON-encoded strings (`"price":"1.91"`). `json.Unmarshal` of a JSON string into a `float64` field returns no error and silently leaves the field at 0; combined with NULL-on-zero patterns this discards the entire numeric feed with no error signal anywhere in the pipeline. The helpers accept both shapes (JSON number or JSON-encoded string), report `ok=false` on missing/null/unparseable, and are the canonical extraction path for `map[string]json.RawMessage` decoders. Re-implementing this inline as a `float64` struct field is the silent-aggregation-failure bug class.

**WebSocket-primary APIs SHOULD declare `streaming:` and use the generated live scaffold.** When the API's facts arrive over WebSocket and REST supplies metadata, follow `references/ws-primary-pattern.md`. Do not reimplement dial/subscribe/reconnect, newline-delimited JSON splitting, metadata status polling, or rebase-log writes in novel code unless the API genuinely breaks the generated lifecycle contract.

**NULL-safe SQL scans MUST use `sql.Null*` scan targets (or `COALESCE(<col>, <zero>)` in the query) for any column that can be NULL.** SQLite returns NULL for any absent JSON field selected via `json_extract(data, '$.optional_field')`, for any nullable column in a typed FTS/upsert table the generator emits, and for any field the API omits from a particular response. `database/sql`'s `rows.Scan` into a bare `string`/`int64`/`float64` returns a non-nil error on NULL (`Scan error on column index N: converting NULL to string is unsupported`) — and the surrounding `for rows.Next()` loop typically `continue`s on scan error, silently dropping every row. The result: queries return zero records, no error reaches the caller, the feature looks healthy because the API call succeeded. Use `var v sql.NullString` (or `NullInt64` / `NullFloat64` / `NullTime`) as the scan target and copy `.String` / `.Int64` / `.Float64` / `.Time` into your row struct, accepting the zero value as the missing-field representation. Re-implementing this inline as bare-string scans is the silent-row-drop bug class.

```go
// Wrong — every NULL column kills the row.
var name string
if err := rows.Scan(&id, &name); err != nil { continue }

// Right — NULL becomes the zero value, no row is lost.
var name sql.NullString
if err := rows.Scan(&id, &name); err != nil { continue }
result.Name = name.String
```

Also right: push the default into the query so the scan target stays bare.

```sql
SELECT id, COALESCE(json_extract(data, '$.name'), '') FROM resources WHERE ...
```

**Typed exit-code verification:** If a novel command intentionally returns a non-zero code for a non-error control-flow result, add `cmd.Annotations["pp:typed-exit-codes"] = "0,<code>"` (or the equivalent `Annotations: map[string]string{...}` literal) and document the same command-specific codes in its help. Do not list the global failure palette in command help unless those exits should count as a verify pass for that command; keep general exit-code troubleshooting in README/SKILL prose.

**Dogfood error-path opt-out:** If a real API returns HTTP 200 plus an empty success envelope for unknown IDs, and the command cannot distinguish bad input from a valid empty result without inventing API-specific semantics, annotate the Cobra command with `cmd.Annotations["pp:no-error-path-probe"] = "true"`. Dogfood will still run help, happy-path, and JSON-fidelity checks, but it will skip `error_path` with reason `no-error-path-probe annotation`. Do not add local "empty means not found" heuristics only to satisfy dogfood unless the upstream API contract actually defines that as an error.

<a id="hand-edit-durability"></a>
**Hand-edits must be regen-mergeable.** `cli-printing-press generate --force` snapshots the existing tree, emits a fresh tree, then runs the same AST-aware reconciliation used by `cli-printing-press regen-merge`. Whole hand-authored files and lost `AddCommand` wiring are preserved automatically; straightforward hand-edits to generated Go files (added declarations, literal drift, body drift) are classified and carried forward when the merge can do so safely. For risky edits, use the standalone `regen-merge` command first when you want a previewable report before applying.

For an extension to be durable, put it in its own file beside the emitted one:

- **Custom config fields:** create `internal/config/<api>_config.go` exporting accessors your novel code reads directly. Do not add fields to the emitted `Config` struct.
- **Custom request headers** (vendor fingerprint, `X-CSRF`, app-version, signed timestamps): create `internal/client/<api>_headers.go` exporting a func that builds the header map; novel code passes that map to `client.GetWithHeaders` / `PostWithHeaders` when it calls the API. The generated `client.go` has no global request mutator, so this pattern only covers requests made directly from novel code — it does not intercept calls from generated endpoint commands. Do not edit the templated header block in `client.go`.
- **Custom auth flow** (browser-sniffed sessions, vendor SSO, refresh hooks beyond OAuth2): create `internal/cli/<api>_auth.go` (package `cli`, same as the generated `auth.go`) with the API-specific token capture or refresh, and wire it from a novel command rather than editing the templated `auth.go` constructor functions (`newAuthLoginCmd`, `newAuthSetupCmd`, etc.).
- **Extended store schema** (typed tables beyond `resources`, vendor JSON columns, full-text indexes): create `internal/store/<api>_migrations.go` running its own `CREATE TABLE ... IF NOT EXISTS` from a lazy init invoked by the novel commands that need it. Do not edit the migration slice in `store.go`.
- **New novel command:** put the command body in its own `internal/cli/<feature>.go` file — it survives regen as a whole hand-authored unit. The `AddCommand` call wiring it into the Cobra tree still goes in `root.go` per the Phase 3 novel-command skeleton above; `cli-printing-press generate --force` re-injects it via the lost-registration merge path. Use standalone `regen-merge` when you want to inspect the merge report before applying. Spec-declared commands are picked up by the generator's typed-tool path and need no hand-wired `AddCommand` at all.

If an extension genuinely cannot live in a separate file (a `case` branch in a templated method switch, an inline modification to a generated handler with no registry hook), file a generator issue requesting the hook rather than depending on repeated conflict-prone merges. The `AddCommand` case above is covered by the merge path.

**MCP exposure:** The generator emits `internal/mcp/cobratree/`, and the MCP binary mirrors the Cobra tree at startup. When you add, rename, or remove a user-facing Cobra command, the MCP surface follows automatically. Two annotations control how each command appears as an MCP tool:

- `cmd.Annotations["mcp:hidden"] = "true"` — exclude the command from the MCP surface entirely. Use only for debug/internal commands that should not become agent tools.
- `cmd.Annotations["mcp:read-only"] = "true"` — declare that this command does not modify external state. The MCP server attaches `readOnlyHint: true` to the resulting tool, so hosts like Claude Desktop don't bucket it under "write/delete tools" and demand permission per call. Apply this to every novel command whose only effect is reading from the API or the local store: lookups, comparisons, aggregations, render-only views, status checks. Skip it for commands that mutate external state (orders, posts, deletes) or write to user-visible files outside the local cache.

Endpoint-mirror tools the generator emits from the spec already get the right annotations automatically (`GET` → read-only, `DELETE` → destructive, etc.) — `mcp:read-only` is only needed on hand-authored Cobra commands the spec doesn't cover.

Do not rationalize skipping transcendence features because "the CLI already works for live API interaction." The absorb manifest was approved by the user. Build what was approved.

