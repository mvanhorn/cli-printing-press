package generator

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
)

// NovelFeatureExclusivityClaim is the Unique Features / Unique Capabilities
// lead-in. Omit it when research listed another tool for the API. Dogfood
// rewrites those sections after generate, so it has to use the same sentence.
const NovelFeatureExclusivityClaim = "These capabilities aren't available in any other tool for this API."

// listedDocCommand is one extra row in the generated command listing.
// Fields are exported for the README and SKILL templates.
type listedDocCommand struct {
	Invocation  string
	Description string
}

// listedDocCommandGroup is a ### / bold heading plus its commands.
type listedDocCommandGroup struct {
	Heading  string
	Commands []listedDocCommand
}

type docCmd struct {
	path        string
	invocation  string
	description string
}

func (g *Generator) additionalCommandGroups() []listedDocCommandGroup {
	return g.docCommandGroups(false)
}

func (g *Generator) referenceCommandGroups() []listedDocCommandGroup {
	return g.docCommandGroups(true)
}

// docCommandGroups lists commands the generator knows about that the spec
// resource loop does not already print. referenceOnly is the SKILL Command
// Reference view: extra_commands stay out of it because they have no
// generated Cobra registration, and verify-skill treats that section as
// generator-owned paths.
func (g *Generator) docCommandGroups(referenceOnly bool) []listedDocCommandGroup {
	if g == nil || g.Spec == nil {
		return nil
	}
	blocked := g.generatedCommandPaths()
	framework := g.activeFrameworkCobraUseNames()
	var cmds []docCmd
	seen := map[string]struct{}{}
	add := func(path, invocation, description string) {
		path = strings.TrimSpace(path)
		invocation = strings.TrimSpace(invocation)
		if path == "" || invocation == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		if _, ok := blocked[path]; ok {
			return
		}
		if !strings.Contains(path, " ") {
			if _, ok := framework[path]; ok {
				return
			}
		}
		seen[path] = struct{}{}
		cmds = append(cmds, docCmd{path: path, invocation: invocation, description: description})
	}

	for _, feature := range g.NovelFeatures {
		path, invocation, ok := novelDocInvocation(feature.Command)
		if !ok {
			continue
		}
		add(path, invocation, docCommandDescription(feature.Description, feature.Name, feature.Command))
	}
	for _, hook := range commandsFromNovelHooks(g.OutputDir) {
		add(hook.path, hook.invocation, docCommandDescription(hook.description))
	}
	if !referenceOnly {
		for _, extra := range g.Spec.ExtraCommands {
			path, ok := extraDocPath(extra.Name)
			if !ok {
				continue
			}
			invocation := strings.TrimSpace(extra.Name)
			if args := strings.TrimSpace(extra.Args); args != "" {
				invocation += " " + args
			}
			add(path, invocation, docCommandDescription(extra.Description))
		}
	}
	return groupDocCommands(cmds)
}

func docCommandDescription(parts ...string) string {
	for _, part := range parts {
		if desc := naming.OneLineNormalize(part); desc != "" {
			return desc
		}
	}
	return "Hand-written command"
}

func novelDocInvocation(command string) (path, invocation string, ok bool) {
	parts := novelFeatureCommandParts(command)
	if len(parts) == 0 {
		return "", "", false
	}
	leaf := novelFeatureUse(parts[len(parts)-1], command)
	if len(parts) == 1 {
		invocation = leaf
	} else {
		invocation = strings.Join(parts[:len(parts)-1], " ") + " " + leaf
	}
	return novelFeatureCommandKey(parts), invocation, true
}

func extraDocPath(name string) (string, bool) {
	parts := novelFeatureCommandParts(name)
	if len(parts) == 0 {
		return "", false
	}
	return novelFeatureCommandKey(parts), true
}

