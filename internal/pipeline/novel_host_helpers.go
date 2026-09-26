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
// an imported type, that package, not the method name alone. A call through
// a struct field uses that field's type. The name bindings used for a call
// are the ones in scope at that call, so a later inner declaration cannot
// retarget an earlier call. Unreferenced files stay out of the gate.

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
	structs map[string]map[string]goTypeRef
}

// resolveEnv is the package currently being walked, plus enough context to
// load the package that defines a struct when a field type is imported.
type resolveEnv struct {
	cliDir string
	module string
	pkg    *goSourcePkg
	cache  map[string]*goSourcePkg
}

type srcRef struct {
	kind       string
	name       string
	importPath string
	recv       goTypeRef
}

// goTypeRef is a named type, or a call whose result type is resolved later.
// An empty importPath means the type is in the package being resolved.
// A zero resultIndex keeps a call on its first result.
type goTypeRef struct {
	name        string
	importPath  string
	callName    string
	callImport  string
	callRecv    string
	resultIndex int
	fields      map[string]goTypeRef
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

	env := &resolveEnv{cliDir: cliDir, module: module, cache: cache}
	follow := func(current *goSourcePkg, file *goSourceFile, node ast.Node, locals map[string]bool) {
		env.pkg = current
		for _, ref := range followNode(node, locals, importAliases(file.file), env) {
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

func nodeRefs(node ast.Node, locals map[string]bool, aliases map[string]string, env *resolveEnv) []srcRef {
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
	b := &binder{env: env, aliases: aliases}
	b.collect(node)
	var refs []srcRef
	appendRefs(node, locals, aliases, b, sel, &refs)
	return refs
}

func appendRefs(node ast.Node, locals map[string]bool, aliases map[string]string, b *binder, sel map[*ast.Ident]bool, refs *[]srcRef) {
	ast.Inspect(node, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if ok {
			appendLitRefs(lit, locals, aliases, b, sel, refs)
			return false
		}
		switch e := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := e.X.(*ast.Ident); ok && aliases[id.Name] != "" && !b.has(id.Pos(), id.Name) {
				*refs = append(*refs, srcRef{kind: "pkg", name: e.Sel.Name, importPath: aliases[id.Name]})
				return true
			}
			if locals[e.Sel.Name] || goPredeclared[e.Sel.Name] {
				return true
			}
			recv := valueType(e.X, b)
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

func appendLitRefs(lit *ast.FuncLit, locals map[string]bool, aliases map[string]string, b *binder, sel map[*ast.Ident]bool, refs *[]srcRef) {
	if lit == nil || lit.Body == nil {
		return
	}
	innerLocals := copyBools(locals)
	if lit.Type != nil {
		addFieldNames(innerLocals, lit.Type.Params)
		addFieldNames(innerLocals, lit.Type.Results)
	}
	markAssigned(lit.Body, innerLocals)
	appendRefs(lit.Body, innerLocals, aliases, b, sel, refs)
}

func followNode(node ast.Node, locals map[string]bool, aliases map[string]string, env *resolveEnv) []srcRef {
	fn, ok := node.(*ast.FuncDecl)
	if ok && locals == nil {
		return nodeRefs(fn, localsInFunc(fn), aliases, env)
	}
	if file, ok := node.(*ast.File); ok {
		var refs []srcRef
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				refs = append(refs, nodeRefs(d, localsInFunc(d), aliases, env)...)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, val := range vs.Values {
						refs = append(refs, nodeRefs(val, nil, aliases, env)...)
					}
				}
			}
		}
		return refs
	}
	return nodeRefs(node, locals, aliases, env)
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
		structs: map[string]map[string]goTypeRef{},
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
	indexStructs(pkg, file)
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

func (t goTypeRef) usable() bool {
	return t.known() || len(t.fields) > 0
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
	rt := funcResultAt(fn, aliases, t.resultIndex)
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

func funcResultAt(fn *ast.FuncDecl, aliases map[string]string, index int) goTypeRef {
	if fn == nil || fn.Type == nil || fn.Type.Results == nil || index < 0 {
		return goTypeRef{}
	}
	at := 0
	for _, field := range fn.Type.Results.List {
		n := len(field.Names)
		if n == 0 {
			n = 1
		}
		if index < at+n {
			t, ok := typeOf(field.Type, aliases)
			if !ok {
				return goTypeRef{}
			}
			return t
		}
		at += n
	}
	return goTypeRef{}
}

// A binding covers positions from the end of its declaration through the end
// of its scope. Lookup picks the innermost one that contains the use, so a
// later inner name cannot retarget an earlier call.
type bindSite struct {
	name  string
	typ   goTypeRef
	from  token.Pos
	to    token.Pos
	depth int
}

type binder struct {
	env     *resolveEnv
	aliases map[string]string
	sites   []bindSite
}

func (b *binder) collect(node ast.Node) {
	if b == nil || node == nil {
		return
	}
	b.walk(node, node.End(), 0)
}

func (b *binder) walk(node ast.Node, scopeEnd token.Pos, depth int) {
	if b == nil || node == nil {
		return
	}
	ast.Walk(&scopeVisitor{b: b, scopeEnd: scopeEnd, depth: depth}, node)
}

func (b *binder) add(name string, typ goTypeRef, from, to token.Pos, depth int) {
	if b == nil || name == "" || name == "_" || !from.IsValid() || !to.IsValid() || from >= to {
		return
	}
	b.sites = append(b.sites, bindSite{name: name, typ: typ, from: from, to: to, depth: depth})
}

func (b *binder) best(pos token.Pos, name string) (bindSite, bool) {
	var best bindSite
	found := false
	if b == nil || name == "" || !pos.IsValid() {
		return best, false
	}
	for _, site := range b.sites {
		if site.name != name || pos < site.from || pos >= site.to {
			continue
		}
		if !found || site.depth > best.depth || (site.depth == best.depth && site.from >= best.from) {
			best = site
			found = true
		}
	}
	return best, found
}

func (b *binder) has(pos token.Pos, name string) bool {
	_, ok := b.best(pos, name)
	return ok
}

func (b *binder) lookup(pos token.Pos, name string) (goTypeRef, bool) {
	site, ok := b.best(pos, name)
	if !ok || !site.typ.usable() {
		return goTypeRef{}, false
	}
	return site.typ, true
}

func (b *binder) addFields(fields *ast.FieldList, from, to token.Pos, depth int) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		t, _ := typeOf(field.Type, b.aliases)
		for _, name := range field.Names {
			if name == nil {
				continue
			}
			b.add(name.Name, t, from, to, depth)
		}
	}
}

func (b *binder) collectFunc(recv *ast.FieldList, typ *ast.FuncType, body *ast.BlockStmt, depth int) {
	if body == nil {
		return
	}
	end := body.End()
	d := depth + 1
	from := body.Pos()
	b.addFields(recv, from, end, d)
	if typ != nil {
		b.addFields(typ.Params, from, end, d)
		b.addFields(typ.Results, from, end, d)
	}
	for _, stmt := range body.List {
		b.walk(stmt, end, d)
	}
}

func (b *binder) bindValueSpec(vs *ast.ValueSpec, from, scopeEnd token.Pos, depth int) {
	if vs == nil {
		return
	}
	if vs.Type != nil {
		t, _ := typeOf(vs.Type, b.aliases)
		for _, name := range vs.Names {
			if name != nil {
				b.add(name.Name, t, from, scopeEnd, depth)
			}
		}
		return
	}
	types := make([]goTypeRef, len(vs.Names))
	switch {
	case len(vs.Values) == 1 && len(vs.Names) > 0:
		types[0] = valueType(vs.Values[0], b)
	case len(vs.Values) == len(vs.Names):
		for i := range vs.Values {
			types[i] = valueType(vs.Values[i], b)
		}
	}
	for i, name := range vs.Names {
		if name == nil {
			continue
		}
		typ := goTypeRef{}
		if i < len(types) {
			typ = types[i]
		}
		b.add(name.Name, typ, from, scopeEnd, depth)
	}
}

func (b *binder) bindAssign(stmt *ast.AssignStmt, from, scopeEnd token.Pos, depth int) {
	if stmt == nil || len(stmt.Lhs) == 0 || len(stmt.Rhs) == 0 {
		return
	}
	define := stmt.Tok == token.DEFINE
	types := assignTypes(b, stmt)
	for i, lhs := range stmt.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok {
			continue
		}
		typ := goTypeRef{}
		if i < len(types) {
			typ = types[i]
		}
		if define {
			b.bindDefine(id.Name, typ, id.Pos(), from, scopeEnd, depth)
			continue
		}
		// An unresolved call is not a type. Replacing the binding with it
		// would hide the name already in scope.
		if !b.knownResult(typ) {
			continue
		}
		site, ok := b.best(id.Pos(), id.Name)
		if !ok {
			continue
		}
		b.add(id.Name, typ, from, site.to, site.depth)
	}
}

// Go redeclaration does not change a name's type, so an unusable result must
// not replace a same-block binding. A new name with no usable type is still
// recorded so an outer name is not used in an inner scope.
func (b *binder) bindDefine(name string, typ goTypeRef, at, from, scopeEnd token.Pos, depth int) {
	if !b.knownResult(typ) {
		if site, ok := b.best(at, name); ok && site.depth == depth && site.typ.usable() {
			return
		}
	}
	b.add(name, typ, from, scopeEnd, depth)
}

// A call's recorded type is only a placeholder until the selected result
// resolves. Treating an unresolved result as a binding would replace the
// in-scope type, and a later method walk would miss the hosts on that value.
func (b *binder) knownResult(typ goTypeRef) bool {
	if !typ.usable() {
		return false
	}
	if typ.callName == "" {
		return true
	}
	if b == nil || b.env == nil || b.env.pkg == nil {
		return false
	}
	if typ.callImport != "" && b.env.cache == nil {
		return false
	}
	return concreteType(b.env.cliDir, b.env.module, b.env.pkg, typ, b.env.cache).usable()
}

func assignTypes(b *binder, stmt *ast.AssignStmt) []goTypeRef {
	out := make([]goTypeRef, len(stmt.Lhs))
	if len(stmt.Rhs) == 1 {
		base := valueType(stmt.Rhs[0], b)
		if base.callName != "" && len(out) > 1 {
			for i := range out {
				out[i] = base
				out[i].resultIndex = i
			}
			return out
		}
		out[0] = base
		return out
	}
	if len(stmt.Rhs) != len(stmt.Lhs) {
		return out
	}
	for i := range stmt.Rhs {
		out[i] = valueType(stmt.Rhs[i], b)
	}
	return out
}

type scopeVisitor struct {
	b        *binder
	scopeEnd token.Pos
	depth    int
}

func (v *scopeVisitor) Visit(node ast.Node) ast.Visitor {
	if v == nil || node == nil {
		return nil
	}
	switch n := node.(type) {
	case *ast.FuncDecl:
		v.b.collectFunc(n.Recv, n.Type, n.Body, v.depth)
		return nil
	case *ast.FuncLit:
		v.b.collectFunc(nil, n.Type, n.Body, v.depth)
		return nil
	case *ast.BlockStmt:
		for _, stmt := range n.List {
			v.b.walk(stmt, n.End(), v.depth+1)
		}
		return nil
	case *ast.IfStmt:
		end := n.End()
		d := v.depth + 1
		v.b.walk(n.Init, end, d)
		v.b.walk(n.Cond, end, d)
		v.b.walk(n.Body, end, d)
		v.b.walk(n.Else, end, d)
		return nil
	case *ast.ForStmt:
		end := n.End()
		d := v.depth + 1
		v.b.walk(n.Init, end, d)
		v.b.walk(n.Cond, end, d)
		v.b.walk(n.Post, end, d)
		v.b.walk(n.Body, end, d)
		return nil
	case *ast.RangeStmt:
		v.b.walk(n.X, v.scopeEnd, v.depth)
		if n.Body == nil {
			return nil
		}
		d := v.depth + 1
		if n.Tok == token.DEFINE {
			v.b.add(identName(n.Key), goTypeRef{}, n.Body.Pos(), n.Body.End(), d)
			v.b.add(identName(n.Value), goTypeRef{}, n.Body.Pos(), n.Body.End(), d)
		}
		for _, stmt := range n.Body.List {
			v.b.walk(stmt, n.Body.End(), d)
		}
		return nil
	case *ast.SwitchStmt:
		end := n.End()
		d := v.depth + 1
		v.b.walk(n.Init, end, d)
		v.b.walk(n.Tag, end, d)
		v.b.walk(n.Body, end, d)
		return nil
	case *ast.TypeSwitchStmt:
		end := n.End()
		d := v.depth + 1
		v.b.walk(n.Init, end, d)
		v.b.walk(n.Assign, end, d)
		v.b.walk(n.Body, end, d)
		return nil
	case *ast.CaseClause:
		for _, expr := range n.List {
			v.b.walk(expr, v.scopeEnd, v.depth)
		}
		d := v.depth + 1
		for _, stmt := range n.Body {
			v.b.walk(stmt, n.End(), d)
		}
		return nil
	case *ast.CommClause:
		if n.Comm != nil {
			v.b.walk(n.Comm, v.scopeEnd, v.depth)
		}
		d := v.depth + 1
		for _, stmt := range n.Body {
			v.b.walk(stmt, n.End(), d)
		}
		return nil
	case *ast.AssignStmt:
		for _, rhs := range n.Rhs {
			v.b.walk(rhs, v.scopeEnd, v.depth)
		}
		for _, lhs := range n.Lhs {
			v.b.walk(lhs, v.scopeEnd, v.depth)
		}
		v.b.bindAssign(n, n.End(), v.scopeEnd, v.depth)
		return nil
	case *ast.ValueSpec:
		for _, val := range n.Values {
			v.b.walk(val, v.scopeEnd, v.depth)
		}
		if n.Type != nil {
			v.b.walk(n.Type, v.scopeEnd, v.depth)
		}
		v.b.bindValueSpec(n, n.End(), v.scopeEnd, v.depth)
		return nil
	default:
		return v
	}
}

func identName(expr ast.Expr) string {
	id, ok := expr.(*ast.Ident)
	if !ok || id == nil {
		return ""
	}
	return id.Name
}

func valueType(expr ast.Expr, b *binder) goTypeRef {
	switch e := expr.(type) {
	case *ast.Ident:
		if b == nil {
			return goTypeRef{}
		}
		t, _ := b.lookup(e.Pos(), e.Name)
		return t
	case *ast.StarExpr:
		return valueType(e.X, b)
	case *ast.ParenExpr:
		return valueType(e.X, b)
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return valueType(e.X, b)
		}
	case *ast.CompositeLit:
		if b == nil {
			return goTypeRef{}
		}
		t, ok := typeOf(e.Type, b.aliases)
		if ok {
			return t
		}
	case *ast.CallExpr:
		return callValueType(e, b)
	case *ast.SelectorExpr:
		base := valueType(e.X, b)
		var env *resolveEnv
		if b != nil {
			env = b.env
		}
		return fieldType(env, base, e.Sel.Name)
	}
	return goTypeRef{}
}

