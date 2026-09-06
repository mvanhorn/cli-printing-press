## 11-build-the-goat (Phase 3: Build The GOAT)

**Receipt entry (required):**

```bash
"$PRINTING_PRESS_BIN" phase-receipt enter --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "11-build-the-goat"
```

<!-- CODEX_PHASE3_START -->
When `CODEX_MODE` is true, read [references/codex-delegation.md](../references/codex-delegation.md)
for the delegation pattern, task type templates, and circuit breaker logic.

When `CODEX_MODE` is false, skip this section.
<!-- CODEX_PHASE3_END -->

Build comprehensively. The absorb manifest from [Phase 1.5](08-ecosystem-absorb-gate.md) IS the feature list.

**First Phase 3 build-log line:** Before writing code, count the shipping-scope transcendence rows in the [Phase 1.5](08-ecosystem-absorb-gate.md) absorb manifest and write this as the first line of `$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-build-log.md`:

```text
Manifest transcendence rows: <planned> planned, 0 built. Phase 3 will not pass until all <planned> ship.
```

Use only rows that Phase 3 is expected to build: include approved transcendence rows with concrete `Command` values, exclude rows whose implementation starts with `(stub)`, and keep `spec-emits` rows out of the hand-code count while still tracking whether their approved command path exists. Update the build log's built count as rows are completed. If `PRIOR_SUB60_REPRINT=true`, this line is also the strict-gate budget: partial transcendence coverage is a hold by default.

**macOS framework access:** When the plan or manifest specifies macOS framework APIs (ScreenCaptureKit, CoreGraphics, CoreAudio, Vision, Shortcuts, etc.), use the Swift subprocess bridge pattern - Go shells out to `swift -e '<inline script>'`. Swift is always available with Xcode CLT. Do NOT attempt Python+PyObjC - it requires separate installation and is unreliable across Python distributions. Reference `agent-capture-pp-cli/internal/capture/cgwindow.go` as the canonical example of this pattern.

Priority 0 (foundation):
- data layer for ALL primary entities from the manifest
- sync/search/SQL path - this is what makes transcendence possible

After completing Priority 0, update the lock heartbeat:
```bash
cli-printing-press lock update --cli <api>-pp-cli --phase build-p0
```

Priority 1 (absorb - match everything):
- ALL absorbed features from the [Phase 1.5](08-ecosystem-absorb-gate.md) manifest
- Every feature from every competing tool, matched and beaten with agent-native output
- This is NOT "top 3-5" - it is the FULL manifest

**Lock heartbeat rule for long priority levels:** If Priority 1 has more than 5 features, update the lock heartbeat after every 3-5 features to prevent the 30-minute staleness threshold from triggering mid-build:
```bash
cli-printing-press lock update --cli <api>-pp-cli --phase build-p1-progress
```

Priority 2 (transcend - build what nobody else has):
- ALL transcendence features from [Phase 1.5](08-ecosystem-absorb-gate.md)
- The NOI commands that only work because everything is in SQLite
- These are the commands that make someone say "I need this"

**Lock heartbeat rule for Priority 2:** Same rule as Priority 1 — if Priority 2 has more than 3 transcendence features, update the heartbeat after every 2-3 features:
```bash
cli-printing-press lock update --cli <api>-pp-cli --phase build-p2-progress
```

After completing Priority 2, update the lock heartbeat:
```bash
cli-printing-press lock update --cli <api>-pp-cli --phase build-p2
```

Priority 3 (polish):
- skipped complex request bodies that block important commands
- naming cleanup for ugly operationId-derived commands
- tests for non-trivial store/workflow logic
- enrich terse flag descriptions: review generated command flags. If any description is under 5 words or is generic spec-derived text (e.g., "access key", "The player"), improve it using the research brief. For example, change "access key" to "Steam API key (get one at steamcommunity.com/dev/apikey)". Focus on auth keys, IDs, and filter parameters.

### Execution Context Budget (slicing rule)

Phase 3's instructions are small; its execution is not. On a 35+ command CLI the build
conversation alone can pass 300k tokens, roughly 2x the zone where instruction-following
stays reliable, and quality degrades silently long before anything crashes.

- **Budget per agent/segment: ~150-200k tokens.** Plan the build as slices that each fit
  the budget before writing the first line of code.
- **Canonical slice order:** (1) foundation — store/data layer, auth and config plumbing,
  shared helpers; (2) core engine — the CLI's central workflow commands; (3) analytics
  and read-side commands; (4) ingest/import, report/export, and phase close (completion
  gate, build-log finalization).
- **Every slice ends build-green.** `go build ./...` and `go vet ./...` must pass before
  a slice hands off. A red build is not a slice boundary.
- **Handoff is an artifact, not shared context.** Close each slice by appending a
  build-log entry: manifest rows completed, files touched, decisions made, next slice
  scope. The next slice starts from the build log and the absorb manifest, never from a
  summary of the previous conversation.
- Orchestrated runs assign one subagent per slice. Single-session runs use the same
  boundaries as checkpoints.

### Agent Build Checklist (per command)

After building each command in Priority 1 and Priority 2, verify these 13 principles are met. These map 1:1 to what [Phase 4.9](15-readme-skill-agents-correctness-audit.md)'s agent readiness reviewer will check - apply them now so the review becomes a confirmation, not a catch-all.

