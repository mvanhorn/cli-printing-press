package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFilterFieldsEnvelopeDescent_EmittedHelper guards the runtime behavior
// of filterFields against list-envelope responses inside the cli-printing-press
// repo's own test suite. The function is emitted into every printed CLI's
// internal/cli/helpers.go from helpers.go.tmpl. Without this gate, regressions
// in the envelope-descent fallback only surface when a user runs `go test ./...`
// inside a generated CLI, which slows the feedback loop and risks shipping a
// broken --select to every CLI built from a future bad commit.
//
// The test follows the TestRootFlagsPrintJSONHonorsOutputFlags pattern: it
// generates a CLI to a temp dir, writes a fixture _test.go alongside the
// emitted helpers, then runs `go test` on the generated module.
func TestFilterFieldsEnvelopeDescent_EmittedHelper(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("envelope-descent")
	outputDir := filepath.Join(t.TempDir(), "envelope-descent-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	testPath := filepath.Join(outputDir, "internal", "cli", "filter_fields_envelope_test.go")
	requireGeneratedCompiles(t, outputDir)

	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// TestFilterFieldsEnvelopeDescent covers the four shapes printed CLIs see in
// practice. The envelope cases pin the regression where wrapper-key + array
// responses returned `+"`{}`"+` because the selector heads matched the inner
// record fields, not the wrapper key.
func TestFilterFieldsEnvelopeDescent(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		fields string
		want   string
	}{
		{
			"bare array element-wise",
			`+"`"+`[{"id":"a","name":"x","other":"y"}]`+"`"+`,
			"id,name",
			`+"`"+`[{"id":"a","name":"x"}]`+"`"+`,
		},
		{
			"envelope single array sibling",
			`+"`"+`{"projects":[{"id":"a","name":"x","other":"y"}]}`+"`"+`,
			"id,name",
			`+"`"+`{"projects":[{"id":"a","name":"x"}]}`+"`"+`,
		},
		{
			"envelope with metadata sibling preserves count",
			`+"`"+`{"total_count":2,"items":[{"id":"a","other":"y"}]}`+"`"+`,
			"id",
			`+"`"+`{"items":[{"id":"a"}],"total_count":2}`+"`"+`,
		},
		{
			"nested type-keyed envelope preserves metadata",
			`+"`"+`{"artists":{"items":[{"id":"a","name":"Radiohead","other":"y"}]},"meta":{"total":1},"links":{"next":"/artists?page=2"}}`+"`"+`,
			"id,name",
			`+"`"+`{"artists":{"items":[{"id":"a","name":"Radiohead"}]},"meta":{"total":1},"links":{"next":"/artists?page=2"}}`+"`"+`,
		},
		{
			"envelope preserves null pagination cursor verbatim",
			`+"`"+`{"items":[{"id":"a"}],"next_cursor":null}`+"`"+`,
			"id",
			`+"`"+`{"items":[{"id":"a"}],"next_cursor":null}`+"`"+`,
		},
		{
			"flat object no match preserves input",
			`+"`"+`{"a":1,"b":2}`+"`"+`,
			"c",
			`+"`"+`{"a":1,"b":2}`+"`"+`,
		},
		{
			"nested object without collection preserves input",
			`+"`"+`{"artist":{"name":"Radiohead"}}`+"`"+`,
			"id",
			`+"`"+`{"artist":{"name":"Radiohead"}}`+"`"+`,
		},
		{
			"selector matches envelope key suppresses descent",
			`+"`"+`{"projects":[{"id":"a","other":"y"}]}`+"`"+`,
			"projects",
			`+"`"+`{"projects":[{"id":"a","other":"y"}]}`+"`"+`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := filterFields(json.RawMessage(tc.input), tc.fields)
			var gotV, wantV interface{}
			if err := json.Unmarshal(got, &gotV); err != nil {
				t.Fatalf("invalid json output: %v (raw=%s)", err, string(got))
			}
			if err := json.Unmarshal([]byte(tc.want), &wantV); err != nil {
				t.Fatalf("invalid want json: %v (raw=%s)", err, tc.want)
			}
			gotBytes, _ := json.Marshal(gotV)
			wantBytes, _ := json.Marshal(wantV)
			if string(gotBytes) != string(wantBytes) {
				t.Errorf("filterFields(%q, %q) = %s, want %s",
					tc.input, tc.fields, string(gotBytes), string(wantBytes))
			}
		})
	}
}