func groupDocCommands(cmds []docCmd) []listedDocCommandGroup {
	if len(cmds) == 0 {
		return nil
	}
	var order []string
	by := map[string][]listedDocCommand{}
	for _, cmd := range cmds {
		fields := strings.Fields(cmd.path)
		if len(fields) == 0 {
			continue
		}
		heading := fields[0]
		if _, ok := by[heading]; !ok {
			order = append(order, heading)
		}
		by[heading] = append(by[heading], listedDocCommand{
			Invocation:  cmd.invocation,
			Description: cmd.description,
		})
	}
	if len(order) == 0 {
		return nil
	}
	// Headings sort so the listing does not depend on map iteration.
	// Commands keep first-seen order within a heading.
	slices.Sort(order)
	out := make([]listedDocCommandGroup, 0, len(order))
	for _, heading := range order {
		out = append(out, listedDocCommandGroup{Heading: heading, Commands: by[heading]})
	}
	return out
}

type hookCommand struct {
	path        string
	invocation  string
	description string
}

// commandsFromNovelHooks lists registerNovelCommand hooks whose parent path
// and Use are string literals in preserved internal/cli sources. Helper
// constructors, dynamic Use values, and commands built in a loop stay
// runtime-only; agent-context is the live inventory for those.
func commandsFromNovelHooks(outputDir string) []hookCommand {
	// An empty output dir would scan the process working directory.
	if strings.TrimSpace(outputDir) == "" {
		return nil
	}
	dir := filepath.Join(outputDir, "internal", "cli")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []hookCommand
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		src, err := os.ReadFile(path)
		if err != nil || !bytes.Contains(src, []byte("registerNovelCommand(")) {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution)
		if err != nil {
			// A preserved file that does not parse should not fail doc generation.
			continue
		}
		out = append(out, novelHookCommands(file)...)
	}
	return out
}

func novelHookCommands(file *ast.File) []hookCommand {
	funcs := map[string]*ast.FuncDecl{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name == nil || fn.Body == nil {
			continue
		}
		funcs[fn.Name.Name] = fn
	}
	seenFn := map[string]bool{}
	seenLit := map[*ast.FuncLit]bool{}
	var out []hookCommand
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "registerNovelCommand" || len(call.Args) != 1 {
			return true
		}
		out = append(out, walkRegisteredHook(call.Args[0], funcs, seenFn, seenLit)...)
		return false
	})
	return out
}

func walkRegisteredHook(arg ast.Expr, funcs map[string]*ast.FuncDecl, seenFn map[string]bool, seenLit map[*ast.FuncLit]bool) []hookCommand {
	switch arg := arg.(type) {
	case *ast.FuncLit:
		if seenLit[arg] {
			return nil
		}
		seenLit[arg] = true
		return walkHookFunc(arg)
	case *ast.Ident:
		if arg.Name == "" || seenFn[arg.Name] {
			return nil
		}
		fn := funcs[arg.Name]
		if fn == nil {
			return nil
		}
		seenFn[arg.Name] = true
		return walkHookFunc(&ast.FuncLit{Type: fn.Type, Body: fn.Body})
	default:
		return nil
	}
}

type hookNode struct {
	leaf       string
	invocation string
	short      string
	boundPath  string
	children   []*hookNode
}

type hookState struct {
	paths   map[string]string
	pending map[string]*hookNode
	out     []hookCommand
}

func walkHookFunc(fn *ast.FuncLit) []hookCommand {
	if fn == nil || fn.Type == nil || fn.Type.Params == nil || len(fn.Type.Params.List) == 0 || fn.Body == nil {
		return nil
	}
	names := fn.Type.Params.List[0].Names
	if len(names) == 0 || names[0] == nil || names[0].Name == "" || names[0].Name == "_" {
		return nil
	}
	st := &hookState{
		paths:   map[string]string{names[0].Name: ""},
		pending: map[string]*hookNode{},
	}
	walkHookBlock(fn.Body, st)
	return st.out
}