1. **Non-interactive**: No TTY prompts, no `bufio.Scanner(os.Stdin)`, works in CI without a terminal
2. **Structured output**: `--json` produces valid JSON, `--select` filters fields correctly. Hand-written novel commands that build a Go-typed slice/struct and emit JSON should use the generated receiver-style helper, `flags.printJSON(cmd, v)`, or call `printJSONFiltered(cmd.OutOrStdout(), v, flags)` directly. Both route through `printOutputWithFlags`, picking up `--select`, `--compact`, `--csv`, and `--quiet` for free. `--agent` envelopes take `meta.source` from `cmd.Annotations["pp:data-source"]` (or an existing provenance envelope); live-annotated commands must set that annotation to `live` so agents do not treat network data as local. Verify with `<cli> <novel> --json --select <field> | jq 'keys'` returning only the requested fields.
3. **Progressive help**: `--help` shows realistic examples with domain-specific values (not "abc123"). **Use `Example: strings.Trim(\`...\`, "\n")` (preserves leading 2-space indent) NOT `strings.TrimSpace(\`...\`)` (strips it).** TrimSpace makes the first example line unindented; dogfood's example-detection parser is tolerant of this in current versions, but the indented form renders correctly across every Cobra version and is the convention used by every generated command.
4. **Actionable errors**: Error messages name the specific flag/arg that's wrong and the correct usage
5. **Safe retries**: Mutation commands support `--dry-run`, idempotent where possible
6. **Composability**: Exit codes are typed (0/2/3/4/5/6/7/10 as applicable), output pipes to `jq` cleanly
7. **Bounded responses**: `--compact` returns only high-gravity fields, list commands have `--limit`
8. **Verify-friendly RunE**: Hand-written commands MUST NOT use `Args: cobra.MinimumNArgs(N)` or `MarkFlagRequired(...)`. Cobra evaluates both before RunE runs, so a `--dry-run` guard inside RunE cannot reach if those gates fail. Verify probes commands with `--dry-run` and expects exit 0; commands with hard arg/flag gates fail those probes. Instead: validate inside RunE, fall through to `cmd.Help()` only for unambiguous help-only invocations (no args and no flags), short-circuit on `dryRunOK(flags)` before any IO, and return `usageErr(...)` with exit 2 when required input is missing in real mode.
   - **Use string for "positional OR flag" commands**: when a command accepts a positional `<x>` OR a flag `--y` as alternatives (e.g., `snapshot <co>` or `snapshot --domain example.com`), declare `Use: "<cmd> [x]"` with **square brackets** (optional), not `<x>` (required). Validate "exactly one of x or --y" inside RunE. Required positionals declared with angle brackets break verify-skill recipes that use the flag-only form.
   - **Declare verifier fixture inputs when generic values are not enough**: if the command needs realistic positional values or required flags to pass the verifier's happy path, add `Annotations: map[string]string{"pp:happy-args": "<item>=example-id;--query=example;--dry-run"}` or assign a whole initialized `cmd.Annotations` map after construction. The verifier consumes tokens separated by unescaped semicolons in order: escape a literal semicolon as `\;`. Positional tokens are `label=value` pairs — the label is discarded and the value becomes the positional, so `<item>=example-id` and `item=example-id` behave identically. **Bare positional values without `=` are silently ignored**, so write `item=example-id`, never just `example-id`; a missing `=` produces no positional fixture and the happy path silently skips. `--flag=value` tokens replace matching Example flags or add new flag/value pairs; bare `--flag` tokens are treated as boolean `--flag=true`. Negative numeric flag values use the `--flag=-12.3` form. Commands without the annotation keep the generic synthesized inputs.
   - **Local-store novel commands also need `pp:typed-exit-codes` to pass publish.** Publish's phase5 gate rejects novel features whose live-dogfood happy paths were skipped ("hollow coverage"). A local-only command (e.g. `entity report`, `monitor diff`) whose happy path returns a graceful not-found exit must declare both `Annotations: map[string]string{"pp:happy-args": "<name>=example", "pp:typed-exit-codes": "0,3"}` — the typed codes let the live matrix count the graceful-empty exit as a pass, and the happy-args give the matrix a real positional. Without both, `publish validate` fails with "phase5 acceptance has hollow coverage" and the whole publish loop restarts.
9. **Side-effect commands refuse under every harness**: Any hand-written command that performs a visible or physical side effect (opens a browser tab, sends a notification, plays audio, dials out to an OS handler, preheats hardware, boils water, writes a physical board) MUST follow this convention:
   - **Print by default; opt in to the action.** The default behavior prints what would happen (`would launch: <url>`); a flag like `--launch` / `--send` / `--play` is required to actually do it. food52's `open` command is the reference shape — see `internal/cli/open.go` after retro #337.
   - **Refuse when `cliutil.IsAnyHarness()` returns true.** The helper covers both `PRINTING_PRESS_VERIFY=1` and `PRINTING_PRESS_DOGFOOD=1`; commands that ignore dogfood can reach real people and hardware during a live matrix even with the print-by-default flag pattern. Commands reaching hardware a person can hear or see MUST refuse under every harness, not curtail. Curtailing makes the effect shorter, not absent. The helper is generated into every CLI's `internal/cliutil/verifyenv.go`; use `writeHarnessRefusal(cmd.OutOrStdout(), flags, "<action>")` or an equivalent structured response so `--json` and `--agent` callers receive JSON. Pattern:
     ```go
     if cliutil.IsAnyHarness() {
         return writeHarnessRefusal(cmd.OutOrStdout(), flags, "launch browser")
     }
     ```
   This is defense-in-depth: the verifier also runs a heuristic side-effect classifier, but it can miss commands whose `--help` text and source don't match the heuristics. The env-var check is the floor.
   - **Long-running read commands curtail work under live-dogfood.** Any hand-written command whose happy path is an expensive read/network operation (full sync loops, content crawlers, bulk archive walks) MUST check `cliutil.IsDogfoodEnv()` and curtail work to fit inside the matrix's flat 30s per-command timeout. `cli-printing-press dogfood --live` sets `PRINTING_PRESS_DOGFOOD=1` in every subprocess. Pattern:
     ```go
     if cliutil.IsDogfoodEnv() {
         return crawl(ctx, opts.WithMaxPages(1))
     }
     ```
     Distinct from `IsAnyHarness`: dogfood is a real-API matrix for reads, so curtail read work (paginate once, smaller `--limit`), never substitute mock data for real calls. Read-only commands under `PRINTING_PRESS_DOGFOOD=1` must still hit the network.
10. **Per-source rate limiting**: any hand-written client in a sibling internal package (`internal/source/<name>/`, `internal/recipes/`, `internal/phgraphql/`, etc. — anything not generator-emitted) that makes outbound HTTP calls MUST use `cliutil.AdaptiveLimiter` and surface `*cliutil.RateLimitError` when 429 retries are exhausted. Empty-on-throttle is indistinguishable from "no data exists" and silently corrupts downstream queries. Read [references/per-source-rate-limiting.md](../references/per-source-rate-limiting.md) when authoring a sibling client. Enforced at generation time by dogfood's `source_client_check`.
11. **Per-command timeout boundary**: Hand-written novel commands that call a sibling typed HTTP client (`internal/<api>/`, `internal/source/<name>/`, `internal/recipes/`, etc.) MUST wrap `cmd.Context()` with `boundCtx(cmd.Context(), flags)` and `defer cancel()` before the first client call. Generated endpoint commands already pass `flags.timeout` into `client.New` for ordinary JSON calls. `response_format: binary` transfers skip that whole-call `http.Client.Timeout` unless the operator set `--timeout` explicitly (a run profile that supplies `timeout` counts); header stalls still use the configured budget via `ResponseHeaderTimeout`. Sibling clients do not get this wiring. Without the `boundCtx` boundary, root `--timeout` is advertised in help but does not bound wide scans, crawlers, or fan-out loops. Do not wrap a generated-client binary download in `boundCtx` just to "honor --timeout" — that reintroduces the mid-flight kill. Hand-built bulk transfers that do not set `X-Printing-Press-Binary-Response` should use `client.StreamingHTTPClient`.
12. **Parallel-fetch partial failures**: any command that fans out N API calls and computes an aggregate (averages, rollups, comparisons, cross-source merges, digest summaries) MUST preserve each fetch error through the result channel and exclude error-tagged entries from totals and denominators. Failed fetches may still appear in the response so the caller can see the gap, but they must not become zero-valued phantom rows that dilute averages or counts. Surface the partial failure explicitly with:
   - a stderr warning that names the failed count and the actual aggregation denominator, for example `warning: 2 of 10 fetches failed; averages computed over the remaining 8 items`
   - a `fetch_failures` field in the JSON response envelope listing the failed entries and error messages

