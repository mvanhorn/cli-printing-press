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
	requireGeneratedCompiles(t, outputDir)

	testPath := filepath.Join(outputDir, "internal", "cli", "output_flags_runtime_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestPrintOutputWithFlagsPlainEmptyArrayWritesMarker(t *testing.T) {
	data := json.RawMessage("[]")
	var out bytes.Buffer

	if err := printOutputWithFlags(&out, data, &rootFlags{plain: true}); err != nil {
		t.Fatalf("printOutputWithFlags returned error: %v", err)
	}
	if got, want := out.String(), "(no rows)\n"; got != want {
		t.Fatalf("--plain empty array without declared columns should write the empty-result marker, got %q want %q", got, want)
	}
}

func TestPrintOutputWithFlagsCSVEmptyArrayWritesMarker(t *testing.T) {
	data := json.RawMessage("[]")
	var out bytes.Buffer

	if err := printOutputWithFlags(&out, data, &rootFlags{csv: true}); err != nil {
		t.Fatalf("printOutputWithFlags returned error: %v", err)
	}
	if got, want := out.String(), "(no rows)\n"; got != want {
		t.Fatalf("--csv without declared fields should write the empty-result marker, got %q want %q", got, want)
	}
}

func TestPrintOutputWithFlagsCSVEmptyArrayWritesDeclaredHeader(t *testing.T) {
	data := json.RawMessage("[]")
	var out bytes.Buffer
	fields := map[string]bool{"id": true, "name": true}

	if err := printOutputWithFlagsMeta(&out, data, &rootFlags{csv: true}, nil, fields); err != nil {
		t.Fatalf("printOutputWithFlagsMeta returned error: %v", err)
	}
	if got, want := out.String(), "id,name\n"; got != want {
		t.Fatalf("empty --csv should write the declared header row, got %q want %q", got, want)
	}
}

func TestPrintOutputWithFlagsPlainEmptyArrayWritesDeclaredHeader(t *testing.T) {
	data := json.RawMessage("[]")
	var out bytes.Buffer
	fields := map[string]bool{"id": true, "name": true}

	if err := printOutputWithFlagsMeta(&out, data, &rootFlags{plain: true}, nil, fields); err != nil {
		t.Fatalf("printOutputWithFlagsMeta returned error: %v", err)
	}
	if got, want := out.String(), "id\tname\n"; got != want {
		t.Fatalf("empty --plain should write the declared tab-separated header, got %q want %q", got, want)
	}
}

func TestPrintOutputWithFlagsCSVEmptyArrayEscapesDeclaredHeader(t *testing.T) {
	data := json.RawMessage("[]")
	var out bytes.Buffer
	fields := map[string]bool{"id": true, "weird,name": true, "quote\"field": true, "cr\rfield": true}

	if err := printOutputWithFlagsMeta(&out, data, &rootFlags{csv: true}, nil, fields); err != nil {
		t.Fatalf("printOutputWithFlagsMeta returned error: %v", err)
	}
	if got, want := out.String(), "\"cr\rfield\",id,\"quote\"\"field\",\"weird,name\"\n"; got != want {
		t.Fatalf("empty --csv should CSV-escape declared header cells, got %q want %q", got, want)
	}
}

