package cli

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func auditCobraSource(cliDir string) ([]ToolsAuditFinding, error) {
	pkgDir := filepath.Join(cliDir, "internal", "cli")
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", pkgDir, err)
	}
	var findings []ToolsAuditFinding
	fset := token.NewFileSet()
	var candidates []cobraAuditCandidate
	childrenByConstructor := map[string][]string{}
	mcpHiddenConstructors := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		full := filepath.Join(pkgDir, name)
		// Skip unparseable files because a separate build surfaces syntax errors.
		file, err := parser.ParseFile(fset, full, nil, 0)
		if err != nil {
			continue
		}
		literalOwners := map[token.Pos]string{}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name == nil || fn.Body == nil {
				continue
			}
			constructor := fn.Name.Name
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.FuncLit:
					return false
				case *ast.CompositeLit:
					if isCobraCommandType(node.Type) {
						literalOwners[node.Pos()] = constructor
						if extractCommandFields(node).mcpHidden {
							mcpHiddenConstructors[constructor] = true
						}
					}
				case *ast.CallExpr:
					if !isAuditAddCommandCall(node) {
						return true
					}
					for _, arg := range node.Args {
						if child := auditCobraConstructorCallName(arg); child != "" {
							childrenByConstructor[constructor] = append(childrenByConstructor[constructor], child)
						}
					}
				}
				return true
			})
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || !isCobraCommandType(lit.Type) {
				return true
			}
			fields := extractCommandFields(lit)
			if fields.use == "" {
				return true
			}
			candidates = append(candidates, cobraAuditCandidate{
				file:        name,
				line:        fset.Position(lit.Pos()).Line,
				fields:      fields,
				constructor: literalOwners[lit.Pos()],
			})
			return true
		})
	}

	hiddenReachable := map[string]bool{}
	var markHidden func(string)
	markHidden = func(constructor string) {
		if constructor == "" || hiddenReachable[constructor] {
			return
		}
		hiddenReachable[constructor] = true
		for _, child := range childrenByConstructor[constructor] {
			markHidden(child)
		}
	}
	for constructor := range mcpHiddenConstructors {
		markHidden(constructor)
	}

	hasParent := map[string]bool{}
	constructors := map[string]bool{}
	for constructor, children := range childrenByConstructor {
		constructors[constructor] = true
		for _, child := range children {
			constructors[child] = true
			hasParent[child] = true
		}
	}
	for _, candidate := range candidates {
		if candidate.constructor != "" {
			constructors[candidate.constructor] = true
		}
	}
	visibleReachable := map[string]bool{}
	var markVisible func(string)
	markVisible = func(constructor string) {
		if constructor == "" || visibleReachable[constructor] || mcpHiddenConstructors[constructor] {
			return
		}
		visibleReachable[constructor] = true
		for _, child := range childrenByConstructor[constructor] {
			markVisible(child)
		}
	}
	for constructor := range constructors {
		if !hasParent[constructor] {
			markVisible(constructor)
		}
	}

	for _, candidate := range candidates {
		hiddenOnly := hiddenReachable[candidate.constructor] && !visibleReachable[candidate.constructor]
		if candidate.fields.cobraHidden || candidate.fields.mcpHidden || hiddenOnly {
			continue
		}
		findings = append(findings, auditCommandFields(candidate.file, candidate.line, candidate.fields)...)
	}
	return findings, nil
}

type cobraAuditCandidate struct {
	file        string
	line        int
	fields      commandFields
	constructor string
}

func isAuditAddCommandCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "AddCommand"
}

func auditCobraConstructorCallName(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}
	ident, ok := call.Fun.(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name
}
