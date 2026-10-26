package pipeline

import (
	"go/ast"
	"go/token"
	"strconv"
)

// A package-level name is exempt only when every use in the package is a
// string affix, or it only initializes another exempt name. A fetch in any
// file keeps the host. Local const and var follow the same rule. Sibling
// files use their own FileSet, so only file's positions are recorded.

type useMode int

const (
	modeOther useMode = iota
	modeAffix
	modeAlias
)

type affixUses struct {
	affix int
	other int
}

type affixBinding struct {
	file      *ast.File
	rhs       ast.Expr
	uses      affixUses
	aliasVias []*affixBinding
}

type affixScope struct {
	parent *affixScope
	names  map[string]*affixBinding
}

type affixWalker struct {
	file   *ast.File
	target *ast.File
	pkg    *affixScope
	lits   map[token.Pos]struct{}
	locals *[]*affixBinding
}

func vocabularyAffixLiterals(file *ast.File, siblings []*ast.File) map[token.Pos]struct{} {
	lits := map[token.Pos]struct{}{}
	if file == nil {
		return lits
	}
	pkg := &affixScope{names: map[string]*affixBinding{}}
	files := make([]*ast.File, 0, 1+len(siblings))
	seen := map[*ast.File]bool{}
	for _, f := range append([]*ast.File{file}, siblings...) {
		if f == nil || seen[f] {
			continue
		}
		seen[f] = true
		files = append(files, f)
		collectPackageAffixBindings(pkg, f)
	}
	var locals []*affixBinding
	for _, f := range files {
		w := &affixWalker{file: f, target: file, pkg: pkg, lits: lits, locals: &locals}
		w.walkFile()
	}
	exemptAffixLiterals(pkg, locals, file, lits)
	return lits
}

func collectPackageAffixBindings(scope *affixScope, file *ast.File) {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
			continue
		}
		var prev []ast.Expr
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			prev = bindPackageSpec(scope, file, gd.Tok, vs, prev)
		}
	}
}

func bindPackageSpec(scope *affixScope, file *ast.File, tok token.Token, vs *ast.ValueSpec, prev []ast.Expr) []ast.Expr {
	if len(vs.Values) == 0 && tok == token.CONST && len(prev) == len(vs.Names) {
		for i, name := range vs.Names {
			addPackageBinding(scope, file, name, prev[i])
		}
		return prev
	}
	if len(vs.Values) == len(vs.Names) && len(vs.Values) > 0 {
		for i, name := range vs.Names {
			addPackageBinding(scope, file, name, vs.Values[i])
		}
		return vs.Values
	}
	for _, name := range vs.Names {
		addPackageBinding(scope, file, name, nil)
	}
	if len(vs.Values) > 0 {
		return vs.Values
	}
	return prev
}

func addPackageBinding(scope *affixScope, file *ast.File, name *ast.Ident, rhs ast.Expr) {
	if name == nil || name.Name == "" || name.Name == "_" {
		return
	}
	if existing, ok := scope.names[name.Name]; ok {
		existing.uses.other++
		existing.rhs = nil
		return
	}
	scope.names[name.Name] = &affixBinding{file: file, rhs: rhs}
}

func (s *affixScope) lookup(name string) *affixBinding {
	for s != nil {
		if b, ok := s.names[name]; ok {
			return b
		}
		s = s.parent
	}
	return nil
}

func (s *affixScope) bindings() []*affixBinding {
	if s == nil {
		return nil
	}
	out := make([]*affixBinding, 0, len(s.names))
	for _, b := range s.names {
		out = append(out, b)
	}
	return out
}

func childAffixScope(parent *affixScope) *affixScope {
	return &affixScope{parent: parent, names: map[string]*affixBinding{}}
}

