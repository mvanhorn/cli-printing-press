package pipeline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Hosts often live in a helper the novel command calls, or in a same-package
// file named for that command. Method calls match the receiver type and, for
// an imported type, that package, not the method name alone. Unreferenced
// files stay out of the gate.

type novelSourceFile struct {
	name    string
	content string
	label   string
}

type goSymbolKind int

const (
	goSymbolFunc goSymbolKind = iota
	goSymbolValue
	goSymbolMethod
)

type goSourceFile struct {
	report    string
	fset      *token.FileSet
	file      *ast.File
	generated bool
}

type goSymbol struct {
	kind goSymbolKind
	name string
	recv string
	file *goSourceFile
	node ast.Node
}

type goSourcePkg struct {
	files   []*goSourceFile
	funcs   map[string]*goSymbol
	values  map[string]*goSymbol
	methods map[string][]*goSymbol
}

type srcRef struct {
	kind       string
	name       string
	importPath string
	recv       goTypeRef
}

// goTypeRef is a named type, or a call whose result type is resolved later.
// An empty importPath means the type is in the package being resolved.
type goTypeRef struct {
	name       string
	importPath string
	callName   string
	callImport string
	callRecv   string
}

type helperReach struct {
	file *goSourceFile
	sym  *goSymbol
}

func hostsFromNovelHelpers(cliDir, cliFilesDir string, contents map[string]string, sources []novelSourceFile, leaves map[string]string) []novelHostDecl {
	if len(sources) == 0 {
		return nil
	}
	cliPkg := parseSourcePkg(cliDir, cliFilesDir, contents)
	if cliPkg == nil {
		return nil
	}
	cache := map[string]*goSourcePkg{cliFilesDir: cliPkg}
	module := cliModulePath(cliDir)
	var declared []novelHostDecl
	for _, source := range sources {
		origin := findSourceFile(cliPkg, source.name)
		if origin == nil {
			continue
		}
		declared = append(declared, reachNovelHelpers(cliDir, module, cliPkg, origin, source.label, featureLeaf(source.content, leaves), cache)...)
	}
	return declared
}

func reachNovelHelpers(cliDir, module string, pkg *goSourcePkg, origin *goSourceFile, label, leaf string, cache map[string]*goSourcePkg) []novelHostDecl {
	seen := map[string]bool{}
	var queue []helperReach
	enqueueFile := func(file *goSourceFile) {
		if file == nil || file == origin || file.generated || file.report == "root.go" {
			return
		}
		key := file.report + "\x00*"
		if seen[key] {
			return
		}
		seen[key] = true
		queue = append(queue, helperReach{file: file})
	}
	enqueueSym := func(sym *goSymbol) {
		if sym == nil || sym.file == nil || sym.file == origin || sym.file.generated {
			return
		}
		key := sym.file.report + "\x00" + sym.recv + "." + sym.name
		if seen[key] {
			return
		}
		seen[key] = true
		queue = append(queue, helperReach{file: sym.file, sym: sym})
	}

	follow := func(current *goSourcePkg, file *goSourceFile, node ast.Node, locals map[string]bool) {
		for _, ref := range followNode(node, locals, importAliases(file.file)) {
			for _, sym := range resolveRef(cliDir, module, current, ref, cache) {
				enqueueSym(sym)
			}
		}
	}
	follow(pkg, origin, origin.file, nil)
	if leaf != "" {
		for _, file := range pkg.files {
			if sharesFeatureFile(file.report, leaf) {
				enqueueFile(file)
			}
		}
	}

	var declared []novelHostDecl
	for i := 0; i < len(queue); i++ {
		item := queue[i]
		if item.sym == nil {
			declared = append(declared, hostsInAST(item.file.fset, item.file.report, item.file.file, label)...)
			follow(packageOf(cache, item.file, pkg), item.file, item.file.file, nil)
			continue
		}
		declared = append(declared, hostsInAST(item.file.fset, item.file.report, item.sym.node, label)...)
		locals := map[string]bool{}
		if fn, ok := item.sym.node.(*ast.FuncDecl); ok {
			locals = localsInFunc(fn)
		}
		follow(packageOf(cache, item.file, pkg), item.file, item.sym.node, locals)
	}
	return declared
}

