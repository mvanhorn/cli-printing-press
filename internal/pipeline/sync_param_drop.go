package pipeline

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/url"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
)

// SyncParamDropResult is the gate's per-call inventory of API calls in
// hand-authored sync / transcendence code whose passed-key set is a
// strict subset of the key set captured for the same endpoint in the
// browser-sniff traffic analysis. The gate is the static counterpart to
// what cobratree gives endpoint-mirror commands: hand-authored sync code
// is exempt from mechanical endpoint-surface checks, so cardinality
// drift against the real site can only be caught here.
//
// Same diff is run twice — once from the skill's Phase 4.x check (against
// the source tree before ship), once from the scorer's dogfood pass — so
// both call sites share this result shape.
type SyncParamDropResult struct {
	// Checked is the number of `client.Get(...)` / `client.Post(...)` /
	// etc. call sites the AST walker inspected. Includes suppressed
	// (`// pp:sync-params-intentional-subset`) and not-in-capture sites.
	Checked int `json:"checked"`
	// Suppressed is the count of call sites that carried the
	// `// pp:sync-params-intentional-subset` opt-out comment immediately
	// above. Tracked so reviewers can see how often the escape hatch
	// fires; unbounded growth is itself a smell.
	Suppressed int `json:"suppressed,omitempty"`
	// Findings are the call sites whose captured-key set is a strict
	// superset of the code's passed-key set on the same path. Empty when
	// every call either matches the capture, calls an uncaptured path,
	// or is suppressed.
	Findings []SyncParamDropFinding `json:"findings,omitempty"`
	// Skipped is true when the gate could not run (no traffic-analysis
	// path resolvable, or the file doesn't parse). Skipped runs do not
	// fail the gate — absence of capture is the no-flag state defined by
	// the acceptance criteria.
	Skipped bool `json:"skipped,omitempty"`
}

// SyncParamDropFinding records one call site whose passed-key set is a
// strict subset of the capture. The reviewer reads File:Line, sees the
// dropped keys, and either widens the call or adds the opt-out comment.
type SyncParamDropFinding struct {
	File         string   `json:"file"`
	Line         int      `json:"line"`
	Method       string   `json:"method"`
	Path         string   `json:"path"`
	PassedKeys   []string `json:"passed_keys"`
	CapturedKeys []string `json:"captured_keys"`
	DroppedKeys  []string `json:"dropped_keys"`
}

// syncParamDropSuppression is the comment marker that opts a single call
// site out of the gate. Read by the AST walker from the comment
// immediately above the call expression statement; reason text after the
// marker is preserved in the suppressed counter for the audit trail but
// is not parsed structurally.
const syncParamDropSuppression = "pp:sync-params-intentional-subset"

// CheckSyncParamDrop walks every .go file under cliDir's syncer-class
// directories, finds `client.<Method>(path, params)` call sites, and
// compares the passed-key set against the same endpoint's request shape
// in the supplied traffic-analysis file. Returns Skipped when the
// traffic-analysis path is empty or unreadable — absence of capture is
// the documented no-flag state.
func CheckSyncParamDrop(cliDir, trafficAnalysisPath string) SyncParamDropResult {
	if strings.TrimSpace(trafficAnalysisPath) == "" {
		return SyncParamDropResult{Skipped: true}
	}
	analysis, err := browsersniff.ReadTrafficAnalysis(trafficAnalysisPath)
	if err != nil || analysis == nil {
		return SyncParamDropResult{Skipped: true}
	}
	capturedByKey := indexCapturedClusters(analysis.EndpointClusters)
	if len(capturedByKey) == 0 {
		return SyncParamDropResult{Skipped: true}
	}

	sources, err := collectSyncSourceFiles(cliDir)
	if err != nil || len(sources) == 0 {
		return SyncParamDropResult{Skipped: true}
	}

	result := SyncParamDropResult{}
	for _, file := range sources {
		fset := token.NewFileSet()
		parsed, parseErr := parser.ParseFile(fset, file, nil, parser.ParseComments)
		if parseErr != nil {
			continue
		}
		walkSyncParamDropCalls(fset, parsed, file, capturedByKey, &result)
	}
	sort.SliceStable(result.Findings, func(i, j int) bool {
		if result.Findings[i].File != result.Findings[j].File {
			return result.Findings[i].File < result.Findings[j].File
		}
		return result.Findings[i].Line < result.Findings[j].Line
	})
	return result
}