func exemptAffixLiterals(pkg *affixScope, locals []*affixBinding, file *ast.File, lits map[token.Pos]struct{}) {
	all := append(pkg.bindings(), locals...)
	tainted := map[*affixBinding]bool{}
	for _, b := range all {
		if b != nil && b.uses.other > 0 {
			tainted[b] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, b := range all {
			if b == nil || tainted[b] {
				continue
			}
			for _, via := range b.aliasVias {
				if tainted[via] {
					tainted[b] = true
					changed = true
					break
				}
			}
		}
	}
	// A const group copies the previous expression. Fetching any name that
	// owns that expression keeps the literal checked.
	taintedRHS := map[ast.Expr]bool{}
	for _, b := range all {
		if b != nil && b.rhs != nil && tainted[b] {
			taintedRHS[b.rhs] = true
		}
	}
	for _, b := range all {
		if b == nil || b.file != file || tainted[b] || taintedRHS[b.rhs] {
			continue
		}
		if b.uses.affix == 0 && len(b.aliasVias) == 0 {
			continue
		}
		collectAffixPieces(b.rhs, lits)
	}
}

func (w *affixWalker) walkFile() {
	scope := w.importScope(w.pkg)
	for _, decl := range w.file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			w.walkFunc(d, scope)
		case *ast.GenDecl:
			w.walkPackageValues(d, scope)
		}
	}
}

func (w *affixWalker) importScope(parent *affixScope) *affixScope {
	scope := childAffixScope(parent)
	if w.file == nil {
		return scope
	}
	for _, imp := range w.file.Imports {
		name := importName(imp)
		if name == "" || name == "_" || name == "." {
			continue
		}
		scope.names[name] = &affixBinding{file: w.file}
	}
	return scope
}

func importName(imp *ast.ImportSpec) string {
	if imp == nil {
		return ""
	}
	if imp.Name != nil {
		return imp.Name.Name
	}
	if imp.Path == nil {
		return ""
	}
	path, err := strconv.Unquote(imp.Path.Value)
	if err != nil {
		return ""
	}
	return pathBase(path)
}

func (w *affixWalker) walkPackageValues(gd *ast.GenDecl, scope *affixScope) {
	if gd.Tok != token.CONST && gd.Tok != token.VAR {
		return
	}
	var prev []ast.Expr
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		// A const spec with no expression list repeats the previous list.
		values := vs.Values
		if len(values) == 0 && gd.Tok == token.CONST && len(prev) == len(vs.Names) {
			values = prev
		} else if len(vs.Values) > 0 {
			prev = vs.Values
		}
		if len(values) == 0 {
			continue
		}
		if len(vs.Names) == len(values) {
			for i, name := range vs.Names {
				if name == nil {
					continue
				}
				b := w.pkg.lookup(name.Name)
				if b != nil && b.file == w.file && b.rhs == values[i] {
					w.walkExpr(values[i], scope, modeAlias, b)
					continue
				}
				w.walkExpr(values[i], scope, modeOther, nil)
			}
			continue
		}
		for _, val := range values {
			w.walkExpr(val, scope, modeOther, nil)
		}
	}
}

func (w *affixWalker) walkFunc(fn *ast.FuncDecl, parent *affixScope) {
	if fn == nil {
		return
	}
	scope := childAffixScope(parent)
	if fn.Recv != nil {
		w.bindFields(scope, fn.Recv)
	}
	if fn.Type != nil {
		w.bindFields(scope, fn.Type.Params)
		w.bindFields(scope, fn.Type.Results)
	}
	w.walkBlock(fn.Body, scope)
}

func (w *affixWalker) walkFuncLit(fn *ast.FuncLit, parent *affixScope) {
	if fn == nil {
		return
	}
	scope := childAffixScope(parent)
	if fn.Type != nil {
		w.bindFields(scope, fn.Type.Params)
		w.bindFields(scope, fn.Type.Results)
	}
	w.walkBlock(fn.Body, scope)
}

func (w *affixWalker) bindFields(scope *affixScope, fields *ast.FieldList) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			w.bindShadow(scope, name)
		}
	}
}

func (w *affixWalker) bindShadow(scope *affixScope, name *ast.Ident) {
	if scope == nil || name == nil || name.Name == "" || name.Name == "_" {
		return
	}
	scope.names[name.Name] = &affixBinding{file: w.file}
}