func TestPrintOutputWithFlagsCSVNonEmptyArrayUsesCSV(t *testing.T) {
	data := json.RawMessage("[{\"id\":\"one\",\"name\":\"Alpha\"}]")
	var out bytes.Buffer

	if err := printOutputWithFlags(&out, data, &rootFlags{csv: true}); err != nil {
		t.Fatalf("printOutputWithFlags returned error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "id,name\n") || !strings.Contains(got, "one,Alpha\n") {
		t.Fatalf("--csv should keep non-empty arrays in CSV form, got %q", got)
	}
	if strings.Contains(got, "[") || strings.Contains(got, "{") {
		t.Fatalf("--csv should not fall back to JSON for non-empty arrays, got %q", got)
	}
}

func TestPrintOutputWithFlagsCSVQuotesCarriageReturnInValues(t *testing.T) {
	data := json.RawMessage("[{\"id\":\"a\\rb\"}]")
	var out bytes.Buffer

	if err := printOutputWithFlags(&out, data, &rootFlags{csv: true}); err != nil {
		t.Fatalf("printOutputWithFlags returned error: %v", err)
	}
	if got, want := out.String(), "id\n\"a\rb\"\n"; got != want {
		t.Fatalf("non-empty --csv should quote values containing carriage returns, got %q want %q", got, want)
	}
}

func TestPrintOutputWithFlagsMachineEmptyArrayIsValidJSON(t *testing.T) {
	for _, flags := range []*rootFlags{
		{asJSON: true},
		{asJSON: true, agent: true},
	} {
		var out bytes.Buffer
		if err := printOutputWithFlags(&out, json.RawMessage("[]"), flags); err != nil {
			t.Fatalf("printOutputWithFlags returned error: %v", err)
		}
		if !json.Valid(out.Bytes()) {
			t.Fatalf("machine output should be valid JSON for empty arrays, got %q", out.String())
		}
	}
}

func TestPrintOutputWithFlagsCSVSingleObjectIsOneRow(t *testing.T) {
	data := json.RawMessage("{\"id\":\"one\"}")
	var out bytes.Buffer

	if err := printOutputWithFlags(&out, data, &rootFlags{csv: true}); err != nil {
		t.Fatalf("printOutputWithFlags returned error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "id\n") || !strings.Contains(got, "one\n") {
		t.Fatalf("--csv should render a single object as one CSV row, got %q", got)
	}
	if strings.Contains(got, "{") {
		t.Fatalf("--csv should not fall back to JSON for a single object, got %q", got)
	}
}

func TestPrintOutputWithFlagsCSVUnwrapsCollectionEnvelope(t *testing.T) {
	data := json.RawMessage("{\"results\":[{\"id\":\"one\",\"name\":\"Alpha\"}],\"meta\":{\"total\":1}}")
	var out bytes.Buffer
	if err := printOutputWithFlags(&out, data, &rootFlags{csv: true}); err != nil {
		t.Fatalf("printOutputWithFlags returned error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "id,name\n") && !strings.Contains(got, "name,id\n") {
		t.Fatalf("--csv should unwrap collection envelopes, got %q", got)
	}
	if strings.Contains(got, "results") || strings.Contains(got, "{") {
		t.Fatalf("--csv should not emit the JSON envelope, got %q", got)
	}
}

func TestPrintOutputWithFlagsQuietPrintsIdentityValues(t *testing.T) {
	data := json.RawMessage("[{\"id\":\"one\",\"name\":\"Alpha\"},{\"id\":\"two\",\"name\":\"Beta\"}]")
	var out bytes.Buffer
	if err := printOutputWithFlags(&out, data, &rootFlags{quiet: true}); err != nil {
		t.Fatalf("printOutputWithFlags returned error: %v", err)
	}
	got := out.String()
	if got != "one\ntwo\n" {
		t.Fatalf("--quiet should print one id per line, got %q", got)
	}
}

func TestPrintOutputWithFlagsCompactReducesDocumentedLists(t *testing.T) {
	data := json.RawMessage("[{\"id\":\"s1\",\"name\":\"Shop\",\"description\":\"verbose\",\"revenue\":99,\"listings\":[1,2,3]}]")
	documented := map[string]bool{"id": true, "name": true, "description": true, "revenue": true, "listings": true}
	var out bytes.Buffer
	if err := printOutputWithFlagsMeta(&out, data, &rootFlags{asJSON: true, compact: true}, map[string]any{"source": "local"}, documented); err != nil {
		t.Fatalf("printOutputWithFlagsMeta returned error: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("compact output is not JSON: %v\n%s", err, out.String())
	}
	if len(rows) != 1 {
		t.Fatalf("compact rows = %#v", rows)
	}
	if rows[0]["id"] != "s1" || rows[0]["name"] != "Shop" {
		t.Fatalf("compact dropped identity fields: %#v", rows[0])
	}
	for _, key := range []string{"description", "revenue", "listings"} {
		if _, ok := rows[0][key]; ok {
			t.Fatalf("schema-aware --compact kept non-gravity %s: %#v", key, rows[0])
		}
	}
}

func TestPrintOutputWithFlagsQuietFallbackIsDeterministic(t *testing.T) {
	monthRows := json.RawMessage("[{\"Month\":4,\"MonthName\":\"April\",\"Year\":2026},{\"Month\":1,\"MonthName\":\"January\",\"Year\":2026}]")
	idRows := json.RawMessage("[{\"user_id\":\"u1\",\"org_id\":\"o1\"}]")
	oneID := json.RawMessage("[{\"count\":2,\"user_id\":\"u1\"}]")
	named := json.RawMessage("[{\"name\":\"Alpha\",\"user_id\":\"u1\"}]")

	var wantMonths, wantIDs, wantOne, wantNamed string
	for i := 0; i < 100; i++ {
		gotMonths := quietStdout(t, monthRows)
		gotIDs := quietStdout(t, idRows)
		gotOne := quietStdout(t, oneID)
		gotNamed := quietStdout(t, named)
		if i == 0 {
			wantMonths, wantIDs, wantOne, wantNamed = gotMonths, gotIDs, gotOne, gotNamed
			if wantMonths != "4\n1\n" {
				t.Fatalf("sorted fallback should print Month, got %q", wantMonths)
			}
			if wantIDs != "o1\n" {
				t.Fatalf("sorted _id fallback should print org_id, got %q", wantIDs)
			}
			if wantOne != "u1\n" {
				t.Fatalf("single _id key should print that value, got %q", wantOne)
			}
			if wantNamed != "Alpha\n" {
				t.Fatalf("name should still win over _id, got %q", wantNamed)
			}
			continue
		}
		if gotMonths != wantMonths || gotIDs != wantIDs || gotOne != wantOne || gotNamed != wantNamed {
			t.Fatalf("quiet fallback changed across runs: months %q ids %q one %q named %q", gotMonths, gotIDs, gotOne, gotNamed)
		}
	}
}

func quietStdout(t *testing.T, data json.RawMessage) string {
	t.Helper()
	var out bytes.Buffer
	if err := printOutputWithFlags(&out, data, &rootFlags{quiet: true}); err != nil {
		t.Fatalf("printOutputWithFlags returned error: %v", err)
	}
	return out.String()
}

func TestPrintOutputWithFlagsCompactPreservesSparseEnvelopeMetadata(t *testing.T) {
	rows := make([]map[string]any, 10)
	for i := range rows {
		rows[i] = map[string]any{"id": fmt.Sprintf("r%d", i), "yield": 1.5}
	}
	rows[0]["note"] = "fallback"
	rows[0]["warnings"] = []any{"low_confidence"}
	rows[0]["errors"] = []any{"bad"}
	rows[1]["fetch_failures"] = []any{"timeout"}
	rows[2]["hint"] = "rare"
	rows[3]["Warnings"] = []any{"pascal"}

	flags := &rootFlags{asJSON: true, compact: true}
	compacted := compactRows(t, rows, flags, nil)
	if _, ok := compacted["r0"]["note"]; ok {
		t.Fatalf("undeclared sparse note survived plain compaction: %#v", compacted["r0"])
	}
	if _, ok := compacted["r2"]["hint"]; ok {
		t.Fatalf("undeclared sparse hint survived plain compaction: %#v", compacted["r2"])
	}
	if compacted["r0"]["yield"] != 1.5 || compacted["r9"]["id"] != "r9" {
		t.Fatalf("frequent fields dropped: %#v", compacted)
	}
	assertJSONArrayField(t, compacted["r0"]["warnings"], "low_confidence")
	assertJSONArrayField(t, compacted["r0"]["errors"], "bad")
	assertJSONArrayField(t, compacted["r1"]["fetch_failures"], "timeout")
	assertJSONArrayField(t, compacted["r3"]["Warnings"], "pascal")

	kept := compactRowsKept(t, rows, flags, "note")
	if kept["r0"]["note"] != "fallback" {
		t.Fatalf("keep floor dropped note: %#v", kept["r0"])
	}
	if _, ok := kept["r2"]["hint"]; ok {
		t.Fatalf("keep floor retained an unlisted sparse field: %#v", kept["r2"])
	}
	assertJSONArrayField(t, kept["r0"]["warnings"], "low_confidence")

	env := map[string]any{
		"found":    true,
		"query":    "q",
		"warnings": []any{"candidates_present"},
		"results": []any{
			map[string]any{"resource_id": "r1", "action": "get", "confidence": 80, "match_score": 0.9, "source": "teach"},
			map[string]any{"resource_id": "r2", "action": "get", "confidence": 80, "match_score": 0.9, "source": "teach"},
			map[string]any{"resource_id": "r3", "action": "get", "confidence": 80, "match_score": 0.9, "source": "teach", "warnings": []any{"low_confidence"}},
			map[string]any{"resource_id": "r4", "action": "get", "confidence": 80, "match_score": 0.9, "source": "teach", "warnings": []any{"resource_not_in_store", "low_confidence"}},
			map[string]any{"resource_id": "r5", "action": "get", "confidence": 80, "match_score": 0.9, "source": "teach"},
		},
	}
	var out bytes.Buffer
	if err := printJSONFiltered(&out, env, flags); err != nil {
		t.Fatalf("printJSONFiltered envelope: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("envelope json: %v\n%s", err, out.String())
	}
	top, _ := got["warnings"].([]any)
	if len(top) != 1 || top[0] != "candidates_present" {
		t.Fatalf("top-level warnings = %#v", got["warnings"])
	}
	results, _ := got["results"].([]any)
	if len(results) != 5 {
		t.Fatalf("results = %#v", got["results"])
	}
	hits := map[string]map[string]any{}
	for _, raw := range results {
		hit, _ := raw.(map[string]any)
		id, _ := hit["resource_id"].(string)
		hits[id] = hit
	}
	if _, ok := hits["r1"]["warnings"]; ok {
		t.Fatalf("r1 gained warnings: %#v", hits["r1"])
	}
	assertJSONArrayField(t, hits["r3"]["warnings"], "low_confidence")
	warns, _ := hits["r4"]["warnings"].([]any)
	if len(warns) != 2 {
		t.Fatalf("r4 warnings = %#v", hits["r4"]["warnings"])
	}
}

func TestPrintOutputWithFlagsSchemaAwareCompactIgnoresEnvelopeFloor(t *testing.T) {
	rows := make([]map[string]any, 5)
	for i := range rows {
		rows[i] = map[string]any{
			"id":       fmt.Sprintf("r%d", i),
			"warnings": []any{"x"},
			"note":     "always",
		}
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	documented := map[string]bool{"id": true, "warnings": true, "note": true}
	var out bytes.Buffer
	if err := printOutputWithFlagsMeta(&out, raw, &rootFlags{asJSON: true, compact: true}, nil, documented); err != nil {
		t.Fatalf("printOutputWithFlagsMeta: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if len(got) != 5 {
		t.Fatalf("rows = %#v", got)
	}
	for _, row := range got {
		if row["id"] == nil {
			t.Fatalf("dropped id: %#v", row)
		}
		if _, ok := row["warnings"]; ok {
			t.Fatalf("schema-aware compact kept warnings: %#v", row)
		}
		if _, ok := row["note"]; ok {
			t.Fatalf("schema-aware compact kept note: %#v", row)
		}
	}
}

func TestPrintOutputWithFlagsSelectWinsOverCompact(t *testing.T) {
	rows := make([]map[string]any, 10)
	for i := range rows {
		rows[i] = map[string]any{"id": fmt.Sprintf("r%d", i), "yield": 1.5}
	}
	rows[0]["note"] = "fallback"
	flags := &rootFlags{asJSON: true, compact: true, selectFields: "id,note"}
	got := compactRows(t, rows, flags, nil)
	if got["r0"]["note"] != "fallback" || got["r0"]["id"] != "r0" {
		t.Fatalf("select lost note: %#v", got["r0"])
	}
	if _, ok := got["r0"]["yield"]; ok {
		t.Fatalf("select kept an unselected field: %#v", got["r0"])
	}
	if _, ok := got["r1"]["note"]; ok {
		t.Fatalf("select invented note: %#v", got["r1"])
	}
}

func compactRows(t *testing.T, rows []map[string]any, flags *rootFlags, keep []string) map[string]map[string]any {
	t.Helper()
	return compactRowsKept(t, rows, flags, keep...)
}

func compactRowsKept(t *testing.T, rows []map[string]any, flags *rootFlags, keep ...string) map[string]map[string]any {
	t.Helper()
	var out bytes.Buffer
	var err error
	if len(keep) == 0 {
		err = printJSONFiltered(&out, rows, flags)
	} else {
		err = printJSONFilteredKeep(&out, rows, flags, keep...)
	}
	if err != nil {
		t.Fatalf("printJSONFiltered: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("compact json: %v\n%s", err, out.String())
	}
	byID := map[string]map[string]any{}
	for _, row := range got {
		id, _ := row["id"].(string)
		byID[id] = row
	}
	return byID
}

func assertJSONArrayField(t *testing.T, v any, want string) {
	t.Helper()
	items, ok := v.([]any)
	if !ok || len(items) == 0 || items[0] != want {
		t.Fatalf("field = %#v, want [%s]", v, want)
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

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestPrintOutputWithFlagsPlainRendersTSV|TestPrintOutputWithFlagsPlainEmptyArrayWritesMarker|TestPrintOutputWithFlagsCSVEmptyArrayWritesMarker|TestPrintOutputWithFlagsCSVEmptyArrayWritesDeclaredHeader|TestPrintOutputWithFlagsPlainEmptyArrayWritesDeclaredHeader|TestPrintOutputWithFlagsCSVEmptyArrayEscapesDeclaredHeader|TestPrintOutputWithFlagsCSVNonEmptyArrayUsesCSV|TestPrintOutputWithFlagsCSVQuotesCarriageReturnInValues|TestPrintOutputWithFlagsMachineEmptyArrayIsValidJSON|TestPrintOutputWithFlagsCSVSingleObjectIsOneRow|TestPrintOutputWithFlagsCSVUnwrapsCollectionEnvelope|TestPrintOutputWithFlagsQuietPrintsIdentityValues|TestPrintOutputWithFlagsCompactReducesDocumentedLists|TestPrintOutputWithFlagsQuietFallbackIsDeterministic|TestPrintOutputWithFlagsCompactPreservesSparseEnvelopeMetadata|TestPrintOutputWithFlagsSchemaAwareCompactIgnoresEnvelopeFloor|TestPrintOutputWithFlagsSelectWinsOverCompact|TestHumanFriendlyForcesTableAndNoColorStripsANSI|TestTerminalControl", "-count=1")
	requireGeneratedCompiles(t, outputDir)
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
		if filepath.Base(path) == "analytics.go.tmpl" {
			require.NotContains(t, src, `counts["<nil>"]`,
				"analytics must not bucket missing group-by values under Go's <nil> string")
			require.Contains(t, src, `const missingGroupLabel = "(none)"`,
				"analytics table output should use a documented missing-value label")
		}
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
	selectIdx := strings.Index(src, "data, selectErr = filterFieldsChecked(data, flags.selectFields)")
	wrapIdx := strings.Index(src, "wrapped, err := wrapWithProvenance(data, prov)")
	require.GreaterOrEqual(t, selectIdx, 0)
	require.GreaterOrEqual(t, wrapIdx, 0)
	require.Less(t, selectIdx, wrapIdx,
		"search.go.tmpl must apply --select to the result array before wrapping it in the provenance envelope")
	require.NotContains(t, src, "if flags.asJSON {",
		"search.go.tmpl still branches only on --json, so other documented output flags can be bypassed")
	require.NotContains(t, src, "flags.asJSON || !isTerminal",
		"search.go.tmpl still lets piped auto-JSON override explicit machine format flags")
	require.NotContains(t, src, "selectErrorForDryRun",
		"search is not a dry-run plan path; all-miss --select must still exit 2 even when the persistent --dry-run flag is set")
}