Silently averaging phantom zeros is worse than reporting a partial result.
13. **Scan-and-filter caps**: any hand-written transcendence command that scans
    a paginated or otherwise unsorted endpoint, filters locally, and then keeps
    matching rows MUST bound scan effort separately from output size. This is the
    "list, filter locally, fan out to detail" shape: the API cannot filter on the
    dimension the command needs, so the command pages through broad results and
    applies the real predicate in Go. `--limit` is not enough because it bounds
    matches kept, not records scanned.

Required elements for every scan-and-filter command:

1. **`--max-scan-pages int`**, or a unit-specific equivalent such as
   `--max-scan-batches` / `--max-scan-records`, with a conservative default.
   Five pages is a reasonable starting point for typical paginated APIs
   (~250 records at 50/page). Lower it under `cliutil.IsDogfoodEnv()` when the
   happy path would otherwise risk the live-dogfood 30s timeout.
2. **`scanned_<unit>` in the JSON envelope**, for example `scanned_orders` or
   `scanned_issues`, so downstream agents can tell whether an empty result
   examined 20 records or 2,000.
3. **`note` in zero-match JSON output**, explaining that the scan cap was hit
   without finding a match and naming the flag that widens the search.
4. **Clear separation between output and scan caps**: `--limit` controls how
   many matches are returned; `--max-scan-pages` controls how many list pages
   or records the command is allowed to examine.

Use this pattern when the endpoint ordering is unrelated to the local predicate:
search-by-property over relevance-ranked search results, issues by a weakly
server-filtered custom field, pull requests by reviewer from an endpoint with
no reviewer filter, rental orders by date from a broad order list, and similar
cases.

```go
type scanFilterView struct {
	Items         []yourEntryType `json:"items"`
	ScannedItems  int             `json:"scanned_items"`
	MaxScanPages  int             `json:"max_scan_pages"`
	Note          string          `json:"note,omitempty"`
}

func newScanFilterCmd(flags *rootFlags) *cobra.Command {
	var limit int
	var maxScanPages int
	var status string
	cmd := &cobra.Command{
		Use:   "find-by-status",
		Short: "Find matching items by scanning the list endpoint",
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "scan")
			}
			if status == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--status is required"))
			}
			if cliutil.IsDogfoodEnv() && maxScanPages > 1 {
				maxScanPages = 1
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			var matches []yourEntryType
			scanned := 0
			scanCapHit := true
			for page := 1; page <= maxScanPages && len(matches) < limit; page++ {
				data, err := c.Get("/api/v1/items", map[string]string{
					"page":     strconv.Itoa(page),
					"pageSize": "50",
				})
				if err != nil {
					return fmt.Errorf("fetching items page %d: %w", page, err)
				}
				items, err := parseItems(data)
				if err != nil {
					return fmt.Errorf("parsing items page %d: %w", page, err)
				}
				for _, item := range items {
					scanned++
					if item.Status != status {
						continue
					}
					matches = append(matches, item)
					if len(matches) >= limit {
						break
					}
				}
				if len(items) == 0 {
					scanCapHit = false
					break
				}
			}
			view := scanFilterView{
				Items:         matches,
				ScannedItems:  scanned,
				MaxScanPages:  maxScanPages,
			}
			if len(matches) == 0 && scanCapHit {
				view.Note = fmt.Sprintf("scanned %d items across up to %d pages without finding status %q; raise --max-scan-pages to widen the search", scanned, maxScanPages, status)
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(view)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 10, "maximum matching items to return")
	cmd.Flags().IntVar(&maxScanPages, "max-scan-pages", 5, "maximum list pages to scan before returning partial or empty results")
	cmd.Flags().StringVar(&status, "status", "", "status to match")
	return cmd
}
```

#### Verify-friendly RunE template