// collectSyncSourceFiles enumerates .go files under hand-authored sync /
// transcendence directories where the gate applies. Generated endpoint
// command files under internal/cli/ are already covered by cobratree's
// endpoint-surface check; the gate intentionally skips them so we don't
// double-count drift the existing checks already catch.
func collectSyncSourceFiles(cliDir string) ([]string, error) {
	candidates := []string{
		filepath.Join(cliDir, "internal", "syncer"),
		filepath.Join(cliDir, "internal", "sync"),
		filepath.Join(cliDir, "internal", "transcend"),
		filepath.Join(cliDir, "internal", "transcendence"),
	}
	var out []string
	for _, dir := range candidates {
		err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return nil
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") {
				return nil
			}
			if strings.HasSuffix(name, "_test.go") {
				return nil
			}
			out = append(out, path)
			return nil
		})
		if err != nil {
			continue
		}
	}
	sort.Strings(out)
	return out, nil
}

// capturedKeysIndex keys endpoint clusters by `METHOD PATH` so the AST
// walker can do an O(1) lookup. Captured shapes are normalized once.
type capturedKeysIndex map[string][]string

func indexCapturedClusters(clusters []browsersniff.EndpointCluster) capturedKeysIndex {
	index := make(capturedKeysIndex, len(clusters))
	for _, cluster := range clusters {
		path := canonicalSyncPath(cluster.Path)
		method := strings.ToUpper(strings.TrimSpace(cluster.Method))
		if path == "" || method == "" {
			continue
		}
		var keys []string
		for _, field := range cluster.RequestShape.Fields {
			name := strings.TrimSpace(field.Name)
			if name == "" {
				continue
			}
			keys = append(keys, name)
		}
		if len(keys) == 0 {
			continue
		}
		sort.Strings(keys)
		key := method + " " + path
		// If the same path appears in multiple clusters (e.g. different
		// content types) merge their captured keys: the gate's question
		// is "did the wider site ever pass key X here," not "did this
		// exact cluster."
		index[key] = mergeStringSets(index[key], keys)
	}
	return index
}

func mergeStringSets(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for _, s := range a {
		seen[s] = struct{}{}
	}
	for _, s := range b {
		seen[s] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// walkSyncParamDropCalls finds every `<recv>.<Method>(path, params)`
// call that looks like an HTTP client invocation, collects the literal
// path and the param-map keys, and emits a finding when the capture for
// the same path holds strictly more keys.
func walkSyncParamDropCalls(fset *token.FileSet, file *ast.File, fileName string, captured capturedKeysIndex, result *SyncParamDropResult) {
	// Build a quick line -> comment-text index so we can read the
	// suppression marker that sits on the line immediately above the
	// call expression. Both leading-comment and standalone-comment
	// forms work; we don't care which.
	suppressionLines := make(map[int]bool)
	for _, group := range file.Comments {
		for _, comment := range group.List {
			text := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(comment.Text, "//"), "/*"))
			if strings.HasPrefix(text, syncParamDropSuppression) {
				startLine := fset.Position(comment.Pos()).Line
				endLine := fset.Position(comment.End()).Line
				for line := startLine; line <= endLine; line++ {
					suppressionLines[line] = true
				}
			}
		}
	}

	// Walk function bodies explicitly (rather than ast.Inspect over the
	// whole file) so each recognized client call has its enclosing
	// function-or-closure body in hand. walkBlockForSyncParamDropCalls
	// uses that body as the scope when resolving a named-map arg back to
	// its declaration + subsequent `m["k"] = v` assignments — the
	// standard Go pattern for conditional query params, which a
	// literal-only walker would silently skip.
	//
	// Two top-level shapes carry function bodies: ordinary `*ast.FuncDecl`
	// entries and `var name = func(...) {...}` package-level value specs
	// (a `*ast.GenDecl` holding `*ast.FuncLit` initializers). Both are
	// real sync entry points in printed CLIs; ast.Inspect would have
	// found their calls under the old implementation but the explicit
	// decl walk drops the GenDecl path unless we handle it here.
	// Index same-file top-level helpers by name. The named-map resolver
	// follows `helperFn(mapName, ...)` calls — the `populate*` / `apply*`
	// pattern hand-authored syncers reach for when key assignment grows
	// past the inline `if/else` block — into the helper's body to union
	// the keys it adds. Same-file only; same-package callees in other
	// files would require a parser pass over those files and are out of
	// scope for this gate's conservative-skip-on-uncertainty rule.
	helpers := sameFileHelpers(file)
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Body == nil {
				continue
			}
			walkBlockForSyncParamDropCalls(fset, d.Body, fileName, captured, suppressionLines, helpers, result)
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, v := range vs.Values {
					if fl, ok := v.(*ast.FuncLit); ok && fl.Body != nil {
						walkBlockForSyncParamDropCalls(fset, fl.Body, fileName, captured, suppressionLines, helpers, result)
					}
				}
			}
		}
	}
}

