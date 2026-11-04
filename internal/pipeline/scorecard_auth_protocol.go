package pipeline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
)

// cookieCredentialAttached reports whether client.go puts the scheme credential
// on an outbound request. A Header.Set("Cookie") string, an unused const, or a
// helper that never runs is not attachment. A seeded jar counts only when the
// scheme sends that credential as cookies; a stamped header such as
// Authorization has to be on the request that is Do'd.
func cookieCredentialAttached(clientContent, headerName string) bool {
	file, err := parser.ParseFile(token.NewFileSet(), "client.go", clientContent, parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	name := strings.TrimSpace(headerName)
	if name == "" {
		name = "Cookie"
	}
	if strings.EqualFold(name, "Cookie") && cookieJarCredentialWired(file) {
		return true
	}
	return requestCarriesAuthMaterial(file, name)
}

func cookieAuthHeaderName(scheme openAPISecurityScheme) string {
	name := strings.TrimSpace(scheme.HeaderName)
	if name == "" {
		return "Cookie"
	}
	return name
}

func cookieJarCredentialWired(file *ast.File) bool {
	fn := findFunc(file, "New")
	if fn == nil || fn.Body == nil {
		return false
	}
	jarParam, ok := funcParamName(findFunc(file, "newHTTPClient"), 1)
	if !ok || !funcInstallsJar(file, findFunc(file, "newHTTPClient"), jarParam, map[string]bool{}) {
		return false
	}
	wire := &jarWire{
		seeded: map[string]bool{},
		backed: map[string]bool{},
		file:   file,
	}
	walkBlock(fn.Body, wire.stmt)
	return wire.wired
}

type jarWire struct {
	file   *ast.File
	seeded map[string]bool
	backed map[string]bool
	wired  bool
}

func (w *jarWire) stmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		if call, ok := s.X.(*ast.CallExpr); ok {
			w.observeSeed(call)
		}
	case *ast.AssignStmt:
		w.assign(s.Lhs, s.Rhs)
	case *ast.ReturnStmt:
		for _, result := range s.Results {
			if id := httpClientFieldIdent(result); id != "" && w.backed[id] {
				w.wired = true
			}
		}
	}
}

func (w *jarWire) observeSeed(call *ast.CallExpr) {
	name := callName(call.Fun)
	if name != "SeedCookieJar" && name != "SeedCookieJarForDomain" {
		return
	}
	if len(call.Args) == 0 || !callPassesCookieCredential(call) || !seedFuncAttachesCookies(w.file, name) {
		return
	}
	if id := exprIdent(call.Args[0]); id != "" {
		w.seeded[id] = true
	}
}

func (w *jarWire) assign(lhs, rhs []ast.Expr) {
	if len(rhs) != 1 || len(lhs) == 0 {
		return
	}
	if call, ok := rhs[0].(*ast.CallExpr); ok && callName(call.Fun) == "newHTTPClient" {
		id := exprIdent(lhs[0])
		if id == "" {
			return
		}
		passed := len(call.Args) >= 2 && w.seeded[exprIdent(call.Args[1])]
		w.backed[id] = passed
		w.seeded[id] = false
		return
	}
	if id := httpClientFieldIdent(rhs[0]); id != "" && w.backed[id] {
		w.wired = true
	}
	if sel, ok := lhs[0].(*ast.SelectorExpr); ok && sel.Sel.Name == "HTTPClient" {
		if w.backed[exprIdent(rhs[0])] {
			w.wired = true
		}
		return
	}
	dst := exprIdent(lhs[0])
	if dst == "" {
		return
	}
	if src := exprIdent(rhs[0]); src != "" {
		w.seeded[dst] = w.seeded[src]
		w.backed[dst] = w.backed[src]
		return
	}
	w.seeded[dst] = false
	w.backed[dst] = false
}

func requestCarriesAuthMaterial(file *ast.File, headerName string) bool {
	want := strings.ToLower(strings.TrimSpace(headerName))
	if want == "" || file == nil {
		return false
	}
	hidePanic := hidesBuiltinPanic(file)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		wire := &reqWire{want: want, paths: []reqPath{{}}}
		if hidePanic {
			wire.seedOuterPanic()
		}
		wire.execFunc(fn)
		if wire.matched {
			return true
		}
	}
	return false
}

// reqState is one *http.Request on one path. Aliases share the pointer, so a
// later Set or Del is visible through every name that still refers to it.
// Clone gets its own header map; WithContext and guardOutbound keep this one.
// id is the object identity across a fork. Paths stay separate at a join so a
// name that points at different requests on different branches is not one
// binding. A later clear applies only on the path where that name still
// refers to the request.
type reqState struct {
	id      int
	headers map[string]bool
	cookies map[string]bool
}

func (s *reqState) carries(want string) bool {
	if s == nil || want == "" {
		return false
	}
	return s.headers[want] || s.cookies[want]
}

func (s *reqState) applyHeader(op, name string, value ast.Expr) {
	key := strings.ToLower(name)
	if s == nil || key == "" {
		return
	}
	switch op {
	case "Del":
		delete(s.headers, key)
		if key == "cookie" {
			s.cookies = map[string]bool{}
		}
	case "Set":
		if key == "cookie" {
			s.cookies = map[string]bool{}
		}
		if authValueExpr(value) {
			s.headers[key] = true
		} else {
			delete(s.headers, key)
		}
	case "Add":
		if authValueExpr(value) {
			s.headers[key] = true
		}
	}
}

