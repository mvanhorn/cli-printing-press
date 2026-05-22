package pipeline

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"strconv"
)

func enrichCommandAnnotationsFromSource(dir string, commands []discoveredCommand) []discoveredCommand {
	if len(commands) == 0 {
		return commands
	}
	annotations := sourceCommandAnnotations(dir)
	if len(annotations) == 0 {
		return commands
	}
	for i := range commands {
		if found := annotations[commands[i].Name]; len(found) > 0 {
			commands[i].Annotations = found
		}
	}
	return commands
}

func sourceCommandAnnotations(dir string) map[string]map[string]string {
	if dir == "" {
		return nil
	}
	cliDir := filepath.Join(dir, "internal", "cli")
	files := listGoFiles(cliDir)
	if len(files) == 0 {
		return nil
	}

	fset := token.NewFileSet()
	out := map[string]map[string]string{}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil || (!bytes.Contains(data, []byte(typedExitCodesAnnotation)) && !bytes.Contains(data, []byte(happyArgsAnnotation))) {
			continue
		}
		file, err := parser.ParseFile(fset, path, data, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || !isCobraCommandLiteral(lit) {
				return true
			}
			name, annotations := commandLiteralMetadata(lit)
			if name != "" && len(annotations) > 0 {
				mergeCommandAnnotations(out, name, annotations)
			}
			return true
		})
		for name, annotations := range commandAnnotationAssignments(file) {
			mergeCommandAnnotations(out, name, annotations)
		}
	}
	return out
}

func mergeCommandAnnotations(out map[string]map[string]string, name string, annotations map[string]string) {
	if len(annotations) == 0 {
		return
	}
	if out[name] == nil {
		out[name] = map[string]string{}
	}
	maps.Copy(out[name], annotations)
}

func isCobraCommandLiteral(lit *ast.CompositeLit) bool {
	switch typ := lit.Type.(type) {
	case *ast.SelectorExpr:
		return typ.Sel.Name == "Command"
	case *ast.Ident:
		return typ.Name == "Command"
	default:
		return false
	}
}

func commandLiteralMetadata(lit *ast.CompositeLit) (string, map[string]string) {
	var name string
	var annotations map[string]string
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
			name = commandNameFromUse(stringLiteralValue(kv.Value))
		case "Annotations":
			annotations = stringMapLiteral(kv.Value)
		}
	}
	return name, annotations
}

func commandNameFromUse(use string) string {
	if match := cobraUseLeafRe.FindStringSubmatch(`Use: "` + use + `"`); match != nil {
		return match[1]
	}
	return ""
}

func stringLiteralValue(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return value
}

func stringMapLiteral(expr ast.Expr) map[string]string {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	out := map[string]string{}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key := stringLiteralValue(kv.Key)
		value := stringLiteralValue(kv.Value)
		if key != "" {
			out[key] = value
		}
	}
	return out
}

func commandAnnotationAssignments(file *ast.File) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		commandNames := map[string]string{}
		annotationsByVar := map[string]map[string]string{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, lhs := range assign.Lhs {
				if i >= len(assign.Rhs) {
					continue
				}
				if ident, ok := lhs.(*ast.Ident); ok {
					if name, annotations := commandLiteralMetadataFromExpr(assign.Rhs[i]); name != "" {
						if previousName := commandNames[ident.Name]; previousName != "" {
							mergeCommandAnnotations(out, previousName, annotationsByVar[ident.Name])
						}
						commandNames[ident.Name] = name
						if len(annotations) > 0 {
							annotationsByVar[ident.Name] = maps.Clone(annotations)
						} else {
							delete(annotationsByVar, ident.Name)
						}
						continue
					}
				}
				varName, key, ok := annotationIndex(lhs)
				if !ok {
					if varName, annotations, ok := annotationMapAssignment(lhs, assign.Rhs[i]); ok {
						if annotationsByVar[varName] == nil {
							annotationsByVar[varName] = map[string]string{}
						}
						maps.Copy(annotationsByVar[varName], annotations)
					}
					continue
				}
				value := stringLiteralValue(assign.Rhs[i])
				if value == "" {
					continue
				}
				if annotationsByVar[varName] == nil {
					annotationsByVar[varName] = map[string]string{}
				}
				annotationsByVar[varName][key] = value
			}
			return true
		})
		for varName, annotations := range annotationsByVar {
			name := commandNames[varName]
			if name == "" {
				continue
			}
			mergeCommandAnnotations(out, name, annotations)
		}
	}
	return out
}

func commandLiteralMetadataFromExpr(expr ast.Expr) (string, map[string]string) {
	switch v := expr.(type) {
	case *ast.CompositeLit:
		if isCobraCommandLiteral(v) {
			return commandLiteralMetadata(v)
		}
	case *ast.UnaryExpr:
		if lit, ok := v.X.(*ast.CompositeLit); ok && isCobraCommandLiteral(lit) {
			return commandLiteralMetadata(lit)
		}
	}
	return "", nil
}

func annotationIndex(expr ast.Expr) (string, string, bool) {
	index, ok := expr.(*ast.IndexExpr)
	if !ok {
		return "", "", false
	}
	key := stringLiteralValue(index.Index)
	if key == "" {
		return "", "", false
	}
	selector, ok := index.X.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Annotations" {
		return "", "", false
	}
	ident, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", "", false
	}
	return ident.Name, key, true
}

func annotationMapAssignment(lhs, rhs ast.Expr) (string, map[string]string, bool) {
	selector, ok := lhs.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Annotations" {
		return "", nil, false
	}
	ident, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", nil, false
	}
	annotations := stringMapLiteral(rhs)
	return ident.Name, annotations, len(annotations) > 0
}
