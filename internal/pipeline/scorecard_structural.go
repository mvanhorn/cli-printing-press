package pipeline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

func cliGoASTFiles(dir string) []*ast.File {
	files := listGoFiles(dir)
	parsed := make([]*ast.File, 0, len(files))
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			continue
		}
		parsed = append(parsed, file)
	}
	return parsed
}

func hasSyncPaginationStructure(cliDir string) bool {
	for _, file := range cliGoASTFiles(cliDir) {
		var found bool
		ast.Inspect(file, func(n ast.Node) bool {
			if found {
				return false
			}
			loop, ok := n.(*ast.ForStmt)
			if !ok || loop.Body == nil {
				return true
			}
			found = loopLooksLikePaginatedFetch(loop.Body)
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

func loopLooksLikePaginatedFetch(body *ast.BlockStmt) bool {
	var hasFetchCall, hasCursorSignal, hasStateSave, hasExit bool
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			if selectorName(node.Fun, "Get", "GetWithHeaders", "Do", "Execute", "Query") || strings.Contains(strings.ToLower(callName(node.Fun)), "fetch") {
				hasFetchCall = true
			}
			if selectorName(node.Fun, "SaveSyncState", "SaveSyncCursor") {
				hasStateSave = true
			}
		case *ast.Ident:
			if paginationName(node.Name) {
				hasCursorSignal = true
			}
		case *ast.SelectorExpr:
			if paginationName(node.Sel.Name) {
				hasCursorSignal = true
			}
		case *ast.BasicLit:
			if node.Kind == token.STRING && paginationLiteral(node.Value) {
				hasCursorSignal = true
			}
		case *ast.BranchStmt:
			if node.Tok == token.BREAK {
				hasExit = true
			}
		case *ast.ReturnStmt:
			hasExit = true
		}
		return true
	})
	return hasFetchCall && hasCursorSignal && (hasStateSave || hasExit)
}

func hasPageProgressStructure(cliDir string) bool {
	for _, file := range cliGoASTFiles(cliDir) {
		var found bool
		ast.Inspect(file, func(n ast.Node) bool {
			if found {
				return false
			}
			loop, ok := n.(*ast.ForStmt)
			if !ok || loop.Body == nil {
				return true
			}
			found = loopPrintsPageProgress(loop.Body)
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

func loopPrintsPageProgress(body *ast.BlockStmt) bool {
	var found bool
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !selectorName(call.Fun, "Printf", "Fprintf", "Fprintln", "Println") {
			return true
		}
		found = slices.ContainsFunc(call.Args, exprMentionsPage)
		return !found
	})
	return found
}

func selectorName(expr ast.Expr, names ...string) bool {
	got := callName(expr)
	return slices.Contains(names, got)
}

func callName(expr ast.Expr) string {
	switch fn := expr.(type) {
	case *ast.SelectorExpr:
		return fn.Sel.Name
	case *ast.Ident:
		return fn.Name
	default:
		return ""
	}
}

func exprMentionsPage(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return false
		}
		v, err := strconv.Unquote(e.Value)
		if err != nil {
			v = e.Value
		}
		return strings.Contains(strings.ToLower(v), "page")
	case *ast.Ident:
		return strings.Contains(strings.ToLower(e.Name), "page")
	case *ast.SelectorExpr:
		return strings.Contains(strings.ToLower(e.Sel.Name), "page")
	default:
		return false
	}
}

func paginationName(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, "cursor") ||
		strings.Contains(name, "nextpage") ||
		strings.Contains(name, "pagetoken") ||
		strings.Contains(name, "hasmore") ||
		strings.Contains(name, "hasnext")
}

func paginationLiteral(value string) bool {
	v, err := strconv.Unquote(value)
	if err != nil {
		v = value
	}
	v = strings.ToLower(v)
	return strings.Contains(v, "cursor") || strings.Contains(v, "next_page") || strings.Contains(v, "nextpage") || strings.Contains(v, "page_token") || strings.Contains(v, "pagetoken")
}

func codeOrchDelegatedFromRegisterTools(dir string) bool {
	toolsPath := filepath.Join(dir, "internal", "mcp", "tools.go")
	file, err := parser.ParseFile(token.NewFileSet(), toolsPath, nil, 0)
	if err != nil {
		return false
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Name == nil || fn.Name.Name != "RegisterTools" {
			continue
		}
		var delegates bool
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && selectorName(call.Fun, "RegisterCodeOrchestrationTools") {
				delegates = true
				return false
			}
			return true
		})
		return delegates
	}
	return false
}