func (s *reqState) addCookie(name string) {
	if s == nil {
		return
	}
	key := strings.ToLower(name)
	if key == "" {
		return
	}
	s.cookies[key] = true
}

type reqEnv = []map[string][]*reqState

// maxAuthPaths caps how many branch states we keep. Past that, live paths
// collapse and a clear through an ambiguous name is dropped rather than
// applied to every candidate.
const maxAuthPaths = 64

// reqPath is one path that can still reach the next statement.
type reqPath struct {
	scopes reqEnv
}

// flowFrame collects break and continue exits for one switch, select, or loop.
// depth is the scope stack at that statement, so an exit from a nested block
// drops the inner scopes first. labels name that statement; continue only
// stops on a loop.
type flowFrame struct {
	labels    []string
	loop      bool
	depth     int
	breaks    []reqPath
	continues []reqPath
	outer     *flowFrame
}

type reqWire struct {
	want    string
	paths   []reqPath
	matched bool
	nextID  int
	frame   *flowFrame
}

func (w *reqWire) scopes() reqEnv {
	if len(w.paths) == 0 {
		return nil
	}
	return w.paths[0].scopes
}

func (w *reqWire) push() {
	for i := range w.paths {
		w.paths[i].scopes = append(w.paths[i].scopes, map[string][]*reqState{})
	}
}

func (w *reqWire) pop() {
	for i := range w.paths {
		popScope(&w.paths[i])
	}
}

func popScope(p *reqPath) {
	if len(p.scopes) == 0 {
		return
	}
	p.scopes = p.scopes[:len(p.scopes)-1]
}

func (w *reqWire) lookup(name string) ([]*reqState, bool) {
	if name == "" {
		return nil, false
	}
	scopes := w.scopes()
	for i := len(scopes) - 1; i >= 0; i-- {
		states, ok := scopes[i][name]
		if !ok {
			continue
		}
		if len(states) == 0 {
			return nil, false
		}
		return states, true
	}
	return nil, false
}

func (w *reqWire) known(name string) bool {
	if name == "" {
		return false
	}
	scopes := w.scopes()
	for i := len(scopes) - 1; i >= 0; i-- {
		if _, ok := scopes[i][name]; ok {
			return true
		}
	}
	return false
}

// bind records name in the current block when define is set, which is how Go
// shadows an outer short-variable declaration. Plain assignment updates the
// block that already holds the name.
func (w *reqWire) bind(name string, states []*reqState, define bool) {
	scopes := w.scopes()
	if name == "" || len(scopes) == 0 {
		return
	}
	if states != nil {
		states = append([]*reqState(nil), states...)
	}
	if define {
		scopes[len(scopes)-1][name] = states
		return
	}
	for i := len(scopes) - 1; i >= 0; i-- {
		if _, ok := scopes[i][name]; ok {
			scopes[i][name] = states
			return
		}
	}
	scopes[len(scopes)-1][name] = states
}

func (w *reqWire) freshReq() *reqState {
	w.nextID++
	return &reqState{
		id:      w.nextID,
		headers: map[string]bool{},
		cookies: map[string]bool{},
	}
}

func (w *reqWire) onEach(fn func()) {
	incoming := append([]reqPath(nil), w.paths...)
	out := make([]reqPath, 0, len(incoming))
	for _, p := range incoming {
		w.paths = []reqPath{cloneReqPath(p)}
		fn()
		out = append(out, w.paths...)
	}
	w.paths = out
	w.shrink()
}

func (w *reqWire) shrink() {
	if len(w.paths) <= maxAuthPaths {
		return
	}
	acc := w.paths[0]
	for _, p := range w.paths[1:] {
		acc.scopes = mergeEnv(acc.scopes, p.scopes)
	}
	w.paths = []reqPath{acc}
}

func (w *reqWire) pushFrame(labels []string, loop bool) {
	depth := 0
	if len(w.paths) > 0 {
		depth = len(w.paths[0].scopes)
	}
	w.frame = &flowFrame{labels: labels, loop: loop, depth: depth, outer: w.frame}
}

func (w *reqWire) popFrame() *flowFrame {
	f := w.frame
	if f == nil {
		return &flowFrame{}
	}
	w.frame = f.outer
	return f
}

func (w *reqWire) frameFor(tok token.Token, label string) *flowFrame {
	for f := w.frame; f != nil; f = f.outer {
		if label != "" {
			if frameHasLabel(f, label) {
				return f
			}
			continue
		}
		if tok == token.CONTINUE && !f.loop {
			continue
		}
		return f
	}
	return nil
}

func frameHasLabel(f *flowFrame, label string) bool {
	return slices.Contains(f.labels, label)
}

func (w *reqWire) transfer(tok token.Token, label string) {
	if len(w.paths) == 0 {
		return
	}
	frame := w.frameFor(tok, label)
	if frame == nil || (tok == token.CONTINUE && !frame.loop) {
		w.paths = nil
		return
	}
	cp := trimScopes(cloneReqPath(w.paths[0]), frame.depth)
	if tok == token.CONTINUE {
		frame.continues = append(frame.continues, cp)
	} else {
		frame.breaks = append(frame.breaks, cp)
	}
	w.paths = nil
}

