package regenmerge

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
)

// detectBodyDrift parses pub and fresh files, walks each function's body,
// and returns the per-function set of call-target identifiers pub uses but
// fresh's same-named function doesn't. Returns nil if there's no drift.
// Conservative — fires only when both files define a function with the
// same canonical name.
func detectBodyDrift(pubPath, freshPath string) *BodyDrift {
	pubFns := bodyCallsByFunc(pubPath)
	if pubFns == nil {
		return nil
	}
	freshFns := bodyCallsByFunc(freshPath)
	if freshFns == nil {
		return nil
	}

	driftFns := map[string][]string{}
	for fn, pubCalls := range pubFns {
		freshCalls, ok := freshFns[fn]
		if !ok {
			// Function exists only in pub. The decl-set check would have
			// flagged that already (TEMPLATED-WITH-ADDITIONS), so we
			// shouldn't be here. Skip.
			continue
		}
		var diff []string
		for call := range pubCalls {
			if _, present := freshCalls[call]; !present {
				diff = append(diff, call)
			}
		}
		if len(diff) > 0 {
			sort.Strings(diff)
			driftFns[fn] = diff
		}
	}
	if len(driftFns) == 0 {
		return nil
	}
	return &BodyDrift{Functions: driftFns}
}

// driftSkipSelectors lists method-name selectors whose argument expressions
// the body-drift walker should NOT recurse into. AddCommand is handled by
// the lost-registration restoration path — re-flagging its arg calls here
// would duplicate that signal and trip false positives on every CLI with
// novel commands.
var driftSkipSelectors = map[string]struct{}{
	"AddCommand": {},
}

// bodyCallsByFunc parses filename and returns a map from canonical function
// name to the set of call-target identifiers in that function's body. For
// `foo()` the target is "foo"; for `pkg.Bar()` or `obj.Bar()` it's "Bar"
// (the selector name — receivers and import aliases differ across files
// but the function identity is the load-bearing signal). Returns nil if
// the file fails to parse.
func bodyCallsByFunc(filename string) map[string]map[string]struct{} {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	out := make(map[string]map[string]struct{}, len(file.Decls))
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		name := canonicalFuncName(fn)
		calls := map[string]struct{}{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fnExpr := ce.Fun.(type) {
			case *ast.Ident:
				calls[fnExpr.Name] = struct{}{}
			case *ast.SelectorExpr:
				if fnExpr.Sel != nil {
					calls[fnExpr.Sel.Name] = struct{}{}
					if _, skip := driftSkipSelectors[fnExpr.Sel.Name]; skip {
						return false
					}
				}
			}
			return true
		})
		out[name] = calls
	}
	return out
}