// sameFileHelpers indexes the file's top-level *ast.FuncDecl entries by
// name so the named-map resolver can look up a helper called as
// `name(...)`. Methods (receivers present) are excluded: a `(*Client).Set`
// call doesn't fit the bare-Ident call shape resolveNamedMapKeys looks
// for, and including methods would invite name collisions where a
// function and a method share a base name.
func sameFileHelpers(file *ast.File) map[string]*ast.FuncDecl {
	out := make(map[string]*ast.FuncDecl)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Recv != nil || fn.Name == nil {
			continue
		}
		// First wins. Multiple top-level functions with the same name
		// don't compile, so a duplicate here is a parse-time artifact we
		// can ignore.
		if _, exists := out[fn.Name.Name]; !exists {
			out[fn.Name.Name] = fn
		}
	}
	return out
}

// walkBlockForSyncParamDropCalls walks a single function/closure body
// looking for recognized client calls. The `body` parameter is the
// scope used for named-map resolution: when the walker encounters a
// nested `*ast.FuncLit`, it recurses with the FuncLit's own Body so
// calls inside the closure resolve their named-map args against the
// closure's scope, not the outer function's. Without this, an inner
// `params := map[string]string{...}` that shadows an outer same-named
// map would have its key set silently unioned with the outer map's,
// hiding real drops inside the closure.
func walkBlockForSyncParamDropCalls(fset *token.FileSet, body *ast.BlockStmt, fileName string, captured capturedKeysIndex, suppressionLines map[int]bool, helpers map[string]*ast.FuncDecl, result *SyncParamDropResult) {
	if body == nil {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if fl, ok := n.(*ast.FuncLit); ok {
			// Recurse into the nested closure with its own body as the
			// scope, then stop ast.Inspect from descending into it
			// under the outer body's scope.
			walkBlockForSyncParamDropCalls(fset, fl.Body, fileName, captured, suppressionLines, helpers, result)
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		method, ok := httpMethodForCall(call)
		if !ok {
			return true
		}
		if len(call.Args) < 1 {
			return true
		}
		path, ok := syncParamStringLiteral(call.Args[0])
		if !ok {
			return true
		}
		path = canonicalSyncPath(path)
		if path == "" {
			return true
		}
		passedKeys := callPassedKeys(call.Args[1:], body, call.Pos(), helpers)
		// Bodies / params that don't parse into a key set produce no
		// signal — silently skip rather than guessing.
		if passedKeys == nil {
			return true
		}

		result.Checked++

		callLine := fset.Position(call.Pos()).Line
		if suppressionLines[callLine-1] {
			result.Suppressed++
			return true
		}

		capturedKeys, present := captured[method+" "+path]
		if !present {
			return true
		}

		dropped := stringSliceDifference(capturedKeys, passedKeys)
		if len(dropped) == 0 {
			return true
		}
		// Only flag when capture is a STRICT superset: every passed key
		// is also in the capture. A call that passes a key the capture
		// never observed is an exotic mode (probably an internal-only
		// flag the live UI never used) — not the drift this gate
		// catches.
		if !stringSliceIsSubset(passedKeys, capturedKeys) {
			return true
		}
		result.Findings = append(result.Findings, SyncParamDropFinding{
			File:         fileName,
			Line:         callLine,
			Method:       method,
			Path:         path,
			PassedKeys:   append([]string(nil), passedKeys...),
			CapturedKeys: append([]string(nil), capturedKeys...),
			DroppedKeys:  dropped,
		})
		return true
	})
}

