package generator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateDeduplicatesCamelCollidingTypeFields runs an end-to-end
// generation on a TypeDef whose JSON keys collide under toCamel, then
// AST-walks the emitted struct to confirm: (a) every input field is
// represented, (b) every Go identifier is unique, and (c) JSON tags are
// preserved verbatim. Subprocess-level Go syntax is checked by parsing.
func TestGenerateDeduplicatesCamelCollidingTypeFields(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("collide-types")
	apiSpec.Types = map[string]spec.TypeDef{
		"reaction_rollup": {
			Fields: []spec.TypeField{
				{Name: "+1", Type: "integer"},
				{Name: "-1", Type: "integer"},
				{Name: "confused", Type: "integer"},
				{Name: "laugh", Type: "integer"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "collide-types-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	typesPath := filepath.Join(outputDir, "internal", "types", "types.go")
	src, err := os.ReadFile(typesPath)
	require.NoError(t, err)

	fields, jsonTags := structFieldsByTypeName(t, typesPath, src, "reaction_rollup")
	assert.ElementsMatch(t, []string{"+1", "-1", "confused", "laugh"}, jsonTags)
	require.Len(t, fields, 4)
	assertNoDuplicates(t, fields, "every Go field name must be unique")
}

// TestGenerateDeduplicatesCollidingNonScalarTypeFields covers the case
// where colliding fields are non-scalar (object/array) types, which take
// the json.RawMessage path in goStructType and require the
// "encoding/json" import to be emitted.
func TestGenerateDeduplicatesCollidingNonScalarTypeFields(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("collide-nonscalar")
	apiSpec.Types = map[string]spec.TypeDef{
		"wrapper": {
			Fields: []spec.TypeField{
				{Name: "+1", Type: "object"},
				{Name: "-1", Type: "object"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "collide-nonscalar-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	typesPath := filepath.Join(outputDir, "internal", "types", "types.go")
	src, err := os.ReadFile(typesPath)
	require.NoError(t, err)

	fields, _ := structFieldsByTypeName(t, typesPath, src, "wrapper")
	require.Len(t, fields, 2)
	assertNoDuplicates(t, fields, "non-scalar colliding fields must produce distinct idents")
	assert.Contains(t, string(src), `import "encoding/json"`,
		"the json import must be emitted when any field is non-scalar, even after dedup")
}

// TestUniquifyTypeFieldIdentifiers exercises the dedup logic directly,
// avoiding a full generator round-trip per case.
func TestUniquifyTypeFieldIdentifiers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		input      []spec.TypeField
		wantIdents []string // toCamel of (IdentName || Name) per field
	}{
		{
			name:       "single special-prefix field keeps canonical V1",
			input:      []spec.TypeField{{Name: "+1"}},
			wantIdents: []string{"V1"},
		},
		{
			name: "paired punctuation prefixes resolve to distinct idents",
			input: []spec.TypeField{
				{Name: "+1"},
				{Name: "-1"},
			},
			wantIdents: []string{"V1", "V12"},
		},
		{
			name: "multi-class collisions across pairs",
			input: []spec.TypeField{
				{Name: "*key"},
				{Name: "@key"},
				{Name: "%val"},
				{Name: "!val"},
			},
			wantIdents: []string{"Key", "Key2", "Val", "Val2"},
		},
		{
			name: "unrelated fields untouched",
			input: []spec.TypeField{
				{Name: "id"},
				{Name: "name"},
			},
			wantIdents: []string{"Id", "Name"},
		},
		{
			name:       "empty fields",
			input:      []spec.TypeField{},
			wantIdents: []string{},
		},
		{
			name: "three-way collision walks suffixes until distinct",
			input: []spec.TypeField{
				{Name: "+1"},
				{Name: "-1"},
				{Name: "*1"},
			},
			wantIdents: []string{"V1", "V12", "V13"},
		},
		{
			name: "suffix candidate collides with a literal sibling",
			input: []spec.TypeField{
				{Name: "+1"},
				{Name: "-1"},
				{Name: "12"},
			},
			wantIdents: []string{"V1", "V12", "V122"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := uniquifyTypeFieldIdentifiers(tc.input)
			require.Len(t, out, len(tc.wantIdents))
			got := make([]string, len(out))
			for i, f := range out {
				got[i] = toCamel(typeFieldIdent(f))
				assert.Equal(t, tc.input[i].Name, f.Name, "Name must never be mutated")
			}
			assert.Equal(t, tc.wantIdents, got)
		})
	}
}

// structFieldsByTypeName returns the Go field identifiers and JSON tag
// values for the named struct in the parsed source. The lookup is routed
// through safeTypeName so callers pass the spec map key and the helper
// handles any name transform the template applied (hyphens, dots, etc.).
func structFieldsByTypeName(t *testing.T, path string, src []byte, typeName string) (fields, jsonTags []string) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	require.NoError(t, err)

	expected := safeTypeName(typeName)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, sp := range gen.Specs {
			ts, ok := sp.(*ast.TypeSpec)
			if !ok || ts.Name.Name != expected {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, f := range st.Fields.List {
				for _, n := range f.Names {
					fields = append(fields, n.Name)
				}
				if f.Tag != nil {
					tag := reflect.StructTag(strings.Trim(f.Tag.Value, "`"))
					if json := tag.Get("json"); json != "" {
						if comma := strings.Index(json, ","); comma >= 0 {
							json = json[:comma]
						}
						jsonTags = append(jsonTags, json)
					}
				}
			}
			return fields, jsonTags
		}
	}
	t.Fatalf("type %q not found in %s", typeName, path)
	return nil, nil
}
