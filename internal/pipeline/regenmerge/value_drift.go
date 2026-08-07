package regenmerge

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
)

// valueDriftDisplayLimit caps the per-side rendered text length surfaced in
// reports so a hand-edited 200-line function doesn't dump 400 lines into a
// merge summary. Truncation does not affect the equality check — that walks
// the full rendered text.
const valueDriftDisplayLimit = 120

// detectValueDrift parses pub and fresh files, renders each top-level decl
// (with its Doc comment stripped and AddCommand statements scrubbed from
// function bodies) via go/format, and compares the rendered text per-decl.
// Returns nil if there's no drift. Conservative — fires only when both files
// parse and a same-named decl exists in both.
//
// This catches what body-drift's call-target walker misses:
//   - basic-literal value changes (const x = "Bearer " → "Token ")
//   - identifier renames in any non-call position (cfg.Bearer → cfg.Token)
//   - type conversions, type assertions, composite-literal field changes
//   - any AST shape difference inside a decl that go/format renders
//     differently
//
// AddCommand call statements are scrubbed from function bodies before
// rendering so AddCommand-only differences (which the LostRegistrations path
// handles via re-injection into a fresh-derived TEMPLATED-CLEAN host) do not
// trigger value-drift.
func detectValueDrift(pubPath, freshPath string) *ValueDrift {
	pubDecls := canonicalDeclTexts(pubPath)
	if pubDecls == nil {
		return nil
	}
	freshDecls := canonicalDeclTexts(freshPath)
	if freshDecls == nil {
		return nil
	}

	drift := map[string]ValueDriftDelta{}
	for name, pubText := range pubDecls {
		freshText, ok := freshDecls[name]
		if !ok {
			// Decl exists only in pub. The decl-set check upstream already
			// flagged this as TEMPLATED-WITH-ADDITIONS, so we shouldn't be
			// here. Skip defensively.
			continue
		}
		if pubText == freshText {
			continue
		}
		drift[name] = ValueDriftDelta{
			Published: shortenForDisplay(pubText),
			Fresh:     shortenForDisplay(freshText),
		}
	}
	if len(drift) == 0 {
		return nil
	}
	return &ValueDrift{Decls: drift}
}

// canonicalDeclTexts parses filename and returns a map from canonical decl
// name to the canonically formatted text for that decl. Doc comments are
// stripped so comment-only diffs don't trigger drift; AddCommand statements
// are stripped from function bodies so AddCommand-only diffs route through
// LostRegistrations instead. Returns nil if the file fails to parse.
//
// Mutates the parsed AST in place; safe because each call owns the file via
// a fresh token.FileSet and parser.ParseFile, and the AST is discarded after
// rendering.
func canonicalDeclTexts(filename string) map[string]string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}

	out := make(map[string]string, len(file.Decls))
	for _, d := range file.Decls {
		var name string
		switch decl := d.(type) {
		case *ast.FuncDecl:
			name = canonicalFuncName(decl)
			decl.Doc = nil
			if decl.Body != nil {
				decl.Body.List = stripAddCommandStmts(decl.Body.List)
			}
		case *ast.GenDecl:
			name = genDeclName(decl)
			decl.Doc = nil
			for _, spec := range decl.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					s.Doc = nil
					s.Comment = nil
				case *ast.ValueSpec:
					s.Doc = nil
					s.Comment = nil
				case *ast.ImportSpec:
					s.Doc = nil
					s.Comment = nil
				}
			}
		default:
			continue
		}
		if name == "" {
			continue
		}
		text, err := canonicalRender(fset, d)
		if err != nil {
			continue
		}
		out[name] = text
	}
	return out
}

// canonicalRender returns the gofmt-canonical text for an AST node. Renders
// via go/format, then re-runs the output through format.Source (wrapped in
// a synthetic package so format.Source accepts it) to strip blank-line and
// other whitespace artifacts that go/format preserves from the original
// FileSet positions. The two-step canonicalization makes the comparison
// whitespace-insensitive without losing semantic detail.
func canonicalRender(fset *token.FileSet, node ast.Node) (string, error) {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, node); err != nil {
		return "", err
	}
	wrapped := append([]byte("package _\n"), buf.Bytes()...)
	formatted, err := format.Source(wrapped)
	if err != nil {
		// Fall back to the un-canonicalized render if format.Source can't
		// parse the wrapped output (extremely unusual — Node already
		// produced syntactically valid Go).
		return buf.String(), nil
	}
	formatted = bytes.TrimPrefix(formatted, []byte("package _\n"))
	// Removing AST statements leaves their original token positions behind;
	// gofmt retains those empty lines. Canonical comparison is explicitly
	// whitespace-insensitive, so collapse them after formatting.
	text := string(bytes.TrimSpace(formatted))
	for strings.Contains(text, "\n\n") {
		text = strings.ReplaceAll(text, "\n\n", "\n")
	}
	return text, nil
}