func (w *reqWire) execBlock(block *ast.BlockStmt) {
	if block == nil {
		return
	}
	w.push()
	w.execList(block.List)
	w.pop()
}

func (w *reqWire) execFunc(fn *ast.FuncDecl) {
	if fn == nil || fn.Body == nil {
		return
	}
	w.push()
	w.bindSignature(fn)
	w.execList(fn.Body.List)
	w.pop()
}

func (w *reqWire) bindSignature(fn *ast.FuncDecl) {
	if fn == nil {
		return
	}
	if fn.Recv != nil {
		w.bindFields(fn.Recv)
	}
	if fn.Type == nil {
		return
	}
	w.bindFields(fn.Type.TypeParams)
	w.bindFields(fn.Type.Params)
	w.bindFields(fn.Type.Results)
}

func (w *reqWire) bindFields(list *ast.FieldList) {
	if list == nil {
		return
	}
	for _, field := range list.List {
		for _, name := range field.Names {
			w.shadow(name)
		}
	}
}

func (w *reqWire) bindDecl(decl ast.Decl) {
	gen, ok := decl.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, spec := range gen.Specs {
		switch s := spec.(type) {
		case *ast.ValueSpec:
			for _, name := range s.Names {
				w.shadow(name)
			}
		case *ast.TypeSpec:
			if s.Name != nil {
				w.shadow(s.Name)
			}
		}
	}
}

func (w *reqWire) execList(list []ast.Stmt) {
	for _, stmt := range list {
		if len(w.paths) == 0 {
			return
		}
		w.execStmt(stmt)
	}
}

// execStmt advances every path that can still reach the next statement.
//
// return and a call of the builtin panic end a path. goto ends it too, and
// the target is not rejoined, so a credential reached only through goto is
// not credited. break and continue transfer to the labeled statement or,
// unlabeled, to the innermost switch, select, or loop. continue ignores
// switches and selects until it reaches a loop.
//
// A switch or type-switch case starts from the state at the switch and,
// separately, from each previous fallthrough. Each case is its own block:
// names it declares are dropped before a fallthrough path is carried into
// the next case, and before the case's other exits leave it. With no
// default the switch state is also an exit. A select runs one comm: it has
// no fallthrough and no implicit skip.
//
// A loop's exits are its breaks plus, when a condition or a range can end
// it, the state before the first iteration and the state after one pass
// through the body or a continue, including the post statement. Conditions
// are not evaluated, except that a bare call of the builtin panic, in a
// condition, switch tag, or range expression, ends the path. A selector
// never counts. A panic already bound in scope does not count, nor does a
// dot import, which leaves the name unresolved. An unconditional loop does
// not fall out of its body. A later iteration is not modeled, so a header
// removed only on a second pass can still be credited. defer, go, and
// function literals are not run.
func (w *reqWire) execStmt(stmt ast.Stmt) {
	if stmt == nil || len(w.paths) == 0 {
		return
	}
	switch s := stmt.(type) {
	case *ast.LabeledStmt:
		w.execLabeled(s)
	case *ast.BlockStmt:
		w.execBlock(s)
	case *ast.ExprStmt:
		w.onEach(func() {
			w.observeExpr(s.X)
			if w.callPanics(s.X) {
				w.paths = nil
			}
		})
	case *ast.AssignStmt:
		w.onEach(func() { w.assign(s.Tok, s.Lhs, s.Rhs) })
	case *ast.ReturnStmt:
		w.onEach(func() {
			for _, result := range s.Results {
				w.observeExpr(result)
			}
			w.paths = nil
		})
	case *ast.BranchStmt:
		w.onEach(func() { w.branch(s) })
	case *ast.IfStmt:
		w.execIf(s)
	case *ast.ForStmt:
		w.execFor(s, nil)
	case *ast.RangeStmt:
		w.execRange(s, nil)
	case *ast.SwitchStmt:
		w.execSwitch(nil, s.Init, nil, s.Tag, s.Body, true)
	case *ast.TypeSwitchStmt:
		w.execSwitch(nil, s.Init, s.Assign, nil, s.Body, true)
	case *ast.SelectStmt:
		w.execSwitch(nil, nil, nil, nil, s.Body, false)
	case *ast.DeclStmt:
		w.onEach(func() { w.bindDecl(s.Decl) })
	}
}

func (w *reqWire) execLabeled(s *ast.LabeledStmt) {
	labels, stmt := labelsOf(s)
	switch inner := stmt.(type) {
	case *ast.ForStmt:
		w.execFor(inner, labels)
	case *ast.RangeStmt:
		w.execRange(inner, labels)
	case *ast.SwitchStmt:
		w.execSwitch(labels, inner.Init, nil, inner.Tag, inner.Body, true)
	case *ast.TypeSwitchStmt:
		w.execSwitch(labels, inner.Init, inner.Assign, nil, inner.Body, true)
	case *ast.SelectStmt:
		w.execSwitch(labels, nil, nil, nil, inner.Body, false)
	default:
		w.execStmt(stmt)
	}
}

func labelsOf(stmt ast.Stmt) ([]string, ast.Stmt) {
	var labels []string
	for {
		lab, ok := stmt.(*ast.LabeledStmt)
		if !ok {
			return labels, stmt
		}
		if lab.Label != nil && lab.Label.Name != "" {
			labels = append(labels, lab.Label.Name)
		}
		stmt = lab.Stmt
	}
}

