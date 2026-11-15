package pipeline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
)

// hookRootBinding is the scope id of a registerNovelCommand callback's
// *cobra.Command parameter. Nested declarations of the same name get other ids.
const hookRootBinding = 1

// commandBinding is one name in a lexical scope. factory is set when the
// name was defined from a constructor call.
type commandBinding struct {
	id      int
	factory string
}

// commandScope is a lexical block. Lookup walks parents, so a nested
// declaration hides the hook parameter without becoming a second root.
type commandScope struct {
	parent *commandScope
	names  map[string]commandBinding
}

func (s *commandScope) child() *commandScope {
	return &commandScope{parent: s}
}

func (s *commandScope) lookup(name string) (commandBinding, bool) {
	for s != nil {
		if b, ok := s.names[name]; ok {
			return b, true
		}
		s = s.parent
	}
	return commandBinding{}, false
}

func (s *commandScope) define(name string, b commandBinding) {
	if name == "" || name == "_" {
		return
	}
	if _, exists := s.names[name]; exists {
		return
	}
	if s.names == nil {
		s.names = map[string]commandBinding{}
	}
	s.names[name] = b
}

// novelHookRootFactories returns constructors registered on a
// registerNovelCommand literal's root parameter. masked is src with those
// literal bodies blanked so a nested declaration named rootCmd cannot
// satisfy the rootCmd regex. A parse error leaves src unchanged.
func novelHookRootFactories(src string) (factories []string, masked string) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return nil, src
	}
	fileScope := fileCommandScope(file)
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			if decl.Body == nil || decl.Type == nil {
				continue
			}
			// Ordinary parameters must not reuse the hook-root id. Each
			// function has its own scope chain, so the counter can restart.
			fnScope := fileScope.child()
			next := hookRootBinding + 1
			bindFieldList(decl.Type.Params, fnScope, &next)
			bindFieldList(decl.Type.Results, fnScope, &next)
			walkHookNode(decl.Body, fnScope, &factories, &next)
		case *ast.GenDecl:
			if decl.Tok != token.VAR {
				continue
			}
			// Masking blanks every hook, including one inside a package
			// variable initializer. Walk those expressions too, or the
			// registration disappears from both passes.
			for _, spec := range decl.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				next := hookRootBinding + 1
				for _, value := range vs.Values {
					walkHookNode(value, fileScope, &factories, &next)
				}
			}
		}
	}
	var blanks [][2]int
	ast.Inspect(file, func(n ast.Node) bool {
		hook, ok := novelHookLiteral(n)
		if !ok || hook.Body == nil {
			return true
		}
		start := fset.Position(hook.Body.Lbrace).Offset
		end := fset.Position(hook.Body.Rbrace).Offset
		if start >= 0 && end > start+1 && end <= len(src) {
			blanks = append(blanks, [2]int{start + 1, end})
		}
		return true
	})
	if len(blanks) == 0 {
		return factories, src
	}
	buf := []byte(src)
	for _, span := range blanks {
		for i := span[0]; i < span[1] && i < len(buf); i++ {
			if buf[i] != '\n' {
				buf[i] = ' '
			}
		}
	}
	return factories, string(buf)
}

func fileCommandScope(file *ast.File) *commandScope {
	s := &commandScope{}
	id := -1
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				factory := ""
				if name != nil && i < len(vs.Values) {
					factory = commandFactoryCallName(vs.Values[i])
				}
				id--
				if name != nil {
					s.define(name.Name, commandBinding{id: id, factory: factory})
				}
			}
		}
	}
	return s
}

func factoriesInHook(hook *ast.FuncLit, outer *commandScope) []string {
	root := hookRootIdent(hook)
	if root == nil || hook.Body == nil || hook.Type == nil || outer == nil {
		return nil
	}
	// The hook is a nested function. Its parent scope is the function
	// that contains the literal, so a command variable captured from
	// there still resolves.
	fnScope := outer.child()
	next := hookRootBinding + 1
	if hook.Type.Params != nil {
		for _, field := range hook.Type.Params.List {
			for _, name := range field.Names {
				id := next
				next++
				if name == root {
					id = hookRootBinding
				}
				fnScope.define(name.Name, commandBinding{id: id})
			}
		}
	}
	bindFieldList(hook.Type.Results, fnScope, &next)
	var out []string
	walkHookNode(hook.Body, fnScope, &out, &next)
	return out
}

func novelHookLiteral(n ast.Node) (*ast.FuncLit, bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil, false
	}
	name, ok := call.Fun.(*ast.Ident)
	if !ok || name.Name != "registerNovelCommand" {
		return nil, false
	}
	hook, ok := call.Args[0].(*ast.FuncLit)
	if !ok {
		return nil, false
	}
	return hook, true
}

func hookRootIdent(hook *ast.FuncLit) *ast.Ident {
	if hook.Type == nil || hook.Type.Params == nil {
		return nil
	}
	for _, field := range hook.Type.Params.List {
		if !isStarCobraCommand(field.Type) || len(field.Names) == 0 {
			continue
		}
		return field.Names[0]
	}
	return nil
}

func bindFieldList(fields *ast.FieldList, s *commandScope, next *int) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			*next++
			s.define(name.Name, commandBinding{id: *next})
		}
	}
}

