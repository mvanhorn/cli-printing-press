package pipeline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Hosts often live in a helper the novel command calls, or in a same-package
// file named for that command. Unreferenced files stay out of the gate.

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
			for _, sym := range resolveRef(cliDir, module, current, ref, node, cache) {
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

func resolveRef(cliDir, module string, pkg *goSourcePkg, ref srcRef, node ast.Node, cache map[string]*goSourcePkg) []*goSymbol {
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
		return resolveMethods(pkg, ref.name, node)
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

func resolveMethods(pkg *goSourcePkg, name string, node ast.Node) []*goSymbol {
	if skipMethodNames[name] {
		return nil
	}
	methods := pkg.methods[name]
	if len(methods) == 0 {
		return nil
	}
	if len(methods) == 1 {
		return methods
	}
	mentioned := map[string]bool{}
	ast.Inspect(node, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if ok {
			mentioned[id.Name] = true
		}
		return true
	})
	var matched []*goSymbol
	for _, method := range methods {
		if mentioned[method.recv] {
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
	var refs []srcRef
	ast.Inspect(node, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := e.X.(*ast.Ident); ok && aliases[id.Name] != "" && !locals[id.Name] {
				refs = append(refs, srcRef{kind: "pkg", name: e.Sel.Name, importPath: aliases[id.Name]})
				return true
			}
			if !locals[e.Sel.Name] && !goPredeclared[e.Sel.Name] {
				refs = append(refs, srcRef{kind: "method", name: e.Sel.Name})
			}
		case *ast.Ident:
			if sel[e] || locals[e.Name] || goPredeclared[e.Name] || e.Name == "_" {
				return true
			}
			refs = append(refs, srcRef{kind: "ident", name: e.Name})
		}
		return true
	})
	return refs
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