// httpMethodForCall recognizes the shapes hand-authored sync code uses
// to dial through the generated client: `client.Get(...)`, `c.Post(...)`,
// `s.client.Patch(...)`, etc. Selector chains of arbitrary depth are
// accepted as long as the trailing identifier is one of the canonical
// HTTP-method names. Names that collide with HTTP methods on unrelated
// receivers (e.g. `cmd.Get`) are filtered by requiring the receiver
// chain's leaf identifier to contain `client` or be a single bare
// identifier (the common `c` / `s` shapes); this leans toward false
// negatives over false positives.
func httpMethodForCall(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	name := sel.Sel.Name
	switch name {
	case "Get", "Post", "Put", "Patch", "Delete":
	default:
		return "", false
	}
	if !receiverLooksLikeHTTPClient(sel.X) {
		return "", false
	}
	return strings.ToUpper(name), true
}

func receiverLooksLikeHTTPClient(expr ast.Expr) bool {
	switch v := expr.(type) {
	case *ast.Ident:
		// Bare-identifier receivers (`c`, `client`, `s`) are accepted.
		// Disambiguates against, e.g., `cmd.Get` where Cobra's `cmd` is
		// a frequent name — but `cmd` itself isn't on the short-identifier
		// allowlist below; only true HTTP-client conventional names.
		// `"h"` is intentionally excluded: an `*http.Client` named `h`
		// uses the stdlib `(*http.Client).Get(url)` shape, which has
		// no params arg — callPassedKeys would treat it as an explicit
		// zero-key call and flag every captured key as dropped. Same
		// reason `"http"` was dropped earlier (bare one-arg stdlib
		// `Get` shape produces false positives).
		switch strings.ToLower(v.Name) {
		case "c", "s", "client", "api":
			return true
		}
		return strings.Contains(strings.ToLower(v.Name), "client")
	case *ast.SelectorExpr:
		// Walk one level deeper: `s.client.Get(...)`.
		if strings.Contains(strings.ToLower(v.Sel.Name), "client") {
			return true
		}
		return receiverLooksLikeHTTPClient(v.X)
	}
	return false
}