func walkHookNode(n ast.Node, s *commandScope, out *[]string, next *int) {
	if n == nil || s == nil || nodeIsNil(n) {
		return
	}
	switch n := n.(type) {
	case *ast.BlockStmt:
		child := s.child()
		for _, stmt := range n.List {
			walkHookNode(stmt, child, out, next)
		}
	case *ast.AssignStmt:
		for _, rhs := range n.Rhs {
			walkHookNode(rhs, s, out, next)
		}
		if n.Tok != token.DEFINE {
			return
		}
		for i, lhs := range n.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			factory := ""
			if i < len(n.Rhs) {
				factory = commandFactoryCallName(n.Rhs[i])
			}
			*next++
			s.define(id.Name, commandBinding{id: *next, factory: factory})
		}
	case *ast.DeclStmt:
		gen, ok := n.Decl.(*ast.GenDecl)
		if !ok {
			return
		}
		for _, spec := range gen.Specs {
			switch spec := spec.(type) {
			case *ast.ValueSpec:
				for _, value := range spec.Values {
					walkHookNode(value, s, out, next)
				}
				for i, name := range spec.Names {
					factory := ""
					if gen.Tok == token.VAR && i < len(spec.Values) {
						factory = commandFactoryCallName(spec.Values[i])
					}
					*next++
					s.define(name.Name, commandBinding{id: *next, factory: factory})
				}
			case *ast.TypeSpec:
				if spec.Name != nil {
					*next++
					s.define(spec.Name.Name, commandBinding{id: *next})
				}
			}
		}
	case *ast.FuncLit:
		child := s.child()
		if n.Type != nil {
			bindFieldList(n.Type.Params, child, next)
			bindFieldList(n.Type.Results, child, next)
		}
		walkHookNode(n.Body, child, out, next)
	case *ast.IfStmt:
		child := s.child()
		walkHookNode(n.Init, child, out, next)
		walkHookNode(n.Cond, child, out, next)
		walkHookNode(n.Body, child, out, next)
		// The init statement's scope includes the else branch.
		walkHookNode(n.Else, child, out, next)
	case *ast.ForStmt:
		child := s.child()
		walkHookNode(n.Init, child, out, next)
		walkHookNode(n.Cond, child, out, next)
		walkHookNode(n.Post, child, out, next)
		walkHookNode(n.Body, child, out, next)
	case *ast.RangeStmt:
		walkHookNode(n.X, s, out, next)
		child := s.child()
		if n.Tok == token.DEFINE {
			bindRangeName(n.Key, child, next)
			bindRangeName(n.Value, child, next)
		}
		walkHookNode(n.Body, child, out, next)
	case *ast.SwitchStmt:
		child := s.child()
		walkHookNode(n.Init, child, out, next)
		walkHookNode(n.Tag, child, out, next)
		walkHookNode(n.Body, child, out, next)
	case *ast.TypeSwitchStmt:
		child := s.child()
		walkHookNode(n.Init, child, out, next)
		walkHookNode(n.Assign, child, out, next)
		walkHookNode(n.Body, child, out, next)
	case *ast.CaseClause:
		for _, expr := range n.List {
			walkHookNode(expr, s, out, next)
		}
		child := s.child()
		for _, stmt := range n.Body {
			walkHookNode(stmt, child, out, next)
		}
	case *ast.CommClause:
		child := s.child()
		walkHookNode(n.Comm, child, out, next)
		for _, stmt := range n.Body {
			walkHookNode(stmt, child, out, next)
		}
	case *ast.CallExpr:
		if hook, ok := novelHookLiteral(n); ok {
			*out = append(*out, factoriesInHook(hook, s)...)
			return
		}
		if recordHookRegistration(n, s, out) {
			return
		}
		walkHookNode(n.Fun, s, out, next)
		for _, arg := range n.Args {
			walkHookNode(arg, s, out, next)
		}
	default:
		walkHookChildren(n, s, out, next)
	}
}

func bindRangeName(expr ast.Expr, s *commandScope, next *int) {
	id, ok := expr.(*ast.Ident)
	if !ok {
		return
	}
	*next++
	s.define(id.Name, commandBinding{id: *next})
}

func recordHookRegistration(call *ast.CallExpr, s *commandScope, out *[]string) bool {
	var parent ast.Expr
	var args []ast.Expr
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		if fun.Sel == nil || fun.Sel.Name != "AddCommand" {
			return false
		}
		parent = fun.X
		args = call.Args
	case *ast.Ident:
		if fun.Name != "addNovelCommandIfAbsent" || len(call.Args) != 2 {
			return false
		}
		parent = call.Args[0]
		args = call.Args[1:]
	default:
		return false
	}
	ident, ok := parent.(*ast.Ident)
	if !ok {
		return true
	}
	binding, ok := s.lookup(ident.Name)
	if !ok || binding.id != hookRootBinding {
		return true
	}
	for _, arg := range args {
		if factory := registrationFactory(arg, s); factory != "" {
			*out = append(*out, factory)
		}
	}
	return true
}

func registrationFactory(expr ast.Expr, s *commandScope) string {
	if name := commandFactoryCallName(expr); name != "" {
		return name
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return ""
	}
	binding, ok := s.lookup(ident.Name)
	if !ok {
		return ""
	}
	return binding.factory
}

func walkHookChildren(n ast.Node, s *commandScope, out *[]string, next *int) {
	v := reflect.ValueOf(n)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return
	}
	for _, field := range v.Fields() {
		if !field.CanInterface() {
			continue
		}
		walkHookValue(field, s, out, next)
	}
}

func walkHookValue(v reflect.Value, s *commandScope, out *[]string, next *int) {
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return
		}
		if child, ok := v.Interface().(ast.Node); ok {
			walkHookNode(child, s, out, next)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			walkHookValue(v.Index(i), s, out, next)
		}
	}
}

func nodeIsNil(n ast.Node) bool {
	v := reflect.ValueOf(n)
	return v.Kind() == reflect.Pointer && v.IsNil()
}
