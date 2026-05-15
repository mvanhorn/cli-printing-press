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
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"encoding/json"
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
			"envelope preserves null pagination cursor verbatim",
			`+"`"+`{"items":[{"id":"a"}],"next_cursor":null}`+"`"+`,
			"id",
			`+"`"+`{"items":[{"id":"a"}],"next_cursor":null}`+"`"+`,
		},
		{
			"flat object no match returns empty",
			`+"`"+`{"a":1,"b":2}`+"`"+`,
			"c",
			`+"`"+`{}`+"`"+`,
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
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestFilterFieldsEnvelopeDescent", "-count=1")
}