func packageOf(cache map[string]*goSourcePkg, file *goSourceFile, fallback *goSourcePkg) *goSourcePkg {
	for _, pkg := range cache {
		if slices.Contains(pkg.files, file) {
			return pkg
		}
	}
	return fallback
}

func resolveRef(cliDir, module string, pkg *goSourcePkg, ref srcRef, cache map[string]*goSourcePkg) []*goSymbol {
	switch ref.kind {
	case "pkg":
		other := loadImportedPkg(cliDir, module, ref.importPath, cache)
		if other == nil {
			return nil
		}
		if sym := other.funcs[ref.name]; sym != nil {
			return []*goSymbol{sym}
		}
		if sym := other.values[ref.name]; sym != nil {
			return []*goSymbol{sym}
		}
		return nil
	case "method":
		return resolveMethods(cliDir, module, pkg, ref, cache)
	default:
		if sym := pkg.funcs[ref.name]; sym != nil {
			return []*goSymbol{sym}
		}
		if sym := pkg.values[ref.name]; sym != nil {
			return []*goSymbol{sym}
		}
		return nil
	}
}

// resolveMethods returns methods whose name and receiver match the call.
// A same-package method is not selected just because it is the only one
// with that name; the receiver type (or its import path) has to match.
func resolveMethods(cliDir, module string, pkg *goSourcePkg, ref srcRef, cache map[string]*goSourcePkg) []*goSymbol {
	if skipMethodNames[ref.name] || !ref.recv.known() {
		return nil
	}
	recv := concreteType(cliDir, module, pkg, ref.recv, cache)
	if recv.name == "" {
		return nil
	}
	owner := pkg
	if recv.importPath != "" {
		owner = loadImportedPkg(cliDir, module, recv.importPath, cache)
	}
	if owner == nil {
		return nil
	}
	var matched []*goSymbol
	for _, method := range owner.methods[ref.name] {
		if method.recv == recv.name {
			matched = append(matched, method)
		}
	}
	return matched
}

func nodeRefs(node ast.Node, locals map[string]bool, aliases map[string]string) []srcRef {
	if node == nil {
		return nil
	}
	sel := map[*ast.Ident]bool{}
	ast.Inspect(node, func(n ast.Node) bool {
		s, ok := n.(*ast.SelectorExpr)
		if ok {
			sel[s.Sel] = true
		}
		return true
	})
	if fn, ok := node.(*ast.FuncDecl); ok && locals == nil {
		locals = localsInFunc(fn)
	}
	var refs []srcRef
	appendRefs(node, locals, aliases, bindingsFor(node, aliases), sel, &refs)
	return refs
}

func appendRefs(node ast.Node, locals map[string]bool, aliases map[string]string, bindings map[string]goTypeRef, sel map[*ast.Ident]bool, refs *[]srcRef) {
	ast.Inspect(node, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if ok {
			appendLitRefs(lit, locals, aliases, bindings, sel, refs)
			return false
		}
		switch e := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := e.X.(*ast.Ident); ok && aliases[id.Name] != "" && !locals[id.Name] {
				*refs = append(*refs, srcRef{kind: "pkg", name: e.Sel.Name, importPath: aliases[id.Name]})
				return true
			}
			if locals[e.Sel.Name] || goPredeclared[e.Sel.Name] {
				return true
			}
			recv := valueType(e.X, bindings, aliases)
			if !recv.known() {
				return true
			}
			*refs = append(*refs, srcRef{kind: "method", name: e.Sel.Name, recv: recv})
		case *ast.Ident:
			if sel[e] || locals[e.Name] || goPredeclared[e.Name] || e.Name == "_" {
				return true
			}
			*refs = append(*refs, srcRef{kind: "ident", name: e.Name})
		}
		return true
	})
}

func appendLitRefs(lit *ast.FuncLit, locals map[string]bool, aliases map[string]string, outer map[string]goTypeRef, sel map[*ast.Ident]bool, refs *[]srcRef) {
	if lit == nil || lit.Type == nil || lit.Body == nil {
		return
	}
	innerLocals := copyBools(locals)
	addFieldNames(innerLocals, lit.Type.Params)
	addFieldNames(innerLocals, lit.Type.Results)
	markAssigned(lit.Body, innerLocals)
	bindings := copyTypes(outer)
	addFieldTypes(bindings, lit.Type.Params, aliases)
	addFieldTypes(bindings, lit.Type.Results, aliases)
	collectBindings(lit.Body, bindings, aliases)
	appendRefs(lit.Body, innerLocals, aliases, bindings, sel, refs)
}

