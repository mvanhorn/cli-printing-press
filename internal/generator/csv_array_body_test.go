package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGenerateCSVArrayBodyFields(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("csv-array-body")
	apiSpec.Resources["messages"] = spec.Resource{
		Description: "Messages",
		Endpoints: map[string]spec.Endpoint{
			"send": {
				Method:      "POST",
				Path:        "/messages",
				Description: "Send a message",
				Body: []spec.Param{
					{
						Name:        "emails",
						Type:        "string_csv_array",
						ItemType:    "string",
						Description: "Recipient email addresses",
					},
					{
						Name:        "attendees",
						Type:        "string_csv_array",
						ItemType:    "object",
						Description: "Attendee email addresses",
						ItemTemplate: map[string]any{
							"emailAddress": map[string]any{"address": "$value"},
							"type":         "required",
						},
					},
					{
						Name:        "subject",
						Type:        "string",
						Description: "Message subject",
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "csv-array-body-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	code := readGeneratedCLIFileContaining(t, outputDir, `cliutil.SplitCSV(bodyEmails)`)

	require.Contains(t, code, `"csv-array-body-pp-cli/internal/cliutil"`)
	require.Contains(t, code, `body["emails"] = cliutil.SplitCSV(bodyEmails)`)
	require.Contains(t, code, `body["attendees"] = cliutil.CSVTemplateObjects(bodyAttendees, map[string]any{"emailAddress": map[string]any{"address": "$value"}, "type": "required"})`)
	require.Contains(t, code, `body["subject"] = bodySubject`)

	helper, err := os.ReadFile(filepath.Join(outputDir, "internal", "cliutil", "csv.go"))
	require.NoError(t, err)
	require.Contains(t, string(helper), `func SplitCSV(input string) []string`)
	require.Contains(t, string(helper), `func CSVTemplateObjects(input string, template map[string]any) []map[string]any`)

	runGoCommand(t, outputDir, "build", "./cmd/csv-array-body-pp-cli")
}

func TestEndpointUsesCSVArrayRespectsBodyFlagDepth(t *testing.T) {
	t.Parallel()

	deepCSV := spec.Param{Name: "csv", Type: "string_csv_array", ItemType: "string"}
	for i := 4; i >= 0; i-- {
		deepCSV = spec.Param{Name: "level", Type: "object", Fields: []spec.Param{deepCSV}}
	}

	require.False(t, endpointUsesCSVArray(spec.Endpoint{Method: "POST", Body: []spec.Param{deepCSV}}))
	boundaryCSV := spec.Param{Name: "csv", Type: "string_csv_array", ItemType: "string"}
	for i := 2; i >= 0; i-- {
		boundaryCSV = spec.Param{Name: "level", Type: "object", Fields: []spec.Param{boundaryCSV}}
	}
	require.False(t, endpointUsesCSVArray(spec.Endpoint{Method: "POST", Body: []spec.Param{boundaryCSV}}))
	require.True(t, endpointUsesCSVArray(spec.Endpoint{Method: "POST", Body: []spec.Param{{
		Name:   "level",
		Type:   "object",
		Fields: []spec.Param{{Name: "csv", Type: "string_csv_array", ItemType: "string"}},
	}}}))
}

func readGeneratedCLIFileContaining(t *testing.T, outputDir, needle string) string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(outputDir, "internal", "cli", "*.go"))
	require.NoError(t, err)
	for _, match := range matches {
		src, err := os.ReadFile(match)
		require.NoError(t, err)
		if strings.Contains(string(src), needle) {
			return string(src)
		}
	}
	t.Fatalf("no generated CLI file contains %q", needle)
	return ""
}
