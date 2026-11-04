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

// reqState is one *http.Request. Aliases share the pointer, so a later Set or
// Del is visible through every name that still refers to it. Clone gets its
// own header map; WithContext and guardOutbound keep this one.
type reqState struct {
	headers map[string]bool
	cookies map[string]bool
}

func newReqState() *reqState {
	return &reqState{
		headers: map[string]bool{},
		cookies: map[string]bool{},
	}
}

func (s *reqState) clone() *reqState {
	if s == nil {
		return newReqState()
	}
	return &reqState{
		headers: cloneBoolMap(s.headers),
		cookies: cloneBoolMap(s.cookies),
	}
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

type reqWire struct {
	want    string
	scopes  []map[string]*reqState
	matched bool
}

func (w *reqWire) push() {
	w.scopes = append(w.scopes, map[string]*reqState{})
}

func (w *reqWire) pop() {
	w.scopes = w.scopes[:len(w.scopes)-1]
}

func (w *reqWire) lookup(name string) (*reqState, bool) {
	if name == "" {
		return nil, false
	}
	for i := len(w.scopes) - 1; i >= 0; i-- {
		state, ok := w.scopes[i][name]
		if !ok {
			continue
		}
		return state, state != nil
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
func (w *reqWire) bind(name string, state *reqState, define bool) {
	if name == "" || len(w.scopes) == 0 {
		return
	}
	if define {
		w.scopes[len(w.scopes)-1][name] = state
		return
	}
	for i := len(w.scopes) - 1; i >= 0; i-- {
		if _, ok := w.scopes[i][name]; ok {
			w.scopes[i][name] = state
			return
		}
	}
	w.scopes[len(w.scopes)-1][name] = state
}

func (w *reqWire) walkBlock(block *ast.BlockStmt) {
	if block == nil {
		return
	}
	w.push()
	for _, stmt := range block.List {
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
		w.observeExpr(s.X)
	case *ast.AssignStmt:
		w.assign(s.Tok, s.Lhs, s.Rhs)
	case *ast.ReturnStmt:
		for _, result := range s.Results {
			w.observeExpr(result)
		}
	case *ast.IfStmt:
		w.push()
		w.walkStmt(s.Init)
		w.walkBlock(s.Body)
		w.walkStmt(s.Else)
		w.pop()
	case *ast.ForStmt:
		w.push()
		w.walkStmt(s.Init)
		w.walkBlock(s.Body)
		w.pop()
	case *ast.RangeStmt:
		w.push()
		if s.Tok == token.DEFINE {
			w.shadow(s.Key)
			w.shadow(s.Value)
		}
		w.walkBlock(s.Body)
		w.pop()
	case *ast.SwitchStmt:
		w.push()
		w.walkStmt(s.Init)
		w.walkBlock(s.Body)
		w.pop()
	case *ast.TypeSwitchStmt:
		w.push()
		w.walkStmt(s.Init)
		w.walkStmt(s.Assign)
		w.walkBlock(s.Body)
		w.pop()
	case *ast.CaseClause:
		w.push()
		for _, inner := range s.Body {
			w.walkStmt(inner)
		}
		w.pop()
	case *ast.SelectStmt:
		w.walkBlock(s.Body)
	case *ast.CommClause:
		w.push()
		w.walkStmt(s.Comm)
		for _, inner := range s.Body {
			w.walkStmt(inner)
		}
		w.pop()
	case *ast.LabeledStmt:
		w.walkStmt(s.Stmt)
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
			w.bind(id, newReqState(), define)
			return
		}
		if sel, ok := unwrapExpr(call.Fun).(*ast.SelectorExpr); ok {
			if recv, ok := w.lookup(exprIdent(sel.X)); ok {
				next := recv
				if sel.Sel.Name == "Clone" {
					next = recv.clone()
				}
				w.bind(id, next, define)
				return
			}
		}
		if src := w.requestArg(call); src != nil {
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
		w.bind(id, newReqState(), false)
	}
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
		if state, known := w.lookup(req); known {
			state.applyHeader(op, name, value)
		}
		return
	}
	if req, name, ok := addCookieOnRequest(call); ok {
		if state, known := w.lookup(req); known {
			state.addCookie(name)
		}
		return
	}
	if callName(call.Fun) == "Do" {
		w.noteDo(call)
	}
}

func (w *reqWire) noteDo(call *ast.CallExpr) {
	if len(call.Args) == 0 {
		return
	}
	state, ok := w.lookup(exprIdent(call.Args[0]))
	if ok && state.carries(w.want) {
		w.matched = true
	}
}

func (w *reqWire) requestArg(call *ast.CallExpr) *reqState {
	for _, arg := range call.Args {
		if state, ok := w.lookup(exprIdent(arg)); ok {
			return state
		}
	}
	return nil
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
