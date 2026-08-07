# Plan — #2679: typed MCP endpoint tools drop spec-declared param defaults

**Issue:** mvanhorn/cli-printing-press#2679 (authored by us as a retro finding). The issue body is the
spec — the design is determined, so this plan is the execution breakdown.

**Defect (verified present in `a12e75ca`):** the generator emits `mcpParamBinding` literals with no
`Default`, so an agent that omits a query param whose spec declares `default:` sends no value and
hits an upstream 4xx — while the cobra flag (which carries the default) succeeds. CLI and MCP diverge.

## Scope boundary (deliberate, keep the regression surface small)

- **Query params only.** `mcpParamBindings()` sets a default only for `loc == "query"` bindings.
  Path params must NOT get a default fallback (a missing path segment is a different, required error).
  **Body-param defaults are out of scope** (a follow-up) — the documented trigger and all three
  cross-CLI examples (splitwise/eventbrite/numista) are query params.
- **Gate emission on a file-level flag** so default-less CLIs stay byte-identical (mirror the existing
  `$hasNestedBodyPath` / `$hasMultipartRequest` conditional-field pattern). Golden churn is then
  limited to fixtures that actually have a defaulted query param.

## Changes

1. **`internal/generator/generator.go`** — add `Default string` to the `mcpParamBinding` struct
   (`:4571`).
2. **`mcpParamBindings()` (`:4592`)** — in the `endpoint.Params` loop, when `loc == "query"` and
   `p.Default != nil`, set `Default: fmt.Sprintf("%v", p.Default)` (mirror the cobra default-render
   path, which formats the `interface{}` with `%v`).
3. **New generator helper `hasMCPParamDefault(apiSpec) bool`** — true iff any endpoint has a query
   param with a non-nil default. Register it in the template FuncMap exactly like
   `hasMCPNestedBodyPath` (find its Go func + its FuncMap registration and mirror both).
4. **`internal/generator/templates/mcp_tools.go.tmpl`:**
   - top (near `:7`): `{{- $hasMCPParamDefault := hasMCPParamDefault .APISpec}}`
   - emitted struct (`:170`): add `Default string` gated on `{{- if $hasMCPParamDefault}}`.
   - binding literals (the four `makeAPIHandler(...)` sites at `:76 :78 :112 :114`): append
     `{{if .Default}}, Default: {{printf "%q" .Default}}{{end}}` after the existing optional fields.
   - handler arg loop (`:307`): replace
     ```
     v, ok := args[binding.PublicName]
     if !ok {
         continue
     }
     ```
     with (default branch gated on `$hasMCPParamDefault`):
     ```
     v, ok := args[binding.PublicName]
     if !ok {
     {{- if $hasMCPParamDefault}}
         if binding.Default != "" {
             v = binding.Default
         } else {
             continue
         }
     {{- else}}
         continue
     {{- end}}
     }
     ```
   This makes an **absent** arg fall back to the default; an **explicitly-passed** value (incl. `""`,
   `ok == true`) still overrides — it never reaches the default branch.

## Tests

- **Positive (generator unit):** an endpoint with a `query` param `p.Default = "city"` →
  `mcpParamBindings(...)` returns a binding with `Default == "city"`.
- **Negative (generator unit):** a query param with `p.Default == nil` → binding `Default == ""`;
  a **path** param with a default → binding `Default == ""` (defaults never apply to path).
- **Emission (string-assert):** generate from a minimal spec with a defaulted query param; assert the
  emitted `internal/mcp/tools.go` contains `Default: "city"` in the binding literal AND the handler
  contains the `binding.Default != ""` fallback.
- **Regression (real entry point, if a runtime harness exists):** the generated `makeAPIHandler`,
  called WITHOUT the arg, puts the default on the wire; called WITH an explicit value (incl. `""`),
  sends that value. Prefer extending an existing generated-module MCP handler test; otherwise the
  override semantics are guaranteed by construction (the `ok == true` path never touches `Default`)
  and asserted via emitted source.

## Verification

`go test ./...` (FULL), `go fmt`, `golangci-lint run`, `scripts/golden.sh verify` — regenerate goldens
for the fixtures whose emitted MCP tools now carry `Default` (expected, scoped to defaulted-query
fixtures). Watch for interplay with the just-merged #2678 (defaulted high-frequency query params in
the global filter) — same area.
