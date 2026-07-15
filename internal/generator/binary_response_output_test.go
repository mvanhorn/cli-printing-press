package generator

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratePromotedBinaryResponseSupportsStructuredOutput(t *testing.T) {
	t.Parallel()

	payload := []byte("%PDF-1.7\nbinary fixture")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		switch r.URL.Path {
		case "/certificate":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write(payload)
		case "/json-body":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"not-binary"}`))
		case "/text-body":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("not binary envelope JSON"))
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	apiSpec := minimalSpec("binary-output")
	apiSpec.BaseURL = server.URL
	apiSpec.Resources = map[string]spec.Resource{
		"certificate": {
			Description: "Download a certificate",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:         http.MethodGet,
					Path:           "/certificate",
					Description:    "Download a certificate",
					ResponseFormat: spec.ResponseFormatBinary,
				},
			},
		},
		"jsonbody": {
			Description: "Return valid non-envelope JSON",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:         http.MethodGet,
					Path:           "/json-body",
					Description:    "Return valid non-envelope JSON",
					ResponseFormat: spec.ResponseFormatBinary,
				},
			},
		},
		"textbody": {
			Description: "Return non-JSON text",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:         http.MethodGet,
					Path:           "/text-body",
					Description:    "Return non-JSON text",
					ResponseFormat: spec.ResponseFormatBinary,
				},
			},
		},
	}
	apiSpec.Learn.Disabled = true

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Export: true, MCP: true}
	require.NoError(t, gen.Generate())

	testPath := filepath.Join(outputDir, "internal", "cli", "binary_response_output_runtime_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func runBinaryCommand(t *testing.T, command string, args ...string) ([]byte, string, error) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MYAPI_TOKEN", "test-token")

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{command}, args...))
	err := root.Execute()
	return stdout.Bytes(), stderr.String(), err
}

func executeBinaryCommand(t *testing.T, args ...string) map[string]json.RawMessage {
	t.Helper()
	stdout, stderr, err := runBinaryCommand(t, "certificate", args...)
	if err != nil {
		t.Fatalf("certificate %v failed: %v\nstdout=%s\nstderr=%s", args, err, stdout, stderr)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(stdout, &got); err != nil {
		t.Fatalf("certificate %v emitted invalid JSON: %v\n%s", args, err, stdout)
	}
	return got
}

func requireBinaryEnvelope(t *testing.T, got map[string]json.RawMessage) {
	t.Helper()
	var binary bool
	if err := json.Unmarshal(got["_pp_binary"], &binary); err != nil || !binary {
		t.Fatalf("_pp_binary = %s, want true", got["_pp_binary"])
	}
	var encoding string
	if err := json.Unmarshal(got["encoding"], &encoding); err != nil || encoding != "base64" {
		t.Fatalf("encoding = %s, want base64", got["encoding"])
	}
	if len(got["data"]) == 0 {
		t.Fatal("binary envelope is missing data")
	}
}

func TestPromotedBinaryResponseOutputModes(t *testing.T) {
	t.Run("bare", func(t *testing.T) {
		stdout, stderr, err := runBinaryCommand(t, "certificate")
		if err != nil {
			t.Fatalf("bare certificate failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
		}
		want := "{\"_pp_binary\":true,\"content_type\":\"application/pdf\",\"encoding\":\"base64\",\"bytes\":23,\"data\":\"JVBERi0xLjcKYmluYXJ5IGZpeHR1cmU=\"}"
		if string(stdout) != want {
			t.Fatalf("bare output changed:\n got: %s\nwant: %s", stdout, want)
		}
	})

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "json", args: []string{"--json"}},
		{name: "compact", args: []string{"--compact"}},
		{name: "plain", args: []string{"--plain"}},
		{name: "select", args: []string{"--select", "_pp_binary,encoding,data"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireBinaryEnvelope(t, executeBinaryCommand(t, tc.args...))
		})
	}

	t.Run("csv", func(t *testing.T) {
		stdout, stderr, err := runBinaryCommand(t, "certificate", "--csv")
		if err != nil {
			t.Fatalf("certificate --csv failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
		}
		want := "_pp_binary,bytes,content_type,data,encoding\ntrue,23,application/pdf,JVBERi0xLjcKYmluYXJ5IGZpeHR1cmU=,base64\n"
		if string(stdout) != want {
			t.Fatalf("csv output changed:\n got: %s\nwant: %s", stdout, want)
		}
	})

	for _, command := range []string{"jsonbody", "textbody"} {
		t.Run("reject-"+command, func(t *testing.T) {
			stdout, stderr, err := runBinaryCommand(t, command, "--json")
			if err == nil {
				t.Fatalf("%s --json succeeded with non-envelope output: %s", command, stdout)
			}
			want := "binary response cannot be rendered as structured output; redirect stdout or use --deliver file:<path>"
			if err.Error() != want {
				t.Fatalf("%s --json error = %q, want %q; stderr=%s", command, err, want, stderr)
			}
			if len(stdout) != 0 {
				t.Fatalf("%s --json emitted output on rejection: %s", command, stdout)
			}
		})
	}

	t.Run("agent", func(t *testing.T) {
		got := executeBinaryCommand(t, "--agent")
		var results map[string]json.RawMessage
		if err := json.Unmarshal(got["results"], &results); err != nil {
			t.Fatalf("agent results must contain the binary envelope: %v", err)
		}
		requireBinaryEnvelope(t, results)
	})
}
`), 0o644))

	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "^TestPromotedBinaryResponseOutputModes$", "-count=1")
}