func callValueType(call *ast.CallExpr, b *binder) goTypeRef {
	var aliases map[string]string
	if b != nil {
		aliases = b.aliases
	}
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if fun.Name == "new" && len(call.Args) == 1 {
			t, ok := typeOf(call.Args[0], aliases)
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
		if ok && aliases[id.Name] != "" && !b.has(id.Pos(), id.Name) {
			return goTypeRef{callName: fun.Sel.Name, callImport: aliases[id.Name]}
		}
		recv := valueType(fun.X, b)
		if recv.name == "" || recv.callName != "" {
			return goTypeRef{}
		}
		return goTypeRef{callName: fun.Sel.Name, callImport: recv.importPath, callRecv: recv.name}
	default:
		return goTypeRef{}
	}
}

func typeOf(expr ast.Expr, aliases map[string]string) (goTypeRef, bool) {
	st, ok := structType(expr)
	if ok {
		return goTypeRef{fields: structFieldMap(st, aliases)}, true
	}
	return typeExpr(expr, aliases)
}

func structType(expr ast.Expr) (*ast.StructType, bool) {
	switch e := expr.(type) {
	case *ast.StructType:
		return e, true
	case *ast.ParenExpr:
		return structType(e.X)
	case *ast.StarExpr:
		return structType(e.X)
	default:
		return nil, false
	}
}

