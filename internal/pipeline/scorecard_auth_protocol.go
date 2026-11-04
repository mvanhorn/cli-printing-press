package pipeline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
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
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		wire := &reqWire{want: want, paths: []reqPath{{}}}
		wire.walkBlock(fn.Body)
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

// reqPath is one feasible control-flow state. dead means a later statement in
// this block does not run. broken means a break already recorded this path as
// an exit of the enclosing switch, select, or loop.
type reqPath struct {
	scopes reqEnv
	dead   bool
	broken bool
}

// breakFrame collects breaks that target the switch, select, or loop that
// pushed it. depth is the scope stack at that construct, so a break inside a
// case drops the case scope before the exit is used.
type breakFrame struct {
	depth int
	exits []reqPath
	outer *breakFrame
}

type reqWire struct {
	want     string
	paths    []reqPath
	matched  bool
	nextID   int
	breakTop *breakFrame
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

func (w *reqWire) anyLive() bool {
	for _, p := range w.paths {
		if !p.dead {
			return true
		}
	}
	return false
}

func (w *reqWire) onEach(fn func()) {
	incoming := append([]reqPath(nil), w.paths...)
	out := make([]reqPath, 0, len(incoming))
	for _, p := range incoming {
		if p.dead {
			out = append(out, p)
			continue
		}
		w.paths = []reqPath{p}
		fn()
		out = append(out, w.paths...)
	}
	w.paths = out
	w.shrink()
}

// withScope runs fn inside one more block scope on each live path.
func (w *reqWire) withScope(fn func()) {
	incoming := append([]reqPath(nil), w.paths...)
	var next []reqPath
	for _, p := range incoming {
		if p.dead {
			next = append(next, p)
			continue
		}
		w.paths = []reqPath{cloneReqPath(p)}
		w.push()
		fn()
		for i := range w.paths {
			popScope(&w.paths[i])
		}
		next = append(next, w.paths...)
	}
	w.paths = next
	w.shrink()
}

func (w *reqWire) pushBreak() {
	depth := 0
	if len(w.paths) > 0 {
		depth = len(w.paths[0].scopes)
	}
	w.breakTop = &breakFrame{depth: depth, outer: w.breakTop}
}

func (w *reqWire) popBreak() []reqPath {
	if w.breakTop == nil {
		return nil
	}
	exits := w.breakTop.exits
	w.breakTop = w.breakTop.outer
	return exits
}

func (w *reqWire) noteBreak() {
	if w.breakTop == nil || len(w.paths) == 0 {
		return
	}
	cp := trimScopes(cloneReqPath(w.paths[0]), w.breakTop.depth)
	cp.dead = false
	w.breakTop.exits = append(w.breakTop.exits, cp)
	w.paths[0].dead = true
	w.paths[0].broken = true
}

func (w *reqWire) shrink() {
	if len(w.paths) <= maxAuthPaths {
		return
	}
	var live, rest []reqPath
	for _, p := range w.paths {
		if p.dead || p.broken {
			rest = append(rest, p)
			continue
		}
		live = append(live, p)
	}
	if len(live) > 1 {
		acc := live[0]
		for _, p := range live[1:] {
			acc.scopes = mergeEnv(acc.scopes, p.scopes)
		}
		acc.dead = false
		acc.broken = false
		live = []reqPath{acc}
	}
	w.paths = append(live, rest...)
	if len(w.paths) > maxAuthPaths {
		w.paths = w.paths[:maxAuthPaths]
	}
}

func (w *reqWire) walkBlock(block *ast.BlockStmt) {
	if block == nil {
		return
	}
	w.push()
	for _, stmt := range block.List {
		if !w.anyLive() {
			break
		}
		w.walkStmt(stmt)
	}
	w.pop()
}

func (w *reqWire) walkStmt(stmt ast.Stmt) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		w.walkBlock(s)
	case *ast.ExprStmt:
		w.onEach(func() { w.observeExpr(s.X) })
	case *ast.AssignStmt:
		w.onEach(func() { w.assign(s.Tok, s.Lhs, s.Rhs) })
	case *ast.ReturnStmt:
		w.onEach(func() {
			for _, result := range s.Results {
				w.observeExpr(result)
			}
			w.paths[0].dead = true
		})
	case *ast.BranchStmt:
		w.onEach(func() { w.branch(s) })
	case *ast.IfStmt:
		w.walkIf(s)
	case *ast.ForStmt:
		w.walkFor(s)
	case *ast.RangeStmt:
		w.walkRange(s)
	case *ast.SwitchStmt:
		w.withScope(func() {
			w.walkStmt(s.Init)
			w.walkCases(s.Body, true)
		})
	case *ast.TypeSwitchStmt:
		w.withScope(func() {
			w.walkStmt(s.Init)
			w.walkStmt(s.Assign)
			w.walkCases(s.Body, true)
		})
	case *ast.SelectStmt:
		w.walkCases(s.Body, false)
	case *ast.LabeledStmt:
		w.walkStmt(s.Stmt)
	}
}