func TestFilterFieldsEnvelopeDescent_UnknownSelector(t *testing.T) {
	input := "{\"items\":[{\"id\":\"a\",\"name\":\"Alpha\"},{\"id\":\"b\",\"name\":\"Beta\"}]}"
	got, warning, err := filterFieldsWithWarning(t, input, "missing")
	if err == nil {
		t.Fatal("unknown selector should return a usage error")
	}
	if ExitCode(err) != 2 {
		t.Fatalf("ExitCode = %d, want 2", ExitCode(err))
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %q, want unmatched path named", err)
	}

	var gotV, wantV interface{}
	if err := json.Unmarshal(got, &gotV); err != nil {
		t.Fatalf("invalid json output: %v (raw=%s)", err, string(got))
	}
	if err := json.Unmarshal([]byte(input), &wantV); err != nil {
		t.Fatalf("invalid input json: %v", err)
	}
	gotBytes, _ := json.Marshal(gotV)
	wantBytes, _ := json.Marshal(wantV)
	if string(gotBytes) != string(wantBytes) {
		t.Fatalf("unknown selector changed the payload: got %s, want %s", gotBytes, wantBytes)
	}
	if !strings.Contains(string(warning), "--select \"missing\" matched no fields") {
		t.Fatalf("warning = %q, want unknown-selector warning", warning)
	}
	if !strings.Contains(string(warning), "valid fields: items") {
		t.Fatalf("warning = %q, want valid top-level fields", warning)
	}
}

func TestFilterFieldsEnvelopeDescent_StopsAtDepthBound(t *testing.T) {
	var input strings.Builder
	for i := 0; i < 33; i++ {
		fmt.Fprintf(&input, `+"`"+`{"level%d":`+"`"+`, i)
	}
	input.WriteString(`+"`"+`{"items":[{"id":"a","extra":"b"}]}`+"`"+`)
	for i := 0; i < 33; i++ {
		input.WriteByte('}')
	}

	got := filterFields(json.RawMessage(input.String()), "id")
	if string(got) != input.String() {
		t.Fatalf("overly deep envelope was unexpectedly traversed: got %s", got)
	}
}

func TestFilterFieldsEnvelopeDescent_EmptyCollectionsDoNotWarn(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		fields string
		want   string
	}{
		{"top-level array", `+"`"+`[]`+"`"+`, "id", `+"`"+`[]`+"`"+`},
		{"list envelope", `+"`"+`{"items":[]}`+"`"+`, "id", `+"`"+`{"items":[]}`+"`"+`},
		{"known dotted head", `+"`"+`{"events":[],"other":1}`+"`"+`, "events.name", `+"`"+`{"events":[]}`+"`"+`},
		{"multi-selector empty envelope", `+"`"+`{"items":[],"total_count":0}`+"`"+`, "id,name", `+"`"+`{"items":[],"total_count":0}`+"`"+`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, warning, err := filterFieldsWithWarning(t, tc.input, tc.fields)
			if err != nil {
				t.Fatalf("empty collection should stay non-fatal: %v", err)
			}
			if string(warning) != "" {
				t.Fatalf("warning = %q, want no warning for an empty collection", warning)
			}
			assertJSONEqual(t, got, tc.want)
		})
	}
}

func TestFilterFieldsEnvelopeDescent_PartiallyInvalidSelectorWarns(t *testing.T) {
	input := `+"`"+`[{"id":"a","name":"Alpha"}]`+"`"+`
	got, warning, err := filterFieldsWithWarning(t, input, "id,naem")

	if err != nil {
		t.Fatalf("mixed match should stay non-fatal: %v", err)
	}
	assertJSONEqual(t, got, `+"`"+`[{"id":"a"}]`+"`"+`)
	if !strings.Contains(string(warning), "--select \"naem\" matched no fields") {
		t.Fatalf("warning = %q, want warning naming the unmatched selector", warning)
	}
	if strings.Contains(string(warning), "--select \"id\" matched no fields") {
		t.Fatalf("warning = %q, valid selector id must not be reported", warning)
	}
}

