package generator

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGenerateNestedObjectBodyEmitsFieldFlags is the end-to-end check
// for issue #942: when a body Param declares Type "object" with non-empty
// Fields, the generated endpoint command must expose one cobra flag per
// leaf (parent-prefixed so siblings sharing a field name do not collide)
// and build the wire-side body as a nested map[string]any rather than a
// JSON-string-only flag. Without this fix, Microsoft Graph and similar
// APIs that wrap dateTime/timeZone (or address/line1/line2/...) under a
// single object property require users to hand-write JSON.
func TestGenerateNestedObjectBodyEmitsFieldFlags(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("nested-body")
	apiSpec.Resources["events"] = spec.Resource{
		Description: "Calendar events",
		Endpoints: map[string]spec.Endpoint{
			"create": {
				Method:      "POST",
				Path:        "/events",
				Description: "Create a calendar event",
				Body: []spec.Param{
					{Name: "subject", Type: "string", Description: "Event title", Required: true},
					{
						Name:        "start",
						Type:        "object",
						Description: "Start of window",
						Fields: []spec.Param{
							{Name: "dateTime", Type: "string", Description: "RFC3339 timestamp", Required: true},
							{Name: "timeZone", Type: "string", Description: "IANA zone"},
						},
					},
					{
						Name: "end",
						Type: "object",
						Fields: []spec.Param{
							{Name: "dateTime", Type: "string", Description: "RFC3339 timestamp"},
							{Name: "timeZone", Type: "string", Description: "IANA zone"},
						},
					},
				},
			},
			"get": {
				Method:      "GET",
				Path:        "/events/{id}",
				Description: "Get one event",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "nested-body-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "events_create.go"))
	require.NoError(t, err)
	got := string(src)

	// Vars: parent-prefixed leaf decls only; no parent var for object-with-Fields.
	for _, want := range []string{
		"var bodyStartDateTime string",
		"var bodyStartTimeZone string",
		"var bodyEndDateTime string",
		"var bodyEndTimeZone string",
		"var bodySubject string",
	} {
		require.Containsf(t, got, want, "expected leaf var %q in generated file", want)
	}
	require.NotContainsf(t, got, "var bodyStart string", "parent var must not appear when Fields populated")
	require.NotContainsf(t, got, "var bodyEnd string", "parent var must not appear when Fields populated")

	// Flag registrations: parent-prefixed flag names so start.dateTime and
	// end.dateTime do not collide on a single --date-time flag.
	for _, want := range []string{
		`cmd.Flags().StringVar(&bodyStartDateTime, "start-date-time"`,
		`cmd.Flags().StringVar(&bodyStartTimeZone, "start-time-zone"`,
		`cmd.Flags().StringVar(&bodyEndDateTime, "end-date-time"`,
		`cmd.Flags().StringVar(&bodyEndTimeZone, "end-time-zone"`,
		`cmd.Flags().StringVar(&bodySubject, "subject"`,
	} {
		require.Containsf(t, got, want, "expected flag registration %q", want)
	}

	// Required-flag validation: the nested requirement applies only when the
	// optional start object is populated, and the parent-prefixed flag in the
	// error message matches the registered flag name.
	require.Contains(t, got, `if bodyStartDateTime != "" || bodyStartTimeZone != "" {`)
	require.Contains(t, got, `cmd.Flags().Changed("start-date-time")`, "required check must use parent-prefixed flag")
	require.Contains(t, got, `"required flag \"%s\" not set", "start-date-time"`)

	// Body construction: nested map literal that only sets the parent key
	// when at least one child field was provided.
	for _, want := range []string{
		"nestedStart := map[string]any{}",
		`nestedStart["dateTime"] = bodyStartDateTime`,
		`nestedStart["timeZone"] = bodyStartTimeZone`,
		`if len(nestedStart) > 0 {`,
		`bodyMap["start"] = nestedStart`,
		"nestedEnd := map[string]any{}",
		`bodyMap["end"] = nestedEnd`,
	} {
		require.Containsf(t, got, want, "expected nested-map fragment %q", want)
	}

	// Make sure the generated file still parses as Go (catches whitespace
	// or scope mistakes in template wiring that the snippet matches above
	// would otherwise miss).
	if !strings.Contains(got, "package cli") {
		t.Fatalf("generated file missing 'package cli' header")
	}
	fset := token.NewFileSet()
	_, parseErr := parser.ParseFile(fset, "events_create.go", got, parser.AllErrors)
	require.NoError(t, parseErr, "generated file with nested-object body must parse as Go")

	mcpGot := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, mcpGot, `mcplib.WithString("start-date-time", mcplib.Description("RFC3339 timestamp"))`)
	require.NotContains(t, mcpGot, `mcplib.WithString("start-date-time", mcplib.Required()`)

	runtimeTest := `package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func runNestedRequiredCommand(t *testing.T, args ...string) error {
	t.Helper()
	var flags rootFlags
	cmd := newRootCmd(&flags)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestOptionalNestedRequiredRuntime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(server.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MYAPI_TOKEN", "test-token")
	t.Setenv("NESTED_BODY_BASE_URL", server.URL)

	if err := runNestedRequiredCommand(t, "events", "create", "--subject", "Planning"); err != nil {
		t.Fatalf("optional parent omitted: %v", err)
	}
	err := runNestedRequiredCommand(t, "events", "create", "--subject", "Planning", "--start-time-zone", "UTC")
	if err == nil || !strings.Contains(err.Error(), "required flag \"start-date-time\" not set") {
		t.Fatalf("partially populated parent error = %v", err)
	}
	if err := runNestedRequiredCommand(t, "events", "create", "--subject", "Planning", "--start-time-zone", "UTC", "--start-date-time", "2026-07-13T12:00:00Z"); err != nil {
		t.Fatalf("complete optional parent: %v", err)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "nested_required_runtime_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestOptionalNestedRequiredRuntime", "-count=1")
	requireGeneratedCompiles(t, outputDir)

	promotedSpec := apiSpec
	delete(promotedSpec.Resources["events"].Endpoints, "get")
	promotedOutputDir := filepath.Join(t.TempDir(), "nested-body-promoted-pp-cli")
	require.NoError(t, New(promotedSpec, promotedOutputDir).Generate())
	promoted := readGeneratedFile(t, promotedOutputDir, "internal", "cli", "promoted_events.go")
	require.Contains(t, promoted, `if bodyStartDateTime != "" || bodyStartTimeZone != "" {`)
	require.Contains(t, promoted, `if !cmd.Flags().Changed("start-date-time") && !flags.dryRun {`)
	requireGeneratedCompiles(t, promotedOutputDir)
}

func TestGenerateInternalYAMLBodyObjectSchema(t *testing.T) {
	t.Parallel()

	apiSpec, err := spec.ParseBytes([]byte(`
name: rich-body
base_url: https://api.example.com
auth:
  type: none
resources:
  graphql:
    endpoints:
      execute:
        method: POST
        path: /graphql
        body:
          properties:
            query:
              type: string
              required: true
              description: GraphQL document
            variables:
              type: object
              description: GraphQL variables
            serializerSettings:
              type: object
              properties:
                includeNulls:
                  type: bool
            queries:
              type: array
              items:
                type: object
                properties:
                  query:
                    type: string
`))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "rich-body-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	var got string
	err = filepath.Walk(filepath.Join(outputDir, "internal", "cli"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || filepath.Ext(path) != ".go" {
			return err
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(src), "bodySerializerSettingsIncludeNulls") {
			got = string(src)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, got)

	for _, want := range []string{
		`cmd.Flags().StringVar(&bodyQuery, "query"`,
		`cmd.Flags().StringVar(&bodyVariables, "variables"`,
		`cmd.Flags().BoolVar(&bodySerializerSettingsIncludeNulls, "serializer-settings-include-nulls"`,
		`cmd.Flags().StringVar(&bodyQueries, "queries"`,
		`json.Unmarshal([]byte(bodyVariables), &parsedVariables)`,
		`json.Unmarshal([]byte(bodyQueries), &parsedQueries)`,
		`nestedSerializerSettings["includeNulls"] = bodySerializerSettingsIncludeNulls`,
		`bodyMap["serializerSettings"] = nestedSerializerSettings`,
	} {
		require.Containsf(t, got, want, "expected generated fragment %q", want)
	}
	require.Contains(t, got, `cmd.Flags().Changed("serializer-settings-include-nulls")`)
	require.Contains(t, got, `cmd.Flags().Changed("query")`)

	fset := token.NewFileSet()
	_, parseErr := parser.ParseFile(fset, "graphql_execute.go", got, parser.AllErrors)
	require.NoError(t, parseErr, "generated file from internal YAML object body schema must parse as Go")

	mcpSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	mcpGot := string(mcpSrc)
	for _, want := range []string{
		`mcplib.WithString("query", mcplib.Required(), mcplib.Description("GraphQL document"))`,
		`mcplib.WithObject("variables", mcplib.Description("GraphQL variables"))`,
		`mcplib.WithBoolean("serializer-settings-include-nulls"`,
		`mcplib.WithArray("queries"`,
		`PublicName: "serializer-settings-include-nulls", WireName: "includeNulls", Location: "body", BodyPath: []string{"serializerSettings", "includeNulls"}`,
		`setNestedBodyArg(bodyArgs, binding.BodyPath, v)`,
	} {
		require.Containsf(t, mcpGot, want, "expected generated MCP fragment %q", want)
	}
	require.NotContains(t, mcpGot, `mcplib.WithString("queries-query"`)

	// formatMCPParamValue must JSON-encode a native composite in its default
	// branch (a query/path-located array/object param now arrives as []any /
	// map[string]any instead of a string); Go's "%v" would emit "[a b c]".
	require.Regexp(t, `(?s)func formatMCPParamValue\(v any\) string \{.*?json\.Marshal\(v\)`, mcpGot)
}