// callPassedKeys extracts the param-or-body key set from the remaining
// arguments of a recognized client call. Three shapes are supported:
//
//   - A composite literal map[string]<T>{ "a": ..., "b": ... } — common
//     for query params and form/JSON bodies built inline.
//   - A struct literal whose field names form the key set —
//     `MenuParams{Week: ..., Country: ...}` — common for typed param
//     containers.
//   - A named local variable that holds a map built up with an initial
//     literal and subsequent `m["k"] = v` assignments — the standard Go
//     pattern for conditional query params. The walker follows the
//     ident back to its declaration in the same function scope and
//     unions the initial keys with every string-keyed assignment that
//     precedes the call site. Keys added inside `if`/`else`/`switch`
//     branches count as present (loud-when-uncertain beats mute):
//     false-positive risk on a never-taken branch is far smaller than
//     the false-negative this resolution exists to close.
//   - The same named-local pattern where keys are added by a same-file
//     helper called as `populateMenuParams(menuParams, ...)`. The
//     resolver follows the helper's body, identifies the parameter
//     positionally matched to the tracked map ident, and unions every
//     `paramName["k"] = v` literal assignment it finds. Same-file
//     helpers only; no transitive chains. See resolveNamedMapKeys for
//     the conservative-skip rules.
//
// nil return means "no recognizable key set" and the call is not
// counted toward Checked. An empty (but non-nil) slice means "explicit
// zero-key call" which is still counted: the capture for the same path
// may hold keys, in which case all of them are reported as dropped.
//
// Out of scope (would need broader analysis): for-range population,
// helpers defined in other files of the same package, transitive
// helper chains, or alias chains (`m2 := m1`). Those remain silent
// skips, captured as known limitations.
func callPassedKeys(args []ast.Expr, scope *ast.BlockStmt, callPos token.Pos, helpers map[string]*ast.FuncDecl) []string {
	if len(args) == 0 {
		return []string{}
	}
	for _, arg := range args {
		if keys, ok := extractCompositeLiteralKeys(arg); ok {
			return keys
		}
		if ident, ok := arg.(*ast.Ident); ok && scope != nil {
			if keys, ok := resolveNamedMapKeys(scope, ident.Name, callPos, helpers); ok {
				return keys
			}
		}
	}
	// No composite literal we can read; if the only remaining arg is a
	// nil-shaped placeholder, that's an explicit empty.
	if slices.ContainsFunc(args, isNilArg) {
		return []string{}
	}
	return nil
}

// resolveNamedMapKeys follows a named-map identifier back to its
// declaration in the same scope (function body or closure body) and
// collects the full key set: initial composite literal + every
// `name["k"] = v` assignment that precedes callPos in source order.
// Returns (keys, false) when the ident has no recognizable declaration
// (e.g. it's a parameter, a closure capture, or a non-map type) —
// caller treats false as "skip," matching the legacy behavior for
// unrecognized shapes.
//
// Nested `*ast.FuncLit` bodies are NOT descended into: a same-named
// map inside an inner closure is a separate binding, and unioning its
// keys with the outer map would hide real drops inside the closure.
// Each closure is walked independently with its own body as the scope
// by walkBlockForSyncParamDropCalls.
func resolveNamedMapKeys(scope *ast.BlockStmt, name string, callPos token.Pos, helpers map[string]*ast.FuncDecl) ([]string, bool) {
	if scope == nil {
		return nil, false
	}
	var (
		found    bool
		keys     []string
		seen     = make(map[string]struct{})
		declSeen bool
	)
	addKey := func(k string) {
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	ast.Inspect(scope, func(n ast.Node) bool {
		if n == nil || n.Pos() >= callPos {
			// Stop at the call site: later assignments don't reflect
			// what the call actually passes.
			return false
		}
		// Do not descend into nested function literals — they introduce
		// a new scope, and any same-named map inside is a separate
		// binding from the one we're resolving.
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		// Helper-function enrichment: `helperFn(name, ...)` calls in
		// scope before the client call. The helper's body may carry
		// `paramName["k"] = v` assignments that contribute to the key
		// set; resolveHelperEnrichmentKeys handles the positional
		// matching and conservative-skip rules.
		if call, ok := n.(*ast.CallExpr); ok && helpers != nil {
			if enrichKeys, ok := resolveHelperEnrichmentKeys(call, name, helpers); ok {
				for _, k := range enrichKeys {
					addKey(k)
				}
				found = true
				// Helper enrichment does not by itself prove declSeen:
				// the helper writes into a map declared somewhere else,
				// and the named-map resolver still needs a local
				// declaration or full-replacement literal to call the
				// initial-state known. The declSeen gate below stays.
			}
		}
		switch s := n.(type) {
		case *ast.AssignStmt:
			// `name := map[string]X{...}` or `name = map[string]X{...}`
			// — initial value or full-replacement assignment.
			for i, lhs := range s.Lhs {
				lhsIdent, ok := lhs.(*ast.Ident)
				if !ok || lhsIdent.Name != name {
					continue
				}
				if i >= len(s.Rhs) {
					continue
				}
				rhs := s.Rhs[i]
				if litKeys, ok := extractCompositeLiteralKeys(rhs); ok {
					found = true
					declSeen = true
					for _, k := range litKeys {
						addKey(k)
					}
				}
			}
			// `name["k"] = v` — index assignment adding a key.
			if len(s.Lhs) == 1 && len(s.Rhs) == 1 {
				if idx, ok := s.Lhs[0].(*ast.IndexExpr); ok {
					if xIdent, ok := idx.X.(*ast.Ident); ok && xIdent.Name == name {
						if keyLit, ok := idx.Index.(*ast.BasicLit); ok && keyLit.Kind == token.STRING {
							if k, ok := syncParamStringLiteral(keyLit); ok {
								found = true
								addKey(k)
							}
						}
					}
				}
			}
		case *ast.GenDecl:
			// `var name = map[string]X{...}` or `var name map[string]X`.
			if s.Tok != token.VAR {
				return true
			}
			for _, spec := range s.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, nameIdent := range vs.Names {
					if nameIdent.Name != name || i >= len(vs.Values) {
						continue
					}
					if litKeys, ok := extractCompositeLiteralKeys(vs.Values[i]); ok {
						found = true
						declSeen = true
						for _, k := range litKeys {
							addKey(k)
						}
					}
				}
			}
		}
		return true
	})
	if !found {
		return nil, false
	}
	// If we only saw index assignments without ever seeing a declaration
	// or full-replacement literal in this function, the ident likely
	// refers to a map declared in an outer scope (a parameter, a struct
	// field, a closure capture). The keys we collected are real but the
	// initial-state of the map is unknown — skip to avoid a confidently
	// wrong key set. The declaration-present case is the named-map
	// pattern this resolver was written for.
	if !declSeen {
		return nil, false
	}
	sort.Strings(keys)
	return keys, true
}