func (w *affixWalker) walkBlock(block *ast.BlockStmt, parent *affixScope) {
	if block == nil {
		return
	}
	scope := childAffixScope(parent)
	for _, stmt := range block.List {
		w.walkStmt(stmt, scope)
	}
}

func (w *affixWalker) walkStmt(stmt ast.Stmt, scope *affixScope) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		w.walkExpr(s.X, scope, modeOther, nil)
	case *ast.ReturnStmt:
		for _, result := range s.Results {
			w.walkExpr(result, scope, modeOther, nil)
		}
	case *ast.GoStmt:
		w.walkCall(s.Call, scope)
	case *ast.DeferStmt:
		w.walkCall(s.Call, scope)
	case *ast.IncDecStmt:
		w.walkExpr(s.X, scope, modeOther, nil)
	case *ast.SendStmt:
		w.walkExpr(s.Chan, scope, modeOther, nil)
		w.walkExpr(s.Value, scope, modeOther, nil)
	case *ast.LabeledStmt:
		w.walkStmt(s.Stmt, scope)
	case *ast.BlockStmt:
		w.walkBlock(s, scope)
	case *ast.AssignStmt:
		w.walkAssign(s, scope)
	case *ast.DeclStmt:
		gd, ok := s.Decl.(*ast.GenDecl)
		if ok {
			w.walkLocalGen(gd, scope)
		}
	case *ast.IfStmt:
		ifScope := scope
		if s.Init != nil {
			ifScope = childAffixScope(scope)
			w.walkStmt(s.Init, ifScope)
		}
		w.walkExpr(s.Cond, ifScope, modeOther, nil)
		w.walkBlock(s.Body, ifScope)
		w.walkStmt(s.Else, ifScope)
	case *ast.ForStmt:
		forScope := scope
		if s.Init != nil {
			forScope = childAffixScope(scope)
			w.walkStmt(s.Init, forScope)
		}
		w.walkExpr(s.Cond, forScope, modeOther, nil)
		w.walkStmt(s.Post, forScope)
		w.walkBlock(s.Body, forScope)
	case *ast.RangeStmt:
		w.walkExpr(s.X, scope, modeOther, nil)
		bodyScope := childAffixScope(scope)
		w.bindShadow(bodyScope, identOf(s.Key))
		w.bindShadow(bodyScope, identOf(s.Value))
		if s.Body != nil {
			for _, inner := range s.Body.List {
				w.walkStmt(inner, bodyScope)
			}
		}
	case *ast.SwitchStmt:
		w.walkSwitch(s, scope)
	case *ast.TypeSwitchStmt:
		w.walkTypeSwitch(s, scope)
	case *ast.SelectStmt:
		if s.Body != nil {
			for _, inner := range s.Body.List {
				w.walkStmt(inner, scope)
			}
		}
	case *ast.CaseClause:
		for _, expr := range s.List {
			w.walkExpr(expr, scope, modeOther, nil)
		}
		caseScope := childAffixScope(scope)
		for _, inner := range s.Body {
			w.walkStmt(inner, caseScope)
		}
	case *ast.CommClause:
		commScope := childAffixScope(scope)
		w.walkStmt(s.Comm, commScope)
		for _, inner := range s.Body {
			w.walkStmt(inner, commScope)
		}
	}
}

func (w *affixWalker) walkSwitch(s *ast.SwitchStmt, scope *affixScope) {
	swScope := scope
	if s.Init != nil {
		swScope = childAffixScope(scope)
		w.walkStmt(s.Init, swScope)
	}
	w.walkExpr(s.Tag, swScope, modeOther, nil)
	if s.Body == nil {
		return
	}
	for _, stmt := range s.Body.List {
		w.walkStmt(stmt, swScope)
	}
}

