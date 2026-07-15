package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedHelpersHonorPlainAndHumanFriendlyFlags(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("output-flags")
	outputDir := filepath.Join(t.TempDir(), "output-flags-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	testPath := filepath.Join(outputDir, "internal", "cli", "output_flags_runtime_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPrintOutputWithFlagsPlainRendersTSV(t *testing.T) {
	data := json.RawMessage("[{\"id\":\"one\",\"name\":\"Alpha\"},{\"id\":\"two\",\"name\":\"Beta\"}]")
	var out bytes.Buffer

	if err := printOutputWithFlags(&out, data, &rootFlags{plain: true}); err != nil {
		t.Fatalf("printOutputWithFlags returned error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "id\tname\n") || !strings.Contains(got, "one\tAlpha\n") || !strings.Contains(got, "two\tBeta\n") {
		t.Fatalf("--plain should render tab-separated rows, got %q", got)
	}
	if strings.Contains(got, "{") || strings.Contains(got, "[") {
		t.Fatalf("--plain should not fall back to JSON for arrays, got %q", got)
	}
}

func TestPrintOutputWithFlagsPlainEmptyArrayIsEmpty(t *testing.T) {
	data := json.RawMessage("[]")
	var out bytes.Buffer

	if err := printOutputWithFlags(&out, data, &rootFlags{plain: true}); err != nil {
		t.Fatalf("printOutputWithFlags returned error: %v", err)
	}
	if got := out.String(); got != "" {
		t.Fatalf("--plain should render empty arrays as an empty stream, got %q", got)
	}
}

func TestHumanFriendlyForcesTableAndNoColorStripsANSI(t *testing.T) {
	oldHumanFriendly, oldNoColor := humanFriendly, noColor
	humanFriendly, noColor = true, false
	t.Cleanup(func() {
		humanFriendly, noColor = oldHumanFriendly, oldNoColor
	})
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")

	if !wantsHumanTable(&bytes.Buffer{}, &rootFlags{}) {
		t.Fatalf("--human-friendly should force human table rendering even when stdout is not a terminal")
	}
	if !colorEnabled() {
		t.Fatalf("--human-friendly should enable color when --no-color/NO_COLOR/TERM=dumb are absent")
	}

	rows := []map[string]any{{"id": "one", "name": "Alpha"}}
	var colored bytes.Buffer
	if err := printAutoTable(&colored, rows); err != nil {
		t.Fatalf("printAutoTable returned error: %v", err)
	}
	if !strings.Contains(colored.String(), "\x1b[1m") {
		t.Fatalf("--human-friendly should enable ANSI table styling, got %q", colored.String())
	}

	noColor = true
	var plain bytes.Buffer
	if err := printAutoTable(&plain, rows); err != nil {
		t.Fatalf("printAutoTable returned error: %v", err)
	}
	if strings.Contains(plain.String(), "\x1b[") {
		t.Fatalf("--no-color should strip ANSI styling, got %q", plain.String())
	}
}

func TestTerminalControlCharactersAreScrubbedFromHumanOutput(t *testing.T) {
	rows := []map[string]any{{
		"id":          "one",
		"name\x1b[31m": "Alpha\x1b[0m\u009b31m",
	}}

	var table bytes.Buffer
	if err := printAutoTable(&table, rows); err != nil {
		t.Fatalf("printAutoTable returned error: %v", err)
	}
	if strings.ContainsAny(table.String(), "\x1b\u009b") {
		t.Fatalf("table output retained terminal controls: %q", table.String())
	}
	if !strings.Contains(table.String(), "Alpha[0m31m") {
		t.Fatalf("table output should preserve printable text, got %q", table.String())
	}

	cardRows := []map[string]any{{
		"name\x1b[31m": "Alpha\u009b31m",
		"flag":          true,
	}}
	var cards bytes.Buffer
	if err := printAutoCards(&cards, cardRows); err != nil {
		t.Fatalf("printAutoCards returned error: %v", err)
	}
	remainingFieldRows := []map[string]any{{
		"id":              "one",
		"status":          "active",
		"details\x1b[31m": []any{"Beta\x1b[0m"},
	}}
	if err := printAutoCards(&cards, remainingFieldRows); err != nil {
		t.Fatalf("printAutoCards returned error: %v", err)
	}
	if strings.ContainsAny(cards.String(), "\x1b\u009b") {
		t.Fatalf("card output retained terminal controls: %q", cards.String())
	}
	if !strings.Contains(cards.String(), "NAME[31M Alpha31m") || !strings.Contains(cards.String(), "details[31m:") || !strings.Contains(cards.String(), "Beta[0m") {
		t.Fatalf("card output should scrub title, remaining header, and array values while preserving printable text, got %q", cards.String())
	}
}

func TestTerminalControlCharactersRemainInJSONOutput(t *testing.T) {
	const rawValue = "Alpha\x1b[31m\u009b31m"
	data, err := json.Marshal([]map[string]any{{"name": rawValue}})
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}

	var out bytes.Buffer
	if err := printOutputWithFlags(&out, data, &rootFlags{asJSON: true}); err != nil {
		t.Fatalf("printOutputWithFlags returned error: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON output is invalid: %v", err)
	}
	if got := decoded[0]["name"]; got != rawValue {
		t.Fatalf("JSON value = %q, want byte-exact %q", got, rawValue)
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestPrintOutputWithFlagsPlainRendersTSV|TestPrintOutputWithFlagsPlainEmptyArrayIsEmpty|TestHumanFriendlyForcesTableAndNoColorStripsANSI|TestTerminalControl", "-count=1")
}

func TestLocalAnalysisTemplatesRouteMachineFormatsThroughSharedGate(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		filepath.Join("templates", "analytics.go.tmpl"),
		filepath.Join("templates", "workflows", "pm_load.go.tmpl"),
		filepath.Join("templates", "workflows", "pm_orphans.go.tmpl"),
		filepath.Join("templates", "workflows", "pm_stale.go.tmpl"),
	} {
		body, err := os.ReadFile(path)
		require.NoError(t, err, "template must exist: %s", path)
		src := string(body)

		require.Contains(t, src, "wantsMachineOutput(flags)",
			"%s must route --json/--csv/--quiet/--plain/--compact/--select through the shared output contract", path)
		require.NotContains(t, src, "if flags.asJSON {",
			"%s still branches only on --json, so other documented output flags can be bypassed", path)
		require.NotContains(t, src, "flags.asJSON || !isTerminal",
			"%s still lets piped auto-JSON override explicit machine format flags", path)
	}

	searchPath := filepath.Join("templates", "search.go.tmpl")
	body, err := os.ReadFile(searchPath)
	require.NoError(t, err, "template must exist: %s", searchPath)
	src := string(body)
	require.Contains(t, src, "!wantsHumanTable(cmd.OutOrStdout(), flags)",
		"search.go.tmpl must route explicit machine formats and default piped output through the shared output contract")
	require.Contains(t, src, "outputFlags := *flags",
		"search.go.tmpl must clear row-shaping flags after applying them before provenance wrapping")
	selectIdx := strings.Index(src, "data = filterFields(data, flags.selectFields)")
	wrapIdx := strings.Index(src, "wrapped, err := wrapWithProvenance(data, prov)")
	require.GreaterOrEqual(t, selectIdx, 0)
	require.GreaterOrEqual(t, wrapIdx, 0)
	require.Less(t, selectIdx, wrapIdx,
		"search.go.tmpl must apply --select to the result array before wrapping it in the provenance envelope")
	require.NotContains(t, src, "if flags.asJSON {",
		"search.go.tmpl still branches only on --json, so other documented output flags can be bypassed")
	require.NotContains(t, src, "flags.asJSON || !isTerminal",
		"search.go.tmpl still lets piped auto-JSON override explicit machine format flags")
}
