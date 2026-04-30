package generator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateDeduplicatesCamelCollidingParams covers Case A from issue #275 F-2.
// Twilio's spec lists StartTime, StartTime>, and StartTime< as distinct query
// params for date-range filtering. toCamel strips '>' and '<' as non-alphanumeric,
// so all three would yield Go identifier "StartTime" and the template would emit
// three `var flagStartTime` declarations in one function — illegal redeclaration.
func TestGenerateDeduplicatesCamelCollidingParams(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("collide-camel")
	// Two endpoints so `list` renders to its own file rather than being
	// consolidated by the single-endpoint promotion path.
	apiSpec.Resources["calls"] = spec.Resource{
		Description: "Calls",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/calls",
				Description: "List calls within a date range",
				Params: []spec.Param{
					{Name: "StartTime", Type: "string", Description: "Exact timestamp"},
					{Name: "StartTime>", Type: "string", Description: "After timestamp"},
					{Name: "StartTime<", Type: "string", Description: "Before timestamp"},
				},
			},
			"get": {
				Method:      "GET",
				Path:        "/calls/{id}",
				Description: "Get one call",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "collide-camel-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	flagVars, flagBindings := parseFlagDeclarations(t,
		filepath.Join(outputDir, "internal", "cli", "calls_list.go"))

	assertNoDuplicates(t, flagVars,
		"each param must produce a distinct Go identifier")
	assertNoDuplicates(t, flagBindings,
		"each param must register a distinct cobra flag name")
	require.Len(t, flagVars, 3,
		"all three params must still be represented after dedup")
}

// TestGenerateRenamesParamCollidingWithPaginationAll covers Case B from issue #275 F-2.
// GitHub's spec has a `repos_notifications_activity-list-repo-for-authenticated-user`
// endpoint that takes an `all` param and is paginated. The endpoint template emits
// `var flagAll` once for the user-defined `all` param and again for pagination's
// "fetch all pages" flag — illegal redeclaration.
func TestGenerateRenamesParamCollidingWithPaginationAll(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("collide-all")
	apiSpec.Resources["notifications"] = spec.Resource{
		Description: "Notifications",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/notifications",
				Description: "List notifications",
				Params: []spec.Param{
					{Name: "all", Type: "bool", Description: "Include read notifications"},
				},
				Pagination: &spec.Pagination{
					Type:           "page_token",
					LimitParam:     "per_page",
					CursorParam:    "page",
					NextCursorPath: "next",
					HasMoreField:   "has_more",
				},
			},
			"get": {
				Method:      "GET",
				Path:        "/notifications/{id}",
				Description: "Get one notification",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "collide-all-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	flagVars, flagBindings := parseFlagDeclarations(t,
		filepath.Join(outputDir, "internal", "cli", "notifications_list.go"))

	assertNoDuplicates(t, flagVars,
		"pagination's reserved flagAll must not collide with a user param named 'all'")
	assertNoDuplicates(t, flagBindings,
		"--all from pagination must not collide with --all from a user param")
	assert.Contains(t, flagVars, "flagAll",
		"pagination's flagAll keeps the canonical name")
}

// TestGenerateRenamesParamCollidingWithAsyncWait covers the async-reserved-name
// path. Async-job endpoints emit `var flagWait`, `var flagWaitTimeout`, and
// `var flagWaitInterval` from the IsAsync branch in command_endpoint.go.tmpl;
// a user param literally named `wait` (or `wait_timeout`, `wait_interval`)
// would otherwise produce a duplicate `var flagWait` in the same function.
//
// Async detection requires a job-id-shaped response field plus a sibling status
// endpoint, so the spec mirrors that contract.
func TestGenerateRenamesParamCollidingWithAsyncWait(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("collide-async")
	apiSpec.Types = map[string]spec.TypeDef{
		"JobResp": {Fields: []spec.TypeField{
			{Name: "job_id", Type: "string"},
			{Name: "status", Type: "string"},
		}},
	}
	apiSpec.Resources["videos"] = spec.Resource{
		Description: "Videos",
		Endpoints: map[string]spec.Endpoint{
			"create": {
				Method:      "POST",
				Path:        "/videos",
				Description: "Create a video render job",
				Response:    spec.ResponseDef{Type: "object", Item: "JobResp"},
				Params: []spec.Param{
					{Name: "wait", Type: "string", Description: "Watermark text on the rendered video"},
				},
			},
			"get": {
				Method:      "GET",
				Path:        "/videos/{id}",
				Description: "Get one video",
				Response:    spec.ResponseDef{Type: "object", Item: "JobResp"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "collide-async-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	flagVars, flagBindings := parseFlagDeclarations(t,
		filepath.Join(outputDir, "internal", "cli", "videos_create.go"))

	assertNoDuplicates(t, flagVars,
		"async's reserved flagWait must not collide with a user param named 'wait'")
	assertNoDuplicates(t, flagBindings,
		"--wait from async must not collide with --wait from a user param")
	assert.Contains(t, flagVars, "flagWait",
		"async's flagWait keeps the canonical name")
}

// parseFlagDeclarations returns the names of all `var flagXxx` declarations and
// the literal flag names passed to cobra's *Var registrations.
func parseFlagDeclarations(t *testing.T, path string) (vars, bindings []string) {
	t.Helper()
	src, err := os.ReadFile(path)
	require.NoError(t, err, "read generated file")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	require.NoError(t, err, "generated file must parse as Go")

	ast.Inspect(file, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.GenDecl:
			if decl.Tok != token.VAR {
				return true
			}
			for _, sp := range decl.Specs {
				vs, ok := sp.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					if strings.HasPrefix(name.Name, "flag") {
						vars = append(vars, name.Name)
					}
				}
			}
		case *ast.CallExpr:
			// cobra registrations: cmd.Flags().StringVar(&flagX, "name", ...)
			sel, ok := decl.Fun.(*ast.SelectorExpr)
			if !ok || !strings.HasSuffix(sel.Sel.Name, "Var") {
				return true
			}
			if len(decl.Args) < 2 {
				return true
			}
			lit, ok := decl.Args[1].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			bindings = append(bindings, strings.Trim(lit.Value, `"`))
		}
		return true
	})
	return vars, bindings
}

func assertNoDuplicates(t *testing.T, names []string, msg string) {
	t.Helper()
	seen := map[string]int{}
	for _, n := range names {
		seen[n]++
	}
	for n, count := range seen {
		assert.Equal(t, 1, count, "%s: %q appears %d times", msg, n, count)
	}
}
