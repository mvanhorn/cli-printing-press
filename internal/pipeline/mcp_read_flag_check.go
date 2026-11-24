package pipeline

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
)

// MCPReadFlagCheckResult is the dogfood gate for hand-written commands that
// open a local file named by a string flag. The printed cobratree walker
// only drops that flag from the MCP schema when the name is on the built-in
// read or destination blocklist, or the command lists it in mcp:read-flags
// or mcp:write-flags. A missing annotation is agent-reachable.
type MCPReadFlagCheckResult struct {
	Checked  int                  `json:"checked"`
	Findings []MCPReadFlagFinding `json:"findings,omitempty"`
	Skipped  bool                 `json:"skipped,omitempty"`
}

// MCPReadFlagFinding names the exposed command and the flag to annotate.
type MCPReadFlagFinding struct {
	Command string `json:"command"`
	Flag    string `json:"flag"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Reason  string `json:"reason"`
}

// mcpFrameworkCommands matches cobratree frameworkCommands. Top-level
// commands in this set are not MCP tools. A nested command with the same
// leaf name still is.
var mcpFrameworkCommands = map[string]bool{
	"about":         true,
	"agent-context": true,
	"api":           true,
	"auth":          true,
	"completion":    true,
	"context":       true,
	"doctor":        true,
	"feedback":      true,
	"help":          true,
	"profile":       true,
	"version":       true,
	"which":         true,
}

func checkMCPReadFlags(cliDir string) MCPReadFlagCheckResult {
	cliPkg := filepath.Join(cliDir, "internal", "cli")
	entries, err := os.ReadDir(cliPkg)
	if err != nil {
		return MCPReadFlagCheckResult{Skipped: true}
	}
	fset := token.NewFileSet()
	var files []mcpCLIFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(cliPkg, name)
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		hand := !isGeneratedPrintingPressFile(string(src))
		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			if hand {
				files = append(files, mcpCLIFile{name: name, hand: true})
			}
			continue
		}
		files = append(files, mcpCLIFile{name: name, file: file, hand: hand})
	}
	if len(files) == 0 {
		return MCPReadFlagCheckResult{}
	}
	a := newMCPReadAnalyzer(fset)
	findings := a.analyze(files)
	checked := 0
	for _, file := range files {
		if file.hand {
			checked++
		}
	}
	return MCPReadFlagCheckResult{Checked: checked, Findings: findings}
}

type mcpCLIFile struct {
	name string
	file *ast.File
	hand bool
}

type mcpObj struct {
	pos        token.Pos
	name       string
	importPath string
}

type mcpScope struct {
	parent *mcpScope
	objs   map[string]*mcpObj
}

func (s *mcpScope) child() *mcpScope {
	return &mcpScope{parent: s, objs: map[string]*mcpObj{}}
}

func (s *mcpScope) lookup(name string) *mcpObj {
	for cur := s; cur != nil; cur = cur.parent {
		if o, ok := cur.objs[name]; ok {
			return o
		}
	}
	return nil
}

func (s *mcpScope) define(name string, pos token.Pos) *mcpObj {
	o := &mcpObj{pos: pos, name: name}
	s.objs[name] = o
	return o
}

type mcpCmd struct {
	use         string
	ann         map[string]string
	hidden      bool
	cobraHidden bool
	ambiguous   bool
	group       bool
	endpoint    bool
	parent      *mcpCmd
	children    []*mcpCmd
}

func (c *mcpCmd) path() string {
	if c == nil {
		return ""
	}
	var parts []string
	seen := map[*mcpCmd]bool{}
	for n := c; n != nil && !seen[n]; n = n.parent {
		seen[n] = true
		if leaf := cobraLeafName(n.use); leaf != "" {
			parts = append([]string{leaf}, parts...)
		}
	}
	return strings.Join(parts, " ")
}

type mcpFunc struct {
	name     string
	params   []*mcpObj
	variadic bool
	results  []*mcpObj
	returns  []mcpReturn
	body     *ast.BlockStmt
}

type mcpReturn struct {
	exprs []ast.Expr
	scope *mcpScope
}

type mcpLit struct {
	params   []*mcpObj
	variadic bool
	results  []*mcpObj
	returns  []mcpReturn
}

type mcpFlag struct {
	name       string
	cmdObj     *mcpObj
	persistent bool
	optional   bool
	literal    bool
	fn         string
}

type mcpInvoke struct {
	caller string
	callee string
	cond   bool
}

type mcpEdge struct {
	from token.Pos
	to   token.Pos
}

type mcpAnalyzer struct {
	fset         *token.FileSet
	pkg          *mcpScope
	identObj     map[token.Pos]*mcpObj
	cmds         map[token.Pos]*mcpCmd
	funcCmd      map[string]*mcpCmd
	funcs        map[string]*mcpFunc
	methods      map[string][]*mcpFunc
	lits         map[token.Pos]*mcpLit
	flags        []mcpFlag
	edges        []mcpEdge
	seed         map[token.Pos][]int
	sinks        []mcpSink
	lookups      []mcpLookup
	runBinds     []mcpRunBind
	invokes      []mcpInvoke
	flagAt       map[token.Pos][]int
	fnReach      map[string]int
	mustMemo     map[string]mustResult
	computing    map[string]bool
	links        []mcpLink
	pending      []mcpFlow
	strVal       map[token.Pos]string
	strBad       map[token.Pos]bool
	flagSetAlias map[token.Pos]mcpFlagSet
	sinkAlias    map[token.Pos]bool
	selObjs      map[string]*mcpObj
	nodePos      map[ast.Node]token.Pos
	nextPos      token.Pos
	reach        map[token.Pos]map[int]bool
}

type mcpFlagSet struct {
	cmd        *mcpObj
	persistent bool
}

type mcpSink struct {
	arg   ast.Expr
	scope *mcpScope
	file  string
	line  int
}

type mcpLookup struct {
	call           *ast.CallExpr
	cmdObj         *mcpObj
	persistentOnly bool
	name           string
}

type mcpRunBind struct {
	cmd    *mcpCmd
	cmdObj *mcpObj
	rhs    ast.Expr
}

type mcpFlow struct {
	lhs     *mcpObj
	rhs     ast.Expr
	scope   *mcpScope
	call    *ast.CallExpr
	multi   []*mcpObj
	file    string
	hand    bool
	imports map[string]string
	dots    []string
}

type mcpLink struct {
	parent ast.Expr
	child  ast.Expr
	scope  *mcpScope
}

type mcpFnCtx struct {
	file    *mcpCLIFile
	fn      *mcpFunc
	lit     *mcpLit
	imports map[string]string
	dots    []string
	scope   *mcpScope
	cond    int
}

func newMCPReadAnalyzer(fset *token.FileSet) *mcpAnalyzer {
	return &mcpAnalyzer{
		fset:     fset,
		pkg:      &mcpScope{objs: map[string]*mcpObj{}},
		identObj: map[token.Pos]*mcpObj{},
		cmds:     map[token.Pos]*mcpCmd{},
		funcCmd:  map[string]*mcpCmd{},
		funcs:    map[string]*mcpFunc{},
		methods:  map[string][]*mcpFunc{},
		lits:     map[token.Pos]*mcpLit{},
		seed:     map[token.Pos][]int{},
		strVal:   map[token.Pos]string{},
		strBad:   map[token.Pos]bool{},
		nextPos:  1 << 30,
	}
}

func (a *mcpAnalyzer) synth(name string) *mcpObj {
	a.nextPos++
	return &mcpObj{pos: a.nextPos, name: name}
}

func (a *mcpAnalyzer) analyze(files []mcpCLIFile) []MCPReadFlagFinding {
	var ctxs []*mcpFnCtx
	for i := range files {
		if files[i].file == nil {
			continue
		}
		ctxs = append(ctxs, a.prepareFile(&files[i]))
	}
	for _, ctx := range ctxs {
		a.walkDecls(ctx)
	}
	a.markOptionalFlags()
	a.sealGuaranteedFlags()
	a.finishFlows()
	a.linkCommands()
	a.bindRuns()
	a.propagateCommands()
	a.resolveLookups()
	a.propagate()
	return a.findings()
}

func (a *mcpAnalyzer) prepareFile(file *mcpCLIFile) *mcpFnCtx {
	ctx := &mcpFnCtx{file: file, imports: map[string]string{}, scope: a.pkg.child()}
	for _, spec := range file.file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		local := filepath.Base(path)
		switch {
		case spec.Name == nil:
		case spec.Name.Name == "_":
			continue
		case spec.Name.Name == ".":
			ctx.dots = append(ctx.dots, path)
			continue
		default:
			local = spec.Name.Name
		}
		obj := ctx.scope.define(local, spec.Pos())
		obj.importPath = path
		ctx.imports[local] = path
		a.identObj[obj.pos] = obj
	}
	for _, decl := range file.file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		a.walkGen(gen, ctx.scope, ctx)
	}
	return ctx
}

func (a *mcpAnalyzer) walkDecls(ctx *mcpFnCtx) {
	for _, decl := range ctx.file.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		a.walkFunc(fn, ctx.scope, ctx)
	}
}

func (a *mcpAnalyzer) walkFunc(fn *ast.FuncDecl, parent *mcpScope, fileCtx *mcpFnCtx) {
	if fn.Name == nil || fn.Name.Name == "" || fn.Name.Name == "_" {
		return
	}
	scope := parent.child()
	pf := &mcpFunc{name: fn.Name.Name}
	if fn.Type.Params != nil {
		pf.params, pf.variadic = a.defineFields(fn.Type.Params, scope)
	}
	if fn.Recv != nil {
		a.methods[fn.Name.Name] = append(a.methods[fn.Name.Name], pf)
	} else {
		obj := a.pkg.define(fn.Name.Name, fn.Name.Pos())
		a.identObj[fn.Name.Pos()] = obj
		a.funcs[fn.Name.Name] = pf
	}
	ctx := *fileCtx
	ctx.fn = pf
	pf.body = fn.Body
	if fn.Body != nil {
		a.walkBlock(fn.Body, scope, &ctx)
	}
}

func (a *mcpAnalyzer) defineFields(fl *ast.FieldList, scope *mcpScope) ([]*mcpObj, bool) {
	var out []*mcpObj
	variadic := false
	for _, field := range fl.List {
		if _, ok := field.Type.(*ast.Ellipsis); ok {
			variadic = true
		}
		if len(field.Names) == 0 {
			out = append(out, a.synth(""))
			continue
		}
		for _, name := range field.Names {
			if name.Name == "_" {
				out = append(out, a.synth("_"))
				continue
			}
			obj := scope.define(name.Name, name.Pos())
			a.identObj[name.Pos()] = obj
			out = append(out, obj)
		}
	}
	return out, variadic
}

func (a *mcpAnalyzer) walkGen(decl *ast.GenDecl, scope *mcpScope, ctx *mcpFnCtx) {
	switch decl.Tok {
	case token.VAR, token.CONST:
	default:
		return
	}
	// File-level consts and vars are package-scoped. A helper in another
	// file has to see the same object the flag binds.
	if scope.parent == a.pkg {
		scope = a.pkg
	}
	for _, spec := range decl.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, name := range vs.Names {
			if name.Name == "_" {
				continue
			}
			obj := scope.define(name.Name, name.Pos())
			a.identObj[name.Pos()] = obj
			if i < len(vs.Values) {
				a.walkExpr(vs.Values[i], scope, ctx)
				a.noteString(obj, vs.Values[i], scope)
				a.pending = append(a.pending, mcpFlow{lhs: obj, rhs: vs.Values[i], scope: scope, imports: ctx.imports, dots: ctx.dots})
			}
		}
	}
}

func (a *mcpAnalyzer) walkBlock(block *ast.BlockStmt, scope *mcpScope, ctx *mcpFnCtx) {
	if block == nil {
		return
	}
	inner := scope.child()
	for _, stmt := range block.List {
		a.walkStmt(stmt, inner, ctx)
	}
}

func (a *mcpAnalyzer) walkStmt(stmt ast.Stmt, scope *mcpScope, ctx *mcpFnCtx) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		a.walkBlock(s, scope, ctx)
	case *ast.DeclStmt:
		if d, ok := s.Decl.(*ast.GenDecl); ok {
			a.walkGen(d, scope, ctx)
		}
	case *ast.AssignStmt:
		a.walkAssign(s, scope, ctx)
	case *ast.ExprStmt:
		a.walkExpr(s.X, scope, ctx)
	case *ast.ReturnStmt:
		for _, e := range s.Results {
			a.walkExpr(e, scope, ctx)
		}
		ret := mcpReturn{exprs: s.Results, scope: scope}
		switch {
		case ctx.lit != nil:
			ctx.lit.returns = append(ctx.lit.returns, ret)
		case ctx.fn != nil:
			ctx.fn.returns = append(ctx.fn.returns, ret)
		}
	case *ast.IfStmt:
		inner := scope.child()
		if s.Init != nil {
			a.walkStmt(s.Init, inner, ctx)
		}
		a.walkExpr(s.Cond, inner, ctx)
		a.walkConditional(s.Body, inner, ctx)
		a.walkConditional(s.Else, inner, ctx)
	case *ast.ForStmt:
		inner := scope.child()
		if s.Init != nil {
			a.walkStmt(s.Init, inner, ctx)
		}
		a.walkExpr(s.Cond, inner, ctx)
		a.walkStmt(s.Post, inner, ctx)
		a.walkConditional(s.Body, inner, ctx)
	case *ast.RangeStmt:
		inner := scope.child()
		a.walkExpr(s.X, scope, ctx)
		if s.Tok == token.DEFINE {
			a.defineIfNew(s.Key, inner)
			a.defineIfNew(s.Value, inner)
		}
		a.walkConditional(s.Body, inner, ctx)
	case *ast.SwitchStmt:
		inner := scope.child()
		if s.Init != nil {
			a.walkStmt(s.Init, inner, ctx)
		}
		a.walkExpr(s.Tag, inner, ctx)
		a.walkConditional(s.Body, inner, ctx)
	case *ast.TypeSwitchStmt:
		inner := scope.child()
		if s.Init != nil {
			a.walkStmt(s.Init, inner, ctx)
		}
		a.walkStmt(s.Assign, inner, ctx)
		a.walkConditional(s.Body, inner, ctx)
	case *ast.CaseClause:
		inner := scope.child()
		for _, e := range s.List {
			a.walkExpr(e, scope, ctx)
		}
		for _, st := range s.Body {
			a.walkConditional(st, inner, ctx)
		}
	case *ast.SelectStmt:
		a.walkConditional(s.Body, scope, ctx)
	case *ast.CommClause:
		inner := scope.child()
		a.walkStmt(s.Comm, scope, ctx)
		for _, st := range s.Body {
			a.walkConditional(st, inner, ctx)
		}
	case *ast.GoStmt:
		a.walkExpr(s.Call, scope, ctx)
	case *ast.DeferStmt:
		a.walkExpr(s.Call, scope, ctx)
	case *ast.SendStmt:
		a.walkExpr(s.Chan, scope, ctx)
		a.walkExpr(s.Value, scope, ctx)
	case *ast.IncDecStmt:
		a.walkExpr(s.X, scope, ctx)
	case *ast.LabeledStmt:
		a.walkStmt(s.Stmt, scope, ctx)
	}
}

func (a *mcpAnalyzer) defineIfNew(e ast.Expr, scope *mcpScope) {
	id, ok := e.(*ast.Ident)
	if !ok || id.Name == "_" {
		return
	}
	if _, exists := scope.objs[id.Name]; exists {
		return
	}
	obj := scope.define(id.Name, id.Pos())
	a.identObj[id.Pos()] = obj
}

func (a *mcpAnalyzer) walkAssign(stmt *ast.AssignStmt, scope *mcpScope, ctx *mcpFnCtx) {
	for _, rhs := range stmt.Rhs {
		a.walkExpr(rhs, scope, ctx)
	}
	if stmt.Tok == token.DEFINE {
		for _, lhs := range stmt.Lhs {
			a.defineIfNew(lhs, scope)
		}
	}
	if len(stmt.Rhs) == 1 && len(stmt.Lhs) > 1 {
		if _, ok := stmt.Rhs[0].(*ast.CallExpr); ok {
			var multi []*mcpObj
			for _, lhs := range stmt.Lhs {
				multi = append(multi, a.exprObj(lhs, scope))
			}
			a.pending = append(a.pending, mcpFlow{rhs: stmt.Rhs[0], scope: scope, multi: multi, imports: ctx.imports, dots: ctx.dots})
			return
		}
	}
	if len(stmt.Lhs) != len(stmt.Rhs) {
		return
	}
	for i := range stmt.Lhs {
		a.noteAnnotationAssign(stmt.Lhs[i], stmt.Rhs[i], scope)
		a.noteRunAssign(stmt.Lhs[i], stmt.Rhs[i], scope)
		lhs := a.exprObj(stmt.Lhs[i], scope)
		if lhs == nil {
			continue
		}
		a.noteString(lhs, stmt.Rhs[i], scope)
		a.noteCommandAssign(lhs, stmt.Rhs[i], scope, ctx)
		a.noteFlagValue(lhs, stmt.Rhs[i], scope, ctx)
		a.rememberFlagSet(lhs, stmt.Rhs[i], scope)
		a.noteLitAssign(lhs, stmt.Rhs[i])
		a.pending = append(a.pending, mcpFlow{
			lhs:     lhs,
			rhs:     stmt.Rhs[i],
			scope:   scope,
			imports: ctx.imports,
			dots:    ctx.dots,
		})
	}
}

func (a *mcpAnalyzer) walkExpr(e ast.Expr, scope *mcpScope, ctx *mcpFnCtx) {
	if e == nil {
		return
	}
	switch n := e.(type) {
	case *ast.Ident:
		if obj := scope.lookup(n.Name); obj != nil {
			a.identObj[n.Pos()] = obj
		}
	case *ast.ParenExpr:
		a.walkExpr(n.X, scope, ctx)
	case *ast.StarExpr:
		a.walkExpr(n.X, scope, ctx)
	case *ast.UnaryExpr:
		a.walkExpr(n.X, scope, ctx)
	case *ast.SelectorExpr:
		a.walkExpr(n.X, scope, ctx)
	case *ast.IndexExpr:
		a.walkExpr(n.X, scope, ctx)
		a.walkExpr(n.Index, scope, ctx)
	case *ast.SliceExpr:
		a.walkExpr(n.X, scope, ctx)
	case *ast.BinaryExpr:
		a.walkExpr(n.X, scope, ctx)
		a.walkExpr(n.Y, scope, ctx)
	case *ast.KeyValueExpr:
		a.walkExpr(n.Key, scope, ctx)
		a.walkExpr(n.Value, scope, ctx)
	case *ast.CompositeLit:
		a.walkExpr(n.Type, scope, ctx)
		for _, elt := range n.Elts {
			a.walkExpr(elt, scope, ctx)
		}
		if cmd := a.commandFromLit(n, scope); cmd != nil {
			a.pendingCmdLit(n, cmd)
		}
	case *ast.CallExpr:
		a.walkExpr(n.Fun, scope, ctx)
		for _, arg := range n.Args {
			a.walkExpr(arg, scope, ctx)
		}
		a.noteCall(n, scope, ctx)
	case *ast.FuncLit:
		a.walkLit(n, scope, ctx)
	}
}

func (a *mcpAnalyzer) walkConditional(stmt ast.Stmt, scope *mcpScope, ctx *mcpFnCtx) {
	if stmt == nil || ctx == nil {
		return
	}
	next := *ctx
	next.cond++
	a.walkStmt(stmt, scope, &next)
}

func (a *mcpAnalyzer) walkLit(lit *ast.FuncLit, parent *mcpScope, ctx *mcpFnCtx) {
	scope := parent.child()
	lf := &mcpLit{}
	if lit.Type != nil && lit.Type.Params != nil {
		lf.params, lf.variadic = a.defineFields(lit.Type.Params, scope)
	}
	a.lits[lit.Pos()] = lf
	inner := *ctx
	inner.fn = nil
	inner.lit = lf
	// A function literal is not command construction. A flag registered here
	// is not guaranteed to exist when the MCP mirror reads the Cobra tree.
	inner.cond++
	a.walkBlock(lit.Body, scope, &inner)
}

func (a *mcpAnalyzer) pendingCmdLit(lit *ast.CompositeLit, cmd *mcpCmd) {
	// The variable this literal is assigned to is recorded from walkAssign.
	// Keep the literal's command on a side table keyed by the literal position
	// so the assign can pick it up. Stored in cmds under the literal pos until
	// the variable object is known; walkAssign moves it.
	a.cmds[lit.Pos()] = cmd
}

func (a *mcpAnalyzer) noteCommandAssign(lhs *mcpObj, rhs ast.Expr, scope *mcpScope, ctx *mcpFnCtx) {
	lit := compositeLit(rhs)
	if lit == nil {
		if id, ok := rhs.(*ast.Ident); ok {
			if obj := a.exprObj(id, scope); obj != nil {
				if cmd := a.cmds[obj.pos]; cmd != nil {
					a.cmds[lhs.pos] = cmd
				}
			}
		}
		return
	}
	cmd := a.cmds[lit.Pos()]
	if cmd == nil {
		cmd = a.commandFromLit(lit, scope)
	}
	if cmd != nil {
		a.cmds[lhs.pos] = cmd
		delete(a.cmds, lit.Pos())
	}
}

func compositeLit(e ast.Expr) *ast.CompositeLit {
	switch n := e.(type) {
	case *ast.CompositeLit:
		return n
	case *ast.UnaryExpr:
		if n.Op == token.AND {
			if lit, ok := n.X.(*ast.CompositeLit); ok {
				return lit
			}
		}
	case *ast.ParenExpr:
		return compositeLit(n.X)
	}
	return nil
}

func (a *mcpAnalyzer) commandFromLit(lit *ast.CompositeLit, scope *mcpScope) *mcpCmd {
	if !a.isCobraCommand(lit.Type, scope) {
		return nil
	}
	cmd := &mcpCmd{ann: map[string]string{}}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, _ := kv.Key.(*ast.Ident)
		if key == nil {
			continue
		}
		switch key.Name {
		case "Use":
			if v, ok := a.stringValue(kv.Value, scope); ok {
				cmd.use = v
			}
		case "Hidden":
			if id, ok := kv.Value.(*ast.Ident); ok && id.Name == "true" {
				cmd.cobraHidden = true
			}
		case "Annotations":
			maps.Copy(cmd.ann, a.stringMap(kv.Value, scope))
		case "Run", "RunE":
			a.runBinds = append(a.runBinds, mcpRunBind{cmd: cmd, rhs: kv.Value})
		}
	}
	a.classifyCmd(cmd)
	return cmd
}

func (a *mcpAnalyzer) classifyCmd(cmd *mcpCmd) {
	if cmd == nil {
		return
	}
	cmd.group = annotationTrue(cmd.ann, "pp:api-resource") || annotationTrue(cmd.ann, "pp:parent-group")
	cmd.endpoint = strings.TrimSpace(cmd.ann["pp:endpoint"]) != ""
	cmd.hidden = cmd.cobraHidden || annotationTrue(cmd.ann, "mcp:hidden")
}

func (a *mcpAnalyzer) isCobraCommand(expr ast.Expr, scope *mcpScope) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Command" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	obj := scope.lookup(id.Name)
	if obj == nil {
		obj = a.identObj[id.Pos()]
	}
	return obj != nil && obj.importPath == "github.com/spf13/cobra"
}

func (a *mcpAnalyzer) noteAnnotationAssign(lhsExpr, rhs ast.Expr, scope *mcpScope) {
	cmdObj := a.annotationCmd(lhsExpr, scope)
	cmd := a.cmdOf(cmdObj)
	if cmd == nil {
		return
	}
	switch e := lhsExpr.(type) {
	case *ast.SelectorExpr:
		if e.Sel.Name == "Hidden" {
			if id, ok := rhs.(*ast.Ident); ok {
				switch id.Name {
				case "true":
					cmd.cobraHidden = true
				case "false":
					cmd.cobraHidden = false
				}
			}
			a.classifyCmd(cmd)
		}
		if e.Sel.Name == "Annotations" {
			next := a.stringMap(rhs, scope)
			cmd.ann = map[string]string{}
			maps.Copy(cmd.ann, next)
			a.classifyCmd(cmd)
		}
	case *ast.IndexExpr:
		sel, ok := e.X.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Annotations" {
			return
		}
		key, ok := a.stringValue(e.Index, scope)
		val, ok2 := a.stringValue(rhs, scope)
		if ok && ok2 {
			cmd.ann[key] = val
			a.classifyCmd(cmd)
		}
	}
}

func (a *mcpAnalyzer) annotationCmd(lhsExpr ast.Expr, scope *mcpScope) *mcpObj {
	switch e := lhsExpr.(type) {
	case *ast.SelectorExpr:
		return a.exprObj(e.X, scope)
	case *ast.IndexExpr:
		sel, ok := e.X.(*ast.SelectorExpr)
		if !ok {
			return nil
		}
		return a.exprObj(sel.X, scope)
	default:
		return nil
	}
}

func (a *mcpAnalyzer) cmdOf(obj *mcpObj) *mcpCmd {
	if obj == nil {
		return nil
	}
	return a.cmds[obj.pos]
}

func (a *mcpAnalyzer) noteCall(call *ast.CallExpr, scope *mcpScope, ctx *mcpFnCtx) {
	a.noteInvoke(call, ctx)
	a.noteGetString(call, scope, ctx)
	a.noteFlagBind(call, scope, ctx)
	a.noteStringResult(call, scope, ctx)
	a.noteLink(call, scope)
	a.pending = append(a.pending, mcpFlow{
		call:    call,
		rhs:     call,
		scope:   scope,
		file:    fileName(ctx),
		hand:    ctx.file != nil && ctx.file.hand,
		imports: ctx.imports,
		dots:    ctx.dots,
	})
}

func (a *mcpAnalyzer) noteLink(call *ast.CallExpr, scope *mcpScope) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) == 0 {
		return
	}
	switch sel.Sel.Name {
	case "AddCommand":
		for _, arg := range call.Args {
			a.links = append(a.links, mcpLink{parent: sel.X, child: arg, scope: scope})
		}
	case "addNovelCommandIfAbsent":
		if len(call.Args) >= 2 {
			a.links = append(a.links, mcpLink{parent: call.Args[0], child: call.Args[1], scope: scope})
		}
	}
}

func (a *mcpAnalyzer) noteFlagBind(call *ast.CallExpr, scope *mcpScope, ctx *mcpFnCtx) {
	if ctx.file == nil || !ctx.file.hand {
		return
	}
	name, target, cmdObj, persistent, ok := a.matchStringVar(call, scope)
	if !ok || target == nil {
		return
	}
	literal := name != ""
	if name == "" {
		name = dynamicFlagName(call.Args[1])
	}
	a.appendFlag(name, cmdObj, persistent, ctx, call.Pos(), literal)
	a.seed[target.pos] = append(a.seed[target.pos], len(a.flags)-1)
}

func (a *mcpAnalyzer) noteFlagValue(lhs *mcpObj, rhs ast.Expr, scope *mcpScope, ctx *mcpFnCtx) {
	call, ok := rhs.(*ast.CallExpr)
	if !ok || ctx.file == nil || !ctx.file.hand {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	if sel.Sel.Name != "String" && sel.Sel.Name != "StringP" {
		return
	}
	cmdObj, persistent, ok := a.flagSet(sel.X, scope)
	if !ok || len(call.Args) == 0 {
		return
	}
	name := a.flagNameArg(call, sel.Sel.Name, scope)
	literal := name != ""
	if name == "" {
		name = dynamicFlagName(call.Args[0])
	}
	a.appendFlag(name, cmdObj, persistent, ctx, call.Pos(), literal)
	a.seed[lhs.pos] = append(a.seed[lhs.pos], len(a.flags)-1)
}

func (a *mcpAnalyzer) noteStringResult(call *ast.CallExpr, scope *mcpScope, ctx *mcpFnCtx) {
	if ctx.file == nil || !ctx.file.hand {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "String" && sel.Sel.Name != "StringP") || len(call.Args) == 0 {
		return
	}
	cmdObj, persistent, ok := a.flagSet(sel.X, scope)
	if !ok {
		return
	}
	name := a.flagNameArg(call, sel.Sel.Name, scope)
	literal := name != ""
	if name == "" {
		name = dynamicFlagName(call.Args[0])
	}
	a.appendFlag(name, cmdObj, persistent, ctx, call.Pos(), literal)
	a.seed[call.Pos()] = append(a.seed[call.Pos()], len(a.flags)-1)
}

func (a *mcpAnalyzer) appendFlag(name string, cmdObj *mcpObj, persistent bool, ctx *mcpFnCtx, pos token.Pos, literal bool) {
	fn, optional := flagContext(ctx)
	a.flags = append(a.flags, mcpFlag{
		name:       name,
		cmdObj:     cmdObj,
		persistent: persistent,
		optional:   optional,
		literal:    literal,
		fn:         fn,
	})
	if !pos.IsValid() {
		return
	}
	if a.flagAt == nil {
		a.flagAt = map[token.Pos][]int{}
	}
	a.flagAt[pos] = append(a.flagAt[pos], len(a.flags)-1)
}

func flagContext(ctx *mcpFnCtx) (fn string, optional bool) {
	if ctx == nil {
		return "<file>", false
	}
	switch {
	case ctx.fn != nil:
		fn = ctx.fn.name
	case ctx.lit != nil:
		fn = "<lit>"
	default:
		fn = "<file>"
	}
	return fn, ctx.cond > 0
}

func (a *mcpAnalyzer) noteInvoke(call *ast.CallExpr, ctx *mcpFnCtx) {
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name == "" || id.Name == "_" {
		return
	}
	fn, optional := flagContext(ctx)
	a.invokes = append(a.invokes, mcpInvoke{caller: fn, callee: id.Name, cond: optional})
}

func (a *mcpAnalyzer) markOptionalFlags() {
	called := map[string]bool{}
	for _, inv := range a.invokes {
		called[inv.callee] = true
	}
	status := map[string]int{"init": 1, "main": 1, "<file>": 1}
	for name := range a.funcs {
		if !called[name] {
			status[name] = 1
		}
	}
	changed := true
	for changed {
		changed = false
		for _, inv := range a.invokes {
			caller := status[inv.caller]
			if caller == 0 {
				continue
			}
			next := 2
			if caller == 1 && !inv.cond {
				next = 1
			}
			prev := status[inv.callee]
			if prev == 1 || prev == next {
				continue
			}
			status[inv.callee] = next
			changed = true
		}
	}
	for i := range a.flags {
		if a.flags[i].optional || status[a.flags[i].fn] == 1 {
			continue
		}
		a.flags[i].optional = true
	}
	a.fnReach = status
}

// A flag registered from every branch of an if/else is present whenever that
// if runs. Cobra then returns the nearer flag, so a farther flag of the same
// name is not the one MCP exposes.
func (a *mcpAnalyzer) sealGuaranteedFlags() {
	for name, fn := range a.funcs {
		if a.fnReach[name] != 1 || fn == nil || fn.body == nil || blockHasGoto(fn.body) {
			continue
		}
		a.sealMust(a.mustFunc(name).flags)
	}
}

func (a *mcpAnalyzer) sealMust(flags map[flagKey][]int) {
	for key, idxs := range flags {
		if key.cmd == nil || key.name == "" {
			continue
		}
		sealed := false
		for _, idx := range idxs {
			if idx < 0 || idx >= len(a.flags) {
				continue
			}
			a.flags[idx].optional = false
			sealed = true
		}
		if sealed {
			continue
		}
		a.flags = append(a.flags, mcpFlag{
			name:       key.name,
			cmdObj:     key.cmd,
			persistent: key.persistent,
			literal:    true,
			fn:         "",
		})
	}
}

type flagKey struct {
	cmd        *mcpObj
	name       string
	persistent bool
}

type mustResult struct {
	flags     map[flagKey][]int
	returns   bool
	mayReturn bool
	opaque    bool
}

func (a *mcpAnalyzer) mustFunc(name string) mustResult {
	if a.mustMemo == nil {
		a.mustMemo = map[string]mustResult{}
	}
	if res, ok := a.mustMemo[name]; ok {
		return res
	}
	if a.computing == nil {
		a.computing = map[string]bool{}
	}
	if a.computing[name] {
		return mustResult{}
	}
	fn := a.funcs[name]
	if fn == nil || fn.body == nil || blockHasGoto(fn.body) {
		a.mustMemo[name] = mustResult{}
		return mustResult{}
	}
	a.computing[name] = true
	res := a.mustBlock(fn.body)
	a.computing[name] = false
	a.mustMemo[name] = res
	return res
}

func (a *mcpAnalyzer) mustBlock(body *ast.BlockStmt) mustResult {
	if body == nil {
		return mustResult{}
	}
	var acc mustResult
	for _, stmt := range body.List {
		acc = seqMust(acc, a.mustStmt(stmt))
	}
	return acc
}

func seqMust(acc, next mustResult) mustResult {
	if acc.returns || acc.opaque || acc.mayReturn {
		return acc
	}
	acc.flags = unionFlags(acc.flags, next.flags)
	acc.returns = next.returns
	acc.mayReturn = next.mayReturn
	acc.opaque = next.opaque
	return acc
}

func (a *mcpAnalyzer) mustStmt(stmt ast.Stmt) mustResult {
	if stmt == nil {
		return mustResult{}
	}
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		return a.mustBlock(s)
	case *ast.IfStmt:
		acc := a.mustStmt(s.Init)
		if acc.returns || acc.opaque || acc.mayReturn {
			return acc
		}
		thenR := a.mustStmt(s.Body)
		if s.Else == nil {
			acc.mayReturn = thenR.returns || thenR.mayReturn
			return acc
		}
		elseR := a.mustStmt(s.Else)
		acc.flags = unionFlags(acc.flags, intersectFlags(thenR.flags, elseR.flags))
		acc.returns = (thenR.returns || thenR.opaque) && (elseR.returns || elseR.opaque)
		acc.mayReturn = !acc.returns && (thenR.returns || thenR.mayReturn || elseR.returns || elseR.mayReturn || thenR.opaque || elseR.opaque)
		return acc
	case *ast.ForStmt:
		acc := a.mustStmt(s.Init)
		if acc.returns || acc.opaque || acc.mayReturn {
			return acc
		}
		if stmtMayExit(s.Body) || stmtMayExit(s.Post) {
			acc.mayReturn = true
		}
		return acc
	case *ast.RangeStmt:
		if stmtMayExit(s.Body) {
			return mustResult{mayReturn: true}
		}
		return mustResult{}
	case *ast.SwitchStmt:
		return a.mustSwitch(s.Init, s.Body)
	case *ast.TypeSwitchStmt:
		return a.mustSwitch(s.Init, s.Body)
	case *ast.SelectStmt:
		if stmtMayExit(s.Body) {
			return mustResult{mayReturn: true}
		}
		return mustResult{}
	case *ast.ExprStmt:
		return a.mustExpr(s.X)
	case *ast.AssignStmt:
		var acc mustResult
		for _, rhs := range s.Rhs {
			acc = seqMust(acc, a.mustExpr(rhs))
		}
		return acc
	case *ast.DeclStmt:
		gen, ok := s.Decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			return mustResult{}
		}
		var acc mustResult
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, val := range vs.Values {
				acc = seqMust(acc, a.mustExpr(val))
			}
		}
		return acc
	case *ast.ReturnStmt:
		return mustResult{returns: true}
	case *ast.LabeledStmt:
		return a.mustStmt(s.Stmt)
	default:
		return mustResult{}
	}
}

func (a *mcpAnalyzer) mustSwitch(init ast.Stmt, body *ast.BlockStmt) mustResult {
	acc := a.mustStmt(init)
	if acc.returns || acc.opaque || acc.mayReturn {
		return acc
	}
	if stmtMayExit(body) {
		acc.mayReturn = true
	}
	return acc
}

func (a *mcpAnalyzer) mustExpr(e ast.Expr) mustResult {
	switch n := e.(type) {
	case *ast.ParenExpr:
		return a.mustExpr(n.X)
	case *ast.CallExpr:
		return a.mustCall(n)
	default:
		return mustResult{}
	}
}

func (a *mcpAnalyzer) mustCall(call *ast.CallExpr) mustResult {
	flags := map[flagKey][]int{}
	for _, idx := range a.flagAt[call.Pos()] {
		if idx < 0 || idx >= len(a.flags) {
			continue
		}
		flag := a.flags[idx]
		if !flag.literal || flag.cmdObj == nil || flag.name == "" {
			continue
		}
		key := flagKey{cmd: flag.cmdObj, name: flag.name, persistent: flag.persistent}
		flags[key] = append(flags[key], idx)
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name == "" || a.funcs[id.Name] == nil {
		return mustResult{flags: flags}
	}
	inner := a.mustFunc(id.Name)
	fn := a.funcs[id.Name]
	for key := range inner.flags {
		arg := a.matchParam(key.cmd, fn.params, call.Args)
		if arg == nil {
			continue
		}
		next := flagKey{cmd: arg, name: key.name, persistent: key.persistent}
		flags[next] = append(flags[next], -1)
	}
	return mustResult{
		flags:     flags,
		returns:   inner.returns,
		mayReturn: !inner.returns && (inner.mayReturn || inner.opaque),
		opaque:    false,
	}
}

func (a *mcpAnalyzer) matchParam(cmd *mcpObj, params []*mcpObj, args []ast.Expr) *mcpObj {
	for i, param := range params {
		if param != cmd || i >= len(args) {
			continue
		}
		return a.exprObj(args[i], nil)
	}
	return nil
}

func unionFlags(left, right map[flagKey][]int) map[flagKey][]int {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}
	out := map[flagKey][]int{}
	for key, idxs := range left {
		out[key] = append([]int(nil), idxs...)
	}
	for key, idxs := range right {
		out[key] = append(out[key], idxs...)
	}
	return out
}

func intersectFlags(left, right map[flagKey][]int) map[flagKey][]int {
	if len(left) == 0 || len(right) == 0 {
		return nil
	}
	out := map[flagKey][]int{}
	for key, idxs := range left {
		other, ok := right[key]
		if !ok {
			continue
		}
		out[key] = append(append([]int(nil), idxs...), other...)
	}
	return out
}

func blockHasGoto(body *ast.BlockStmt) bool {
	return stmtMayExitKind(body, true)
}

func stmtMayExit(stmt ast.Stmt) bool {
	return stmtMayExitKind(stmt, false)
}

func stmtMayExitKind(stmt ast.Stmt, gotoOnly bool) bool {
	if stmt == nil {
		return false
	}
	found := false
	ast.Inspect(stmt, func(n ast.Node) bool {
		if found {
			return false
		}
		switch node := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if !gotoOnly {
				found = true
			}
			return false
		case *ast.BranchStmt:
			if node.Tok == token.GOTO {
				found = true
			}
			return false
		default:
			return true
		}
	})
	return found
}

func (a *mcpAnalyzer) noteGetString(call *ast.CallExpr, scope *mcpScope, ctx *mcpFnCtx) {
	if ctx.file == nil || !ctx.file.hand {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "GetString" || len(call.Args) == 0 {
		return
	}
	cmdObj, persistent, ok := a.flagSet(sel.X, scope)
	if !ok || cmdObj == nil {
		return
	}
	a.lookups = append(a.lookups, mcpLookup{
		call:           call,
		cmdObj:         cmdObj,
		persistentOnly: persistent,
		name:           a.flagNameArg(call, sel.Sel.Name, scope),
	})
}

func (a *mcpAnalyzer) noteRunAssign(lhsExpr, rhs ast.Expr, scope *mcpScope) {
	sel, ok := lhsExpr.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "Run" && sel.Sel.Name != "RunE") {
		return
	}
	a.runBinds = append(a.runBinds, mcpRunBind{cmdObj: a.exprObj(sel.X, scope), rhs: rhs})
}

func (a *mcpAnalyzer) bindRuns() {
	for _, bind := range a.runBinds {
		cmd := bind.cmd
		if cmd == nil {
			cmd = a.cmdOf(bind.cmdObj)
		}
		a.bindRunExpr(cmd, bind.rhs)
	}
}

func (a *mcpAnalyzer) bindRunExpr(cmd *mcpCmd, rhs ast.Expr) {
	if cmd == nil || rhs == nil {
		return
	}
	switch n := rhs.(type) {
	case *ast.FuncLit:
		if lf := a.lits[n.Pos()]; lf != nil {
			a.bindRunParams(cmd, lf.params)
		}
	case *ast.Ident:
		if fn := a.funcs[n.Name]; fn != nil {
			a.bindRunParams(cmd, fn.params)
		}
	case *ast.ParenExpr:
		a.bindRunExpr(cmd, n.X)
	}
}

func (a *mcpAnalyzer) bindRunParams(cmd *mcpCmd, params []*mcpObj) {
	if len(params) == 0 || params[0] == nil {
		return
	}
	a.cmds[params[0].pos] = cmd
}

func (a *mcpAnalyzer) propagateCommands() {
	ambiguous := &mcpCmd{ambiguous: true}
	changed := true
	for changed {
		changed = false
		for _, flow := range a.pending {
			if flow.call == nil {
				continue
			}
			params, variadic := a.callParams(flow.call, flow.scope)
			if len(params) == 0 {
				continue
			}
			for i, arg := range flow.call.Args {
				var param *mcpObj
				switch {
				case i < len(params):
					param = params[i]
				case variadic:
					param = params[len(params)-1]
				}
				if param == nil {
					continue
				}
				src := a.cmdOf(a.exprObj(arg, flow.scope))
				if src == nil {
					continue
				}
				dst := a.cmds[param.pos]
				switch {
				case src.ambiguous:
					if dst == nil || !dst.ambiguous {
						a.cmds[param.pos] = ambiguous
						changed = true
					}
				case dst == nil:
					a.cmds[param.pos] = src
					changed = true
				case dst != src && !dst.ambiguous:
					a.cmds[param.pos] = ambiguous
					changed = true
				}
			}
		}
	}
}

func (a *mcpAnalyzer) callParams(call *ast.CallExpr, scope *mcpScope) ([]*mcpObj, bool) {
	if fn := a.resolveFunc(call, scope); fn != nil {
		return fn.params, fn.variadic
	}
	if lit := a.resolveLit(call, scope); lit != nil {
		return lit.params, lit.variadic
	}
	return nil, false
}

func (a *mcpAnalyzer) resolveLookups() {
	for _, look := range a.lookups {
		idxs := a.lookupFlags(look)
		if len(idxs) == 0 {
			continue
		}
		a.seed[look.call.Pos()] = append(a.seed[look.call.Pos()], idxs...)
	}
}

func (a *mcpAnalyzer) lookupFlags(look mcpLookup) []int {
	cmd := a.cmdOf(look.cmdObj)
	if cmd == nil {
		return nil
	}
	if !cmd.ambiguous {
		return a.ownedFlags(cmd, look.persistentOnly, look.name)
	}
	seen := map[int]bool{}
	var out []int
	for _, candidate := range a.commands() {
		for _, idx := range a.ownedFlags(candidate, look.persistentOnly, look.name) {
			if seen[idx] {
				continue
			}
			seen[idx] = true
			out = append(out, idx)
		}
	}
	return out
}

// ownedFlags is the flag Cobra returns from this command's flag set.
// Flags() prefers a local flag, then this command's persistent flag, then the
// nearest ancestor's persistent flag. A farther flag of the same name is not
// the one that is read or exposed. PersistentFlags() is only this command's
// own persistent set.
func (a *mcpAnalyzer) ownedFlags(cmd *mcpCmd, persistentOnly bool, name string) []int {
	seen := map[string]bool{}
	var out []int
	take := func(i int, flag mcpFlag) {
		if flag.name == "" || (name != "" && flag.name != name) || seen[flag.name] {
			return
		}
		out = append(out, i)
		// A registration that only runs on some paths does not hide the flag
		// Cobra still inherits when that path is skipped.
		if !flag.optional {
			seen[flag.name] = true
		}
	}
	if persistentOnly {
		for i, flag := range a.flags {
			if a.cmdOf(flag.cmdObj) == cmd && flag.persistent {
				take(i, flag)
			}
		}
		return out
	}
	for i, flag := range a.flags {
		if a.cmdOf(flag.cmdObj) == cmd && !flag.persistent {
			take(i, flag)
		}
	}
	for i, flag := range a.flags {
		if a.cmdOf(flag.cmdObj) == cmd && flag.persistent {
			take(i, flag)
		}
	}
	for cur := cmd.parent; cur != nil; cur = cur.parent {
		for i, flag := range a.flags {
			if a.cmdOf(flag.cmdObj) == cur && flag.persistent {
				take(i, flag)
			}
		}
	}
	return out
}

func (a *mcpAnalyzer) commands() []*mcpCmd {
	seen := map[*mcpCmd]bool{}
	var out []*mcpCmd
	for _, cmd := range a.cmds {
		if cmd == nil || cmd.ambiguous || seen[cmd] {
			continue
		}
		seen[cmd] = true
		out = append(out, cmd)
	}
	return out
}

func dynamicFlagName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok && id.Name != "_" {
		return id.Name
	}
	return "(dynamic)"
}

func (a *mcpAnalyzer) matchStringVar(call *ast.CallExpr, scope *mcpScope) (name string, target, cmdObj *mcpObj, persistent, ok bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", nil, nil, false, false
	}
	if sel.Sel.Name != "StringVar" && sel.Sel.Name != "StringVarP" {
		return "", nil, nil, false, false
	}
	cmdObj, persistent, ok = a.flagSet(sel.X, scope)
	if !ok || len(call.Args) < 2 {
		return "", nil, nil, false, false
	}
	target = a.addrObj(call.Args[0], scope)
	name = a.flagNameArg(call, sel.Sel.Name, scope)
	if target == nil {
		return "", nil, nil, false, false
	}
	return name, target, cmdObj, persistent, true
}

func (a *mcpAnalyzer) flagNameArg(call *ast.CallExpr, method string, scope *mcpScope) string {
	idx := 0
	if method == "StringVar" || method == "StringVarP" {
		idx = 1
	}
	if idx >= len(call.Args) {
		return ""
	}
	v, ok := a.stringValue(call.Args[idx], scope)
	if !ok {
		return ""
	}
	return strings.TrimLeft(strings.TrimSpace(v), "-")
}

func (a *mcpAnalyzer) flagSet(e ast.Expr, scope *mcpScope) (cmd *mcpObj, persistent, ok bool) {
	if id, isIdent := e.(*ast.Ident); isIdent {
		obj := a.exprObj(id, scope)
		if obj == nil {
			return nil, false, false
		}
		if info, known := a.flagSetAlias[obj.pos]; known {
			return info.cmd, info.persistent, true
		}
		return nil, false, false
	}
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return nil, false, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false, false
	}
	switch sel.Sel.Name {
	case "Flags":
		return a.exprObj(sel.X, scope), false, true
	case "PersistentFlags":
		return a.exprObj(sel.X, scope), true, true
	default:
		return nil, false, false
	}
}

func (a *mcpAnalyzer) rememberFlagSet(lhs *mcpObj, rhs ast.Expr, scope *mcpScope) {
	if a.flagSetAlias == nil {
		a.flagSetAlias = map[token.Pos]mcpFlagSet{}
	}
	call, ok := rhs.(*ast.CallExpr)
	if !ok {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	cmd := a.exprObj(sel.X, scope)
	switch sel.Sel.Name {
	case "Flags":
		a.flagSetAlias[lhs.pos] = mcpFlagSet{cmd: cmd, persistent: false}
	case "PersistentFlags":
		a.flagSetAlias[lhs.pos] = mcpFlagSet{cmd: cmd, persistent: true}
	}
}

func fileName(ctx *mcpFnCtx) string {
	if ctx == nil || ctx.file == nil {
		return ""
	}
	return ctx.file.name
}

func (a *mcpAnalyzer) noteLitAssign(lhs *mcpObj, rhs ast.Expr) {
	lit, ok := rhs.(*ast.FuncLit)
	if !ok || lhs == nil {
		return
	}
	if lf := a.lits[lit.Pos()]; lf != nil {
		a.lits[lhs.pos] = lf
	}
}

func (a *mcpAnalyzer) finishFlows() {
	if a.sinkAlias == nil {
		a.sinkAlias = map[token.Pos]bool{}
	}
	changed := true
	for changed {
		changed = false
		for _, flow := range a.pending {
			if flow.lhs == nil || flow.rhs == nil || flow.call != nil {
				continue
			}
			ctx := &mcpFnCtx{imports: flow.imports, dots: flow.dots}
			if a.isReadSink(flow.rhs, flow.scope, ctx) && !a.sinkAlias[flow.lhs.pos] {
				a.sinkAlias[flow.lhs.pos] = true
				changed = true
			}
		}
	}
	for _, flow := range a.pending {
		ctx := &mcpFnCtx{imports: flow.imports, dots: flow.dots}
		switch {
		case flow.call != nil:
			a.flowCall(flow.call, flow.scope, ctx)
			if flow.hand && len(flow.call.Args) > 0 && a.isReadSink(flow.call.Fun, flow.scope, ctx) {
				a.sinks = append(a.sinks, mcpSink{
					arg:   flow.call.Args[0],
					scope: flow.scope,
					file:  flow.file,
					line:  a.line(flow.call),
				})
			}
		case len(flow.multi) > 0:
			call, _ := flow.rhs.(*ast.CallExpr)
			for i, lhs := range flow.multi {
				if lhs == nil || call == nil {
					continue
				}
				a.edges = append(a.edges, mcpEdge{from: a.resultPos(call, flow.scope, ctx, i), to: lhs.pos})
			}
		case flow.lhs != nil && flow.rhs != nil:
			a.edges = append(a.edges, mcpEdge{from: a.exprPos(flow.rhs, flow.scope, ctx), to: flow.lhs.pos})
		}
	}
	seenLit := map[*mcpLit]bool{}
	for _, fn := range a.funcs {
		a.flowReturns(fn, nil)
	}
	for _, group := range a.methods {
		for _, fn := range group {
			a.flowReturns(fn, nil)
		}
	}
	for _, lit := range a.lits {
		if seenLit[lit] {
			continue
		}
		seenLit[lit] = true
		a.flowReturns(nil, lit)
	}
	for _, sink := range a.sinks {
		a.exprPos(sink.arg, sink.scope, &mcpFnCtx{})
	}
}

func (a *mcpAnalyzer) flowReturns(fn *mcpFunc, lit *mcpLit) {
	var rets []mcpReturn
	if fn != nil {
		rets = fn.returns
	} else if lit != nil {
		rets = lit.returns
	}
	for _, ret := range rets {
		ctx := &mcpFnCtx{}
		for i, expr := range ret.exprs {
			var dst *mcpObj
			if fn != nil {
				dst = a.funcResult(fn, i)
			} else {
				dst = a.litResult(lit, i)
			}
			a.edges = append(a.edges, mcpEdge{from: a.exprPos(expr, ret.scope, ctx), to: dst.pos})
		}
	}
}

func (a *mcpAnalyzer) funcResult(fn *mcpFunc, i int) *mcpObj {
	for len(fn.results) <= i {
		fn.results = append(fn.results, a.synth(fn.name))
	}
	return fn.results[i]
}

func (a *mcpAnalyzer) litResult(lit *mcpLit, i int) *mcpObj {
	for len(lit.results) <= i {
		lit.results = append(lit.results, a.synth("lit"))
	}
	return lit.results[i]
}

func (a *mcpAnalyzer) flowCall(call *ast.CallExpr, scope *mcpScope, ctx *mcpFnCtx) {
	dst := call.Pos()
	if idxs, ok := a.passthrough(call, scope, ctx); ok {
		for _, i := range idxs {
			if i >= 0 && i < len(call.Args) {
				a.edges = append(a.edges, mcpEdge{from: a.exprPos(call.Args[i], scope, ctx), to: dst})
			}
		}
		return
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && !a.isPackageSel(sel, scope) {
		// Same-named methods are not disambiguated without types. A tainted
		// argument has to reach every one of them, or a helper that opens
		// the file is invisible when another method shares its name.
		if group := a.methods[sel.Sel.Name]; len(group) > 0 {
			for _, fn := range group {
				a.flowArgs(call, fn.params, fn.variadic, scope, ctx)
				if len(fn.results) > 0 || len(fn.returns) > 0 {
					a.edges = append(a.edges, mcpEdge{from: a.funcResult(fn, 0).pos, to: dst})
				}
			}
			return
		}
	}
	if fn := a.resolveFunc(call, scope); fn != nil {
		a.flowArgs(call, fn.params, fn.variadic, scope, ctx)
		if len(fn.results) > 0 || len(fn.returns) > 0 {
			a.edges = append(a.edges, mcpEdge{from: a.funcResult(fn, 0).pos, to: dst})
		}
		return
	}
	if lit := a.resolveLit(call, scope); lit != nil {
		a.flowArgs(call, lit.params, lit.variadic, scope, ctx)
		if len(lit.results) > 0 || len(lit.returns) > 0 {
			a.edges = append(a.edges, mcpEdge{from: a.litResult(lit, 0).pos, to: dst})
		}
	}
}

func (a *mcpAnalyzer) flowArgs(call *ast.CallExpr, params []*mcpObj, variadic bool, scope *mcpScope, ctx *mcpFnCtx) {
	if len(params) == 0 {
		return
	}
	for i, arg := range call.Args {
		var param *mcpObj
		switch {
		case i < len(params):
			param = params[i]
		case variadic:
			param = params[len(params)-1]
		default:
			continue
		}
		a.edges = append(a.edges, mcpEdge{from: a.exprPos(arg, scope, ctx), to: param.pos})
	}
}

func (a *mcpAnalyzer) resolveFunc(call *ast.CallExpr, scope *mcpScope) *mcpFunc {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		obj := a.exprObj(fun, scope)
		pkgObj := a.pkg.lookup(fun.Name)
		if obj != nil && pkgObj != nil && obj.pos == pkgObj.pos {
			return a.funcs[fun.Name]
		}
	}
	return nil
}

func (a *mcpAnalyzer) resolveLit(call *ast.CallExpr, scope *mcpScope) *mcpLit {
	switch fun := call.Fun.(type) {
	case *ast.FuncLit:
		return a.lits[fun.Pos()]
	case *ast.Ident:
		obj := a.exprObj(fun, scope)
		if obj == nil {
			return nil
		}
		return a.lits[obj.pos]
	default:
		return nil
	}
}

func (a *mcpAnalyzer) resultPos(call *ast.CallExpr, scope *mcpScope, ctx *mcpFnCtx, i int) token.Pos {
	if fn := a.resolveFunc(call, scope); fn != nil {
		return a.funcResult(fn, i).pos
	}
	if lit := a.resolveLit(call, scope); lit != nil {
		return a.litResult(lit, i).pos
	}
	return a.exprPos(call, scope, ctx)
}

func (a *mcpAnalyzer) exprPos(e ast.Expr, scope *mcpScope, ctx *mcpFnCtx) token.Pos {
	if e == nil {
		return token.NoPos
	}
	if a.nodePos == nil {
		a.nodePos = map[ast.Node]token.Pos{}
	}
	if pos, ok := a.nodePos[e]; ok {
		return pos
	}
	var pos token.Pos
	switch n := e.(type) {
	case *ast.Ident:
		if obj := a.exprObj(n, scope); obj != nil && obj.importPath == "" {
			pos = obj.pos
		} else {
			pos = n.Pos()
		}
	case *ast.ParenExpr:
		pos = a.exprPos(n.X, scope, ctx)
	case *ast.StarExpr:
		pos = a.synth("star").pos
		a.nodePos[e] = pos
		a.edges = append(a.edges, mcpEdge{from: a.exprPos(n.X, scope, ctx), to: pos})
		return pos
	case *ast.UnaryExpr:
		pos = a.exprPos(n.X, scope, ctx)
	case *ast.SliceExpr:
		pos = a.synth("slice").pos
		a.nodePos[e] = pos
		a.edges = append(a.edges, mcpEdge{from: a.exprPos(n.X, scope, ctx), to: pos})
		return pos
	case *ast.BinaryExpr:
		pos = n.Pos()
		if n.Op == token.ADD {
			// Pos() is the left operand's position, so a derived value needs
			// its own id or it collides with that operand.
			pos = a.synth("add").pos
			a.nodePos[e] = pos
			a.edges = append(a.edges,
				mcpEdge{from: a.exprPos(n.X, scope, ctx), to: pos},
				mcpEdge{from: a.exprPos(n.Y, scope, ctx), to: pos},
			)
			return pos
		}
	case *ast.SelectorExpr:
		if obj := a.valueObj(n, scope); obj != nil {
			pos = obj.pos
		} else {
			pos = n.Pos()
		}
	case *ast.CallExpr:
		pos = n.Pos()
	default:
		pos = e.Pos()
	}
	a.nodePos[e] = pos
	return pos
}

func (a *mcpAnalyzer) exprObj(e ast.Expr, scope *mcpScope) *mcpObj {
	id, ok := e.(*ast.Ident)
	if !ok || id == nil || id.Name == "_" {
		return nil
	}
	if obj := a.identObj[id.Pos()]; obj != nil {
		return obj
	}
	if scope == nil {
		return nil
	}
	obj := scope.lookup(id.Name)
	if obj != nil {
		a.identObj[id.Pos()] = obj
	}
	return obj
}

func (a *mcpAnalyzer) valueObj(e ast.Expr, scope *mcpScope) *mcpObj {
	switch n := e.(type) {
	case *ast.Ident:
		return a.exprObj(n, scope)
	case *ast.ParenExpr:
		return a.valueObj(n.X, scope)
	case *ast.SelectorExpr:
		base := a.valueObj(n.X, scope)
		if base == nil || base.importPath != "" {
			return nil
		}
		return a.selectorObj(base, n.Sel.Name)
	default:
		return nil
	}
}

func (a *mcpAnalyzer) selectorObj(base *mcpObj, field string) *mcpObj {
	if a.selObjs == nil {
		a.selObjs = map[string]*mcpObj{}
	}
	key := strconv.Itoa(int(base.pos)) + "." + field
	if obj := a.selObjs[key]; obj != nil {
		return obj
	}
	obj := a.synth(field)
	a.selObjs[key] = obj
	return obj
}

func (a *mcpAnalyzer) addrObj(e ast.Expr, scope *mcpScope) *mcpObj {
	switch n := e.(type) {
	case *ast.UnaryExpr:
		if n.Op == token.AND {
			return a.valueObj(n.X, scope)
		}
	case *ast.ParenExpr:
		return a.addrObj(n.X, scope)
	}
	return nil
}

func (a *mcpAnalyzer) passthrough(call *ast.CallExpr, scope *mcpScope, ctx *mcpFnCtx) ([]int, bool) {
	if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "string" && len(call.Args) == 1 {
		return []int{0}, true
	}
	if _, ok := call.Fun.(*ast.ArrayType); ok && len(call.Args) == 1 {
		return []int{0}, true
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !a.isPackageSel(sel, scope) {
		return nil, false
	}
	pkg := a.packagePath(sel, scope, ctx)
	switch pkg {
	case "path/filepath":
		switch sel.Sel.Name {
		case "Clean", "FromSlash", "ToSlash", "Abs", "EvalSymlinks":
			return []int{0}, true
		case "Join":
			if i, ok := soleJoinArg(call.Args); ok {
				return []int{i}, true
			}
		}
	case "strings":
		switch sel.Sel.Name {
		case "TrimSpace", "Trim", "TrimPrefix", "TrimSuffix", "ToLower", "ToUpper":
			return []int{0}, true
		}
	case "fmt":
		switch sel.Sel.Name {
		case "Sprint":
			return allIndexes(len(call.Args)), true
		case "Sprintf":
			var idxs []int
			for i := range call.Args {
				if i == 0 && isBasicString(call.Args[i]) {
					continue
				}
				idxs = append(idxs, i)
			}
			return idxs, true
		}
	}
	return nil, false
}

func allIndexes(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func soleJoinArg(args []ast.Expr) (int, bool) {
	// Join(dir, id) is a path component, not a flag that names the file.
	// Requiring mcp:read-flags there would hide ordinary id and lang flags.
	// Join(terms) and Join(".", terms) still name the file.
	sig := -1
	for i, arg := range args {
		if trivialPathLit(arg) {
			continue
		}
		if sig >= 0 {
			return 0, false
		}
		sig = i
	}
	return sig, sig >= 0
}

func trivialPathLit(e ast.Expr) bool {
	v, ok := basicString(e)
	if !ok {
		return false
	}
	switch v {
	case "", ".", "/", "\\", "./", ".\\":
		return true
	default:
		return false
	}
}

func (a *mcpAnalyzer) isPackageSel(sel *ast.SelectorExpr, scope *mcpScope) bool {
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	obj := a.exprObj(id, scope)
	return obj != nil && obj.importPath != ""
}

func (a *mcpAnalyzer) packagePath(sel *ast.SelectorExpr, scope *mcpScope, ctx *mcpFnCtx) string {
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	if obj := a.exprObj(id, scope); obj != nil && obj.importPath != "" {
		return obj.importPath
	}
	if ctx != nil {
		return ctx.imports[id.Name]
	}
	return ""
}

func (a *mcpAnalyzer) isReadSink(fun ast.Expr, scope *mcpScope, ctx *mcpFnCtx) bool {
	switch n := fun.(type) {
	case *ast.SelectorExpr:
		if !a.isPackageSel(n, scope) {
			return false
		}
		return readSink(a.packagePath(n, scope, ctx), n.Sel.Name)
	case *ast.Ident:
		if obj := a.exprObj(n, scope); obj != nil && a.sinkAlias[obj.pos] {
			return true
		}
		if ctx == nil {
			return false
		}
		for _, path := range ctx.dots {
			if readSink(path, n.Name) {
				return true
			}
		}
	}
	return false
}

func readSink(pkg, name string) bool {
	switch pkg {
	case "os":
		switch name {
		case "Open", "OpenFile", "ReadFile", "ReadDir", "Stat", "Lstat", "Readlink", "OpenRoot", "DirFS":
			return true
		}
	case "io/ioutil":
		switch name {
		case "ReadFile", "ReadDir":
			return true
		}
	case "path/filepath":
		switch name {
		case "Walk", "WalkDir", "Glob":
			return true
		}
	}
	return false
}

func (a *mcpAnalyzer) linkCommands() {
	for _, fn := range a.funcs {
		for _, ret := range fn.returns {
			for _, expr := range ret.exprs {
				if cmd := a.resolveCmd(expr, ret.scope); cmd != nil {
					a.funcCmd[fn.name] = cmd
				}
			}
		}
	}
	for _, link := range a.links {
		parent := a.resolveCmd(link.parent, link.scope)
		child := a.resolveCmd(link.child, link.scope)
		if parent == nil || child == nil || parent == child || child.parent != nil {
			continue
		}
		child.parent = parent
		parent.children = append(parent.children, child)
	}
}

func (a *mcpAnalyzer) resolveCmd(e ast.Expr, scope *mcpScope) *mcpCmd {
	switch n := e.(type) {
	case *ast.Ident:
		obj := a.exprObj(n, scope)
		if obj == nil {
			return nil
		}
		return a.cmds[obj.pos]
	case *ast.CallExpr:
		id, ok := n.Fun.(*ast.Ident)
		if !ok {
			return nil
		}
		return a.funcCmd[id.Name]
	case *ast.ParenExpr:
		return a.resolveCmd(n.X, scope)
	case *ast.UnaryExpr:
		return a.resolveCmd(n.X, scope)
	default:
		return nil
	}
}

func (a *mcpAnalyzer) propagate() {
	reach := map[token.Pos]map[int]bool{}
	type item struct {
		pos  token.Pos
		flag int
	}
	var q []item
	push := func(pos token.Pos, flag int) {
		if pos == token.NoPos {
			return
		}
		set := reach[pos]
		if set == nil {
			set = map[int]bool{}
			reach[pos] = set
		}
		if set[flag] {
			return
		}
		set[flag] = true
		q = append(q, item{pos, flag})
	}
	for pos, flags := range a.seed {
		for _, flag := range flags {
			push(pos, flag)
		}
	}
	for len(q) > 0 {
		cur := q[0]
		q = q[1:]
		for _, edge := range a.edges {
			if edge.from == cur.pos {
				push(edge.to, cur.flag)
			}
		}
	}
	a.reach = reach
}

func (a *mcpAnalyzer) findings() []MCPReadFlagFinding {
	seen := map[string]MCPReadFlagFinding{}
	for _, sink := range a.sinks {
		ctx := &mcpFnCtx{}
		pos := a.exprPos(sink.arg, sink.scope, ctx)
		for idx := range a.reach[pos] {
			if idx < 0 || idx >= len(a.flags) {
				continue
			}
			flag := a.flags[idx]
			var cmd *mcpCmd
			if flag.cmdObj != nil {
				cmd = a.cmds[flag.cmdObj.pos]
			}
			for _, command := range a.leakPaths(flag, idx, cmd) {
				key := command + "\x00" + flag.name
				next := MCPReadFlagFinding{
					Command: command,
					Flag:    flag.name,
					File:    sink.file,
					Line:    sink.line,
					Reason:  fmt.Sprintf("opens a file named by --%s without mcp:read-flags", flag.name),
				}
				if prev, ok := seen[key]; ok && (prev.Line < next.Line || (prev.Line == next.Line && prev.File <= next.File)) {
					continue
				}
				seen[key] = next
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]MCPReadFlagFinding, 0, len(seen))
	for _, finding := range seen {
		out = append(out, finding)
	}
	slices.SortFunc(out, func(a, b MCPReadFlagFinding) int {
		if a.File != b.File {
			return strings.Compare(a.File, b.File)
		}
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return strings.Compare(a.Flag, b.Flag)
	})
	return out
}

func (a *mcpAnalyzer) leakPaths(flag mcpFlag, flagIdx int, cmd *mcpCmd) []string {
	if generator.MCPBlockedInputFlag(flag.name) || generator.MCPBlockedDestinationFlag(flag.name) {
		return nil
	}
	if cmd == nil {
		return []string{"(command)"}
	}
	targets := []*mcpCmd{cmd}
	if flag.persistent {
		targets = append(targets, cmd.descendants()...)
	}
	var paths []string
	exposed := false
	for _, target := range targets {
		if a.flagShadowed(flagIdx, cmd, target) || !commandExposed(target) {
			continue
		}
		exposed = true
		if flagBlockedOn(target, flag, cmd) {
			continue
		}
		path := target.path()
		if path == "" {
			path = "(command)"
		}
		paths = append(paths, path)
	}
	if len(paths) > 0 {
		return paths
	}
	if !exposed && flag.persistent && cmd.group && len(cmd.children) == 0 && !flagBlockedOn(cmd, flag, cmd) {
		path := cmd.path()
		if path == "" {
			path = "(command)"
		}
		return []string{path}
	}
	return nil
}

func (c *mcpCmd) descendants() []*mcpCmd {
	var out []*mcpCmd
	seen := map[*mcpCmd]bool{}
	var walk func(*mcpCmd)
	walk = func(n *mcpCmd) {
		for _, child := range n.children {
			if child == nil || seen[child] {
				continue
			}
			seen[child] = true
			out = append(out, child)
			walk(child)
		}
	}
	walk(c)
	return out
}

func commandExposed(cmd *mcpCmd) bool {
	if cmd == nil {
		return false
	}
	seen := map[*mcpCmd]bool{}
	for n := cmd; n != nil && !seen[n]; n = n.parent {
		seen[n] = true
		if n.hidden {
			return false
		}
	}
	if cmd.group || cmd.endpoint {
		return false
	}
	return !isTopFramework(cmd)
}

func isTopFramework(cmd *mcpCmd) bool {
	if cmd == nil || cmd.parent == nil || cmd.parent.parent != nil {
		return false
	}
	return mcpFrameworkCommands[cobraLeafName(cmd.use)]
}

func (a *mcpAnalyzer) flagShadowed(flagIdx int, declarer, target *mcpCmd) bool {
	if declarer == nil || target == nil || target == declarer || flagIdx < 0 || flagIdx >= len(a.flags) {
		return false
	}
	name := a.flags[flagIdx].name
	if name == "" {
		return false
	}
	for cur := target; cur != nil && cur != declarer; cur = cur.parent {
		for i, other := range a.flags {
			if i == flagIdx || other.optional || other.name != name || a.cmdOf(other.cmdObj) != cur {
				continue
			}
			if other.persistent || cur == target {
				return true
			}
		}
	}
	return false
}

func flagBlockedOn(cmd *mcpCmd, flag mcpFlag, declarer *mcpCmd) bool {
	if cmd == nil {
		return false
	}
	if annotationListsFlag(cmd.ann, "mcp:read-flags", flag.name) || annotationListsFlag(cmd.ann, "mcp:write-flags", flag.name) {
		return true
	}
	if flag.persistent && declarer != nil && cmd != declarer {
		if annotationListsFlag(declarer.ann, "mcp:read-flags", flag.name) || annotationListsFlag(declarer.ann, "mcp:write-flags", flag.name) {
			return true
		}
	}
	return false
}

func annotationListsFlag(ann map[string]string, key, name string) bool {
	return annotationFlagNames(ann[key])[name]
}

func annotationFlagNames(raw string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || unicodeSpace(r)
	}) {
		name := strings.TrimLeft(strings.TrimSpace(part), "-")
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func unicodeSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

func annotationTrue(ann map[string]string, key string) bool {
	switch strings.ToLower(strings.TrimSpace(ann[key])) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

func cobraLeafName(use string) string {
	use = strings.TrimSpace(use)
	if use == "" {
		return ""
	}
	leaf, _, _ := strings.Cut(use, " ")
	return leaf
}

func (a *mcpAnalyzer) noteString(obj *mcpObj, e ast.Expr, scope *mcpScope) {
	if obj == nil || a.strBad[obj.pos] {
		return
	}
	if v, ok := basicString(e); ok {
		a.setString(obj, v)
		return
	}
	if id, ok := e.(*ast.Ident); ok {
		if src := a.exprObj(id, scope); src != nil && !a.strBad[src.pos] {
			if v, ok := a.strVal[src.pos]; ok {
				a.setString(obj, v)
				return
			}
		}
	}
	a.strBad[obj.pos] = true
	delete(a.strVal, obj.pos)
}

func (a *mcpAnalyzer) setString(obj *mcpObj, v string) {
	if a.strBad[obj.pos] {
		return
	}
	if prev, ok := a.strVal[obj.pos]; ok && prev != v {
		a.strBad[obj.pos] = true
		delete(a.strVal, obj.pos)
		return
	}
	a.strVal[obj.pos] = v
}

func (a *mcpAnalyzer) stringValue(e ast.Expr, scope *mcpScope) (string, bool) {
	if v, ok := basicString(e); ok {
		return v, true
	}
	if bin, ok := e.(*ast.BinaryExpr); ok && bin.Op == token.ADD {
		left, ok1 := a.stringValue(bin.X, scope)
		right, ok2 := a.stringValue(bin.Y, scope)
		if ok1 && ok2 {
			return left + right, true
		}
	}
	id, ok := e.(*ast.Ident)
	if !ok {
		return "", false
	}
	obj := a.exprObj(id, scope)
	if obj == nil || a.strBad[obj.pos] {
		return "", false
	}
	v, ok := a.strVal[obj.pos]
	return v, ok
}

func (a *mcpAnalyzer) stringMap(e ast.Expr, scope *mcpScope) map[string]string {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		if id, ok := e.(*ast.Ident); ok {
			if obj := a.exprObj(id, scope); obj != nil {
				if cmd := a.cmds[obj.pos]; cmd != nil {
					return cmd.ann
				}
			}
		}
		return nil
	}
	out := map[string]string{}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := a.stringValue(kv.Key, scope)
		val, ok2 := a.stringValue(kv.Value, scope)
		if ok && ok2 {
			out[key] = val
		}
	}
	return out
}

func basicString(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return v, true
}

func isBasicString(e ast.Expr) bool {
	_, ok := basicString(e)
	return ok
}

func (a *mcpAnalyzer) line(n ast.Node) int {
	if n == nil {
		return 0
	}
	return a.fset.Position(n.Pos()).Line
}
