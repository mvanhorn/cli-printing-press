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
	payload, err := json.Marshal([]map[string]any{{"population": 3483757.0}})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var out bytes.Buffer
	if err := printCSV(&out, payload); err != nil {
		t.Fatalf("printCSV() error = %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "3483757") {
		t.Fatalf("expected decimal float rendering, got %s", got)
	}
	if strings.Contains(got, "e+") || strings.Contains(got, "E+") {
		t.Fatalf("expected no scientific notation, got %s", got)
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestPrintCSVFloat64AvoidsScientificNotation", "-count=1")
}
