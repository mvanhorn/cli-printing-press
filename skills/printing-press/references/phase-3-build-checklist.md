# Phase 3: Agent Build Checklist + build-guidance templates

Lazy-loaded body for the Phase 3 per-command build checklist. The dispatcher keeps the 12 principle names as an inline skeleton; this file holds the full text, the scan-and-filter pattern, and the Verify-friendly RunE template. Read in full while building Priority 1/2 commands.

> **Maintenance contract:** the 12 principle names are duplicated — the inline skeleton in `SKILL.md` (the "Agent Build Checklist" list under Phase 3) and the full text below. If you rename or re-scope a principle in one copy, mirror the change in the other; `verify-skill` cannot catch prose-level drift between the two.

### Agent Build Checklist (per command)

After building each command in Priority 1 and Priority 2, verify these 12 principles are met. These map 1:1 to what Phase 4.9's agent readiness reviewer will check - apply them now so the review becomes a confirmation, not a catch-all.

1. **Non-interactive**: No TTY prompts, no `bufio.Scanner(os.Stdin)`, works in CI without a terminal
2. **Structured output**: `--json` produces valid JSON, `--select` filters fields correctly. Hand-written novel commands that build a Go-typed slice/struct and emit JSON should use the generated receiver-style helper, `flags.printJSON(cmd, v)`, or call `printJSONFiltered(cmd.OutOrStdout(), v, flags)` directly. Both route through `printOutputWithFlags`, picking up `--select`, `--compact`, `--csv`, and `--quiet` for free. Verify with `<cli> <novel> --json --select <field> | jq 'keys'` returning only the requested fields.
3. **Progressive help**: `--help` shows realistic examples with domain-specific values (not "abc123"). **Use `Example: strings.Trim(\`...\`, "\n")` (preserves leading 2-space indent) NOT `strings.TrimSpace(\`...\`)` (strips it).** TrimSpace makes the first example line unindented; dogfood's example-detection parser is tolerant of this in current versions, but the indented form renders correctly across every Cobra version and is the convention used by every generated command.
4. **Actionable errors**: Error messages name the specific flag/arg that's wrong and the correct usage
5. **Safe retries**: Mutation commands support `--dry-run`, idempotent where possible
6. **Composability**: Exit codes are typed (0/2/3/4/5/7/10 as applicable), output pipes to `jq` cleanly
7. **Bounded responses**: `--compact` returns only high-gravity fields, list commands have `--limit`
8. **Verify-friendly RunE**: Hand-written commands MUST NOT use `Args: cobra.MinimumNArgs(N)` or `MarkFlagRequired(...)`. Cobra evaluates both before RunE runs, so a `--dry-run` guard inside RunE cannot reach if those gates fail. Verify probes commands with `--dry-run` and expects exit 0; commands with hard arg/flag gates fail those probes. Instead: validate inside RunE, fall through to `cmd.Help()` only for unambiguous help-only invocations (no args and no flags), short-circuit on `dryRunOK(flags)` before any IO, and return `usageErr(...)` with exit 2 when required input is missing in real mode.
   - **Use string for "positional OR flag" commands**: when a command accepts a positional `<x>` OR a flag `--y` as alternatives (e.g., `snapshot <co>` or `snapshot --domain example.com`), declare `Use: "<cmd> [x]"` with **square brackets** (optional), not `<x>` (required). Validate "exactly one of x or --y" inside RunE. Required positionals declared with angle brackets break verify-skill recipes that use the flag-only form.
   - **Declare verifier fixture inputs when generic values are not enough**: if the command needs realistic positional values or required flags to pass the verifier's happy path, add `Annotations: map[string]string{"pp:happy-args": "<item>=example-id;--query=example"}` or assign a whole initialized `cmd.Annotations` map after construction. The verifier consumes semicolon-separated tokens in order: `<label>=value` tokens overlay synthesized positional args, and `--flag=value` tokens overlay or add flag/value pairs. Commands without the annotation keep the generic synthesized inputs.
