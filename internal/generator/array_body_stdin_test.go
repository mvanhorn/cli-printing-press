package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedCommandSendsArrayStdinBody(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("arraybody")
	apiSpec.Resources = map[string]spec.Resource{
		"uploads": {
			Description: "Manage uploads",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/uploads",
					Description: "List uploads",
				},
				"create": {
					Method:             "POST",
					Path:               "/uploads",
					Description:        "Create upload batch",
					RequestContentType: "application/json",
					BodyJSONFallback:   true,
					BodyIsArray:        true,
					Params: []spec.Param{{
						Name: "trace",
						Type: "string",
					}},
				},
			},
		},
		"batches": {
			Description: "Manage batches",
			Endpoints: map[string]spec.Endpoint{
				"create": {
					Method:             "POST",
					Path:               "/batches",
					Description:        "Create batch",
					RequestContentType: "application/json",
					BodyJSONFallback:   true,
					BodyIsArray:        true,
					Params: []spec.Param{{
						Name: "trace",
						Type: "string",
					}},
				},
			},
		},
		"profiles": {
			Description: "Manage profiles",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/profiles",
					Description: "List profiles",
				},
				"create": {
					Method:             "POST",
					Path:               "/profiles",
					Description:        "Create profile",
					RequestContentType: "application/json",
					Body: []spec.Param{{
						Name: "name",
						Type: "string",
					}},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	behaviorTest := `package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCreateSendsArrayStdinBody(t *testing.T) {
	var gotBody []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/uploads" {
			t.Fatalf("path = %q, want /uploads", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode top-level array body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
	}))
	defer server.Close()

	t.Setenv("ARRAYBODY_BASE_URL", server.URL)
	restoreStdin := replaceStdin(t, "[{\"contactId\":\"x\",\"role\":\"MEMBER\",\"notify\":true}]")
	defer restoreStdin()

	flags := &rootFlags{asJSON: true}
	cmd := newRootCmd(flags)
	cmd.SetArgs([]string{"uploads", "create", "--stdin"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(gotBody) != 1 {
		t.Fatalf("body length = %d, want 1: %#v", len(gotBody), gotBody)
	}
	if gotBody[0]["contactId"] != "x" || gotBody[0]["role"] != "MEMBER" || gotBody[0]["notify"] != true {
		t.Fatalf("body element lost through stdin path: %#v", gotBody[0])
	}
}

func TestCreateRejectsMalformedStdinJSONBeforeTransport(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv("ARRAYBODY_BASE_URL", server.URL)
	restoreStdin := replaceStdin(t, "[{\"broken\":]")
	defer restoreStdin()

	flags := &rootFlags{asJSON: true}
	cmd := newRootCmd(flags)
	cmd.SetArgs([]string{"uploads", "create", "--stdin"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "parsing stdin JSON") {
		t.Fatalf("error = %v, want parsing stdin JSON", err)
	}
	if calls != 0 {
		t.Fatalf("server called %d time(s), want 0", calls)
	}
}

func TestCreateRejectsNullStdinJSONBeforeTransport(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv("ARRAYBODY_BASE_URL", server.URL)
	restoreStdin := replaceStdin(t, "null")
	defer restoreStdin()

	flags := &rootFlags{asJSON: true}
	cmd := newRootCmd(flags)
	cmd.SetArgs([]string{"uploads", "create", "--stdin"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "expected JSON array, got null") {
		t.Fatalf("error = %v, want expected JSON array null error", err)
	}
	if calls != 0 {
		t.Fatalf("server called %d time(s), want 0", calls)
	}
}

func TestCreateDefaultsArrayFallbackBodyToEmptyArray(t *testing.T) {
	var gotBody []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if r.URL.Query().Get("trace") != "request-1" {
			t.Fatalf("trace query = %q, want request-1", r.URL.Query().Get("trace"))
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode top-level array body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
	}))
	defer server.Close()

	t.Setenv("ARRAYBODY_BASE_URL", server.URL)

	flags := &rootFlags{asJSON: true}
	cmd := newRootCmd(flags)
	cmd.SetArgs([]string{"uploads", "create", "--trace", "request-1"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(gotBody) != 0 {
		t.Fatalf("default body length = %d, want 0: %#v", len(gotBody), gotBody)
	}
}

func TestObjectBodyRejectsArrayStdinBeforeTransport(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv("ARRAYBODY_BASE_URL", server.URL)
	restoreStdin := replaceStdin(t, ` + "`" + `[{"name":"not-an-object"}]` + "`" + `)
	defer restoreStdin()

	flags := &rootFlags{asJSON: true}
	cmd := newRootCmd(flags)
	cmd.SetArgs([]string{"profiles", "create", "--stdin"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "parsing stdin JSON") {
		t.Fatalf("error = %v, want parsing stdin JSON", err)
	}
	if calls != 0 {
		t.Fatalf("server called %d time(s), want 0", calls)
	}
}

func TestPromotedCreateDefaultsArrayFallbackBodyToEmptyArray(t *testing.T) {
	var gotBody []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/batches" {
			t.Fatalf("path = %q, want /batches", r.URL.Path)
		}
		if r.URL.Query().Get("trace") != "request-2" {
			t.Fatalf("trace query = %q, want request-2", r.URL.Query().Get("trace"))
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode top-level array body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
	}))
	defer server.Close()

	t.Setenv("ARRAYBODY_BASE_URL", server.URL)

	flags := &rootFlags{asJSON: true}
	cmd := newRootCmd(flags)
	cmd.SetArgs([]string{"batches", "--trace", "request-2"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(gotBody) != 0 {
		t.Fatalf("default body length = %d, want 0: %#v", len(gotBody), gotBody)
	}
}

func replaceStdin(t *testing.T, data string) func() {
	t.Helper()
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdin: %v", err)
	}
	if _, err := w.WriteString(data); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	os.Stdin = r
	return func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "array_body_stdin_test.go"), []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "Test(Create(SendsArrayStdinBody|RejectsMalformedStdinJSONBeforeTransport|RejectsNullStdinJSONBeforeTransport|DefaultsArrayFallbackBodyToEmptyArray)|ObjectBodyRejectsArrayStdinBeforeTransport|PromotedCreateDefaultsArrayFallbackBodyToEmptyArray)", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}