func (w *reqWire) branch(s *ast.BranchStmt) {
	label := ""
	if s.Label != nil {
		label = s.Label.Name
	}
	switch s.Tok {
	case token.BREAK, token.CONTINUE:
		w.transfer(s.Tok, label)
	default:
		// goto, or a fallthrough that is not the last statement of a case.
		// The target is not rejoined, so this path stops.
		w.paths = nil
	}
}

func (w *reqWire) execIf(s *ast.IfStmt) {
	incoming := append([]reqPath(nil), w.paths...)
	var next []reqPath
	for _, p := range incoming {
		w.paths = []reqPath{cloneReqPath(p)}
		w.push()
		w.execStmt(s.Init)
		if w.callPanics(s.Cond) || len(w.paths) == 0 {
			continue
		}
		bases := append([]reqPath(nil), w.paths...)
		var joined []reqPath
		for _, base := range bases {
			w.paths = []reqPath{cloneReqPath(base)}
			w.execStmt(s.Body)
			thenPaths := append([]reqPath(nil), w.paths...)
			w.paths = []reqPath{cloneReqPath(base)}
			w.execStmt(s.Else)
			joined = append(joined, thenPaths...)
			joined = append(joined, w.paths...)
		}
		for i := range joined {
			popScope(&joined[i])
		}
		next = append(next, joined...)
	}
	w.paths = next
	w.shrink()
}

func (w *reqWire) caseExits(body *ast.BlockStmt, entry reqPath, conditional bool) []reqPath {
	if body == nil {
		if conditional {
			return []reqPath{cloneReqPath(entry)}
		}
		return nil
	}
	entry = cloneReqPath(entry)
	var exits, falling []reqPath
	sawDefault := false
	for _, stmt := range body.List {
		entries := []reqPath{cloneReqPath(entry)}
		if conditional {
			entries = append(entries, clonePaths(falling)...)
		}
		if clauseIsDefault(stmt) {
			sawDefault = true
		}
		var flow, fell []reqPath
		for _, start := range entries {
			w.paths = []reqPath{cloneReqPath(start)}
			if w.execClause(stmt) {
				fell = append(fell, w.paths...)
				continue
			}
			flow = append(flow, w.paths...)
		}
		falling = fell
		exits = append(exits, flow...)
	}
	if conditional && !sawDefault {
		exits = append(exits, cloneReqPath(entry))
	}
	return exits
}

func (w *reqWire) execClause(stmt ast.Stmt) bool {
	switch clause := stmt.(type) {
	case *ast.CaseClause:
		return w.execCaseBody(clause.Body)
	case *ast.CommClause:
		w.execComm(clause)
		return false
	default:
		w.execStmt(stmt)
		return false
	}
}

// execCaseBody runs one switch case in its own block. Fallthrough leaves
// that block before the path is carried forward, so the next case resolves
// names where Go does: the blocks outside this case.
func (w *reqWire) execCaseBody(body []ast.Stmt) bool {
	depth := w.scopeDepth()
	w.push()
	for _, inner := range body {
		if isFallthrough(inner) {
			live := len(w.paths) > 0
			w.restoreDepth(depth)
			return live
		}
		w.execStmt(inner)
		if len(w.paths) == 0 {
			return false
		}
	}
	w.restoreDepth(depth)
	return false
}

func (w *reqWire) scopeDepth() int {
	if len(w.paths) == 0 {
		return 0
	}
	return len(w.paths[0].scopes)
}

func (w *reqWire) restoreDepth(depth int) {
	for i := range w.paths {
		if len(w.paths[i].scopes) > depth {
			w.paths[i].scopes = w.paths[i].scopes[:depth]
		}
	}
}

func (w *reqWire) execComm(clause *ast.CommClause) {
	if clause == nil {
		return
	}
	w.execStmt(clause.Comm)
	w.execList(clause.Body)
}

func clauseIsDefault(stmt ast.Stmt) bool {
	switch clause := stmt.(type) {
	case *ast.CaseClause:
		return clause.List == nil
	case *ast.CommClause:
		return clause.Comm == nil
	default:
		return false
	}
}

func (w *reqWire) execSwitch(labels []string, init, assign ast.Stmt, tag ast.Expr, body *ast.BlockStmt, conditional bool) {
	incoming := append([]reqPath(nil), w.paths...)
	var next []reqPath
	for _, p := range incoming {
		w.paths = []reqPath{cloneReqPath(p)}
		w.push()
		w.execStmt(init)
		w.execStmt(assign)
		if w.callPanics(tag) || len(w.paths) == 0 {
			continue
		}
		bases := append([]reqPath(nil), w.paths...)
		var exits []reqPath
		for _, base := range bases {
			w.paths = []reqPath{cloneReqPath(base)}
			w.pushFrame(labels, false)
			exits = append(exits, w.caseExits(body, base, conditional)...)
			exits = append(exits, w.popFrame().breaks...)
		}
		for i := range exits {
			popScope(&exits[i])
		}
		next = append(next, exits...)
	}
	w.paths = next
	w.shrink()
}