func followNode(node ast.Node, locals map[string]bool, aliases map[string]string) []srcRef {
	fn, ok := node.(*ast.FuncDecl)
	if ok && locals == nil {
		return nodeRefs(fn, localsInFunc(fn), aliases)
	}
	if file, ok := node.(*ast.File); ok {
		var refs []srcRef
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				refs = append(refs, nodeRefs(d, localsInFunc(d), aliases)...)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, val := range vs.Values {
						refs = append(refs, nodeRefs(val, nil, aliases)...)
					}
				}
			}
		}
		return refs
	}
	return nodeRefs(node, locals, aliases)
}

func localsInFunc(fn *ast.FuncDecl) map[string]bool {
	locals := map[string]bool{}
	if fn == nil || fn.Type == nil {
		return locals
	}
	addFieldNames(locals, fn.Recv)
	addFieldNames(locals, fn.Type.Params)
	addFieldNames(locals, fn.Type.Results)
	if fn.Name != nil {
		locals[fn.Name.Name] = true
	}
	if fn.Body == nil {
		return locals
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.AssignStmt:
			if e.Tok != token.DEFINE {
				return true
			}
			for _, lhs := range e.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					locals[id.Name] = true
				}
			}
		case *ast.RangeStmt:
			if e.Tok != token.DEFINE {
				return true
			}
			if id, ok := e.Key.(*ast.Ident); ok {
				locals[id.Name] = true
			}
			if id, ok := e.Value.(*ast.Ident); ok {
				locals[id.Name] = true
			}
		case *ast.DeclStmt:
			gd, ok := e.Decl.(*ast.GenDecl)
			if !ok {
				return true
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					locals[name.Name] = true
				}
			}
		}
		return true
	})
	return locals
}

func addFieldNames(locals map[string]bool, fields *ast.FieldList) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			locals[name.Name] = true
		}
	}
}

func importAliases(file *ast.File) map[string]string {
	aliases := map[string]string{}
	if file == nil {
		return aliases
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path == "" {
			continue
		}
		if spec.Name != nil {
			if spec.Name.Name == "_" || spec.Name.Name == "." {
				continue
			}
			aliases[spec.Name.Name] = path
			continue
		}
		aliases[pathBase(path)] = path
	}
	return aliases
}

func pathBase(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func parseSourcePkg(cliDir, dir string, preset map[string]string) *goSourcePkg {
	pkg := &goSourcePkg{
		funcs:   map[string]*goSymbol{},
		values:  map[string]*goSymbol{},
		methods: map[string][]*goSymbol{},
	}
	if preset != nil {
		names := make([]string, 0, len(preset))
		for name := range preset {
			names = append(names, name)
		}
		for _, name := range names {
			addParsedFile(pkg, cliDir, filepath.Join(dir, name), name, preset[name])
		}
		return pkg
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		addParsedFile(pkg, cliDir, path, name, string(data))
	}
	if len(pkg.files) == 0 {
		return nil
	}
	return pkg
}

func addParsedFile(pkg *goSourcePkg, cliDir, path, name, content string) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, content, parser.SkipObjectResolution)
	if err != nil {
		return
	}
	parsed := &goSourceFile{
		report:    reportPath(cliDir, path, name),
		fset:      fset,
		file:      file,
		generated: isGeneratedPrintingPressFile(content),
	}
	pkg.files = append(pkg.files, parsed)
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name == nil {
				continue
			}
			sym := &goSymbol{kind: goSymbolFunc, name: d.Name.Name, file: parsed, node: d}
			if d.Recv != nil {
				sym.kind = goSymbolMethod
				sym.recv = recvTypeName(d)
				pkg.methods[sym.name] = append(pkg.methods[sym.name], sym)
				continue
			}
			if _, ok := pkg.funcs[sym.name]; !ok {
				pkg.funcs[sym.name] = sym
			}
		case *ast.GenDecl:
			if d.Tok != token.CONST && d.Tok != token.VAR {
				continue
			}
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					node := ast.Node(vs)
					if i < len(vs.Values) {
						node = vs.Values[i]
					}
					if _, ok := pkg.values[name.Name]; ok {
						continue
					}
					pkg.values[name.Name] = &goSymbol{kind: goSymbolValue, name: name.Name, file: parsed, node: node}
				}
			}
		}
	}
}

func recvTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	return typeIdentName(fn.Recv.List[0].Type)
}

func typeIdentName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return typeIdentName(e.X)
	case *ast.IndexExpr:
		return typeIdentName(e.X)
	case *ast.IndexListExpr:
		return typeIdentName(e.X)
	default:
		return ""
	}
}

func (t goTypeRef) known() bool {
	return t.name != "" || t.callName != ""
}

func concreteType(cliDir, module string, pkg *goSourcePkg, t goTypeRef, cache map[string]*goSourcePkg) goTypeRef {
	if t.callName == "" {
		return t
	}
	owner := pkg
	if t.callImport != "" {
		owner = loadImportedPkg(cliDir, module, t.callImport, cache)
	}
	fn, aliases := callFunc(owner, t)
	if fn == nil {
		return goTypeRef{}
	}
	rt := funcResultType(fn, aliases)
	if rt.name != "" && rt.importPath == "" {
		rt.importPath = t.callImport
	}
	return rt
}

func callFunc(pkg *goSourcePkg, t goTypeRef) (*ast.FuncDecl, map[string]string) {
	if pkg == nil || t.callName == "" {
		return nil, nil
	}
	if t.callRecv != "" {
		for _, method := range pkg.methods[t.callName] {
			if method.recv != t.callRecv || method.file == nil {
				continue
			}
			fn, ok := method.node.(*ast.FuncDecl)
			if !ok {
				continue
			}
			return fn, importAliases(method.file.file)
		}
		return nil, nil
	}
	sym := pkg.funcs[t.callName]
	if sym == nil || sym.file == nil {
		return nil, nil
	}
	fn, ok := sym.node.(*ast.FuncDecl)
	if !ok {
		return nil, nil
	}
	return fn, importAliases(sym.file.file)
}

func funcResultType(fn *ast.FuncDecl, aliases map[string]string) goTypeRef {
	if fn == nil || fn.Type == nil || fn.Type.Results == nil || len(fn.Type.Results.List) == 0 {
		return goTypeRef{}
	}
	t, ok := typeExpr(fn.Type.Results.List[0].Type, aliases)
	if !ok {
		return goTypeRef{}
	}
	return t
}

func bindingsFor(node ast.Node, aliases map[string]string) map[string]goTypeRef {
	fn, ok := node.(*ast.FuncDecl)
	if !ok || fn.Type == nil {
		return nil
	}
	bindings := map[string]goTypeRef{}
	addFieldTypes(bindings, fn.Recv, aliases)
	addFieldTypes(bindings, fn.Type.Params, aliases)
	addFieldTypes(bindings, fn.Type.Results, aliases)
	if fn.Body != nil {
		collectBindings(fn.Body, bindings, aliases)
	}
	return bindings
}

func collectBindings(node ast.Node, bindings map[string]goTypeRef, aliases map[string]string) {
	if node == nil || bindings == nil {
		return
	}
	ast.Inspect(node, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ValueSpec:
			bindValueSpec(bindings, e, aliases)
		case *ast.AssignStmt:
			bindAssign(bindings, e, aliases)
		}
		return true
	})
}

func bindValueSpec(bindings map[string]goTypeRef, vs *ast.ValueSpec, aliases map[string]string) {
	if vs == nil {
		return
	}
	if vs.Type != nil {
		t, ok := typeExpr(vs.Type, aliases)
		if !ok {
			return
		}
		for _, name := range vs.Names {
			if name != nil && name.Name != "_" {
				bindings[name.Name] = t
			}
		}
		return
	}
	if len(vs.Values) == 1 && len(vs.Names) > 0 {
		t := valueType(vs.Values[0], bindings, aliases)
		if t.known() && vs.Names[0] != nil && vs.Names[0].Name != "_" {
			bindings[vs.Names[0].Name] = t
		}
		return
	}
	if len(vs.Values) != len(vs.Names) {
		return
	}
	for i := range vs.Names {
		if vs.Names[i] == nil || vs.Names[i].Name == "_" {
			continue
		}
		t := valueType(vs.Values[i], bindings, aliases)
		if t.known() {
			bindings[vs.Names[i].Name] = t
		}
	}
}