func TestFilterFieldsEnvelopeDescent_EmptyEnvelopeSelectorWarnings(t *testing.T) {
	input := `+"`"+`{"items":[]}`+"`"+`
	cases := []struct {
		name           string
		fields         string
		wantWarnings   []string
		forbidWarnings []string
	}{
		{
			name:           "known prefix and unrelated typo",
			fields:         "items.id,naem",
			wantWarnings:   []string{},
			forbidWarnings: []string{"items.id", "naem"},
		},
		{
			name:         "multiple unrelated selectors",
			fields:       "naem,missing",
			wantWarnings: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, warning, err := filterFieldsWithWarning(t, input, tc.fields)
			if err != nil {
				t.Fatalf("empty envelope should stay non-fatal: %v", err)
			}
			assertJSONEqual(t, got, input)
			for _, field := range tc.wantWarnings {
				if !strings.Contains(string(warning), "--select \""+field+"\" matched no fields") {
					t.Fatalf("warning = %q, want warning naming %q", warning, field)
				}
			}
			for _, field := range tc.forbidWarnings {
				if strings.Contains(string(warning), "--select \""+field+"\" matched no fields") {
					t.Fatalf("warning = %q, indeterminate selector %q must not be reported", warning, field)
				}
			}
		})
	}
}

func TestFilterFields_EmptyEnvelopeMultiSelectStaysOK(t *testing.T) {
	input := `+"`"+`{"items":[],"total_count":0}`+"`"+`
	got, warning, err := filterFieldsWithWarning(t, input, "id,name")
	if err != nil {
		t.Fatalf("multi-selector on an empty envelope must stay exit 0: %v", err)
	}
	assertJSONEqual(t, got, input)
	if string(warning) != "" {
		t.Fatalf("warning = %q, want no warning when matching cannot be determined", warning)
	}
}

func TestFilterFields_CompatibilityWrapper(t *testing.T) {
	got := filterFields(json.RawMessage(`+"`"+`{"id":"a","name":"x"}`+"`"+`), "id")
	assertJSONEqual(t, got, `+"`"+`{"id":"a"}`+"`"+`)
}

func TestFilterFields_AllMissNamesEveryPath(t *testing.T) {
	input := `+"`"+`{"id":"a","name":"Alpha"}`+"`"+`
	got, warning, err := filterFieldsWithWarning(t, input, "all,bogus,names")
	assertJSONEqual(t, got, input)
	if err == nil {
		t.Fatal("expected usage error when every --select path misses")
	}
	if ExitCode(err) != 2 {
		t.Fatalf("ExitCode = %d, want 2", ExitCode(err))
	}
	for _, field := range []string{"all", "bogus", "names"} {
		if !strings.Contains(err.Error(), field) {
			t.Fatalf("error = %q, want it to name %q", err, field)
		}
		if !strings.Contains(string(warning), "--select \""+field+"\" matched no fields") {
			t.Fatalf("warning = %q, want warning naming %q", warning, field)
		}
	}
}

func TestFilterFields_HeterogeneousSupersetStaysOK(t *testing.T) {
	input := `+"`"+`[{"id":"a"},{"id":"b","company":"Acme"}]`+"`"+`
	got, warning, err := filterFieldsWithWarning(t, input, "id,name,company")
	if err != nil {
		t.Fatalf("superset select across heterogeneous rows should stay non-fatal: %v", err)
	}
	assertJSONEqual(t, got, `+"`"+`[{"id":"a"},{"company":"Acme","id":"b"}]`+"`"+`)
	if !strings.Contains(string(warning), "--select \"name\" matched no fields") {
		t.Fatalf("warning = %q, want unmatched name path", warning)
	}
	if strings.Contains(string(warning), "--select \"company\" matched no fields") {
		t.Fatalf("warning = %q, company present on some rows must not be treated as a miss", warning)
	}
}