func (w *reqWire) execFor(s *ast.ForStmt, labels []string) {
	w.execLoop(labels, s.Cond != nil, s.Cond,
		func() { w.execStmt(s.Init) },
		func() { w.execBlock(s.Body) },
		func() { w.execStmt(s.Post) },
	)
}

func (w *reqWire) execRange(s *ast.RangeStmt, labels []string) {
	w.execLoop(labels, true, s.X,
		func() {
			if s.Tok == token.DEFINE {
				w.shadow(s.Key)
				w.shadow(s.Value)
			}
		},
		func() { w.execBlock(s.Body) },
		func() {},
	)
}

func (w *reqWire) execLoop(labels []string, canLeave bool, abort ast.Expr, init, body, post func()) {
	incoming := append([]reqPath(nil), w.paths...)
	var next []reqPath
	for _, p := range incoming {
		w.paths = []reqPath{cloneReqPath(p)}
		w.push()
		init()
		if w.callPanics(abort) {
			continue
		}
		afterInit := append([]reqPath(nil), w.paths...)
		var succ []reqPath
		for _, base := range afterInit {
			if canLeave {
				succ = append(succ, cloneReqPath(base))
			}
			w.paths = []reqPath{cloneReqPath(base)}
			w.pushFrame(labels, true)
			body()
			completed := append([]reqPath(nil), w.paths...)
			frame := w.popFrame()
			succ = append(succ, frame.breaks...)
			if canLeave {
				completed = append(completed, frame.continues...)
				for _, st := range completed {
					w.paths = []reqPath{cloneReqPath(st)}
					post()
					succ = append(succ, w.paths...)
				}
			}
		}
		for i := range succ {
			popScope(&succ[i])
		}
		next = append(next, succ...)
	}
	w.paths = next
	w.shrink()
}

func (w *reqWire) callPanics(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if found || n == nil {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if ok && w.builtinPanic(call.Fun) {
			found = true
			return false
		}
		return true
	})
	return found
}

// builtinPanic reports whether fun is a call of the predeclared panic.
// Selectors are methods. A panic bound in the walker's scopes, including
// the outer binding seeded for a package declaration or a dot import, is
// not the builtin. When the name is not a bare identifier, it is not either.
func (w *reqWire) builtinPanic(fun ast.Expr) bool {
	if fun == nil {
		return false
	}
	fun = unwrapExpr(fun)
	if fun == nil {
		return false
	}
	id, ok := fun.(*ast.Ident)
	if !ok || !identIsPanic(id) {
		return false
	}
	return !w.known("panic")
}

// seedOuterPanic hides the builtin for a package-level panic, or for a dot
// import whose names this file does not list. The binding sits outside the
// function, so a bare panic() in that file does not end the path.
func (w *reqWire) seedOuterPanic() {
	if len(w.paths) == 0 {
		return
	}
	for i := range w.paths {
		w.paths[i].scopes = append(reqEnv{{"panic": nil}}, w.paths[i].scopes...)
	}
}

func hidesBuiltinPanic(file *ast.File) bool {
	if file == nil {
		return false
	}
	if slices.ContainsFunc(file.Imports, importHidesPanic) {
		return true
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && identIsPanic(d.Name) {
				return true
			}
		case *ast.GenDecl:
			if genHidesPanic(d) {
				return true
			}
		}
	}
	return false
}

func genHidesPanic(decl *ast.GenDecl) bool {
	if decl == nil {
		return false
	}
	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.ValueSpec:
			if slices.ContainsFunc(s.Names, identIsPanic) {
				return true
			}
		case *ast.TypeSpec:
			if identIsPanic(s.Name) {
				return true
			}
		}
	}
	return false
}

func importHidesPanic(imp *ast.ImportSpec) bool {
	if imp == nil {
		return false
	}
	if imp.Name != nil {
		switch imp.Name.Name {
		case "_":
			// A blank import adds no name, so the builtin stays visible.
			return false
		case ".":
			// A dot import's names are not in this file, so panic may not be the builtin.
			return true
		default:
			if imp.Name.Name != "" {
				return imp.Name.Name == "panic"
			}
		}
	}
	if imp.Path == nil {
		return true
	}
	specPath, err := strconv.Unquote(imp.Path.Value)
	if err != nil {
		return true
	}
	return importPathHidesPanic(specPath)
}

// importPathHidesPanic uses the import path's last element as the local
// name. A version suffix keeps the parent element, matching the usual
// module layout. A package clause that disagrees with that path is not
// visible to this walk.
func importPathHidesPanic(specPath string) bool {
	base := path.Base(specPath)
	if base == "panic" {
		return true
	}
	if len(base) < 2 || base[0] != 'v' || !digitsOnly(base[1:]) {
		return false
	}
	return path.Base(path.Dir(specPath)) == "panic"
}

func digitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func identIsPanic(id *ast.Ident) bool {
	return id != nil && id.Name == "panic"
}

func clonePaths(paths []reqPath) []reqPath {
	out := make([]reqPath, len(paths))
	for i, p := range paths {
		out[i] = cloneReqPath(p)
	}
	return out
}

func (w *reqWire) shadow(expr ast.Expr) {
	if id := exprIdent(expr); id != "" {
		w.bind(id, nil, true)
	}
}