9. **Side-effect commands stay quiet under verify**: Any hand-written command that performs a visible side effect (opens a browser tab, sends a notification, plays audio, dials out to an OS handler) MUST follow both halves of the convention:
   - **Print by default; opt in to the action.** The default behavior prints what would happen (`would launch: <url>`); a flag like `--launch` / `--send` / `--play` is required to actually do it. food52's `open` command is the reference shape — see `internal/cli/open.go` after retro #337.
   - **Short-circuit when `cliutil.IsVerifyEnv()` returns true.** The Printing Press verifier sets `PRINTING_PRESS_VERIFY=1` in every mock-mode subprocess; commands that ignore it can spam the user's environment during a verify pass even with the print-by-default flag pattern. The helper is generated into every CLI's `internal/cliutil/verifyenv.go`. Pattern:
     ```go
     if cliutil.IsVerifyEnv() {
         fmt.Fprintln(cmd.OutOrStdout(), "would launch:", url)
         return nil
     }
     ```
   This is defense-in-depth: the verifier also runs a heuristic side-effect classifier, but it can miss commands whose `--help` text and source don't match the heuristics. The env-var check is the floor.
   - **Long-running commands curtail work under live-dogfood.** Any hand-written command whose happy path is an expensive network operation (full sync loops, content crawlers, bulk archive walks) MUST check `cliutil.IsDogfoodEnv()` and curtail work to fit inside the matrix's flat 30s per-command timeout. `cli-printing-press dogfood --live` sets `PRINTING_PRESS_DOGFOOD=1` in every subprocess. Pattern:
     ```go
     if cliutil.IsDogfoodEnv() {
         return crawl(ctx, opts.WithMaxPages(1))
     }
     ```
     Distinct from `IsVerifyEnv`: dogfood is a real-API matrix, so curtail work (paginate once, smaller `--limit`), never substitute mock data for real calls.
10. **Per-source rate limiting**: any hand-written client in a sibling internal package (`internal/source/<name>/`, `internal/recipes/`, `internal/phgraphql/`, etc. — anything not generator-emitted) that makes outbound HTTP calls MUST use `cliutil.AdaptiveLimiter` and surface `*cliutil.RateLimitError` when 429 retries are exhausted. Empty-on-throttle is indistinguishable from "no data exists" and silently corrupts downstream queries. Read [references/per-source-rate-limiting.md](references/per-source-rate-limiting.md) when authoring a sibling client. Enforced at generation time by dogfood's `source_client_check`.
11. **Parallel-fetch partial failures**: any command that fans out N API calls and computes an aggregate (averages, rollups, comparisons, cross-source merges, digest summaries) MUST preserve each fetch error through the result channel and exclude error-tagged entries from totals and denominators. Failed fetches may still appear in the response so the caller can see the gap, but they must not become zero-valued phantom rows that dilute averages or counts. Surface the partial failure explicitly with:
   - a stderr warning that names the failed count and the actual aggregation denominator, for example `warning: 2 of 10 fetches failed; averages computed over the remaining 8 items`
   - a `fetch_failures` field in the JSON response envelope listing the failed entries and error messages

Silently averaging phantom zeros is worse than reporting a partial result.
12. **Scan-and-filter caps**: any hand-written transcendence command that scans
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
				fmt.Fprintf(cmd.OutOrStdout(), "would scan up to %d pages for matching items\n", maxScanPages)
				return nil
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
        return nil
    }
    if <required input missing> {
        _ = cmd.Usage()
        return usageErr(fmt.Errorf("<flag-or-arg> is required"))
    }
    // ... real work ...
}
```

Why each branch exists: the `len(args) == 0 && cmd.Flags().NFlag() == 0` branch handles an interactive `<cli> mycommand` help-only invocation without treating help as an error. The `dryRunOK` branch handles verify's `<cli> mycommand <fixture> --dry-run` probes before network or filesystem IO. The required-input branch handles non-help invocations where a mode or output flag is present (`--no-input`, `--agent`, `--json`) but the required ID, query, path, or other command input is still missing. Missing required input must print usage and return `usageErr(...)` so callers get exit code 2 instead of a silent rc=0 skip.

Multi-positional commands (N >= 2 required args) must use a two-check shape so only the bare help probe returns exit 0:

```go
if len(args) == 0 && cmd.Flags().NFlag() == 0 {
	return cmd.Help() // bare invocation help probe
}
if len(args) < N {
	_ = cmd.Usage()
	return usageErr(fmt.Errorf("missing required positional argument"))
}
```

This preserves verify-friendly help behavior for 0 args while making partial positional input (`1..N-1`) fail with exit 2 in dogfood `error_path`. Single-positional commands can keep the single required-input check. If a multi-positional command supports `--dry-run`, place its `dryRunOK(flags)` branch after the `len(args) < N` gate (once all N positionals are present), so the dry-run probe still short-circuits.

Do not collapse the first and third branches into `if len(args) == 0 || <flag empty> { return cmd.Help() }`. `cmd.Help()` returns `nil`, so agents and scripts cannot distinguish "help was requested" from "the command skipped required work."

For commands with no required inputs, omit the `usageErr(...)` branch entirely and keep the help-only plus dry-run branches.

If the command reads a file or directory (`os.ReadFile`, `os.ReadDir`, `os.Stat`, `os.Open`, `os.OpenFile`, `os.Lstat`, `filepath.Walk`, `filepath.WalkDir`, or any other filesystem access), the read MUST come after `dryRunOK()`, not before. Filesystem reads before `dryRunOK()` cause `validate-narrative --full-examples` to fail with a missing-file error rather than a clean dry-run exit 0.