// resolveHelperEnrichmentKeys detects calls of the form `helperFn(name, ...)`
// — i.e. the tracked map ident appears as one of the helper's positional
// arguments — and walks the helper's body for direct
// `paramName["k"] = v` literal assignments, where `paramName` is the
// helper's parameter name at the matching position. Returns
// (keys, false) when no helper match is found or any of the
// conservative-skip conditions fire; the caller treats false as "no
// enrichment to add."
//
// The same-file lookup is sufficient for the hand-authored sync patterns
// the gate targets — `populateMenuParams` / `applyMenuFilters` style
// helpers usually sit next to the calling sync function. Cross-file
// callees would require a parser pass over every file in the package
// and are intentionally out of scope; #1875 captures them as a follow-up.
//
// Conservative-skip rules (any one returns false):
//   - The call's Fun is not a bare Ident (selector calls like `s.populate(...)`
//     are skipped because we'd need package-level method resolution).
//   - The helper isn't in the same-file index.
//   - The helper has no parameter at the position the named ident
//     appears, or zero positions match (the ident is in a variadic tail,
//     a struct literal field, etc.).
//   - The matching parameter has no name (`_` placeholder) or the field
//     declares multiple names sharing one type — we can't resolve which
//     name corresponds to the matched position without type info.
//   - The helper's body itself transitively calls another same-file
//     helper that touches the parameter. One level only; nested chains
//     remain a silent skip.
//
// Loud-when-uncertain still applies to the keys we DO collect: an
// `if/else`-guarded `paramName["debug"] = "1"` counts as present even
// if the runtime branch is never taken — same logic as the inline
// named-map resolver.
func resolveHelperEnrichmentKeys(call *ast.CallExpr, name string, helpers map[string]*ast.FuncDecl) ([]string, bool) {
	calleeIdent, ok := call.Fun.(*ast.Ident)
	if !ok {
		return nil, false
	}
	helper, ok := helpers[calleeIdent.Name]
	if !ok || helper == nil || helper.Body == nil {
		return nil, false
	}
	// Find the positional index of the tracked map among the call's
	// arguments. We accept the first match — passing the same ident
	// twice is unusual and either position resolving identically is
	// fine.
	argIdx := -1
	for i, arg := range call.Args {
		ident, ok := arg.(*ast.Ident)
		if !ok {
			continue
		}
		if ident.Name == name {
			argIdx = i
			break
		}
	}
	if argIdx < 0 {
		return nil, false
	}
	paramName := paramNameAtPosition(helper.Type.Params, argIdx)
	if paramName == "" {
		return nil, false
	}
	// Reject helpers that themselves dispatch through another helper
	// touching the param. Without recursion tracking the keys we'd add
	// could be silently incomplete; conservative-skip-on-uncertainty
	// keeps the gate's false-positive rate low.
	if helperBodyDispatchesThroughHelper(helper.Body, paramName, helpers) {
		return nil, false
	}
	var (
		seen = make(map[string]struct{})
		keys []string
	)
	addKey := func(k string) {
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	ast.Inspect(helper.Body, func(n ast.Node) bool {
		// Don't descend into nested closures — they introduce a fresh
		// scope where paramName may be shadowed.
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		idx, ok := assign.Lhs[0].(*ast.IndexExpr)
		if !ok {
			return true
		}
		xIdent, ok := idx.X.(*ast.Ident)
		if !ok || xIdent.Name != paramName {
			return true
		}
		keyLit, ok := idx.Index.(*ast.BasicLit)
		if !ok || keyLit.Kind != token.STRING {
			return true
		}
		if k, ok := syncParamStringLiteral(keyLit); ok {
			addKey(k)
		}
		return true
	})
	if len(keys) == 0 {
		return nil, false
	}
	sort.Strings(keys)
	return keys, true
}

// paramNameAtPosition returns the parameter name at the given positional
// index across a *ast.FieldList. Returns "" when the position is out of
// range, falls on an unnamed parameter, or the field at that position
// declares multiple names sharing one type (`func(a, b string, m
// map[string]string)` — position 2's "m" is resolvable; positions 0 and
// 1 share a Field, and we'd need to decide whether index 0 names `a` or
// `b` — the field declaration order is preserved so we count off names
// within each Field. The caller treats "" as a conservative skip.
func paramNameAtPosition(params *ast.FieldList, position int) string {
	if params == nil {
		return ""
	}
	pos := 0
	for _, field := range params.List {
		nameCount := len(field.Names)
		if nameCount == 0 {
			// Anonymous parameter — single position, no name we can use.
			if pos == position {
				return ""
			}
			pos++
			continue
		}
		for _, n := range field.Names {
			if pos == position {
				if n == nil {
					return ""
				}
				return n.Name
			}
			pos++
		}
	}
	return ""
}

// helperBodyDispatchesThroughHelper returns true when the helper body
// passes paramName as an argument to another same-file helper. Used to
// short-circuit transitive chains where the gate would otherwise report
// an incomplete key set — the gate prefers a silent skip to a confidently
// wrong "passed" set.
func helperBodyDispatchesThroughHelper(body *ast.BlockStmt, paramName string, helpers map[string]*ast.FuncDecl) bool {
	if body == nil {
		return false
	}
	dispatches := false
	ast.Inspect(body, func(n ast.Node) bool {
		// Skip nested closures — paramName may be shadowed inside them,
		// matching the symmetric guard in resolveHelperEnrichmentKeys so
		// the two passes agree on what counts as a same-scope dispatch.
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		calleeIdent, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		if _, isHelper := helpers[calleeIdent.Name]; !isHelper {
			return true
		}
		for _, arg := range call.Args {
			if ident, ok := arg.(*ast.Ident); ok && ident.Name == paramName {
				dispatches = true
				return false
			}
		}
		return true
	})
	return dispatches
}

func extractCompositeLiteralKeys(expr ast.Expr) ([]string, bool) {
	// Unwrap `&Foo{...}` and `*Foo{...}` (rare) to the inner literal.
	if unary, ok := expr.(*ast.UnaryExpr); ok {
		return extractCompositeLiteralKeys(unary.X)
	}
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	var keys []string
	mapShape := false
	if _, isMap := lit.Type.(*ast.MapType); isMap {
		mapShape = true
		// Initialize to a non-nil empty slice so an empty map literal
		// (`map[string]string{}`) flows through as the "explicit
		// zero-key call" signal: the walker counts it toward Checked
		// and every captured key for the same path is reported as
		// dropped. Without this, `keys` would stay nil when Elts is
		// empty, the function would return (nil, true), and the
		// walker's `passedKeys == nil` guard would silently bypass
		// the gate — the exact false negative this gate is designed
		// to catch.
		keys = []string{}
	}
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			// Positional struct literal — we can't recover field names
			// without type info, so bail rather than guess.
			return nil, false
		}
		switch k := kv.Key.(type) {
		case *ast.BasicLit:
			if mapShape {
				if v, ok := syncParamStringLiteral(k); ok {
					keys = append(keys, v)
				}
			}
		case *ast.Ident:
			if mapShape {
				return nil, false
			}
			// Struct-literal field name. We accept the Go field name
			// verbatim; sync code typically picks Go field names that
			// match wire keys (Week -> `week`, ProductSku -> `product-sku`)
			// but the gate normalizes both sides to lower-case-with-dashes
			// at compare time.
			keys = append(keys, k.Name)
		}
	}
	if !mapShape && len(keys) == 0 {
		return nil, false
	}
	return keys, true
}