func (w *reqWire) assign(tok token.Token, lhs, rhs []ast.Expr) {
	define := tok == token.DEFINE
	if len(rhs) != 1 || len(lhs) == 0 {
		if define {
			w.shadowDefs(lhs)
		}
		return
	}
	if call, ok := rhs[0].(*ast.CallExpr); ok && callName(call.Fun) == "Do" {
		w.noteDo(call)
		if define {
			w.shadowDefs(lhs)
		}
		return
	}
	id := exprIdent(lhs[0])
	if id == "" {
		if define {
			w.shadowDefs(lhs)
		}
		return
	}
	if call, ok := rhs[0].(*ast.CallExpr); ok {
		if selectorName(call.Fun, "NewRequest", "NewRequestWithContext") {
			w.bind(id, []*reqState{w.freshReq()}, define)
			if define {
				w.shadowDefs(lhs[1:])
			}
			return
		}
		if sel, ok := unwrapExpr(call.Fun).(*ast.SelectorExpr); ok {
			if recv, ok := w.lookup(exprIdent(sel.X)); ok {
				next := recv
				if sel.Sel.Name == "Clone" {
					next = w.detachAll(recv)
				}
				w.bind(id, next, define)
				if define {
					w.shadowDefs(lhs[1:])
				}
				return
			}
		}
		if src := w.requestArg(call); len(src) > 0 {
			w.bind(id, src, define)
			if define {
				w.shadowDefs(lhs[1:])
			}
			return
		}
	}
	if src, ok := w.lookup(exprIdent(rhs[0])); ok {
		w.bind(id, src, define)
		if define {
			w.shadowDefs(lhs[1:])
		}
		return
	}
	if define {
		w.shadowDefs(lhs)
		return
	}
	if _, ok := w.lookup(id); ok {
		w.bind(id, []*reqState{w.freshReq()}, false)
	}
}

func (w *reqWire) shadowDefs(lhs []ast.Expr) {
	for _, expr := range lhs {
		w.shadow(expr)
	}
}

func (w *reqWire) detachAll(states []*reqState) []*reqState {
	out := make([]*reqState, 0, len(states))
	seen := map[*reqState]*reqState{}
	for _, st := range states {
		if st == nil {
			continue
		}
		if cp, ok := seen[st]; ok {
			out = append(out, cp)
			continue
		}
		w.nextID++
		cp := &reqState{
			id:      w.nextID,
			headers: cloneBoolMap(st.headers),
			cookies: cloneBoolMap(st.cookies),
		}
		seen[st] = cp
		out = append(out, cp)
	}
	return out
}

func (w *reqWire) observeExpr(expr ast.Expr) {
	call, ok := unwrapExpr(expr).(*ast.CallExpr)
	if !ok {
		return
	}
	w.observeCall(call)
	for _, arg := range call.Args {
		w.observeExpr(arg)
	}
}

func (w *reqWire) observeCall(call *ast.CallExpr) {
	if req, op, name, value, ok := headerMutOnRequest(call); ok {
		if states, known := w.lookup(req); known {
			// A joined name can point at different requests. Clearing every
			// candidate would erase a credential on a path where this name
			// points somewhere else.
			clear := op == "Del" || (op == "Set" && !authValueExpr(value))
			w.mutate(states, clear, func(state *reqState) {
				state.applyHeader(op, name, value)
			})
		}
		return
	}
	if req, name, ok := addCookieOnRequest(call); ok {
		if states, known := w.lookup(req); known {
			w.mutate(states, false, func(state *reqState) {
				state.addCookie(name)
			})
		}
		return
	}
	if callName(call.Fun) == "Do" {
		w.noteDo(call)
	}
}

func (w *reqWire) mutate(states []*reqState, clear bool, fn func(*reqState)) {
	if clear && !oneRequest(states) {
		return
	}
	seen := map[int]bool{}
	for _, state := range states {
		if state == nil || seen[state.id] {
			continue
		}
		seen[state.id] = true
		fn(state)
	}
}

func oneRequest(states []*reqState) bool {
	var id int
	seen := false
	for _, state := range states {
		if state == nil {
			continue
		}
		if !seen {
			id = state.id
			seen = true
			continue
		}
		if state.id != id {
			return false
		}
	}
	return seen
}

func (w *reqWire) noteDo(call *ast.CallExpr) {
	if len(w.paths) == 0 || len(call.Args) == 0 {
		return
	}
	states, ok := w.lookup(exprIdent(call.Args[0]))
	if !ok {
		return
	}
	for _, state := range states {
		if state.carries(w.want) {
			w.matched = true
			return
		}
	}
}

func (w *reqWire) requestArg(call *ast.CallExpr) []*reqState {
	for _, arg := range call.Args {
		if states, ok := w.lookup(exprIdent(arg)); ok {
			return states
		}
	}
	return nil
}

func isFallthrough(stmt ast.Stmt) bool {
	br, ok := stmt.(*ast.BranchStmt)
	return ok && br.Tok == token.FALLTHROUGH
}

func cloneReqPath(p reqPath) reqPath {
	return reqPath{scopes: cloneEnv(p.scopes)}
}

func trimScopes(p reqPath, depth int) reqPath {
	if depth >= 0 && depth < len(p.scopes) {
		p.scopes = p.scopes[:depth]
	}
	return p
}