func (w *affixWalker) walkTypeSwitch(s *ast.TypeSwitchStmt, scope *affixScope) {
	swScope := scope
	if s.Init != nil {
		swScope = childAffixScope(scope)
		w.walkStmt(s.Init, swScope)
	}
	var lhs *ast.Ident
	switch a := s.Assign.(type) {
	case *ast.ExprStmt:
		w.walkExpr(a.X, swScope, modeOther, nil)
	case *ast.AssignStmt:
		for _, rhs := range a.Rhs {
			w.walkExpr(rhs, swScope, modeOther, nil)
		}
		if len(a.Lhs) == 1 {
			lhs, _ = a.Lhs[0].(*ast.Ident)
		}
	}
	if s.Body == nil {
		return
	}
	for _, stmt := range s.Body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok {
			w.walkStmt(stmt, swScope)
			continue
		}
		for _, expr := range clause.List {
			w.walkExpr(expr, swScope, modeOther, nil)
		}
		caseScope := childAffixScope(swScope)
		w.bindShadow(caseScope, lhs)
		for _, inner := range clause.Body {
			w.walkStmt(inner, caseScope)
		}
	}
}

func (w *affixWalker) walkAssign(s *ast.AssignStmt, scope *affixScope) {
	if s.Tok == token.DEFINE && len(s.Lhs) == 1 && len(s.Rhs) == 1 {
		if id, ok := s.Lhs[0].(*ast.Ident); ok && id != nil && id.Name != "" && id.Name != "_" {
			b := &affixBinding{file: w.file, rhs: s.Rhs[0]}
			w.walkExpr(s.Rhs[0], scope, modeAlias, b)
			scope.names[id.Name] = b
			*w.locals = append(*w.locals, b)
			return
		}
	}
	for _, rhs := range s.Rhs {
		w.walkExpr(rhs, scope, modeOther, nil)
	}
	if s.Tok != token.DEFINE {
		for _, lhs := range s.Lhs {
			w.walkExpr(lhs, scope, modeOther, nil)
		}
		return
	}
	for _, lhs := range s.Lhs {
		w.bindShadow(scope, identOf(lhs))
	}
}

func (w *affixWalker) walkLocalGen(gd *ast.GenDecl, scope *affixScope) {
	if gd.Tok != token.CONST && gd.Tok != token.VAR {
		return
	}
	var prev []ast.Expr
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		switch {
		case len(vs.Values) == 0 && gd.Tok == token.CONST && len(prev) == len(vs.Names):
			for i, name := range vs.Names {
				w.bindLocal(scope, name, prev[i])
			}
		case len(vs.Names) == len(vs.Values) && len(vs.Values) > 0:
			for i, name := range vs.Names {
				if name == nil || name.Name == "" || name.Name == "_" {
					continue
				}
				b := &affixBinding{file: w.file, rhs: vs.Values[i]}
				w.walkExpr(vs.Values[i], scope, modeAlias, b)
				scope.names[name.Name] = b
				*w.locals = append(*w.locals, b)
			}
			prev = vs.Values
		default:
			for _, val := range vs.Values {
				w.walkExpr(val, scope, modeOther, nil)
			}
			for _, name := range vs.Names {
				w.bindShadow(scope, name)
			}
			if len(vs.Values) > 0 {
				prev = vs.Values
			}
		}
	}
}

func (w *affixWalker) bindLocal(scope *affixScope, name *ast.Ident, rhs ast.Expr) {
	if name == nil || name.Name == "" || name.Name == "_" {
		return
	}
	b := &affixBinding{file: w.file, rhs: rhs}
	// rhs is the previous const expression this spec omitted.
	w.walkExpr(rhs, scope, modeAlias, b)
	scope.names[name.Name] = b
	*w.locals = append(*w.locals, b)
}

