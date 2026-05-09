package generator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmbeddedTemplateReadsUseSlashPaths(t *testing.T) {
	t.Parallel()

	for _, filename := range []string{"generator.go", "plan_generate.go"} {
		t.Run(filename, func(t *testing.T) {
			t.Parallel()

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, filename, nil, 0)
			require.NoError(t, err)

			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}

				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Join" {
					return true
				}
				ident, ok := selector.X.(*ast.Ident)
				if !ok || ident.Name != "filepath" || len(call.Args) == 0 {
					return true
				}
				firstArg, ok := call.Args[0].(*ast.BasicLit)
				require.Falsef(t, ok && firstArg.Value == `"templates"`, "%s uses filepath.Join for an embed.FS template path at %s", filename, fset.Position(call.Pos()))
				return true
			})
		})
	}
}