func cloneEnv(scopes reqEnv) reqEnv {
	seen := map[*reqState]*reqState{}
	out := make(reqEnv, len(scopes))
	for i, scope := range scopes {
		out[i] = make(map[string][]*reqState, len(scope))
		for name, states := range scope {
			out[i][name] = forkStates(states, seen)
		}
	}
	return out
}

func forkStates(states []*reqState, seen map[*reqState]*reqState) []*reqState {
	if states == nil {
		return nil
	}
	out := make([]*reqState, len(states))
	for i, st := range states {
		out[i] = forkState(st, seen)
	}
	return out
}

func forkState(st *reqState, seen map[*reqState]*reqState) *reqState {
	if st == nil {
		return nil
	}
	if cp, ok := seen[st]; ok {
		return cp
	}
	cp := &reqState{
		id:      st.id,
		headers: cloneBoolMap(st.headers),
		cookies: cloneBoolMap(st.cookies),
	}
	seen[st] = cp
	return cp
}

func mergeEnv(a, b reqEnv) reqEnv {
	n := max(len(a), len(b))
	merged := map[int]*reqState{}
	absorbEnv(merged, a)
	absorbEnv(merged, b)
	out := make(reqEnv, n)
	for i := range n {
		out[i] = map[string][]*reqState{}
		keys := map[string]bool{}
		var scopeA, scopeB map[string][]*reqState
		if i < len(a) {
			scopeA = a[i]
			for name := range scopeA {
				keys[name] = true
			}
		}
		if i < len(b) {
			scopeB = b[i]
			for name := range scopeB {
				keys[name] = true
			}
		}
		for name := range keys {
			var statesA, statesB []*reqState
			haveA, haveB := false, false
			if scopeA != nil {
				statesA, haveA = scopeA[name]
			}
			if scopeB != nil {
				statesB, haveB = scopeB[name]
			}
			out[i][name] = joinedBinding(statesA, statesB, haveA, haveB, merged)
		}
	}
	return out
}

func absorbEnv(merged map[int]*reqState, scopes reqEnv) {
	for _, scope := range scopes {
		for _, states := range scope {
			for _, st := range states {
				absorbState(merged, st)
			}
		}
	}
}

func absorbState(merged map[int]*reqState, st *reqState) {
	if st == nil {
		return
	}
	prev, ok := merged[st.id]
	if !ok {
		merged[st.id] = &reqState{
			id:      st.id,
			headers: cloneBoolMap(st.headers),
			cookies: cloneBoolMap(st.cookies),
		}
		return
	}
	orInto(prev.headers, st.headers)
	orInto(prev.cookies, st.cookies)
}

func joinedBinding(a, b []*reqState, haveA, haveB bool, merged map[int]*reqState) []*reqState {
	var out []*reqState
	seen := map[int]bool{}
	add := func(states []*reqState) {
		for _, st := range states {
			if st == nil || seen[st.id] {
				continue
			}
			seen[st.id] = true
			if next := merged[st.id]; next != nil {
				out = append(out, next)
			}
		}
	}
	if haveA {
		add(a)
	}
	if haveB {
		add(b)
	}
	return out
}

func orInto(dst, src map[string]bool) {
	for key, present := range src {
		if present {
			dst[key] = true
		}
	}
}

func headerMutOnRequest(call *ast.CallExpr) (string, string, string, ast.Expr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", "", "", nil, false
	}
	headerSel, ok := sel.X.(*ast.SelectorExpr)
	if !ok || headerSel.Sel.Name != "Header" {
		return "", "", "", nil, false
	}
	req := exprIdent(headerSel.X)
	if req == "" {
		return "", "", "", nil, false
	}
	switch sel.Sel.Name {
	case "Set", "Add":
		if len(call.Args) < 2 {
			return "", "", "", nil, false
		}
		name := stringLit(call.Args[0])
		if name == "" {
			return "", "", "", nil, false
		}
		return req, sel.Sel.Name, name, call.Args[1], true
	case "Del":
		if len(call.Args) < 1 {
			return "", "", "", nil, false
		}
		name := stringLit(call.Args[0])
		if name == "" {
			return "", "", "", nil, false
		}
		return req, "Del", name, nil, true
	default:
		return "", "", "", nil, false
	}
}

func addCookieOnRequest(call *ast.CallExpr) (string, string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "AddCookie" || len(call.Args) != 1 {
		return "", "", false
	}
	req := exprIdent(sel.X)
	name, ok := cookieComposite(call.Args[0])
	if req == "" || !ok {
		return "", "", false
	}
	return req, name, true
}

func cookieComposite(expr ast.Expr) (string, bool) {
	expr = unwrapExpr(expr)
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		expr = unary.X
	}
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return "", false
	}
	var name string
	valueOK := false
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
		case "Name":
			name = stringLit(kv.Value)
		case "Value":
			valueOK = authValueExpr(kv.Value)
		}
	}
	if name == "" || !valueOK {
		return "", false
	}
	return name, true
}