func walkHookBlock(block *ast.BlockStmt, st *hookState) {
	if block == nil {
		return
	}
	for _, stmt := range block.List {
		walkHookStmt(stmt, st)
	}
}

func walkHookStmt(stmt ast.Stmt, st *hookState) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		walkHookBlock(s, st)
	case *ast.IfStmt:
		walkHookStmt(s.Init, st)
		walkHookBlock(s.Body, st)
		walkHookStmt(s.Else, st)
	case *ast.SwitchStmt:
		walkHookStmt(s.Init, st)
		walkHookBlock(s.Body, st)
	case *ast.CaseClause:
		for _, inner := range s.Body {
			walkHookStmt(inner, st)
		}
	case *ast.LabeledStmt:
		walkHookStmt(s.Stmt, st)
	case *ast.AssignStmt:
		st.handleAssign(s)
	case *ast.DeclStmt:
		st.handleDecl(s)
	case *ast.ExprStmt:
		call, ok := s.X.(*ast.CallExpr)
		if ok {
			st.handleCall(call)
		}
	}
}

func (st *hookState) handleAssign(stmt *ast.AssignStmt) {
	if stmt == nil || len(stmt.Rhs) != 1 {
		return
	}
	if recv, segments, ok := findCall(stmt.Rhs[0]); ok {
		recvIdent, ok := recv.(*ast.Ident)
		if !ok {
			return
		}
		parentPath, known := st.resolvedPath(recvIdent.Name)
		if !known || len(stmt.Lhs) == 0 {
			return
		}
		lhs, ok := stmt.Lhs[0].(*ast.Ident)
		if !ok || lhs.Name == "" || lhs.Name == "_" {
			return
		}
		st.paths[lhs.Name] = joinDocPath(parentPath, segments...)
		delete(st.pending, lhs.Name)
		return
	}
	if len(stmt.Lhs) != 1 {
		return
	}
	lhs, ok := stmt.Lhs[0].(*ast.Ident)
	if !ok || lhs.Name == "" || lhs.Name == "_" {
		return
	}
	node, ok := hookNodeFromComposite(stmt.Rhs[0])
	if !ok {
		return
	}
	delete(st.paths, lhs.Name)
	st.pending[lhs.Name] = node
}

func (st *hookState) handleDecl(stmt *ast.DeclStmt) {
	gen, ok := stmt.Decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.VAR {
		return
	}
	for _, spec := range gen.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 || vs.Names[0] == nil {
			continue
		}
		node, ok := hookNodeFromComposite(vs.Values[0])
		if !ok || vs.Names[0].Name == "" || vs.Names[0].Name == "_" {
			continue
		}
		delete(st.paths, vs.Names[0].Name)
		st.pending[vs.Names[0].Name] = node
	}
}

