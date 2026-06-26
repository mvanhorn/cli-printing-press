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
						Name:        "tags",
						Type:        "array",
						ItemType:    "string",
						Description: "Message tags",
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

	code := readGeneratedCLIFileContaining(t, outputDir, `cliutil.ParseStringList(bodyEmails)`)

	require.Contains(t, code, `"csv-array-body-pp-cli/internal/cliutil"`)
	require.Contains(t, code, `parsedEmails, parseErr := cliutil.ParseStringList(bodyEmails)`)
	require.Contains(t, code, `return fmt.Errorf("parsing --emails list: %w", parseErr)`)
	require.Contains(t, code, `body["emails"] = parsedEmails`)
	require.Contains(t, code, `parsedTags, parseErr := cliutil.ParseStringList(bodyTags)`)
	require.Contains(t, code, `body["tags"] = parsedTags`)
	require.Contains(t, code, `body["attendees"] = cliutil.CSVTemplateObjects(bodyAttendees, map[string]any{"emailAddress": map[string]any{"address": "$value"}, "type": "required"})`)
	require.Contains(t, code, `body["subject"] = bodySubject`)

	helper, err := os.ReadFile(filepath.Join(outputDir, "internal", "cliutil", "csv.go"))
	require.NoError(t, err)
	require.Contains(t, string(helper), `func SplitCSV(input string) []string`)
	require.Contains(t, string(helper), `func ParseStringList(input string) ([]string, error)`)
	require.Contains(t, string(helper), `func CSVTemplateObjects(input string, template map[string]any) []map[string]any`)

	runGoCommand(t, outputDir, "build", "./cmd/csv-array-body-pp-cli")
}

func TestGeneratedParseStringListAcceptsCSVOrJSONArray(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("csv-array-runtime")
	apiSpec.Resources["messages"] = spec.Resource{
		Description: "Messages",
		Endpoints: map[string]spec.Endpoint{
			"send": {
				Method:      "POST",
				Path:        "/messages",
				Description: "Send a message",
				Body: []spec.Param{
					{Name: "emails", Type: "string_csv_array", ItemType: "string"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "csv-array-runtime-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	testSrc := []byte(`package cliutil

import (
	"strings"
	"testing"
)

func TestParseStringListCSVOrJSON(t *testing.T) {
	csv, err := ParseStringList("ada@example.com, grace@example.com")
	if err != nil {
		t.Fatalf("CSV parse failed: %v", err)
	}
	if len(csv) != 2 || csv[0] != "ada@example.com" || csv[1] != "grace@example.com" {
		t.Fatalf("unexpected CSV parse: %#v", csv)
	}

	jsonList, err := ParseStringList(` + "`" + `["ada@example.com","grace@example.com"]` + "`" + `)
	if err != nil {
		t.Fatalf("JSON parse failed: %v", err)
	}
	if len(jsonList) != 2 || jsonList[0] != "ada@example.com" || jsonList[1] != "grace@example.com" {
		t.Fatalf("unexpected JSON parse: %#v", jsonList)
	}

		if _, err := ParseStringList(` + "`" + `[{"email":"ada@example.com"}]` + "`" + `); err == nil {
			t.Fatal("object arrays must not be accepted as string lists")
		} else if !strings.Contains(err.Error(), "expected JSON array of strings or comma-separated list") {
			t.Fatalf("unexpected object-array error: %v", err)
		}
	}
	`)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cliutil", "csv_extra_test.go"), testSrc, 0o600))
	runGoCommand(t, outputDir, "test", "./internal/cliutil/...")
}

func TestGenerateCSVResponseParseHelpers(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("csv-response")
	apiSpec.Resources["reports"] = spec.Resource{
		Description: "Reports",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:         "GET",
				Path:           "/reports",
				Description:    "List reports",
				ResponseFormat: spec.ResponseFormatCSV,
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "csv-response-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helper, err := os.ReadFile(filepath.Join(outputDir, "internal", "cliutil", "csv_parse.go"))
	require.NoError(t, err)
	require.Contains(t, string(helper), `func ParseCSV(raw json.RawMessage) []map[string]any`)
	require.Contains(t, string(helper), `func ParseCSVFirstRow(raw json.RawMessage) map[string]any`)

	testSrc := []byte(`package cliutil

import (
	"encoding/json"
	"testing"
)

func TestParseCSVResponse(t *testing.T) {
	rows := ParseCSV(json.RawMessage("Domain;Rank\nexample.com;1\n"))
	if len(rows) != 1 || rows[0]["Domain"] != "example.com" || rows[0]["Rank"] != "1" {
		t.Fatalf("unexpected rows: %#v", rows)
	}
	quotedHeader := ParseCSV(json.RawMessage("\"Revenue,Total\";Quarter\n100;Q1\n"))
	if len(quotedHeader) != 1 || quotedHeader[0]["Revenue,Total"] != "100" || quotedHeader[0]["Quarter"] != "Q1" {
		t.Fatalf("unexpected quoted-header rows: %#v", quotedHeader)
	}
	if headerOnly := ParseCSV(json.RawMessage("Name,Score\n")); headerOnly != nil {
		t.Fatalf("expected nil header-only CSV, got %#v", headerOnly)
	}
	first := ParseCSVFirstRow(json.RawMessage("\"Name,Score\\nAda,42\\n\""))
	if first["Name"] != "Ada" || first["Score"] != "42" {
		t.Fatalf("unexpected first row: %#v", first)
	}
}
`)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cliutil", "csv_parse_extra_test.go"), testSrc, 0o600))
	runGoCommand(t, outputDir, "test", "./internal/cliutil/...")
}

func TestGenerateJSONOnlyOmitsCSVResponseParseHelpers(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("json-only")
	outputDir := filepath.Join(t.TempDir(), "json-only-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	_, err := os.Stat(filepath.Join(outputDir, "internal", "cliutil", "csv_parse.go"))
	require.True(t, os.IsNotExist(err), "JSON-only CLIs should not emit csv_parse.go")
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