func authValueExpr(expr ast.Expr) bool {
	switch e := unwrapExpr(expr).(type) {
	case *ast.Ident:
		return e.Name == "authHeader"
	case *ast.CallExpr:
		switch callName(e.Fun) {
		case "AuthHeader", "authHeader", "CookieCredential":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func callPassesCookieCredential(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		inner, ok := unwrapExpr(arg).(*ast.CallExpr)
		if ok && callName(inner.Fun) == "CookieCredential" {
			return true
		}
	}
	return false
}

func seedFuncAttachesCookies(file *ast.File, name string) bool {
	fn := findFunc(file, name)
	if fn == nil {
		return true
	}
	return funcAttachesCookies(file, fn, map[string]bool{})
}

func funcAttachesCookies(file *ast.File, fn *ast.FuncDecl, seen map[string]bool) bool {
	if fn == nil || fn.Body == nil || fn.Name == nil || seen[fn.Name.Name] {
		return false
	}
	seen[fn.Name.Name] = true
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch callName(call.Fun) {
		case "SetCookies":
			found = true
			return false
		case "SeedCookieJar", "SeedCookieJarForDomain", "seedCookieJar":
			next := findFunc(file, callName(call.Fun))
			if next == nil || funcAttachesCookies(file, next, seen) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func funcInstallsJar(file *ast.File, fn *ast.FuncDecl, jarIdent string, seen map[string]bool) bool {
	if fn == nil || fn.Body == nil || fn.Name == nil || jarIdent == "" || seen[fn.Name.Name] {
		return false
	}
	seen[fn.Name.Name] = true
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, result := range ret.Results {
			if exprInstallsJar(file, result, jarIdent, seen) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func exprInstallsJar(file *ast.File, expr ast.Expr, jarIdent string, seen map[string]bool) bool {
	expr = unwrapExpr(expr)
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		expr = unary.X
	}
	switch node := expr.(type) {
	case *ast.CompositeLit:
		if !compositeNamedClient(node) {
			return false
		}
		for _, elt := range node.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if ok && key.Name == "Jar" && exprIdent(kv.Value) == jarIdent {
				return true
			}
		}
	case *ast.CallExpr:
		for i, arg := range node.Args {
			if exprIdent(arg) != jarIdent {
				continue
			}
			name := callName(node.Fun)
			if name == "chromeClient" && findFunc(file, name) == nil {
				return true
			}
			callee := findFunc(file, name)
			param, ok := funcParamName(callee, i)
			if ok && funcInstallsJar(file, callee, param, seen) {
				return true
			}
		}
	}
	return false
}

func compositeNamedClient(lit *ast.CompositeLit) bool {
	switch t := lit.Type.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name == "Client"
	case *ast.Ident:
		return t.Name == "Client"
	default:
		return false
	}
}

func httpClientFieldIdent(expr ast.Expr) string {
	expr = unwrapExpr(expr)
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		expr = unary.X
	}
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return ""
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "HTTPClient" {
			continue
		}
		return exprIdent(kv.Value)
	}
	return ""
}

func findFunc(file *ast.File, name string) *ast.FuncDecl {
	if file == nil || name == "" {
		return nil
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name == nil || fn.Name.Name != name {
			continue
		}
		return fn
	}
	return nil
}

func funcParamName(fn *ast.FuncDecl, index int) (string, bool) {
	if fn == nil || fn.Type == nil || fn.Type.Params == nil || index < 0 {
		return "", false
	}
	i := 0
	for _, field := range fn.Type.Params.List {
		if len(field.Names) == 0 {
			if i == index {
				return "", false
			}
			i++
			continue
		}
		for _, name := range field.Names {
			if i == index {
				if name == nil || name.Name == "" || name.Name == "_" {
					return "", false
				}
				return name.Name, true
			}
			i++
		}
	}
	return "", false
}

func exprIdent(expr ast.Expr) string {
	id, ok := unwrapExpr(expr).(*ast.Ident)
	if !ok || id.Name == "" || id.Name == "_" {
		return ""
	}
	return id.Name
}

func stringLit(expr ast.Expr) string {
	lit, ok := unwrapExpr(expr).(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return value
}

func unwrapExpr(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok || paren == nil {
			return expr
		}
		expr = paren.X
	}
}

func cloneBoolMap(src map[string]bool) map[string]bool {
	out := make(map[string]bool, len(src))
	maps.Copy(out, src)
	return out
}

func walkBlock(block *ast.BlockStmt, fn func(ast.Stmt)) {
	if block == nil {
		return
	}
	for _, stmt := range block.List {
		walkStmt(stmt, fn)
	}
}

func walkStmt(stmt ast.Stmt, fn func(ast.Stmt)) {
	if stmt == nil {
		return
	}
	fn(stmt)
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		walkBlock(s, fn)
	case *ast.IfStmt:
		walkStmt(s.Init, fn)
		walkBlock(s.Body, fn)
		walkStmt(s.Else, fn)
	case *ast.ForStmt:
		walkStmt(s.Init, fn)
		walkBlock(s.Body, fn)
	case *ast.RangeStmt:
		walkBlock(s.Body, fn)
	case *ast.SwitchStmt:
		walkStmt(s.Init, fn)
		walkBlock(s.Body, fn)
	case *ast.TypeSwitchStmt:
		walkStmt(s.Init, fn)
		walkBlock(s.Body, fn)
	case *ast.CaseClause:
		for _, inner := range s.Body {
			walkStmt(inner, fn)
		}
	case *ast.SelectStmt:
		walkBlock(s.Body, fn)
	case *ast.CommClause:
		for _, inner := range s.Body {
			walkStmt(inner, fn)
		}
	case *ast.LabeledStmt:
		walkStmt(s.Stmt, fn)
	}
}