func isNilArg(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}
	return ident.Name == "nil"
}

// syncParamStringLiteral returns the unquoted string value of a basic
// literal and whether the input was a string literal at all. Local to
// the gate so the AST walk can distinguish "not a string literal"
// (return false) from "the literal empty string" (return "", true)
// without piggybacking on runtime_annotations.go's stringLiteralValue,
// which collapses those two cases.
func syncParamStringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	return stringLiteralValue(expr), true
}

// canonicalSyncPath drops a leading scheme/host (`https://api.example.com/menu` ->
// `/menu`), strips any query string, and ensures the result starts with
// `/`. This is the same shape `EndpointCluster.Path` carries.
func canonicalSyncPath(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil {
			s = u.Path
		}
	}
	if idx := strings.Index(s, "?"); idx >= 0 {
		s = s[:idx]
	}
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return s
}

// stringSliceDifference returns the keys present in `a` but absent from
// `b`, comparing under the same normalization the gate uses for matching
// Go field names to wire keys.
func stringSliceDifference(a, b []string) []string {
	bSet := make(map[string]struct{}, len(b))
	for _, s := range b {
		bSet[normalizeParamKey(s)] = struct{}{}
	}
	var out []string
	for _, s := range a {
		if _, ok := bSet[normalizeParamKey(s)]; !ok {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func stringSliceIsSubset(a, b []string) bool {
	bSet := make(map[string]struct{}, len(b))
	for _, s := range b {
		bSet[normalizeParamKey(s)] = struct{}{}
	}
	for _, s := range a {
		if _, ok := bSet[normalizeParamKey(s)]; !ok {
			return false
		}
	}
	return true
}

// normalizeParamKey collapses the common stylistic gap between the Go
// field-name side (`ProductSku`, `customerPlanId`) and the wire-key side
// (`product-sku`, `customerPlanId`) so the comparison doesn't false-flag
// on case alone. Dashes and underscores are dropped; the result is
// lowercased. Same path used on both sides.
func normalizeParamKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '-' || r == '_':
			continue
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// FormatSyncParamDropFinding renders one finding as a single line for
// the human-readable CLI output and the dogfood summary string.
func FormatSyncParamDropFinding(f SyncParamDropFinding) string {
	return fmt.Sprintf("%s:%d: %s %s — dropped params: %s",
		f.File, f.Line, f.Method, f.Path, strings.Join(f.DroppedKeys, ", "),
	)
}