func bindAssign(bindings map[string]goTypeRef, stmt *ast.AssignStmt, aliases map[string]string) {
	if stmt == nil || len(stmt.Lhs) == 0 || len(stmt.Rhs) == 0 {
		return
	}
	if len(stmt.Rhs) == 1 {
		t := valueType(stmt.Rhs[0], bindings, aliases)
		if !t.known() {
			return
		}
		id, ok := stmt.Lhs[0].(*ast.Ident)
		if ok && id.Name != "_" {
			bindings[id.Name] = t
		}
		return
	}
	if len(stmt.Lhs) != len(stmt.Rhs) {
		return
	}
	for i := range stmt.Lhs {
		id, ok := stmt.Lhs[i].(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		t := valueType(stmt.Rhs[i], bindings, aliases)
		if t.known() {
			bindings[id.Name] = t
		}
	}
}

func valueType(expr ast.Expr, bindings map[string]goTypeRef, aliases map[string]string) goTypeRef {
	switch e := expr.(type) {
	case *ast.Ident:
		if bindings == nil {
			return goTypeRef{}
		}
		return bindings[e.Name]
	case *ast.StarExpr:
		return valueType(e.X, bindings, aliases)
	case *ast.ParenExpr:
		return valueType(e.X, bindings, aliases)
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return valueType(e.X, bindings, aliases)
		}
	case *ast.CompositeLit:
		t, ok := typeExpr(e.Type, aliases)
		if ok {
			return t
		}
	case *ast.CallExpr:
		return callValueType(e, bindings, aliases)
	}
	return goTypeRef{}
}

func callValueType(call *ast.CallExpr, bindings map[string]goTypeRef, aliases map[string]string) goTypeRef {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if fun.Name == "new" && len(call.Args) == 1 {
			t, ok := typeExpr(call.Args[0], aliases)
			if ok {
				return t
			}
			return goTypeRef{}
		}
		if fun.Name == "_" || goPredeclared[fun.Name] {
			return goTypeRef{}
		}
		return goTypeRef{callName: fun.Name}
	case *ast.SelectorExpr:
		id, ok := fun.X.(*ast.Ident)
		if ok && aliases[id.Name] != "" && !bindingHas(bindings, id.Name) {
			return goTypeRef{callName: fun.Sel.Name, callImport: aliases[id.Name]}
		}
		recv := valueType(fun.X, bindings, aliases)
		if recv.name == "" || recv.callName != "" {
			return goTypeRef{}
		}
		return goTypeRef{callName: fun.Sel.Name, callImport: recv.importPath, callRecv: recv.name}
	default:
		return goTypeRef{}
	}
}

func bindingHas(bindings map[string]goTypeRef, name string) bool {
	if bindings == nil {
		return false
	}
	_, ok := bindings[name]
	return ok
}

func typeExpr(expr ast.Expr, aliases map[string]string) (goTypeRef, bool) {
	if expr == nil {
		return goTypeRef{}, false
	}
	switch e := expr.(type) {
	case *ast.Ident:
		if e.Name == "" || e.Name == "_" || goPredeclared[e.Name] {
			return goTypeRef{}, false
		}
		return goTypeRef{name: e.Name}, true
	case *ast.StarExpr:
		return typeExpr(e.X, aliases)
	case *ast.ParenExpr:
		return typeExpr(e.X, aliases)
	case *ast.SelectorExpr:
		id, ok := e.X.(*ast.Ident)
		if !ok || aliases[id.Name] == "" {
			return goTypeRef{}, false
		}
		return goTypeRef{name: e.Sel.Name, importPath: aliases[id.Name]}, true
	case *ast.IndexExpr:
		return typeExpr(e.X, aliases)
	case *ast.IndexListExpr:
		return typeExpr(e.X, aliases)
	default:
		return goTypeRef{}, false
	}
}

func addFieldTypes(bindings map[string]goTypeRef, fields *ast.FieldList, aliases map[string]string) {
	if bindings == nil || fields == nil {
		return
	}
	for _, field := range fields.List {
		t, ok := typeExpr(field.Type, aliases)
		if !ok {
			continue
		}
		for _, name := range field.Names {
			if name.Name == "_" {
				continue
			}
			bindings[name.Name] = t
		}
	}
}

func copyTypes(in map[string]goTypeRef) map[string]goTypeRef {
	out := make(map[string]goTypeRef, len(in))
	maps.Copy(out, in)
	return out
}