func structFieldMap(st *ast.StructType, aliases map[string]string) map[string]goTypeRef {
	fields := map[string]goTypeRef{}
	if st == nil || st.Fields == nil {
		return fields
	}
	for _, field := range st.Fields.List {
		t, ok := typeOf(field.Type, aliases)
		if !ok {
			continue
		}
		for _, name := range field.Names {
			if name == nil || name.Name == "_" {
				continue
			}
			fields[name.Name] = t
		}
	}
	return fields
}

func indexStructs(pkg *goSourcePkg, file *ast.File) {
	if pkg == nil || file == nil {
		return
	}
	if pkg.structs == nil {
		pkg.structs = map[string]map[string]goTypeRef{}
	}
	aliases := importAliases(file)
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name == nil || ts.Name.Name == "" || ts.Name.Name == "_" {
				continue
			}
			st, ok := structType(ts.Type)
			if !ok {
				continue
			}
			if _, exists := pkg.structs[ts.Name.Name]; exists {
				continue
			}
			pkg.structs[ts.Name.Name] = structFieldMap(st, aliases)
		}
	}
}

func fieldType(env *resolveEnv, recv goTypeRef, name string) goTypeRef {
	if name == "" {
		return goTypeRef{}
	}
	if t, ok := recv.fields[name]; ok {
		return t
	}
	if env == nil || recv.name == "" || recv.callName != "" {
		return goTypeRef{}
	}
	owner := env.pkg
	if recv.importPath != "" {
		if env.cache == nil {
			return goTypeRef{}
		}
		owner = loadImportedPkg(env.cliDir, env.module, recv.importPath, env.cache)
	}
	if owner == nil || owner.structs == nil {
		return goTypeRef{}
	}
	return qualifyFieldType(owner.structs[recv.name][name], recv.importPath)
}

// A field written as Client inside an imported struct is that package's Client.
func qualifyFieldType(t goTypeRef, importPath string) goTypeRef {
	if importPath == "" {
		return t
	}
	if t.name != "" && t.importPath == "" && t.callName == "" {
		t.importPath = importPath
	}
	if len(t.fields) == 0 {
		return t
	}
	fields := make(map[string]goTypeRef, len(t.fields))
	for name, ft := range t.fields {
		fields[name] = qualifyFieldType(ft, importPath)
	}
	t.fields = fields
	return t
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
