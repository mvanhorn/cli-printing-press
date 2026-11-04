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
		wire := &reqWire{want: want}
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
// id is the object identity: a path fork keeps it, and a join unions headers
// of the same id so a Del on one branch cannot erase a credential another
// branch still carries into Do.
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

type reqWire struct {
	want    string
	scopes  reqEnv
	matched bool
	dead    bool
	nextID  int
}

func (w *reqWire) push() {
	w.scopes = append(w.scopes, map[string][]*reqState{})
}

func (w *reqWire) pop() {
	w.scopes = w.scopes[:len(w.scopes)-1]
}

func (w *reqWire) lookup(name string) ([]*reqState, bool) {
	if name == "" {
		return nil, false
	}
	for i := len(w.scopes) - 1; i >= 0; i-- {
		states, ok := w.scopes[i][name]
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
	for i := len(w.scopes) - 1; i >= 0; i-- {
		if _, ok := w.scopes[i][name]; ok {
			return true
		}
	}
	return false
}

// bind records name in the current block when define is set, which is how Go
// shadows an outer short-variable declaration. Plain assignment updates the
// block that already holds the name.
func (w *reqWire) bind(name string, states []*reqState, define bool) {
	if name == "" || len(w.scopes) == 0 {
		return
	}
	if states != nil {
		states = append([]*reqState(nil), states...)
	}
	if define {
		w.scopes[len(w.scopes)-1][name] = states
		return
	}
	for i := len(w.scopes) - 1; i >= 0; i-- {
		if _, ok := w.scopes[i][name]; ok {
			w.scopes[i][name] = states
			return
		}
	}
	w.scopes[len(w.scopes)-1][name] = states
}

func (w *reqWire) freshReq() *reqState {
	w.nextID++
	return &reqState{
		id:      w.nextID,
		headers: map[string]bool{},
		cookies: map[string]bool{},
	}
}

func (w *reqWire) walkBlock(block *ast.BlockStmt) {
	if block == nil || w.dead {
		return
	}
	w.push()
	for _, stmt := range block.List {
		if w.dead {
			break
		}
		w.walkStmt(stmt)
	}
	w.pop()
}

func (w *reqWire) walkStmt(stmt ast.Stmt) {
	if stmt == nil || w.dead {
		return
	}
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		w.walkBlock(s)
	case *ast.ExprStmt:
		w.observeExpr(s.X)
	case *ast.AssignStmt:
		w.assign(s.Tok, s.Lhs, s.Rhs)
	case *ast.ReturnStmt:
		for _, result := range s.Results {
			w.observeExpr(result)
		}
		w.dead = true
	case *ast.IfStmt:
		w.walkIf(s)
	case *ast.ForStmt:
		w.push()
		w.walkStmt(s.Init)
		dead := w.dead
		w.walkBlock(s.Body)
		// The loop may not run, and a return inside it does not kill the
		// statements after the loop.
		w.dead = dead
		w.pop()
	case *ast.RangeStmt:
		w.push()
		if s.Tok == token.DEFINE {
			w.shadow(s.Key)
			w.shadow(s.Value)
		}
		dead := w.dead
		w.walkBlock(s.Body)
		w.dead = dead
		w.pop()
	case *ast.SwitchStmt:
		w.push()
		w.walkStmt(s.Init)
		w.walkCases(s.Body, true)
		w.pop()
	case *ast.TypeSwitchStmt:
		w.push()
		w.walkStmt(s.Init)
		w.walkStmt(s.Assign)
		w.walkCases(s.Body, true)
		w.pop()
	case *ast.SelectStmt:
		w.walkCases(s.Body, false)
	case *ast.LabeledStmt:
		w.walkStmt(s.Stmt)
	}
}

// walkIf keeps each branch's request state. A Do after the if sees only
// branches that fall through; a return ends that branch before the join.
func (w *reqWire) walkIf(s *ast.IfStmt) {
	w.push()
	w.walkStmt(s.Init)
	base := cloneEnv(w.scopes)
	w.walkBlock(s.Body)
	thenDead := w.dead
	thenEnv := cloneEnv(w.scopes)
	w.dead = false
	w.scopes = cloneEnv(base)
	w.walkStmt(s.Else)
	w.finishJoin(base, thenEnv, thenDead, w.scopes, w.dead)
	w.pop()
}

func (w *reqWire) finishJoin(base, left reqEnv, leftDead bool, right reqEnv, rightDead bool) {
	switch {
	case leftDead && rightDead:
		w.dead = true
		w.scopes = base
	case leftDead:
		w.dead = false
		w.scopes = right
	case rightDead:
		w.dead = false
		w.scopes = left
	default:
		w.dead = false
		w.scopes = mergeEnv(left, right)
	}
}

// walkCases treats case and comm bodies as alternatives. fallthrough chains
// the next case onto this one; a switch with no default also keeps the path
// that matches nothing. Select has no such path: some comm runs.
func (w *reqWire) walkCases(body *ast.BlockStmt, switchStmt bool) {
	if body == nil {
		return
	}
	base := cloneEnv(w.scopes)
	var exits []reqEnv
	var fallFrom reqEnv
	falling := false
	sawDefault := false
	for _, stmt := range body.List {
		entry := cloneEnv(base)
		if falling {
			entry = mergeEnv(entry, fallFrom)
			falling = false
		}
		w.dead = false
		w.scopes = entry
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
		if w.dead {
			w.dead = false
			continue
		}
		if fell {
			fallFrom = cloneEnv(w.scopes)
			falling = true
			continue
		}
		exits = append(exits, cloneEnv(w.scopes))
	}
	if falling {
		exits = append(exits, fallFrom)
	}
	if switchStmt && !sawDefault {
		exits = append(exits, base)
	}
	if len(exits) == 0 {
		w.dead = true
		w.scopes = base
		return
	}
	merged := exits[0]
	for _, exit := range exits[1:] {
		merged = mergeEnv(merged, exit)
	}
	w.dead = false
	w.scopes = merged
}

func (w *reqWire) walkCaseBody(body []ast.Stmt, fallthroughOK bool) bool {
	w.push()
	defer w.pop()
	for _, inner := range body {
		if fallthroughOK && isFallthrough(inner) {
			return true
		}
		w.walkStmt(inner)
		if w.dead {
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
		if w.dead {
			return
		}
		w.walkStmt(inner)
	}
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
			for _, state := range states {
				state.applyHeader(op, name, value)
			}
		}
		return
	}
	if req, name, ok := addCookieOnRequest(call); ok {
		if states, known := w.lookup(req); known {
			for _, state := range states {
				state.addCookie(name)
			}
		}
		return
	}
	if callName(call.Fun) == "Do" {
		w.noteDo(call)
	}
}

func (w *reqWire) noteDo(call *ast.CallExpr) {
	if w.dead || len(call.Args) == 0 {
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
	for i := 0; i < n; i++ {
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