Use this shape for every hand-written transcendence command. The generator emits the `dryRunOK` helper into `internal/cli/helpers.go`:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    if len(args) == 0 && cmd.Flags().NFlag() == 0 {
        return cmd.Help()
    }
    if dryRunOK(flags) {
        return writeDryRun(cmd.OutOrStdout(), flags, "<command name>")
    }
    if <required input missing> {
        _ = cmd.Usage()
        return usageErr(fmt.Errorf("<flag-or-arg> is required"))
    }
    // ... real work ...
}
```

Why each branch exists: the `len(args) == 0 && cmd.Flags().NFlag() == 0` branch handles an interactive `<cli> mycommand` help-only invocation without treating help as an error. The `dryRunOK` branch handles verify's `<cli> mycommand --dry-run` probes before required-flag validation, required positionals, network, or filesystem IO; it must end in `writeDryRun(cmd.OutOrStdout(), flags, "<command name>")` so `--dry-run --json` still produces a parseable envelope. Never `return nil` from this branch: empty stdout under `--json` fails dogfood `json_fidelity`. Generated novel-feature TODO scaffolds (`novel_feature_command.go.tmpl`) already emit this same `writeDryRun` short-circuit before positional or required-flag gates; keep that branch when replacing the TODO body. The required-input branch handles non-help invocations where a mode or output flag is present (`--no-input`, `--agent`, `--json`) but the required ID, query, path, or other command input is still missing. Missing required input must print usage and return `usageErr(...)` so callers get exit code 2 instead of a silent rc=0 skip.

For SQLite-backed novel commands only, add this missing-mirror guard after `dryRunOK(flags)`, after any required-input `usageErr(...)` check, and after `dbPath` is resolved, but before `store.OpenWithContext`, `store.OpenReadOnly`, `sql.Open`, or other SQLite access:

```go
if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
	fmt.Fprintf(cmd.ErrOrStderr(), "no local mirror at %s\nrun: <cli> sync --resources <resource> --db %s\n", dbPath, dbPath)
	if !wantsHumanTable(cmd.OutOrStdout(), flags) {
		return printJSONFiltered(cmd.OutOrStdout(), make([]yourRowType, 0), flags)
	}
	return nil
}
```

The missing-mirror branch covers a different probe layer from `dryRunOK`: live execution without `--dry-run`, before the user has run `sync`. Route every non-human format through `printJSONFiltered` so agents receive a valid empty result instead of a SQLite open failure; print a human hint to stderr that names the sync command needed to populate the mirror. The unconditional `return nil` is intentional for both machine and human paths: a missing local mirror is an empty local-cache state, not a usage or API failure. Do not add this branch to novel commands that call live API endpoints directly or do not use the local store.

Multi-positional commands (N >= 2 required args) must use a two-check shape so only the bare help probe returns exit 0:

```go
if len(args) == 0 && cmd.Flags().NFlag() == 0 {
	return cmd.Help() // bare invocation help probe
}
if dryRunOK(flags) {
	return writeDryRun(cmd.OutOrStdout(), flags, "<command name>")
}
if len(args) < N {
	_ = cmd.Usage()
	return usageErr(fmt.Errorf("missing required positional argument"))
}
```

This preserves verify-friendly help behavior for 0 args while making partial positional input (`1..N-1`) fail with exit 2 in dogfood `error_path`. Single-positional commands can keep the single required-input check. If a multi-positional command supports `--dry-run`, place its `dryRunOK(flags)` branch before the `len(args) < N` gate so `<cmd> --dry-run` short-circuits with a parseable envelope; a probe has neither the positionals nor any required file. Partial positionals without `--dry-run` still return `usageErr` and exit 2. Nothing that requires user input may run before the dry-run branch.

Do not collapse the first and third branches into `if len(args) == 0 || <flag empty> { return cmd.Help() }`. `cmd.Help()` returns `nil`, so agents and scripts cannot distinguish "help was requested" from "the command skipped required work."

For commands with no required inputs, omit the `usageErr(...)` branch entirely and keep the help-only plus dry-run branches.

If the command reads a file or directory (`os.ReadFile`, `os.ReadDir`, `os.Stat`, `os.Open`, `os.OpenFile`, `os.Lstat`, `filepath.Walk`, `filepath.WalkDir`, or any other filesystem access), the read MUST come after `dryRunOK()`, not before. Filesystem reads before `dryRunOK()` cause `validate-narrative --full-examples` to fail with a missing-file error rather than a clean dry-run exit 0.

### Phase 3 delegation: require feature-level acceptance

When Phase 3 implementation is delegated to a sub-agent (via `Agent` tool or Codex), the delegation prompt MUST require behavioral acceptance tests per major feature, not just "does the command build and run." Agents consistently over-report success when the contract is only "command executes without error."

Required in every Phase 3 delegation prompt:

1. **Per-feature acceptance assertions** that check output content, not just exit codes. Examples the prompt should make concrete:
   - Search/ranker: "After `<cli> goat 'brownies'`, assert at least 3 of the top 5 results contain 'brown' in their title or URL. If fewer, the extractor is broken."
   - Lookup: "After `<cli> sub buttermilk --json`, assert the parsed JSON is an array of objects with `substitute`, `ratio`, `context` fields."
   - Transform: "After `<cli> recipe get <known-url> --servings 6`, assert the output ingredient quantities differ from the `--servings 4` invocation (scaling actually ran)."
2. **Absence-of-correctness tests** for every feature whose correct answer can be empty or complete:
   - Calendar/window commands: "Given `--days N`, assert exactly N rows are returned, including zero-count days."
   - Drift/diff commands: "Given only one snapshot or no changed values, assert the command returns `[]` rather than fabricating drift."
   - Alert/watch commands: "Given no matching records, assert empty output plus an honest reason, not stale or unrelated data."
3. **Negative tests** per filter/search command: run with a deliberately-mismatching query and assert the result set does NOT contain irrelevant items.
4. **No parent-command delegation without flags.** If a parent command delegates to a leaf command's `RunE`, the parent must declare every flag the delegate accepts. Prefer group parents that show help over aliasing a parent to a child.
5. **Structured pass/fail report** in the agent's response (raw output of each assertion, not a summary).

A Phase 3 delegation that reports PASS without behavioral assertions is treated as untrusted — re-run acceptance tests before accepting the result.

### Search Dedup Rule

When building cross-entity search commands, use per-table FTS search methods individually. Do NOT combine per-table search with the generic `db.Search()` — this causes duplicate results because the same entities exist in both `resources_fts` and per-table FTS indexes.

### Priority 1 Review Gate

After completing ALL Priority 1 (absorbed) features, BEFORE starting Priority 2 (transcendence):

Pick 3 random commands from Priority 1. Run each with:
```bash
<cli> <command> --help          # Does it show realistic examples?
<cli> <command> --dry-run       # Does it show the request without sending?
<cli> <command> --json          # Does it produce valid JSON?
```

If any of the 3 fail, there's a systemic issue. Fix it across all commands before proceeding. This catches problems like "--dry-run not wired" or "--json outputs table instead of JSON" early, when they're cheap to fix.

After passing the Priority 1 Review Gate, update the lock heartbeat:
```bash
cli-printing-press lock update --cli <api>-pp-cli --phase build-p1
```

Get Priority 0 and 1 working first (the foundation and absorbed features), pass the review gate, then build Priority 2 (transcendence), then verify.

Write:

`$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-build-log.md`

Include:
- what was built
- what was intentionally deferred
- skipped body fields that remain
- any generator limitations found

### Phase 3 Completion Gate

**MANDATORY. Do NOT proceed to [Phase 4](12-shipcheck.md) until this gate passes.**

Before moving to shipcheck, verify the build log against the absorb manifest. Counting alone is not enough: a build that replaces an approved `keywords-data google-ads search-volume --auto-mode` with a self-contained wrapper `keywords volume` keeps the count right while shipping a different command than what [Phase 1.5](08-ecosystem-absorb-gate.md) approved. The gate must verify the **specific approved command path** for each row that declares one.

**Sub-60 reprint strictness:** If this run is reprinting an existing library CLI whose prior `.printing-press.json` had `scorecard.steinberger.percentage < 60` (`PRIOR_SUB60_REPRINT=true` from [Phase 0](03-resolve-and-reuse.md)), partial transcendence implementation is a HOLD by default. The Phase 3 Completion Gate may not use `partial-implementation OK` semantics while any shipping-scope transcendence row is missing. To override, write an explicit `partial_transcendence_override` note in the build log that names each missing row, explains why it is intentionally deferred, and states that the user accepted the sub-60 reprint shipping with partial novel coverage. Without that note, any missing approved transcendence row blocks [Phase 4](12-shipcheck.md).

1. **Per-row Cobra resolution check.** Read approved command paths from `$RESEARCH_DIR/<stamp>-feat-<api>-pp-cli-absorb-manifest.md`:
   - Every transcendence row's `Command` value.
   - Every absorbed row whose `Our Implementation` value starts with `<api>-pp-cli <clean command path>`.
   - Every absorbed row whose `Our Implementation` value starts with `(behavior in <api>-pp-cli <command path>)`. For these rows, first extract the text between the literal prefix `(behavior in ` and the first closing `)`, producing `<api>-pp-cli <command path>`, then apply the same binary-strip and flag-strip rules to that extracted command text.
   - Skip rows that start with `(generated endpoint)` because the generator-emitted typed endpoint surface already covers those commands.
   - Skip rows that start with `(stub)` because the [Phase Gate 1.5](08-ecosystem-absorb-gate.md) stub approval list is their source of truth; stubs are intentionally unresolved implementation placeholders and must not be counted as built commands.
   - Do not infer command paths from freeform prose. Any absorbed row whose `Our Implementation` value does not start with `<api>-pp-cli <clean command path>`, `(behavior in <api>-pp-cli <command path>)`, `(generated endpoint)`, or `(stub)` is an invalid manifest row; return to [Phase 1.5](08-ecosystem-absorb-gate.md) and normalize it before proceeding.

   For each approved path, including command text extracted from `(behavior in <api>-pp-cli <command path>)` rows, strip any leading binary name, then strip flag tokens and quoted args to get the leaf command path (drop everything from the first `-` token onward; `bottleneck` stays `bottleneck`, `velocity --weeks 4` becomes `velocity`, `compare "LeBron" "Curry"` becomes `compare`, `keywords-data google-ads search-volume --auto-mode` becomes `keywords-data google-ads search-volume`). Then run:
   ```bash
   ./<api>-pp-cli <leaf path> --help
   ```
   Assert (a) exit code 0 AND (b) the help output's `Usage:` spec line is `<binary> <leaf path> [flags]` — i.e., the line **immediately before** ` [flags]` is the full leaf path you requested. Cobra falls through to the parent's help when a subcommand is unknown — same exit 0, but the Usage spec line is `<binary> <parent> [command]` instead of `<binary> <parent> <leaf> [flags]`. The grep-able signal is `<leaf> [flags]` for a real command vs `[command]` for a parent fall-through; the leaf appearing only under `Available Commands:` is also a fall-through.
2. **HALT on any miss.** If any approved row fails (a) or (b), STOP and name the manifest section plus row number or source line in the miss message, e.g. `Absorbed row 3: timeline did not resolve as a Cobra command`. Either build the approved command path now, or return to [Phase 1.5](08-ecosystem-absorb-gate.md) with a revised manifest for explicit re-approval per the existing "no mid-build downgrade" rule. Do not invent a wrapper command and silently update the manifest. Do not classify the feature as "documentation-only" because integration touches many files.
3. **Deterministic backstop.** After the per-row walk, run the same machine-checked equivalent so a manifest-vs-`research.json` drift cannot mask a miss:
   ```bash
   cli-printing-press dogfood --dir "$CLI_WORK_DIR" --research-dir "$API_RUN_DIR" --json \
     | jq -e '.novel_features_check | .found == .planned and (.missing // []) == [] and (.skipped // false) == false'
   ```
   The `novel_features_check` block reports planned/found/missing against `research.json`'s `novel_features`; an exit-0 here plus a clean per-row walk means both sources agree the build matches [Phase 1.5](08-ecosystem-absorb-gate.md) approval. **`skipped: true` is a HALT, not a pass at this gate.** Dogfood marks the check skipped only when `--research-dir` is missing or `research.json` has no `novel_features` key — both conditions mean the gate has no source of truth to verify against, which is exactly the silent-bypass path the gate was designed to prevent. If you reach this gate with no `novel_features` in `research.json` but the absorb manifest lists transcendence rows, re-derive `research.json` from the manifest (per Step 1.5e) before re-running. If `dogfood` reports missing features that the manifest still lists, either `research.json` was edited mid-build (re-derive it from the manifest) or the build is genuinely incomplete (return to step 1).
4. **Test presence for pure-logic novel packages.** Every Go package you created under `internal/` for novel-feature logic (parsers, matchers, scalers, scrapers — anything that isn't command wiring) must have a `_test.go` with at least one table-driven happy-path test per exported function. `cli-printing-press dogfood` surfaces violations as structural issues: pure-logic packages with zero tests fail shipcheck; packages with fewer than 3 test functions are flagged as warnings for [Phase 4.85](16-agentic-output-review.md)'s agentic review. Trivial placeholder tests pass the file-presence check but are the wrong shape — write real assertions or the review catches you.

The check is structural — no judgment about whether each command does "enough." Behavioral correctness remains dogfood's and scorecard's job in [Phase 4](12-shipcheck.md).

The generator handles Priority 0 (data layer) and most of Priority 1 (absorbed API endpoints). Priority 2 (transcendence) is always hand-built — the generator does not produce these. If you skip Priority 2, the CLI ships without the features that differentiate it from every other tool.

**Starter templates for novel commands.** Cobra wiring is mechanical and consistent across novel features; the actual feature work lives in the RunE body. Copy the wrapper below and one of the RunE skeletons that follows, fill in the placeholders from the absorb manifest's transcendence row (`Name`, `Command`, `Description`, `Example`, `WhyItMatters`), and replace the body comments with your implementation. Dogfood, verify, and scorecard still apply to the result — the templates raise the floor without changing what shipcheck checks.

**Helpers already emitted by the generator.** Do not reinvent these helpers in novel command files. They live in `internal/cli/helpers.go` after generation and are available to every hand-written command in package `cli`:

- `printJSONFiltered(w io.Writer, v any, flags *rootFlags) error` - apply `--select`, `--compact`, `--csv`, and `--quiet` while writing JSON from a Go value.
- `printAutoTable(w io.Writer, items []map[string]any) error` - render JSON-like rows as the generated human table format.
- `defaultDBPath(name string) string` - resolve the local SQLite database path for `<name>`.
- `dryRunOK(flags *rootFlags) bool` - detect verify-friendly `--dry-run` short-circuits before network, store, or filesystem work.
- `writeDryRun(w io.Writer, flags *rootFlags, action string) error` - end a `--dry-run` short-circuit with a `{"dry_run":true,"action":...,"would":...}` envelope under `--json` and a prose line otherwise; pass `cmd.OutOrStdout()`. Never `return nil` silently from a dry-run branch: empty stdout under `--json` fails the live-dogfood `json_fidelity` check. Let it own the whole dry-run output — an extra prose write before it makes stdout unparseable under `--json`.
- `boundCtx(parent context.Context, flags *rootFlags) (context.Context, context.CancelFunc)` - apply root `--timeout` to hand-written commands that call sibling typed clients instead of the generated `internal/client`. Do not wrap generated-client binary/stream downloads in `boundCtx`; those skip the default whole-call Timeout unless `--timeout` was set explicitly (including a run profile that supplies `timeout`).
- `filterFields(data json.RawMessage, fields string) json.RawMessage` - apply `--select` to a JSON blob.
- `compactFields(data json.RawMessage) json.RawMessage` - apply `--compact` to a JSON blob.
- `isTerminal(w io.Writer) bool` - detect terminal output versus pipes.
- `wantsHumanTable(w io.Writer, flags *rootFlags) bool` - detect when output should use the generated human table instead of machine JSON.

**Empty results and output modes.** Any novel command that can return zero rows
MUST choose the output mode before printing empty-result prose. Initialize
list-shaped results with `make(..., 0)` so JSON mode emits `[]`, never `null`.
Route JSON, agent, and CSV output through the generated helpers, and keep
human-only empty-result prose inside the human branch:

```go
rows := make([]yourRowType, 0)
// populate rows...
if !wantsHumanTable(cmd.OutOrStdout(), flags) {
	return printJSONFiltered(cmd.OutOrStdout(), rows, flags)
}
if len(rows) == 0 {
	// human-only empty-result prose; never write this before the machine branch
	fmt.Fprintln(cmd.OutOrStdout(), "No matching items found.")
	return nil
}
// Render the non-empty human table here.
return nil
```

Do not put a `len(rows) == 0` prose return before the mode check, and do not
manually branch only on `flags.asJSON`. `wantsHumanTable` covers `--json`,
`--agent`, `--csv`, `--plain`, `--quiet`, `--compact`, `--select`, and piped
output. The Phase 5 dogfood run must exercise a known zero-result case when
available: `--json` and `--agent` stdout must parse as JSON, `--csv` must be an
empty or valid CSV stream, and human mode may retain the explanatory prose.

```go
// internal/cli/<command>.go — replace <command> with the kebab leaf
// of NovelFeature.Command (e.g., "issues stale" → "issues_stale.go").
package cli