func (w *affixWalker) walkCall(call *ast.CallExpr, scope *affixScope) {
	if call == nil {
		return
	}
	if affix, ok := stringsAffixExpr(call); ok {
		w.walkExpr(call.Fun, scope, modeOther, nil)
		if len(call.Args) > 0 {
			w.walkExpr(call.Args[0], scope, modeOther, nil)
		}
		w.walkExpr(affix, scope, modeAffix, nil)
		for i := 2; i < len(call.Args); i++ {
			w.walkExpr(call.Args[i], scope, modeOther, nil)
		}
		return
	}
	w.walkExpr(call.Fun, scope, modeOther, nil)
	for _, arg := range call.Args {
		w.walkExpr(arg, scope, modeOther, nil)
	}
}

func (w *affixWalker) walkExpr(expr ast.Expr, scope *affixScope, mode useMode, via *affixBinding) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Ident:
		w.noteIdent(e, scope, mode, via)
	case *ast.BasicLit:
		if mode == modeAffix && e.Kind == token.STRING && w.file == w.target {
			w.lits[e.Pos()] = struct{}{}
		}
	case *ast.ParenExpr:
		w.walkExpr(e.X, scope, mode, via)
	case *ast.BinaryExpr:
		next := modeOther
		if e.Op == token.ADD && (mode == modeAffix || mode == modeAlias) {
			next = mode
		}
		w.walkExpr(e.X, scope, next, via)
		w.walkExpr(e.Y, scope, next, via)
	case *ast.CallExpr:
		w.walkCall(e, scope)
	case *ast.SelectorExpr:
		w.walkExpr(e.X, scope, modeOther, nil)
	case *ast.UnaryExpr:
		w.walkExpr(e.X, scope, modeOther, nil)
	case *ast.StarExpr:
		w.walkExpr(e.X, scope, modeOther, nil)
	case *ast.IndexExpr:
		w.walkExpr(e.X, scope, modeOther, nil)
		w.walkExpr(e.Index, scope, modeOther, nil)
	case *ast.SliceExpr:
		w.walkExpr(e.X, scope, modeOther, nil)
		w.walkExpr(e.Low, scope, modeOther, nil)
		w.walkExpr(e.High, scope, modeOther, nil)
		w.walkExpr(e.Max, scope, modeOther, nil)
	case *ast.TypeAssertExpr:
		w.walkExpr(e.X, scope, modeOther, nil)
	case *ast.KeyValueExpr:
		w.walkExpr(e.Key, scope, modeOther, nil)
		w.walkExpr(e.Value, scope, modeOther, nil)
	case *ast.CompositeLit:
		w.walkExpr(e.Type, scope, modeOther, nil)
		for _, elt := range e.Elts {
			w.walkExpr(elt, scope, modeOther, nil)
		}
	case *ast.Ellipsis:
		w.walkExpr(e.Elt, scope, modeOther, nil)
	case *ast.FuncLit:
		w.walkFuncLit(e, scope)
	case *ast.IndexListExpr:
		w.walkExpr(e.X, scope, modeOther, nil)
		for _, idx := range e.Indices {
			w.walkExpr(idx, scope, modeOther, nil)
		}
	default:
		ast.Inspect(e, func(n ast.Node) bool {
			if n == nil || n == e {
				return true
			}
			switch inner := n.(type) {
			case *ast.CallExpr:
				w.walkCall(inner, scope)
				return false
			case *ast.FuncLit:
				w.walkFuncLit(inner, scope)
				return false
			case *ast.Ident:
				w.noteIdent(inner, scope, modeOther, nil)
			}
			return true
		})
	}
}

func (w *affixWalker) noteIdent(id *ast.Ident, scope *affixScope, mode useMode, via *affixBinding) {
	if id == nil || id.Name == "" || id.Name == "_" || scope == nil {
		return
	}
	b := scope.lookup(id.Name)
	if b == nil {
		return
	}
	switch mode {
	case modeAffix:
		b.uses.affix++
	case modeAlias:
		if via != nil && via != b {
			b.aliasVias = append(b.aliasVias, via)
			return
		}
		b.uses.other++
	default:
		b.uses.other++
	}
}

func identOf(expr ast.Expr) *ast.Ident {
	id, _ := expr.(*ast.Ident)
	return id
}