func copyBools(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	maps.Copy(out, in)
	return out
}

func markAssigned(node ast.Node, locals map[string]bool) {
	if node == nil || locals == nil {
		return
	}
	ast.Inspect(node, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		switch e := n.(type) {
		case *ast.AssignStmt:
			if e.Tok != token.DEFINE {
				return true
			}
			for _, lhs := range e.Lhs {
				id, ok := lhs.(*ast.Ident)
				if ok {
					locals[id.Name] = true
				}
			}
		case *ast.ValueSpec:
			for _, name := range e.Names {
				locals[name.Name] = true
			}
		case *ast.RangeStmt:
			if e.Tok != token.DEFINE {
				return true
			}
			if id, ok := e.Key.(*ast.Ident); ok {
				locals[id.Name] = true
			}
			if id, ok := e.Value.(*ast.Ident); ok {
				locals[id.Name] = true
			}
		}
		return true
	})
}

func loadImportedPkg(cliDir, module, importPath string, cache map[string]*goSourcePkg) *goSourcePkg {
	dir := localPkgDir(cliDir, module, importPath)
	if dir == "" {
		return nil
	}
	if pkg, ok := cache[dir]; ok {
		return pkg
	}
	pkg := parseSourcePkg(cliDir, dir, nil)
	cache[dir] = pkg
	return pkg
}

func localPkgDir(cliDir, module, importPath string) string {
	importPath = strings.TrimSpace(importPath)
	if importPath == "" || strings.Contains(importPath, "..") {
		return ""
	}
	var rel string
	switch {
	case module != "" && importPath == module:
		rel = ""
	case module != "" && strings.HasPrefix(importPath, module+"/"):
		rel = strings.TrimPrefix(importPath, module+"/")
	default:
		idx := strings.LastIndex(importPath, "internal/")
		if idx < 0 {
			return ""
		}
		rel = importPath[idx:]
	}
	dir := filepath.Join(cliDir, filepath.FromSlash(rel))
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return ""
	}
	back, err := filepath.Rel(cliDir, dir)
	if err != nil || strings.HasPrefix(back, "..") {
		return ""
	}
	return dir
}

func cliModulePath(cliDir string) string {
	data, err := os.ReadFile(filepath.Join(cliDir, "go.mod"))
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module ")
		if ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func reportPath(cliDir, path, name string) string {
	rel, err := filepath.Rel(cliDir, path)
	if err != nil {
		return name
	}
	rel = filepath.ToSlash(rel)
	const prefix = "internal/cli/"
	if strings.HasPrefix(rel, prefix) && !strings.Contains(rel[len(prefix):], "/") {
		return name
	}
	return rel
}

func findSourceFile(pkg *goSourcePkg, name string) *goSourceFile {
	for _, file := range pkg.files {
		if file.report == name {
			return file
		}
	}
	return nil
}

func featureLeaf(content string, leaves map[string]string) string {
	for _, match := range cobraUseLeafRe.FindAllStringSubmatch(content, -1) {
		if _, ok := leaves[match[1]]; ok {
			return match[1]
		}
	}
	return ""
}

func sharesFeatureFile(report, leaf string) bool {
	stem := strings.TrimSuffix(filepath.Base(report), ".go")
	if stem == leaf {
		return true
	}
	return strings.HasPrefix(stem, leaf+"_") || strings.HasPrefix(stem, leaf+"-")
}

var goPredeclared = map[string]bool{
	"any": true, "append": true, "bool": true, "byte": true, "cap": true, "clear": true,
	"close": true, "comparable": true, "complex": true, "complex128": true, "complex64": true,
	"copy": true, "delete": true, "error": true, "false": true, "float32": true, "float64": true,
	"imag": true, "int": true, "int16": true, "int32": true, "int64": true, "int8": true,
	"iota": true, "len": true, "make": true, "max": true, "min": true, "new": true,
	"nil": true, "panic": true, "print": true, "println": true, "real": true, "recover": true,
	"rune": true, "string": true, "true": true, "uint": true, "uint16": true, "uint32": true,
	"uint64": true, "uint8": true, "uintptr": true,
}

var skipMethodNames = map[string]bool{
	"Close": true, "Error": true, "Format": true, "GoString": true, "Len": true,
	"Read": true, "String": true, "Unwrap": true, "Write": true,
}
