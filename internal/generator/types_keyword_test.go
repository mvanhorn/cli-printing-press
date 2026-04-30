package generator

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGenerateTypesAvoidsGoKeywordCollision covers issue #275 F-3. GitHub's
// OpenAPI spec has hundreds of type definitions; at least one is named `import`,
// which the generator emits verbatim as `type import struct {`, a parse error
// because `import` is a Go reserved word. Other reserved words (`package`,
// `func`, `type`, `var`, `range`, `select`, etc.) hit the same trap.
//
// `safeTypeName` (and the OpenAPI parser's `sanitizeTypeName`) strip
// non-alphanumeric characters but never check for keywords. Both should
// produce identifiers that are guaranteed-safe for `type X struct { ... }`.
func TestGenerateTypesAvoidsGoKeywordCollision(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("keyword-types")
	apiSpec.Types = map[string]spec.TypeDef{
		"import":  {Fields: []spec.TypeField{{Name: "id", Type: "string"}}},
		"package": {Fields: []spec.TypeField{{Name: "name", Type: "string"}}},
		"func":    {Fields: []spec.TypeField{{Name: "name", Type: "string"}}},
		"User":    {Fields: []spec.TypeField{{Name: "id", Type: "string"}}},
	}

	outputDir := filepath.Join(t.TempDir(), "keyword-types-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	typesPath := filepath.Join(outputDir, "internal", "types", "types.go")
	src, err := os.ReadFile(typesPath)
	require.NoError(t, err, "generated types.go must exist")

	_, err = parser.ParseFile(token.NewFileSet(), typesPath, src, 0)
	require.NoError(t, err,
		"generated types.go must parse as Go even when source schemas use Go reserved words for names")
}
