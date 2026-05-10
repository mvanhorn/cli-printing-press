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

	// Required-flag validation: parent-prefixed flag in the error message
	// matches the registered flag name.
	require.Contains(t, got, `cmd.Flags().Changed("start-date-time")`, "required check must use parent-prefixed flag")
	require.Contains(t, got, `"required flag \"%s\" not set", "start-date-time"`)

	// Body construction: nested map literal that only sets the parent key
	// when at least one child field was provided.
	for _, want := range []string{
		"nestedStart := map[string]any{}",
		`nestedStart["dateTime"] = bodyStartDateTime`,
		`nestedStart["timeZone"] = bodyStartTimeZone`,
		`if len(nestedStart) > 0 {`,
		`body["start"] = nestedStart`,
		"nestedEnd := map[string]any{}",
		`body["end"] = nestedEnd`,
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
}