// branch records break as an exit of the innermost switch, select, or loop.
// A label still uses that innermost frame. The path is not a function return,
// so a return later in the case does not drop the break path.
func (w *reqWire) branch(s *ast.BranchStmt) {
	if s.Tok == token.BREAK {
		w.noteBreak()
	}
}

// walkIf keeps each branch's request state. A Do after the if sees only
// branches that fall through; a return ends that branch before the join.
func (w *reqWire) walkIf(s *ast.IfStmt) {
	incoming := append([]reqPath(nil), w.paths...)
	var next []reqPath
	for _, p := range incoming {
		if p.dead {
			next = append(next, p)
			continue
		}
		w.paths = []reqPath{cloneReqPath(p)}
		w.push()
		w.walkStmt(s.Init)
		started := append([]reqPath(nil), w.paths...)
		var joined []reqPath
		for _, base := range started {
			if base.dead {
				joined = append(joined, base)
				continue
			}
			w.paths = []reqPath{cloneReqPath(base)}
			w.walkBlock(s.Body)
			thenPaths := append([]reqPath(nil), w.paths...)
			w.paths = []reqPath{cloneReqPath(base)}
			w.walkStmt(s.Else)
			elsePaths := append([]reqPath(nil), w.paths...)
			joined = append(joined, joinBranches(thenPaths, elsePaths, base)...)
		}
		for i := range joined {
			popScope(&joined[i])
		}
		next = append(next, joined...)
	}
	w.paths = next
	w.shrink()
}

func joinBranches(thenPaths, elsePaths []reqPath, base reqPath) []reqPath {
	thenLive := living(thenPaths)
	elseLive := living(elsePaths)
	switch {
	case len(thenLive) == 0 && len(elseLive) == 0:
		base = cloneReqPath(base)
		base.dead = true
		return []reqPath{base}
	case len(thenLive) == 0:
		return elseLive
	case len(elseLive) == 0:
		return thenLive
	default:
		return append(thenLive, elseLive...)
	}
}

func living(paths []reqPath) []reqPath {
	var out []reqPath
	for _, p := range paths {
		if p.dead || p.broken {
			continue
		}
		out = append(out, p)
	}
	return out
}

// walkCases treats case and comm bodies as alternatives. fallthrough chains
// the next case onto this one; a switch with no default also keeps the path
// that matches nothing. Select has no such path: some comm runs. break exits
// the switch or select with the state at the break, including when a later
// statement in the case returns.
func (w *reqWire) walkCases(body *ast.BlockStmt, switchStmt bool) {
	if body == nil {
		return
	}
	incoming := append([]reqPath(nil), w.paths...)
	var next []reqPath
	for _, p := range incoming {
		if p.dead {
			next = append(next, p)
			continue
		}
		base := cloneReqPath(p)
		w.paths = []reqPath{base}
		w.pushBreak()
		exits := w.caseExits(body, base, switchStmt)
		exits = append(exits, w.popBreak()...)
		if len(exits) == 0 {
			dead := cloneReqPath(base)
			dead.dead = true
			next = append(next, dead)
			continue
		}
		next = append(next, exits...)
	}
	w.paths = next
	w.shrink()
}

func (w *reqWire) caseExits(body *ast.BlockStmt, base reqPath, switchStmt bool) []reqPath {
	var exits []reqPath
	var falling []reqPath
	sawDefault := false
	for _, stmt := range body.List {
		var entries []reqPath
		if falling != nil {
			entries = falling
			falling = nil
		} else {
			entries = []reqPath{cloneReqPath(base)}
		}
		for i := range entries {
			entries[i].dead = false
		}
		w.paths = entries
		fell := false
		switch clause := stmt.(type) {
		case *ast.CaseClause:
			if clause.List == nil {
				sawDefault = true
			}
			fell = w.walkCaseBody(clause.Body, switchStmt)
		case *ast.CommClause:
			if clause.Comm == nil {
				sawDefault = true
			}
			w.walkComm(clause)
		default:
			w.walkStmt(stmt)
		}
		if fell {
			falling = living(w.paths)
			continue
		}
		exits = append(exits, living(w.paths)...)
	}
	if len(falling) > 0 {
		exits = append(exits, falling...)
	}
	if switchStmt && !sawDefault {
		exits = append(exits, cloneReqPath(base))
	}
	return exits
}

