package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestEnumParamEmitsValidation ensures that params declared with enum constraints
// cause the generated command to (a) emit runtime validation that warns on
// unknown values and (b) include a "(one of: ...)" hint in the flag description.
// Regression guard for #205.
func TestEnumParamEmitsValidation(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("enum-test")
	// Two endpoints so sibling "search" renders to its own file rather than
	// getting consolidated into the promoted parent. The enum check fires in
	// either file path, but `widgets_search.go` is easier to assert against.
	apiSpec.Resources["widgets"] = spec.Resource{
		Description: "Widgets",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/widgets",
				Description: "List widgets",
			},
			"search": {
				Method:      "GET",
				Path:        "/widgets/search",
				Description: "Search widgets filtered by status",
				Params: []spec.Param{
					{
						Name:        "status",
						Type:        "string",
						Required:    false,
						Description: "Widget status",
						Enum:        []string{"active", "archived", "pending"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "enum-test-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "widgets_search.go"))
	require.NoError(t, err)
	code := string(src)

	// Flag description includes the enum hint.
	require.Contains(t, code, `(one of: active, archived, pending)`,
		"flag description must include enum values")

	// Runtime validation block emitted.
	require.Contains(t, code, `allowedStatus := []string{ "active", "archived", "pending" }`,
		"runtime validation must declare the allowed set")
	require.Contains(t, code, `warning: --%s %q not in allowed set %v`,
		"runtime validation must warn on unknown value")
}

// TestNonEnumParamDoesNotEmitValidation ensures the enum block is gated
// on the Enum slice being non-empty — plain params stay untouched.
func TestNonEnumParamDoesNotEmitValidation(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("no-enum")
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Items",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/items",
				Description: "List items",
			},
			"search": {
				Method:      "GET",
				Path:        "/items/search",
				Description: "Search items",
				Params: []spec.Param{
					{Name: "query", Type: "string", Required: false, Description: "Search query"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "no-enum-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "items_search.go"))
	require.NoError(t, err)
	code := string(src)

	require.NotContains(t, code, `allowedQuery`,
		"params without Enum must not emit validation code")
	require.NotContains(t, code, `(one of:`,
		"params without Enum must not get a description hint")
}

// TestMultipleEnumParamsDoNotCollide ensures two enum-constrained flags on
// the same command produce distinct local variables (`allowedStatus`,
// `allowedKind`) — not a shared `allowed` that would break when the second
// param's validation ran.
func TestMultipleEnumParamsDoNotCollide(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("multi-enum")
	apiSpec.Resources["widgets"] = spec.Resource{
		Description: "Widgets",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method: "GET", Path: "/widgets", Description: "List",
			},
			"search": {
				Method:      "GET",
				Path:        "/widgets/search",
				Description: "Search with multiple enum filters",
				Params: []spec.Param{
					{Name: "status", Type: "string", Description: "status",
						Enum: []string{"active", "archived"}},
					{Name: "kind", Type: "string", Description: "kind",
						Enum: []string{"alpha", "beta"}},
				},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), "multi-enum-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "widgets_search.go"))
	require.NoError(t, err)
	code := string(src)

	// Each enum param gets its own uniquely-named locals.
	require.Contains(t, code, `allowedStatus := []string{ "active", "archived" }`)
	require.Contains(t, code, `allowedKind := []string{ "alpha", "beta" }`)
	require.Contains(t, code, `validStatus := false`)
	require.Contains(t, code, `validKind := false`)
}

// TestIntEnumParamSkipped documents the current scope: only string-typed
// enum params get runtime validation. Int-typed enum params (common in
// OpenAPI for HTTP status filters, severity levels) get the "(one of: ...)"
// description hint but no runtime comparison — the generated flag uses
// IntVar and the validation template compares against a []string, so
// emitting the check for int would be a type mismatch. Regression guard
// that this split-behavior is a conscious choice.
func TestIntEnumParamSkipped(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("int-enum")
	apiSpec.Resources["events"] = spec.Resource{
		Description: "Events",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method: "GET", Path: "/events", Description: "List",
			},
			"search": {
				Method:      "GET",
				Path:        "/events/search",
				Description: "Filter by severity",
				Params: []spec.Param{
					{Name: "severity", Type: "int", Description: "0=info,1=warn,2=error",
						Enum: []string{"0", "1", "2"}},
				},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), "int-enum-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "events_search.go"))
	require.NoError(t, err)
	code := string(src)

	require.NotContains(t, code, `allowedSeverity`,
		"int-typed enum params skip runtime validation (template guard excludes non-string types)")
	// The description hint IS emitted for int enums — users still see allowed
	// values in --help even though runtime validation is skipped.
	require.Contains(t, code, `(one of: 0, 1, 2)`,
		"int-typed enum params still get the description hint so users see allowed values")
}

// TestPositionalEnumParamSkipped documents the current scope: enum
// validation fires on flags, not positional args. A positional like
// `<status>` with enum values won't get runtime checking. This is a
// known gap (see PR #208 discussion); the test pins the behavior so
// future changes are deliberate.
func TestPositionalEnumParamSkipped(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("positional-enum")
	apiSpec.Resources["widgets"] = spec.Resource{
		Description: "Widgets",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method: "GET", Path: "/widgets", Description: "List",
			},
			"action": {
				Method:      "POST",
				Path:        "/widgets/{action}",
				Description: "Perform action",
				Params: []spec.Param{
					{Name: "action", Type: "string", Required: true, Positional: true,
						Description: "action", Enum: []string{"start", "stop", "restart"}},
				},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), "positional-enum-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "widgets_action.go"))
	require.NoError(t, err)
	code := string(src)

	require.NotContains(t, code, `allowedAction`,
		"positional enum params are currently skipped (positionals aren't cobra flags)")
}