func (st *hookState) handleCall(call *ast.CallExpr) {
	if call == nil {
		return
	}
	if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "addNovelCommandIfAbsent" && len(call.Args) == 2 {
		st.attach(call.Args[0], call.Args[1])
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "AddCommand" {
		return
	}
	for _, arg := range call.Args {
		st.attach(sel.X, arg)
	}
}

func (st *hookState) attach(parentExpr, candExpr ast.Expr) {
	parentIdent, ok := parentExpr.(*ast.Ident)
	if !ok {
		return
	}
	node, ident, ok := st.candidateNode(candExpr)
	if !ok {
		return
	}
	if parentPath, known := st.resolvedPath(parentIdent.Name); known {
		st.bindNode(ident, parentPath, node)
		return
	}
	parentNode, ok := st.pending[parentIdent.Name]
	if !ok || parentNode.boundPath != "" {
		return
	}
	parentNode.children = append(parentNode.children, node)
}

func (st *hookState) candidateNode(expr ast.Expr) (*hookNode, string, bool) {
	if node, ok := hookNodeFromComposite(expr); ok {
		return node, "", true
	}
	id, ok := expr.(*ast.Ident)
	if !ok || id.Name == "" || id.Name == "_" {
		return nil, "", false
	}
	node, ok := st.pending[id.Name]
	if !ok || node.boundPath != "" {
		return nil, "", false
	}
	return node, id.Name, true
}

func (st *hookState) bindNode(ident, parentPath string, node *hookNode) {
	if node == nil || node.boundPath != "" {
		return
	}
	full := joinDocPath(parentPath, node.leaf)
	invocation := node.invocation
	if parentPath != "" {
		invocation = parentPath + " " + node.invocation
	}
	node.boundPath = full
	st.out = append(st.out, hookCommand{
		path:        full,
		invocation:  invocation,
		description: node.short,
	})
	if ident != "" {
		st.paths[ident] = full
		delete(st.pending, ident)
	}
	for _, child := range node.children {
		st.bindNode("", full, child)
	}
}

func (st *hookState) resolvedPath(name string) (string, bool) {
	if path, ok := st.paths[name]; ok {
		return path, true
	}
	if node, ok := st.pending[name]; ok && node.boundPath != "" {
		return node.boundPath, true
	}
	return "", false
}

func hookNodeFromComposite(expr ast.Expr) (*hookNode, bool) {
	use, short, ok := commandLiteral(expr)
	if !ok {
		return nil, false
	}
	leaf, invocation, ok := splitCobraUse(use)
	if !ok {
		return nil, false
	}
	return &hookNode{leaf: leaf, invocation: invocation, short: short}, true
}

func commandLiteral(expr ast.Expr) (use, short string, ok bool) {
	unary, ok := expr.(*ast.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return "", "", false
	}
	lit, ok := unary.X.(*ast.CompositeLit)
	if !ok || !isCobraCommandType(lit.Type) {
		return "", "", false
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Use":
			use, ok = stringLiteral(kv.Value)
			if !ok {
				return "", "", false
			}
		case "Short":
			if s, ok := stringLiteral(kv.Value); ok {
				short = s
			}
		}
	}
	if use == "" {
		return "", "", false
	}
	return use, short, true
}

func isCobraCommandType(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name == "Command"
	case *ast.SelectorExpr:
		return t.Sel != nil && t.Sel.Name == "Command"
	default:
		return false
	}
}

func findCall(expr ast.Expr) (recv ast.Expr, segments []string, ok bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil, nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "Find" {
		return nil, nil, false
	}
	segs, ok := stringSliceLiteral(call.Args[0])
	if !ok {
		return nil, nil, false
	}
	return sel.X, segs, true
}

func stringSliceLiteral(expr ast.Expr) ([]string, bool) {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	if lit.Type != nil {
		arr, ok := lit.Type.(*ast.ArrayType)
		if !ok || arr.Len != nil {
			return nil, false
		}
		elt, ok := arr.Elt.(*ast.Ident)
		if !ok || elt.Name != "string" {
			return nil, false
		}
	}
	out := make([]string, 0, len(lit.Elts))
	for _, elt := range lit.Elts {
		s, ok := stringLiteral(elt)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

func splitCobraUse(use string) (leaf, invocation string, ok bool) {
	var inv []string
	var leafSet bool
	for field := range strings.FieldsSeq(use) {
		if strings.HasPrefix(field, "-") {
			break
		}
		inv = append(inv, field)
		if leafSet || strings.Contains(field, "<") || strings.Contains(field, "[") {
			continue
		}
		leaf = toKebab(field)
		leafSet = true
	}
	if !leafSet || leaf == "" {
		return "", "", false
	}
	return leaf, strings.Join(inv, " "), true
}

func joinDocPath(parent string, segments ...string) string {
	var parts []string
	if parent != "" {
		parts = append(parts, strings.Fields(parent)...)
	}
	for _, segment := range segments {
		for field := range strings.FieldsSeq(segment) {
			if field == "" {
				continue
			}
			parts = append(parts, toKebab(field))
		}
	}
	return strings.Join(parts, " ")
}
