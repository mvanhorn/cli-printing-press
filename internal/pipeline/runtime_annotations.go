package pipeline

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
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
		if err != nil || !bytes.Contains(data, []byte(typedExitCodesAnnotation)) {
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
				out[name] = annotations
			}
			return true
		})
	}
	return out
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
