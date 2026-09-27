package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrintCSVFloat64AvoidsScientificNotation(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("csv-float")
	outputDir := filepath.Join(t.TempDir(), "csv-float-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	testPath := filepath.Join(outputDir, "internal", "cli", "print_csv_float_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
)

func TestPrintCSVFloat64AvoidsScientificNotation(t *testing.T) {
	// Large value (>= 1e6) previously rendered as 3.483757e+06; small value
	// (< 1e-4) previously rendered as 1e-05. Cover both exponent signs.
	payload, err := json.Marshal([]map[string]any{{"population": 3483757.0, "ratio": 0.00001}})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var out bytes.Buffer
	if err := printCSV(&out, payload); err != nil {
		t.Fatalf("printCSV() error = %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "3483757") {
		t.Fatalf("expected decimal float rendering of large value, got %s", got)
	}
	if !strings.Contains(got, "0.00001") {
		t.Fatalf("expected fixed-notation rendering of small value, got %s", got)
	}
	if strings.Contains(got, "e+") || strings.Contains(got, "E+") ||
		strings.Contains(got, "e-") || strings.Contains(got, "E-") {
		t.Fatalf("expected no scientific notation (neither exponent sign), got %s", got)
	}
}

func TestPrintCSVAndPlainNestedCellsAreJSON(t *testing.T) {
	payload, err := json.Marshal([]map[string]any{{
		"id":    "1",
		"score": 4.6,
		"properties": map[string]any{
			"stage": "lead",
			"n":     1790000000000.0,
			"tiny":  1e-7,
			"huge":  1e22,
			"label": "a<b",
		},
		"tags": []any{"a", "b"},
		"geometry": map[string]any{
			"type":        "Point",
			"coordinates": []any{-117.1, 35.2, 8.3},
		},
	}})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	wantProps := "{\"huge\":10000000000000000000000,\"label\":\"a<b\",\"n\":1790000000000,\"stage\":\"lead\",\"tiny\":0.0000001}"
	wantGeom := "{\"coordinates\":[-117.1,35.2,8.3],\"type\":\"Point\"}"
	wantTags := "[\"a\",\"b\"]"

	var csvOut bytes.Buffer
	if err := printCSV(&csvOut, payload); err != nil {
		t.Fatalf("printCSV() error = %v", err)
	}
	csvText := csvOut.String()
	if strings.Contains(csvText, "map[") || strings.Contains(csvText, "\\u003c") ||
		strings.Contains(csvText, "e+") || strings.Contains(csvText, "e-") ||
		strings.Contains(csvText, "E+") || strings.Contains(csvText, "E-") {
		t.Fatalf("csv nested cells are not machine JSON: %s", csvText)
	}
	records, err := csv.NewReader(strings.NewReader(csvText)).ReadAll()
	if err != nil {
		t.Fatalf("csv.ReadAll() error = %v\n%s", err, csvText)
	}
	if len(records) != 2 {
		t.Fatalf("csv records = %#v", records)
	}
	col := map[string]int{}
	for i, name := range records[0] {
		col[name] = i
	}
	row := records[1]
	if row[col["id"]] != "1" || row[col["score"]] != "4.6" {
		t.Fatalf("scalar cells changed: %#v", row)
	}
	if row[col["properties"]] != wantProps || row[col["geometry"]] != wantGeom || row[col["tags"]] != wantTags {
		t.Fatalf("nested cells = properties %q geometry %q tags %q", row[col["properties"]], row[col["geometry"]], row[col["tags"]])
	}
	for _, cell := range []string{row[col["properties"]], row[col["geometry"]], row[col["tags"]]} {
		if !json.Valid([]byte(cell)) {
			t.Fatalf("cell is not JSON: %s", cell)
		}
	}

	var plainOut bytes.Buffer
	if err := printPlain(&plainOut, payload); err != nil {
		t.Fatalf("printPlain() error = %v", err)
	}
	plainText := plainOut.String()
	if strings.Contains(plainText, "map[") || !strings.Contains(plainText, wantProps) || !strings.Contains(plainText, wantTags) {
		t.Fatalf("plain nested cells = %s", plainText)
	}
	plainLines := strings.Split(strings.TrimSuffix(plainText, "\n"), "\n")
	if len(plainLines) != 2 {
		t.Fatalf("plain lines = %#v", plainLines)
	}
	plainCols := strings.Split(plainLines[0], "\t")
	plainVals := strings.Split(plainLines[1], "\t")
	plainCol := map[string]int{}
	for i, name := range plainCols {
		plainCol[name] = i
	}
	if plainVals[plainCol["properties"]] != wantProps || plainVals[plainCol["tags"]] != wantTags || plainVals[plainCol["geometry"]] != wantGeom {
		t.Fatalf("plain cells = %#v", plainVals)
	}

	single := json.RawMessage("{\"id\":\"1\",\"properties\":{\"stage\":\"lead\",\"n\":1790000000000}}")
	var singleCSV bytes.Buffer
	if err := printCSV(&singleCSV, single); err != nil {
		t.Fatalf("printCSV(single) error = %v", err)
	}
	singleRecords, err := csv.NewReader(strings.NewReader(singleCSV.String())).ReadAll()
	if err != nil {
		t.Fatalf("single csv: %v\n%s", err, singleCSV.String())
	}
	if len(singleRecords) != 2 || len(singleRecords[0]) != 2 {
		t.Fatalf("single-object csv = %#v", singleRecords)
	}
	singleCol := map[string]int{}
	for i, name := range singleRecords[0] {
		singleCol[name] = i
	}
	if singleRecords[1][singleCol["id"]] != "1" || singleRecords[1][singleCol["properties"]] != "{\"n\":1790000000000,\"stage\":\"lead\"}" {
		t.Fatalf("single-object csv = %#v", singleRecords)
	}
	var singlePlain bytes.Buffer
	if err := printPlain(&singlePlain, single); err != nil {
		t.Fatalf("printPlain(single) error = %v", err)
	}
	if !strings.Contains(singlePlain.String(), "{\"n\":1790000000000,\"stage\":\"lead\"}") {
		t.Fatalf("single-object plain = %s", singlePlain.String())
	}
}

func TestPrintCSVAndPlainScalarCellsStayPlain(t *testing.T) {
	payload, err := json.Marshal([]map[string]any{{
		"id":    "one",
		"name":  "Alpha, \"Beta\"",
		"n":     3483757.0,
		"ratio": 0.00001,
		"note":  "line1\nline2",
	}})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var csvOut bytes.Buffer
	if err := printCSV(&csvOut, payload); err != nil {
		t.Fatalf("printCSV() error = %v", err)
	}
	wantCSV := "id,n,name,note,ratio\n" + "one,3483757,\"Alpha, \"\"Beta\"\"\",\"line1\nline2\",0.00001\n"
	if csvOut.String() != wantCSV {
		t.Fatalf("scalar csv = %q\nwant %q", csvOut.String(), wantCSV)
	}

	var plainOut bytes.Buffer
	if err := printPlain(&plainOut, payload); err != nil {
		t.Fatalf("printPlain() error = %v", err)
	}
	wantPlain := "id\tn\tname\tnote\tratio\n" + "one\t3483757\tAlpha, \"Beta\"\tline1 line2\t0.00001\n"
	if plainOut.String() != wantPlain {
		t.Fatalf("scalar plain = %q\nwant %q", plainOut.String(), wantPlain)
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestPrintCSVFloat64AvoidsScientificNotation|TestPrintCSVAndPlainNestedCellsAreJSON|TestPrintCSVAndPlainScalarCellsStayPlain", "-count=1")
}