func (w *reqWire) walkCaseBody(body []ast.Stmt, fallthroughOK bool) bool {
	w.push()
	defer w.pop()
	for _, inner := range body {
		if fallthroughOK && isFallthrough(inner) {
			return w.anyLive()
		}
		w.walkStmt(inner)
		if !w.anyLive() {
			return false
		}
	}
	return false
}

func (w *reqWire) walkComm(clause *ast.CommClause) {
	w.push()
	defer w.pop()
	w.walkStmt(clause.Comm)
	for _, inner := range clause.Body {
		if !w.anyLive() {
			return
		}
		w.walkStmt(inner)
	}
}

func (w *reqWire) walkFor(s *ast.ForStmt) {
	w.walkLoop(func() { w.walkStmt(s.Init) }, func() { w.walkBlock(s.Body) })
}

func (w *reqWire) walkRange(s *ast.RangeStmt) {
	w.walkLoop(func() {
		if s.Tok == token.DEFINE {
			w.shadow(s.Key)
			w.shadow(s.Value)
		}
	}, func() { w.walkBlock(s.Body) })
}

// walkLoop runs init and then body. break leaves the loop with the state at
// the break. A return inside the body does not kill the statements after the
// loop: the loop may not take that path.
func (w *reqWire) walkLoop(init, body func()) {
	incoming := append([]reqPath(nil), w.paths...)
	var next []reqPath
	for _, p := range incoming {
		if p.dead {
			next = append(next, p)
			continue
		}
		w.paths = []reqPath{cloneReqPath(p)}
		w.push()
		init()
		started := append([]reqPath(nil), w.paths...)
		var succ []reqPath
		for _, base := range started {
			if base.dead {
				succ = append(succ, base)
				continue
			}
			w.paths = []reqPath{cloneReqPath(base)}
			w.pushBreak()
			body()
			breaks := w.popBreak()
			succ = append(succ, breaks...)
			for _, walked := range w.paths {
				if walked.broken {
					continue
				}
				walked.dead = false
				succ = append(succ, walked)
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

func (w *reqWire) shadow(expr ast.Expr) {
	if id := exprIdent(expr); id != "" {
		w.bind(id, nil, true)
	}
}

func (w *reqWire) assign(tok token.Token, lhs, rhs []ast.Expr) {
	if len(rhs) != 1 || len(lhs) == 0 {
		return
	}
	if call, ok := rhs[0].(*ast.CallExpr); ok && callName(call.Fun) == "Do" {
		w.noteDo(call)
		return
	}
	id := exprIdent(lhs[0])
	if id == "" {
		return
	}
	define := tok == token.DEFINE
	if call, ok := rhs[0].(*ast.CallExpr); ok {
		if selectorName(call.Fun, "NewRequest", "NewRequestWithContext") {
			w.bind(id, []*reqState{w.freshReq()}, define)
			return
		}
		if sel, ok := unwrapExpr(call.Fun).(*ast.SelectorExpr); ok {
			if recv, ok := w.lookup(exprIdent(sel.X)); ok {
				next := recv
				if sel.Sel.Name == "Clone" {
					next = w.detachAll(recv)
				}
				w.bind(id, next, define)
				return
			}
		}
		if src := w.requestArg(call); len(src) > 0 {
			w.bind(id, src, define)
			return
		}
	}
	if src, ok := w.lookup(exprIdent(rhs[0])); ok {
		w.bind(id, src, define)
		return
	}
	if define {
		if w.known(id) {
			w.bind(id, nil, true)
		}
		return
	}
	if _, ok := w.lookup(id); ok {
		w.bind(id, []*reqState{w.freshReq()}, false)
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
	if len(w.paths) == 0 || w.paths[0].dead || len(call.Args) == 0 {
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
	return reqPath{scopes: cloneEnv(p.scopes), dead: p.dead}
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