// stripAddCommandStmts removes top-level AddCommand call statements from a
// function body's statement list. AddCommand call additions are handled by
// the LostRegistrations re-injection path, not by drift detection — leaving
// them in the comparison would route every templated host file with a
// hand-added subcommand through TEMPLATED-VALUE-DRIFT (preserve pub) and
// silently disable the re-injection path.
func stripAddCommandStmts(stmts []ast.Stmt) []ast.Stmt {
	out := make([]ast.Stmt, 0, len(stmts))
	for _, stmt := range stmts {
		if isAddCommandASTStmt(stmt) || isNovelCommandsHookASTStmt(stmt) || isClientHooksASTStmt(stmt) {
			continue
		}
		out = append(out, stmt)
	}
	return out
}

// isNovelCommandsHookASTStmt recognizes the generated optional-extension hook.
// The hook is structural generator evolution, not authored value drift; keeping
// it in the comparison would make a cross-spec force regen retain the old root
// and strand an otherwise preserved markerless command source without wiring.
func isNovelCommandsHookASTStmt(stmt ast.Stmt) bool {
	ifStmt, ok := stmt.(*ast.IfStmt)
	if ok && ifStmt.Else == nil && len(ifStmt.Body.List) == 1 {
		expr, ok := ifStmt.Body.List[0].(*ast.ExprStmt)
		if ok {
			call, ok := expr.X.(*ast.CallExpr)
			if ok && isIdent(call.Fun, "novelCommands") {
				return true
			}
		}
	}
	forStmt, ok := stmt.(*ast.RangeStmt)
	if !ok || !isIdent(forStmt.X, "novelCommandHooks") || len(forStmt.Body.List) != 1 {
		return false
	}
	expr, ok := forStmt.Body.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := expr.X.(*ast.CallExpr)
	return ok && isIdent(call.Fun, "hook")
}

// isClientHooksASTStmt recognizes the generated client-extension loop. It is
// template evolution, not authored value drift; otherwise a force regen would
// retain an older root that has no way to run a preserved package extension.
func isClientHooksASTStmt(stmt ast.Stmt) bool {
	forStmt, ok := stmt.(*ast.RangeStmt)
	if !ok || !isIdent(forStmt.X, "clientHooks") || len(forStmt.Body.List) != 1 {
		return false
	}
	ifStmt, ok := forStmt.Body.List[0].(*ast.IfStmt)
	if !ok || ifStmt.Init == nil || len(ifStmt.Body.List) != 1 {
		return false
	}
	assign, ok := ifStmt.Init.(*ast.AssignStmt)
	if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 || !isIdent(assign.Lhs[0], "err") {
		return false
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	return ok && isIdent(call.Fun, "hook")
}

func isIdent(expr ast.Expr, name string) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == name
}

// isAddCommandASTStmt reports whether stmt is an `<recv>.AddCommand(...)`
// call statement. Operates on go/ast (apply.go has a dave/dst counterpart
// for the rewriter that lives there).
func isAddCommandASTStmt(stmt ast.Stmt) bool {
	es, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	ce, ok := es.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return false
	}
	return sel.Sel.Name == "AddCommand"
}

// genDeclName returns a canonical key for a *ast.GenDecl. For single-spec
// decls (the common case for top-level const/var/type/import) the key is
// the spec's name. For multi-spec decls (`var ( x = 1; y = 2 )`), the key
// is a stable concatenation of all names so the whole block compares as a
// unit. Import-only decls return "" and are skipped — import lists evolve
// freely and are not load-bearing for hand-edit detection.
func genDeclName(decl *ast.GenDecl) string {
	if decl == nil || len(decl.Specs) == 0 {
		return ""
	}
	if decl.Tok == token.IMPORT {
		return ""
	}
	var names []string
	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			if s.Name != nil {
				names = append(names, s.Name.Name)
			}
		case *ast.ValueSpec:
			for _, n := range s.Names {
				names = append(names, n.Name)
			}
		}
	}
	if len(names) == 0 {
		return ""
	}
	return decl.Tok.String() + ":" + strings.Join(names, ",")
}

func shortenForDisplay(s string) string {
	if len(s) <= valueDriftDisplayLimit {
		return s
	}
	return s[:valueDriftDisplayLimit] + "..."
}
