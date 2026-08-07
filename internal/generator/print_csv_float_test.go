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
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestPrintCSVFloat64AvoidsScientificNotation", "-count=1")
}
