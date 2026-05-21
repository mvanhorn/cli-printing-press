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

	ast.Inspect(file, func(n ast.Node) bool {
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
		passedKeys := callPassedKeys(call.Args[1:])
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
// arguments of a recognized client call. Two shapes are supported:
//
//   - A composite literal map[string]<T>{ "a": ..., "b": ... } — common
//     for query params and form/JSON bodies built inline.
//   - A struct literal whose field names form the key set —
//     `MenuParams{Week: ..., Country: ...}` — common for typed param
//     containers.
//
// nil return means "no recognizable key set" and the call is not
// counted toward Checked. An empty (but non-nil) slice means "explicit
// zero-key call" which is still counted: the capture for the same path
// may hold keys, in which case all of them are reported as dropped.
func callPassedKeys(args []ast.Expr) []string {
	if len(args) == 0 {
		return []string{}
	}
	for _, arg := range args {
		if keys, ok := extractCompositeLiteralKeys(arg); ok {
			return keys
		}
	}
	// No composite literal we can read; if the only remaining arg is a
	// nil-shaped placeholder, that's an explicit empty.
	if slices.ContainsFunc(args, isNilArg) {
		return []string{}
	}
	return nil
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