func TestPrintOutputWithFlags_SelectAllMissKeepsJSON(t *testing.T) {
	input := json.RawMessage(`+"`"+`{"id":"a","name":"Alpha"}`+"`"+`)
	stdout, warning, err := printSelected(t, input, "zzz_nonexistent")
	if err == nil {
		t.Fatal("expected non-zero-class error for an all-miss --select")
	}
	if ExitCode(err) == 0 {
		t.Fatal("ExitCode = 0, want non-zero")
	}
	if !json.Valid(bytes.TrimSpace(stdout)) {
		t.Fatalf("stdout is not JSON: %q", stdout)
	}
	assertJSONEqual(t, json.RawMessage(bytes.TrimSpace(stdout)), string(input))
	if !strings.Contains(string(warning), "--select \"zzz_nonexistent\" matched no fields") {
		t.Fatalf("warning = %q, want unmatched path named", warning)
	}
	if !strings.Contains(err.Error(), "zzz_nonexistent") {
		t.Fatalf("error = %q, want unmatched path named", err)
	}
}

func TestPrintOutputWithFlags_SelectMixedMatchOK(t *testing.T) {
	input := json.RawMessage(`+"`"+`{"id":"a","name":"Alpha"}`+"`"+`)
	stdout, warning, err := printSelected(t, input, "id,nonexistent")
	if err != nil {
		t.Fatalf("mixed match should stay exit 0: %v", err)
	}
	if !json.Valid(bytes.TrimSpace(stdout)) {
		t.Fatalf("stdout is not JSON: %q", stdout)
	}
	assertJSONEqual(t, json.RawMessage(bytes.TrimSpace(stdout)), `+"`"+`{"id":"a"}`+"`"+`)
	if !strings.Contains(string(warning), "--select \"nonexistent\" matched no fields") {
		t.Fatalf("warning = %q, want unmatched path named", warning)
	}
}

func filterFieldsWithWarning(t *testing.T, input, fields string) (json.RawMessage, []byte, error) {
	t.Helper()
	oldStderr := os.Stderr
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error: %v", err)
	}
	os.Stderr = write
	got, ferr := filterFieldsChecked(json.RawMessage(input), fields)
	_ = write.Close()
	os.Stderr = oldStderr
	warning, _ := io.ReadAll(read)
	_ = read.Close()
	return got, warning, ferr
}

func printSelected(t *testing.T, input json.RawMessage, fields string) ([]byte, []byte, error) {
	t.Helper()
	var stdout bytes.Buffer
	oldStderr := os.Stderr
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error: %v", err)
	}
	os.Stderr = write
	printErr := printOutputWithFlags(&stdout, input, &rootFlags{asJSON: true, selectFields: fields})
	_ = write.Close()
	os.Stderr = oldStderr
	warning, _ := io.ReadAll(read)
	_ = read.Close()
	return stdout.Bytes(), warning, printErr
}

func assertJSONEqual(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var gotV, wantV interface{}
	if err := json.Unmarshal(got, &gotV); err != nil {
		t.Fatalf("invalid json output: %v (raw=%s)", err, string(got))
	}
	if err := json.Unmarshal([]byte(want), &wantV); err != nil {
		t.Fatalf("invalid want json: %v (raw=%s)", err, want)
	}
	gotBytes, _ := json.Marshal(gotV)
	wantBytes, _ := json.Marshal(wantV)
	if string(gotBytes) != string(wantBytes) {
		t.Fatalf("filterFields output = %s, want %s", gotBytes, wantBytes)
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "^(TestFilterFieldsEnvelopeDescent|TestFilterFieldsEnvelopeDescent_UnknownSelector|TestFilterFieldsEnvelopeDescent_EmptyCollectionsDoNotWarn|TestFilterFieldsEnvelopeDescent_PartiallyInvalidSelectorWarns|TestFilterFieldsEnvelopeDescent_EmptyEnvelopeSelectorWarnings|TestFilterFields_EmptyEnvelopeMultiSelectStaysOK|TestFilterFields_CompatibilityWrapper|TestFilterFields_AllMissNamesEveryPath|TestFilterFields_HeterogeneousSupersetStaysOK|TestPrintOutputWithFlags_SelectAllMissKeepsJSON|TestPrintOutputWithFlags_SelectMixedMatchOK)$", "-count=1")
}

func TestFilterFieldsCompatibilityWrapper_NovelCallerCompiles(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("select-compat")
	outputDir := filepath.Join(t.TempDir(), "select-compat-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "novel_select.go"), []byte(`package cli

import "encoding/json"

func projectSelected(data json.RawMessage, fields string) json.RawMessage {
	return filterFields(data, fields)
}
`), 0o644))
	requireGeneratedCompiles(t, outputDir)
}