import (
	"github.com/spf13/cobra"
	// add: "encoding/json", "fmt", "os", "<module>/internal/store", etc. as needed
)

func newNovelXxxCmd(flags *rootFlags) *cobra.Command {
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

// Multi-word Commands like "issues stale": register the child from the
// preserved file through the novel hook. Hooks run after generated novel
// parent groups are attached, so Find sees those parents. Resolve the
// generated parent by its command path and use the duplicate-safe helper
// (a real command replaces a TODO scaffold with the same name):
//   registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
//       issuesCmd, _, err := root.Find([]string{"issues"})
//       if err == nil {
//           addNovelCommandIfAbsent(issuesCmd, newNovelIssuesStaleCmd(flags))
//       }
//   })
// Leaf commands must declare every non-root flag used in their examples.
// Use kebab-case flag names, such as --max-age instead of --maxAge, so the
// generated CLI convention and verify-skill flag scanner stay aligned.
// Do not rely on parent-local flags like --org or --project being accepted by
// child commands unless the parent registered them with PersistentFlags().
// Single-word Commands use the same hook: addNovelCommandIfAbsent(root, newNovelXxxCmd(flags)).
// A preserved hook that still uses root.AddCommand for the same name also
// wins: after hooks run, generated TODO scaffolds with a real sibling are dropped.
```

**RunE skeleton — API-call shape** (live data via a sibling typed client):

```go
RunE: func(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && cmd.Flags().NFlag() == 0 {
		return cmd.Help()
	}
	if dryRunOK(flags) {
		return writeDryRun(cmd.OutOrStdout(), flags, "<command name>")
	}
	ctx, cancel := boundCtx(cmd.Context(), flags)
	defer cancel()
	if <required input missing> {
		_ = cmd.Usage()
		return usageErr(fmt.Errorf("<flag-or-arg> is required"))
	}
	c, err := newSiblingClient(flags) // replace with your internal/<api> or internal/source/<name> constructor
	if err != nil {
		return err
	}
	// Pass ctx to every sibling-client request so root --timeout bounds
	// crawls, wide scans, and fan-out loops. Generated endpoint commands that
	// use flags.newClient()/internal/client already consume flags.timeout.
	data, err := c.FetchResource(ctx, <request params>)
	if err != nil {
		return fmt.Errorf("fetching <resource>: %w", err)
	}
	// If the API returns CSV (`response_format: csv` in any spec endpoint),
	// wrap raw client data with cliutil.ParseCSV(data) before embedding it in a JSON envelope.
	// XML-only endpoints (`response_format: xml`) are auto-detected from the spec's
	// response content type; the generated client already normalizes those bodies to JSON
	// via cliutil.XMLToJSON, so novel code can treat `data` as ordinary JSON.
	// Parse data into your feature's view. Use cliutil.CleanText for any
	// text extracted from HTML or schema.org JSON-LD; re-implementing
	// HTML-entity unescape inline is the &#39; bug class.
	var view yourViewType // = parse(data)
	if !wantsHumanTable(cmd.OutOrStdout(), flags) {
		return printJSONFiltered(cmd.OutOrStdout(), view, flags)
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
		return writeDryRun(cmd.OutOrStdout(), flags, "<command name>")
	}
	ctx, cancel := boundCtx(cmd.Context(), flags)
	defer cancel()
	if <required input missing> {
		_ = cmd.Usage()
		return usageErr(fmt.Errorf("<flag-or-arg> is required"))
	}
	c, err := newSiblingClient(flags) // replace with your internal/<api> or internal/source/<name> constructor
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
			data, err := c.FetchDetail(ctx, id)
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
	if !wantsHumanTable(cmd.OutOrStdout(), flags) {
		return printJSONFiltered(cmd.OutOrStdout(), view, flags)
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

SQLite store queries must use a **drain-first** pattern. SQLite uses a single connection by default, so never issue another query while a `*sql.Rows` is open. If the command needs follow-up lookups, parent-to-child expansion, or ID-to-name/email resolution, scan the first result set into plain structs/slices, check `rows.Err()`, close `rows`, and only then run follow-up `QueryContext` / `QueryRowContext` calls.

```go
// Declare these alongside the cmd literal, before return cmd:
//   var dbPath string
//   cmd.Flags().StringVar(&dbPath, "db", "", "Database path")

RunE: func(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && cmd.Flags().NFlag() == 0 {
		return cmd.Help()
	}
	if dryRunOK(flags) {
		return writeDryRun(cmd.OutOrStdout(), flags, "<command name>")
	}
	ctx, cancel := boundCtx(cmd.Context(), flags)
	defer cancel()
	if <required input missing> {
		_ = cmd.Usage()
		return usageErr(fmt.Errorf("<flag-or-arg> is required"))
	}
	if dbPath == "" {
		dbPath = defaultDBPath("<cli>-pp-cli") // replace <cli> with the API slug
	}
	if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
		fmt.Fprintf(cmd.ErrOrStderr(), "no local mirror at %s\nrun: <cli> sync --resources <resource> --db %s\n", dbPath, dbPath)
		if !wantsHumanTable(cmd.OutOrStdout(), flags) {
			return printJSONFiltered(cmd.OutOrStdout(), make([]yourRowType, 0), flags)
		}
		return nil
	}
	db, err := store.OpenWithContext(ctx, dbPath)
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
	rows, err := db.DB().QueryContext(ctx, `
		SELECT id, data FROM resources
		WHERE resource_type IN ('<resource>', '<parent>_<resource>')
		  AND ...`)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	// Scan each row. id/data on the resources table are NOT NULL so bare
	// strings are safe; ANY optional field selected via json_extract or
	// pulled from a typed FTS/upsert table can be NULL — use sql.Null*
	// scan targets (or COALESCE in the SQL) for those, see the NULL-safe
	// scans paragraph below.
	rawRows := make([]yourRawRowType, 0) // make([]T, 0) keeps empty JSON as [] not null
	for rows.Next() {
		var row yourRawRowType
		// Scan only columns from this result set here. Do NOT query again
		// inside this loop; drain and close rows first.
		if err := rows.Scan(&row.ID, &row.Data); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan row: %w", err)
		}
		rawRows = append(rawRows, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close rows: %w", err)
	}
	results := make([]yourRowType, 0, len(rawRows))
	for _, raw := range rawRows {
		// Follow-up queries are safe here because the parent *sql.Rows is closed.
		// Resolve IDs, expand child rows, or perform local joins, then append.
		_ = raw
	}
	if !wantsHumanTable(cmd.OutOrStdout(), flags) {
		return printJSONFiltered(cmd.OutOrStdout(), results, flags)
	}
	// Human/terminal output.
	return nil
},
```

For flat-only resources, the typed FTS/upsert tables the generator emits (e.g., `tasks_fts`, `projects`) work too — `SELECT id, data FROM <typed-table>` is the fast path. The `IN (...)` pattern above is the safe default whenever the resource may be hierarchical; `cli-printing-press dogfood --json` shows the actual `resource_type` distribution so you can confirm without running raw SQL.

For features that combine both (cache an API response in the store, or fall through to live when the local store is stale), nest one skeleton inside the other and use the `--data-source auto/local/live` flag pattern from the generated `sync` command.

**Store write transaction guardrail:** `store.Upsert` and `store.UpsertBatch` open their own write transaction. Do not call them from inside an open `db.DB().BeginTx` / `tx.Prepare` / `tx.Exec` write transaction; SQLite WAL permits only one writer and the nested write can busy-wait, fail, or corrupt the outer statement state. If a novel command writes both custom tables and `resources`, either use transaction-scoped helpers that operate on the same `*sql.Tx`, or commit/rollback the custom-table transaction before calling `store.Upsert` / `store.UpsertBatch`. Never discard these errors with `_`; a busy-timeout is data loss.

**Shared helpers available to novel code:** The generator emits `internal/cliutil/` in every CLI. When authoring novel commands, prefer `cliutil.FanoutRun` for any aggregation command (any `--site`/`--source`/`--region` CSV fan-out) and `cliutil.CleanText` for any text extracted from HTML or schema.org JSON-LD. Re-implementing these inline is how recipe-goat's trending silent-drop and `&#39;` entity bugs shipped.

**Hand-coded duration flags MUST use `cliutil.ParseDurationLoose` with a `StringVar` flag (not `DurationVar`).** Go's `time.ParseDuration` rejects the `7d`/`30d`/`1w`/`4w` day/week shorthand that the framework's `sync --since` already accepts, so a `DurationVar` flag fails at runtime on input agents and users reasonably expect. Declare the flag as a `StringVar`, then post-parse with `cliutil.ParseDurationLoose`, which adds `d`/`w` suffix support and otherwise defers to `time.ParseDuration`.

**OData v3 datetime fields MUST be decoded with `cliutil.ParseODataDate`.** OData v3 APIs (Exact Online, Microsoft Dynamics 365 Business Central, Dynamics NAV) return dates as `/Date(1715731200000)/` string literals that no standard parser accepts, so the raw value passes straight through to JSON output and agents cannot parse `created_at`/`due_date`. `cliutil.ParseODataDate(s) (time.Time, bool)` decodes the literal to a UTC `time.Time` and falls back to RFC3339, so callers need not dispatch on format. Re-implementing this inline per command is how the same regex ships inconsistently across OData CLIs.

**Streaming frame normalizers MUST use `cliutil.ExtractNumber` / `cliutil.ExtractInt` rather than raw `float64`/`int64` struct fields.** Real-world WebSocket and streaming JSON feeds (Binance, Coinbase, Kraken, Stripe `*_decimal`, vendor-specific market-data feeds) commonly encode numeric values as JSON-encoded strings (`"price":"1.91"`). `json.Unmarshal` of a JSON string into a `float64` field returns no error and silently leaves the field at 0; combined with NULL-on-zero patterns this discards the entire numeric feed with no error signal anywhere in the pipeline. The helpers accept both shapes (JSON number or JSON-encoded string), report `ok=false` on missing/null/unparseable, and are the canonical extraction path for `map[string]json.RawMessage` decoders. Re-implementing this inline as a `float64` struct field is the silent-aggregation-failure bug class.

**WebSocket-primary APIs SHOULD declare `streaming:` and use the generated live scaffold.** When the API's facts arrive over WebSocket and REST supplies metadata, follow `../references/ws-primary-pattern.md`. Do not reimplement dial/subscribe/reconnect, newline-delimited JSON splitting, metadata status polling, or rebase-log writes in novel code unless the API genuinely breaks the generated lifecycle contract.

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
**Hand-edits must be regen-mergeable.** `cli-printing-press generate --force` snapshots the existing tree, emits a fresh tree, then runs the same AST-aware reconciliation used by `cli-printing-press regen-merge`. Whole hand-authored files and lost `AddCommand` wiring are preserved automatically; straightforward hand-edits to generated Go files (added declarations, literal drift, body drift) are classified and carried forward when the merge can do so safely. A spec-checksum mismatch falls back to novel-only preservation (fusion guard) and lists each dropped TEMPLATED file on stderr; same-spec `--force` still carries those edits. If `--force` would drop a `.go` file that lacks the generator header (typically a hand-replaced generated file on a cross-spec / fusion-guard run), it lists those paths and requires `--yes` or an interactive confirmation before deleting them. Same-spec `--force` keeps standalone hand-authored files (`internal/store/<api>_migrations.go`, `internal/client/<api>_headers.go`, `internal/cli/<api>_auth.go`, new packages) and does not prompt. Spec- and research-driven generated surfaces (`internal/cli/which.go`, `internal/cli/learn_init.go`, `internal/mcp/tools.go`, and `root.go` Highlights) take the fresh emission so a still-compiling older copy cannot ship after the input rewrote them. When a reprint has the prior template emission as `--base`, rewritten function bodies that still match that emission take fresh as well, including inside a file that also has a real hand-edit. For risky edits, use the standalone `regen-merge` command first when you want a previewable report before applying.

For an extension to be durable, put it in its own file beside the emitted one:

- **Custom config fields:** create `internal/config/<api>_config.go` exporting accessors your novel code reads directly. Do not add fields to the emitted `Config` struct.
- **Custom request headers** (vendor fingerprint, `X-CSRF`, app-version, signed timestamps): create `internal/client/<api>_headers.go` exporting a func that builds the header map; novel code passes that map to `client.GetWithHeaders` / `PostWithHeaders` when it calls the API. The generated `client.go` has no global request mutator, so this pattern only covers requests made directly from novel code — it does not intercept calls from generated endpoint commands. Do not edit the templated header block in `client.go`.
- **Custom auth flow** (browser-sniffed sessions, vendor SSO, refresh hooks beyond OAuth2): create `internal/cli/<api>_auth.go` (package `cli`, same as the generated `auth.go`) with the API-specific token capture or refresh, and wire it from a novel command rather than editing the templated `auth.go` constructor functions (`newAuthLoginCmd`, `newAuthSetupCmd`, etc.). If the custom flow implements OAuth2 Authorization Code + PKCE, read [references/oauth2-pkce-cli-checklist.md](../references/oauth2-pkce-cli-checklist.md) before writing or reviewing the command.
- **Extended store schema** (typed tables beyond `resources`, vendor JSON columns, full-text indexes): create `internal/store/<api>_migrations.go` running its own `CREATE TABLE ... IF NOT EXISTS` from a lazy init invoked by the novel commands that need it. Do not edit the migration slice in `store.go`.
- **New novel command:** put the command body and its `registerNovelCommand` hook in their own `internal/cli/<feature>.go` file. Generated TODO scaffolds may refresh on `generate --force`; once you replace the TODO body with a real implementation, regen preserves that hand-authored file instead of re-emitting the scaffold. A separate hook file is also preserved, and `addNovelCommandIfAbsent` prefers that real command over a TODO scaffold with the same name so the scaffold cannot shadow it. Do not edit `root.go` for novel wiring. `generate --force` also re-injects any lost `AddCommand` call whose constructor remains in a preserved novel file. Use standalone `regen-merge` when you want to inspect the merge report before applying. Spec-declared commands are picked up by the generator's typed-tool path and need no hand-wired `AddCommand` at all.

If an extension genuinely cannot live in a separate file (a `case` branch in a templated method switch, an inline modification to a generated handler with no registry hook), file a generator issue requesting the hook rather than depending on repeated conflict-prone merges. The `AddCommand` case above is covered by the merge path.

**MCP exposure:** The generator emits `internal/mcp/cobratree/`, and the MCP binary mirrors the Cobra tree at startup. When you add, rename, or remove a user-facing Cobra command, the MCP surface follows automatically. Two annotations control how each command appears as an MCP tool:

- `cmd.Annotations["mcp:hidden"] = "true"` — exclude the command from the MCP surface entirely. Use only for debug/internal commands that should not become agent tools.
- `cmd.Annotations["mcp:read-only"] = "true"` — declare that this command does not modify external state. The MCP server attaches `readOnlyHint: true` to the resulting tool, so hosts like Claude Desktop don't bucket it under "write/delete tools" and demand permission per call. Apply this to every novel command whose only effect is reading from the API or the local store: lookups, comparisons, aggregations, render-only views, status checks. Skip it for commands that mutate external state (orders, posts, deletes) or write to user-visible files outside the local cache.
- `cmd.Annotations["mcp:write-positionals"] = "0"` — mark zero-based positional indexes that write to user-visible files when populated. Use this with `mcp:read-only` only when the command is otherwise read-only and the positional has a stdout-safe escape such as `-`. The MCP shell-out wrapper rejects non-stdout values for those positionals so a read-only-hinted tool cannot write files through the free-form `args` field. Use comma-separated indexes for multiple write sinks.

Endpoint-mirror tools the generator emits from the spec already get the right annotations automatically. Conventional REST `GET` is read-only. RPC-over-GET (shared gateway path, `.cgi`, or a query `method=`/`action=` selector) fails closed: without a read token, `mutation: false`, or `mcp:read-only`, the command is not marked read-only. An internal `mutation: true` field or OpenAPI `x-pp-mutation: true` marks a GET action as a write. `mutation: false` / `x-pp-mutation: false` marks a POST (or other non-GET) search/list endpoint as a read so the generated command prints the response body instead of the mutation acknowledgment envelope. The generator also conservatively recognizes clearly state-changing GET operation-name prefixes such as `start`, `stop`, `restart`, and `deploy`, and read-shaped prefixes such as `search`, `list`, and `query` on POST. `mcp:read-only` is only needed on hand-authored Cobra commands the spec doesn't cover.

Do not rationalize skipping transcendence features because "the CLI already works for live API interaction." The absorb manifest was approved by the user. Build what was approved.

Before following `Next:`, record the durable handoff and point `--evidence` at
the Phase 3 build log:

```bash
"$PRINTING_PRESS_BIN" phase-receipt complete --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "11-build-the-goat" --evidence "$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-build-log.md"
```

That canonical completion is for a build that ships forward. If the Phase 3 Completion Gate instead forces a return to [08-ecosystem-absorb-gate](08-ecosystem-absorb-gate.md) because a shipping-scope feature proved infeasible (the build-infeasible rule in this file's Completion Gate), record that rework handoff instead of the canonical one, then re-enter the absorb gate rather than following the canonical `Next:` below:

```bash
"$PRINTING_PRESS_BIN" phase-receipt complete --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "11-build-the-goat" --next "08-ecosystem-absorb-gate" --note "<why the manifest needs rework>"
```

Next: phases/12-shipcheck.md
