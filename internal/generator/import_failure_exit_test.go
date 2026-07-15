package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGeneratedImportFailureExitBehavior executes the emitted REST import
// command against a local HTTP server. This protects the command's exit-code
// contract rather than only asserting on template text.
func TestGeneratedImportFailureExitBehavior(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("importfail")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources["items"].Endpoints["create"] = spec.Endpoint{
		Method:      "POST",
		Path:        "/items",
		Description: "Create item",
		Body:        []spec.Param{{Name: "name", Type: "string", Required: true}},
		Response:    spec.ResponseDef{Type: "object"},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Import: true, MCP: true}
	require.NoError(t, gen.Generate())

	behaviorTest := `package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runImportBatch(t *testing.T, statusForName func(string) int, records int, asJSON bool) (error, string, string, int) {
	t.Helper()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusForName(fmt.Sprint(body["name"])))
		_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
	}))
	defer server.Close()
	t.Setenv("IMPORTFAIL_BASE_URL", server.URL)

	var input strings.Builder
	for i := 0; i < records; i++ {
		fmt.Fprintf(&input, ` + "`" + `{"name":"item-%d"}` + "`" + `+"\n", i)
	}
	inputPath := filepath.Join(t.TempDir(), "records.jsonl")
	if err := os.WriteFile(inputPath, []byte(input.String()), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}

	flags := &rootFlags{asJSON: asJSON}
	cmd := newImportCmd(flags)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"items", "--input", inputPath})
	stderrReader, stderrWriter, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("capture stderr: %v", pipeErr)
	}
	originalStderr := os.Stderr
	os.Stderr = stderrWriter
	err := cmd.Execute()
	os.Stderr = originalStderr
	if closeErr := stderrWriter.Close(); closeErr != nil {
		t.Fatalf("close stderr writer: %v", closeErr)
	}
	stderr, readErr := io.ReadAll(stderrReader)
	if readErr != nil {
		t.Fatalf("read stderr: %v", readErr)
	}
	if closeErr := stderrReader.Close(); closeErr != nil {
		t.Fatalf("close stderr reader: %v", closeErr)
	}
	return err, out.String(), string(stderr), requests
}

func TestImportBatchAllFailuresReturnsNonZeroAfterEnvelope(t *testing.T) {
	err, out, _, requests := runImportBatch(t, func(string) int { return http.StatusInternalServerError }, 2, true)
	if err == nil {
		t.Fatal("expected a typed import failure")
	}
	if code := ExitCode(err); code != 5 {
		t.Fatalf("exit code = %d, want 5; err=%v", code, err)
	}
	if requests < 2 {
		t.Fatalf("requests = %d, want both non-auth records attempted", requests)
	}
	for _, want := range []string{` + "`" + `"succeeded": 0` + "`" + `, ` + "`" + `"failed": 2` + "`" + `, ` + "`" + `"skipped": 0` + "`" + `} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %s: %s", want, out)
		}
	}
}


func TestImportBatchPartialFailureReportsCommittedCount(t *testing.T) {
	err, out, _, _ := runImportBatch(t, func(name string) int {
		if name == "item-0" {
			return http.StatusCreated
		}
		return http.StatusInternalServerError
	}, 2, true)
	if err == nil || ExitCode(err) != 5 {
		t.Fatalf("expected typed partial failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "1 succeeded") || !strings.Contains(err.Error(), "1 failed") {
		t.Fatalf("partial failure error missing counts: %v", err)
	}
	for _, want := range []string{` + "`" + `"succeeded": 1` + "`" + `, ` + "`" + `"failed": 1` + "`" + `} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %s: %s", want, out)
		}
	}
}

func TestImportBatchAuthFailureAbortsAfterEnvelope(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			err, out, _, requests := runImportBatch(t, func(string) int { return status }, 3, true)
			if err == nil {
				t.Fatal("expected an auth failure")
			}
			if code := ExitCode(err); code != 4 {
				t.Fatalf("exit code = %d, want 4; err=%v", code, err)
			}
			if requests != 1 {
				t.Fatalf("requests = %d, want immediate abort after the first auth failure", requests)
			}
			if !strings.Contains(out, ` + "`" + `"failed": 1` + "`" + `) {
				t.Fatalf("auth failure output missing failed count: %s", out)
			}
		})
	}
}

func TestImportBatchMidstreamAuthFailurePreservesCommittedCount(t *testing.T) {
	err, out, _, requests := runImportBatch(t, func(name string) int {
		if name == "item-0" {
			return http.StatusCreated
		}
		return http.StatusUnauthorized
	}, 3, true)
	if err == nil || ExitCode(err) != 4 {
		t.Fatalf("expected typed auth failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "1 succeeded, 1 failed, and 1 skipped") {
		t.Fatalf("auth failure error missing committed counts: %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want committed record plus auth failure only", requests)
	}
	for _, want := range []string{` + "`" + `"succeeded": 1` + "`" + `, ` + "`" + `"failed": 1` + "`" + `, ` + "`" + `"skipped": 1` + "`" + `} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %s: %s", want, out)
		}
	}
}

func TestImportBatchHumanFailurePrintsSummary(t *testing.T) {
	err, _, stderr, _ := runImportBatch(t, func(string) int { return http.StatusInternalServerError }, 1, false)
	if err == nil || ExitCode(err) != 5 {
		t.Fatalf("expected typed import failure, got %v", err)
	}
	if !strings.Contains(stderr, "Import complete: 0 succeeded, 1 failed, 0 skipped") {
		t.Fatalf("stderr missing human summary: %s", stderr)
	}
}

func TestImportBatchCleanRunKeepsZeroExit(t *testing.T) {
	err, out, _, requests := runImportBatch(t, func(string) int { return http.StatusCreated }, 2, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if !strings.Contains(out, ` + "`" + `"succeeded": 2` + "`" + `) || !strings.Contains(out, ` + "`" + `"failed": 0` + "`" + `) {
		t.Fatalf("unexpected output: %s", out)
	}
}
`

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "import_failure_exit_test.go"), []byte(behaviorTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestImportBatch", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}
