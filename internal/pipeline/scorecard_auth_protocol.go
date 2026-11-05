package pipeline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// cookieCredentialAttached reports whether client.go matches a credential
// attachment shape client.go.tmpl emits for headerName.
//
// Jar-only cookie auth is a SeedCookieJar* call inside New that takes
// cfg.CookieCredential() and passes that jar to newHTTPClient. That shape
// counts only for the name Cookie; a seeded jar does not stand in for
// Authorization or any other stamped header. Header-borne cookie auth and
// composed auth are Header.Set or Header.Add of headerName with the
// authHeader identifier (composed's empty header is already Authorization).
// OpenAPI apiKey-in-cookie is AddCookie of &http.Cookie{Name: headerName,
// Value: authHeader}.
//
// Dead calls, a later Header.Del, and hand-written clients that use another
// shape are not recognized. Bearer and api-key scoring have the same
// exposure: they match an emitted literal and nothing else.
func cookieCredentialAttached(clientContent, headerName string) bool {
	name := strings.TrimSpace(headerName)
	if name == "" {
		name = "Cookie"
	}
	file, err := parser.ParseFile(token.NewFileSet(), "client.go", clientContent, parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	if strings.EqualFold(name, "Cookie") && jarCredentialSeeded(file) {
		return true
	}
	hit := false
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || hit {
			return !hit
		}
		if headerStampsAuth(call, name) || cookieStampsAuth(call, name) {
			hit = true
			return false
		}
		return true
	})
	return hit
}

func cookieAuthHeaderName(scheme openAPISecurityScheme) string {
	name := strings.TrimSpace(scheme.HeaderName)
	if name == "" {
		return "Cookie"
	}
	return name
}

func jarCredentialSeeded(file *ast.File) bool {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "New" || fn.Body == nil {
			continue
		}
		if newWiresSeededJar(fn.Body) {
			return true
		}
	}
	return false
}

func newWiresSeededJar(body *ast.BlockStmt) bool {
	seeded := map[string]bool{}
	passed := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callName(call.Fun)
		if strings.HasPrefix(name, "SeedCookieJar") && argCallsCookieCredential(call) {
			if id := identName(unwrapParen(callArg(call, 0))); id != "" {
				seeded[id] = true
			}
		}
		if name == "newHTTPClient" {
			if id := identName(unwrapParen(callArg(call, 1))); id != "" {
				passed[id] = true
			}
		}
		return true
	})
	for id := range seeded {
		if passed[id] {
			return true
		}
	}
	return false
}

func argCallsCookieCredential(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		inner, ok := unwrapParen(arg).(*ast.CallExpr)
		if ok && callName(inner.Fun) == "CookieCredential" {
			return true
		}
	}
	return false
}

func headerStampsAuth(call *ast.CallExpr, headerName string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "Set" && sel.Sel.Name != "Add") {
		return false
	}
	header, ok := sel.X.(*ast.SelectorExpr)
	if !ok || header.Sel.Name != "Header" {
		return false
	}
	return strings.EqualFold(stringLit(callArg(call, 0)), headerName) && isAuthHeaderIdent(callArg(call, 1))
}

func cookieStampsAuth(call *ast.CallExpr, cookieName string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "AddCookie" || len(call.Args) != 1 {
		return false
	}
	expr := unwrapParen(call.Args[0])
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		expr = unwrapParen(unary.X)
	}
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return false
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
			valueOK = isAuthHeaderIdent(kv.Value)
		}
	}
	return valueOK && name != "" && strings.EqualFold(name, cookieName)
}

func callArg(call *ast.CallExpr, index int) ast.Expr {
	if call == nil || index < 0 || index >= len(call.Args) {
		return nil
	}
	return call.Args[index]
}

func isAuthHeaderIdent(expr ast.Expr) bool {
	id, ok := unwrapParen(expr).(*ast.Ident)
	return ok && id.Name == "authHeader"
}

func stringLit(expr ast.Expr) string {
	lit, ok := unwrapParen(expr).(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return value
}

func unwrapParen(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}
